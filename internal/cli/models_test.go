package cli

import (
	"testing"

	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

func TestResolveListMode(t *testing.T) {
	cases := []struct {
		name                       string
		all, cat, inst             bool
		wantInstalled, wantCatalog bool
	}{
		{"default: installed only", false, false, false, true, false},
		{"--installed", false, false, true, true, false},
		{"--catalog only", false, true, false, false, true},
		{"--all: both", true, false, false, true, true},
		{"--catalog --installed: both", false, true, true, true, true},
		{"--all overrides --catalog", true, true, false, true, true},
	}
	for _, c := range cases {
		gi, gc := resolveListMode(c.all, c.cat, c.inst)
		if gi != c.wantInstalled || gc != c.wantCatalog {
			t.Errorf("%s: got (installed=%v catalog=%v), want (installed=%v catalog=%v)",
				c.name, gi, gc, c.wantInstalled, c.wantCatalog)
		}
	}
}

func testRegistry() *store.Registry {
	return &store.Registry{Models: map[string]store.InstalledModel{
		"sd15-emaonly": { // a catalog name
			Name: "sd15-emaonly", Path: "/x/sd15.gguf",
			Profile: profile.Profile{Arch: profile.ArchSD15}, Rating: profile.RatingSafe,
		},
		"my-local": { // not in the catalog
			Name: "my-local", Path: "/x/local.safetensors",
			Profile: profile.Profile{Arch: profile.ArchSDXL},
		},
		"flux-multi": { // multi-component: no single Path
			Name: "flux-multi", Components: store.Components{ClipL: "/x/clip_l.safetensors"},
			Profile: profile.Profile{Arch: profile.ArchFlux},
		},
	}}
}

func TestCatalogViewsMarkInstalled(t *testing.T) {
	views := catalogViews(testRegistry())
	if len(views) == 0 {
		t.Fatal("catalogViews returned nothing")
	}
	byName := map[string]catalogView{}
	for _, v := range views {
		byName[v.Name] = v
	}
	if !byName["sd15-emaonly"].Installed {
		t.Error("sd15-emaonly is registered; catalog view should mark it installed")
	}
	// A catalog entry that is NOT registered must read as not installed.
	if v, ok := byName["juggernaut-xl-v9"]; !ok {
		t.Error("expected juggernaut-xl-v9 in the catalog")
	} else if v.Installed {
		t.Error("juggernaut-xl-v9 is not registered; should not be marked installed")
	}
}

func TestInstalledViews(t *testing.T) {
	views := installedViews(testRegistry())
	if len(views) != 3 {
		t.Fatalf("got %d installed views, want 3", len(views))
	}
	// Sorted by name for deterministic output.
	if views[0].Name != "flux-multi" || views[1].Name != "my-local" || views[2].Name != "sd15-emaonly" {
		t.Errorf("installed views not sorted by name: %v", []string{views[0].Name, views[1].Name, views[2].Name})
	}
	byName := map[string]installedView{}
	for _, v := range views {
		byName[v.Name] = v
	}
	if !byName["sd15-emaonly"].InCatalog {
		t.Error("sd15-emaonly is a catalog name; InCatalog should be true")
	}
	if byName["my-local"].InCatalog {
		t.Error("my-local is not a catalog name; InCatalog should be false")
	}
	if !byName["flux-multi"].MultiComponent {
		t.Error("flux-multi has no single Path; MultiComponent should be true")
	}
	if byName["my-local"].MultiComponent {
		t.Error("my-local has a single Path; MultiComponent should be false")
	}
}

func TestResolveModelPageURL(t *testing.T) {
	// A known Civitai catalog model resolves to its version page.
	got, err := resolveModelPageURL("anima-yume")
	if err != nil {
		t.Fatalf("resolveModelPageURL(anima-yume): %v", err)
	}
	if got != "https://civitai.com/model-versions/3065644" {
		t.Errorf("anima-yume page = %q, want the civitai model-versions URL", got)
	}

	// An HF-sourced catalog model resolves to its repo page.
	if u, err := resolveModelPageURL("flux1-schnell"); err != nil || u == "" {
		t.Errorf("flux1-schnell should resolve to an HF page, got (%q, %v)", u, err)
	}

	// An unknown model is an error (not a silent empty URL).
	if _, err := resolveModelPageURL("does-not-exist"); err == nil {
		t.Error("resolveModelPageURL of an unknown model should error")
	}
}

func TestCatalogViewsCarryPageURL(t *testing.T) {
	// The JSON that backs `models list --json` (and the MCP list_models tool) must
	// carry page_url for a sourced catalog model — it's the data a front-end reads.
	views := catalogViews(testRegistry())
	for _, v := range views {
		if v.Name == "anima-yume" {
			if v.PageURL != "https://civitai.com/model-versions/3065644" {
				t.Errorf("anima-yume catalogView.PageURL = %q, want the civitai URL", v.PageURL)
			}
			return
		}
	}
	t.Fatal("anima-yume not found in catalog views")
}
