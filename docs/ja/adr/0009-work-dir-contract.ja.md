# ADR-0009: work dir は呼び出しごとの `work_dir` で受け取り、既定ルートと起動フラグを捨てる

- Status: Accepted
- Date: 2026-09-13

## Context

組織 ADR-021（ファイル渡し MCP サーバーの work dir 契約）をこのサーバーに適用する。
参照実装は voice-scribe（ADR-0010）、絶対パス入力とブラックリストの形は
pcap-analyzer-mcp（ADR-0008）で確定済み。ここは受け取る側である。

このサーバーの出力先は 3 段のフォールバックを持っていた —— 呼び出しの
`workspace_root`、起動フラグ `--workspace-root`、config の `[mcp] workspace_root`、
そして最後に `~/.local/share/image-forge/mcp-workspaces`。後ろ 3 つはいずれも
**運用者が書いた場所**であり、呼び出し側がそこを読めるかは偶然でしかない。生成は
成功し、返ったパスだけが開けない、という形で失敗する。

4 ランタイム（Claude Code / ChatGPT Codex / gem-agent / lagent）の実測では、MCP の
`roots` も環境変数も半数には届かない。**呼び出しごとの引数だけが共通の経路**である。

## Decision

1. **引数は `work_dir`、`generate` と `upscale` で必須。** 意味は「呼び出し側が
   読み戻せる絶対パス」。ワークスペースは `<work_dir>/<workspace_id>/`。
2. **解決順は 引数 → `_meta["jp.nlink/work_dir"]` → エラー。**
3. **既定ルート・起動フラグ・config キーを全て削除する。** `--workspace-root` は
   ランタイム固有の値を起動行に埋める仕掛けであり、共有した登録ファイルでは
   未定義変数が無言で空文字に展開される。`[mcp] workspace_root` も同じ理由で消す。
4. **検証は閉じた一覧**（絶対 / `~` 無し / `..` 無し / 存在する dir / 書込可 /
   システム・資格情報の位置でない）。dir は作らない。**モデルストア
   （`store.Home()`）は拒否する** —— 生成画像がモデルの隣に落ちるのを防ぐ。
5. 入力画像（`init` / `mask` / `control`）は従来どおりワークスペース相対。
   `os.Root` によるカーネル封じ込めは変えない。
6. 強制はテスト: 旧綴りがスキーマに無いこと、`work_dir` を宣言したら必須であること、
   `_meta` 経路が通ること。

## Consequences

- **破壊的。** `workspace_root` を送る呼び出しは新しい名前を告げて拒否される。
  `--workspace-root` を書いた登録行、`[mcp] workspace_root` を書いた config は
  効かなくなる（フラグは未知オプションとして落ちる）
- `internal/mcp/workdir` は voice-scribe / pcap-analyzer / gem-scribe と同一ファイル
- 既定ルートが消え、`internal/mcp/workspace` は「呼び出し側 dir の下に作る」だけになる

## References

- 組織 ADR-021、voice-scribe ADR-0010（参照実装）、pcap-analyzer-mcp ADR-0008
- ADR-0003（MCP サーバーサブコマンド）— `--workspace-root` を導入した記録。本 ADR が
  そのフラグを撤回する
