// Package profile defines model profiles — the per-model settings image-forge
// auto-applies so users never hand-tune clip-skip, VAE, resolution, sampler,
// prediction type, or quantization. Defaults vary per architecture.
package profile

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
		return Profile{Arch: a, Prediction: PredEps, Sampler: "euler", Steps: 8, CFG: 3.5, Width: 1024, Height: 1024, NegativeOK: true}
	default:
		return Profile{Arch: ArchUnknown, Prediction: PredEps, Sampler: "euler_a", Steps: 25, CFG: 7, Width: 512, Height: 512, NegativeOK: true}
	}
}
