# RFP: image-forge

> Generated: 2026-07-06
> Status: Draft

## 1. Problem Statement

`image-forge` は、SDXL をはじめとする最新のローカル拡散モデル（Animagine XL /
Illustrious / Pony 系などのアニメ系、および FLUX / SD3.5 / Z-Image などの汎用高品質
モデル）を、**macOS（Apple Silicon）上で専門知識なしにワンストップで動かす** 画像生成
エンジン兼モデル運用 CLI である。

モデルごとに存在する「動かすための正解設定」— CLIP-skip、専用 VAE（SDXL fp16 の黒画像
NaN 対策）、native 解像度、推奨 sampler/steps、予測方式（eps / v-pred）、量子化レベル —
はユーザに一切要求せず、**モデルプロファイル**として隠蔽して自動適用する。対象ユーザは
「ローカルで良い画像を出したいが、モデルの内部設定を触りたくない」層。DiffusionBee の
「no dependencies / no technical knowledge needed」思想を、CLI とモデル運用ツールの形で
実現する。

## 2. Functional Specification

### Commands / API Surface

単一バイナリ + サブコマンド同居（util-series 流儀）。

- `image-forge gen` — txt2img / img2img 生成。
  - 主要フラグ: `-p/--prompt`, `-n/--negative`, `--seed`, `--steps`, `--cfg`,
    `-W/-H`（または `--size`）, `--sampler`, `--clip-skip`, `--vae`,
    `--batch/--num`, `-m/--model <profile>`, `-o/--output`。
  - LoRA: `--lora <name>:<weight>`（複数指定可）。
  - img2img: `--init <image>` `--strength <float>`。
  - プロファイルから clip-skip / VAE / 解像度 / sampler / cfg / negative 有無を
    **自動適用**し、明示フラグで上書き可能。
- `image-forge models` — モデル運用。
  - `list` — インストール済み + カタログ表示（列: name / arch / content_rating /
    license / RAM 階層 / installed）。
  - `pull <name | hf:repo | civitai:id | url>` — チェックポイント（+ 必要 VAE）を
    ダウンロード → RAM 自動検出 → 必要なら量子化 → プロファイル登録まで一括。
    フラグ: `--quant q8_0|q4_k|none`, `--allow-nsfw`。
  - `import <path.safetensors>` — ローカルファイルを登録。アーキ自動判定し
    プロファイル生成。
  - `quantize <name> --to <type>` — GGUF 量子化（sd.cpp 内蔵コンバータを利用）。
  - `rm <name>` — 登録解除。
- `image-forge serve` — **Phase 2**。モデル常駐 + JSON 行 API デーモン。

### Input / Output

- 出力: PNG（生成パラメータをメタデータに埋め込み）。
- 進捗: stderr への **JSON 行ストリーム**（DiffusionBee の `sdbk` テキスト
  プロトコルを近代化。1 行 1 イベント: `load` / `progress` / `done` / `error`）。
- 入力: プロンプトはフラグまたは `--input` ファイル。img2img/将来 inpaint は
  画像/マスクをファイルパスで受ける。

### Configuration

- user config（TOML）: `~/.config/image-forge/config.toml`（既定モデル、量子化方針、
  `allow_nsfw`、出力先など）。
- 環境変数: `CIVITAI_TOKEN` / `HF_TOKEN`（トークンは config か env で管理し、
  **リポジトリには絶対にコミットしない**）。
- カタログ: バイナリ埋め込みの既定カタログ + ユーザ拡張可能なローカルカタログ。
  各エントリは `arch` / `prediction_type (eps|vpred)` / `content_rating
  (safe|questionable|explicit)` / `license` / `min_ram` / `recommended_ram` /
  推奨サンプラー等のプロファイル既定を持つ。

### External Dependencies

- **stable-diffusion.cpp**（ggml、CGO 静的リンクでバイナリに内包）+ Metal。
- Hugging Face / Civitai の HTTP API（モデルダウンロード。ユーザ供給トークン）。
- その他の外部サービス依存なし。モデル本体は **同梱・再配布しない**。

