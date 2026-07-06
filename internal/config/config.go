// Package config loads optional user configuration (config.toml). Every field has
// a sensible default, so a missing file is fine.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/nlink-jp/image-forge/internal/store"
)

// Config holds user settings. Tokens here are a fallback; the matching
// environment variables (HF_TOKEN / CIVITAI_TOKEN) take precedence.
type Config struct {
	DefaultModel string `toml:"default_model"`
	Output       string `toml:"output"`
	AllowNSFW    bool   `toml:"allow_nsfw"`
	HFToken      string `toml:"hf_token"`
	CivitaiToken string `toml:"civitai_token"`
}

// Path is the config file location, matching the other util-series tools:
// $IMAGE_FORGE_CONFIG if set, else $XDG_CONFIG_HOME/image-forge/config.toml, else
// ~/.config/image-forge/config.toml. (The data directory — registry and models —
// is separate; see store.Home.)
func Path() string {
	if p := os.Getenv("IMAGE_FORGE_CONFIG"); p != "" {
		return p
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "image-forge", "config.toml")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "image-forge", "config.toml")
}

// legacyPath is the pre-v0.5 location ($IMAGE_FORGE_HOME/config.toml, in the data
// dir), read as a fallback so existing configs keep working.
func legacyPath() string {
	return filepath.Join(store.Home(), "config.toml")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// resolvePath returns the config file to read: Path() if present; otherwise the
// legacy location (unless IMAGE_FORGE_CONFIG explicitly pointed elsewhere); else "".
func resolvePath() string {
	if p := Path(); fileExists(p) {
		return p
	}
	if os.Getenv("IMAGE_FORGE_CONFIG") != "" {
		return "" // explicit override that doesn't exist — don't fall back
	}
	if lp := legacyPath(); lp != Path() && fileExists(lp) {
		return lp
	}
	return ""
}

// Load reads the config file, returning defaults when none exists.
func Load() (Config, error) {
	c := Config{Output: "out.png"}
	p := resolvePath()
	if p == "" {
		return c, nil
	}
	if _, err := toml.DecodeFile(p, &c); err != nil {
		return c, err
	}
	if c.Output == "" {
		c.Output = "out.png"
	}
	return c, nil
}

// ResolveHFToken returns the effective Hugging Face token (env wins over config).
func (c Config) ResolveHFToken() string {
	if e := os.Getenv("HF_TOKEN"); e != "" {
		return e
	}
	return c.HFToken
}

// ResolveCivitaiToken returns the effective Civitai token (env wins over config).
func (c Config) ResolveCivitaiToken() string {
	if e := os.Getenv("CIVITAI_TOKEN"); e != "" {
		return e
	}
	return c.CivitaiToken
}
