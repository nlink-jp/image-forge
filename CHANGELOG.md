# Changelog

All notable changes to image-forge are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/), and the
project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Project scaffold: Go module, `make build` → `dist/`, single-binary subcommand
  dispatch (`gen` / `models` / `serve` / `version`).
- Model profile system (`internal/profile`): per-architecture defaults that hide
  CLIP-skip, native resolution, sampler/steps, cfg, and negative-prompt handling
  for SD1.5 / SDXL / SD3.5 / FLUX / Z-Image.
- Model catalog (`internal/catalog`): curated entries with `content_rating`,
  `license`, RAM tier, prediction type, and source; NSFW opt-in helper.
- Engine interface (`internal/engine`) with a toolchain-less stub; the real
  stable-diffusion.cpp CGO binding lands under the `cgo_sdcpp` build tag.
- RFP (`docs/{ja,en}`) and ADR-0001 (engine embedding via CGO static link).
- `third_party/stable-diffusion.cpp` submodule (master-758) + `make deps` to build
  ggml/sd.cpp static libraries with the Metal backend.
- **Build bring-up spike (ADR-0001) proven**: `make build-engine` statically links
  sd.cpp + ggml + Metal into a single binary (system dylibs/frameworks only; ~57 MB
  with the full generation path linked in). Verified on Apple M2 Max. The project's
  highest-risk task is de-risked.
- **`gen` txt2img wired end-to-end**: prompt / negative / seed / steps / cfg / size /
  sampler / clip-skip / batch / `--lora <path>:<weight>` / `-o` output, via sd.cpp's
  `new_sd_ctx` + `generate_image`. Progress streams as JSON lines on stderr; images
  save as PNG. Verified on M2 Max (SD1.5 Q8_0 GGUF → 512×512 in ~54 s incl. Metal
  cold start).
- **`models` tooling**: `list` (catalog + installed, with rating/license/RAM tier),
  `import <path>` (register a local model, auto-detect architecture), `pull
  <name|hf:owner/repo/file|url>` (download to the data dir + register; NSFW opt-in via
  `--allow-nsfw`), `rm`. New `internal/store` (JSON registry) and `internal/download`
  (HF/URL fetch with progress) packages.
- **Profile wiring in `gen`**: `-m <name>` resolves an installed model and
  auto-applies its profile (clip-skip, VAE, resolution, sampler, steps, cfg, prompt
  prefix, negative handling); explicit flags override. `--model-path` bypasses the
  registry. Verified E2E (import sd15 → `gen -m sd15` with only `--steps` set → the
  SD15 profile filled 512×512 / euler_a / clip-skip 1).
- **`models pull` auto-downloads the dedicated VAE** (e.g. the SDXL fp16-fix) and
  attaches it, hiding that gotcha; catalog entries are file-qualified HF refs.
- **SDXL flow validated on the real target**: `models pull animagine-xl-4
  --allow-nsfw` (6.5 GB checkpoint + fp16-fix VAE) → `gen -m animagine-xl-4` with
  only prompt/negative auto-filled clip-skip 2 / 1024×1024 / euler_a / fp16-fix VAE,
  producing a correct 1024×1024 anime render on M2 Max (~1:47, no black-image NaN).
- **img2img**: `gen --init <PNG/JPEG> --strength <0..1>` loads the init image and
  matches the output size to it. Verified E2E (sd15, apple.png → guided transform).
- **Resident `serve` mode**: reads one JSON request per line on stdin and streams
  events on stdout, keeping the model loaded across requests and reloading only when
  the requested model changes — avoids the per-request model load + Metal init.
  Verified E2E: two requests → a single `load` event. The engine is now a **Session**
  (`Open` loads once; `Render` renders many); `gen` and `serve` share the
  model-resolution + profile-merge path (`resolve.go`).
- **`models quantize <name> --to <type>`**: converts a registered model to a GGUF at
  the given quant (q8_0/q4_k/...) via sd.cpp's `convert`, baking in the model's VAE,
  and registers the result with the same profile. Verified: Animagine XL 4.0 6.5 GB
  → 4.0 GB q8_0 → correct 1024×1024 render (baked fp16-fix VAE, no black image).
- **Config file** (`config.toml`): optional `default_model`, `output`, `allow_nsfw`,
  and fallback `hf_token` / `civitai_token` (env vars take precedence). Loaded from
  `$IMAGE_FORGE_HOME/config.toml` (or `$IMAGE_FORGE_CONFIG`); ships a
  [`config.example.toml`](config.example.toml). `gen` uses `default_model` / `output`
  when omitted; `models pull` honors `allow_nsfw`. New dep: `github.com/BurntSushi/toml`.

### Fixed
- **cgo pointer panic when applying LoRAs** ("Go pointer to unpinned Go pointer"):
  the LoRA array must live in C memory, not a Go slice, so `&g` passed to
  `generate_image` holds no Go pointers. LoRA (`--lora <path>:<weight>`) is now
  validated E2E with LCM-LoRA — at 4 steps / cfg 1 the output is coherent only with
  the LoRA applied (incoherent without it).

### Notes / Known limitations
- Civitai token support is deferred; catalog entries whose HF source is repo-only
  (no file) are not yet directly pullable (use `models import`).
- inpaint and ControlNet are not wired yet.
- Progress events currently reflect sd.cpp's internal phases (text encoder / sampler /
  VAE), so the `step X/Y` denominator changes between phases — to be normalized to
  sampling steps.
- sd.cpp logs to stderr alongside our JSON progress; a log callback to route/quiet it
  is a follow-up.
- Metal cold-load is ~8.5 s (one-time), reinforcing the value of the resident
  `serve` mode (load model/device once).
