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

// Path is the config file location: $IMAGE_FORGE_CONFIG if set, else
// <data dir>/config.toml.
func Path() string {
	if p := os.Getenv("IMAGE_FORGE_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(store.Home(), "config.toml")
}

// Load reads the config file, returning defaults when it is absent.
func Load() (Config, error) {
	c := Config{Output: "out.png"}
	p := Path()
	if _, err := os.Stat(p); os.IsNotExist(err) {
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