## 3. Design Decisions

- **拡散モデルは自作しない。** 中核は成熟した stable-diffusion.cpp に委ねる。
  DiffusionBee からは「常駐デーモン + 進捗ストリーミング + モデル管理」という
  **骨格の型のみ** を参照し、ML 実装は借りない（DiffusionBee 作者自身も自作 Keras 実装
  から Apple Core ML へ移行済み）。
- **言語 = Go + CGO 静的リンク。** util-series の単一バイナリ + サブコマンド同居 +
  Developer ID 署名 / notarize フローに乗せる。真の単一バイナリを志向。
- **マルチアーキ・プロファイル方式。** SDXL / FLUX / SD3.5 / Z-Image で sampler・
  cfg・negative prompt の要否・解像度既定が異なるため、`arch` 別に既定を持ち、
  モデル固有の落とし穴をユーザから隠蔽する。
- **既存ツールとの関係。** `gem-image`（クラウド Gemini 画像生成）に対する
  **ローカル拡散エンジン** の対。明確に別物。
- **スコープ外**: 学習 / fine-tuning、モデルの再配布、動画生成（Wan 等）、
  非 Apple Silicon 最適化、GUI（将来別プロジェクトまたは Phase 3 以降）。

## 4. Development Plan

### Phase 1: Core

1. **ビルド疎通スパイク（最優先・独立レビュー可）** — CGO で ggml / stable-diffusion.cpp
   を Metal バックエンドごと静的リンクし、単一 Go バイナリを生成。`make build` →
   `dist/`、Developer ID 署名 + notarize までの疎通を確認。Metal shader 埋め込みと
   ggml 静的リンクが本プロジェクト最大の技術リスクのため先頭に置く。
2. **txt2img コア + マルチアーキ・プロファイル系 + テスト**（純関数: プロファイル解決、
   RAM→量子化判定、sd.cpp 引数構築、カタログ解析。sd.cpp 呼び出し層は注入可能に）。
3. **models ツール** — import / pull / quantize / list / rm。カタログ（content_rating /
   license / RAM 階層）、NSFW オプトイン、トークン管理。
4. **LoRA 適用**（複数 LoRA + weight）。
5. **img2img**（init image + denoise strength）。
6. **互換検証** — sd.cpp の v-prediction / ZSNR 対応状況を検証し、NoobAI / Illustrious
   v2 系（v-pred）のカタログ扱い（experimental → 昇格可否）を決定。

### Phase 2: Features

- `serve` 常駐モード（JSON 行 API、モデル一度ロードで連投）。
- inpaint（マスク入力）。
- ControlNet。
- プロンプト重み付け、追加スケジューラ / サンプラーの露出。

### Phase 3: Release

- README.md / README.ja.md / CHANGELOG.md / AGENTS.md 整備。
- `make build-all`（darwin arm64 主対象）、署名 + notarize 済み zip（canonical binary 名）、
  実モデルでの E2E、`gh release`、umbrella submodule ポインタ更新、org profile 更新、
  `check-org.sh` 緑化。

**独立レビュー可能な単位**: ①ビルド疎通スパイク、②txt2img + プロファイル、
③models ツール、④LoRA、⑤img2img はそれぞれ独立してレビュー可能。

## 5. Required API Scopes / Permissions

OAuth スコープ・IAM ロールは不要。

- **Hugging Face**: gated リポジトリ取得時にユーザ供給の HF トークン（任意）。
- **Civitai**: ダウンロード / NSFW コンテンツ取得にユーザ供給の API トークン。
- いずれも user config / 環境変数で管理し、リポジトリにはコミットしない。
- GCP / Vertex 等のクラウド権限は一切不要（完全ローカル動作）。

## 6. Series Placement

Series: **util-series**

