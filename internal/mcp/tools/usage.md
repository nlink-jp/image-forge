# image-forge mcp — how to use this server

This server generates **images** locally with an embedded diffusion engine
(stable-diffusion.cpp, Apple Silicon / Metal). You (the agent) write the prompt
and choose the model; the server does the rendering. It is **file-mediated** and
**async**: tools return file **paths**, never image bytes, and `generate`
enqueues a background job you poll with `check_job`. The produced PNG is viewed
by the human user on this host.

Call `get_usage` once before your first generation.

## Workspace model (read this first)

All output lives in a workspace: `<workspace_root>/<workspace_id>/`

```
<init/mask images>   img2img / inpaint inputs   (you place these)
output/              rendered PNGs              (server-written)
```

- `workspace_id`: `[a-zA-Z0-9_-]{1,64}`, one per generation project.
- `workspace_root` (optional): an **absolute path to a directory you prepared** —
  create it with your own file tools wherever you may write, then pass the same
  value on the call. Omit it to use the server's default root
  (`~/.local/share/image-forge/mcp-workspaces`), which requires the server and
  you to share an unrestricted filesystem view.
- Input images (`init`, `mask`) are referenced by paths **relative to the
  workspace root** — place them in the workspace first.
- The server never reads or writes outside the workspace (kernel-enforced;
  symlinks inside the workspace that point outside fail with `path_not_allowed`).

## Tools

- `get_usage` — this manual.
- `list_models` — list models as JSON. `scope=installed` (default) are the
  models you can generate with right now; `scope=catalog` are curated models the
  **user** can pull with the CLI; `scope=all` shows both. Pick a `name` for
  `generate`'s `model`.
- `generate` — enqueue a render, returns `{job_id, state:"queued"}` immediately.
- `check_job` — poll a job by `job_id`.

## Generate parameters

Required: `workspace_id`, `prompt`.

- `model` — an installed model name (from `list_models`). If omitted, the
  server's configured `default_model` is used; if there is none, you get
  `model_required` — call `list_models` and pass one.
- `workspace_root` — absolute agent-prepared root (see above).
- `negative` — negative prompt.
- `seed` — integer; `-1` = random (the concrete seed is reported back).
- `steps`, `cfg`, `width`, `height`, `sampler`, `scheduler`, `clip_skip`,
  `batch` — override the model profile's defaults.
- `init` — img2img source, a workspace-relative image path.
- `mask` — inpaint mask, a workspace-relative image path; **requires `init`**
  (white = regenerate, black = keep).
- `strength` — img2img denoise strength `0..1` (with `init`).
- `output_name` — base name for the PNG (default `gen`); the final file is
  `output/<output_name>-<seed>.png`.

## Job lifecycle (async)

1. `generate` → `{job_id, state:"queued"}`. Renders are **serialized** (one at a
   time) — the engine is not concurrent-safe, so multiple `generate` calls queue.
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
| path_not_allowed | use workspace-relative input paths / a valid absolute workspace_root; symlinks out of the workspace are rejected |
| invalid_workspace_id | match [a-zA-Z0-9_-]{1,64} |
| invalid_arguments | fix the flagged argument (e.g. output_name must be a plain file name; mask requires init) |
| invalid_scope | list_models scope must be installed|catalog|all |
| render_failed | inspect the message; a bad init/mask image or bad parameter → fix it and retry |
| job_not_found | the server restarted (async jobs are in-memory); re-submit generate |
