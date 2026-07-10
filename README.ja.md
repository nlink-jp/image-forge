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
| `--lora <name\|path>:<weight>` | LoRA 適用（複数指定可）。インストール済み LoRA はレジストリ名で解決、パスも可。レンダごとに適用＝**モデル再ロードなし** |
| `--control-net <name\|path>` `--control <image>` | ControlNet: 制御画像で生成を誘導（`--control-strength`、`--canny` でエッジ前処理）。**ControlNet を変えるとベースモデルが再ロードされる** |
| `--hires auto\|on\|off` | hires.fix（生成→拡大→ディテール付与の2nd img2imgパス）。`auto`(既定)はプロファイルに従う、`on`/`off`で強制 |
| `--hires-scale` `--hires-denoise` `--hires-upscaler latent\|lanczos\|nearest\|model` `--hires-model <name\|path>` | hires 微調整（既定: latent / scale 1.5 / denoise 0.5） |
| `--no-metadata` | プロンプト/パラメータ/モデルを PNG に埋め込まない |

進捗は stderr への JSON 行ストリーム（`load` / `progress` / `done` / `error`）、
1 行 1 イベント。出力パスは stdout に表示。

**メタデータ埋め込み**: 生成PNGは既定でプロンプト・パラメータ・モデルをテキストチャンクに
記録する — **AUTOMATIC1111互換の `parameters` チャンク**（Civitai/A1111 が解釈）＋完全な
**`image-forge` JSON** チャンク。日本語等のUnicodeプロンプトは `iTXt`(UTF-8) で文字化けなし。
`--no-metadata` または config `[metadata] embed = false` でオフ（共有画像にプロンプトを
残したくない場合など）。

### `upscale` — 画像の超解像

```sh
image-forge upscale <input> -o <output> [--scale N] [--model <name> | --model-path <path>]
```

既存画像に独立した Real-ESRGAN パス（通常4x）を実行。ESRGAN モデルはインストール済みの
`upscaler` 種別モデル（`--model`、例 `realesrgan-x4plus` / `realesrgan-x4-anime` を
`models pull`）か直 `--model-path` から解決。どちらも無ければ config `[upscaler]
default_model` かインストール済みが1つならそれを使用。進捗は stderr に JSON、出力パスは
stdout に表示。

### `models` — モデル運用

```sh
image-forge models list [--catalog|--all] [--json] [--kind K]   # インストール済み(既定)/カタログ/両方
image-forge models pull <name | hf:owner/repo/file | civitai:<versionId> | url> [--allow-nsfw] [--name N]
image-forge models import <path> [--name N] [--arch A] [--vae V] [--kind K]
image-forge models quantize <name> --to <type> [--name N]
image-forge models rm <name>
```

レジストリは 4 つの **kind**（`--kind diffusion|lora|controlnet|upscaler`）を持ちます。
ベースの拡散モデルに加え、単体では描画できない補助モデル 3 種です。LoRA と
ControlNet は学習時のベース**アーキテクチャ**を記録するため、非互換な組み合わせを
事前に弾けます（ADR-0006）。

```sh
image-forge models pull lcm-lora-sdxl          # LoRA も他のモデルと同じように取得
image-forge models list --kind lora --json     # フロントエンドが列挙する対象
image-forge gen -p "a red apple" -m animagine-xl-4 \
  --lora lcm-lora-sdxl:1.0 --steps 6 --cfg 1.5 --sampler lcm
```

- **list**: 既定では**インストール済み**モデルを表示（名前・アーキ・格付け・
  ライセンス・パス）。pull した ESRGAN アップスケーラもアーキ `upscaler` として並ぶ。
  `--catalog` はカタログ（`installed` 列付き）、`--all` は両方をセクション分けで表示。
  いずれも `--json` で機械可読出力（各エントリに `kind`＝`""`/diffusion か `upscaler`；
  installed→配列、catalog→`installed` フラグ付き配列、`--all`→`installed`/`catalog` 配列を持つオブジェクト）。
- **pull**: カタログ名をソースに解決し、チェックポイントと（カタログエントリなら）
  専用 VAE を DL してプロファイル登録。生の `hf:owner/repo/file` 参照、
  `civitai:<versionId>` 参照（Civitai モデルのダウンロードURLの数字。`CIVITAI_TOKEN` 必須）、
  直 URL も可。**多コンポーネントモデル**（FLUX等）は diffusion + テキストエンコーダ +
  VAE の全ファイルを自動でDL。ダウンロードはレジューム＋リトライ対応（大容量DL中に接続が
  切れても最初からやり直さない）。既に手元にある同一ファイル（別名で登録済みでも）は
  再ダウンロードせず再利用する。
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

