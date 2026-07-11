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
	DefaultModel string            `toml:"default_model"`
	Output       string            `toml:"output"`
	AllowNSFW    bool              `toml:"allow_nsfw"`
	ModelsDir    string            `toml:"models_dir"`
	HFToken      string            `toml:"hf_token"`
	CivitaiToken string            `toml:"civitai_token"`
	MCP          MCPConfig         `toml:"mcp"`
	Hires        HiresConfig       `toml:"hires"`
	Upscaler     UpscalerConfig    `toml:"upscaler"`
	Metadata     MetadataConfig    `toml:"metadata"`
	Performance  PerformanceConfig `toml:"performance"`
}

// PerformanceConfig holds engine performance flags.
type PerformanceConfig struct {
	// FlashAttn opts into flash attention (default off). On Apple Silicon / Metal
	// it is neutral at native resolution and a modest win only on large / hires
	// renders (~8% faster, some memory), and it changes outputs slightly
	// (numerically-equivalent attention, not bit-identical). Off by default keeps
	// same-seed outputs stable; enable it for large / hires work.
	FlashAttn *bool `toml:"flash_attn"`
}

// FlashAttn reports whether flash attention should be enabled at model load.
// Defaults to false (opt-in); `[performance] flash_attn = true` enables it, and
// `gen --flash-attn` composes on top for one invocation.
func (c Config) FlashAttn() bool {
	if c.Performance.FlashAttn == nil {
		return false
	}
	return *c.Performance.FlashAttn
}

// MetadataConfig controls embedding generation metadata (prompt/params/model)
// into generated PNGs as text chunks. Embed is a pointer so absence (nil) means
// "on" — the default is embed=true; only an explicit `embed = false` disables it.
type MetadataConfig struct {
	Embed *bool `toml:"embed"`
}

// EmbedMetadata reports whether generation metadata should be embedded into
// output PNGs. Defaults to true (absence means on); only an explicit
// `[metadata] embed = false` turns it off. The `gen --no-metadata` flag composes
// on top of this (either one being false suppresses embedding).
func (c Config) EmbedMetadata() bool {
	if c.Metadata.Embed == nil {
		return true
	}
	return *c.Metadata.Embed
}

// ModelsDirResolved returns the configured model-file directory with "~"
// expanded, or "" to use the default (<data-dir>/models). Relocating it (e.g.
// onto a bigger disk) affects new pulls; already-installed models keep the
// absolute paths recorded in the registry.
func (c Config) ModelsDirResolved() string {
	return expandHome(c.ModelsDir)
}

// HiresConfig holds the default hires.fix upscaler policy.
type HiresConfig struct {
	// Upscaler selects which upscaler hires.fix uses by default: "latent"
	// (built-in, no model), "lanczos"/"nearest" (built-in), "model", "auto"
	// (a downloaded ESRGAN upscaler if one is installed, else latent), or the
	// name of an installed upscaler model. The gen flags and the model profile
	// override this. Empty is treated as "auto".
	Upscaler string `toml:"upscaler"`
}

// UpscalerConfig holds defaults for standalone/hires ESRGAN upscaling.
type UpscalerConfig struct {
	// DefaultModel is the installed upscaler-model name used when a model is
	// needed but not named — the `upscale` command without --model, and hires
	// with an ESRGAN upscaler and no explicit model.
	DefaultModel string `toml:"default_model"`
}

// HiresUpscaler returns the configured default hires upscaler policy, defaulting
// to "auto" (prefer a downloaded ESRGAN, else the built-in latent upscaler).
func (c Config) HiresUpscaler() string {
	if c.Hires.Upscaler == "" {
		return "auto"
	}
	return c.Hires.Upscaler
}

// MCPConfig holds optional settings for the `image-forge mcp` server. Every
// field is optional; an empty WorkspaceRoot falls back to the built-in default
// (<data-dir>/mcp-workspaces).
type MCPConfig struct {
	// WorkspaceRoot is the default root the MCP server writes workspaces under
	// when a call omits workspace_root.
	WorkspaceRoot string `toml:"workspace_root"`
}

// MCPWorkspaceRoot returns the configured default MCP workspace root, with "~"
// expanded. Empty means "use the built-in default".
func (c Config) MCPWorkspaceRoot() string {
	return expandHome(c.MCP.WorkspaceRoot)
}

// expandHome expands a leading "~" to the user's home directory.
func expandHome(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if len(p) > 1 && p[1] == '/' {
		return filepath.Join(home, p[2:])
	}
	return p
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
