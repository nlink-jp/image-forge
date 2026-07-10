package cli

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/config"
	"github.com/nlink-jp/image-forge/internal/engine"
	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

// resolveSeed returns a concrete seed: for seed < 0 it draws a random
// non-negative int64 (so a "random" run is still reported and reproducible);
// otherwise it returns seed unchanged.
func resolveSeed(seed int64) int64 {
	if seed >= 0 {
		return seed
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.BigEndian.Uint64(b[:]) >> 1)
}

// seededOutput inserts the seed before the extension when producing more than one
// image, so each file is traceable to its seed.
func seededOutput(base string, seed int64, count int) string {
	if count <= 1 {
		return base
	}
	if base == "" {
		base = "out.png"
	}
	ext := filepath.Ext(base)
	if ext == "" {
		ext = ".png"
	}
	return fmt.Sprintf("%s-%d%s", strings.TrimSuffix(base, ext), seed, ext)
}

// predArg maps a profile prediction type to the sd.cpp prediction string that
// engine.Open expects ("" = auto-detect from the model, "v" = force v-prediction).
func predArg(p profile.Prediction) string {
	if p == profile.PredVPred {
		return "v"
	}
	return ""
}

// normPrediction maps a user-supplied --prediction value to sd.cpp's string
// ("" = auto-detect, "eps", or "v").
func normPrediction(s string) string {
	switch strings.ToLower(s) {
	case "v", "vpred", "v-pred", "v_pred":
		return "v"
	case "eps", "epsilon":
		return "eps"
	default: // "auto" or empty => auto-detect
		return ""
	}
}

// resolved is a model ready to open: a single-file Path or multi-component
// Components, plus its VAE and base profile.
type resolved struct {
	Path       string
	VAEPath    string
	Components store.Components
	Profile    profile.Profile
	Kind       string // "" (diffusion) or "upscaler"
}

// resolveModel maps a registry name or a direct file path to a resolved model.
func resolveModel(modelName, modelPath string) (resolved, error) {
	switch {
	case modelName != "":
		reg, e := store.Load()
		if e != nil {
			return resolved{}, e
		}
		im, ok := reg.Get(modelName)
		if !ok {
			return resolved{}, fmt.Errorf("model %q is not installed (try: image-forge models pull %s)", modelName, modelName)
		}
		return resolved{Path: im.Path, VAEPath: im.VAEPath, Components: im.Components, Profile: im.Profile, Kind: im.Kind}, nil
	case modelPath != "":
		return resolved{Path: modelPath, Profile: profile.ArchDefaults(profile.Detect(filepath.Base(modelPath)))}, nil
	default:
		return resolved{}, fmt.Errorf("a model is required (-m <name> or --model-path <path>)")
	}
}

// installedUpscalers returns the installed upscaler-kind models as name -> path.
func installedUpscalers() map[string]string {
	reg, err := store.Load()
	if err != nil {
		return nil
	}
	m := map[string]string{}
	for name, im := range reg.Models {
		if im.IsUpscaler() {
			m[name] = im.Path
		}
	}
	return m
}

// resolveUpscalerModel maps an installed upscaler name or a direct file path to
// an ESRGAN model path for the standalone `upscale` command. With neither given
// it falls back to the config [upscaler] default_model, then to the sole
// installed upscaler. It rejects a diffusion model passed as --model.
// kindNoun renders a model kind for error messages.
func kindNoun(kind string) string {
	switch kind {
	case catalog.KindLoRA:
		return "LoRA"
	case catalog.KindControlNet:
		return "ControlNet model"
	case catalog.KindUpscaler:
		return "upscaler"
	default:
		return "diffusion model"
	}
}

// looksLikePath reports whether a --lora / --control-net value is a filesystem
// path rather than a bare registry name (it has a separator or a file extension).
func looksLikePath(s string) bool {
	return strings.ContainsRune(s, filepath.Separator) || filepath.Ext(s) != ""
}

// resolveAuxModel resolves a LoRA / ControlNet reference to a file path: an
// installed model of `kind` resolves by registry name; a value that looks like a
// path passes through unchanged (so existing path-based invocations keep
// working); a bare name that isn't installed is a clear error. `get` is the
// registry lookup, injected so this stays unit-testable. See ADR-0006.
func resolveAuxModel(ref, kind string, get func(string) (store.InstalledModel, bool)) (string, error) {
	if ref == "" {
		return "", nil
	}
	if im, ok := get(ref); ok {
		if im.Kind != kind {
			return "", fmt.Errorf("%q is a %s, not a %s", ref, kindNoun(im.Kind), kindNoun(kind))
		}
		return im.Path, nil
	}
	if looksLikePath(ref) {
		return ref, nil
	}
	return "", fmt.Errorf("%s %q is not installed (try: image-forge models pull %s)", kindNoun(kind), ref, ref)
}

