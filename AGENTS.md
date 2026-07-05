# AGENTS.md — image-forge

## What this is

A local diffusion image-generation engine + model-management CLI for macOS
(Apple Silicon). Wraps **stable-diffusion.cpp** (ggml/Metal, statically linked via
CGO) behind a Go single binary. Hides per-model gotchas (CLIP-skip, dedicated VAE,
resolution, sampler, prediction type, quantization) inside **model profiles** so
users never hand-tune them. Series: **util-series**. Local-diffusion counterpart to
`gem-image` (cloud Gemini).

Status: **early scaffold (Phase 2).** Command surface + catalog + profiles exist;
the diffusion runtime is not wired. Full design: `docs/{ja,en}/image-forge-rfp*`.

## Build & test

```sh
make build         # scaffold binary (no engine) → dist/image-forge
make build-engine  # full binary w/ sd.cpp runtime (needs cmake + Metal Toolchain)
make test          # go test ./...
make vet           # go vet ./...
```

- **Never `go build` directly** — always `make build` (outputs to `dist/`).
- Version is injected via `-ldflags -X main.version=$(git describe ...)`.
- The engine is compiled in only under the `cgo_sdcpp` build tag. Default builds
  use the stub (`internal/engine/engine_stub.go`) that returns `ErrNoRuntime`.

## Structure

```
main.go                     entry; injects version; delegates to internal/cli
internal/cli/               subcommand dispatch (gen/models/serve/version) — thin, testable
internal/profile/           model profiles + per-architecture defaults (the gotcha-hiding core)
internal/catalog/           curated model catalog (content_rating, license, RAM tier, source)
internal/engine/            Engine interface; stub now, CGO sd.cpp binding under `cgo_sdcpp`
docs/{ja,en}/               RFP; docs/adr/ architecture decisions
Makefile                    build/build-engine/deps/test/vet/clean/build-all
```

## Gotchas

- **Toolchain for the engine build**: `cmake` (`brew install cmake`) and the Xcode
  **Metal Toolchain** (`xcodebuild -downloadComponent MetalToolchain`) are required
  for `make build-engine`. Neither is needed for scaffold work.
- **CGO static link is the biggest risk** — Metal shader embedding + ggml static
  link. The build bring-up spike is the first Phase 1 milestone; de-risk CPU-only
  first, then add Metal.
- **Models are never bundled/redistributed.** Users download; the catalog only
  points at sources and surfaces license + content rating.
- **NSFW is opt-in.** `questionable`/`explicit` entries need `--allow-nsfw` / config.
- **v-prediction is experimental.** sd.cpp v-pred/ZSNR support is maturing; NoobAI /
  Illustrious v2 are flagged `Experimental` until verified. eps models are reliable.
- **Catalog source ids are provisional** (RFP stage) — verify each HF/Civitai id
  before wiring `pull`.
- **Secrets**: `HF_TOKEN` / `CIVITAI_TOKEN` via env/config only — never commit.
- **Tests are mandatory** for new behaviour; keep the engine layer injectable so
  generation logic is testable without the runtime.
