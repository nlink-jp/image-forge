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
