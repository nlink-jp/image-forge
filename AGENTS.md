# AGENTS.md — image-forge

## What this is

A local diffusion image-generation engine + model-management CLI for macOS
(Apple Silicon). Wraps **stable-diffusion.cpp** (ggml/Metal, statically linked via
CGO) behind a Go single binary. Hides per-model gotchas (CLIP-skip, dedicated VAE,
resolution, sampler, prediction type, quantization) inside **model profiles** so
users never hand-tune them. Series: **util-series**. Local-diffusion counterpart to
`gem-image` (cloud Gemini).

Status: **v0.1.0 released** (public, signed + notarized). **Phase 2 in progress:**
inpaint (`gen --init --mask`) wired + E2E-verified. `gen` txt2img/img2img/inpaint/
LoRA, `models` list/import/pull/quantize/rm, resident `serve`, config.toml — all E2E
on M2 Max (SD1.5 + Animagine XL / SDXL, q8_0, LCM-LoRA, NoobAI v-pred). v-prediction
is wired via the profile (`--prediction eps|v|auto` overrides). Next Phase 2:
ControlNet, Civitai token, catalog file-qualify. Full design: `docs/{ja,en}/image-forge-rfp*`.

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
internal/cli/               dispatch (cli.go); gen (gen.go); models (models.go); serve (serve.go);
                            shared model-resolution + profile merge (resolve.go)
internal/profile/           model profiles, per-arch defaults, arch Detect (the gotcha-hiding core)
internal/catalog/           curated model catalog (content_rating, license, RAM tier, source) + Profile()
internal/store/             installed-model registry (JSON) at $IMAGE_FORGE_HOME/registry.json
internal/config/            optional config.toml (default_model/output/allow_nsfw/tokens; BurntSushi/toml)
internal/download/          HF (hf:owner/repo/file) / URL fetch with progress; token from caller
internal/engine/            Session interface (Open loads once, Render renders many); output.go
                            (pure, tested); engine_stub.go (no runtime); engine_sdcpp.go (CGO
                            sd.cpp binding: Open/Render, txt2img+img2img, under `cgo_sdcpp`)
docs/{ja,en}/               RFP; docs/adr/ architecture decisions
Makefile                    build/build-engine/deps/test/vet/clean/build-all
```

## Gotchas

- **Toolchain for the engine build**: `cmake` (`brew install cmake`) and the Xcode
  **Metal Toolchain** (`xcodebuild -downloadComponent MetalToolchain`) are required
  for `make deps` / `make build-engine`. Neither is needed for scaffold work.
- **CGO static link is proven** (ADR-0001): `make build-engine` links sd.cpp + ggml
  + Metal into one 4.7 MB binary (verified on M2 Max, `image-forge version` inits
  Metal). `make deps` builds the sd.cpp static libs; the CGO flags live in
  `internal/engine/engine_sdcpp.go` (paths via `${SRCDIR}`).
- **Metal cold-load ~8.5 s** (one-time) — a reason the resident `serve` mode loads
  the model/device once.
- **`go test ./...` must exclude `third_party/`** — the vendored submodule carries
  stray Go files (libwebp swig). Use `make test` / `make vet` (they filter it out).
- **cgo pointer rule**: any array/struct whose pointer is stored inside a C struct
  passed to C (e.g. `g.loras`, `g.init_image.data`) must be C-allocated
  (`C.malloc`), never a Go slice — else cgo panics with "Go pointer to unpinned Go
  pointer". LoRA validated via LCM-LoRA (coherent 4-step gen only with the LoRA).
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
