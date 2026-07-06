package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_DefaultsWhenAbsent(t *testing.T) {
	t.Setenv("IMAGE_FORGE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Output != "out.png" {
		t.Errorf("default output = %q, want out.png", c.Output)
	}
	if c.AllowNSFW || c.DefaultModel != "" {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestLoad_File(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(
		"default_model = \"animagine-xl-4\"\nallow_nsfw = true\noutput = \"r/o.png\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FORGE_CONFIG", p)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultModel != "animagine-xl-4" || !c.AllowNSFW || c.Output != "r/o.png" {
		t.Errorf("loaded config mismatch: %+v", c)
	}
}

func TestResolveHFToken_EnvWins(t *testing.T) {
	c := Config{HFToken: "from-config"}
	t.Setenv("HF_TOKEN", "from-env")
	if got := c.ResolveHFToken(); got != "from-env" {
		t.Errorf("env should win, got %q", got)
	}
	t.Setenv("HF_TOKEN", "")
	if got := c.ResolveHFToken(); got != "from-config" {
		t.Errorf("fallback to config, got %q", got)
	}
}
