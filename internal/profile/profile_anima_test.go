package profile

import "testing"

// "animagine" (an SDXL model) contains the substring "anima" (a distinct
// architecture). Detect must not confuse them — a misdetected Animagine would
// silently get Anima's turbo defaults (CFG 1, 10 steps) and produce garbage.
func TestDetectAnimaVsAnimagine(t *testing.T) {
	cases := map[string]Arch{
		"anima-turbo":             ArchAnima,
		"anima_turboV10":          ArchAnima,
		"Anima":                   ArchAnima,
		"anima-base-v1.0":         ArchAnima,
		"animagine-xl-4.0":        ArchSDXL,
		"animagine-xl-4":          ArchSDXL,
		"cagliostrolab/animagine": ArchSDXL,
		"illustriousXL11_v11":     ArchSDXL,
		"noobai-XL-Vpred":         ArchSDXL,
		"flux1-schnell":           ArchFlux,
		"z-image-turbo":           ArchZImage,
	}
	for name, want := range cases {
		if got := Detect(name); got != want {
			t.Errorf("Detect(%q) = %q, want %q", name, got, want)
		}
	}
}

// The Anima turbo release is distilled: CFG 1, ~8-12 steps, no negative prompt.
func TestArchDefaultsAnima(t *testing.T) {
	p := ArchDefaults(ArchAnima)
	if p.Arch != ArchAnima || p.CFG != 1 || p.Steps != 10 || p.Width != 1024 {
		t.Errorf("anima defaults = %+v", p)
	}
	if p.NegativeOK {
		t.Error("at CFG 1 a negative prompt has no effect; NegativeOK should be false")
	}
}
