# ADR-0008: Reconcile the registry with a relocated `models_dir`

- Status: Accepted
- Date: 2026-08-02

## Context

`config.toml`'s `models_dir` relocates where **pulled model files are written**
(`store.SetModelsDir` at startup, read by `store.ModelsDir()`). The registry, by
contrast, records an **absolute path per weight file** — `path`, `vae_path`, and
each field of `components`. Those two facts are never reconciled.

So the ordinary way a user moves their models onto a bigger disk —

1. move `~/.local/share/image-forge/models/*` (or the old `models_dir`) to the new
   volume,
2. point `models_dir` at the new location,

— leaves every installed model's registry path aimed at a directory that no
longer exists. `config.go` documents this ("already-installed models keep the
absolute paths recorded in the registry"), but nothing acts on it.

The failure is silent in exactly the wrong place. `installedViews` builds its rows
straight from the registry and **never stats the files**, so `models list`,
`models list --json`, the MCP `list_models` tool, and the GUI's Manage Models
window all report the models as installed and healthy. `gen` / `serve` are the
first code to touch the filesystem, and they fail there with a load error naming
the stale directory. The user's observation is "the model manager sees my models
but generation says the old path" — the tool asserted a state it had not checked.

Two distinct defects:

- **No path reconciliation.** Nothing rewrites the registry when the models dir
  moves, and there is no command to ask for it.
- **No existence check.** Every listing surface presents unverified state as fact.

A third scenario shares the same shape and the same fix: a `models_dir` on an
external volume that simply isn't mounted. The files are not "gone", but the
listing must not claim they are there.

## Decision

**Add an explicit `models relocate` command, and make every listing surface
report missing weight files.** Reconciliation is a user-invoked, previewable
operation — not an implicit repair.

1. **`store` owns the file-field walk.** `InstalledModel` gains `fileRefs()`,
   returning a get/set accessor per recorded weight-file field (`path`,
   `vae_path`, `components.*`) with its field name. Every consumer that must
   enumerate or rewrite an installed model's files goes through it, so a new
   component field can never be missed by one caller and honored by another.
   Built on it:
   - `InstalledModel.MissingFiles(exists)` — the recorded files `exists` rejects.
   - `Registry.Relocate(dir, apply, exists)` — the plan/apply core.

   `exists` is injected (not `os.Stat`) so both are unit-testable against a
   synthetic filesystem, per the testability rule.

2. **Relocation rebases only what is broken, and only by basename.** A file is
   rewritten to `<dir>/<basename>` **iff** the recorded path does not exist **and**
   that candidate does. A path that still resolves is never touched — a user with
   models deliberately spread across directories keeps them. A missing file with
   no candidate in the target dir is reported as unresolved, never guessed at.

3. **Dry-run by default; `--apply` writes.** `models relocate` prints the plan and
   exits; `models relocate --apply` rewrites the registry. This mirrors
   `models gc`'s report-then-opt-in shape. The target is `store.ModelsDir()` (i.e.
   the configured `models_dir`); `--to <dir>` overrides it, which also makes the
   operation its own undo — re-run with `--to <old-dir>`.

4. **Listings report missing files.** `installedView` gains
   `missing_files []string` (omitted when empty), populated by stat'ing the
   registry paths. The `models list` table gains a **`STATUS`** column, blank for a
   healthy model and `MISSING` for a broken one, plus a footer naming the absent
   files and pointing at `models relocate`. Because `ListModels` is the single
   source for the CLI, `--json`, and MCP, all three gain it at once (ADR-0002's
   shared-view rule).

5. **The GUI surfaces it and refuses to render.** `ModelInfo` decodes
   `missing_files`; Manage Models marks such rows, and the Composer keeps a model
   with missing files out of the picker and blocks generation with an actionable
   message. A front-end that shows a model as available must be able to load it.

### Rejected: implicit rebasing at registry load

`store.Load()` could silently substitute `<models_dir>/<basename>` for any missing
path. It would make the config edit "just work" with no user action — but it
resolves by basename against a directory the user just changed, so an unrelated
same-named file would be loaded without a word, and the registry on disk would
keep disagreeing with what the process actually used. State that repairs itself
invisibly is harder to reason about than state that reports its own breakage.
Rejected in favor of an explicit command plus honest listings.

### Out of scope

- **Moving the files.** `relocate` rewrites the registry only; it never copies or
  deletes weights. Moving the bytes is the user's step (`mv`), and `--dry-run`
  before it will correctly report everything unresolved.
- **Watching for config changes.** No daemon reconciles on `config.toml` edits.
- **Per-model relocation.** The whole registry is reconciled at once; nothing so
  far suggests wanting one model repointed alone.

## Consequences

- Moving models onto another disk becomes: `mv` the files, edit `models_dir`, run
  `image-forge models relocate --apply`. Three explicit steps, each verifiable.
- A model whose weights are absent — relocated dir, unmounted volume, a file
  deleted outside image-forge — is reported as `MISSING` everywhere it is listed,
  instead of failing at load time with a stale path.
- `models list` does `O(files)` stats per invocation. On a missing/unmounted path
  these fail immediately (ENOENT), so the cost is bounded and the degenerate case
  is the fast one.
- `fileRefs()` gives `rm --purge`, `gc`, `MissingFiles`, and `Relocate` one
  definition of "the files this model owns"; `Files()` is now expressed in terms
  of it.
- Adding a weight-file field to `InstalledModel` requires updating `fileRefs()`
  and nothing else.
