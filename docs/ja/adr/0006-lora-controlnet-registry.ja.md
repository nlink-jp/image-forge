# ADR-0006: LoRA と ControlNet を一級のモデル kind として登録する

- Status: Accepted
- Date: 2026-07-09

## Context

`gen` は既に LoRA と ControlNet を受け付けるが、**生のファイルパス**としてしか扱えない:

```sh
image-forge gen "..." --lora /some/path/lcm.safetensors:0.8 \
                      --control-net /some/path/canny.safetensors --control edge.png
```

他の誰もこれらのファイルの存在を知らない。レジストリ（`models list/pull/import/rm`）が
理解する kind は 2 つ — `"" `（diffusion）と `"upscaler"` — だけなので:

- image-forge を通して LoRA や ControlNet を**入手する**手段が無い。利用者は自分で探し、
  ダウンロードし、ファイルを置かなければならない。
- **フロントエンドが列挙できる**ものが無い。image-forge-gui はピッカーを出したいのに、
  `models list --json` は LoRA/ControlNet のエントリを返さない。
- **アーキテクチャ互換性**がどこにも記録されない。SDXL の LoRA を SD1.5 のベースに
  当てると、黙ってゴミが出る（あるいは sd.cpp の奥深くでエラーになる）。利用者が気づく
  のは描画時である。

一方で、常駐エンジンには設計へ encode しておく価値のある非対称性がある:
`reloadKey`（`internal/cli/render.go` 参照）は ControlNet のパスを含むが、LoRA は
**含まない**。つまり **LoRA の変更はリクエストごとで安く、ControlNet モデルの変更は
ベースモデルを再ロードする。**

## Decision

**LoRA と ControlNet を、`upscaler` が既にそうであるのと全く同じようにレジストリの
モデル kind として扱い**、生のパスではなく名前解決を主たるインターフェースにする。

1. **`catalog` と `store` に新しい kind を 2 つ**:

   ```go
   KindDiffusion  = ""          // 既定
   KindUpscaler   = "upscaler"
   KindLoRA       = "lora"       // 新規
   KindControlNet = "controlnet" // 新規
   ```

   `IsUpscaler()` と並んで `IsLoRA()` / `IsControlNet()` の述語を置く。

2. **アップスケーラと違い、これらは Arch を持つ。** アップスケーラはアーキテクチャ非依存で、
   素の `profile.Profile{Name}` を登録する。LoRA/ControlNet は学習元のベースアーキテクチャに
   縛られるので `profile.Profile{Name, Arch}` を登録する — prediction・clip-skip・VAE・
   hires 系のフィールドは持たない。したがって `models list --json` はこれらについて使える
   `arch` を報告し、それがフロントエンド（そして後には CLI）に非互換な組み合わせを
   フィルタさせる。

3. **キュレーションした一群の**（ESRGAN アップスケーラと同様の）**カタログエントリ**を置き、
   `models pull <name>` で入手できるようにする。既存の `Source{HF: …}` ダウンロード経路を
   再利用する — 新しい取得機構は要らない。

4. **名前がパスへ解決される**（`gen` / `serve` で）: `--lora <name-or-path>:<weight>` と
   `--control-net <name-or-path>`。正しい kind のインストール済みモデル名にあたる値は
   そのパスへ解決され、それ以外はパスとしてそのまま渡される。これは
   `resolveUpscalerModel` に倣ったもので、既存のパス指定の呼び出しはすべて動き続ける。

5. **serve プロトコルは変わらない。** すでに `loras: ["path:weight"]` と
   `control_net: "<path>"` を受け取る。GUI は `models list --json`（`path`・`kind`・`arch`
   を公開する）を読み、解決済みのパスを送る。新しい wire フィールドも、新しいエンジン表面も
   無い。

## Consequences

- `image-forge models pull lcm-lora-sdxl` が動く。フロントエンドは `models list --json`
  だけで LoRA/ControlNet を列挙し arch でフィルタできる。
- 既存の `--lora /abs/path.safetensors:0.8` という呼び出しは動き続ける — 解決は
  名前優先・パスへフォールバックである。
- レジストリに diffusion プロファイルを持たない kind が入る。「インストール済みモデルは
  描画できる」と仮定している箇所はすべて `Kind` を確認しなければならない。
  `installedViews` の `MultiComponent` ヒューリスティック
  （`Path == "" && Kind != "upscaler"`）は `Path == "" && Kind == KindDiffusion` に
  なる。でなければ LoRA が誤って報告される。
- Arch は助言的である: 記録しフィルタもするが、最終的な裁定者は sd.cpp である。
  不一致は、分かりにくい描画結果ではなく明快な事前エラーになる。
  （0.27 までは CLI がまったく比較していなかった。0.28.0 から `resolveAuxModel` が、
  インストール済みのモデルとアーキテクチャが異なるインストール済みの LoRA / ControlNet を
  拒否する。比べるのは事実であるアーキテクチャだけで、登録のたびにその出どころ —— カタログ、
  `--arch`（今は検証する）、名前からの推測（何にも当たらなければ SDXL）—— を記録し、推測は
  比べない。どちらかがパス指定なら判定は sd.cpp に任せ、0.28.0 より前の登録もカタログ自身の
  ものでなければ同様。）
- **ControlNet の変更はベースモデルを再ロードする**（`reloadKey` に含まれる）。LoRA の
  変更はしない。フロントエンドはこのコストを見せるべきである — セッション途中で
  ControlNet を差し替えるのは無料ではなく、LoRA の差し替えは無料である。
- LoRA の*スタッキング意味論*を sd.cpp がやること以上に実装しようとはしない（各
  `path:weight` を描画ごとに適用する）。
- **sd.cpp の `lora_apply_mode` を `at_runtime` に固定する。** その `auto` 既定は非量子化の
  重みに対して `immediately` を選び、LoRA をモデルのパラメータへ事前にマージする
  （`ModelManager::apply_loras_to_params`）。この経路は SDXL の LCM-LoRA のような
  UNet 専用 LoRA（`lora_unet_*` テンソル 2364 個、`lora_te*` なし）で **SIGSEGV** し、
  プロセス全体を落とす — GUI が依存する常駐 `serve` エンジンにとっては致命的である。
  `at_runtime` は代わりに forward パスの途中で LoRA を適用する: 一度のマージではなく
  ステップごとの計算コストを払う代わりに、堅牢になる。速度より正しさを採る。上流が
  マージ経路を直したら再考する。
