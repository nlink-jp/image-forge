package cli

import (
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/catalog"
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
	got, err := resolveAuxModel("lcm-lora-sdxl", catalog.KindLoRA, get)
	if err != nil || got != "/models/lcm.safetensors" {
		t.Errorf("lora by name = %q, %v", got, err)
	}
	got, err = resolveAuxModel("canny-sdxl", catalog.KindControlNet, get)
	if err != nil || got != "/models/canny.safetensors" {
		t.Errorf("controlnet by name = %q, %v", got, err)
	}

	// An empty ref stays empty (feature not requested).
	if got, err := resolveAuxModel("", catalog.KindLoRA, get); err != nil || got != "" {
		t.Errorf("empty ref = %q, %v", got, err)
	}
}

func TestResolveAuxModelPathPassthrough(t *testing.T) {
	get := fakeRegistry()
	// Values that look like paths pass through unchanged (back-compat).
	for _, p := range []string{"/abs/path/x.safetensors", "rel/dir/y.pth", "z.safetensors"} {
		got, err := resolveAuxModel(p, catalog.KindLoRA, get)
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
	_, err := resolveAuxModel("no-such-lora", catalog.KindLoRA, get)
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("missing lora err = %v, want 'not installed'", err)
	}

	// A name registered under the wrong kind is rejected.
	_, err = resolveAuxModel("animagine-xl-4", catalog.KindLoRA, get)
	if err == nil || !strings.Contains(err.Error(), "not a LoRA") {
		t.Errorf("wrong-kind err = %v, want 'not a LoRA'", err)
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
