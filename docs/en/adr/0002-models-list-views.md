# ADR-0002: Split `models list` into installed / catalog views with JSON output

- Status: Accepted
- Date: 2026-07-07

## Context

`models list` rendered a single table that merged two different things: the
curated catalog (each row tagged `available` / `installed` in a STATUS column)
and any installed models that were not in the catalog. Users found the combined
view hard to read — "what do I have" and "what can I get" are distinct questions,
and mixing catalog metadata (RAM tier, license) with install state (path) in one
table blurred both. There was also no machine-readable output for scripting.

Two shapes were considered for separating the views:

1. **Separate subcommands** — `models list` (installed) + `models catalog`.
2. **One command with filter flags** — `models list [--installed|--catalog|--all]`.

## Decision

**Adopt option 2: keep a single `models list` command with mode flags, plus a
`--json` flag on every mode.**

- `models list` (default) — **installed** models only: `NAME ARCH RATING LICENSE PATH`.
- `models list --catalog` — the curated catalog: `NAME ARCH RATING RAM LICENSE INSTALLED`.
- `models list --all` — both, as two clearly-labelled sections (`INSTALLED`, `CATALOG`).
- `--json` on any mode emits stable, purpose-built JSON (installed → array;
  catalog → array with an `installed` flag; `--all` → `{"installed":[…],"catalog":[…]}`).

The JSON is rendered from dedicated `installedView` / `catalogView` structs rather
than the internal `store.InstalledModel` / `catalog.Entry` types, so the output
contract is decoupled from internal refactors (the registry's nested `profile`
blob, for instance, is not leaked).

This is a **behaviour change**: `models list` no longer shows the catalog by
default. It is called out in the CHANGELOG. image-forge is pre-1.0 and freshly
released, so the churn is acceptable; `--all` preserves the everything-at-once
view for anyone who wants it.

## Consequences

- Clearer default output; the two questions have their own views.
- Scriptable via `--json` (aligns with the util-series `--json` convention).
- Mode resolution lives in a pure `resolveListMode` helper and is unit-tested;
  the view builders (`installedViews` / `catalogViews`) are tested independently
  of the terminal rendering.
- Keeping one command (vs. a new `catalog` subcommand) keeps the `models`
  surface small and matches the existing `pull` / `import` / `quantize` / `rm`
  verb set.
