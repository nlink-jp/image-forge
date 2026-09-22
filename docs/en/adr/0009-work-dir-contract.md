# ADR-0009: Take the work dir as a per-call `work_dir`, and drop the default root and the launch flag

- Status: Accepted — its implementation (the checks of the work directory) is replaced by
  ADR-0010 (nlink-jp/pathguard)
- Date: 2026-09-13

## Context

Organization ADR-021 (the work dir contract for file-mediated MCP servers) is
applied to this server. The reference implementation is voice-scribe (ADR-0010),
and the shape of absolute-path inputs and the blacklist was settled by
pcap-analyzer-mcp (ADR-0008). This is the receiving side.

This server's output destination had a four-step fallback — the call's
`workspace_root`, the launch flag `--workspace-root`, the config's
`[mcp] workspace_root`, and finally
`~/.local/share/image-forge/mcp-workspaces`. The last three are all
**places the operator wrote**, and whether the caller can read them is nothing
but coincidence. The failure takes the form of a generation that succeeds while
only the returned path cannot be opened.

Measured across the four runtimes (Claude Code / ChatGPT Codex / gem-agent /
lagent), neither MCP's `roots` nor the environment reaches half of them.
**Only the per-call argument is a channel all of them share.**

## Decision

1. **The argument is `work_dir`, required by `generate` and `upscale`.** It means
   "an absolute path the caller can read back". The workspace is
   `<work_dir>/<workspace_id>/`.
2. **The resolution order is the argument → `_meta["jp.nlink/work_dir"]` → error.**
3. **The default root, the launch flag, and the config key are all deleted.**
   `--workspace-root` is a device for embedding a runtime-specific value in the
   launch line, and in a shared registration file an undefined variable expands
   silently to the empty string. `[mcp] workspace_root` goes for the same reason.
4. **Validation is a closed list** (absolute / no `~` / no `..` / an existing dir /
   writable / not a system or credential location). The dir is not created.
   **The model store (`store.Home()`) is denied** — this keeps generated images
   from landing next to the models.
5. Input images (`init` / `mask` / `control`) stay workspace-relative as before.
   Kernel containment via `os.Root` is unchanged.
6. Enforcement is a test: that no retired spelling is in the schema, that
   `work_dir` is required once declared, and that the `_meta` path works.

## Consequences

- **Breaking.** A call sending `workspace_root` is refused with the new name
  named. A config carrying `[mcp] workspace_root` stops taking effect: the key is
  ignored. A registration line carrying `--workspace-root` stops the server from
  starting — the flag is gone, so `image-forge mcp` exits with "flag provided but
  not defined: -workspace-root"; remove it from the line
- `internal/mcp/workdir` is the same file as in voice-scribe / pcap-analyzer / gem-scribe
- With the default root gone, `internal/mcp/workspace` only "creates under the caller's dir"

## References

- Organization ADR-021, voice-scribe ADR-0010 (the reference implementation), pcap-analyzer-mcp ADR-0008
- ADR-0003 (the MCP server subcommand) — the record that introduced the default
  root and the per-call `workspace_root`. This ADR withdraws both, together with
  the `--workspace-root` flag and the `[mcp] workspace_root` key added after it
