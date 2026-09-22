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
  - `NewResolverFor(places...)` と `LocalPath` —— 守る場所を自分で名指す（設定ファイルは、そのファイルとして
    守る）ことと、読み取りの判定。
- 呼び出し箇所（`Resolve`・`Validate`）は変えない。変わるのは組み立ての 1 行（`internal/cli/mcp.go`）と、ゼロ値で組み立てていたテストだけである。
- **MCP から渡された生のパスを判定する。** `loras` と `control_net` は登録名か生のパス（ADR-0006）、
  `hires_model` は導入済みの upscaler かファイルで、生のパスはどこにあってもエンジンが読む。これまでは
  まったく検査していなかったので、資格情報のファイルを LoRA として読ませることができた。レンダーの前に、
  そして何かがそれを stat する前に（stat の結果の違いで、ファイルの有無が漏れる）、登録簿が知らない値を
  読み取り用の検査器で判定し、当たれば `path_not_allowed` を返す。読み取り用の検査器は床に設定ディレクトリ
  （`hf_token` を置きうる）を加えたもので、モデルのディレクトリは加えない（LoRA はそこから読む）。導入済みの
  名前は登録簿が自分のファイルへ解決し、それが開かれるので、パスとしては判定しない —— 名前をパスとして
  判定すると、サーバーの作業ディレクトリを判定することになる（`~/.claude` で起動したランタイムでは、
  導入済みの LoRA がすべて拒まれた）。CLI と GUI の `serve` は人が自分で選ぶ経路なので
  判定しない。
- 判定そのもののテストは pathguard にある。ここに残すのはアダプタのテスト（`_meta` の取り出し、
  エラーの写し、守る場所、ゼロ値が拒むこと）と、既存の契約テストである。

- 設定ファイル（`config.Path()`、旧来のデータディレクトリの `config.toml`）はファイルとして守り、そのディレクトリは
  image-forge 自身のもの（既定または `$XDG_CONFIG_HOME` の形）のときだけ守る。`IMAGE_FORGE_CONFIG=~/image-forge.toml`
  でホーム全体がサーバーのディレクトリにならないように。相対パスの `XDG_CONFIG_HOME` は無視する。
- ワークスペースの入力（`init`・`mask`・`control`・`input`）は、解決した先を work dir の検査器の `LocalPath` で判定する。
  ワークスペースは `CheckBeneath` を通っても、サーバーのディレクトリを含みうる（`work_dir=~/.local`、`workspace_id=share`）。
- 配線は `mcpGuards` 1 か所にまとめ、テストはそれを使う: work dir の検査器（データ・モデル・設定の
  ディレクトリを守る）、それで各ワークスペースを判定するワークスペースの管理者、読み取り用の検査器。

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

## Amendment (2026-09-22): 実際に使うディレクトリも判定する

独立レビューで、`work_dir` だけを検査していたために `work_dir=~/.config` と `workspace_id=gh` でワークスペースが
`~/.config/gh` になることが分かった。ADR-0009 の写しの頃からの穴である。`workspace.NewManager(check)` は判定を
必須の引数として受け取り、`EnsureUnder` は `<work_dir>/<workspace_id>` を作る前・使う前に
`workdir.Resolver.CheckBeneath`（pathguard v0.2.0）で判定する。判定の無い Manager はすべてのワークスペースを
拒む。pathguard v0.2.0 は NUL バイトを含むパスも拒む。

## Amendment (2026-09-22, v0.29.1): ファイルの有無で答えを変えない

ワークスペース内の入力画像（`init`・`mask`・`control`・upscale の `input`）は、`VerifyRegular` で存在を確かめて
から床に掛けていた。ワークスペースは床の場所を含み得る（`work_dir=~/.local` と `workspace_id=share` はこのサーバーの
データディレクトリを含む。`.env`、`~/.ssh` 内のリンクが同期フォルダを指すならその行き先）ので、それらは、あれば
`path_not_allowed`、無ければ `input_not_found` になり、答えがどれが存在するかを教えていた。slack-mcp-extender と
chrome-pilot-mcp の独立レビューで見つかった型で、ここでは HOME を一時ディレクトリにしたテストで実測した（10 組中
6 組で答えが違った。仕掛けたリンクで外へ出る 4 組は `os.Root` が存在に関係なく拒んでいた）。

- `resolveInput` は、読むファイル（`ws.Path(rel)`）を床に掛けてから `VerifyRegular` する。pathguard がパス上の
  リンクを自分で辿るので、置き場所を別に求める必要は無い。
- 生のパスで渡す LoRA・ControlNet・hires のモデルは、以前から何かが開く前に判定していた（`mcpReadRefused`）。
  15 組で答えが同じであることを `TestModelPathExistenceIsNotRevealed` が、レンダラーの答えでも同じであることを
  `TestTheRendererAnswersModelPathsAlike` が固定する（hires のモデルは解決時に stat されるので順序が効く）。
- `TestExistenceIsNotRevealed`（internal/mcp/tools）は、同じパスをファイルがある状態と消した状態で `generate` と
  `upscale` を呼び、答え全体を比べる。4 つの変異（入力の判定の順序を戻す・入力の床を外す — ここで落ちる。モデルの床を
  外す・hires のモデルを判定の前に解決する — internal/cli で落ちる）はすべてアサーションで落ちた。
- pathguard 側の既知の限界（次のリリースに向けて記録）:
  - 資格情報ディレクトリの項目を通って `..` で抜けるパス（パス自体でも、仕掛けたリンクの行き先でも）は、通った場所
    ではなく行き着く場所で判定されるので、その項目がリンクか・行き先がどこかが答えに出うる。pathguard が判定するのは
    Clean した形で、歩いた途中のディレクトリではない。
  - `work_dir` は pathguard/workdir が組織 ADR-022 §4 の順序（not found が denied より先）で検証するので、資格情報の
    ディレクトリを指す `work_dir` は、存在するかどうかで答えが変わる。
  - 非 ASCII 名のリンク先を別の Unicode 正規化で綴ると、同一性で拒むのはそれが存在するときだけになる（pathguard は
    正規化しない）。別の場所に作ったハードリンクを拒むのは、床の場所そのものであるファイル（`~/.netrc`、
    `~/.docker/config.json` など）へのものだけで、それも存在するときだけ。資格情報ディレクトリの中のファイル
    （`~/.ssh/id_rsa`）や `.env` へのハードリンクは拒まない —— ディレクトリはそれ自身の同一性で比べ、中のファイルでは比べない。
- 判定と読み取りは 2 段で、その間にすり替えたリンクは辿られる（check-to-use の競合。ここでは閉じていない。閉じるには、
  開いたものを記述子から判定する必要がある）。

## References

- 組織 ADR-021（ファイル渡し MCP サーバーの work dir 契約）
- ADR-0009（work dir 契約）: 検証の閉じた一覧 —— その実装をここで置き換える
- nlink-jp/pathguard の RFP（`docs/ja/pathguard-rfp.ja.md`）
