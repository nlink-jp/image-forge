package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nlink-jp/image-forge/internal/config"
	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

// seedRegistry writes a registry with a diffusion model and an upscaler into a
// temp IMAGE_FORGE_HOME for the duration of the test, and creates the weight
// files they point at — resolution rejects an installed model whose files are
// absent (ADR-0008), so the paths must be real. Returns (diffusion, upscaler).
func seedRegistry(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("IMAGE_FORGE_HOME", home)
	models := filepath.Join(home, "models")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	sdxl := filepath.Join(models, "sdxl.safetensors")
	esrgan := filepath.Join(models, "realesrgan.pth")
	touch(t, sdxl, 8)
	touch(t, esrgan, 8)

	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	reg.Add(store.InstalledModel{
		Name: "sdxl-model", Path: sdxl,
		Profile: profile.ArchDefaults(profile.ArchSDXL),
	})
	reg.Add(store.InstalledModel{
		Name: "realesrgan-x4plus", Kind: "upscaler", Path: esrgan,
		Profile: profile.Profile{Name: "realesrgan-x4plus"},
	})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	return sdxl, esrgan
}

func TestResolveUpscalerModel(t *testing.T) {
	_, esrgan := seedRegistry(t) // registry has exactly one upscaler: realesrgan-x4plus
	none := config.Config{}

	// An installed upscaler resolves to its path.
	got, err := resolveUpscalerModel("realesrgan-x4plus", "", none)
	if err != nil {
		t.Fatalf("resolve upscaler by name: %v", err)
	}
	if got != esrgan {
		t.Errorf("path = %q", got)
	}

	// A diffusion model passed as --model is rejected.
	if _, err := resolveUpscalerModel("sdxl-model", "", none); err == nil {
		t.Error("expected rejection of a diffusion model as an upscaler")
	}

	// An unknown name errors.
	if _, err := resolveUpscalerModel("nope", "", none); err == nil {
		t.Error("expected error for an uninstalled upscaler")
	}

	// A raw path is passed through.
	got, err = resolveUpscalerModel("", "/tmp/some.pth", none)
	if err != nil || got != "/tmp/some.pth" {
		t.Errorf("model-path passthrough: got %q err %v", got, err)
	}

	// No name/path falls back to the sole installed upscaler.
	got, err = resolveUpscalerModel("", "", none)
	if err != nil || got != esrgan {
		t.Errorf("sole-upscaler fallback: got %q err %v", got, err)
	}

	// config [upscaler] default_model resolves to that upscaler.
	cfg := config.Config{Upscaler: config.UpscalerConfig{DefaultModel: "realesrgan-x4plus"}}
	got, err = resolveUpscalerModel("", "", cfg)
	if err != nil || got != esrgan {
		t.Errorf("config default_model: got %q err %v", got, err)
	}

	// config default_model naming an uninstalled upscaler errors.
	if _, err := resolveUpscalerModel("", "", config.Config{Upscaler: config.UpscalerConfig{DefaultModel: "ghost"}}); err == nil {
		t.Error("expected error when default_model is not installed")
	}
}

func TestResolveUpscalerModel_NoneInstalled(t *testing.T) {
	// A registry with no upscaler and no config default => a clear error.
	t.Setenv("IMAGE_FORGE_HOME", t.TempDir())
	if _, err := resolveUpscalerModel("", "", config.Config{}); err == nil {
		t.Error("expected error when no upscaler is available")
	}
}

func TestResolveHiresModel(t *testing.T) {
	_, esrgan := seedRegistry(t)

	if got, err := resolveHiresModel(""); err != nil || got != "" {
		t.Errorf("empty ref: got %q err %v", got, err)
	}
	if got, err := resolveHiresModel("realesrgan-x4plus"); err != nil || got != esrgan {
		t.Errorf("by name: got %q err %v", got, err)
	}
	// A diffusion model is rejected as a hires model.
	if _, err := resolveHiresModel("sdxl-model"); err == nil {
		t.Error("expected rejection of a diffusion model as --hires-model")
	}
	// An unknown name that is not a file errors.
	if _, err := resolveHiresModel("not-installed-and-not-a-file"); err == nil {
		t.Error("expected error for an unknown, non-file hires model")
	}
}
