# ADR-0003: MCP server as an `image-forge mcp` subcommand (file-mediated, async jobs)

- Status: Accepted
- Date: 2026-07-07

## Context

image-forge is easy to drive from the CLI. To let an AI use it as an
image-generation tool, we add a Model Context Protocol (MCP) server. Two design
questions drove this ADR: **where the server lives**, and **how images are
handed back**.

The util-series already has MCP servers — `voice-studio-mcp`, `video-studio-mcp`,
`data-toolbox-mcp` — each a separate project that hand-rolls JSON-RPC 2.0 over
stdio (no external MCP library) and drives an *external* engine as a child
process (AivisSpeech, ffmpeg, a Podman container). They are **file-mediated**:
the agent works in a workspace directory, tools return file *paths* (never media
bytes), and long operations run as **async jobs** (`master` → `job_id`,
`check_job` polls).

image-forge differs in one decisive way: its diffusion engine
(stable-diffusion.cpp + ggml + Metal) is **statically linked into the binary**
(ADR-0001), not an external process.

## Decision

**Ship the MCP server as an `image-forge mcp` subcommand of the existing binary,
and copy the studios' file-mediated + async-job contract for how work and files
are exchanged.**

Placement — **subcommand, not a separate project**:

- The engine is already in the binary. A separate `image-forge-mcp` would have to
  either re-link the heavy CGO/Metal engine (a second slow, darwin/arm64-only
  build to sign and notarize) or drive `image-forge serve` as a child process
  (an extra process boundary for no benefit). In-binary, the MCP worker reuses
  the **resident engine session** directly — the same load-once / reload-on-model-
  change machinery as `serve` — and shares the registry, config, and profiles.
- Matches the util-series single-binary-subcommand convention. The studios are
  separate only because their engines are external; that reason does not apply
  here.

File handoff — **copy the studios**:

- **Workspace model.** One workspace = one working directory (a default root
  under the data dir, or an agent-prepared `workspace_root` per call). The server
  writes only under `<ws>/output/`. Input images (img2img/inpaint) are
  agent-placed and referenced by workspace-relative paths, resolved through
  `os.Root` so planted symlinks cannot escape the workspace.
- **Paths, not bytes.** `generate` writes a PNG under `output/` and returns its
  path; image bytes are never sent over the protocol.
- **Async jobs.** `generate` enqueues and returns a `job_id`; a single worker
  processes jobs FIFO (the Metal engine is not concurrent-safe) against the
  resident session; `check_job` polls for state, progress, and the output
  path(s). This side-steps client tool-call timeouts on a 1–2 minute render.
- **Structured tool errors** `{code, message, details}` and a **`get_usage`**
  tool that documents the workspace model, the `generate` schema, the job
  lifecycle, and a recovery table.

Tools (v1): `get_usage`, `generate`, `check_job`, `list_models`. Model
management (`pull`/`quantize`) stays CLI-only — multi-GB downloads and NSFW
opt-in are a poor fit for an AI tool call.

## Consequences

- One binary, one signing/notarization flow; no second heavy engine build.
- The AI generates into a workspace and gets a file path it (or the user) can
  use; the resident session keeps the model warm across jobs.
- Transport is stdio only for now; Streamable HTTP is a possible follow-up.
- The JSON-RPC / MCP protocol layer, workspace containment, and job manager are
  ported from `video-studio-mcp` (proven), keeping image-forge's zero-extra-dep
  posture (stdlib + BurntSushi/toml).
