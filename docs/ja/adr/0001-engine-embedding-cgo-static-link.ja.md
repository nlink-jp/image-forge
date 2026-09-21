# ADR-0001: stable-diffusion.cpp を CGO 静的リンクで埋め込む

- Status: Accepted
- Date: 2026-07-06

## Context

image-forge にはローカルの diffusion ランタイムが必要である。diffusion を自前で再実装は
せず、成熟したエンジン（stable-diffusion.cpp — ggml・Metal・GGUF）をラップする。未決
だったのは **エンジンを出荷物へどう埋め込むか** であり、nlink-jp の単一バイナリ +
Developer ID 署名 + notarization という規約が前提にある。

検討した選択肢は 2 つ:

1. **CGO 静的リンク** — ggml / stable-diffusion.cpp（Metal バックエンド込み）を Go
   バイナリへ静的リンクする。成果物 1 つ、署名 1 つ。
2. **サブプロセス同梱** — 別の `sd` バイナリを並べて同梱し `exec` で駆動する。ビルドは
   単純だが、署名/notarize 対象が 2 バイナリになり、zip バンドル構成になる。

## Decision

**CGO 静的リンクを採用する。** 真の単一バイナリは util-series の規約に沿い、既存の
単一成果物の署名/notarization フローをそのまま保てる。

主リスク（Metal シェーダの埋め込み + ggml の静的リンク）を封じ込めるため、Phase 1 の
**ビルド立ち上げスパイク**を最初に置き、2 段に分けてリスクを下げる:

- **1a** CPU のみの静的リンク — Go ↔ C ↔ ggml の配管と単一バイナリ出力を証明する
  （必要なのは `cmake` だけ）。
- **1b** Metal バックエンドを追加 — Xcode Metal Toolchain が必要。

エンジンがコンパイルに含まれるのは `cgo_sdcpp` ビルドタグの下だけで、既定ビルドは
`ErrNoRuntime` を返すスタブを使う。これによりスキャフォールド作業と、ツールチェインの
無い CI が緑のままになる。

## Consequences

- 利点: 署名/notarize 済みバイナリが 1 つ。ランタイムのプロセス管理が不要。util-series
  のリリースツールと整合する。
- 欠点: ビルドに `cmake` + Metal Toolchain が要る。CGO/Metal のリンクは本プロジェクト
  最大のリスク作業であり、最初に取り組む。
- Metal の静的リンクがどうしても通らないと分かった場合は再考し、選択肢 2（サブプロセス
  同梱）へ、2 成果物の署名レシピとともに戻る。
