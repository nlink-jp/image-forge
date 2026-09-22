# ADR-0010: パスの判定は nlink-jp/pathguard に任せる — 写しを持たない

- Status: Accepted
- Date: 2026-09-22

## Context

ADR-0009 以来、`work_dir` の検証（資格情報の位置の検査を含む）は `internal/mcp/workdir` にあった。voice-scribe（組織 ADR-021 の参照実装）からの写しで、同じ写しが
ほかの 7 サーバーにもあった。どの写しも場所を**名前で**比べていた。APFS は既定で大文字小文字を区別しないので、
`~/.SSH`、`.ENV`、`/USR/local` が同じ場所を指しながら検査を通った。ホームディレクトリが分からない
ときは資格情報の位置の検査が "" を返し、すべてを通した。

組織はこの判定を 1 つのモジュールにまとめた（`nlink-jp/pathguard`、lib-series）。場所を
ファイルの実体と、ディスクと同じやり方で同一視した名前の両方で比べ、まだ存在しない場所も
その親の実体で捕まえる。一覧は gem-agent・lagent と同じものを 1 つ持つ。

## Decision

- `github.com/nlink-jp/pathguard` v0.1.0 を依存に加える。この org の外のコードは入らない。
- `internal/mcp/workdir` は**薄いアダプタ**にする。持つのは次だけ:
  - リクエストの `_meta` を文脈から取り出して `pathguard/workdir` の `Resolve` に渡すこと、
  - その `*workdir.Error` を `toolerr` の同じ code・message・details に移すこと、
  - `NewResolver(serverDirs...)` —— このサーバー自身のディレクトリを守る場所（`pathguard.ServerDir`）
    として渡し、`work_dir_required` の 1 文を `RequiredHint` で添える。渡すのはデータディレクトリ
    （`store.Home()`）と、**モデルのディレクトリ（`store.ModelsDir()`）**。後者は `models_dir` で
    データディレクトリの外へ移せるのに、これまで守られていなかった。空や相対のパスは何も守らない
    のではなく、すべての呼び出しを拒ませる、
  - `Sensitive` —— `pathguard/workdir.Sensitive`（Local の方針）をそのまま出す。
- 呼び出し箇所（`Resolve`・`Validate`）は変えない。変わるのは組み立ての 1 行（`internal/cli/mcp.go`）と、ゼロ値で組み立てていたテストだけである。
- **MCP から渡された生のパスを判定する。** `loras` と `control_net` は登録名か生のパス（ADR-0006）、
  `hires_model` は導入済みの upscaler かファイルで、生のパスはどこにあってもエンジンが読む。これまでは
  まったく検査していなかったので、資格情報のファイルを LoRA として読ませることができた。レンダーの前に、
  それぞれの値をそれが指すはずのパスとして `Sensitive` で判定し、当たれば `path_not_allowed` を返す。
  登録名をパスとして判定しても何も拒まない（導入済みのモデルはモデルのディレクトリにある）。CLI と GUI の `serve` は人が自分で選ぶ経路なので
  判定しない。
- 判定そのもののテストは pathguard にある。ここに残すのはアダプタのテスト（`_meta` の取り出し、
  エラーの写し、守る場所、ゼロ値が拒むこと）と、既存の契約テストである。

## Consequences

MCP サーバーの挙動が変わる（CHANGELOG に書く）:

- **新たに拒む**: 資格情報・エージェント制御の位置にある、生のパスで渡された LoRA・ControlNet・hires の
  モデル（`path_not_allowed`）。`work_dir` として、`models_dir` で移したモデルのディレクトリ。ランタイムと同じ一覧のうち、自分のホームにある本物の場所
  （`~/.kube`、`~/.config/gh`、`~/.azure`、`~/.terraform.d`、`~/.gemini`、`~/.config/mcp-bridge`、
  `~/.netrc`、`~/.npmrc`、`~/.pypirc`、`~/.git-credentials`、`~/.vault-token`、`~/.docker/config.json`、
  `~/.claude.json`、`~/.bash_history`、`~/.zsh_history`）。床のどの場所についても、大文字小文字の違い・
  リンク・ファームリンクなど、あらゆる綴り。それらのディレクトリの直下にあるリンクの指す先（同期フォルダへの
  リンクになった `~/.ssh/config` なら、その指す先のファイル）。`$HOME` がアカウントのホームと違うときは、
  両方を守る。
- 相対パスの `XDG_DATA_HOME` は、XDG の仕様どおり無視する（データディレクトリが作業ディレクトリの下に
  できていた。今は、このサーバー自身のディレクトリが絶対パスでないと、すべての呼び出しが拒まれる）。
- **ホームが分からなければ、どの `work_dir` も拒む**。以前は通していた。
- `work_dir_denied` の `details` に `reason` が加わる。
- 1 回の検査は約 2 ms（pathguard の実測）。生成の時間に比べて無視できる。

写しを持たないので、判定の修正は pathguard のリリースと、ここでの依存の更新 1 行になる。

## References

- 組織 ADR-021（ファイル渡し MCP サーバーの work dir 契約）
- ADR-0009（work dir 契約）: 検証の閉じた一覧 —— その実装をここで置き換える
- nlink-jp/pathguard の RFP（`docs/ja/pathguard-rfp.ja.md`）
