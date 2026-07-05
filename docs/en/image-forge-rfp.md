# RFP: image-forge

> Generated: 2026-07-06
> Status: Draft

## 1. Problem Statement

`image-forge` is a local image-generation engine and model-management CLI that lets
users run modern local diffusion models — anime-focused ones (Animagine XL,
Illustrious, Pony family, ...) as well as general high-quality ones (FLUX, SD3.5,
Z-Image, ...) — **on macOS (Apple Silicon) with zero technical knowledge required,
in a one-stop flow.**

Every per-model "settings you must get right to make it work" — CLIP-skip, the
dedicated VAE (to avoid the SDXL fp16 black-image NaN), native resolution,
recommended sampler/steps, prediction type (eps / v-pred), quantization level — is
never surfaced to the user. It is hidden inside a **model profile** and applied
automatically. Target users are people who "want good images locally but do not want
to touch a model's internal settings." It brings DiffusionBee's "no dependencies /
no technical knowledge needed" philosophy to a CLI + model-ops tool.

## 2. Functional Specification

### Commands / API Surface

Single binary with co-located subcommands (util-series idiom).

- `image-forge gen` — txt2img / img2img generation.
  - Key flags: `-p/--prompt`, `-n/--negative`, `--seed`, `--steps`, `--cfg`,
    `-W/-H` (or `--size`), `--sampler`, `--clip-skip`, `--vae`, `--batch/--num`,
    `-m/--model <profile>`, `-o/--output`.
  - LoRA: `--lora <name>:<weight>` (repeatable).
  - img2img: `--init <image>` `--strength <float>`.
  - clip-skip / VAE / resolution / sampler / cfg / negative handling are
    **auto-applied** from the profile and overridable via explicit flags.
- `image-forge models` — model operations.
  - `list` — installed + catalog (columns: name / arch / content_rating /
    license / RAM tier / installed).
  - `pull <name | hf:repo | civitai:id | url>` — download checkpoint (+ required
    VAE) → auto-detect RAM → quantize if needed → register profile, all in one.
    Flags: `--quant q8_0|q4_k|none`, `--allow-nsfw`.
  - `import <path.safetensors>` — register a local file; auto-detect architecture
    and build a profile.
  - `quantize <name> --to <type>` — GGUF quantization (uses sd.cpp's built-in
    converter).
  - `rm <name>` — unregister.
- `image-forge serve` — **Phase 2**. Model-resident JSON-line API daemon.

### Input / Output

- Output: PNG (generation parameters embedded in metadata).
- Progress: a **JSON-line stream** on stderr (a modernization of DiffusionBee's
  `sdbk` text protocol; one event per line: `load` / `progress` / `done` / `error`).
- Input: prompt via flag or `--input` file. img2img / future inpaint take
  image/mask by file path.

### Configuration

- User config (TOML): `~/.config/image-forge/config.toml` (default model,
  quantization policy, `allow_nsfw`, output directory, ...).
- Environment variables: `CIVITAI_TOKEN` / `HF_TOKEN` (tokens live in config or
  env and are **never committed to the repository**).
- Catalog: a binary-embedded default catalog + a user-extensible local catalog.
  Each entry carries profile defaults: `arch` / `prediction_type (eps|vpred)` /
  `content_rating (safe|questionable|explicit)` / `license` / `min_ram` /
  `recommended_ram` / recommended sampler, etc.

### External Dependencies

- **stable-diffusion.cpp** (ggml; statically linked into the binary via CGO) +
  Metal.
- Hugging Face / Civitai HTTP APIs (model downloads; user-supplied tokens).
- No other external service dependencies. Model weights are **never bundled or
  redistributed**.

## 3. Design Decisions

- **Do not reimplement diffusion.** The core is delegated to the mature
  stable-diffusion.cpp. From DiffusionBee we reference **only the skeleton** —
  resident daemon + progress streaming + model management — not its ML code
  (DiffusionBee's own author migrated from a home-grown Keras implementation to
  Apple Core ML).
- **Language = Go + CGO static link.** Fits util-series' single-binary +
  co-located-subcommand + Developer ID signing / notarization flow. Aims for a
  true single binary.
- **Multi-architecture profile system.** SDXL / FLUX / SD3.5 / Z-Image differ in
  sampler, cfg, whether a negative prompt applies, and default resolution, so
  defaults are held per `arch`, hiding per-model pitfalls from the user.
- **Relation to existing tools.** The local-diffusion counterpart to `gem-image`
  (cloud Gemini image generation); clearly a distinct tool.
- **Out of scope**: training / fine-tuning, model redistribution, video generation
  (Wan, etc.), non-Apple-Silicon optimization, GUI (a future separate project or
  Phase 3+).

## 4. Development Plan

### Phase 1: Core

1. **Build bring-up spike (top priority, independently reviewable)** — statically
   link ggml / stable-diffusion.cpp, including the Metal backend, via CGO into a
   single Go binary. Validate `make build` → `dist/` and the Developer ID signing +
   notarization path. Metal shader embedding and ggml static linking are the
   project's biggest technical risk, so this goes first.
2. **txt2img core + multi-arch profile system + tests** (pure functions: profile
   resolution, RAM→quant decision, sd.cpp arg building, catalog parsing; the sd.cpp
   invocation layer is made injectable).
