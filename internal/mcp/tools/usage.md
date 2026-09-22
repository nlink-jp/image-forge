# image-forge mcp — how to use this server

This server generates **images** locally with an embedded diffusion engine
(stable-diffusion.cpp, Apple Silicon / Metal). You (the agent) write the prompt
and choose the model; the server does the rendering. It is **file-mediated** and
**async**: tools return file **paths**, never image bytes, and `generate`
enqueues a background job you poll with `check_job`. The produced PNG is viewed
by the human user on this host.

Call `get_usage` once before your first generation.

## Workspace model (read this first)

All output lives in a workspace: `<work_dir>/<workspace_id>/`

```
<input images>       img2img / inpaint / control inputs   (you place these)
output/              rendered PNGs                         (server-written)
```

- `workspace_id`: `[a-zA-Z0-9_-]{1,64}`, one per generation project.
- `work_dir` (**required**, every call): the **absolute path of a directory you
  can read back** — your session or working directory. Generated images come
  back as paths under it, never as bytes, so a directory you cannot open leaves
  you holding a path to nothing. There is no default: it must already exist, and
  nothing here expands `~` or resolves a relative path. Your runtime may supply
  it for you by setting `_meta["jp.nlink/work_dir"]` on the call; the argument
  always wins.
- Input images (`init`, `mask`, `control`) are referenced by paths **relative to
  the workspace** — place them in the workspace first.
- The server never reads or writes outside the workspace (kernel-enforced;
  symlinks inside the workspace that point outside fail with `path_not_allowed`),
  with one exception: the model files it loads — installed models, and a LoRA,
  ControlNet or hires model you name by a raw path (see those arguments).
  The workspace directory itself is checked too: if `<work_dir>/<workspace_id>`
  is a symlink rather than a real directory, the call is refused instead of
  silently working somewhere else.

## Tools

- `get_usage` — this manual.
- `list_models` — list models as JSON. `scope=installed` (default) are the
  models you can generate with right now; `scope=catalog` are curated models the
  **user** can pull with the CLI; `scope=all` shows both. Each entry has a
  `kind` (`""` = diffusion, `upscaler` = ESRGAN super-resolution). Pick a `name`
  for `generate`'s `model` (a diffusion one) or `upscale`'s `model` (an upscaler).
- `generate` — enqueue a render, returns `{job_id, state:"queued"}` immediately.
- `upscale` — enqueue a standalone ESRGAN super-resolution of an existing image,
  returns `{job_id, state:"queued"}` immediately.
- `check_job` — poll a job by `job_id`.

## Generate parameters

Required: `workspace_id`, `prompt`.

- `model` — an installed model name (from `list_models`). If omitted, the
  server's configured `default_model` is used; if there is none, you get
  `model_required` — call `list_models` and pass one.
- `work_dir` — the absolute directory you can read back (see above).
- `negative` — negative prompt.
- `seed` — integer; `-1` = random (the concrete seed is reported back).
- `steps`, `cfg`, `width`, `height`, `sampler`, `scheduler`, `clip_skip`,
  `batch` — override the model profile's defaults.
- `init` — img2img source, a workspace-relative image path.
- `mask` — inpaint mask, a workspace-relative image path; **requires `init`**
  (white = regenerate, black = keep).
- `strength` — img2img denoise strength `0..1` (with `init`).
- `loras` — an array of LoRAs to apply, each `"<installed-name-or-path>:<weight>"`
  (see `list_models`). Applied per render, no model reload. A LoRA's registry name
  resolves to its file. An installed LoRA or ControlNet made for another
  architecture than the model is refused before loading when both arches are
  facts — `list_models` marks those `arch_trusted: true`; a guessed arch is never
  compared. A raw path is read where it lies, so one in a credential or
  agent-control location (`~/.ssh`, `~/.aws`, `~/.config/gh`, … — the list
  gem-agent and lagent use, under any spelling, and wherever a link directly
  inside one of those directories points), in this server's config directory
  (`~/.config/image-forge`, which may hold tokens), or a `.env` file is refused
  with `path_not_allowed`, whether or not it exists. The same holds for
  `control_net` and `hires_model`. An installed name is never judged as a path:
  the registry resolves it to its own file.
- `control_net` — a ControlNet installed name or path (see
  `list_models` scope with `kind` `controlnet`). Loaded with the base model, so
  **changing it reloads the base**. Ships for SD1.5 (`controlnet-canny-sd15`) and
  SDXL (`controlnet-canny-sdxl`).
