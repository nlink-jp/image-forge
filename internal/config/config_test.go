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

func TestLoad_LegacyFallback(t *testing.T) {
	// New config path absent, legacy $IMAGE_FORGE_HOME/config.toml present → read it.
	home := t.TempDir()
	t.Setenv("IMAGE_FORGE_HOME", home)       // store.Home() -> legacy dir
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // new config dir (empty)
	t.Setenv("IMAGE_FORGE_CONFIG", "")       // no explicit override
	if err := os.WriteFile(filepath.Join(home, "config.toml"),
		[]byte("default_model = \"legacy-model\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultModel != "legacy-model" {
		t.Errorf("legacy config not read: %+v", c)
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

func TestModelsDirResolved(t *testing.T) {
	if got := (Config{}).ModelsDirResolved(); got != "" {
		t.Errorf("empty models_dir should resolve to \"\" (use default), got %q", got)
	}
	if got := (Config{ModelsDir: "/mnt/ext/models"}).ModelsDirResolved(); got != "/mnt/ext/models" {
		t.Errorf("absolute models_dir = %q", got)
	}
	home, _ := os.UserHomeDir()
	if got := (Config{ModelsDir: "~/if-models"}).ModelsDirResolved(); got != home+"/if-models" {
		t.Errorf("~ expansion = %q, want %s/if-models", got, home)
	}
}

func TestEmbedMetadata_DefaultTrue(t *testing.T) {
	// Absent config => embedding is on by default.
	if !(Config{}).EmbedMetadata() {
		t.Error("EmbedMetadata should default to true when unset")
	}
	// Loading a file without a [metadata] section keeps the default.
	t.Setenv("IMAGE_FORGE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.EmbedMetadata() {
		t.Error("EmbedMetadata should be true when [metadata] is absent")
	}
}

func TestFlashAttn_DefaultFalseExplicitTrue(t *testing.T) {
	if (Config{}).FlashAttn() {
		t.Error("FlashAttn should default to false (opt-in) when unset")
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[performance]\nflash_attn = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FORGE_CONFIG", p)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.FlashAttn() {
		t.Error("FlashAttn should be true with [performance] flash_attn = true")
	}
}

func TestEmbedMetadata_ExplicitFalse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[metadata]\nembed = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FORGE_CONFIG", p)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.EmbedMetadata() {
		t.Error("explicit embed = false should disable embedding")
	}

	// And explicit true stays true.
	p2 := filepath.Join(t.TempDir(), "config2.toml")
	if err := os.WriteFile(p2, []byte("[metadata]\nembed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FORGE_CONFIG", p2)
	c2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c2.EmbedMetadata() {
		t.Error("explicit embed = true should enable embedding")
	}
}
