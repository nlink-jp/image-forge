# image-forge

macOS（Apple Silicon）向けのローカル拡散画像生成エンジン兼モデル運用 CLI。アニメ系
（Animagine XL / Illustrious / Pony 系）から汎用高品質（FLUX / SD3.5 / Z-Image）まで、
**内部設定を一切触らずに** ローカルで動かす。

モデルごとの落とし穴（CLIP-skip、SDXL fp16-fix 専用 VAE、native 解像度、sampler/steps、
予測方式）は **モデルプロファイル** に隠蔽して自動適用する。`image-forge` は `gem-image`
（クラウド Gemini）に対するローカル拡散版の対。

中核は [stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp)
（ggml/Metal）で、Go 単一バイナリに静的リンク。

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
brew install cmake
xcodebuild -downloadComponent MetalToolchain
make build-engine            # dist/image-forge に単一バイナリ

make build                   # ランタイム無しのスキャフォールドバイナリ（開発用）
```

## クイックスタート

```sh
# 1. モデル取得 — チェックポイント＋専用VAEをDLしプロファイル登録:
image-forge models pull animagine-xl-4 --allow-nsfw

# 2. 生成 — CLIP-skip / VAE / 1024 / sampler はプロファイルが自動で埋める:
image-forge gen -m animagine-xl-4 -p "1girl, cherry blossoms, masterpiece" -o out.png
```

## プロファイルの仕組み

各モデルは、良い出力に必要な設定（アーキ、CLIP-skip、専用VAE、native解像度、sampler、
steps、CFG、プロンプト前置）を encode した **プロファイル** を持つ。`gen -m <name>` は
それを自動適用し、明示的に渡したフラグが上書きする。これにより、Pony/Animagine の SDXL
モデルが CLIP-skip 2・1024 キャンバス・fp16-fix VAE を必要とすることを知らなくても、
正しく動かせる。

## コマンド

### `gen` — 生成

| フラグ | 意味 |
| --- | --- |
| `-p` | プロンプト（必須） |
| `-n` | ネガティブプロンプト |
| `-m` | インストール済みモデル名（`models list` 参照） |
| `--model-path` | モデルファイルの直接指定（レジストリを迂回） |
| `-o` | 出力パス（既定 `out.png`、バッチは連番付与） |
| `--seed` | シード（既定 42、`-1` = ランダム） |
| `--count` | 生成枚数。`--seed -1` と併用で各画像に別々のランダムseed（ファイル名は `<out>-<seed>.png`、seedを表示） |
| `--steps` `--cfg` `-W` `-H` `--sampler` `--scheduler` `--clip-skip` | プロファイルを上書き（`--scheduler`: discrete / karras / exponential / ays / …） |
| `--vae` | 外部 VAE（プロファイルを上書き） |
| `--prediction` | `eps` / `v`（v-prediction）/ `auto` を強制。既定はモデルプロファイル |
| `--batch` | 1回あたりの生成枚数（sd.cpp batch、連番seed） |
| `--init` `--strength` | img2img: 初期画像＋denoise強度（0..1、低いほど初期画像寄り） |
| `--mask` | inpaint（`--init` と併用）: マスクの白領域のみ再生成（init と同サイズ） |
| `--lora <path>:<weight>` | LoRA 適用（複数指定可） |
| `--control-net <model>` `--control <image>` | ControlNet: 制御画像で生成を誘導（`--control-strength`、`--canny` でエッジ前処理） |

進捗は stderr への JSON 行ストリーム（`load` / `progress` / `done` / `error`）、
1 行 1 イベント。出力パスは stdout に表示。

### `models` — モデル運用

```sh
image-forge models list                                  # カタログ＋インストール済み
image-forge models pull <name | hf:owner/repo/file | civitai:<versionId> | url> [--allow-nsfw] [--name N]
image-forge models import <path> [--name N] [--arch A] [--vae V]
image-forge models quantize <name> --to <type> [--name N]
image-forge models rm <name>
```

- **pull**: カタログ名をソースに解決し、チェックポイントと（カタログエントリなら）
  専用 VAE を DL してプロファイル登録。生の `hf:owner/repo/file` 参照、
  `civitai:<versionId>` 参照（Civitai モデルのダウンロードURLの数字。`CIVITAI_TOKEN` 必須）、
  直 URL も可。**多コンポーネントモデル**（FLUX等）は diffusion + テキストエンコーダ +
  VAE の全ファイルを自動でDL。ダウンロードはレジューム＋リトライ対応（大容量DL中に接続が
  切れても最初からやり直さない）。
- **import**: 手元のモデルファイルを登録。アーキは名前から自動判定（`--arch
  sdxl|sd15|sd35|flux|zimage` で上書き）。
- **quantize**: 登録済みモデルを `--to` ∈
  `q8_0 q5_0 q5_1 q4_0 q4_1 q2_k q3_k q4_k q5_k q6_k f16 f32` の GGUF に変換し、VAE を
  bake、`<name>-<type>` として登録。q8_0 ≈ ほぼ無劣化で約半分、q4_* ≈ 約1/3（省メモリ）。

### `serve` — 常駐モード

モデルを一度ロードして多数のリクエストを処理する（モデル変化時のみ再ロード）。毎回の
モデルロード＋Metal 初期化を回避。

```sh
image-forge serve < requests.jsonl
```

**入力** — stdin に 1 行 1 JSON:

```json
{"prompt":"1girl, cherry blossoms","model":"animagine-xl-4","seed":1,"output":"a.png"}
```

フィールド: `prompt`（必須）；`model` または `model_path`；任意で `negative`, `seed`,
`steps`, `cfg`, `width`, `height`, `sampler`, `scheduler`, `prediction`,
`clip_skip`, `batch`, `init`, `mask`, `strength`, `loras`（`["path:weight", ...]`）,
`control_net`, `control`, `control_strength`, `canny`, `output`, `vae`。任意
フィールドが無ければプロファイル既定にフォールバック。`seed: -1` でランダムseed（`done`
イベントで報告）。

**出力** — stdout に 1 行 1 イベント:
開始時 `{"kind":"ready"}`、（再）ロード時 `{"kind":"load","message":"<path>"}`、
ステップ毎 `{"kind":"progress","progress":0.5}`、画像毎 `{"kind":"done","output":"a.png"}`、
失敗時 `{"kind":"error","message":"..."}`。

## モデルとコンテンツ格付け

カタログは各エントリに `content_rating`（`safe` / `questionable` / `explicit`）と
`license` を付与する。questionable/explicit のモデルは明示オプトイン（`--allow-nsfw`）が
必要。最終判断は利用者に委ねる。

ダウンロード元は Hugging Face / Civitai / 直 URL。トークンは `HF_TOKEN` /
`CIVITAI_TOKEN`（環境変数）で渡す — **絶対にコミットしない**。

> **v-prediction 系モデル**（NoobAI、Illustrious v2）も動作する。モデルのプロファイルが
> v-prediction を自動設定する（`--prediction v|eps|auto` で上書き可）。eps 系
> （Animagine XL 4.0、Illustrious v1、Pony）も同様に動作する。

## 設定

- **データディレクトリ**: `$IMAGE_FORGE_HOME`（既定 `~/.local/share/image-forge`）に
  モデルレジストリ（`registry.json`）と DL 済みモデルファイル（`models/`）を格納。
- **設定ファイル**（任意）: `~/.config/image-forge/config.toml`（`$XDG_CONFIG_HOME` /
  `$IMAGE_FORGE_CONFIG` を尊重）。`default_model` / `output` / `allow_nsfw` /
  フォールバックトークンを設定。[`config.example.toml`](config.example.toml) をコピーして
  編集。（v0.5前の場所 `$IMAGE_FORGE_HOME/config.toml` も後方互換で読む。）
- **トークン**: `HF_TOKEN`（gated HF リポ）、`CIVITAI_TOKEN`（Civitai DL）。環境変数が
  設定ファイルより優先。**トークンは絶対にコミットしない。**

## 開発

```sh
make build          # スキャフォールドバイナリ（ランタイム無し）
make build-engine   # sd.cpp ランタイム込みの完全版
make test           # go test（third_party 除外）
make vet
```

**util-series** の一員。構造と注意点は [AGENTS.md](AGENTS.md)、設計全体は
[docs/ja/image-forge-rfp.ja.md](docs/ja/image-forge-rfp.ja.md) を参照。