- `control` — the ControlNet control image, a workspace-relative image path;
  **requires `control_net`**. `control_strength` (`0..1`, default `0.9`) sets its
  influence; `canny` edge-preprocesses it (off = it is already an edge map).
- `output_name` — base name for the PNG (default `gen`); the final file is
  `output/<output_name>-<seed>.png`.
- `hires` — hires.fix, a second higher-res pass that adds detail: `auto`
  (default; follow the model profile — some models ship with it on), `on`, or
  `off`. Fine-grained: `hires_scale` (default profile or 1.5), `hires_denoise`
  (`0..1`, default profile or 0.5), `hires_upscaler`
  (`latent`|`lanczos`|`nearest`|`model`; default: the model profile, else the
  config's `[hires] upscaler`, whose default `auto` picks an ESRGAN — the
  configured `[upscaler] default_model`, or the only one installed — else
  latent), and `hires_model` (an installed upscaler name, or a raw path to an
  upscaler file, for `hires_upscaler=model`; without it the same pick applies, and when it finds
  none — no ESRGAN installed, or two or more and no `default_model` — the pass
  falls back to latent). hires roughly doubles render time
  and raises peak memory.

## Upscale parameters

Required: `workspace_id`, `input`.

- `input` — the image to upscale, a workspace-relative path (place it first).
- `model` — an installed **upscaler** name (`list_models` → `kind: upscaler`).
  Omit it only when exactly one upscaler is installed; otherwise you get
  `model_required`.
- `scale` — upscale factor (default: the model's native factor, typically 4).
- `output_name` — base name for the PNG (default `upscaled`); the final file is
  `output/<output_name>.png`.
- `work_dir` — the absolute directory you can read back (see above).

## Job lifecycle (async)

1. `generate` / `upscale` → `{job_id, state:"queued"}`. Jobs are **serialized**
   (one at a time) — the engine is not concurrent-safe, so calls queue.
2. `check_job {job_id}` → `state` is `queued` | `running` | `done` | `error`,
   with `progress` (`fraction` 0..1, `message`).
3. On `done`: `result.outputs` is a list of
   `{path (workspace-relative), abs_path (absolute), seed}`. Show `abs_path` to
   the user; reuse `path` for a follow-up img2img in the same workspace.
4. On `error`: `error` is a structured `{code, message, details}` — see below.

Jobs are in-memory only. After a server restart `check_job` returns
`job_not_found`; just re-submit `generate` (it re-renders from the same
workspace).

## Error recovery

| code | action |
|------|--------|
| model_required | no model given and no default_model configured; call list_models, pass a name as "model" |
| model_not_found | the named model is not installed; call list_models (scope=installed); the user pulls catalog models with the CLI |
| no_runtime | this build has no diffusion runtime (built without cgo_sdcpp); the user must install the engine build |
| input_not_found | place the referenced init/mask image in the workspace, then retry |
| path_not_allowed | use workspace-relative input paths; symlinks out of the workspace are rejected, as is a workspace directory that is itself a symlink, and a LoRA / ControlNet / hires model path in a credential or agent-control location |
| work_dir_required | no `work_dir` argument and no `_meta` hint — pass the absolute path of a directory you can read back |
| work_dir_invalid | not absolute, started with `~`, or contained `..` |
| work_dir_not_found | not there, or not a directory — it is yours, so this is a typo; the server does not create it |
| work_dir_not_writable | the server cannot write there |
| work_dir_denied | a system location, your home directory itself, a credential or agent-control location (or where a link directly inside one points), this server's own data, models or config directory — under any spelling — or the home directory cannot be determined; for `work_dir`, and for the workspace directory `<work_dir>/<workspace_id>` it would use. `details.reason` says which: `system_dir`, `home_dir`, `sensitive_path`, `server_dir`, `home_unknown`, `unconfigured`, `unresolvable_path` |
| invalid_workspace_id | match [a-zA-Z0-9_-]{1,64} |
| invalid_arguments | fix the flagged argument (e.g. output_name must be a plain file name; mask requires init) |
| invalid_scope | list_models scope must be installed|catalog|all |
| render_failed | inspect the message; a bad init/mask image or bad parameter → fix it and retry |
| job_not_found | the server restarted (async jobs are in-memory); re-submit generate |
