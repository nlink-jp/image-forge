package cli

import (
	"testing"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

// A LoRA whose trigger words are missing from the prompt loads without error and
// silently does nothing, so the tokens must survive from catalog -> registry ->
// `models list --json`.
func TestSplitTriggers(t *testing.T) {
	cases := map[string][]string{
		"":                    nil,
		"mythp0rt":            {"mythp0rt"},
		"pov, on couch":       {"pov", "on couch"},
		" a , , b ":           {"a", "b"},
		"genba_neko,chibi,:3": {"genba_neko", "chibi", ":3"},
	}
	for in, want := range cases {
		got := splitTriggers(in)
		if len(got) != len(want) {
			t.Errorf("splitTriggers(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitTriggers(%q) = %v, want %v", in, got, want)
				break
			}
		}
	}
}

// installedViews must surface trigger words so a front-end can show (or insert)
// them when the user picks a LoRA.
func TestInstalledViewsSurfacesTriggerWords(t *testing.T) {
	reg := &store.Registry{Models: map[string]store.InstalledModel{
		"style-lora": {
			Name: "style-lora", Kind: catalog.KindLoRA, Path: "/m/style.safetensors",
			Profile:      profile.Profile{Name: "style-lora", Arch: profile.ArchSDXL},
			TriggerWords: []string{"mythp0rt", "on couch"},
		},
		"plain-lora": {
			Name: "plain-lora", Kind: catalog.KindLoRA, Path: "/m/plain.safetensors",
			Profile: profile.Profile{Name: "plain-lora", Arch: profile.ArchSDXL},
		},
	}}
	views := installedViews(reg)
	byName := map[string]installedView{}
	for _, v := range views {
		byName[v.Name] = v
	}
	got := byName["style-lora"].TriggerWords
	if len(got) != 2 || got[0] != "mythp0rt" || got[1] != "on couch" {
		t.Errorf("trigger words = %v", got)
	}
	// A LoRA that needs no trigger (LCM, sliders) reports none, and the field is
	// omitted from JSON rather than being an empty array.
	if got := byName["plain-lora"].TriggerWords; got != nil {
		t.Errorf("expected no trigger words, got %v", got)
	}
}

// Catalog entries carry the tokens through to `models list --catalog --json`.
func TestCatalogViewsSurfacesTriggerWords(t *testing.T) {
	var checked bool
	for _, v := range catalogViews(&store.Registry{Models: map[string]store.InstalledModel{}}) {
		e, ok := catalog.Find(v.Name)
		if !ok {
			t.Fatalf("catalog view %q has no entry", v.Name)
		}
		if len(e.TriggerWords) != len(v.TriggerWords) {
			t.Errorf("%s: view triggers %v != entry %v", v.Name, v.TriggerWords, e.TriggerWords)
		}
		checked = true
	}
	if !checked {
		t.Fatal("catalog is empty")
	}
}

// Only LoRAs should carry trigger words — a base model or upscaler with them
// would be a data-entry mistake.
func TestOnlyLoRAsHaveTriggerWords(t *testing.T) {
	for _, e := range catalog.Default() {
		if len(e.TriggerWords) > 0 && !e.IsLoRA() {
			t.Errorf("%s entry %q has trigger words but is not a LoRA", e.Kind, e.Name)
		}
	}
}
