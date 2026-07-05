# image-forge

> **状態: 初期スキャフォールド（Phase 2）。** コマンド体系とモデルカタログは配置済み。
> 拡散ランタイム（静的リンクする stable-diffusion.cpp）は未接続。設計全体は
> [docs/ja/image-forge-rfp.ja.md](docs/ja/image-forge-rfp.ja.md) を参照。

macOS（Apple Silicon）向けのローカル拡散画像生成エンジン兼モデル運用 CLI。アニメ系
（Animagine XL / Illustrious / Pony 系）から汎用高品質（FLUX / SD3.5 / Z-Image）まで、
**内部設定を一切触らずに** ローカルで動かす。

モデルごとの落とし穴（CLIP-skip、SDXL fp16-fix 専用 VAE、native 解像度、sampler/steps、
予測方式、量子化）は **モデルプロファイル** に隠蔽して自動適用する。`image-forge` は
`gem-image`（クラウド Gemini）に対するローカル拡散版の対。

## 動作要件

- **Apple Silicon（arm64）の macOS** + Metal。
- **メモリ: 16GB ベースライン（最低）/ 32GB 以上推奨。** SDXL / Z-Image は 16GB で快適。
  FLUX / SD3.5 Large / Qwen-Image は 16GB では Q4 量子化前提、32GB+ で快適。
- ビルドツールチェーン（エンジンビルド時のみ）: `cmake`、Xcode の **Metal Toolchain**、
  CGO 有効な Go 1.26+。

モデル本体は **同梱しない**。各自でダウンロードする（カタログが各モデルのライセンスと
コンテンツ格付けを表示する）。

## インストール / ビルド

```sh
# スキャフォールドバイナリ（拡散ランタイム無し・開発用）:
make build

# sd.cpp ランタイムを静的リンクした完全版（cmake + Metal Toolchain 必要）:
brew install cmake
xcodebuild -downloadComponent MetalToolchain
make build-engine
```

出力は `dist/image-forge` の単一バイナリ。

## 使い方

```sh
# 生成（設定はモデルプロファイルから。フラグで上書き可）:
image-forge gen -p "score_9, 1girl, cherry blossom" -m animagine-xl-4 -o out.png

# img2img (--strength は 0..1: 低いほど初期画像を保持、高いほどプロンプト追従):
image-forge gen -p "..." --init in.png --strength 0.6 -o out.png

# LoRA 適用:
image-forge gen -p "..." --lora my-style:0.8 --lora detail:0.4

# モデル運用（DL + VAE取得 + RAM連動量子化 + プロファイル登録）:
image-forge models list
image-forge models pull animagine-xl-4
image-forge models import ~/Downloads/my-checkpoint.safetensors
image-forge models quantize animagine-xl-4 --to q4_k

# 常駐モード: モデルを一度ロードして連投（stdin に 1 行 1 JSON リクエスト）:
echo '{"prompt":"1girl, cherry blossoms","model":"animagine-xl-4","output":"a.png"}' | image-forge serve
```

進捗は stderr への JSON 行ストリーム（`load` / `progress` / `done` / `error`）、
1 行 1 イベントで出力。

## モデルとコンテンツ格付け

カタログは各エントリに `content_rating`（`safe` / `questionable` / `explicit`）と
`license` を付与する。questionable/explicit のモデルは明示オプトイン
（`--allow-nsfw` または config の `allow_nsfw = true`）が必要。最終判断は利用者に委ねる。

ダウンロード元は Hugging Face / Civitai / 直 URL。トークンは `HF_TOKEN` /
`CIVITAI_TOKEN`（環境変数または config）で渡す — **絶対にコミットしない**。

> **v-prediction 系モデル**（NoobAI、Illustrious v2）は *experimental* 扱い。
> stable-diffusion.cpp の v-pred / ZSNR 対応が発展途上のため。eps（epsilon-prediction）
> 系（Animagine XL 4.0、Illustrious v1、Pony）は確実に動作する。

## 設定

`~/.config/image-forge/config.toml` — 既定モデル、量子化方針、`allow_nsfw`、出力先。
環境変数: `HF_TOKEN`、`CIVITAI_TOKEN`。

## 開発

```sh
make build   # スキャフォールドバイナリ
make test    # go test ./...
make vet     # go vet ./...
```

**util-series** の一員。構造と注意点は [AGENTS.md](AGENTS.md) を参照。
