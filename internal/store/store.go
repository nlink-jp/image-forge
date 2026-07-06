// Package store persists the registry of installed models — each entry pairs an
// on-disk model path with the generation profile to apply. The registry is
// machine-managed state (JSON), distinct from user-facing config.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/nlink-jp/image-forge/internal/profile"
)

// Components are the separate weight files for multi-component models (FLUX,
// SD3.5, Z-Image), where there is no single all-in-one checkpoint. Paths are
// absolute local files.
type Components struct {
	DiffusionModel string `json:"diffusion_model,omitempty"`
	ClipL          string `json:"clip_l,omitempty"`
	ClipG          string `json:"clip_g,omitempty"`
	T5XXL          string `json:"t5xxl,omitempty"`
}

// InstalledModel is a registered, ready-to-use model. Either Path (a single-file
// checkpoint) or Components (multi-component) is set.
type InstalledModel struct {
	Name       string          `json:"name"`
	Path       string          `json:"path"`
	VAEPath    string          `json:"vae_path,omitempty"`
	Components Components      `json:"components,omitempty"`
	Profile    profile.Profile `json:"profile"`
	Rating     profile.Rating  `json:"rating,omitempty"`
	License    string          `json:"license,omitempty"`
}

// Registry is the set of installed models, keyed by name.
type Registry struct {
	Models map[string]InstalledModel `json:"models"`
}

// Home is the image-forge data directory. Overridable via IMAGE_FORGE_HOME
// (tests) or XDG_DATA_HOME.
func Home() string {
	if h := os.Getenv("IMAGE_FORGE_HOME"); h != "" {
		return h
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "image-forge")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "image-forge")
}

// ModelsDir is where pulled model files are stored.
func ModelsDir() string { return filepath.Join(Home(), "models") }

func registryPath() string { return filepath.Join(Home(), "registry.json") }

// Load reads the registry, returning an empty one if it does not exist yet.
func Load() (*Registry, error) {
	r := &Registry{Models: map[string]InstalledModel{}}
	b, err := os.ReadFile(registryPath())
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, r); err != nil {
		return nil, err
	}
	if r.Models == nil {
		r.Models = map[string]InstalledModel{}
	}
	return r, nil
}

// Save writes the registry to disk, creating the data directory if needed.
func (r *Registry) Save() error {
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(registryPath(), b, 0o644)
}

func (r *Registry) Add(m InstalledModel) { r.Models[m.Name] = m }

func (r *Registry) Get(name string) (InstalledModel, bool) {
	m, ok := r.Models[name]
	return m, ok
}

func (r *Registry) Remove(name string) bool {
	if _, ok := r.Models[name]; !ok {
		return false
	}
	delete(r.Models, name)
	return true
}