3. **models tool** — import / pull / quantize / list / rm. Catalog
   (content_rating / license / RAM tier), NSFW opt-in, token handling.
4. **LoRA** (multiple LoRAs + weights).
5. **img2img** (init image + denoise strength).
6. **Compatibility verification** — assess sd.cpp's v-prediction / ZSNR support and
   decide catalog status (experimental → promotable?) for NoobAI / Illustrious v2
   (v-pred) models.

### Phase 2: Features

- `serve` resident mode (JSON-line API, model loaded once for repeated shots).
- inpaint (mask input).
- ControlNet.
- Prompt weighting; exposing additional schedulers / samplers.

### Phase 3: Release

- README.md / README.ja.md / CHANGELOG.md / AGENTS.md.
- `make build-all` (darwin arm64 primary), signed + notarized zip (canonical
  binary name), real-model E2E, `gh release`, umbrella submodule pointer update,
  org profile update, `check-org.sh` green.

**Independently reviewable units**: (1) build spike, (2) txt2img + profiles,
(3) models tool, (4) LoRA, (5) img2img are each independently reviewable.

## 5. Required API Scopes / Permissions

No OAuth scopes or IAM roles.

- **Hugging Face**: a user-supplied HF token (optional) for gated repositories.
- **Civitai**: a user-supplied API token for downloads / NSFW content.
- Both are managed via user config / environment variables and never committed.
- No cloud permissions (GCP / Vertex, etc.) required — fully local operation.

## 6. Series Placement

Series: **util-series**

Reason: a pipe-friendly, local-first Go single-binary CLI with co-located
subcommands that maps directly onto util-series conventions (`make build` →
`dist/`, Developer ID signing + notarization, macOS releases). It is neither a
cloud-service client (cli-series) nor an LLM-interaction tool (lite-series); as a
local data-transformation / processing utility, util-series is the best fit.

## 7. External Platform Constraints

- **Environment**: Apple Silicon + Metal (arm64 macOS). **Baseline (minimum) 16GB /
  recommended 32GB+.** A CPU fallback is possible via sd.cpp but slow, so it is not
  a primary target.
- **Memory tiers**:
  - 16GB: SDXL family (~6.5GB fp16) / Z-Image Turbo run comfortably. FLUX / SD3.5
    Large / Qwen-Image require Q4 quantization.
  - 32GB+: large models such as FLUX.1-dev / SD3.5 Large / Qwen-Image run
    comfortably with quantization.
  - `models pull` detects actual RAM and proposes/applies a quantization level
    using 16 / 32GB as thresholds.
- **Model licenses**: the user's responsibility. The tool never bundles or
  redistributes models. The catalog surfaces `license` (commercial / output rights)
  and `content_rating`; NSFW is opt-in and the final judgment is left to the user.
- **Civitai API**: rate limits, tokens required for many downloads, ToS (downloads
  are user-initiated). **HF**: gated repos need a token, large LFS files
  (resume / checksum).
- **v-prediction / ZSNR**: sd.cpp support is developing / limited. eps models run
  reliably; v-pred models are treated as experimental for now.

---

## Discussion Log

- **Reference policy**: DiffusionBee's Text2Img is referenced as the skeleton
  (resident + progress + model management); its ML code is not reused. DiffusionBee
  itself migrated from a home-grown Keras implementation to Apple Core ML,
  validating the "delegate the engine core to an existing runtime" stance.
- **Engine core**: stable-diffusion.cpp (ggml / Metal / GGUF) selected over MLX,
  diffusers, and Core ML for its alignment with single-binary / minimal-dependency /
  notarization workflows.
- **Embedding method**: CGO static link vs. bundled subprocess considered →
  **CGO static link chosen** (favoring a true single binary and a single signature).
  Because Metal shader embedding / ggml static linking is Phase 1's biggest risk,
  the build bring-up spike is placed first in the plan.
- **Target models**: initially Animagine XL / Pony-family tPonynai3 → SDXL required
  → CLIP-skip 2 / SDXL fp16-fix VAE / 1024 resolution / euler_a·25 steps / score
  tags identified as the "pitfalls to make them work." Established the policy of
  auto-applying these via profiles.
- **Sources / catalog**: HF / Civitai / direct URL supported. Agreed to attach a
  content_rating flag to catalog entries, keep NSFW opt-in, and leave the judgment
  to the user.
- **HF high-quality model survey**: anime (Animagine XL 4.0 / Illustrious XL /
  NoobAI vpred) + general (FLUX.1-schnell [Apache 2.0] / FLUX.1-dev [non-commercial] /
  SD3.5 / Qwen-Image [Apache 2.0] / Z-Image Turbo). v-pred models are experimental
  since sd.cpp support is still maturing. Added `license` and `prediction_type` to
  the catalog schema.
- **Hardware requirement**: 16GB baseline / 32GB+ recommended, fixed as a design
  requirement.
- **Scope**: not restricted to anime; general high-quality models included too.
  Phase 1 extra features are LoRA + img2img (inpaint / serve in Phase 2;
  ControlNet / GUI in Phase 2/3).
- **Naming**: `image-forge` (a local engine clearly differentiated from gem-image,
  the cloud Gemini tool).
