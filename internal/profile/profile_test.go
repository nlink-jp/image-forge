package profile

import "testing"

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
		"animagine-xl-4.0":     ArchSDXL,
		"ponyDiffusionV6XL":    ArchSDXL,
		"noobai-XL-Vpred":      ArchSDXL,
		"illustrious-xl-v1":    ArchSDXL,
		"sd15-pruned":          ArchSD15,
		"v1-5-emaonly":         ArchSD15,
		"FLUX.1-schnell":       ArchFlux,
		"z-image-turbo":        ArchZImage,
		"stable-diffusion-3.5": ArchSD35,
		"mystery-model":        ArchSDXL, // default is SDXL
	}
	for name, want := range cases {
		if got := Detect(name); got != want {
			t.Errorf("Detect(%q) = %q, want %q", name, got, want)
		}
	}
}
