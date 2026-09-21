# ADR-0002: `models list` をインストール済み / カタログのビューに分割し、JSON 出力を付ける

- Status: Accepted
- Date: 2026-07-07

## Context

`models list` は 2 つの異なるものを 1 つの表に混ぜて描画していた — キュレーション済み
カタログ（各行を STATUS 列で `available` / `installed` と示す）と、カタログに無い
インストール済みモデルである。この混在ビューは読みにくいと利用者から指摘された —
「何を持っているか」と「何を入手できるか」は別の問いであり、カタログのメタデータ
（RAM ティア、ライセンス）とインストール状態（パス）を 1 つの表に混ぜれば双方が
ぼやける。加えて、スクリプト向けの機械可読な出力が無かった。

ビューを分ける形として 2 案を検討した:

1. **サブコマンドを分ける** — `models list`（インストール済み）+ `models catalog`。
2. **1 コマンド + フィルタフラグ** — `models list [--installed|--catalog|--all]`。

## Decision

**案 2 を採用する: `models list` は 1 コマンドのままモードフラグを持ち、加えて全モードに
`--json` フラグを付ける。**

- `models list`（既定）— **インストール済み**モデルのみ: `NAME ARCH RATING LICENSE PATH`
  （後に ADR-0008 が、ファイルの欠けたモデル向けに `STATUS` 列を加えた）。
- `models list --catalog` — キュレーション済みカタログ: `NAME ARCH RATING RAM LICENSE INSTALLED`。
- `models list --all` — 両方を、明示ラベル付きの 2 セクション（`INSTALLED`、`CATALOG`）で。
- `--json` はどのモードでも安定した目的専用の JSON を出す（installed → 配列、
  catalog → `installed` フラグ付きの配列、`--all` → `{"installed":[…],"catalog":[…]}`）。

JSON は内部型 `store.InstalledModel` / `catalog.Entry` ではなく専用の `installedView` /
`catalogView` 構造体から描画する。これにより出力契約が内部リファクタから切り離される
（たとえばレジストリの入れ子 `profile` blob が漏れない）。

これは **挙動変更**である: `models list` は既定でカタログを表示しなくなる。CHANGELOG に
明記する。image-forge は 1.0 前でリリース直後なので、この揺れは許容できる。一度に全部
見たい人向けには `--all` がそのビューを残す。

## Consequences

- 既定出力が明快になる。2 つの問いがそれぞれ自分のビューを持つ。
- `--json` でスクリプト可能になる（util-series の `--json` 規約に沿う）。
- モード解決は純粋な `resolveListMode` ヘルパに置き、ユニットテストする。ビュー構築
  （`installedViews` / `catalogViews`）は端末描画と独立にテストする。
- コマンドを 1 つに保つ（新しい `catalog` サブコマンドを作らない）ことで `models` の
  表面が小さく保たれ、既存の `pull` / `import` / `quantize` / `rm` という動詞の並びに
  合う。
