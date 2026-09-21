package cli

import (
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

// fakeRegistry builds a lookup func over the given installed models.
func fakeRegistry(models ...store.InstalledModel) func(string) (store.InstalledModel, bool) {
	byName := map[string]store.InstalledModel{}
	for _, m := range models {
		byName[m.Name] = m
	}
	return func(n string) (store.InstalledModel, bool) {
		m, ok := byName[n]
		return m, ok
	}
}

func TestResolveAuxModel(t *testing.T) {
	get := fakeRegistry(
		store.InstalledModel{Name: "lcm-lora-sdxl", Kind: catalog.KindLoRA, Path: "/models/lcm.safetensors"},
		store.InstalledModel{Name: "canny-sdxl", Kind: catalog.KindControlNet, Path: "/models/canny.safetensors"},
		store.InstalledModel{Name: "animagine-xl-4", Kind: catalog.KindDiffusion, Path: "/models/anim.safetensors"},
	)

	// A registry name of the right kind resolves to its installed path.
	got, err := resolveAuxModel("lcm-lora-sdxl", catalog.KindLoRA, "", get)
	if err != nil || got != "/models/lcm.safetensors" {
		t.Errorf("lora by name = %q, %v", got, err)
	}
	got, err = resolveAuxModel("canny-sdxl", catalog.KindControlNet, "", get)
	if err != nil || got != "/models/canny.safetensors" {
		t.Errorf("controlnet by name = %q, %v", got, err)
	}

	// An empty ref stays empty (feature not requested).
	if got, err := resolveAuxModel("", catalog.KindLoRA, "", get); err != nil || got != "" {
		t.Errorf("empty ref = %q, %v", got, err)
	}
}

func TestResolveAuxModelPathPassthrough(t *testing.T) {
	get := fakeRegistry()
	// Values that look like paths pass through unchanged (back-compat).
	for _, p := range []string{"/abs/path/x.safetensors", "rel/dir/y.pth", "z.safetensors"} {
		got, err := resolveAuxModel(p, catalog.KindLoRA, "", get)
		if err != nil || got != p {
			t.Errorf("path %q => %q, %v; want passthrough", p, got, err)
		}
	}
}

func TestResolveAuxModelErrors(t *testing.T) {
	get := fakeRegistry(
		store.InstalledModel{Name: "animagine-xl-4", Kind: catalog.KindDiffusion, Path: "/models/anim.safetensors"},
	)

	// A bare name that isn't installed is a clear error, not a bogus path.
	_, err := resolveAuxModel("no-such-lora", catalog.KindLoRA, "", get)
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("missing lora err = %v, want 'not installed'", err)
	}

	// A name registered under the wrong kind is rejected.
	_, err = resolveAuxModel("animagine-xl-4", catalog.KindLoRA, "", get)
	if err == nil || !strings.Contains(err.Error(), "not a LoRA") {
		t.Errorf("wrong-kind err = %v, want 'not a LoRA'", err)
	}
}

// auxProfile is the single kind→profile mapping shared by `models import` and
// `models pull` (ADR-0007). It must reproduce ADR-0006's rule: base models get
// full render defaults, upscalers are name-only, LoRA/ControlNet carry arch only.
func TestAuxProfile(t *testing.T) {
	// Diffusion: full architecture defaults, with the name stamped on.
	d := auxProfile(catalog.KindDiffusion, "my-base", profile.ArchSDXL)
	if d.Name != "my-base" || d.Arch != profile.ArchSDXL {
		t.Errorf("diffusion name/arch = %q/%q", d.Name, d.Arch)
	}
	if d.Steps == 0 || d.CFG == 0 || d.Sampler == "" {
		t.Errorf("diffusion should carry render defaults, got %+v", d)
	}

	// Upscaler: architecture-agnostic, name only — no arch, no render fields.
	u := auxProfile(catalog.KindUpscaler, "esrgan-x4", profile.ArchSDXL)
	if u.Name != "esrgan-x4" || u.Arch != "" {
		t.Errorf("upscaler = %+v, want name-only with no arch", u)
	}
	if u.Steps != 0 || u.CFG != 0 || u.Sampler != "" {
		t.Errorf("upscaler should carry no render defaults, got %+v", u)
	}

	// LoRA / ControlNet: bound to a base arch, but nothing else (ADR-0006).
	for _, k := range []string{catalog.KindLoRA, catalog.KindControlNet} {
		p := auxProfile(k, "aux", profile.ArchSD15)
		if p.Name != "aux" || p.Arch != profile.ArchSD15 {
			t.Errorf("%s name/arch = %q/%q", k, p.Name, p.Arch)
		}
		if p.Steps != 0 || p.CFG != 0 || p.Sampler != "" || p.ClipSkip != 0 || p.Prediction != "" {
			t.Errorf("%s should carry arch only, got %+v", k, p)
		}
	}
}

