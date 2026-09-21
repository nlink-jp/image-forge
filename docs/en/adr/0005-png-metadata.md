# ADR-0005: Embed generation metadata in PNG text chunks

- Status: Accepted
- Date: 2026-07-07

## Context

Users want the prompt, parameters, and model recorded *in* the generated image
so it is self-describing — the same thing AUTOMATIC1111 / ComfyUI / NovelAI do,
and what Civitai reads to show "generation data."

image-forge outputs **PNG**. The AI-image convention for PNG is **text chunks**
(`tEXt` / `iTXt`), **not EXIF** — EXIF is a JPEG/TIFF construct. So "put it in the
EXIF" is, for PNG, "put it in a text chunk." Go's `image/png` encoder does not
expose text chunks, and image-forge keeps a zero-extra-dependency posture (only
BurntSushi/toml), so we hand-roll the chunk insertion.

## Decision

**After encoding the PNG, splice in text chunks carrying the generation metadata.
Two keywords:**

1. **`parameters`** — an **AUTOMATIC1111-compatible** string, for interop (Civitai
   and A1111 parse it directly):
   ```
   <prompt>
   Negative prompt: <negative>
   Steps: 26, Sampler: euler_a, CFG scale: 7, Seed: 20240707, Size: 1024x1024,
   Model: prefect-pony-xl, Clip skip: 2[, Denoising strength: .., Hires upscale: ..,
   Hires upscaler: .., Version: image-forge vX.Y.Z]
   ```
2. **`image-forge`** — a **complete JSON** record (image-forge's own, lossless):
   model / model_path / prompt / negative / seed / steps / cfg / width / height /
   sampler / scheduler / clip_skip / prediction / vae / loras / img2img / hires /
   controlnet / version.

**Encoding:** `tEXt` when the string is Latin-1-safe, else **`iTXt` (UTF-8)** — so
Japanese/Unicode prompts round-trip correctly (this mirrors what PIL/A1111 do).
Chunks are inserted immediately after `IHDR`.

**Where it's built:** at the CLI layer (which knows the friendly model name,
prediction type, and binary version), then carried on `engine.Request.Metadata`
(and `engine.UpscaleParams.Metadata`) and written by `saveImage`. The engine just
writes the text it is handed; the string-building/interop logic stays in `cli` and
is unit-tested. The pure PNG-chunk writer lives in a non-cgo file, tested under the
stub build.

**Default on, with an opt-out** for privacy (an embedded prompt is visible to
anyone the image is shared with): `gen --no-metadata`, and config `[metadata]
embed = false`. `serve` / the MCP `generate` tool honor the config.

**Upscale (v0.12.1+):** `upscale` reads the source PNG's metadata
(`engine.ReadPNGText`, the inverse of the chunk writer) and **carries it through**
— the upscaled image keeps the original prompt / seed / parameters (and the
source's `parameters` chunk) plus an `upscale` sub-record `{upscaler, factor,
source}`, so an upscale stays self-describing and its provenance survives a
gallery reload. When the source has no image-forge metadata, only the light
`upscale` record is written.

**No filesystem paths (v0.13.1+).** The original record embedded absolute paths
(`model_path`, `vae_path`, `loras: ["/abs/path.safetensors:1"]`, `img2img.init`,
`controlnet.image`, `hires.model`). That was wrong. A generated image is *made to
be shared*, and an absolute path leaks the machine's layout — and, via the home
directory, **the user's name** (`/Users/alice/…`) — to anyone the image reaches,
including Civitai. It also reproduces nothing: the path is meaningless on another
machine.

So every model reference is now recorded as an **identifier**: its registry name
when installed, else the file's base name. That is *more* useful for
reproduction, not less, because `-m` / `--lora` / `--control-net` resolve
installed names (ADR-0006) — `"loras": ["lcm-lora-sdxl:1"]` is directly
re-runnable. **Input images are not recorded at all**: `img2img` keeps only
`strength`, `controlnet` only `strength` / `canny`. A file *name* can itself be
personal (`my-passport-scan.png`), and A1111's own `parameters` chunk records the
denoising strength without naming the init image — we match that. A regression
test asserts no `/Users`, `/Volumes`, or file extension can appear in either
chunk.

This narrows "lossless" (a Consequence below): we keep everything needed to
*reproduce* the image, and deliberately drop what only describes *this machine*.

## Consequences

- Images are self-describing and round-trip into the A1111/Civitai ecosystem,
  while the JSON chunk preserves everything needed to **reproduce** the render —
  but never the filesystem paths or input images that would only describe *this
  machine* (see "No filesystem paths" above). It is not a byte-for-byte record of
  the invocation, and deliberately so.
- Unicode prompts are handled (iTXt), unlike a naive tEXt-only approach.
- No new dependency; the chunk writer is pure Go and unit-tested; the interop
  string builder is tested independently of the engine.
- Opt-out covers the privacy concern of shipping prompts inside shared images.
