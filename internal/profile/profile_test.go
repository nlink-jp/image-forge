package profile

import (
	"strings"
	"testing"
)

func TestArchDefaults_SDXLHidesGotchas(t *testing.T) {
	// SDXL anime models need clip-skip 2 and 1024 native resolution — the
	// defaults must encode this so users never set it by hand.
	p := ArchDefaults(ArchSDXL)
	if p.ClipSkip != 2 {
		t.Errorf("SDXL clip-skip = %d, want 2", p.ClipSkip)
	}
	if p.Width != 1024 || p.Height != 1024 {
		t.Errorf("SDXL resolution = %dx%d, want 1024x1024", p.Width, p.Height)
	}
	if !p.NegativeOK {
		t.Error("SDXL should allow a negative prompt")
	}
}

func TestArchDefaults_FluxSchnellNoNegative(t *testing.T) {
	// Distilled FLUX takes cfg~1 and no negative prompt.
	p := ArchDefaults(ArchFlux)
	if p.NegativeOK {
		t.Error("FLUX (distilled) should not use a negative prompt")
	}
	if p.CFG > 1.5 {
		t.Errorf("FLUX cfg = %v, want ~1", p.CFG)
	}
}

func TestArchDefaults_Unknown(t *testing.T) {
	if got := ArchDefaults("nonsense").Arch; got != ArchUnknown {
		t.Errorf("unknown arch = %q, want %q", got, ArchUnknown)
	}
}

func TestDetect(t *testing.T) {
	cases := map[string]Arch{
		"animagine-xl-4.0":  ArchSDXL,
		"ponyDiffusionV6XL": ArchSDXL,
		"noobai-XL-Vpred":   ArchSDXL,
		"illustrious-xl-v1": ArchSDXL,
		"sd15-pruned":       ArchSD15,
		"v1-5-emaonly":      ArchSD15,
		"FLUX.1-schnell":    ArchFlux,
		"z-image-turbo":     ArchZImage,
		// Qwen-Image-2.1 under each spelling its files and repos use. None may
		// reach the SDXL default, and "2.1" must not be read as another family.
		"qwen-image-2.1":       ArchQwenImage21,
		"qwen_image_2.1-Q4_K":  ArchQwenImage21,
		"Qwen-Image-2.1-Turbo": ArchQwenImage21,
		"qwen_image_2.1_bf16":  ArchQwenImage21,
		"stable-diffusion-3.5": ArchSD35,
		"mystery-model":        ArchSDXL, // default is SDXL
	}
	for name, want := range cases {
		if got := Detect(name); got != want {
			t.Errorf("Detect(%q) = %q, want %q", name, got, want)
		}
	}
}

// Qwen-Image-2.1 is not distilled: unlike the other LLM-conditioned entries
// (Z-Image turbo, Anima turbo) it needs real CFG and takes a negative prompt.
// Inheriting a turbo default (CFG 1, no negative) would render it washed out.
func TestArchDefaults_QwenImage21(t *testing.T) {
	p := ArchDefaults(ArchQwenImage21)
	if p.Arch != ArchQwenImage21 {
		t.Fatalf("arch = %q", p.Arch)
	}
	if p.CFG != 6 || p.Steps != 40 || p.Sampler != "euler" {
		t.Errorf("cfg/steps/sampler = %v/%d/%q, want 6/40/euler", p.CFG, p.Steps, p.Sampler)
	}
	if !p.NegativeOK {
		t.Error("NegativeOK = false; this model runs true CFG and uses a negative prompt")
	}
	if p.Width%32 != 0 || p.Height%32 != 0 {
		t.Errorf("default size %dx%d is not a multiple of 32", p.Width, p.Height)
	}
}

// The limit is the one measured for Qwen-Image-2.1: 1280x1280 rendered
// correctly and must pass, 1536x1536 and 2048x2048 rendered broken and must be
// refused. Nothing else has a limit.
func TestCheckSize(t *testing.T) {
	cases := []struct {
		name   string
		arch   Arch
		w, h   int
		hires  float64
		refuse string // "" = accepted; else a fragment of the refusal
	}{
		{"default", ArchQwenImage21, 1024, 1024, 0, ""},
		{"verified 1280", ArchQwenImage21, 1280, 1280, 0, ""},
		{"largest square", ArchQwenImage21, 1440, 1440, 0, ""},
		{"wide, within the area", ArchQwenImage21, 1920, 1088, 0, ""},
		{"exactly the limit", ArchQwenImage21, 2048, 1024, 0, ""},
		{"one block over", ArchQwenImage21, 2048, 1056, 0, "2048x1056 is too large"},
		{"broken 1536", ArchQwenImage21, 1536, 1536, 0, "1536x1536 is too large"},
		{"broken 2048", ArchQwenImage21, 2048, 2048, 0, "1440x1440 fits"},
		{"hires within", ArchQwenImage21, 1024, 1024, 1.25, ""},
		{"hires over", ArchQwenImage21, 1024, 1024, 1.5, "hires.fix would render 1536x1536"},
		{"hires off ignores the scale", ArchQwenImage21, 1024, 1024, 0, ""},
		{"sdxl has no limit", ArchSDXL, 4096, 4096, 2, ""},
		{"an arch that is not a fact", "", 4096, 4096, 2, ""},
	}
	for _, c := range cases {
		err := CheckSize(c.arch, c.w, c.h, c.hires)
		switch {
		case c.refuse == "" && err != nil:
			t.Errorf("%s: refused: %v", c.name, err)
		case c.refuse != "" && (err == nil || !strings.Contains(err.Error(), c.refuse)):
			t.Errorf("%s: err = %v, want a refusal containing %q", c.name, err, c.refuse)
		}
	}
}

// The profile's own default size must never be one the check refuses.
func TestArchDefaultsPassCheckSize(t *testing.T) {
	for _, a := range []Arch{ArchSD15, ArchSDXL, ArchSD35, ArchFlux, ArchZImage, ArchAnima, ArchQwenImage21} {
		p := ArchDefaults(a)
		if err := CheckSize(a, p.Width, p.Height, 0); err != nil {
			t.Errorf("%s default %dx%d: %v", a, p.Width, p.Height, err)
		}
	}
}
