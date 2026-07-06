package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nlink-jp/image-forge/internal/engine"
	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

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
		return resolved{Path: im.Path, VAEPath: im.VAEPath, Components: im.Components, Profile: im.Profile}, nil
	case modelPath != "":
		return resolved{Path: modelPath, Profile: profile.ArchDefaults(profile.Detect(filepath.Base(modelPath)))}, nil
	default:
		return resolved{}, fmt.Errorf("a model is required (-m <name> or --model-path <path>)")
	}
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