// resolveAuxRefs resolves LoRA references (in place) and a ControlNet reference
// against the registry: registry names become installed paths, raw paths pass
// through. Shared by `gen`, the resident `serve` loop, and the MCP worker so all
// three accept installed names identically (ADR-0006).
func resolveAuxRefs(loras []engine.LoRA, controlNet string) ([]engine.LoRA, string, error) {
	reg, err := store.Load()
	if err != nil {
		return nil, "", err
	}
	get := func(n string) (store.InstalledModel, bool) { return reg.Get(n) }
	for i := range loras {
		p, err := resolveAuxModel(loras[i].Path, catalog.KindLoRA, get)
		if err != nil {
			return nil, "", fmt.Errorf("lora: %w", err)
		}
		loras[i].Path = p
	}
	cn, err := resolveAuxModel(controlNet, catalog.KindControlNet, get)
	if err != nil {
		return nil, "", fmt.Errorf("control-net: %w", err)
	}
	return loras, cn, nil
}

func resolveUpscalerModel(name, path string, conf config.Config) (string, error) {
	switch {
	case name != "":
		reg, err := store.Load()
		if err != nil {
			return "", err
		}
		im, ok := reg.Get(name)
		if !ok {
			return "", fmt.Errorf("upscaler %q is not installed (try: image-forge models pull %s)", name, name)
		}
		if !im.IsUpscaler() {
			return "", fmt.Errorf("model %q is a diffusion model, not an upscaler — pull one with `image-forge models pull realesrgan-x4plus`", name)
		}
		return im.Path, nil
	case path != "":
		return path, nil
	default:
		installed := installedUpscalers()
		if dm := conf.Upscaler.DefaultModel; dm != "" {
			if p, ok := installed[dm]; ok {
				return p, nil
			}
			return "", fmt.Errorf("config [upscaler] default_model %q is not an installed upscaler (pull it: image-forge models pull %s)", dm, dm)
		}
		if len(installed) == 1 {
			for _, p := range installed {
				return p, nil
			}
		}
		return "", fmt.Errorf("an upscaler model is required: pass --model <name> / --model-path <path>, or set [upscaler] default_model")
	}
}

// resolveHiresModel resolves the --hires-model reference (used with
// --hires-upscaler model): an installed upscaler name resolves to its path;
// anything else is treated as a raw file path. Empty in => empty out.
func resolveHiresModel(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	reg, err := store.Load()
	if err != nil {
		return "", err
	}
	if im, ok := reg.Get(ref); ok {
		if !im.IsUpscaler() {
			return "", fmt.Errorf("--hires-model %q is a diffusion model, not an upscaler", ref)
		}
		return im.Path, nil
	}
	if _, err := os.Stat(ref); err != nil {
		return "", fmt.Errorf("--hires-model %q is not an installed upscaler and is not a file (pull it: image-forge models pull %s)", ref, ref)
	}
	return ref, nil
}

// hiresOverrides carries explicit per-field hires overrides; a nil pointer (or
// empty Model) means "not overridden".
type hiresOverrides struct {
	Scale    *float64
	Denoise  *float64
	Upscaler *string
	Steps    *int
	Model    string // resolved ESRGAN path for --hires-upscaler model
}

// hiresResolved is the resolved hires configuration ready to copy into an
// engine.Request.
type hiresResolved struct {
	Enabled  bool
	Scale    float64
	Denoise  float64
	Upscaler string
	Steps    int
	Model    string
}

// hiresEnv is the config- and registry-derived context for hires upscaler
// selection: the config [hires] upscaler policy, the config [upscaler]
// default_model, and the installed upscaler models (name -> path).
type hiresEnv struct {
	ConfigUpscaler string
	DefaultModel   string
	Installed      map[string]string
}

// hiresEnvFromConfig builds a hiresEnv from config + the installed registry.
func hiresEnvFromConfig(conf config.Config) hiresEnv {
	return hiresEnv{
		ConfigUpscaler: conf.HiresUpscaler(),
		DefaultModel:   conf.Upscaler.DefaultModel,
		Installed:      installedUpscalers(),
	}
}

// pickUpscalerModel chooses the ESRGAN model path when a model is needed: an
// explicit (pre-resolved) --hires-model path wins; else the config
// default_model if installed; else the sole installed upscaler; else "".
func pickUpscalerModel(cliModel, defaultModel string, installed map[string]string) string {
	if cliModel != "" {
		return cliModel
	}
	if defaultModel != "" {
		if p, ok := installed[defaultModel]; ok {
			return p
		}
	}
	if len(installed) == 1 {
		for _, p := range installed {
			return p
		}
	}
	return ""
}

