# image-forge

> **Status: early scaffold (Phase 2).** The command surface and model catalog are
> in place; the diffusion runtime (stable-diffusion.cpp, statically linked) is not
> wired yet. See [docs/en/image-forge-rfp.md](docs/en/image-forge-rfp.md) for the
> full design.

A local diffusion image-generation engine and model-management CLI for macOS
(Apple Silicon). Run modern models — anime-focused (Animagine XL, Illustrious,
Pony family) and general high-quality (FLUX, SD3.5, Z-Image) — locally, **without
touching a single internal setting**.

Every per-model gotcha (CLIP-skip, the dedicated SDXL fp16-fix VAE, native
resolution, sampler/steps, prediction type, quantization) is hidden inside a
**model profile** and applied automatically. `image-forge` is the local-diffusion
counterpart to `gem-image` (cloud Gemini).

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
# Scaffold binary (no diffusion runtime — for development):
make build

# Full binary with the statically-linked sd.cpp runtime (needs cmake + Metal Toolchain):
brew install cmake
xcodebuild -downloadComponent MetalToolchain
make build-engine
```

The output is a single binary at `dist/image-forge`.

## Usage

```sh
# Generate (settings come from the model profile; override with flags):
image-forge gen -p "score_9, 1girl, cherry blossom" -m animagine-xl-4 -o out.png

# img2img:
image-forge gen -p "..." --init in.png --strength 0.6 -o out.png

# Apply LoRAs:
image-forge gen -p "..." --lora my-style:0.8 --lora detail:0.4

# Manage models (download + VAE + RAM-aware quantization + profile registration):
image-forge models list
image-forge models pull animagine-xl-4
image-forge models import ~/Downloads/my-checkpoint.safetensors
image-forge models quantize animagine-xl-4 --to q4_k
```

Progress is emitted as a JSON-line stream on stderr (`load` / `progress` / `done` /
`error`), one event per line.

## Models & content rating

The curated catalog tags each entry with `content_rating`
(`safe` / `questionable` / `explicit`) and `license`. Questionable/explicit models
require an explicit opt-in (`--allow-nsfw` or `allow_nsfw = true` in config); the
final judgment is left to you.

Downloads come from Hugging Face / Civitai / direct URLs. Provide tokens via
`HF_TOKEN` / `CIVITAI_TOKEN` (env or config) — **never commit them**.

> **v-prediction models** (NoobAI, Illustrious v2) are marked *experimental*:
> stable-diffusion.cpp's v-pred / ZSNR support is still maturing. Epsilon-prediction
> models (Animagine XL 4.0, Illustrious v1, Pony) work reliably.

## Configuration

`~/.config/image-forge/config.toml` — default model, quantization policy,
`allow_nsfw`, output directory. Environment: `HF_TOKEN`, `CIVITAI_TOKEN`.

## Development

```sh
make build   # scaffold binary
make test    # go test ./...
make vet     # go vet ./...
```

Part of **util-series**. See [AGENTS.md](AGENTS.md) for structure and gotchas.
