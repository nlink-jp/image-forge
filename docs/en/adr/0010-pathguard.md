# ADR-0010: Leave path judgement to nlink-jp/pathguard — keep no copy

- Status: Accepted
- Date: 2026-09-22

## Context

Since ADR-0009, `work_dir` validation (the credential-location check included) lived in
`internal/mcp/workdir`, a copy of voice-scribe's (the reference implementation of organization
ADR-021); seven other servers held the same copy. Every copy compared places **by name**. APFS is
case-insensitive by default, so `~/.SSH`, `.ENV` and `/USR/local` named the same places and passed
the checks. When the home directory could not be determined, the credential-location check returned "" and
passed everything.

The organization moved this judgement into one module (`nlink-jp/pathguard`, lib-series). It compares
places by file identity and by names folded the way the disk folds them, and it catches a place that
does not exist yet through the identity of its parent. It holds one list, the same as gem-agent's and
lagent's.

## Decision

- Depend on `github.com/nlink-jp/pathguard` v0.1.0. No code from outside this organization comes
  with it.
- `internal/mcp/workdir` becomes a **thin adapter**. It keeps only:
  - taking the request's `_meta` from the context and passing it to `pathguard/workdir`'s `Resolve`,
  - moving that `*workdir.Error` onto `toolerr` with the same code, message and details,
  - `NewResolver(serverDirs...)` — passing this server's own directories as protected places
    (`pathguard.ServerDir`), and its one sentence for `work_dir_required` as `RequiredHint`. Those are
    the data directory (`store.Home()`) and **the models directory (`store.ModelsDir()`)**, which
    `models_dir` can move outside the data directory and which was not protected until now. An empty
    or relative path refuses every call rather than protecting nothing,
  - `Sensitive` — `pathguard/workdir.Sensitive` (the Local policy), passed through.
- The call sites (`Resolve`, `Validate`) do not change. What changes is the one line that builds the resolver (`internal/cli/mcp.go`) and the tests that built it as a zero value.
- **Raw paths an MCP call names are judged.** `loras` and `control_net` are installed names or raw
  paths (ADR-0006), and `hires_model` is an installed upscaler or a file; a raw path is read by the
  engine wherever it lies. None of them was checked, so a credential file could be loaded as a LoRA.
  Before the render, every value is judged with `Sensitive` as the path it would be, and a refused
  one returns `path_not_allowed`. A registry name judged that way refuses nothing, since installed
  models live in the models directory. The CLI and the GUI's `serve` loop are a person's own choice and are
  not judged.
- The tests of the judgement itself are in pathguard. What stays here are the adapter's tests (taking
  `_meta`, carrying the error across, the protected place, a zero value refusing) and the existing
  contract tests.

## Consequences

The MCP server behaves differently (the CHANGELOG says so):

- **Refused now**: a LoRA, ControlNet or hires model given as a raw path in a credential or
  agent-control location (`path_not_allowed`); the models directory moved by `models_dir`, as a
  `work_dir`; the real places under your home from the runtimes' list (`~/.kube`,
  `~/.config/gh`, `~/.azure`, `~/.terraform.d`, `~/.gemini`, `~/.config/mcp-bridge`, `~/.netrc`,
  `~/.npmrc`, `~/.pypirc`, `~/.git-credentials`, `~/.vault-token`, `~/.docker/config.json`,
  `~/.claude.json`, `~/.bash_history`, `~/.zsh_history`); every spelling of any floor place — case
  variants, links, firmlinks; Linux `/etc` as a `work_dir`.
- **An unknown home refuses every `work_dir`.** It used to pass them.
- `work_dir_denied` carries `reason` in its `details`.
- One check costs about 2 ms (measured in pathguard) — nothing next to a render.

With no copy here, a fix to the judgement is a pathguard release and a one-line dependency update.

## References

- Organization ADR-021 (the work-dir contract of the file-mediated MCP servers)
- ADR-0009 (work-dir contract): the closed list of checks — whose implementation this replaces
- nlink-jp/pathguard's RFP (`docs/en/pathguard-rfp.md`)