func TestNormalizeKind(t *testing.T) {
	cases := map[string]string{
		"":            catalog.KindDiffusion,
		"diffusion":   catalog.KindDiffusion,
		"lora":        catalog.KindLoRA,
		"LoRA":        catalog.KindLoRA,
		"controlnet":  catalog.KindControlNet,
		"control-net": catalog.KindControlNet,
		"control_net": catalog.KindControlNet,
		"upscaler":    catalog.KindUpscaler,
	}
	for in, want := range cases {
		got, err := normalizeKind(in)
		if err != nil || got != want {
			t.Errorf("normalizeKind(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := normalizeKind("bogus"); err == nil {
		t.Error("normalizeKind(bogus) should error")
	}
}

func TestFilterByKind(t *testing.T) {
	views := []installedView{
		{Name: "a", Kind: catalog.KindDiffusion},
		{Name: "b", Kind: catalog.KindLoRA},
		{Name: "c", Kind: catalog.KindLoRA},
		{Name: "d", Kind: catalog.KindUpscaler},
	}
	kindOf := func(v installedView) string { return v.Kind }

	loras := filterByKind(views, catalog.KindLoRA, kindOf)
	if len(loras) != 2 || loras[0].Name != "b" || loras[1].Name != "c" {
		t.Errorf("lora filter = %+v", loras)
	}
	diff := filterByKind(views, catalog.KindDiffusion, kindOf)
	if len(diff) != 1 || diff[0].Name != "a" {
		t.Errorf("diffusion filter = %+v", diff)
	}
	if got := filterByKind(views, catalog.KindControlNet, kindOf); len(got) != 0 {
		t.Errorf("controlnet filter = %+v, want empty", got)
	}
}

// LoRA / ControlNet catalog entries must carry an Arch (unlike upscalers) so
// callers can filter incompatible base-model combinations (ADR-0006).
func TestLoRACatalogEntriesCarryArch(t *testing.T) {
	var sawLoRA bool
	for _, e := range catalog.Default() {
		if e.IsLoRA() || e.IsControlNet() {
			sawLoRA = sawLoRA || e.IsLoRA()
			if e.Arch == "" {
				t.Errorf("%s entry %q has no Arch", e.Kind, e.Name)
			}
			if e.Source.HF == "" && e.Source.Civitai == "" && e.Source.URL == "" {
				t.Errorf("%s entry %q has no downloadable source", e.Kind, e.Name)
			}
		}
		if e.IsUpscaler() && e.Arch != "" {
			t.Errorf("upscaler %q should be architecture-agnostic, got %q", e.Name, e.Arch)
		}
	}
	if !sawLoRA {
		t.Error("expected at least one LoRA catalog entry")
	}
}

// TestResolveAuxModelRefusesAnotherArchitecture is ADR-0006's promise that
// ADR-0007 repeats: an installed LoRA / ControlNet recorded for another
// architecture than the model is a clear error before the render, not a
// failure deep in sd.cpp or a garbage image. Only recorded architectures are
// compared, so a raw path on either side still passes through.
func TestResolveAuxModelRefusesAnotherArchitecture(t *testing.T) {
	get := fakeRegistry(
		store.InstalledModel{Name: "lcm-lora-sdxl", Kind: catalog.KindLoRA, Path: "/models/lcm.safetensors",
			Profile: profile.Profile{Name: "lcm-lora-sdxl", Arch: profile.ArchSDXL}},
		store.InstalledModel{Name: "canny-sdxl", Kind: catalog.KindControlNet, Path: "/models/canny.safetensors",
			Profile: profile.Profile{Name: "canny-sdxl", Arch: profile.ArchSDXL}},
		store.InstalledModel{Name: "untagged-lora", Kind: catalog.KindLoRA, Path: "/models/u.safetensors"},
	)

	for _, kind := range []struct{ ref, kind string }{
		{"lcm-lora-sdxl", catalog.KindLoRA}, {"canny-sdxl", catalog.KindControlNet},
	} {
		_, err := resolveAuxModel(kind.ref, kind.kind, profile.ArchSD15, get)
		if err == nil || !strings.Contains(err.Error(), "is for sdxl and the model is sd15") {
			t.Errorf("%s against sd15: err = %v, want an architecture mismatch", kind.ref, err)
		}
		if got, err := resolveAuxModel(kind.ref, kind.kind, profile.ArchSDXL, get); err != nil || got == "" {
			t.Errorf("%s against sdxl = %q, %v; want it resolved", kind.ref, got, err)
		}
	}

	pass := []struct {
		name, ref string
		base      profile.Arch
	}{
		{"model with no recorded arch", "lcm-lora-sdxl", ""},
		{"model arch unknown", "lcm-lora-sdxl", profile.ArchUnknown},
		{"LoRA with no recorded arch", "untagged-lora", profile.ArchSD15},
		{"LoRA given by path", "/somewhere/sdxl-lora.safetensors", profile.ArchSD15},
	}
	for _, tc := range pass {
		if _, err := resolveAuxModel(tc.ref, catalog.KindLoRA, tc.base, get); err != nil {
			t.Errorf("%s: %v; only two recorded architectures are compared", tc.name, err)
		}
	}
}

// TestOnlyARegisteredModelHasARecordedArch: a --model-path's architecture is
// profile.Detect's guess from the filename, which is SDXL for any name it
// does not recognise, so it must not be used to refuse a LoRA.
func TestOnlyARegisteredModelHasARecordedArch(t *testing.T) {
	guessed := resolved{Path: "/x/model.safetensors", Profile: profile.Profile{Arch: profile.ArchSDXL}}
	if a := guessed.recordedArch(); a != "" {
		t.Errorf("raw path recordedArch = %q, want none", a)
	}
	registered := resolved{Profile: profile.Profile{Arch: profile.ArchSD15}, Registered: true}
	if a := registered.recordedArch(); a != profile.ArchSD15 {
		t.Errorf("registered recordedArch = %q, want sd15", a)
	}
}
