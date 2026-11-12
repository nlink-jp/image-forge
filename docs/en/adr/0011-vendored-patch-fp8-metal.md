# ADR-0011: Patch the vendored ggml so Metal declines FP8 matrix products

- Status: Accepted
- Date: 2026-11-13

## Context

stable-diffusion.cpp `master-1090` keeps an FP8 safetensors tensor (dtype `F8_E4M3` / `F8_E5M2`) at
one byte per element. Before a matrix product with such a weight it asks the backend whether it can
do the product; when the backend says no, sd.cpp casts the weight for that call.

The Metal backend of the ggml that sd.cpp vendors says yes, and has no kernel for it. The first such
product aborts the process. Measured on this version:

- every FLUX and SD3.5 render, which load `t5xxl_fp8_e4m3fn`, aborted;
- so did a checkpoint in which a single tensor (`time_embed.0.weight`) was stored as FP8.

The first attempt stayed outside the vendored tree: image-forge asked sd.cpp, through
`tensor_type_rules`, to convert the FP8 components to F16 as they were read. That fixed the catalog's
FLUX and SD3.5. The pre-release review found what it missed, and the measurement confirmed it: sd.cpp
exempts a list of tensors from any conversion rule (input and output layers, time embedders,
embeddings), so an FP8 checkpoint a user imports kept those at FP8 and still aborted, where v0.30.0
rendered it. The rule also overrode a configured `wtype`.

The cause is one function in ggml's Metal backend. Nothing image-forge passes to sd.cpp reaches it.

## Decision

- Carry **one local patch** to the vendored ggml:
  `third_party/patches/0001-ggml-metal-decline-fp8-mul-mat.patch`. It makes
  `ggml_metal_supports_mul_mat_op` return false for an FP8 weight, so sd.cpp's own fallback runs.
  Nothing else changes; the conversion rules are removed.
- Patches are files in this repository, listed in order in `third_party/patches/series` with the
  directory each applies in. The submodule pointer still names an unmodified upstream commit.
- `make deps` runs on every engine build. It applies the patches (`scripts/vendor-patches.sh apply`)
  and then lets cmake decide what is stale, so a moved pin, a changed patch or a library built before
  a patch is rebuilt, not linked as it is.
- `scripts/vendor-patches.sh verify` (`make verify-vendor`) runs on every engine build. It covers the
  vendored checkout and every submodule inside it, and fails unless each sits on the commit its
  parent records, holds no untracked file, and differs from that commit only as its patches do (the
  same files by the same line counts; for a directory with no patch, not at all). A hand edit
  anywhere in the vendored tree, a patch dropped from the series while still in the tree, or a patch
  that no longer describes the tree stops the build. What it compares is file names and line counts,
  not content: an edit confined to the lines a patch already changed is not seen.
- The submodule is marked `ignore = dirty` in `.gitmodules`, because its working copy now always
  differs from the pinned commit. That also means `git describe --dirty`, which names the build, no
  longer reflects the vendored tree; the pin itself is still tracked, and `verify` is what watches
  the content.
- One patch is one decision and one ADR. A second patch gets its own.

## Consequences

- An FP8 weight stays at one byte in memory and is cast per call. Peak memory for FLUX is lower than
  in v0.30.0, which expanded FP8 to F16 at load.
- Moving the stable-diffusion.cpp pin has a fixed order: `vendor-patches.sh revert`, move the pin,
  `make deps`. If the patch no longer applies, the build stops and says so — that is the moment to
  check whether upstream fixed the backend (render `flux1-schnell` with the patch removed) and drop
  the patch, not to force it.
- The patch is in a submodule of the submodule. While it is applied, `git submodule update
  --recursive` either refuses to move that tree (when the new commit touches the patched file) or
  carries the edit across unexamined; revert first, so that `apply` is what decides.
- The defect is upstream's. Reporting it there is the repository owner's call; this ADR does not
  depend on it.

## Alternatives considered

- **Refuse FP8 files the rules cannot fully convert.** No crash, but checkpoints that worked in
  v0.30.0 stop working, and image-forge would carry a copy of sd.cpp's exemption list that drifts.
- **Convert FP8 files to an F16 copy before loading.** Leaves the vendored tree alone and matches
  v0.30.0, at the price of a second copy on disk (about 10 GB for the 5 GB T5) and a conversion pass.
- **Build with `SD_USE_UPSTREAM_GGML`.** It does convert FP8 at load, but it also switches off sd.cpp's
  own Metal extensions and INT8 paths.
