# Adding a model to the catalog

The curated catalog lives in [`internal/catalog/catalog.go`](../../internal/catalog/catalog.go)
as the list returned by `Default()`. Each entry lets a user run
`image-forge models pull <name>` and get the checkpoint **plus its per-model
gotchas** (CLIP-skip, dedicated VAE, prompt prefix, …) applied automatically via
the model profile. This guide is the checklist for adding one.

> The point of the catalog is to hide gotchas. If a model needs a non-obvious
> setting to produce good output, encode it in the entry — don't make the user
> discover it.

## 0. Before you start

- Work in the canonical checkout (`util-series/image-forge`), on `main`.
- You'll need the model's real source reference and, for Civitai, a
  `CIVITAI_TOKEN` to download during E2E.

## 1. Identify the source and get the exact reference

`Source` supports Hugging Face, Civitai, a direct URL, or a multi-component set.

### Hugging Face (single file) — preferred when available

Use a **file-qualified** ref: `owner/repo/file.safetensors`.

```sh
# List the repo's root-level .safetensors and check gating:
curl -s "https://huggingface.co/api/models/SG161222/RealVisXL_V5.0" \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); print("gated:",d.get("gated"),"license:",(d.get("cardData") or {}).get("license")); [print(s["rfilename"]) for s in d["siblings"] if s["rfilename"].endswith(".safetensors") and "/" not in s["rfilename"]]'
```

- **Gated repos**: `401` = no/invalid token; `403` = valid token but the license
  hasn't been accepted on the web. For `403`, find an **ungated mirror** (e.g.
  `camenduru/FLUX.1-dev/ae.safetensors`, `adamo1139/…-ungated/…`) rather than
  forcing users to click through a license.
- **Diffusers-format repos are NOT single-file pullable.** If the `.safetensors`
  live only under `unet/`, `text_encoder/`, `vae/` folders (no single root
  checkpoint), you cannot use a single `HF` ref — use a Civitai version id or the
  multi-component path instead. (Many `John6666/*` Civitai mirrors are
  diffusers-only.)
- **HF Xet storage**: some repos serve weights via Xet (the `resolve/main/…` URL
  302-redirects to `*.cdn.hf.co`). A full GET (following redirects) returns the
  real bytes — our downloader handles it. Don't be alarmed that a `Range: 0-0`
  probe returns a small manifest instead of one byte.

### Civitai — use the VERSION id, not the model id

`Source.Civitai` takes a **version id** (the number `models pull` resolves via the
API). The catalog URL usually only has the *model* id, so look up the version:

```sh
# model id 439889 -> latest version id + base model + primary file
curl -s "https://civitai.com/api/v1/models/439889" \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); v=d["modelVersions"][0]; print("version id:",v["id"],"| base:",v["baseModel"]); [print(f["name"],f.get("type")) for f in v["files"] if f.get("primary")]'
```

Or read `?modelVersionId=` from a version-specific URL. Downloads need
`CIVITAI_TOKEN` (env or config). Tokens are redacted from logs — never commit one.

## 2. Choose the profile fields

Start from the architecture defaults and override only the gotchas.

| Field | How to pick it |
| --- | --- |
| `Arch` | `ArchSDXL` / `ArchSD15` / `ArchSD35` / `ArchFlux` / `ArchZImage`. Pony & Illustrious are `ArchSDXL`. |
| `Prediction` | `PredEps` for almost everything; `PredVPred` for v-prediction models (e.g. NoobAI v-pred). |
| `Rating` | `RatingSafe` / `RatingQuestionable` / `RatingExplicit`. NSFW-capable anime/Pony → `Questionable`; NSFW-leaning → `Explicit`. `Questionable`/`Explicit` require `--allow-nsfw`. The flag is the gate; the rating is your honest signal. Judgment is left to the user. |
| `License` | The upstream license; append `(verify)` / "see Civitai listing" when a community merge's terms are unclear. |
| `MinRAMGB` / `RecRAMGB` | SDXL: `16` / `32`. SD1.5: `8` / `16`. FLUX/SD3.5-large: `16` (Q4) / `32`. |

