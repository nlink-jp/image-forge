// Package store persists the registry of installed models — each entry pairs an
// on-disk model path with the generation profile to apply. The registry is
// machine-managed state (JSON), distinct from user-facing config.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

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
	LLM            string `json:"llm,omitempty"`
}

// InstalledModel is a registered, ready-to-use model. Either Path (a single-file
// checkpoint) or Components (multi-component) is set.
type InstalledModel struct {
	Name string `json:"name"`
	// "" (diffusion, default), "upscaler", "lora", or "controlnet" (ADR-0006).
	Kind       string          `json:"kind,omitempty"`
	Path       string          `json:"path"`
	VAEPath    string          `json:"vae_path,omitempty"`
	Components Components      `json:"components,omitempty"`
	Profile    profile.Profile `json:"profile"`
	Rating     profile.Rating  `json:"rating,omitempty"`
	License    string          `json:"license,omitempty"`
	// LicenseFlags are notable usage restrictions (non-commercial / no-derivatives
	// / attribution / share-alike), recorded at install so a front-end can surface
	// them for the installed model without consulting the catalog.
	LicenseFlags []string `json:"license_flags,omitempty"`
	// Attribution is the credit text to give when the license requires it.
	Attribution string `json:"attribution,omitempty"`
	// TriggerWords are the prompt tokens a LoRA needs to take effect. Recorded at
	// install time so `models list` can tell the user what to type — a LoRA whose
	// trigger is missing loads silently and does nothing.
	TriggerWords []string `json:"trigger_words,omitempty"`
}

// FileRef is one recorded weight-file field of an InstalledModel, with its name
// and read/write access to the value. Field uses the JSON spelling ("path",
// "vae_path", "components.clip_l") so it can be shown to the user as-is.
type FileRef struct {
	Field string
	Get   func() string
	Set   func(string)
}

// fileRefs returns an accessor for every weight-file field this model records —
// the single-file checkpoint (Path), an external VAE (VAEPath), and each
// multi-component part. It is the single definition of "the files this model
// owns" (ADR-0008): Files, MissingFiles and Registry.Relocate are all built on
// it, so adding a component field only has to be handled here.
//
// The receiver is a pointer because the setters write through to it.
func (m *InstalledModel) fileRefs() []FileRef {
	c := &m.Components
	return []FileRef{
		{"path", func() string { return m.Path }, func(s string) { m.Path = s }},
		{"vae_path", func() string { return m.VAEPath }, func(s string) { m.VAEPath = s }},
		{"components.diffusion_model", func() string { return c.DiffusionModel }, func(s string) { c.DiffusionModel = s }},
		{"components.clip_l", func() string { return c.ClipL }, func(s string) { c.ClipL = s }},
		{"components.clip_g", func() string { return c.ClipG }, func(s string) { c.ClipG = s }},
		{"components.t5xxl", func() string { return c.T5XXL }, func(s string) { c.T5XXL = s }},
		{"components.llm", func() string { return c.LLM }, func(s string) { c.LLM = s }},
	}
}

// Files returns every weight file this model references (see fileRefs), cleaned.
// Empty fields are omitted. Used by `rm --purge` and `gc` to map a model to the
// files it owns and to build the set of files still in use.
func (m InstalledModel) Files() []string {
	var fs []string
	for _, r := range m.fileRefs() {
		if p := r.Get(); p != "" {
			fs = append(fs, filepath.Clean(p))
		}
	}
	return fs
}

// MissingFiles returns the recorded weight files that `exists` rejects, in
// fileRefs order. Empty means every file this model needs is present. `exists`
// is injected so callers can test against a synthetic filesystem; production
// callers pass FileExists.
//
// A non-empty result means the model cannot be loaded even though it is
// registered — typically the models dir moved (see `models relocate`), the
// volume holding it is not mounted, or a file was deleted outside image-forge.
func (m InstalledModel) MissingFiles(exists func(string) bool) []string {
	var out []string
	for _, r := range m.fileRefs() {
		if p := r.Get(); p != "" && !exists(p) {
			out = append(out, p)
		}
	}
	return out
}

// FileExists is the production `exists` predicate for MissingFiles / Relocate.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReferencedFiles is the set of all files (cleaned absolute paths) referenced by
// any installed model. A file shared by several models — a common VAE, or text
// encoders reused across multi-component models — appears once. `gc` treats a
// file in the models dir that is absent from this set as orphaned; `rm --purge`
// keeps a file still present here (another model needs it).
func (r *Registry) ReferencedFiles() map[string]bool {
	set := map[string]bool{}
	for _, m := range r.Models {
		for _, f := range m.Files() {
			set[f] = true
		}
	}
	return set
}

