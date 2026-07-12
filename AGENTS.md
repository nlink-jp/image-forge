# AGENTS.md — image-forge

## What this is

A local diffusion image-generation engine + model-management CLI for macOS
(Apple Silicon). Wraps **stable-diffusion.cpp** (ggml/Metal, statically linked via
CGO) behind a Go single binary. Hides per-model gotchas (CLIP-skip, dedicated VAE,
resolution, sampler, prediction type, quantization) inside **model profiles** so
users never hand-tune them. Series: **util-series**. Local-diffusion counterpart to
`gem-image` (cloud Gemini).

Status: **v0.17.0 released** (public, signed + notarized). **Phase 1 + Phase 2
complete.** inpaint (`gen --init --mask`) wired + E2E-verified. `gen` txt2img/img2img/inpaint/
LoRA, `models` list/import/pull/open/quantize/rm/gc, resident `serve`, config.toml — all E2E
on M2 Max (SD1.5 + Animagine XL / SDXL, q8_0, LCM-LoRA, NoobAI v-pred). v-prediction
is wired via the profile (`--prediction eps|v|auto` overrides). Civitai downloads,
**multi-component models** (FLUX / SD3.5 / Z-Image / **Anima**; resumable/retrying
downloads; a component field may use a `civitai:<versionId>` ref to pair a Civitai-hosted
DiT with shared HF encoders/VAE — e.g. `anima-yume`, `nova-anime-am`), and **ControlNet**
(`--control-net`/`--control`/`--canny` via `preprocess_canny`) are all wired and
verified E2E. ControlNet is verified for **SD1.5** (`controlnet-canny-sd15`) **and
SDXL** (`controlnet-canny-sdxl`) — the vendored sd.cpp (upstream #1752) converts
diffusers-format ControlNet names on load and sizes the ControlNet graph for SDXL,
so diffusers SDXL ControlNets load directly (no pre-conversion). `gen`, `serve`,
**and the MCP `generate` tool** all expose LoRA + ControlNet. Phase 1 + Phase 2
features are complete. Full design: `docs/{ja,en}/image-forge-rfp*`.

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
                            upscale (upscale.go); mcp server bootstrap (mcp.go); shared resident
                            engine (render.go); model-resolution + profile/hires merge (resolve.go)
internal/mcp/               `image-forge mcp` MCP stdio server (ADR-0003): jsonrpc, transport,
                            mcpserver (initialize/tools list+call), toolerr ({code,message,details}),
                            job (async FIFO worker), workspace (os.Root containment), tools
                            (get_usage/generate/check_job/list_models/upscale)
internal/profile/           model profiles, per-arch defaults, arch Detect (the gotcha-hiding core)
internal/catalog/           curated model catalog (kind, content_rating, license + license_flags/attribution, trigger_words, RAM tier, source) + Profile()
internal/store/             installed-model registry (JSON) at $IMAGE_FORGE_HOME/registry.json;
                            ModelsDir relocatable via config models_dir / $IMAGE_FORGE_MODELS_DIR
                            (store.SetModelsDir, set from config in cli.Run — store stays config-free).
                            InstalledModel.Files() lists a model's weight files;
                            Registry.ReferencedFiles() is the in-use set for gc/purge
internal/config/            optional config.toml (default_model/output/allow_nsfw/tokens/
                            [performance] flash_attn + vae_tiling; BurntSushi/toml)
internal/download/          HF (hf:owner/repo/file) / URL fetch with progress; token from caller
internal/engine/            Session interface (Open loads once, Render renders many); output.go
                            (pure, tested); pngmeta.go (pure: tEXt/iTXt writer for embedded
                            metadata, ADR-0005); engine_stub.go (no runtime); engine_sdcpp.go (CGO
                            sd.cpp binding: Open/Render/Upscale, under `cgo_sdcpp`)
docs/{ja,en}/               RFP; adding-a-model.md (catalog contributor guide); docs/adr/ decisions
Makefile                    build/build-engine/deps/test/vet/clean/build-all
```

## Gotchas

- **Toolchain for the engine build**: `cmake` (`brew install cmake`) and the Xcode
  **Metal Toolchain** (`xcodebuild -downloadComponent MetalToolchain`) are required
  for `make deps` / `make build-engine`. Neither is needed for scaffold work.
- **sd.cpp prints progress to stdout unless a callback is registered (critical)**:
  with no `sd_progress_cb` set, sd.cpp printf's a `|####| N/M - X MB/s` bar to
  **fd 1 (stdout)** — notably during model load in `new_sd_ctx` (before Render sets
  the real callback). Invisible in a TTY (a `\r`-updated line), but preserved on a
  pipe, so it corrupts the `mcp` JSON-RPC stream and adds noise to `gen`/`serve`
  stdout. Fix (two layers): (1) `engine_sdcpp.go` keeps a **no-op callback**
  installed whenever not rendering (`ifg_silence_progress`, called before
  `new_sd_ctx` and restored after each render) so sd.cpp never printf's; (2)
  `runMCP` also dups the real stdout for the transport and repoints fd 1 at stderr
  (`redirectStdoutToStderr` in `mcp.go`) as defense-in-depth. Verify with the dummy
  stdio client (initialize→generate→check_job→PNG); a blank/garbage line on the
  response stream means something reached stdout anyway.
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
- **Adding a catalog model**: follow [`docs/en/adding-a-model.md`](docs/en/adding-a-model.md)
  (JA: [`docs/ja/adding-a-model.ja.md`](docs/ja/adding-a-model.ja.md)) — the source
  lookup, the per-arch/Pony/realistic gotchas, the entry's `Kind` + `LicenseFlags` /
  `Attribution` / `TriggerWords` (each backed by a test to keep it honest), and the
  mandatory pull+render E2E.
- **Upscaling & hires.fix** (ADR-0004): sd.cpp does both. Standalone
  `image-forge upscale` uses `new_upscaler_ctx`/`upscale` with an ESRGAN model
  (catalog `Kind: "upscaler"`, e.g. `realesrgan-x4plus`). hires.fix is set in the
  gen params (`g.hires` via `sd_hires_params_init`) and driven by the model
  profile; `gen --hires auto|on|off` (auto follows the profile). The hires
  upscaler resolves CLI → profile → config `[hires] upscaler` → built-in latent;
  `"auto"`/`[upscaler] default_model` pick a downloaded ESRGAN if present.
  `str_to_sd_hires_upscaler` is case-sensitive on display names — map lowercase
  names to the enum directly (see `hiresUpscalerEnum`).
- **Performance flags are opt-in (`[performance]`).** `flash_attn` (`OpenParams.FlashAttn`,
  set at load) and `vae_tiling` (`Request.VAETiling` → `g.vae_tiling_params.enabled`, set
  per-render) both default OFF because they change same-seed output slightly; each has a
  `gen --flash-attn` / `--vae-tiling` override and is also honored by `serve`/`mcp` via config.
  `vae_tiling` is the escape hatch for high-res VAE-decode OOM on 16 GB (sd.cpp falls back to a
  256px tile when `tile_size`/`rel_size` are 0). sd.cpp also has an `auto_fit` ctx mode that
  auto-tiles on actual OOM, but it bundles discrete-GPU param-offload logic and logs a scary
  "no usable GPU devices" on Metal, so we don't enable it.
  `wtype` (`OpenParams.WType` → `cp.wtype`, reusing the `quantTypes` map) quantizes weights at
  load so a big f16 model fits without a pre-converted GGUF — unlike the other two it IS part of
  `reloadKey` (it changes the loaded weights), and unknown types are rejected by `validateWType`
  (`engine.QuantTypes()`/`ValidWType`, cgo + stub).
- **Disk reclamation (`rm --purge` / `gc`) is share-aware AND HITL-gated.** Files
  can be shared across registry entries (a common VAE, text encoders reused by
  multi-component models), and `import` registers files *in place* (Path can be
  outside ModelsDir). So `--purge` deletes a file only when no *other* installed
  model references it (usedByOthers computed *before* removal, so a declined purge
  is a full no-op) AND it's under ModelsDir; `gc` only touches unreferenced
  top-level files in ModelsDir, never directories, never files elsewhere.
- **Every destructive delete goes through `confirmFunc` (confirm.go).** `gc --force`
  and `rm --purge` list the files + size, then call `confirm(summary)`. Production
  wires `stdinConfirm` → `confirmDestructive`, which requires an interactive TTY
  (`isInteractive`, a dependency-free `ModeCharDevice` check) and an exact `yes`
  (`affirmative`). **A non-TTY (script / pipe / `go test`) can never confirm, so it
  can never delete.** The only escape hatch is `--confirmed-by-frontend`
  (`resolveConfirm`): a trusted front-end (the GUI) passes it *after* its own
  confirmation dialog, so `ServeClient.removeArgs` appends it for `rm --purge`. This exists because a
  `gc --force` run against the wrong ModelsDir once deleted real models. Test the
  delete logic via the `runGc` / `runRm` cores with an injected confirmer on a
  throwaway dir; regression tests (`*_NoTTY_DeletesNothing`) assert the real command
  paths delete nothing without a TTY. Still also pin `store.SetModelsDir(dir)` in
  such tests — `cli.Run()` leaks the real config's models_dir into that global
  (see `gcTestDirs`); the TTY gate is the second line of defense.
- **Models are never bundled/redistributed.** Users download; the catalog only
  points at sources and surfaces license + content rating.
- **NSFW is opt-in.** `questionable`/`explicit` entries need `--allow-nsfw` / config.
- **v-prediction is experimental.** sd.cpp v-pred/ZSNR support is maturing; NoobAI /
  Illustrious v2 are flagged `Experimental` until verified. eps models are reliable.
- **Distilled vs non-distilled Flux.** `flux1-schnell` is guidance-distilled (4 steps,
  CFG 1 — the ArchFlux default). `flux1-dev` is NOT: it needs ~20 steps, so its
  catalog entry sets the per-entry `Steps` override (also `CFG` available). sd.cpp's
  `distilled_guidance` default (3.5) is already the right Flux guidance, so nothing
  else is needed. Qwen-Image would need a new arch (`profile` has none) — deferred.
- **Flow/DiT guidance knobs (`Request.Guidance`/`FlowShift`/`SLGScale`/`ImgCFG`).**
  Map to `g.sample_params.guidance.distilled_guidance` / `.flow_shift` / `.slg` /
  `.img_cfg`. Each 0 keeps sd.cpp's default; sd.cpp ignores knobs that don't apply to
  the loaded arch (distilled/flow are no-ops for SDXL/SD1.5; SLG is DiT-only). SLG
  needs a skip-layers array — a package-level `slgSD35Layers = [7,8,9]` (stable
  address) is passed only while `generate_image` runs, which never retains it.
  Exposed as `gen --guidance/--flow-shift/--slg-scale/--img-cfg`, serve JSON, and MCP.
- **Catalog source ids are provisional** (RFP stage) — verify each HF/Civitai id
  before wiring `pull`.
- **Secrets**: `HF_TOKEN` / `CIVITAI_TOKEN` via env/config only — never commit.
- **Tests are mandatory** for new behaviour; keep the engine layer injectable so
  generation logic is testable without the runtime.
