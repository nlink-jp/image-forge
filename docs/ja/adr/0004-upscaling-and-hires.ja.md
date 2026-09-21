# ADR-0004: アップスケーリング — 単体 ESRGAN + プロファイル駆動の hires.fix

- Status: Accepted
- Date: 2026-07-07

## Context

DiffusionBee（image-forge が参照しているアプリ）には **Upscale** 機能があり、カタログの
多くのモデル — 特に Pony / Illustrious 系のアニメ SDXL — は Civitai のページに
*「hires.fix は常に有効にせよ」*といった注記を載せている。ここには別個の需要が 2 つある:

1. **任意の既存画像をアップスケールする**（超解像、後処理）。
2. **hires 品質で生成する** — *hires.fix* パイプライン（ネイティブ解像度で生成 →
   アップスケール → より高い解像度で 2 回目の img2img パスをかけ、ディテールを足して
   アーティファクトを直す）。これは **モデルごとの推奨事項**であり、まさに image-forge が
   プロファイルの裏に隠している種類の落とし穴である（CLIP-skip・VAE・スコアタグと同じ）。

stable-diffusion.cpp は両方をネイティブに備えている:

- 単体: `new_upscaler_ctx(esrgan_path, …)` + `upscale(ctx, img, factor, …)` +
  `free_upscaler_ctx`。ESRGAN モデル（Real-ESRGAN）が必要。
- hires.fix: `sd_img_gen_params_t` が `sd_hires_params_t hires` を内包する
  （`enabled`・`upscaler`・`model_path`・`scale`・`denoising_strength`・`steps`・
  `upscale_tile_size`）。`sd_hires_params_init` の既定は `upscaler = LATENT`・
  `scale = 2.0`・`denoise = 0.7` — つまり hires は **追加モデル無し**で動く（latent
  アップスケーラ）。ESRGAN モデルは任意の品質向上手段である。

## Decision

**両方を採用する。**ただし image-forge 流に統合する。

### 1. 単体アップスケーラ

- エンジン: アップスケーラ ctx API をラップする `Upscale(input, esrganPath, factor)`。
- CLI: `image-forge upscale <in> -o <out> [--scale N] [--model <name> | --model-path <p>]`。
- MCP: `upscale` ツール（ワークスペース相対の入力パス → アップスケール後の出力パス）。
- ESRGAN モデルは一級だが **非 diffusion** のカタログ項目である — §3 を参照。

### 2. 生成時の hires.fix。プロファイルが駆動する

- **モデルプロファイル**が hires の既定値（`enabled`・`scale`・`denoise`・`upscaler`・
  `steps`）を持つようになる。上流の注記が「hires を使え」と言っているモデルは、カタログ
  エントリに `Hires.Enabled = true` を載せて出荷する。これにより `gen -m <model>` は
  追加フラグ無しで hires 品質の出力を出す — 落とし穴は隠れたままになる。
- 利用者は **`--hires auto|on|off`** で上書きする（`--prediction eps|v|auto` に倣う）:
  **`auto`（既定）はプロファイルに従い**、`on` は強制的に有効、`off` は強制的に無効。
  細かい上書きは `--hires-scale`・`--hires-denoise`・
  `--hires-upscaler latent|lanczos|nearest|model`・`--hires-model <esrgan>`。
- image-forge の意見を持った既定値（上書き可能で、sd.cpp より控えめ）: アップスケーラは
  `latent`（ダウンロード不要）、**`scale 1.5`**、**`denoise 0.5`** — 2.0/0.7 は 16 GB の
  基準機には重く、0.7 は元の構図から離れすぎる。
- `serve` と `mcp` の `generate` ツールも同じ hires 制御を受け付ける。

### 2b. hires アップスケーラの選択はダウンロード済み ESRGAN モデルを使い、config で駆動する

hires のアップスケーラは組み込みの latent に固定されていない。利用者が ESRGAN
アップスケーラを pull したら、hires.fix はそれを自動的に使える。hires アップスケーラの
解決優先順位（先に当たったものが勝つ）:

1. CLI `--hires-upscaler`（`latent|lanczos|nearest|model`）+ `--hires-model <name>`。
2. モデルプロファイルの hires 設定。
3. Config `[hires] upscaler` — `"latent"`（組み込み）・`"auto"`・またはインストール済み
   アップスケーラモデル名。
4. 組み込み `latent` へのフォールバック。

`"auto"`（config の既定）の意味は: **ダウンロード済みの ESRGAN アップスケーラが入って
いればそれを使い、無ければ `latent` に戻る。** モデルが必要になったとき（hires の
`upscaler=model`、または `--model` 無しの単体 `upscale` コマンド）、ESRGAN は次の順で
選ばれる: `--hires-model` / `--model` → プロファイル → `[upscaler] default_model` →
インストール済みが 1 つだけならそれ →（hires なら）latent に戻る /（`upscale` なら）
「pull せよ」という明快なエラー。

Config:

```toml
[hires]
# hires.fix のアップスケーラ: "latent"（組み込み）| "auto"（ダウンロード済みの ESRGAN が
# 入っていればそれ、無ければ latent）| インストール済みアップスケーラモデル名。
upscaler = "auto"

[upscaler]
# モデルが必要になったときに使う既定の ESRGAN（hires=model、--model 無しの `upscale`）
default_model = "realesrgan-x4-anime"
```

これにより、ダウンロード不要の経路（latent）を動いたまま保ちつつ、より良いアップスケーラが
存在した瞬間に自動的に格上げされる — 挙動は完全にオプション/config で制御される。

### 3. カタログ内の ESRGAN モデル（非 diffusion の `Kind`）

`catalog.Entry` と `store.InstalledModel` が `Kind`（既定の `""`/`diffusion`、または
`upscaler`）を持つようになる。アップスケーラのエントリは `Kind: upscaler` + `Source.HF`
に ESRGAN ファイルを設定し、VAE / prediction / profile は持たない。`models pull` はこれを
ダウンロードして登録し、`models list` は kind を表示し、`upscale --model` と
`--hires-upscaler model --hires-model` は `upscaler` kind のモデルを解決する。初期
エントリ（ungated な HF）: `realesrgan-x4plus`（汎用、
`schwgHao/RealESRGAN_x4plus/RealESRGAN_x4plus.pth`）と `realesrgan-x4-anime`
（`utnah/esrgan/RealESRGAN_x4plus_anime_6B.pth`）。

## Consequences

- DiffusionBee 流のアップスケーリングと「常に hires を使え」という推奨の両方がカバー
  される。hires はプロファイルへ畳み込まれるので、利用者はそれを知らなくてよい。
  `--hires auto/on/off` が逃げ道を残す。
- **コスト**: hires.fix は生成時間をおおよそ 2 倍にし（2 回目のパス）、ピークメモリを
  上げる。控えめな `scale 1.5` の既定が 16 GB を使える範囲に保つ。README に記載し、
  大きなターゲットを要求された場合は CLI が警告する。
- カタログ/レジストリのエントリに新しい `Kind` が付く — 小さく加算的で、既定値は既存の
  挙動を保つ。
- latent の hires はダウンロード不要。ESRGAN（単体および `--hires-upscaler model`）は
  1 つの Real-ESRGAN モデルを共有し、必要に応じて pull される。
