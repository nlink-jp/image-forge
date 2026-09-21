# ADR-0007: Let `models pull` override kind/arch/trigger for non-catalog auxiliary models

- Status: Accepted
- Date: 2026-07-14
- Supplements: [ADR-0006](0006-lora-controlnet-registry.md)

## Context

ADR-0006 made LoRA and ControlNet first-class registry kinds and let
`models pull <name>` acquire the curated ones. But it wired kind detection to the
**catalog only**. `modelsPull` reads `kind`, `arch`, and trigger words from the
matched catalog entry (`e.Kind`, `e.Arch`, `e.TriggerWords`); when the ref is
**not** a catalog name — a raw `hf:owner/repo/file`, `civitai:<id>`, or direct URL
— it falls through the `!known` branch, which hardcodes:

```go
prof = profile.ArchDefaults(profile.Detect(regName))  // full diffusion defaults
prof.Name = regName
rating = profile.RatingSafe
// kind stays "" (KindDiffusion); no trigger words
```

So pulling any LoRA that isn't in the curated catalog registers it as a **base
diffusion model**: wrong `Kind`, a full diffusion profile (prediction, clip-skip,
VAE, hires fields it has no business carrying), and no trigger words. The
consequences match ADR-0006's own warnings about kind confusion:

- `--lora <name>` can't resolve it — `resolveAuxModel` rejects a name registered
  under the wrong kind ("not a LoRA").
- `models list` (and a front-end reading `--json`) shows it as a renderable base
  model. The `MultiComponent` heuristic and arch-filtering ADR-0006 introduced
  are defeated for this entry.
- There is no arch on record, so incompatible-base filtering can't fire.

`import` already carries `--kind/--arch/--trigger` for exactly this reason, but it
only registers a **local** file. The only recovery today is a two-step dance:
`pull` the ref (mis-registered), then `import` the downloaded file with `--kind`
to overwrite the entry. That is the gap: **the acquire-and-tag path exists for
local files but not for remote refs.**

## Decision

**Give `models pull` the same `--kind`, `--arch`, and `--trigger` overrides that
`models import` has, applied only to the non-catalog (`!known`) path.** Catalog
entries stay authoritative, exactly as ADR-0006 §3 intends.

1. **New flags on `pull`** — `--kind lora|controlnet|upscaler` (default: base
   diffusion, unchanged), `--arch sdxl|sd15|…` (default: auto-detect from the
   registry name, unchanged), `--trigger "a,b"` (comma-separated activation
   tokens). Same validation helpers as `import`: `normalizeKind`, `splitTriggers`.

2. **The kind→profile mapping becomes one shared pure function.** ADR-0006's rule
   — diffusion gets full `ArchDefaults`; upscaler is a bare `Profile{Name}`;
   LoRA/ControlNet keep `Profile{Name, Arch}` and nothing else — was inlined in
   `import`. It is extracted to `auxProfile(kind, name, arch)` and used by **both**
   `pull` and `import`, so the invariant lives in exactly one place and is unit
   tested directly (per the testability rule). Aux kinds carry no VAE in either
   path (unchanged).

3. **Catalog entries ignore the overrides, and say so.** When the ref matches a
   catalog entry and an override flag was passed, `pull` emits a stderr note that
   the override is ignored (the catalog is curated and authoritative). Silently
   applying them would let a user mis-tag a vetted model; silently dropping them
   would hide intent. A visible note is the honest middle.

4. **The default is unchanged and back-compatible.** With no flags, a non-catalog
   ref still registers as a base diffusion model with auto-detected arch — every
   existing `pull` invocation behaves identically.

### Out of scope

- **`--vae` on `pull`.** `import` has it for local base models; `pull`'s VAE comes
  from the catalog entry, and a raw base-model pull needing a separate VAE is not
  the reported problem. Deliberately not added — revisit if it comes up.
- **Format/effect verification.** ADR-0006 and `docs/*/adding-a-model.md` require
  vetting a LoRA (kohya keys, renders-and-does-something) *before it enters the
  catalog*. A user pulling a raw ref with `--kind lora` is opting out of that
  curation for their own machine; `pull` does not attempt to validate the tensors.

## Consequences

- `image-forge models pull hf:owner/repo/lora.safetensors --kind lora --arch sdxl
  --trigger "…" --name my-lora` registers a correctly-typed, arch-bound,
  trigger-carrying LoRA in one step. `--lora my-lora:0.8` then resolves.
- The two-step `pull`-then-`import` workaround is no longer needed (it still works).
- The kind→profile invariant has a single home (`auxProfile`); `import` and `pull`
  can't drift apart, and the mapping is unit-testable without touching the network.
- Overrides are honored only off the catalog. A curated entry can never be
  re-tagged by a flag — the arch-compatibility guarantees ADR-0006 built on the
  catalog stay intact.
- The override is advisory the same way ADR-0006's arch is: image-forge records
  what the user asserts, sd.cpp remains the final arbiter at render time. A
  mislabeled `--kind` produces a clear resolution/compat error, not a silent
  wrong render.
