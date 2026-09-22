# Changelog

All notable changes to image-forge are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/), and the
project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- **Path judgement moved to [nlink-jp/pathguard](https://github.com/nlink-jp/pathguard)**
  (ADR-0010). `internal/mcp/workdir` is now an adapter onto it; the MCP server
  builds it with `workdir.NewResolver(store.Home(), store.ModelsDir())`. Places
  are compared by file identity and by names folded the way the disk folds
  them, instead of by name.
- A `work_dir` is now **refused** in the models directory when `models_dir`
  moves it outside the data directory, in the real places under your home from
  the list gem-agent and lagent use (newly `~/.kube`, `~/.config/gh`, `~/.azure`,
  `~/.terraform.d`, `~/.gemini`, `~/.config/mcp-bridge`, `~/.netrc`, `~/.npmrc`,
  `~/.pypirc`, `~/.git-credentials`, `~/.vault-token`, `~/.docker/config.json`,
  `~/.claude.json`, `~/.bash_history`, `~/.zsh_history`), under every spelling of
  any refused place, wherever a link directly inside one of those directories
  points (a `~/.ssh/config` that links into a sync folder protects the file it
  points at), in the account's own home too when `$HOME` names another, and
  whenever the home directory cannot be determined. `work_dir_denied` carries
  `reason` in its `details`.
- A relative `XDG_DATA_HOME` is ignored, as the XDG spec says; it put the data
  directory under the working directory.

### Security

- **Raw LoRA, ControlNet and hires model paths from MCP are judged.** They are
  read wherever they lie and were not checked at all, so a credential file
  could be loaded as a LoRA. One in a credential or agent-control location, or a
  `.env` file, is now refused with `path_not_allowed` before the render.
  Registry names, the CLI and the GUI's `serve` loop are unchanged.

### Tests

- `TestWorkDirComesFromRequestMeta` waits for its job; returning first raced the
  removal of the temporary directory the job writes into.

## [0.28.0] - 2026-09-22

### Added

- **A LoRA or ControlNet made for another architecture is refused before
  anything loads**, in `gen`, `serve` and the MCP server alike. ADR-0006 and
  ADR-0007 promised this; the code only checked the kind, so an SDXL LoRA on an
  SD1.5 base failed deep in sd.cpp or drew garbage. Only architectures that are
  facts are compared — the catalog's own, or one given with `--arch` — so a
  guess never refuses anything. To overrule a record you know is wrong,
  re-register with `--arch`, or pass the LoRA / ControlNet file by path.
- Every registration records where its architecture came from (`arch_source`:
  catalog, flag or detected), and `models list --json` / MCP `list_models`
  report `arch_trusted`, so a front-end can filter by the same rule. A model
  registered before this version is trusted only if it is the catalog entry of
  the same name.

### Changed

- **`--arch` is validated** on `models import` and `models pull`:
  `sd15|sdxl|sd35|flux|zimage|anima`, any case (Pony, Illustrious and NoobAI are
  `sdxl`). A value such as `pony`, `SDXL-turbo` or `unknown` used to be stored
  as typed — for a base model it silently gave the 512 px defaults of an
  unknown architecture — and is now an error.

### Fixed

- The hires upscaler default was described as `latent` in `gen --help`, the MCP
  `generate` schema and its usage reference, and the README. It is the model
  profile's, else config `[hires] upscaler`, whose default `auto` picks the
  ESRGAN named by `[upscaler] default_model`, else the only one installed, else
  latent. The MCP reference also said `hires_model` is required for
  `hires_upscaler=model`; without it the same pick applies.

### Documentation

- The ADRs agree with each other and with the code: ADR-0003 points at ADR-0009,
  which replaced its default root; ADR-0009 lists four fallback steps and says a
  leftover `--workspace-root` stops `image-forge mcp` from starting (measured)
  while a leftover `[mcp] workspace_root` is ignored; ADR-0004, ADR-0005,
  ADR-0002 and ADR-0008 lose smaller contradictions.

## [0.27.0] - 2026-09-21

### Fixed

- **`make verify-release` now fails closed.** Its last block chained unzip, the
  packaged binary's `--version` and `spctl` with `&&` and ended the whole chain
  in `|| true`, so a zip that did not unpack or a binary that did not run exited
  0 and the upload proceeded. Each step is now judged on its own, the packaged
  binary's `--version` must contain the tag being released, and only the
  informational `spctl` line may be ignored. Matches the org template
  (CONVENTIONS.md §Code Signing → Verifying a release).

- **`anima-turbo` reported the wrong licence, and said commercial use was
  permitted.** The entry carried NVIDIA's Open Model License — the terms of the
  Cosmos base model Anima was trained from — while the weights it downloads are
  published by CircleStone Labs under their Non-Commercial License v1.2:
  non-commercial and non-production use only, with revenue-generating use
  requiring a licence from them (generated outputs may still be used
  commercially, §2.e). It is now reported as that, flagged `non-commercial`, and
  carries the attribution notice the licence requires. A test pinned the old
  flags, so the mistake had a guard holding it in place; that expectation is
  corrected with the entry.
- **A licence correction now reaches an already-installed model.**
  `models list` took the licence recorded in the registry at install time and
  fell back to the catalog only when it was empty — so a model pulled before a
  correction kept reporting the old terms, beside corrected flags from the
  catalog. The comment above that code already said the catalog is the source of
  truth for descriptive metadata; now it is.
- **`controlnet-canny-sd15`** is reported as OpenRAIL, which is what
  `lllyasviel/ControlNet-v1-1` declares, rather than CreativeML OpenRAIL-M (a
  different licence). The fp16 conversion repo the file comes from declares none.
- **`illustrious-xl-v1`** now states that the Hugging Face card for those
  weights declares `other` / `sdxl-license` while the Civitai listing states no
  derivatives and credit required. Both exist; the stricter is reported.

### Added

- **`license_source` on every catalog entry, surfaced in `models list --json`.**
  It names the card or listing the licence was read from, which for most entries
  is not the repository the bytes come from. Verified against the publishers on
  2026-09-21 and pinned per entry by a test that also compares the entry count,
  so a model cannot be added without stating where its terms came from.

## [0.26.5] - 2026-09-21

### Security

- **A symlink planted at `<work_dir>/<workspace_id>` no longer redirects the
  whole workspace.** `os.Root` confines operations inside a root but resolves
  the root path itself normally, so if anything else with write access to your
  work directory (another tool, sandboxed code) left a link at the workspace's
  name, every read and write anchored on the link's target: images were written
  outside the directory you named and the call reported success. The workspace
  directory is now created through an `os.Root` on `work_dir` and then verified
  by real path; a workspace whose name resolves elsewhere is refused with
  `path_not_allowed`, naming the id and what it resolved to.
- **A symlink planted at the engine's temp output path is no longer written
  through.** `generate` and `upscale` hand the diffusion engine an absolute
  path (`output/<name>.tmp.png`), which the engine opens with plain
  `os.Create` — following any link left there and writing the render outside
  the workspace. The tool then failed while reading the result back, so the
  out-of-workspace write had already happened silently. That name is now
  unlinked through the containment root before the path is handed out, so the
  engine always creates the file fresh.

## [0.26.4] - 2026-09-14

### Added

- `TestEveryRequiredNameIsDeclared` — a schema that lists a name in `required`
  without declaring it in `properties` makes a strict client refuse the whole
  tool list (Vertex AI: "schema at top-level requires unspecified property").
  data-toolbox-mcp shipped exactly that and broke a session outright; the
  existing contract test checked declared ⇒ required only, so the fleet is
  pinned in both directions now.

## [0.26.3] - 2026-09-14

### Fixed

- **`input_not_found` named nothing.** "input %q is not in the workspace —
  place it there first" does not say where the workspace is, and it reaches the
  model on every `init`, `mask`, `control` and `upscale` input. It now names the
  absolute path it looked at. (Found on voice-scribe with a real agent, which
  answered that sentence by inventing a directory and spent four rounds
  recovering; these servers share the workspace code.)

## [0.26.2] - 2026-09-14

### Fixed

- **The initialize `instructions` field never mentioned `work_dir`.** It is the
  first thing the model reads about this server — before any tool list — and it
  still described "a workspace directory you prepare" while every tool required
  an argument it did not name. It now states the contract: `work_dir` is the
  absolute path of a directory you can read back, required, with no default.

### Added

- `TestInstructionsNameTheWorkDirContract` — the schema and description tests
  walked `tools/list`; nothing walked what `initialize` returns (ADR-0009).

## [0.26.1] - 2026-09-13

### Fixed

- A stale comment still described `MCPConfig` as falling back to a built-in
  default workspace root; 0.26.0 removed both the key and the default.
- **Both READMEs still showed `image-forge mcp [--workspace-root <dir>]`** — a
  flag 0.26.0 deleted — and the paragraph under it had been left half-rewritten
  in either language.

### Added

- `TestNoToolDescriptionNamesARetiredWorkDirName` — the schema test pinned the
  argument name, nothing pinned the sentences beside it.

## [0.26.0] - 2026-09-13

### Changed

- **Breaking: `workspace_root` is now `work_dir`, required by `generate` and
  `upscale`.** It means the absolute path of a directory the caller can read
  back; the workspace is `<work_dir>/<workspace_id>/`. A call still sending
  `workspace_root` (or `workspaceRoot` / `workspace_dir`) is refused with
  `work_dir_required` naming the replacement. See
  [ADR-0009](docs/en/adr/0009-work-dir-contract.md); organization ADR-021.
- **Breaking: the default workspace root is gone, and so are the ways to
  configure one** — the `--workspace-root` flag and the `[mcp] workspace_root`
  config key. All three were places an operator named a directory the caller may
  not be able to read, which turned a successful render into a path that cannot
  be opened.
- A runtime may supply the directory instead of the model: the server reads
  `_meta["jp.nlink/work_dir"]` when the argument is absent. The argument wins.
- The model store (`~/.local/share/image-forge`) is refused as a work directory,
  along with system locations, the home directory itself, and credential
  directories.

### Added

- `work_dir_required`, `work_dir_invalid`, `work_dir_not_found`,
  `work_dir_not_writable`, `work_dir_denied` — five codes that say which part of
  the contract failed.

## [0.25.1] - 2026-08-31

### Changed

- The `workspace_root` argument now says plainly that the caller should pass a
  root it can read back: every result is returned as a path under that root, so
  a workspace the caller cannot open leaves it holding a path to nothing. Text
  only — the behaviour is unchanged.

## [0.25.0] - 2026-08-02

### Added
- **`models relocate`** — re-points the registry after you move the model files to
  another disk. `config.toml`'s `models_dir` only redirects *new* pulls, so editing
  it left every installed model recorded at its old absolute path. Dry-run by
  default; `--apply` writes, `--to DIR` targets another directory (and is the undo).
  A recorded path is rewritten only when it is gone **and** the same filename exists
  in the target dir — a path that still resolves is never touched, and a missing
  file with no match is reported rather than guessed at. See ADR-0008.
- **Missing weight files are reported** — `models list` gains a `STATUS` column
  (`MISSING`) plus a footer naming the absent files, and `models list --json` /
  the MCP `list_models` tool gain `missing_files`. Previously every listing built
  its rows from the registry without stat'ing anything, so a model whose weights
  had moved was shown as healthy and only failed at generation time.

### Fixed
- **`gen` / `serve` / `mcp` now fail early and clearly** for an installed model
  whose weight files are absent, naming the files, the current models dir, and the
  fix (`models relocate --apply`, or mounting the volume) — instead of surfacing a
  low-level engine error about a stale path.

## [0.24.0] - 2026-07-14

### Added
- **`models pull` can tag a non-catalog model** — `--kind lora|controlnet|upscaler`,
  `--arch`, and `--trigger` now work on `pull`, matching `import`. Pulling a raw
  `hf:`/`civitai:`/URL ref used to always register it as a base diffusion model, so
  a LoRA fetched this way was mis-typed (wrong `Kind`, a full diffusion profile, no
  arch, no trigger words) and `--lora <name>` couldn't resolve it. You can now pull
  and correctly type it in one step:
  `models pull hf:owner/repo/lora.safetensors --kind lora --arch sdxl --trigger "…"`.
  Catalog names remain authoritative — the overrides are ignored (with a note) for
  them. Defaults are unchanged: a bare non-catalog pull is still a base diffusion
  model with auto-detected arch. See ADR-0007 (supplements ADR-0006).

## [0.23.1] - 2026-07-12

### Changed
- **`LICENSE` is now bundled** in the release archive alongside `README.md`,
  per `nlink-jp/.github` CONVENTIONS.md §Release Archive Standard. The archive
  name (`image-forge-vX.Y.Z-darwin-arm64.zip`) and the canonical in-archive
  binary name (`image-forge`) were already compliant. image-forge is CGO +
  Metal, so it ships **darwin/arm64 only** by design (no Intel / universal /
  Linux / Windows target) — unchanged.
- **darwin code-signature identifier** is now explicitly pinned to the
  canonical `image-forge` via `codesign -i` (re-vendored
  `scripts/codesign-darwin.sh` from the org template).

No change to the binary's behaviour — a packaging / build-config release.

## [0.23.0] - 2026-07-12

### Added
- **Open a model's web page** — `image-forge models open <name>` opens the model's
  source page (Civitai model page or Hugging Face repo) in the browser, so the model
  card is one command away instead of a search (`--print` writes the URL instead).
  The same URL is surfaced as `page_url` in `models list --json` (and the MCP
  `list_models` tool), which a front-end reads for an "open model page" link. Derived
  purely from the catalog `Source` — Civitai's `model-versions/<id>` URL 308-redirects
  to the canonical model page, so the version id we already store is enough (no model
  id, no API call).

## [0.22.0] - 2026-07-12

### Added
- **Multi-component entries can source a component from Civitai** — a component
  field (typically `DiffusionModel`) now accepts a `civitai:<versionId>` ref,
  resolved via the Civitai API at pull time (needs `CIVITAI_TOKEN`) while the other
  components stay Hugging Face refs. This pairs a Civitai-hosted DiT with shared
  HF-hosted encoders/VAE.
- **Catalog: two Anima-based anime checkpoints** — `anima-yume` (AnimaYume,
  questionable) and `nova-anime-am` (Nova Anime AM, explicit). Both are Civitai
  DiTs paired with the shared `circlestone-labs/Anima` Qwen3-0.6B encoder + Qwen-Image
  VAE (need `CIVITAI_TOKEN`). Unlike `anima-turbo` these "base" checkpoints are NOT
  guidance-distilled, so they set a CFG 5 / 24-step override (the arch turbo default of
  CFG 1 / 10 steps renders washed-out). Verified with a real pull + render.
- **Catalog: two Illustrious-based SDXL checkpoints** — `akium-ijin` (Akium IJIN,
  anime / 2.5D semi-real, questionable) and `akium-lumen` (Akium Lumen ILL base,
  anime, explicit). Both are Civitai-sourced (need `CIVITAI_TOKEN`), CLIP-skip 2,
  images non-commercial (rent-only) with derivatives allowed. Verified with a real
  pull + render.

## [0.21.0] - 2026-07-12

### Added
- **Load-time weight quantization** (#11): `[performance] wtype = "q4_k"` (or `gen
  --wtype q4_k`) quantizes an f16 checkpoint's weights as they load — fitting a
  bigger model into 16/32 GB without a pre-converted GGUF on disk or a second copy.
  Any `models quantize` type works (`q8_0` … `q4_0`; `f16`/`f32` keep full
  precision); empty keeps the original weights. Maps to `sd_ctx_params.wtype` in
  `Open()`; unknown types are rejected up front (`gen`/`serve`/`mcp`). It's part of
  the model's reload identity, so changing it reloads (unlike flash attention).

## [0.20.0] - 2026-07-11

### Added
- **Flux / SD3.5 guidance controls** (#10): `--guidance` (Flux distilled guidance —
  the key knob for `flux1-dev`, default 3.5), `--flow-shift` (flow-matching timestep
  shift), `--slg-scale` (skip-layer guidance for DiT models; ~2.5 for SD3.5, skips
  layers 7–9), and `--img-cfg` (separate image CFG for img2img). Previously `Render`
  only set `txt_cfg`, so these flow-matching knobs were unreachable. Each is 0/absent
  = keep sd.cpp's default, and irrelevant knobs are ignored per arch. Wired through
  `gen`, `serve` (JSON `guidance`/`flow_shift`/`slg_scale`/`img_cfg`), and the MCP
  `generate` tool.

## [0.19.0] - 2026-07-11

### Added
- **Catalog: FLUX.1-dev and SD3.5-Large** (#12), rounding out the large-model tier.
  Both are multi-component GGUF (Q4) and reuse the FLUX/SD3.5 text encoders already
  shared by `flux1-schnell` / `sd35-medium`, so they add only the diffusion GGUF.
  `flux1-dev` carries the **non-commercial** license flag (weights are
  non-commercial; outputs may be used commercially) and, since it is not
  guidance-distilled like schnell, uses a per-entry **step override** (~20 steps;
  sd.cpp's `distilled_guidance` default of 3.5 is already correct). `sd35-large`
  carries the Stability attribution flag. Both verified with a real pull + render.
  (Qwen-Image needs a new architecture in the profile system and is tracked
  separately.)
- Catalog entries can now override the arch-default **`Steps` / `CFG`** (used by
  `flux1-dev`).

## [0.18.1] - 2026-07-11

### Changed
- **Destructive deletes now require an interactive confirmation (HITL).** `models
  gc --force` and `models rm --purge` list the exact files and total size, then
  require typing `yes` at a terminal before deleting anything. When stdin is not a
  TTY (a script, a pipe, a test run), they refuse and delete nothing — making it
  impossible for automation to wipe a models directory, and closing a foot-gun
  where `gc --force` run against the wrong directory (e.g. a mis-scoped test) could
  delete real model files. The one escape hatch is `--confirmed-by-frontend`, for a
  trusted front-end (the GUI) that has already confirmed with the user via its own
  dialog — not for scripts. The confirmer is injected, so the delete logic stays
  unit-testable without ever touching a real terminal or real files; regression
  tests assert the real `gc --force` / `rm --purge` paths delete nothing without a
  TTY (and that `--confirmed-by-frontend` deletes as intended).

## [0.18.0] - 2026-07-11

### Added
- **Flash attention (opt-in)** — `[performance] flash_attn = true` (or `gen
  --flash-attn`) enables sd.cpp's flash attention for the diffusion model and text
  encoders (#5). **Off by default.** Measured on M2 Max / Metal it is neutral at
  native resolution and a modest win only on **large / hires** renders (~8% faster,
  ~120 MB less peak RSS at 1536²), and it changes outputs slightly
  (numerically-equivalent attention, not bit-identical) — so it is opt-in to keep
  same-seed outputs stable. Enable it for large / hires work.
- **Tiled VAE decoding (opt-in)** — `[performance] vae_tiling = true` (or `gen
  --vae-tiling`) decodes the final latent in overlapping 256px tiles instead of one
  pass (#6). **Off by default.** It caps VAE-decode memory so high-resolution /
  hires renders that would otherwise OOM the VAE — the usual failure point on the
  16 GB baseline — can finish. It costs a little speed and adds near-invisible tile
  seams (0.5 overlap), so it is opt-in; native-resolution output is unaffected while
  it stays off. Turn it on if a high-res render dies during VAE decode. Wired
  through `gen`, `serve`, and `mcp`.
- **`models gc` and `models rm --purge`** reclaim disk (#4). Models are multi-GB, so
  `rm` alone (which only forgets the registry entry) leaves the files behind. `rm
  <name> --purge` now also deletes the model's weight files — while keeping any file
  another installed model still shares (a common VAE / text encoder) and any file
  outside the managed models dir (imported in place). `models gc` reclaims files in
  the models dir that no installed model references (leftover `.part` downloads,
  files orphaned by a plain `rm`); it reports what it would free and only deletes
  with `--force`.

## [0.17.1] - 2026-07-11

### Fixed
- **`--batch N` now records each image's real seed** (#1). sd.cpp uses `base+b` for
  the b-th batch image, but image-forge reported the base seed for all of them and
  reused one metadata blob — so images 2..N carried the wrong seed in the `done`
  event and the PNG metadata and couldn't be reproduced. Metadata is now built per
  image with its own seed (`base+i`), across `gen --batch`, `serve`, and `mcp`.
- **Cancelling a render actually stops it** (#2). `Render` ignored its `ctx`, so an
  in-flight `generate_image` ran to completion; only queued MCP jobs were dropped.
  A watcher now calls sd.cpp's `sd_cancel_generation` when `ctx` is cancelled, so
  the render aborts. `serve` gained SIGINT/SIGTERM handling (aborts the in-flight
  render, then exits), and the MCP job manager's documented cancel-on-shutdown is
  now real. Verified: a SIGINT mid-render exits in ~5 s instead of running the full
  render, with no output written.
- **Unknown sampler / scheduler names are rejected** (#3). A typo like
  `--sampler eluer_a` used to hit sd.cpp's out-of-range enum and silently produce
  bad output; it now errors with the full list of valid names (reflected from
  sd.cpp, so it never drifts). Applies to `gen`, `serve`, and `mcp`.
- **`serve` error events now carry the failing request's `output`**, so a
  front-end can free the exact in-flight entry (an error otherwise has no key to
  remove it by). Supports the GUI's in-flight bookkeeping fix.

## [0.17.0] - 2026-07-11

### Added
- **SDXL ControlNet now works** — catalog entry **`controlnet-canny-sdxl`** (xinsir,
  Apache-2.0). The vendored **stable-diffusion.cpp was updated** to include upstream
  [#1752](https://github.com/leejet/stable-diffusion.cpp/pull/1752), which converts
  diffusers-format ControlNet tensor names on load and sizes the ControlNet graph for
  SDXL's deep transformers — so the diffusers file loads **directly, no pre-conversion**.
  Verified end-to-end (an SDXL base + canny control preserves the control image's
  geometry under a completely different prompt). This supersedes the v0.16.0 "No SDXL
  ControlNet yet" note.

### Fixed
- **Pulled model files are named after the registry name.** Previously a pull saved the
  file under its upstream basename, so models that share a generic name — the two LCM
  LoRAs (`pytorch_lora_weights.safetensors`) or a diffusers ControlNet
  (`diffusion_pytorch_model.safetensors`) — collided in the models dir and silently
  reused each other's bytes. Now each pull writes `<registry-name><ext>`. Existing
  installs keep their recorded paths; only new pulls are affected.

## [0.16.0] - 2026-07-11

### Added
- **First ControlNet catalog entry: `controlnet-canny-sd15`.** A canny-edge
  ControlNet for SD1.5 (`comfyanonymous/ControlNet-v1-1_fp16_safetensors`, OpenRAIL),
  now pullable with `models pull controlnet-canny-sd15` and usable via
  `gen -m <sd15> --control-net controlnet-canny-sd15 --control <img> --canny`.
  **Verified end-to-end** — the output follows the control image's edges.
- **LoRA and ControlNet in the MCP `generate` tool.** The tool now accepts `loras`
  (an array of `"<name-or-path>:<weight>"`), `control_net` (an installed ControlNet
  name or path), `control` (a workspace-relative control image, verified inside the
  workspace like `init`/`mask`), `control_strength`, and `canny` — the same
  capabilities `gen`/`serve` already had. Name→path resolution is handled downstream
  by the shared `buildRender`, so the MCP path adds no new resolution logic. Verified
  end-to-end (a LoRA render with combined `credit` metadata, and a ControlNet render
  that follows the control edges).

### Notes
- **No SDXL ControlNet yet.** sd.cpp's loader only understands the original
  ControlNet format (`control_model.` / `input_blocks.` keys); every public SDXL
  canny ControlNet (xinsir, lllyasviel/sd_control_collection) ships in *diffusers*
  format (`down_blocks` / `controlnet_down_blocks`), which sd.cpp cannot convert and
  fails to load. An SDXL entry will be added only once one actually renders.

## [0.15.1] - 2026-07-11

### Fixed
- **`z-image-turbo`'s license was mislabeled** `review-license` ("source repo
  declares none"). The weights are **Apache-2.0** — the upstream
  [`Tongyi-MAI/Z-Image`](https://github.com/Tongyi-MAI/Z-Image) repo and the
  `Tongyi-MAI/Z-Image-Turbo` model card both declare it. Reclassified as
  permissive (no flags), so it's no longer needlessly flagged for review.

## [0.15.0] - 2026-07-11

### Added
- **License restriction flags across the whole catalog.** Every entry (base
  models, LoRAs, upscalers) can carry machine-readable `LicenseFlags`
  (`non-commercial` / `no-derivatives` / `attribution` / `share-alike` /
  `review-license`) alongside the free-text `License`, so a front-end can
  reliably highlight a model's notable terms instead of parsing prose. Exposed as
  `license_flags` in both `models list --json` views, and backfilled from the
  catalog for cataloged installed models (so entries installed before this — or
  corrected in the catalog since — report accurately without a re-pull).

  Flags are **derived, not guessed** — from each Civitai listing's
  `allowCommercialUse` / `allowDerivatives` / `allowNoCredit`, or the HF license.
  Notable base models: **`prefect-pony-xl`** (non-commercial + no-derivatives +
  attribution), **`momoiro-pony`** (non-commercial + attribution — no commercial
  use at all), **`akium-unmotivated`** (non-commercial), **`illustrious-xl-v1`/
  `v1.1`** and **`t-ponynai3-*`** (no-derivatives), **`noobai-xl-vpred`**
  (share-alike, Fair AI copyleft), **`sd35-medium`** / **`anima-turbo`**
  (attribution), **`z-image-turbo`** (review-license — its source repo declares
  none). Plus the LoRAs (`dmd2-sdxl-4step`, `genba-neko-*`,
  `s1-dramatic-lighting-*`). Permissive models (Apache / OpenRAIL / BSD) carry none.
- **Credit / attribution recorded in image metadata.** Every catalog entry whose
  license requires attribution now carries an `Attribution` string (the exact
  credit to give — e.g. `"Illustrious XL by ONOMAAI (Civitai)"`). At generation
  time the credits of every model that shaped the render (base model + LoRAs) are
  de-duplicated, joined, and written to the output PNG's image-forge metadata
  under a new `credit` field, so whoever shares the image has the attribution the
  license calls for. **Non-destructive** — nothing is burned into the pixels.
  Exposed as `attribution` in both `models list --json` views (backfilled from
  the catalog like the flags), so a front-end can show and copy it. Permissive
  renders write no `credit` field at all.

### Fixed
- **`animagine-xl-4`'s license was mislabeled** "Fair AI Public License 1.0-SD";
  its HF model card declares **OpenRAIL++**. Corrected (permissive, no flags).

## [0.14.0] - 2026-07-11

### Docs
- `adding-a-model` now documents the `lora` / `controlnet` registry kinds (they
  were added in v0.13.0 but the guide still only described `upscaler`), including
  that LoRA/ControlNet must set `Arch`, and how to verify a LoRA's format and
  effect before adding it.

### Added
- **LoRA trigger words are recorded and surfaced.** Many LoRAs only take effect
  when specific tokens appear in the prompt (Civitai's "trained words"); without
  them the LoRA loads without error and silently does nothing. Catalog entries now
  carry `TriggerWords`, they are copied onto the registry entry at install time,
  printed right after `pull` / `import`, and exposed as `trigger_words` in
  `models list --json` (both installed and catalog views) so a front-end can show
  or insert them. `models import <path> --kind lora --trigger "a,b"` sets them for
  a local file. Empty for LoRAs that need none (LCM, sliders).
- **New LoRA catalog entries** (all verified: kohya-format header, pulled, and
  rendered against the same seed without them to prove they actually do something):
  - **`sdxl-lightning-4step`** / **`sdxl-lightning-8step`** (ByteDance, OpenRAIL++)
    — few-step sampling, generally sharper than LCM. `--steps 4|8 --cfg 1 --sampler euler`.
  - **`dmd2-sdxl-4step`** (DMD2) — `--steps 4 --cfg 1 --sampler euler`.
    **Licensed CC BY-NC 4.0: non-commercial use only**, surfaced in `models list`.

  ByteDance's Hyper-SD was evaluated and **not** added: its `LICENSE.md` only
  covers the FLUX.1-dev derivatives and the repo declares no license for the SDXL
  LoRA, so its terms are unclear.
- `pull` / `import` now print the model's kind and architecture for auxiliary
  models (`installed "sdxl-lightning-8step" (lora, sdxl) -> …`).
- **New architecture: `anima`** — CircleStone Labs / Comfy Org's 2B anime model
  (sd.cpp `VERSION_ANIMA`). It is *not* an SDXL derivative. Like Z-Image it is
  **multi-component**: the DiT checkpoint holds only `model.diffusion_model.*`,
  and sd.cpp's `AnimaConditioner` additionally needs a **Qwen3-0.6B** text encoder
  (`text_encoders.llm`) and the **Qwen-Image VAE**. The single-file Civitai
  download does not load on its own. Catalog entry **`anima-turbo`** pulls all
  three from `circlestone-labs/Anima`; the profile applies the distilled defaults
  (CFG 1, 10 steps, sampler euler, no negative prompt). `--arch` accepts `anima`.

  `profile.Detect` had to be taught the difference between **`anima`** and
  **`animagine`** — an SDXL model whose name contains "anima". Without ordering the
  match, every Animagine checkpoint would have been misdetected as Anima and
  silently given CFG 1 / 10-step defaults. Guarded by a test.
- **Style / concept LoRA catalog entries** (SDXL family — Illustrious / NoobAI),
  each with its `TriggerWords`, and each verified by rendering with and without it
  at the same seed:
  - **`genba-neko-illustrious`** (現場猫風) — safe
  - **`mythic-fantasy-illustrious`**, **`s1-dramatic-lighting-illustrious`**,
    **`lighting-slider-illustrious`** — questionable
  - **`pov-on-couch-illustrious`**, **`ai-illust-ojisan-noobai`** — explicit
    (require `--allow-nsfw` or config `allow_nsfw`)

  Ratings mirror the Civitai listing's `nsfwLevel`. `genba-neko`'s listing forbids
  derivatives and requires credit; that is stated in its `License`.
- **The same six styles trained on the Anima base** (`*-anima`, for use with
  `anima-turbo`). Also verified with/without at the same seed. Notable: Anima LoRAs
  use a different key layout (`lora_unet_blocks_N_cross_attn_*` — Anima is a DiT,
  not a UNet) yet sd.cpp applies them fine; and effect strength differs by base
  (Anima's `s1-dramatic-lighting` is markedly more dramatic than the Illustrious
  one, and its `lighting-slider` darkens at positive weight where the Illustrious
  one brightens — the direction is base-dependent).

## [0.13.1] - 2026-07-09

### Fixed (privacy)
- **Embedded PNG metadata no longer contains filesystem paths.** Since v0.12.0
  every generated image carried absolute paths — `model_path`, `vae_path`,
  `loras: ["/abs/path.safetensors:1"]`, `img2img.init`, `controlnet.image`,
  `hires.model` — leaking the machine's layout and, via the home directory, **the
  user's name** (`/Users/alice/…`) to anyone the image was shared with, Civitai
  included. `upscale` (v0.12.1+) propagated them to upscaled images too.

  Models are now recorded as **identifiers**: the registry name when installed,
  else the file's base name. This is *better* for reproduction, since `-m` /
  `--lora` / `--control-net` resolve installed names — `"loras":
  ["lcm-lora-sdxl:1"]` is directly re-runnable. **Input images are not recorded at
  all**: `img2img` keeps only `strength`, `controlnet` only `strength` / `canny`
  (a file name can itself be personal, and A1111 records the denoising strength
  without naming the init image). `model_path` / `vae_path` are gone; `vae` holds
  an identifier. A regression test asserts neither chunk can contain `/Users`,
  `/Volumes`, or a model file extension. See ADR-0005.

  Already-generated images keep whatever they were written with; re-generate (or
  re-`upscale`, which now drops the old paths on carry-through) to clean them.

## [0.13.0] - 2026-07-09

### Added
- **LoRA and ControlNet are first-class registry model kinds** (ADR-0006). The
  registry previously understood only diffusion models and upscalers, so a LoRA
  or ControlNet could only be named by raw file path — nothing could acquire,
  enumerate, or arch-check them.
  - `models pull lcm-lora-sdxl` installs a LoRA like any other model. Catalog
    entries added for **`lcm-lora-sdxl`** and **`lcm-lora-sd15`** (Latent
    Consistency LoRAs: few-step sampling at ~4-8 steps, CFG ~1-2, sampler `lcm`).
  - `models import <path> --kind lora|controlnet|upscaler` registers a local file.
  - `models list --kind diffusion|lora|controlnet|upscaler` filters the listing
    (table and `--json`).
  - LoRA / ControlNet entries record the base **architecture** they were trained
    against (unlike upscalers, which are architecture-agnostic), so `models list
    --json` reports a usable `arch` and callers can reject incompatible pairings.
- **`--lora` and `--control-net` accept an installed registry name** as well as a
  path: `gen --lora lcm-lora-sdxl:1.0`. A value that looks like a path passes
  through unchanged (existing invocations keep working); a bare name that isn't
  installed, or one registered under a different kind, is a clear up-front error
  instead of a confusing failure inside sd.cpp. Resolution is shared by `gen`,
  `serve`, and the MCP render worker.

### Fixed
- **`--lora` no longer crashes the process.** sd.cpp's default `lora_apply_mode`
  (`auto`) merges the LoRA into the model parameters up front for non-quantized
  weights, and that merge path segfaults on UNet-only LoRAs (e.g. the SDXL
  LCM-LoRA). image-forge now pins `lora_apply_mode = at_runtime`, applying the
  LoRA during the forward pass. This affected any `gen --lora <path>` on an fp16
  model — and would have taken down the resident `serve` engine. Verified E2E:
  `--lora lcm-lora-sdxl:1.0 --steps 6 --cfg 1.5 --sampler lcm` now renders, and
  the LoRA is demonstrably applied (the same seed without it is a blurry mess).

### Notes
- Changing the **ControlNet** model reloads the base model (it is part of the
  engine's reload key); changing **LoRAs** does not — they are applied per render.
- ControlNet catalog entries are not included yet; use `models import --kind
  controlnet <path>` until they are verified against sd.cpp's expected format.

## [0.12.1] - 2026-07-09

### Changed
- **`upscale` now preserves the source image's generation metadata.** An upscaled
  PNG carries the original prompt / seed / parameters through (plus the source's
  AUTOMATIC1111 `parameters` chunk), with an `upscale` sub-record `{upscaler,
  factor, source}` noting how it was produced — instead of only a light
  `{upscaler, factor, source}` record. So an upscaled image stays self-describing
  and its provenance survives (e.g. a GUI gallery can still show the prompt after
  reload). When the source has no image-forge metadata, the light record is
  written as before (now nested under `upscale`). Extends ADR-0005.
- Added `engine.ReadPNGText` (a tEXt / iTXt reader, the inverse of the existing
  writer) so `upscale` can read the source PNG's chunks.

## [0.12.0] - 2026-07-07

### Added
- **Generation metadata embedded in the PNG** (self-describing images). Every
  generated PNG carries the prompt, parameters, and model as text chunks: an
  **AUTOMATIC1111-compatible `parameters` chunk** (Civitai / A1111 parse it) and a
  complete **`image-forge` JSON** chunk. Unicode prompts use `iTXt` (UTF-8) so they
  round-trip. On by default; disable with `gen --no-metadata` or config
  `[metadata] embed = false`. `serve` / the MCP `generate` tool honor the config;
  `upscale` embeds a light record. Verified E2E (round-trip English tEXt + Japanese
  iTXt, valid CRCs). See ADR-0005.

## [0.11.0] - 2026-07-07

### Added
- **Configurable model directory** — set `models_dir` in the config (or
  `$IMAGE_FORGE_MODELS_DIR`) to store the multi-GB model files on a bigger disk.
  Affects new pulls; already-installed models keep their registry paths, and the
  small `registry.json` stays in the data directory. Verified E2E.

### Docs
- `models list` documents that pulled ESRGAN upscalers appear (arch `upscaler`)
  and that `--json` carries a `kind` field.

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
