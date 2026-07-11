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
	// The verified SD1.5 canny ControlNet must be present, marked as controlnet
	// kind, arch-bound (unlike upscalers), and carry no diffusion VAE/prediction.
	// (No SDXL ControlNet ships: sd.cpp cannot load diffusers-format SDXL weights.)
	e, ok := Find("controlnet-canny-sd15")
	if !ok {
		t.Fatal("expected controlnet-canny-sd15 in the catalog")
	}
	if !e.IsControlNet() {
		t.Errorf("controlnet-canny-sd15: IsControlNet() should be true (kind=%q)", e.Kind)
	}
	if e.Arch != profile.ArchSD15 {
		t.Errorf("controlnet-canny-sd15: arch = %q, want SD15 (ControlNet is arch-bound)", e.Arch)
	}
	if e.Source.HF == "" {
		t.Error("controlnet-canny-sd15: must have an HF source")
	}
	if e.Source.VAE != "" || e.Prediction != "" {
		t.Error("controlnet-canny-sd15: a ControlNet must not carry a VAE or prediction type")
	}
	if e.License == "" {
		t.Error("controlnet-canny-sd15: must surface a license")
	}
	// No SDXL ControlNet until one actually renders in sd.cpp.
	for _, cn := range Default() {
		if cn.IsControlNet() && cn.Arch == profile.ArchSDXL {
			t.Errorf("%s: an SDXL ControlNet was added — verify it renders in sd.cpp first (diffusers format does not load)", cn.Name)
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