### Per-model gotchas to encode

- **SDXL dedicated VAE** — always attach the fp16-fix VAE to SDXL entries to avoid
  the fp16 black-image (NaN) failure:
  `VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors"`.
- **CLIP-skip** — the SDXL arch default is **2** (anime-leaning). Anime/Pony/
  Illustrious: leave it (or set `ClipSkip: 2` explicitly). **Photorealistic SDXL**
  (RealVisXL, Juggernaut): override to `ClipSkip: 1`.
- **Pony score tags** — Pony-family models need the quality prefix, so hide it:
  `PromptPrefix: "score_9, score_8_up, score_7_up, score_6_up, score_5_up, score_4_up"`.
- **Multi-component** (FLUX / SD3.5 / Z-Image) — leave `HF`/`Civitai` empty and set
  `DiffusionModel` + the encoders (`ClipL` / `ClipG` / `T5XXL` / `LLM`) + `VAE`.
  **Use standard fp8 (`t5xxl_fp8_e4m3fn`), bf16, or GGUF only** — ComfyUI's
  `fp8_scaled` / `fp8_mixed` builds are NOT sd.cpp-compatible (they load blank or
  fail).

## 3. Add the entry

Add to the list in `Default()`, grouped with similar models. Example (Civitai
Pony) — see the real entries around `prefect-pony-xl` / `realvisxl-v5`:

```go
{
    Name: "prefect-pony-xl", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
    Rating: profile.RatingQuestionable, License: "Pony-derived; see Civitai listing",
    MinRAMGB: 16, RecRAMGB: 32,
    Source: Source{
        Civitai: "2114187", // https://civitai.com/models/439889 (v6)
        VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
    },
    ClipSkip:     2,
    PromptPrefix: "score_9, score_8_up, score_7_up, score_6_up, score_5_up, score_4_up",
    Notes:        "Prefect Pony XL v6 (Civitai): high-quality Pony SDXL. Needs CIVITAI_TOKEN.",
},
```

## 4. Add/update tests

Tests are mandatory. In [`internal/catalog/catalog_test.go`](../../internal/catalog/catalog_test.go):

- Add the name to the relevant existing table test (e.g. Pony score-prefix,
  Civitai version-id, photoreal clip-skip-1).
- The base invariants (non-empty name, unique, license present, prediction
  propagates) already cover every entry — keep them green.

## 5. Build & check

```sh
make build          # scaffold compiles
make test           # go test (third_party excluded)
make vet
./dist/image-forge models list --catalog   # your entry shows up
```

## 6. Verify E2E on the real engine (mandatory before release)

Catalog metadata being right is not enough — pull and render it:

```sh
make build-engine
image-forge models pull <name> [--allow-nsfw]     # downloads (or reuses an existing file)
image-forge gen -m <name> -p "…" -o /tmp/test.png
```

Then **open the PNG** and confirm it's a coherent image, not a black frame (VAE/
NaN) or pure noise (wrong prediction type). `models pull` reuses an already-present
checkpoint/VAE (`haveFile`), so re-verifying is cheap if you already have the file.

## 7. Ship it

- Update `CHANGELOG.md` (a single-model addition is a patch bump).
- READMEs don't enumerate models, so they usually need no change.
- Release per the org checklist: `chore: release vX.Y.Z` → tag → `make package`
  (sign + notarize) → `gh release` + upload zip → bump the umbrella submodule
  pointer → `check-org.sh`.

## Gotchas cheat-sheet

- Civitai `Source` wants the **version id**, not the model id.
- Gated HF: `401` = token; `403` = license-not-accepted → ungated mirror.
- Diffusers-layout HF repos aren't single-file pullable.
- ComfyUI `fp8_scaled` / `fp8_mixed` encoders/checkpoints are sd.cpp-incompatible.
- SDXL arch default CLIP-skip is 2 (anime); photorealistic → override to 1.
- SDXL fp16 needs the fp16-fix VAE (else black images).
- Pony-family needs the `score_*` prompt prefix.
- Black image → VAE/precision; pure noise → wrong `Prediction` (eps vs v).