// IsUpscaler reports whether this installed model is a standalone ESRGAN
// upscaler rather than a diffusion model.
func (m InstalledModel) IsUpscaler() bool { return m.Kind == "upscaler" }

// IsLoRA reports whether this installed model is a LoRA adapter (applied on top
// of a base diffusion model of the same architecture).
func (m InstalledModel) IsLoRA() bool { return m.Kind == "lora" }

// IsControlNet reports whether this installed model is a ControlNet model
// (loaded alongside a base diffusion model of the same architecture).
func (m InstalledModel) IsControlNet() bool { return m.Kind == "controlnet" }

// IsDiffusion reports whether this installed model is a renderable base model
// (as opposed to an upscaler / LoRA / ControlNet helper).
func (m InstalledModel) IsDiffusion() bool { return m.Kind == "" }

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

// modelsDirOverride, when non-empty, relocates the model-file directory away
// from <home>/models — e.g. onto a bigger disk. It is set once at startup from
// config (SetModelsDir) rather than read here, so this package stays free of a
// config import (config already imports store).
var modelsDirOverride string

// SetModelsDir overrides the directory pulled model files are stored in. An
// empty dir restores the default (<home>/models). Also honored via the
// IMAGE_FORGE_MODELS_DIR environment variable. Call once at startup.
func SetModelsDir(dir string) { modelsDirOverride = dir }

// ModelsDir is where pulled model files are stored: the SetModelsDir/config
// override, else $IMAGE_FORGE_MODELS_DIR, else <home>/models.
func ModelsDir() string {
	if modelsDirOverride != "" {
		return modelsDirOverride
	}
	if d := os.Getenv("IMAGE_FORGE_MODELS_DIR"); d != "" {
		return d
	}
	return filepath.Join(Home(), "models")
}

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

// Relocation is one recorded weight-file path that relocation rewrites.
type Relocation struct {
	Model string
	Field string // the JSON field name, e.g. "path" or "components.clip_l"
	From  string
	To    string
}

// MissingFile is a recorded weight file that is absent on disk and that
// relocation could not account for — no same-named file exists in the target
// directory, so there is nothing to point the registry at.
type MissingFile struct {
	Model string
	Field string
	Path  string
}

// RelocatePlan is the outcome of reconciling the registry against a models
// directory: the paths rewritten (or that would be, on a dry run) and the
// missing files relocation could not resolve.
type RelocatePlan struct {
	Moves      []Relocation
	Unresolved []MissingFile
}

// Empty reports whether the registry is already consistent with the target
// directory — nothing to rewrite and nothing missing.
func (p RelocatePlan) Empty() bool { return len(p.Moves) == 0 && len(p.Unresolved) == 0 }

// Relocate reconciles recorded weight-file paths with dir, the directory the
// model files now live in (ADR-0008). A recorded path is rewritten to
// dir/<basename> only when the recorded path is absent AND that candidate is
// present: a path that still resolves is never touched (models deliberately kept
// outside the models dir keep working), and a missing file with no candidate is
// reported as unresolved rather than guessed at.
//
// With apply false nothing is mutated — the returned plan is a preview. With
// apply true the in-memory registry is updated; the caller Saves it. Results are
// ordered by model name then field, so a dry run and the apply that follows read
// identically. `exists` is injected for testability (production: FileExists).
func (r *Registry) Relocate(dir string, apply bool, exists func(string) bool) RelocatePlan {
	var plan RelocatePlan
	names := make([]string, 0, len(r.Models))
	for n := range r.Models {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		m := r.Models[n] // a copy; written back only when apply changed it
		changed := false
		for _, ref := range m.fileRefs() {
			p := ref.Get()
			if p == "" || exists(p) {
				continue
			}
			cand := filepath.Join(dir, filepath.Base(p))
			if !exists(cand) {
				plan.Unresolved = append(plan.Unresolved, MissingFile{Model: n, Field: ref.Field, Path: p})
				continue
			}
			plan.Moves = append(plan.Moves, Relocation{Model: n, Field: ref.Field, From: p, To: cand})
			if apply {
				ref.Set(cand)
				changed = true
			}
		}
		if changed {
			r.Models[n] = m
		}
	}
	return plan
}
