package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

// archRegistry installs an SD1.5 base and an SDXL LoRA, both with --arch, into
// a throwaway registry, with real (empty) weight files so resolution gets as
// far as the check. The config points at an empty file so nothing of the
// operator's is read.
func archRegistry(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("IMAGE_FORGE_HOME", home)
	cfg := filepath.Join(home, "config.toml")
	if err := os.WriteFile(cfg, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FORGE_CONFIG", cfg)
	weights := func(name string) string {
		p := filepath.Join(home, name)
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	reg.Add(store.InstalledModel{Name: "base-sd15", Path: weights("base.safetensors"),
		Profile: profile.ArchDefaults(profile.ArchSD15), ArchSource: store.ArchFromFlag})
	reg.Add(store.InstalledModel{Name: "lora-sdxl", Kind: catalog.KindLoRA, Path: weights("lora.safetensors"),
		Profile: profile.Profile{Name: "lora-sdxl", Arch: profile.ArchSDXL}, ArchSource: store.ArchFromFlag})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
}

// The check is wired into both ways a render is built: buildRender (the
// resident serve loop and the MCP worker) and gen. Removing the arch from
// resolveModel, or passing none at either call site, fails here — the review
// found all three could be switched off with the unit tests still green.
func TestEveryRenderPathRefusesAnArchMismatch(t *testing.T) {
	archRegistry(t)

	_, _, _, _, err := buildRender(RenderRequest{Model: "base-sd15", LoRAs: []string{"lora-sdxl:1"}})
	if err == nil || !strings.Contains(err.Error(), "is for sdxl and the model is sd15") {
		t.Errorf("buildRender: err = %v, want the architecture mismatch", err)
	}

	err = runGen([]string{"-p", "x", "-m", "base-sd15", "--lora", "lora-sdxl:1", "-o", filepath.Join(t.TempDir(), "o.png")})
	if err == nil || !strings.Contains(err.Error(), "is for sdxl and the model is sd15") {
		t.Errorf("gen: err = %v, want the architecture mismatch before anything loads", err)
	}
}

// The write side: what a registration records. archFor is the one decision
// import and a raw-ref pull share; derivedArchSource carries trust through
// quantize. A guess recorded as a fact would bring back the dreamshaper_8
// refusal, so each branch is pinned.
func TestArchForRecordsAGuessAsAGuess(t *testing.T) {
	a, src, err := archFor("dreamshaper_8", "")
	if err != nil || a != profile.ArchSDXL || src != store.ArchDetected {
		t.Errorf("no --arch: %q %q %v, want sdxl detected", a, src, err)
	}
	a, src, err = archFor("dreamshaper_8", "SD15")
	if err != nil || a != profile.ArchSD15 || src != store.ArchFromFlag {
		t.Errorf("--arch SD15: %q %q %v, want sd15 flag", a, src, err)
	}
	if _, _, err := archFor("x", "pony"); err == nil {
		t.Error("--arch pony was accepted")
	}
}

func TestDerivedArchSourceKeepsTheSourcesTrust(t *testing.T) {
	cases := []struct {
		name string
		src  store.InstalledModel
		want string
	}{
		{"flag", store.InstalledModel{Name: "a", Profile: profile.Profile{Arch: profile.ArchSD15}, ArchSource: store.ArchFromFlag}, store.ArchFromFlag},
		{"detected", store.InstalledModel{Name: "a", Profile: profile.Profile{Arch: profile.ArchSDXL}, ArchSource: store.ArchDetected}, store.ArchDetected},
		{"old catalog install", store.InstalledModel{Name: "lcm-lora-sd15", Profile: profile.Profile{Arch: profile.ArchSD15}}, store.ArchFromCatalog},
		{"old, not the catalog's", store.InstalledModel{Name: "dreamshaper_8", Profile: profile.Profile{Arch: profile.ArchSDXL}}, ""},
	}
	for _, tc := range cases {
		if got := derivedArchSource(tc.src); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// modelsImport end to end against a throwaway registry: what is written for
// no --arch, a valid one, and an invalid one.
func TestModelsImportRecordsWhereTheArchCameFrom(t *testing.T) {
	archRegistry(t)
	file := filepath.Join(t.TempDir(), "dreamshaper_8.safetensors")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := modelsImport([]string{file}); err != nil {
		t.Fatal(err)
	}
	if err := modelsImport([]string{file, "--name", "ds8-sd15", "--arch", "SD15"}); err != nil {
		t.Fatal(err)
	}
	if err := modelsImport([]string{file, "--name", "ds8-pony", "--arch", "pony"}); err == nil {
		t.Error("--arch pony was imported")
	}
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if im, _ := reg.Get("dreamshaper_8"); im.ArchSource != store.ArchDetected || trustedArch(im) != "" {
		t.Errorf("no --arch: source %q trusted %q; want a guess that is not trusted", im.ArchSource, trustedArch(im))
	}
	if im, _ := reg.Get("ds8-sd15"); im.ArchSource != store.ArchFromFlag || trustedArch(im) != profile.ArchSD15 {
		t.Errorf("--arch SD15: source %q trusted %q; want sd15 from the flag", im.ArchSource, trustedArch(im))
	}
	if _, ok := reg.Get("ds8-pony"); ok {
		t.Error("an invalid --arch left a registry entry")
	}
}
