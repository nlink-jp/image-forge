# ADR-0006: Register LoRA and ControlNet as first-class model kinds

- Status: Accepted
- Date: 2026-07-09

## Context

`gen` already accepts LoRAs and ControlNet, but only as **raw file paths**:

```sh
image-forge gen "..." --lora /some/path/lcm.safetensors:0.8 \
                      --control-net /some/path/canny.safetensors --control edge.png
```

Nothing else knows these files exist. The registry (`models list/pull/import/rm`)
only understands two kinds — `"" ` (diffusion) and `"upscaler"` — so:

- There is no way to **acquire** a LoRA or ControlNet through image-forge; users
  must find, download, and place the file themselves.
- There is nothing for a **front-end to enumerate**. image-forge-gui wants to
  offer pickers, and `models list --json` returns no LoRA/ControlNet entries.
- Nothing records **architecture compatibility**. An SDXL LoRA silently produces
  garbage (or an error deep in sd.cpp) against an SD1.5 base. The user finds out
  at render time.

Meanwhile the resident engine has an asymmetry worth encoding in the design:
`reloadKey` (see `internal/cli/render.go`) includes the ControlNet path but
**not** the LoRAs. So **changing LoRAs is per-request and cheap; changing the
ControlNet model reloads the base model.**

## Decision

**Treat LoRA and ControlNet as registry model kinds, exactly as `upscaler`
already is**, and let name resolution (not raw paths) be the primary interface.

1. **Two new kinds** in `catalog` and `store`:

   ```go
   KindDiffusion  = ""          // default
   KindUpscaler   = "upscaler"
   KindLoRA       = "lora"       // new
   KindControlNet = "controlnet" // new
   ```

   with `IsLoRA()` / `IsControlNet()` predicates alongside `IsUpscaler()`.

2. **They carry an Arch, unlike upscalers.** An upscaler is architecture-agnostic
   and registers a bare `profile.Profile{Name}`. A LoRA/ControlNet is bound to the
   base architecture it was trained against, so it registers
   `profile.Profile{Name, Arch}` — no prediction, clip-skip, VAE, or hires fields.
   `models list --json` therefore reports a usable `arch` for them, which is what
   lets a front-end (and, later, the CLI) filter incompatible combinations.

3. **Catalog entries** for a curated set (as with the ESRGAN upscalers), so
   `models pull <name>` acquires them. They reuse the existing `Source{HF: …}`
   download path — no new fetch machinery.

4. **Names resolve to paths** in `gen` / `serve`: `--lora <name-or-path>:<weight>`
   and `--control-net <name-or-path>`. A value that names an installed model of
   the right kind resolves to its path; anything else is passed through as a path.
   This mirrors `resolveUpscalerModel` and keeps every existing path-based
   invocation working.

5. **The serve protocol is unchanged.** It already takes `loras: ["path:weight"]`
   and `control_net: "<path>"`. A GUI reads `models list --json` (which exposes
   `path`, `kind`, `arch`) and sends resolved paths. No new wire fields, no new
   engine surface.

## Consequences

- `image-forge models pull sdxl-lcm-lora` works; front-ends can enumerate and
  arch-filter LoRAs/ControlNets from `models list --json` alone.
- Existing `--lora /abs/path.safetensors:0.8` invocations keep working —
  resolution is name-first, path-fallback.
- The registry gains kinds that have no diffusion profile. Anywhere that assumes
  "an installed model is renderable" must check `Kind`. `installedViews`'
  `MultiComponent` heuristic (`Path == "" && Kind != "upscaler"`) becomes
  `Path == "" && Kind == KindDiffusion`, or LoRAs would be misreported.
- Arch is advisory: we record and filter on it, but sd.cpp is the final arbiter.
  A mismatch is a clear up-front error instead of a confusing render.
- **ControlNet changes reload the base model** (it is in `reloadKey`); LoRA
  changes do not. Front-ends should surface that cost — swapping a ControlNet
  mid-session is not free, swapping a LoRA is.
- We do not attempt LoRA *stacking semantics* beyond what sd.cpp does (each
  `path:weight`, applied per render).
- **We pin sd.cpp's `lora_apply_mode` to `at_runtime`.** Its `auto` default picks
  `immediately` for non-quantized weights, merging the LoRA into the model params
  up front (`ModelManager::apply_loras_to_params`). That path **segfaults** on
  UNet-only LoRAs such as the SDXL LCM-LoRA (2364 `lora_unet_*` tensors, no
  `lora_te*`), taking the whole process down — fatal for the resident `serve`
  engine a GUI depends on. `at_runtime` applies the LoRA during the forward pass
  instead: robust, at the cost of per-step compute rather than a one-off merge.
  Correctness over speed. Revisit if upstream fixes the merge path.
