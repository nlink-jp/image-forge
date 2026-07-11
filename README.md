# image-forge

A local diffusion image-generation engine and model-management CLI for macOS
(Apple Silicon). Run modern models — anime-focused (Animagine XL, Illustrious,
Pony family) and general high-quality (FLUX, SD3.5, Z-Image) — locally, **without
touching a single internal setting**.

Every per-model gotcha (CLIP-skip, the dedicated SDXL fp16-fix VAE, native
resolution, sampler/steps, prediction type) is hidden inside a **model profile**
and applied automatically. `image-forge` is the local-diffusion counterpart to
`gem-image` (cloud Gemini).

Built on [stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp)
(ggml/Metal), statically linked into a single Go binary.

## Requirements

- **macOS on Apple Silicon** (arm64) with Metal.
- **RAM: 16 GB baseline (minimum), 32 GB+ recommended.** SDXL / Z-Image / Anima run
  well on 16 GB; FLUX / SD3.5 need Q4 quantization on 16 GB and are comfortable on
  32 GB+.
- Build toolchain (engine build only): `cmake`, the Xcode **Metal Toolchain**, and
  a CGO-enabled Go 1.26+.

Model weights are **not bundled**; you download them yourself (the catalog surfaces
each model's license and content rating).

## Install / Build

```sh
brew install cmake
xcodebuild -downloadComponent MetalToolchain
make build-engine            # single binary at dist/image-forge

make build                   # scaffold binary WITHOUT the runtime (for development)
```

## Quick start

```sh
# 1. Get a model — downloads the checkpoint + its dedicated VAE and registers a profile:
image-forge models pull animagine-xl-4 --allow-nsfw

# 2. Generate — the profile fills in CLIP-skip / VAE / 1024 / sampler for you:
image-forge gen -m animagine-xl-4 -p "1girl, cherry blossoms, masterpiece" -o out.png
```

## How profiles work

Each model carries a **profile** that encodes the settings it needs to produce good
output (architecture, CLIP-skip, dedicated VAE, native resolution, sampler, steps,
CFG, prompt prefix). `gen -m <name>` applies that profile automatically; any flag
you pass explicitly overrides it. This is what lets you run Pony/Animagine SDXL
models correctly without knowing that they need CLIP-skip 2, a 1024 canvas, and the
fp16-fix VAE.

## Commands

### `gen` — generate

| flag | meaning |
| --- | --- |
| `-p` | prompt (required) |
| `-n` | negative prompt |
| `-m` | installed model name (see `models list`) |
| `--model-path` | path to a model file (bypasses the registry) |
| `-o` | output path (default `out.png`; batches insert an index) |
| `--seed` | seed (default 42; `-1` = random) |
| `--count` | number of images; with `--seed -1` each gets a fresh random seed (files named `<out>-<seed>.png`, and the seed is printed) |
| `--steps` `--cfg` `-W` `-H` `--sampler` `--scheduler` `--clip-skip` | override the profile (`--scheduler`: discrete / karras / exponential / ays / …) |
| `--vae` | external VAE (overrides the profile) |
| `--prediction` | force `eps` / `v` (v-prediction) / `auto`; default: from the model profile |
| `--batch` | images per run (sd.cpp batch, sequential seeds) |
| `--init` `--strength` | img2img: init image + denoise strength (0..1; lower = closer to the init) |
| `--mask` | inpaint (with `--init`): regenerate only the white region of the mask (same size as the init) |
| `--lora <name\|path>:<weight>` | apply a LoRA (repeatable). An installed LoRA's registry name resolves to its file; a path also works. Applied per render — no model reload |
| `--control-net <name\|path>` `--control <image>` | ControlNet: steer generation by a control image (add `--control-strength`, and `--canny` to edge-preprocess). **Changing the ControlNet reloads the base model** |
| `--hires auto\|on\|off` | hires.fix (generate → upscale → a 2nd img2img pass for detail). `auto` (default) follows the model profile; `on`/`off` force it |
| `--hires-scale` `--hires-denoise` `--hires-upscaler latent\|lanczos\|nearest\|model` `--hires-model <name\|path>` | fine-tune hires (defaults: latent, scale 1.5, denoise 0.5) |
| `--no-metadata` | do not embed the prompt/parameters/model into the PNG |

Progress is emitted as a JSON-line stream on stderr (`load` / `progress` / `done` /
`error`), one event per line; the output path is printed to stdout.

**Embedded metadata**: generated PNGs carry the prompt, parameters, and model in
text chunks by default — an **AUTOMATIC1111-compatible `parameters` chunk** (which
Civitai / A1111 parse) plus an **`image-forge` JSON** chunk. Unicode prompts
use `iTXt` (UTF-8) so they round-trip. Turn it off with `--no-metadata` or config
`[metadata] embed = false` (e.g. to keep prompts out of shared images).

**No filesystem paths are embedded.** Models are recorded by identifier — the
registry name when installed, else the file's base name — so a shared image never
reveals your directory layout or username, and `"loras": ["lcm-lora-sdxl:1"]` is
directly re-runnable. Input images (img2img init, ControlNet control) are not
recorded at all; only the parameters that shaped the render (`strength`, `canny`).

**Attribution.** When a model in use requires credit, the `image-forge` JSON
carries a `credit` field combining the attributions of every model that shaped
the render (base model + LoRAs), so whoever shares the image has the credit the
license calls for. It is a record only — nothing is burned into the pixels — and
permissive renders write no `credit` at all. A model's own attribution is also in
`models list --json` as `attribution`, next to its `license_flags`.

### `upscale` — super-resolve an image

```sh
image-forge upscale <input> -o <output> [--scale N] [--model <name> | --model-path <path>]
```

Runs a standalone Real-ESRGAN pass (typically 4×) over an existing image. The
ESRGAN model comes from an installed `upscaler`-kind model (`--model`, e.g.
`realesrgan-x4plus` / `realesrgan-x4-anime` — `models pull` them) or a direct
`--model-path`; with neither, it uses the config `[upscaler] default_model` or the
sole installed upscaler. Progress streams as JSON on stderr; the output path is
printed to stdout.

### `models` — manage models

```sh
image-forge models list [--catalog|--all] [--json] [--kind K]   # installed (default), catalog, or both
image-forge models pull <name | hf:owner/repo/file | civitai:<versionId> | url> [--allow-nsfw] [--name N]
image-forge models import <path> [--name N] [--arch A] [--vae V] [--kind K] [--trigger "a,b"]
image-forge models quantize <name> --to <type> [--name N]
image-forge models rm <name>
```

The registry holds four **kinds** (`--kind diffusion|lora|controlnet|upscaler`):
a base diffusion model, plus three auxiliary kinds that aren't renderable on
their own. LoRA and ControlNet entries record the base **architecture** they were
trained against, so incompatible combinations can be caught up front (ADR-0006).

```sh
image-forge models pull lcm-lora-sdxl          # a LoRA, like any other model
image-forge models list --kind lora --json     # what a front-end enumerates
image-forge gen -p "a red apple" -m animagine-xl-4 \
  --lora lcm-lora-sdxl:1.0 --steps 6 --cfg 1.5 --sampler lcm

image-forge models pull controlnet-canny-sdxl  # a ControlNet (SDXL)
image-forge gen -p "a house at night, snow" -m juggernaut-xl-v9 \
  --control-net controlnet-canny-sdxl --control photo.png --canny
```

**ControlNet** ships for both **SD1.5** (`controlnet-canny-sd15`) and **SDXL**
(`controlnet-canny-sdxl`). The engine loads both the original ControlNet format
and **diffusers-format SDXL ControlNets** (converting the names on load), so most
canny/depth/etc. ControlNets work via `models import <path> --kind controlnet`
once you've confirmed they render.

Many LoRAs need **trigger words** in the prompt to take effect — without them the
LoRA loads and silently does nothing. They are recorded in the catalog, kept on
the registry entry at install time, printed after `pull` / `import`, and exposed
as `trigger_words` in `models list --json` (so a front-end can show or insert
them). `--trigger "a,b"` sets them when importing a local file.

- **list** shows your **installed** models by default (name, arch, rating,
  license, path); pulled ESRGAN upscalers appear here too with arch `upscaler`.
  `--catalog` lists the curated catalog instead (with an `installed` column), and
  `--all` shows both as separate sections. Add `--json` to any of these for
  machine-readable output (each entry carries a `kind` — `""`/diffusion, `lora`,
  `controlnet`, or `upscaler`; installed → array; catalog → array with an
  `installed` flag; `--all` → an object with `installed` and `catalog` arrays).
- **pull** resolves a catalog name to its source, downloads the checkpoint and (for
  catalog entries) the dedicated VAE, and registers a profile. You can also pull a
  raw `hf:owner/repo/file` reference, a `civitai:<versionId>` reference (the number
  in a Civitai model's download URL — requires `CIVITAI_TOKEN`), or a direct URL.
  **Multi-component models** (e.g. FLUX) download all their weight files — diffusion
  model + text encoders + VAE — automatically. Downloads resume and retry, so a
  dropped connection during a large model doesn't start over, and a checkpoint or
  VAE you already have (even under another registered name) is reused rather than
  re-downloaded.
- **import** registers a model file you already have; the architecture is
  auto-detected from the name (override with `--arch sdxl|sd15|sd35|flux|zimage|anima`).
- **quantize** converts a registered model to a GGUF at `--to` ∈
  `q8_0 q5_0 q5_1 q4_0 q4_1 q2_k q3_k q4_k q5_k q6_k f16 f32`, baking in its VAE, and
  registers it as `<name>-<type>`. q8_0 ≈ half size at near-full quality; q4_* ≈ a
  third, for tight RAM.

### `serve` — resident mode

Loads a model once and renders many requests against it (reloading only when the
requested model changes), avoiding the per-request model load + Metal init.

```sh
image-forge serve < requests.jsonl
```

**Input** — one JSON object per line on stdin:

```json
{"prompt":"1girl, cherry blossoms","model":"animagine-xl-4","seed":1,"output":"a.png"}
```

Fields: `prompt` (required); `model` or `model_path`; and optional `negative`,
`seed`, `steps`, `cfg`, `width`, `height`, `sampler`, `scheduler`, `prediction`,
`clip_skip`, `batch`, `init`, `mask`, `strength`, `loras` (`["path:weight", ...]`),
`control_net`, `control`, `control_strength`, `canny`, `output`, `vae`. Absent
optional fields fall back to the model profile. `seed: -1` draws a random seed
(reported in the `done` event).

**Output** — one JSON event per line on stdout:
`{"kind":"ready"}` at start, `{"kind":"load","message":"<path>"}` on a (re)load,
`{"kind":"progress","progress":0.5}` per step, `{"kind":"done","output":"a.png"}`
per image, `{"kind":"error","message":"..."}` on failure.

### `mcp` — MCP server (use it from an AI)

Exposes image generation to an AI over the Model Context Protocol (JSON-RPC 2.0
on stdio), reusing the resident engine.

```sh
image-forge mcp [--workspace-root <dir>]
```

It is **file-mediated** (like the voice-/video-studio MCP servers): tools return
file **paths**, never image bytes. Work happens in a **workspace** directory (a
default root under the data dir, or an agent-prepared `workspace_root` per call);
generated PNGs land in the workspace's `output/`. Generation is **async** — a
render takes a minute or two, so the server returns a `job_id` immediately and
the client polls.

Tools:

- **`get_usage`** — the operating manual (workspace model, params, job lifecycle,
  recovery table). Call it first.
- **`generate`** — enqueue a render: `workspace_id` + `prompt` (required), plus
  optional `model`, `negative`, `seed`, `steps`, `cfg`, `width`, `height`,
  `sampler`, `scheduler`, `clip_skip`, `batch`, `init`/`mask`/`strength`
  (img2img/inpaint, workspace-relative paths), `loras` (`["name-or-path:weight"]`),
  `control_net` + `control` (a workspace-relative control image) +
  `control_strength` + `canny`, `hires` (auto/on/off) +
  `hires_scale`/`hires_denoise`/`hires_upscaler`/`hires_model`, `output_name`.
  Returns a `job_id`.
- **`upscale`** — enqueue a Real-ESRGAN upscale of a workspace image:
  `workspace_id` + `input` (required), optional `model`/`scale`/`output_name`.
  Returns a `job_id`.
- **`check_job`** — poll a `job_id`: `state` (queued/running/done/error),
  progress, and on done the output PNG path(s) + seed(s).
- **`list_models`** — installed / catalog models (`scope`), the same views as
  `models list --json`.

Errors are structured `{code, message, details}`. To register it with a client,
point the client at the `image-forge` binary with the `mcp` argument. See
ADR-0003 for the design.

## Models & content rating

The curated catalog tags each entry with `content_rating`
(`safe` / `questionable` / `explicit`) and `license`. Questionable/explicit models
require an explicit opt-in (`--allow-nsfw`); the final judgment is left to you.

Downloads come from Hugging Face / Civitai / direct URLs. Provide tokens via
`HF_TOKEN` / `CIVITAI_TOKEN` (environment) — **never commit them**.

> **v-prediction models** (NoobAI, Illustrious v2) work: the model's profile sets the
> v-prediction parameterization automatically (override with `--prediction v|eps|auto`).
> Epsilon-prediction models (Animagine XL 4.0, Illustrious v1, Pony) also work.

## Configuration

- **Data directory**: `$IMAGE_FORGE_HOME` (default `~/.local/share/image-forge`)
  holds the model registry (`registry.json`) and pulled model files (`models/`).
- **Model directory** (for a bigger disk): set `models_dir` in the config (or
  `$IMAGE_FORGE_MODELS_DIR`) to store the multi-GB model files elsewhere. It
  affects **new** pulls; already-installed models keep the absolute paths in the
  registry, so both locations coexist (relocate an existing one with `models rm`
  + re-pull). The small `registry.json` stays in the data directory.
- **Config file** (optional): `~/.config/image-forge/config.toml` (honors
  `$XDG_CONFIG_HOME` and `$IMAGE_FORGE_CONFIG`). Sets `default_model`, `output`,
  `allow_nsfw`, fallback tokens, and the hires upscaler policy (`[hires] upscaler`
  defaults to `"auto"` — a downloaded ESRGAN if installed, else the built-in
  latent; `[upscaler] default_model`). See [`config.example.toml`](config.example.toml)
  — copy it and edit. (The pre-v0.5 location, `$IMAGE_FORGE_HOME/config.toml`, is
  still read as a fallback.)
- **Flash attention** (opt-in): `[performance] flash_attn = true` (or `gen
  --flash-attn`). On Apple Silicon it is neutral at native resolution and a modest
  win only on **large / hires** renders; it changes outputs slightly, so it is off
  by default to keep same-seed outputs stable.
- **Tiled VAE decoding** (opt-in): `[performance] vae_tiling = true` (or `gen
  --vae-tiling`). Decodes the final latent in overlapping 256px tiles, capping
  VAE-decode memory so **high-resolution / hires** renders that would otherwise run
  out of memory during VAE decode — the usual failure point on 16 GB — can finish.
  Costs a little speed and adds near-invisible seams, so it is off by default; turn
  it on if a high-res render dies at the decode step.
- **Tokens**: `HF_TOKEN` (gated HF repos), `CIVITAI_TOKEN` (Civitai downloads).
  Environment variables take precedence over the config file. **Never commit tokens.**

## Development

```sh
make build          # scaffold binary (no runtime)
make build-engine   # full binary with the sd.cpp runtime
make test           # go test (third_party excluded)
make vet
```

Part of **util-series**. See [AGENTS.md](AGENTS.md) for structure and gotchas,
[docs/en/adding-a-model.md](docs/en/adding-a-model.md) to add a catalog model, and
[docs/en/image-forge-rfp.md](docs/en/image-forge-rfp.md) for the full design.

## License

MIT — see [LICENSE](LICENSE).

The shipped binary statically links [stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp)
(MIT, © 2023 leejet) and [ggml](https://github.com/ggml-org/ggml) (MIT,
© 2023–2026 The ggml authors). Model weights are not bundled; each model keeps
its own license (surfaced by `models list`).