Reason: Go 単一バイナリ + サブコマンド同居のパイプフレンドリーなローカルファースト
ユーティリティであり、util-series の規約（`make build` → `dist/`、Developer ID 署名 +
notarize、macOS リリース）にそのまま乗る。クラウドサービスクライアント（cli-series）でも
LLM 対話（lite-series）でもなく、ローカルのデータ変換 / 処理系ツールとして util-series が
最適。

## 7. External Platform Constraints

- **動作環境**: Apple Silicon + Metal（arm64 macOS）。**ベースライン（最低）16GB /
  推奨 32GB 以上**。CPU フォールバックは sd.cpp で可能だが低速のため主対象外。
- **メモリ階層**:
  - 16GB: SDXL 系（~6.5GB fp16）/ Z-Image Turbo は快適。FLUX / SD3.5 Large / Qwen-Image は
    Q4 量子化前提。
  - 32GB+: FLUX.1-dev / SD3.5 Large / Qwen-Image 等の大型も量子化併用で快適。
  - `models pull` は実 RAM を検出し、16 / 32GB を閾値に量子化レベルを提案・実行。
- **モデルライセンス**: ユーザ責任。ツールはモデルを同梱・再配布しない。カタログは
  `license`（商用 / 出力権）と `content_rating` を可視化し、NSFW はオプトイン、
  最終判断はユーザに委ねる。
- **Civitai API**: レート制限、多くのダウンロードでトークン必須、ToS（ダウンロードは
  ユーザ起点）。**HF**: gated リポはトークン必須、LFS 大容量（レジューム / チェックサム）。
- **v-prediction / ZSNR**: sd.cpp の対応は開発中・限定的。eps 系モデルは確実に動作するが、
  v-pred 系は当面 experimental 扱い。

---

## Discussion Log

- **参照方針**: DiffusionBee の Text2Img は「常駐 + 進捗 + モデル管理」の骨格として参照し、
  ML 実装は借りない。DiffusionBee 自身が自作 Keras 実装 → Apple Core ML へ移行しており、
  「エンジン中核は既存ランタイムに委ねる」判断を踏襲。
- **エンジン中核**: stable-diffusion.cpp（ggml / Metal / GGUF）を採用。MLX・diffusers・
  Core ML と比較し、単一バイナリ / 依存最小 / notarize フローとの整合で選定。
- **内包方式**: CGO 静的リンク vs 子プロセス同梱を検討 → **CGO 静的リンク採用**（真の
  単一バイナリ・署名 1 つを優先）。Metal shader 埋め込み / ggml 静的リンクが Phase 1 の
  最大リスクのため、ビルド疎通スパイクを開発計画の先頭に配置。
- **対象モデル**: 当初 Animagine XL / Pony 系 tPonynai3 → SDXL 必須 → CLIP-skip 2 /
  SDXL fp16-fix VAE / 1024 解像度 / euler_a・25 steps / score タグが「動かすための落とし穴」
  と判明。プロファイルで自動適用し隠蔽する方針を確立。
- **取得元 / カタログ**: HF / Civitai / 直 URL に対応。カタログに content_rating フラグを
  持たせ、NSFW はオプトイン、判断はユーザに委ねる方針で合意。
- **HF 高品質モデル調査**: アニメ系（Animagine XL 4.0 / Illustrious XL / NoobAI vpred）+
  汎用（FLUX.1-schnell〔Apache 2.0〕/ FLUX.1-dev〔非商用〕/ SD3.5 / Qwen-Image〔Apache 2.0〕/
  Z-Image Turbo）を整理。v-prediction 系は sd.cpp 対応が発展途上のため experimental 扱いに。
  カタログスキーマに `license` と `prediction_type` を追加。
- **ハードウェア要件**: 16GB ベースライン / 32GB+ 推奨を設計要件として確定。
- **スコープ**: アニメ限定にせず汎用高品質モデルも横断。Phase 1 追加機能は LoRA + img2img
  （inpaint / serve は Phase 2、ControlNet / GUI は Phase 2/3）。
- **命名**: `image-forge`（gem-image〔クラウド Gemini〕と明確に差別化したローカルエンジン）。
