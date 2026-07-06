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
- **RAM: 16 GB baseline (minimum), 32 GB+ recommended.** SDXL / Z-Image run well on
  16 GB; FLUX / SD3.5 Large / Qwen-Image need Q4 quantization on 16 GB and are
  comfortable on 32 GB+.
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
| `--seed` | seed (default 42) |
| `--steps` `--cfg` `-W` `-H` `--sampler` `--clip-skip` | override the profile |
| `--vae` | external VAE (overrides the profile) |
| `--prediction` | force `eps` / `v` (v-prediction) / `auto`; default: from the model profile |
| `--batch` | number of images |
| `--init` `--strength` | img2img: init image + denoise strength (0..1; lower = closer to the init) |
| `--mask` | inpaint (with `--init`): regenerate only the white region of the mask (same size as the init) |
| `--lora <path>:<weight>` | apply a LoRA (repeatable) |
| `--control-net <model>` `--control <image>` | ControlNet: steer generation by a control image (add `--control-strength`, and `--canny` to edge-preprocess) |

Progress is emitted as a JSON-line stream on stderr (`load` / `progress` / `done` /
`error`), one event per line; the output path is printed to stdout.

### `models` — manage models

```sh
image-forge models list                                  # catalog + installed
image-forge models pull <name | hf:owner/repo/file | civitai:<versionId> | url> [--allow-nsfw] [--name N]
image-forge models import <path> [--name N] [--arch A] [--vae V]
image-forge models quantize <name> --to <type> [--name N]
image-forge models rm <name>
```

- **pull** resolves a catalog name to its source, downloads the checkpoint and (for
  catalog entries) the dedicated VAE, and registers a profile. You can also pull a
  raw `hf:owner/repo/file` reference, a `civitai:<versionId>` reference (the number
  in a Civitai model's download URL — requires `CIVITAI_TOKEN`), or a direct URL.
  **Multi-component models** (e.g. FLUX) download all their weight files — diffusion
  model + text encoders + VAE — automatically. Downloads resume and retry, so a
  dropped connection during a large model doesn't start over.
- **import** registers a model file you already have; the architecture is
  auto-detected from the name (override with `--arch sdxl|sd15|sd35|flux|zimage`).
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
`seed`, `steps`, `cfg`, `width`, `height`, `sampler`, `prediction`, `clip_skip`,
`batch`, `init`, `mask`, `strength`, `loras` (`["path:weight", ...]`),
`control_net`, `control`, `control_strength`, `canny`, `output`, `vae`. Absent
optional fields fall back to the model profile.

**Output** — one JSON event per line on stdout:
`{"kind":"ready"}` at start, `{"kind":"load","message":"<path>"}` on a (re)load,
`{"kind":"progress","progress":0.5}` per step, `{"kind":"done","output":"a.png"}`
per image, `{"kind":"error","message":"..."}` on failure.

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
- **Config file** (optional): `$IMAGE_FORGE_HOME/config.toml` (or point
  `$IMAGE_FORGE_CONFIG` elsewhere). Sets `default_model`, `output`, `allow_nsfw`, and
  fallback tokens. See [`config.example.toml`](config.example.toml) — copy it and edit.
- **Tokens**: `HF_TOKEN` (gated HF repos), `CIVITAI_TOKEN` (Civitai downloads).
  Environment variables take precedence over the config file. **Never commit tokens.**

## Development

```sh
make build          # scaffold binary (no runtime)
make build-engine   # full binary with the sd.cpp runtime
make test           # go test (third_party excluded)
make vet
```

Part of **util-series**. See [AGENTS.md](AGENTS.md) for structure and gotchas, and
[docs/en/image-forge-rfp.md](docs/en/image-forge-rfp.md) for the full design.
