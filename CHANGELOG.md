# Changelog

All notable changes to image-forge are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/), and the
project adheres to [Semantic Versioning](https://semver.org/).

## [0.10.0] - 2026-07-07

Upscaling: a standalone ESRGAN upscaler and profile-driven hires.fix.

### Added
- **`image-forge upscale <in> -o <out> [--scale N] [--model <name>|--model-path <p>]`**
  — standalone Real-ESRGAN super-resolution for any image. Verified E2E
  (512×512 → 2048×2048). Also an MCP `upscale` tool.
- **hires.fix at generation time**, driven by the model profile. `gen --hires
  auto|on|off` — **`auto` (default) follows the profile**, `on`/`off` force it;
  `--hires-scale` / `--hires-denoise` / `--hires-upscaler latent|lanczos|nearest|model`
  / `--hires-model` fine-tune. Conservative defaults (latent, scale 1.5, denoise
  0.5) keep the 16 GB baseline usable. `serve` and the MCP `generate` tool accept
  the same controls. A model whose upstream notes recommend hires (e.g.
  `prefect-pony-xl`) ships with it on by default. Verified E2E (512 base → 768
  hires second pass). See ADR-0004.
- **ESRGAN upscalers in the catalog** as a new `upscaler` kind: `realesrgan-x4plus`
  (general) and `realesrgan-x4-anime` (anime), pulled like any model.
- **Config `[hires] upscaler` and `[upscaler] default_model`**: `[hires] upscaler`
  defaults to `"auto"`, so once you pull an ESRGAN, hires.fix automatically uses
  it (instead of the built-in latent upscaler); set it to `"latent"` to pin the
  built-in. Precedence: CLI flag → model profile → config → built-in latent.

## [0.9.1] - 2026-07-07

### Added
- **`prefect-pony-xl`** catalog entry — Prefect Pony XL v6 (Civitai version
  2114187), a high-quality Pony-based SDXL model. Single-file, with the fp16-fix
  VAE and the Pony `score_*` prefix applied automatically (needs `CIVITAI_TOKEN`).
  Verified E2E (clean 1024×1024 anime render).

### Docs
- **`docs/{en,ja}/adding-a-model`** — a contributor guide for adding a catalog
  model: source lookup (HF single-file / Civitai version id / multi-component),
  the per-arch / Pony / photorealistic gotchas, tests, and the mandatory
  pull+render E2E. Linked from the READMEs and AGENTS.md.

## [0.9.0] - 2026-07-07

An `image-forge mcp` server so an AI can generate images.

### Added
- **`image-forge mcp`**: an MCP (Model Context Protocol) stdio server that
  exposes image generation to an AI, reusing the resident engine. It is
  file-mediated (like the voice-/video-studio MCP servers): tools return file
  **paths**, never image bytes; work happens in a **workspace** directory and
  outputs land in its `output/`. Generation is **async** — `generate` returns a
  `job_id` and the client polls `check_job`. Tools: `get_usage`, `generate`,
  `check_job`, `list_models` (same views as `models list --json`). Errors are
  structured `{code, message, details}`. Verified E2E over stdio with a dummy
  client (handshake → generate → live progress → a real PNG in the workspace).
  See ADR-0003.

### Fixed
- **sd.cpp's model-load progress bar no longer leaks to stdout.** With no
  progress callback registered, sd.cpp printf's a `|####| N/M - X MB/s` bar to
  stdout during the model read (in `new_sd_ctx`, before the render callback is
  set). It was invisible in a terminal (a `\r`-updated line that flashes by) but
  was preserved on a pipe — which corrupted the `mcp` JSON-RPC stream and added
  noise to `gen`/`serve` stdout. A no-op callback now keeps sd.cpp silent
  whenever we are not actively rendering; the `mcp` server additionally isolates
  stdout at the fd level (defense-in-depth).

## [0.8.0] - 2026-07-07

Separate installed / catalog views for `models list`, plus JSON output.

### Added
- **`models list --json`** (on every mode): machine-readable output. Installed →
  a JSON array; `--catalog` → an array with an `installed` flag per entry;
  `--all` → an object with `installed` and `catalog` arrays. Rendered from stable,
  purpose-built structs, decoupled from the internal registry/catalog types.
- **`models list --catalog`** lists only the curated catalog (with an `installed`
  column), and **`models list --all`** shows installed models and the catalog as
  two clearly-labelled sections. See ADR-0002.
- **`LICENSE` file** (MIT, © 2026 nlink-jp), matching the util-series convention;
  README notes the statically-linked stable-diffusion.cpp / ggml (both MIT).

### Changed
- **`models list` now shows installed models by default** (name, arch, rating,
  license, path) instead of the old combined catalog+installed table. Use
  `--catalog` for the catalog and `--all` for both. This separates the two
  questions — "what do I have" vs. "what can I get" — that the merged table
  blurred together.

## [0.7.0] - 2026-07-07

More curated Civitai anime models, and `pull` reuses files you already have.

### Added
- **Five curated Civitai SDXL anime models** (each needs `CIVITAI_TOKEN`):
  `illustrious-xl-v1.1` and `akium-unmotivated` (Illustrious family), and the
  Pony family `t-ponynai3-v7`, `t-ponynai3-v5.5`, `momoiro-pony`. Every entry
  resolves a Civitai version id, attaches the SDXL fp16-fix VAE, and applies
  clip-skip 2 / 1024 / euler_a; the Pony entries auto-prefix the `score_*`
  quality tags (the Pony gotcha, hidden in the profile). Verified E2E
  (`t-ponynai3-v7` → clean 1024×1024 anime render).
- **Two curated photorealistic SDXL models** (Hugging Face, ungated, no token):
  `realvisxl-v5` (RealVis V5.0) and `juggernaut-xl-v9`. Single-file checkpoints
  with the fp16-fix VAE attached; they override to **clip-skip 1** (the realism
  default) instead of the anime-leaning SDXL default of 2. Verified E2E
  (`realvisxl-v5` → a photorealistic 1024×1024 portrait).

### Changed
- **`models pull` reuses an already-downloaded file** instead of re-fetching it:
  if the resolved checkpoint or VAE is already present (even registered under a
  different name), the multi-GB download is skipped. Previously only
  multi-component pulls skipped existing files.

## [0.6.0] - 2026-07-07

Independent scheduler and random-seed batch generation.

### Added
- **`--scheduler`**: pick the noise schedule (discrete / karras / exponential / ays
  / …) independently of the sampler; `serve` accepts a `scheduler` field.
- **`--count N` with random seeds**: generate N images in one loaded session.
  `--seed -1` draws a fresh random seed per image; files are named
  `<out>-<seed>.png`, the seed is printed, and it is reported in the `done` event
  (so `serve` clients get it too).

## [0.5.0] - 2026-07-07

ControlNet, more models (Z-Image, SD3.5), and a config-path move.

### Added
- **ControlNet**: `gen --control-net <model> --control <image> [--control-strength]
  [--canny]` guides generation by a control image. The ControlNet model loads with
  the base model; `--canny` runs sd.cpp's edge preprocessor on the control image.
  `serve` accepts `control_net` / `control` / `control_strength` / `canny`. Verified
  E2E: a canny control from a red-apple photo steers a txt2img "green apple" to the
  same silhouette.
- **Z-Image Turbo** catalog entry + LLM (Qwen) text-encoder support for
  multi-component models (`OpenParams.LLM` → sd.cpp `llm_path`). Verified E2E (bf16
  Qwen). Note: ComfyUI fp8-scaled/mixed encoder builds are not sd.cpp-compatible.
- **SD3.5 Medium** catalog entry (GGUF diffusion + CLIP-L/G + T5 + an ungated VAE
  mirror), multi-component. Verified E2E (renders a legible in-image "SD3.5" sign).

### Changed
- **Config file location** is now `~/.config/image-forge/config.toml` (XDG config
  dir), matching the other util-series tools. The pre-v0.5 location
  (`$IMAGE_FORGE_HOME/config.toml`) is still read as a fallback.

## [0.4.0] - 2026-07-07

Multi-component models (FLUX) and resumable downloads.

### Added
- **Multi-component models**: models assembled from separate weight files
  (diffusion model + CLIP-L / CLIP-G / T5-XXL encoders + VAE) — e.g. FLUX — are now
  supported. `models pull flux1-schnell` downloads all components (skipping any
  already present) and registers them; `gen`/`serve` load them together.
  `catalog.Source` gains component refs and `engine.Open` takes an `OpenParams`
  struct with per-component paths. Verified E2E: FLUX schnell renders a
  photorealistic image with legible in-image text.
- **Resumable downloads**: `Fetch` resumes a partial `.part` via an HTTP Range
  request and retries transient failures (with backoff) — large model downloads
  routinely hit dropped connections.

## [0.3.0] - 2026-07-07

Civitai downloads and catalog updates.

### Added
- **Civitai downloads**: `models pull civitai:<versionId>` (and catalog entries with
  a Civitai source) resolve the file via the Civitai API and download it with your
  token (`CIVITAI_TOKEN` or `civitai_token`, required — Civitai returns 401 without
  one). Tokens are redacted from logs and error messages.

### Changed
- **Illustrious XL v1.0** is now directly pullable (`models pull illustrious-xl-v1`),
  file-qualified like Animagine (single-file SDXL + fp16-fix VAE). The FLUX and
  Z-Image catalog entries now note that they are multi-component (diffusion +
  encoders + VAE) and need `models import` — single-file pull isn't supported yet.

## [0.2.0] - 2026-07-06

Image editing and v-prediction support.

### Added
- **inpaint**: `gen --init <image> --mask <mask>` regenerates only the masked
  (white) region and preserves the rest (black); the mask is 1-channel and must
  match the init image size. Works with regular models (masked img2img) — no
  dedicated inpainting model required. `serve` accepts a `mask` field. Verified E2E
  (sky-only storm-cloud edit over a preserved meadow).
- **v-prediction** wired and verified: the model profile sets the prediction
  parameterization at model load; `--prediction eps|v|auto` (and the serve
  `prediction` field) override it. NoobAI XL v-pred is promoted from experimental —
  verified E2E: the profile (v) renders cleanly while forcing `--prediction eps`
  produces pure noise, proving v-pred is correctly applied.

## [0.1.0] - 2026-07-06

Initial release — a local diffusion image-generation engine and model-management
CLI for macOS (Apple Silicon), built on stable-diffusion.cpp (CGO/Metal, single
binary). Runs SDXL anime and general models locally with per-model gotchas hidden
behind profiles.

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