// resolveHiresUpscalerSpec turns an upscaler spec — latent|lanczos|nearest|model|
// auto|<installed-upscaler-name> — into the concrete engine upscaler mode and,
// for model-based upscaling, the ESRGAN model path. "auto" uses a downloaded
// ESRGAN if one is available, else latent; an unresolvable model request falls
// back to the built-in latent upscaler so generation never fails for a missing
// model.
func resolveHiresUpscalerSpec(spec, cliModel, defaultModel string, installed map[string]string) (mode, modelPath string) {
	switch strings.ToLower(spec) {
	case "lanczos":
		return "lanczos", ""
	case "nearest":
		return "nearest", ""
	case "latent":
		return "latent", ""
	case "model":
		if m := pickUpscalerModel(cliModel, defaultModel, installed); m != "" {
			return "model", m
		}
		return "latent", ""
	case "auto":
		if m := pickUpscalerModel(cliModel, defaultModel, installed); m != "" {
			return "model", m
		}
		return "latent", ""
	default:
		if p, ok := installed[spec]; ok {
			return "model", p
		}
		return "latent", ""
	}
}

// resolveHires resolves whether hires.fix runs and with what parameters.
// mode is "auto" (follow the profile), "on" (force enabled), or "off" (force
// disabled). When enabled, each parameter is filled from the override, else the
// profile, else image-forge's opinionated default. The upscaler is chosen at the
// highest priority that named one — CLI override, then profile, then config
// [hires] upscaler — then resolved (including "auto") against the installed
// upscalers via env.
func resolveHires(mode string, prof profile.Profile, ov hiresOverrides, env hiresEnv) hiresResolved {
	enabled := prof.HiresEnabled
	switch strings.ToLower(mode) {
	case "on":
		enabled = true
	case "off":
		enabled = false
	default: // "auto" or ""
		enabled = prof.HiresEnabled
	}
	r := hiresResolved{Enabled: enabled}
	if !enabled {
		return r
	}
	switch {
	case ov.Scale != nil:
		r.Scale = *ov.Scale
	case prof.HiresScale > 0:
		r.Scale = prof.HiresScale
	default:
		r.Scale = profile.DefaultHiresScale
	}
	switch {
	case ov.Denoise != nil:
		r.Denoise = *ov.Denoise
	case prof.HiresDenoise > 0:
		r.Denoise = prof.HiresDenoise
	default:
		r.Denoise = profile.DefaultHiresDenoise
	}
	// Upscaler spec priority: CLI override → profile → config [hires] upscaler →
	// the built-in default. The spec (which may be "auto" or an installed name) is
	// then resolved to a concrete engine upscaler + ESRGAN model path.
	spec := profile.DefaultHiresUpscaler // "latent"
	switch {
	case ov.Upscaler != nil && *ov.Upscaler != "":
		spec = *ov.Upscaler
	case prof.HiresUpscaler != "":
		spec = prof.HiresUpscaler
	case env.ConfigUpscaler != "":
		spec = env.ConfigUpscaler
	}
	r.Upscaler, r.Model = resolveHiresUpscalerSpec(spec, ov.Model, env.DefaultModel, env.Installed)
	switch {
	case ov.Steps != nil:
		r.Steps = *ov.Steps
	default:
		r.Steps = prof.HiresSteps // 0 => let sd.cpp derive the hires step count
	}
	return r
}

// genOverrides holds per-field overrides; a nil pointer means "use the profile".
type genOverrides struct {
	Negative *string
	Steps    *int
	CFG      *float64
	Width    *int
	Height   *int
	Sampler  *string
	ClipSkip *int
	VAE      *string
}

// applyProfile merges a base profile with overrides into an engine.Request. The
// profile supplies the per-model gotchas (clip-skip, resolution, sampler, prompt
// prefix, ...); non-nil overrides win.
func applyProfile(modelPath, regVAE, prompt string, seed int64, batch int, initImg string, strength float64, loras []engine.LoRA, output string, prof profile.Profile, ov genOverrides) engine.Request {
	finalPrompt := prompt
	if prof.PromptPrefix != "" {
		finalPrompt = prof.PromptPrefix + ", " + prompt
	}
	vae := regVAE
	if ov.VAE != nil {
		vae = *ov.VAE
	}
	neg := ""
	if ov.Negative != nil {
		neg = *ov.Negative
	}
	return engine.Request{
		Prompt:    finalPrompt,
		Negative:  neg,
		Seed:      seed,
		Batch:     batch,
		ModelPath: modelPath,
		VAEPath:   vae,
		LoRAs:     loras,
		Output:    output,
		InitImage: initImg,
		Strength:  strength,
		ClipSkip:  orInt(ov.ClipSkip, prof.ClipSkip),
		Steps:     orInt(ov.Steps, prof.Steps),
		Width:     orInt(ov.Width, prof.Width),
		Height:    orInt(ov.Height, prof.Height),
		CFG:       orFloat(ov.CFG, prof.CFG),
		Sampler:   orStr(ov.Sampler, prof.Sampler),
	}
}

func orInt(o *int, d int) int {
	if o != nil {
		return *o
	}
	return d
}

func orFloat(o *float64, d float64) float64 {
	if o != nil {
		return *o
	}
	return d
}

func orStr(o *string, d string) string {
	if o != nil {
		return *o
	}
	return d
}
