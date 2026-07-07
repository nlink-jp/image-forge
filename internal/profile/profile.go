// Package profile defines model profiles — the per-model settings image-forge
// auto-applies so users never hand-tune clip-skip, VAE, resolution, sampler,
// prediction type, or quantization. Defaults vary per architecture.
package profile

import "strings"

// Arch is a model architecture family.
type Arch string

const (
	ArchSD15    Arch = "sd15"
	ArchSDXL    Arch = "sdxl"
	ArchSD35    Arch = "sd35"
	ArchFlux    Arch = "flux"
	ArchZImage  Arch = "zimage"
	ArchUnknown Arch = "unknown"
)

// Prediction is the noise-prediction parameterization.
type Prediction string

const (
	PredEps   Prediction = "eps"   // epsilon-prediction — reliably supported
	PredVPred Prediction = "vpred" // v-prediction (+ZSNR) — experimental in sd.cpp
)

// Rating mirrors Civitai's content taxonomy. questionable/explicit need opt-in.
type Rating string

const (
	RatingSafe         Rating = "safe"
	RatingQuestionable Rating = "questionable"
	RatingExplicit     Rating = "explicit"
)

// image-forge's opinionated hires.fix defaults — applied when hires is on but a
// specific parameter is left unset. More conservative than sd.cpp's own defaults
// (scale 2.0 / denoise 0.7): 1.5/0.5 keeps the 16 GB baseline usable and stays
// closer to the base composition. The latent upscaler needs no extra download.
const (
	DefaultHiresUpscaler = "latent"
	DefaultHiresScale    = 1.5
	DefaultHiresDenoise  = 0.5
)

// Profile is the resolved generation settings for a model.
type Profile struct {
	Name         string
	Arch         Arch
	Prediction   Prediction
	VAE          string // path/ref to a dedicated VAE (e.g. sdxl fp16-fix)
	ClipSkip     int
	Sampler      string
	Steps        int
	CFG          float64
	Width        int
	Height       int
	PromptPrefix string // e.g. "score_9, score_8_up, ..." for Pony-family
	NegativeOK   bool   // false for distilled models (e.g. FLUX schnell)

	// hires.fix defaults for this model. HiresEnabled makes `gen` produce
	// hires-quality output with no extra flags (the per-model "always use hires"
	// recommendation, hidden in the profile). The rest are the model's preferred
	// values; a zero value falls back to the Default* constants above.
	HiresEnabled  bool
	HiresScale    float64
	HiresDenoise  float64
	HiresUpscaler string // latent | lanczos | nearest | model
	HiresSteps    int
}

// Detect guesses an architecture from a model name or filename. It defaults to
// SDXL (the primary target) when nothing matches.
func Detect(name string) Arch {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "sd15"), strings.Contains(n, "v1-5"), strings.Contains(n, "v1.5"), strings.Contains(n, "sd-1.5"):
		return ArchSD15
	case strings.Contains(n, "sd3"), strings.Contains(n, "sd-3"), strings.Contains(n, "3.5"):
		return ArchSD35
	case strings.Contains(n, "flux"):
		return ArchFlux
	case strings.Contains(n, "z-image"), strings.Contains(n, "zimage"):
		return ArchZImage
	case strings.Contains(n, "xl"), strings.Contains(n, "pony"), strings.Contains(n, "illustrious"), strings.Contains(n, "animagine"), strings.Contains(n, "noob"):
		return ArchSDXL
	default:
		return ArchSDXL
	}
}

// ArchDefaults returns baseline settings for an architecture; catalog entries
// and user flags layer on top.
func ArchDefaults(a Arch) Profile {
	switch a {
	case ArchSDXL:
		return Profile{Arch: a, Prediction: PredEps, ClipSkip: 2, Sampler: "euler_a", Steps: 25, CFG: 7, Width: 1024, Height: 1024, NegativeOK: true}
	case ArchSD15:
		return Profile{Arch: a, Prediction: PredEps, ClipSkip: 1, Sampler: "euler_a", Steps: 25, CFG: 7, Width: 512, Height: 512, NegativeOK: true}
	case ArchSD35:
		return Profile{Arch: a, Prediction: PredEps, Sampler: "euler", Steps: 28, CFG: 4.5, Width: 1024, Height: 1024, NegativeOK: true}
	case ArchFlux:
		// Distilled: guidance ~1, no negative prompt, few steps (schnell).
		return Profile{Arch: a, Prediction: PredEps, Sampler: "euler", Steps: 4, CFG: 1, Width: 1024, Height: 1024, NegativeOK: false}
	case ArchZImage:
		// Turbo: distilled, low guidance.
		return Profile{Arch: a, Prediction: PredEps, Sampler: "euler", Steps: 8, CFG: 1, Width: 1024, Height: 1024, NegativeOK: false}
	default:
		return Profile{Arch: ArchUnknown, Prediction: PredEps, Sampler: "euler_a", Steps: 25, CFG: 7, Width: 512, Height: 512, NegativeOK: true}
	}
}
