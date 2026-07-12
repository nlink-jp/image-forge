package catalog

import (
	"testing"

	"github.com/nlink-jp/image-forge/internal/profile"
)

func TestDefault_NonEmptyAndNamed(t *testing.T) {
	entries := Default()
	if len(entries) == 0 {
		t.Fatal("default catalog is empty")
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Name == "" {
			t.Error("entry with empty name")
		}
		if seen[e.Name] {
			t.Errorf("duplicate entry name %q", e.Name)
		}
		seen[e.Name] = true
		if e.License == "" {
			t.Errorf("%s: license must be surfaced", e.Name)
		}
	}
}

func TestNeedsOptIn(t *testing.T) {
	cases := map[profile.Rating]bool{
		profile.RatingSafe:         false,
		profile.RatingQuestionable: true,
		profile.RatingExplicit:     true,
	}
	for rating, want := range cases {
		if got := (Entry{Rating: rating}).NeedsOptIn(); got != want {
			t.Errorf("rating %q: NeedsOptIn = %v, want %v", rating, got, want)
		}
	}
}

func TestProfilePropagatesPrediction(t *testing.T) {
	// The per-model prediction type must flow into the built profile — this is
	// what makes v-prediction models (e.g. NoobAI) render correctly.
	for _, e := range Default() {
		if e.Prediction != "" && e.Profile().Prediction != e.Prediction {
			t.Errorf("%s: profile prediction = %q, want %q", e.Name, e.Profile().Prediction, e.Prediction)
		}
	}
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

func TestLargeModelTierEntries(t *testing.T) {
	// FLUX.1-dev: flux arch, non-commercial license flag, and — because it is NOT
	// guidance-distilled like schnell — a step override (~20, not schnell's 4).
	dev, ok := Find("flux1-dev")
	if !ok {
		t.Fatal("flux1-dev missing from the catalog")
	}
	if dev.Arch != profile.ArchFlux {
		t.Errorf("flux1-dev arch = %q, want flux", dev.Arch)
	}
	if !hasFlag(dev.LicenseFlags, LicenseNonCommercial) {
		t.Error("flux1-dev must carry the non-commercial license flag")
	}
	if got := dev.Profile().Steps; got != 20 {
		t.Errorf("flux1-dev profile steps = %d, want 20 (not distilled like schnell's 4)", got)
	}
	if dev.NeedsOptIn() {
		t.Error("flux1-dev is safe-rated; it should not require an NSFW opt-in")
	}

	// SD3.5-Large: sd35 arch, attribution flag + a credit line, and it inherits the
	// sd35 arch step default (28, no override needed).
	lg, ok := Find("sd35-large")
	if !ok {
		t.Fatal("sd35-large missing from the catalog")
	}
	if lg.Arch != profile.ArchSD35 {
		t.Errorf("sd35-large arch = %q, want sd35", lg.Arch)
	}
	if !hasFlag(lg.LicenseFlags, LicenseAttribution) {
		t.Error("sd35-large must carry the attribution license flag")
	}
	if lg.Attribution == "" {
		t.Error("sd35-large has the attribution flag but no credit text")
	}
}

func TestProfileStepsCFGOverride(t *testing.T) {
	// A per-entry Steps/CFG override replaces the arch default; zero leaves it.
	base := (Entry{Arch: profile.ArchFlux}).Profile()
	over := (Entry{Arch: profile.ArchFlux, Steps: 20, CFG: 3.5}).Profile()
	if base.Steps == 20 {
		t.Fatal("test assumes the flux arch default is not 20")
	}
	if over.Steps != 20 || over.CFG != 3.5 {
		t.Errorf("override not applied: steps=%d cfg=%v", over.Steps, over.CFG)
	}
	if (Entry{Arch: profile.ArchFlux}).Profile().Steps != base.Steps {
		t.Error("zero Steps should leave the arch default")
	}
}

func TestFind(t *testing.T) {
	if _, ok := Find("flux1-schnell"); !ok {
		t.Error("expected to find flux1-schnell")
	}
	if _, ok := Find("does-not-exist"); ok {
		t.Error("did not expect to find a bogus entry")
	}
}

func TestPonyEntriesCarryScorePrefix(t *testing.T) {
	// Pony-family SDXL models need the "score_*" quality tags to produce good
	// output; that gotcha is hidden in the catalog's PromptPrefix and must flow
	// into the built profile.
	for _, name := range []string{"t-ponynai3-v7", "t-ponynai3-v5.5", "momoiro-pony", "prefect-pony-xl"} {
		e, ok := Find(name)
		if !ok {
			t.Fatalf("expected to find %q", name)
		}
		if e.PromptPrefix == "" {
			t.Errorf("%s: expected a Pony score-tag PromptPrefix", name)
		}
		if e.Profile().PromptPrefix != e.PromptPrefix {
			t.Errorf("%s: PromptPrefix did not propagate into the profile", name)
		}
	}
}

func TestPhotorealEntriesUseClipSkip1(t *testing.T) {
	// Photorealistic SDXL models want clip-skip 1, overriding the anime-leaning
	// SDXL arch default of 2. Verify the override reaches the built profile.
	for _, name := range []string{"realvisxl-v5", "juggernaut-xl-v9"} {
		e, ok := Find(name)
		if !ok {
			t.Fatalf("expected to find %q", name)
		}
		if e.ClipSkip != 1 {
			t.Errorf("%s: ClipSkip = %d, want 1", name, e.ClipSkip)
		}
		if e.Profile().ClipSkip != 1 {
			t.Errorf("%s: profile ClipSkip = %d, want 1 (override did not propagate)", name, e.Profile().ClipSkip)
		}
		if e.Source.VAE == "" {
			t.Errorf("%s: SDXL entry should attach the fp16-fix VAE", name)
		}
	}
}

func TestUpscalerEntries(t *testing.T) {
	// The seed ESRGAN upscalers must be present, marked as upscaler kind, carry a
	// license, and have no diffusion VAE / prediction.
	want := map[string]string{
		"realesrgan-x4plus":   "schwgHao/RealESRGAN_x4plus/RealESRGAN_x4plus.pth",
		"realesrgan-x4-anime": "utnah/esrgan/RealESRGAN_x4plus_anime_6B.pth",
	}
	for name, hf := range want {
		e, ok := Find(name)
		if !ok {
			t.Fatalf("expected upscaler %q in the catalog", name)
		}
		if !e.IsUpscaler() {
			t.Errorf("%s: IsUpscaler() should be true (kind=%q)", name, e.Kind)
		}
		if e.Source.HF != hf {
			t.Errorf("%s: HF source = %q, want %q", name, e.Source.HF, hf)
		}
		if e.Source.VAE != "" {
			t.Errorf("%s: an upscaler must not carry a VAE", name)
		}
		if e.Prediction != "" {
			t.Errorf("%s: an upscaler must not carry a prediction type", name)
		}
		if e.Rating != profile.RatingSafe {
			t.Errorf("%s: upscaler should be rated safe", name)
		}
		if e.License == "" {
			t.Errorf("%s: upscaler must surface a license", name)
		}
	}
}

func TestControlNetEntries(t *testing.T) {
	// The verified canny ControlNets must be present, marked as controlnet kind,
	// arch-bound (unlike upscalers), and carry no diffusion VAE/prediction. Both
	// SD1.5 and SDXL now render (SDXL loads a diffusers-format file directly since
	// the sd.cpp update — upstream #1752).
	want := map[string]profile.Arch{
		"controlnet-canny-sd15": profile.ArchSD15,
		"controlnet-canny-sdxl": profile.ArchSDXL,
	}
	for name, arch := range want {
		e, ok := Find(name)
		if !ok {
			t.Errorf("expected %s in the catalog", name)
			continue
		}
		if !e.IsControlNet() {
			t.Errorf("%s: IsControlNet() should be true (kind=%q)", name, e.Kind)
		}
		if e.Arch != arch {
			t.Errorf("%s: arch = %q, want %q (ControlNet is arch-bound)", name, e.Arch, arch)
		}
		if e.Source.HF == "" {
			t.Errorf("%s: must have an HF source", name)
		}
		if e.Source.VAE != "" || e.Prediction != "" {
			t.Errorf("%s: a ControlNet must not carry a VAE or prediction type", name)
		}
		if e.License == "" {
			t.Errorf("%s: must surface a license", name)
		}
	}
}

func TestHiresDefaultsPropagate(t *testing.T) {
	// prefect-pony-xl ships hires on by default (its Civitai page recommends it);
	// the flag and values must flow into the built profile.
	e, ok := Find("prefect-pony-xl")
	if !ok {
		t.Fatal("expected prefect-pony-xl in the catalog")
	}
	if !e.HiresEnabled {
		t.Error("prefect-pony-xl should ship hires on by default")
	}
	p := e.Profile()
	if !p.HiresEnabled || p.HiresScale != 1.5 || p.HiresDenoise != 0.5 || p.HiresUpscaler != "latent" {
		t.Errorf("hires defaults did not propagate into the profile: %+v", p)
	}

	// A model without a hires recommendation must stay off by default.
	other, _ := Find("animagine-xl-4")
	if other.Profile().HiresEnabled {
		t.Error("animagine-xl-4 should not enable hires by default")
	}
}

func TestCivitaiEntriesUsePullableVersionIDs(t *testing.T) {
	// The Civitai-sourced entries must reference a version id (numeric), not a
	// model id, so `models pull <name>` resolves the download via the API.
	want := map[string]string{
		"illustrious-xl-v1.1": "1411690",
		"akium-unmotivated":   "3046291",
		"akium-ijin":          "3081528",
		"akium-lumen":         "2962026",
		"t-ponynai3-v7":       "1392706",
		"t-ponynai3-v5.5":     "593760",
		"momoiro-pony":        "425904",
		"prefect-pony-xl":     "2114187",
	}
	for name, id := range want {
		e, ok := Find(name)
		if !ok {
			t.Fatalf("expected to find %q", name)
		}
		if e.Source.Civitai != id {
			t.Errorf("%s: Civitai version id = %q, want %q", name, e.Source.Civitai, id)
		}
		if e.Arch != profile.ArchSDXL {
			t.Errorf("%s: arch = %q, want SDXL", name, e.Arch)
		}
		if e.Source.VAE == "" {
			t.Errorf("%s: SDXL entry should attach the fp16-fix VAE", name)
		}
	}
}

func TestAnimaCivitaiComponentEntries(t *testing.T) {
	// Anima "base" checkpoints sourced from Civitai: the DiT component is pulled
	// via a `civitai:<versionId>` ref (a Civitai component inside a multi-component
	// entry — resolved through download.CivitaiResolve at pull time), while the
	// encoder and VAE reuse the shared HF-hosted Anima files. Unlike anima-turbo
	// they are NOT guidance-distilled, so each carries a CFG/step override.
	want := map[string]string{
		"anima-yume":    "civitai:3065644",
		"nova-anime-am": "civitai:3086321",
	}
	for name, dit := range want {
		e, ok := Find(name)
		if !ok {
			t.Fatalf("expected to find %q", name)
		}
		if e.Arch != profile.ArchAnima {
			t.Errorf("%s: arch = %q, want anima", name, e.Arch)
		}
		if !e.IsMultiComponent() {
			t.Errorf("%s: should be multi-component (has a DiffusionModel)", name)
		}
		if e.Source.DiffusionModel != dit {
			t.Errorf("%s: DiffusionModel = %q, want %q", name, e.Source.DiffusionModel, dit)
		}
		// The shared Anima encoder + VAE must be attached, or the DiT won't load.
		if e.Source.LLM == "" || e.Source.VAE == "" {
			t.Errorf("%s: anima DiT needs the shared Qwen3 LLM and Qwen-Image VAE (LLM=%q VAE=%q)", name, e.Source.LLM, e.Source.VAE)
		}
		// Non-distilled: the arch turbo defaults (CFG 1 / 10 steps) render
		// incoherently, so the entry must override them.
		p := e.Profile()
		if p.CFG <= 1 {
			t.Errorf("%s: CFG override = %v, want > 1 (not distilled like anima-turbo)", name, p.CFG)
		}
		if p.Steps < 20 {
			t.Errorf("%s: Steps override = %d, want >= 20 (not distilled)", name, p.Steps)
		}
	}

	// anima-yume is questionable, nova-anime-am is explicit — both need the opt-in.
	for _, name := range []string{"anima-yume", "nova-anime-am"} {
		e, _ := Find(name)
		if !e.NeedsOptIn() {
			t.Errorf("%s: NSFW-capable Civitai anime model should require the opt-in", name)
		}
	}
}
