# ADR-0005: 生成メタデータを PNG テキストチャンクに埋め込む

- Status: Accepted
- Date: 2026-07-07

## Context

利用者はプロンプト・パラメータ・モデルが生成画像の*中*に記録され、画像自体が自己記述的で
あることを望む — AUTOMATIC1111 / ComfyUI / NovelAI がやっているのと同じことであり、
Civitai が「generation data」を表示するために読むものでもある。

image-forge は **PNG** を出力する。PNG における AI 画像の慣行は **テキストチャンク**
（`tEXt` / `iTXt`）であって **EXIF ではない** — EXIF は JPEG/TIFF の構造である。つまり
「EXIF に入れる」は、PNG では「テキストチャンクに入れる」になる。Go の `image/png`
エンコーダはテキストチャンクを公開しておらず、image-forge は追加依存ゼロの姿勢
（BurntSushi/toml のみ）を保つので、チャンク挿入は自前で書く。

## Decision

**PNG をエンコードした後に、生成メタデータを載せたテキストチャンクを差し込む。キーワードは
2 つ:**

1. **`parameters`** — **AUTOMATIC1111 互換**の文字列。相互運用のため（Civitai と A1111 は
   これを直接パースする）:
   ```
   <prompt>
   Negative prompt: <negative>
   Steps: 26, Sampler: euler_a, CFG scale: 7, Seed: 20240707, Size: 1024x1024,
   Model: prefect-pony-xl, Clip skip: 2[, Denoising strength: .., Hires upscale: ..,
   Hires upscaler: .., Version: image-forge vX.Y.Z]
   ```
2. **`image-forge`** — **完全な JSON** レコード（image-forge 独自、無損失）:
   model / model_path / prompt / negative / seed / steps / cfg / width / height /
   sampler / scheduler / clip_skip / prediction / vae / loras / img2img / hires /
   controlnet / version。

**エンコーディング:** 文字列が Latin-1 で安全なら `tEXt`、そうでなければ
**`iTXt`（UTF-8）** — これにより日本語/Unicode のプロンプトが正しく往復する
（PIL/A1111 と同じ挙動）。チャンクは `IHDR` の直後に挿入する。

**どこで組み立てるか:** CLI 層（分かりやすいモデル名・予測方式・バイナリのバージョンを
知っている層）で組み立て、`engine.Request.Metadata`（および
`engine.UpscaleParams.Metadata`）に載せて運び、`saveImage` が書き込む。エンジンは渡された
テキストを書くだけで、文字列組み立てと相互運用のロジックは `cli` に留まり、ユニット
テストされる。純粋な PNG チャンクライタは非 cgo のファイルに置き、スタブビルドで
テストする。

**既定 ON、ただしオプトアウトあり。** プライバシーのため（埋め込まれたプロンプトは画像を
共有した相手全員に見える）: `gen --no-metadata`、および config `[metadata] embed = false`。
`serve` と MCP の `generate` ツールは config を尊重する。

**upscale (v0.12.1+):** `upscale` は元 PNG のメタデータを読み（`engine.ReadPNGText`。
チャンクライタの逆）、**そのまま引き継ぐ** — アップスケールされた画像は元のプロンプト /
seed / パラメータ（および元の `parameters` チャンク）を保ち、加えて `upscale` サブレコード
`{upscaler, factor, source}` を持つ。これによりアップスケールも自己記述的であり続け、
その来歴がギャラリーの再読み込みを越えて残る。元画像に image-forge のメタデータが無い
場合は、軽い `upscale` レコードのみを書く。

**ファイルシステムのパスは書かない (v0.13.1+)。** 当初のレコードは絶対パスを埋め込んで
いた（`model_path`、`vae_path`、`loras: ["/abs/path.safetensors:1"]`、`img2img.init`、
`controlnet.image`、`hires.model`）。これは誤りだった。生成画像は*共有されるために作られる*
ものであり、絶対パスはそのマシンのレイアウトを — そしてホームディレクトリ経由で
**利用者の名前**（`/Users/alice/…`）を — 画像が届く相手全員に、Civitai を含めて漏らす。
しかも再現には何の役にも立たない: そのパスは別のマシンでは無意味である。

そこで、モデルの参照はすべて**識別子**として記録するようにした: インストール済みなら
レジストリ名、そうでなければファイルのベース名。これは再現に対して「劣る」のではなく
*より役に立つ*。`-m` / `--lora` / `--control-net` はインストール済みの名前を解決するので
（ADR-0006）、`"loras": ["lcm-lora-sdxl:1"]` はそのまま再実行できる。**入力画像は一切
記録しない**: `img2img` は `strength` だけ、`controlnet` は `strength` / `canny` だけを
保つ。ファイル*名*それ自体が個人情報になり得る（`my-passport-scan.png`）し、A1111 自身の
`parameters` チャンクも init 画像を名指しせずに denoising strength を記録している — それに
合わせる。リグレッションテストが、どちらのチャンクにも `/Users`・`/Volumes`・ファイル
拡張子が現れないことを保証する。

これは「無損失」の意味を狭める（下の Consequences 参照）: 画像を*再現*するために必要な
ものは全て保ち、*このマシン*のことしか語らないものは意図的に捨てる。

## Consequences

- 画像は自己記述的であり、A1111/Civitai のエコシステムへ往復する。一方 JSON チャンクは
  描画を**再現**するために必要なものすべてを保つ — ただし*このマシン*のことしか語らない
  ファイルシステムのパスや入力画像は決して保たない（上の「ファイルシステムのパスは
  書かない」参照）。呼び出しのバイト単位の記録ではなく、それは意図的である。
- Unicode のプロンプトを扱える（iTXt）。素朴な tEXt のみの実装とは違う。
- 新しい依存は無い。チャンクライタは純 Go でユニットテスト済み。相互運用文字列のビルダは
  エンジンと独立にテストされる。
- オプトアウトが、共有される画像の中にプロンプトを同梱してしまうというプライバシー上の
  懸念をカバーする。
