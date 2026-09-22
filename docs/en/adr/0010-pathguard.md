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
  - `NewResolverFor(places...)` and `LocalPath` — naming its own places (a config file is protected as
    the file) and judging reads.
- The call sites (`Resolve`, `Validate`) do not change. What changes is the one line that builds the resolver (`internal/cli/mcp.go`) and the tests that built it as a zero value.
- **Raw paths an MCP call names are judged.** `loras` and `control_net` are installed names or raw
  paths (ADR-0006), and `hires_model` is an installed upscaler or a file; a raw path is read by the
  engine wherever it lies. None of them was checked, so a credential file could be loaded as a LoRA.
  Before the render — and before anything stats it, since a different stat result would say whether
  the file exists — every value the registry does not know is judged by the read guard, and a refused
  one returns `path_not_allowed`. The read guard is the floor plus the config directory (which may
  hold `hf_token`), not the models directory (LoRAs are read from it). An installed name is resolved
  by the registry to its own file, which is what is opened, so it is not judged as a path — judging a
  name as a path judges the server's working directory (a runtime started in `~/.claude` found every
  installed LoRA refused). The CLI and the GUI's `serve` loop are a person's own choice and are
  not judged.
- The tests of the judgement itself are in pathguard. What stays here are the adapter's tests (taking
  `_meta`, carrying the error across, the protected place, a zero value refusing) and the existing
  contract tests.

- The config files (`config.Path()`, and the legacy `config.toml` in the data directory) are protected
  as files; their directory only when it is image-forge's own (the default or `$XDG_CONFIG_HOME`
  form), so `IMAGE_FORGE_CONFIG=~/image-forge.toml` does not make the home directory a server
  directory. A relative `XDG_CONFIG_HOME` is ignored.
- Workspace inputs (`init`, `mask`, `control`, `input`) are judged, as the file they resolve to, with
  the work-directory resolver's `LocalPath`: a workspace that passed `CheckBeneath` may still contain a
  server directory (`work_dir=~/.local`, `workspace_id=share`).
- The wiring is one function, `mcpGuards`, which the tests use: the work-directory resolver (protecting
  the data, models and config directories), the workspace manager that judges every workspace with it,
  and the read guard.

## Consequences

The MCP server behaves differently (the CHANGELOG says so):

- **Refused now**: a LoRA, ControlNet or hires model given as a raw path in a credential or
  agent-control location (`path_not_allowed`); the models directory moved by `models_dir`, as a
  `work_dir`; the real places under your home from the runtimes' list (`~/.kube`,
  `~/.config/gh`, `~/.azure`, `~/.terraform.d`, `~/.gemini`, `~/.config/mcp-bridge`, `~/.netrc`,
  `~/.npmrc`, `~/.pypirc`, `~/.git-credentials`, `~/.vault-token`, `~/.docker/config.json`,
  `~/.claude.json`, `~/.bash_history`, `~/.zsh_history`); every spelling of any floor place — case
  variants, links, firmlinks; wherever a link directly inside one of those directories points (a
  `~/.ssh/config` that links into a sync folder protects the file it points at); when `$HOME` names
  another directory than the account's home, both.
- A relative `XDG_DATA_HOME` is ignored, as the XDG spec says (it put the data directory under the
  working directory; now a server directory that is not absolute would refuse every call).
- **An unknown home refuses every `work_dir`.** It used to pass them.
- `work_dir_denied` carries `reason` in its `details`.
- One check costs about 2 ms (measured in pathguard) — nothing next to a render.

With no copy here, a fix to the judgement is a pathguard release and a one-line dependency update.

## Amendment (2026-09-22): judge the directory actually used

An independent review found that checking only `work_dir` let `work_dir=~/.config` with
`workspace_id=gh` make the workspace `~/.config/gh`; the hole dates from the ADR-0009 copy.
`workspace.NewManager(check)` takes the judgement as a required argument, and `EnsureUnder` judges
`<work_dir>/<workspace_id>` with `workdir.Resolver.CheckBeneath` (pathguard v0.2.0) before making or
using it. A Manager without one refuses every workspace. pathguard v0.2.0 also refuses a path holding
a NUL byte.

## References

- Organization ADR-021 (the work-dir contract of the file-mediated MCP servers)
- ADR-0009 (work-dir contract): the closed list of checks — whose implementation this replaces
- nlink-jp/pathguard's RFP (`docs/en/pathguard-rfp.md`)
