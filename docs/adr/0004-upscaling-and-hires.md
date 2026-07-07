# ADR-0004: Upscaling — standalone ESRGAN + profile-driven hires.fix

- Status: Accepted
- Date: 2026-07-07

## Context

DiffusionBee (the app image-forge references) has an **Upscale** feature, and
many catalog models — especially Pony / Illustrious anime SDXL — carry a note on
their Civitai page along the lines of *"always enable hires.fix."* Two distinct
needs:

1. **Upscale an arbitrary existing image** (super-resolution, post-process).
2. **Generate at hires quality** — the *hires.fix* pipeline (generate at native
   res → upscale → a second img2img pass at higher res that adds detail and fixes
   artifacts). This is a **per-model recommendation**, exactly the kind of gotcha
   image-forge already hides behind the profile (like CLIP-skip, VAE, score tags).

stable-diffusion.cpp supports both natively:

- Standalone: `new_upscaler_ctx(esrgan_path, …)` + `upscale(ctx, img, factor, …)`
  + `free_upscaler_ctx`. Needs an ESRGAN model (Real-ESRGAN).
- hires.fix: `sd_img_gen_params_t` embeds a `sd_hires_params_t hires`
  (`enabled`, `upscaler`, `model_path`, `scale`, `denoising_strength`, `steps`,
  `upscale_tile_size`). `sd_hires_params_init` defaults to `upscaler = LATENT`,
  `scale = 2.0`, `denoise = 0.7` — so hires works with **no extra model** (latent
  upscaler); an ESRGAN model is an optional quality upgrade.

## Decision

**Adopt both**, integrated the image-forge way.

### 1. Standalone upscaler

- Engine: `Upscale(input, esrganPath, factor)` wrapping the upscaler ctx API.
- CLI: `image-forge upscale <in> -o <out> [--scale N] [--model <name> | --model-path <p>]`.
- MCP: an `upscale` tool (workspace-relative input path → upscaled output path).
- The ESRGAN model is a first-class but **non-diffusion** catalog item — see §3.

### 2. Generation-time hires.fix, driven by the profile

- The **model profile** gains hires defaults (`enabled`, `scale`, `denoise`,
  `upscaler`, `steps`). A model whose upstream notes say "use hires" ships with
  `Hires.Enabled = true` in its catalog entry, so `gen -m <model>` produces
  hires-quality output with no extra flags — the gotcha stays hidden.
- The user overrides with **`--hires auto|on|off`** (mirroring `--prediction
  eps|v|auto`): **`auto` (default) follows the profile**, `on` forces it enabled,
  `off` forces it disabled. Fine-grained overrides: `--hires-scale`,
  `--hires-denoise`, `--hires-upscaler latent|lanczos|nearest|model`,
  `--hires-model <esrgan>`.
- image-forge's opinionated defaults (overridable, and more conservative than
  sd.cpp's): upscaler `latent` (no download), **`scale 1.5`**, **`denoise 0.5`**
  — 2.0/0.7 is heavy for the 16 GB baseline and 0.7 drifts too far from the base
  composition.
- `serve` and the `mcp` `generate` tool accept the same hires controls.

### 2b. Hires upscaler selection uses downloaded ESRGAN models, config-driven

The hires upscaler is not fixed to the built-in latent one: once a user pulls an
ESRGAN upscaler, hires.fix can use it automatically. Resolution precedence for
the hires upscaler (first wins):

1. CLI `--hires-upscaler` (`latent|lanczos|nearest|model`) + `--hires-model <name>`.
2. The model profile's hires settings.
3. Config `[hires] upscaler` — `"latent"` (built-in), `"auto"`, or an installed
   upscaler-model name.
4. Built-in `latent` fallback.

`"auto"` (the config default) means: **use a downloaded ESRGAN upscaler if one is
installed, else fall back to `latent`.** When a model is needed (hires
`upscaler=model`, or the standalone `upscale` command with no `--model`), the
ESRGAN is chosen by: `--hires-model` / `--model` → profile → `[upscaler]
default_model` → the sole installed upscaler → (for hires) fall back to latent /
(for `upscale`) a clear "pull one" error.

Config:

```toml
[hires]
# hires.fix upscaler: "latent" (built-in) | "auto" (a downloaded ESRGAN if
# installed, else latent) | an installed upscaler-model name.
upscaler = "auto"

[upscaler]
# default ESRGAN used when a model is needed (hires=model, `upscale` without --model)
default_model = "realesrgan-x4-anime"
```

This keeps the zero-download path working (latent) while automatically upgrading
to a better upscaler the moment one is present — the behaviour is fully
option/config-controlled.

### 3. ESRGAN models in the catalog (a non-diffusion `Kind`)

`catalog.Entry` and `store.InstalledModel` gain a `Kind` (`""`/`diffusion`
default, or `upscaler`). Upscaler entries set `Kind: upscaler` + `Source.HF` to
the ESRGAN file and carry no VAE / prediction / profile. `models pull` downloads
and registers them; `models list` shows their kind; `upscale --model` and
`--hires-upscaler model --hires-model` resolve `upscaler`-kind models. Seed
entries (ungated HF): `realesrgan-x4plus` (general,
`schwgHao/RealESRGAN_x4plus/RealESRGAN_x4plus.pth`) and `realesrgan-x4-anime`
(`utnah/esrgan/RealESRGAN_x4plus_anime_6B.pth`).

## Consequences

- Both DiffusionBee-style upscaling and the "always use hires" recommendation are
  covered, with hires folded into the profile so users don't have to know about
  it. `--hires auto/on/off` keeps the escape hatch.
- **Cost**: hires.fix roughly doubles generation time (a second pass) and raises
  peak memory; the conservative `scale 1.5` default keeps 16 GB usable. Documented
  in the README; the CLI warns if a large target is requested.
- A new `Kind` on catalog/registry entries — small, additive, defaults preserve
  existing behavior.
- Latent hires needs no download; ESRGAN (standalone and `--hires-upscaler model`)
  shares one Real-ESRGAN model, pulled on demand.
