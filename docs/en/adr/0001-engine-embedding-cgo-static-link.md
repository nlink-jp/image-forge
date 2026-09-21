# ADR-0001: Embed stable-diffusion.cpp via CGO static link

- Status: Accepted
- Date: 2026-07-06

## Context

image-forge needs a local diffusion runtime. We do not reimplement diffusion; we
wrap a mature engine (stable-diffusion.cpp — ggml, Metal, GGUF). The open question
was **how the engine is embedded** in the shipped artifact, given nlink-jp's
single-binary + Developer ID signing + notarization conventions.

Two options were weighed:

1. **CGO static link** — link ggml / stable-diffusion.cpp (including the Metal
   backend) statically into the Go binary. One artifact, one signature.
2. **Bundled subprocess** — ship a separate `sd` binary alongside and drive it via
   `exec`. Simpler build, but two binaries to sign/notarize and a zip-bundle layout.

## Decision

**Adopt CGO static link.** A true single binary matches util-series conventions and
keeps the existing single-artifact signing/notarization flow intact.

To contain the main risk (Metal shader embedding + ggml static linking), the Phase 1
**build bring-up spike** is sequenced first and de-risked in two steps:

- **1a** CPU-only static link — proves the Go ↔ C ↔ ggml plumbing and single-binary
  output (needs `cmake` only).
- **1b** Add the Metal backend — needs the Xcode Metal Toolchain.

The engine is compiled in only under the `cgo_sdcpp` build tag; default builds use a
stub returning `ErrNoRuntime`, so scaffold work and toolchain-less CI stay green.

## Consequences

- Positive: one signed/notarized binary; no runtime process management; aligns with
  util-series release tooling.
- Negative: the build requires `cmake` + the Metal Toolchain; the CGO/Metal link is
  the project's highest-risk task and is tackled first.
- Revisit if the Metal static link proves intractable — fall back to option 2
  (bundled subprocess) with a two-artifact signing recipe.