### `mcp` — MCP サーバー（AIから使う）

画像生成を MCP（stdio 上の JSON-RPC 2.0）で AI に公開する。常駐エンジンを再利用。

```sh
image-forge mcp [--workspace-root <dir>]
```

voice-/video-studio の MCP サーバー同様 **file-mediated**（ツールは画像bytesではなく
ファイル**パス**を返す）。作業は**ワークスペース**ディレクトリ内（既定ルートはデータ
ディレクトリ下、または呼び出し毎に `workspace_root` を指定）、生成PNGは `output/` に出力。
生成は1〜2分かかるため**非同期** — `generate` は即座に `job_id` を返し、クライアントが
ポーリングする。

ツール:

- **`get_usage`** — 操作マニュアル（ワークスペースモデル・パラメータ・jobライフサイクル・
  リカバリ表）。最初に呼ぶ。
- **`generate`** — 生成を投入: `workspace_id` + `prompt`（必須）、任意で `model`,
  `negative`, `seed`, `steps`, `cfg`, `width`, `height`, `sampler`, `scheduler`,
  `clip_skip`, `batch`, `init`/`mask`/`strength`（img2img/inpaint、ワークスペース相対
  パス）, `hires`(auto/on/off) + `hires_scale`/`hires_denoise`/`hires_upscaler`/`hires_model`,
  `output_name`。`job_id` を返す。
- **`upscale`** — ワークスペース画像の Real-ESRGAN 拡大を投入: `workspace_id` +
  `input`（必須）、任意で `model`/`scale`/`output_name`。`job_id` を返す。
- **`check_job`** — `job_id` をポーリング: `state`(queued/running/done/error)・進捗、
  完了時に出力PNGパスとseed。
- **`list_models`** — インストール済み / カタログ（`scope`）。`models list --json` と同一ビュー。

エラーは構造化 `{code, message, details}`。クライアントには `image-forge` バイナリを
`mcp` 引数付きで登録する。設計は ADR-0003 を参照。

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
- **モデルディレクトリ**（大容量ディスク向け）: config の `models_dir`（または
  `$IMAGE_FORGE_MODELS_DIR`）で数GBのモデルファイルを別の場所に置ける。**新規** pull に
  適用され、インストール済みモデルはレジストリの絶対パスを保持するため両方の場所が共存する
  （既存を移すには `models rm` + 再pull）。小さな `registry.json` はデータディレクトリに残る。
- **設定ファイル**（任意）: `~/.config/image-forge/config.toml`（`$XDG_CONFIG_HOME` /
  `$IMAGE_FORGE_CONFIG` を尊重）。`default_model` / `output` / `allow_nsfw` /
  フォールバックトークン、hires アップスケーラ方針（`[hires] upscaler` 既定 `"auto"`＝DL済
  ESRGANがあればそれ、無ければ内蔵latent；`[upscaler] default_model`）を設定。
  [`config.example.toml`](config.example.toml) をコピーして編集。（v0.5前の場所
  `$IMAGE_FORGE_HOME/config.toml` も後方互換で読む。）
- **トークン**: `HF_TOKEN`（gated HF リポ）、`CIVITAI_TOKEN`（Civitai DL）。環境変数が
  設定ファイルより優先。**トークンは絶対にコミットしない。**

## 開発

```sh
make build          # スキャフォールドバイナリ（ランタイム無し）
make build-engine   # sd.cpp ランタイム込みの完全版
make test           # go test（third_party 除外）
make vet
```

**util-series** の一員。構造と注意点は [AGENTS.md](AGENTS.md)、カタログへのモデル追加は
[docs/ja/adding-a-model.ja.md](docs/ja/adding-a-model.ja.md)、設計全体は
[docs/ja/image-forge-rfp.ja.md](docs/ja/image-forge-rfp.ja.md) を参照。

## ライセンス

MIT — [LICENSE](LICENSE) を参照。

配布バイナリは [stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp)
（MIT, © 2023 leejet）と [ggml](https://github.com/ggml-org/ggml)（MIT,
© 2023–2026 The ggml authors）を静的リンクしている。モデルの重みは同梱せず、各モデルは
それぞれのライセンスに従う（`models list` で表示）。
