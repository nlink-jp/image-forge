// Package profile defines model profiles — the per-model settings image-forge
// auto-applies so users never hand-tune clip-skip, VAE, resolution, sampler,
// prediction type, or quantization. Defaults vary per architecture.
package profile

import (
	"fmt"
	"math"
	"strings"
)

// Arch is a model architecture family.
type Arch string

const (
	ArchSD15   Arch = "sd15"
	ArchSDXL   Arch = "sdxl"
	ArchSD35   Arch = "sd35"
	ArchFlux   Arch = "flux"
	ArchZImage Arch = "zimage"
	// ArchAnima is CircleStone Labs / Comfy Org's 2B anime model — its own
	// architecture (sd.cpp: VERSION_ANIMA, Qwen conditioner), not an SDXL derivative.
	ArchAnima Arch = "anima"
	// ArchQwenImage21 is Qwen-Image-2.1 (sd.cpp: VERSION_QWEN_IMAGE_2_1): a 7B
	// single-stream DiT conditioned by the Qwen3-VL-8B LLM, with its own VAE.
	// It shares nothing loadable with Qwen-Image 1.x — neither the DiT nor the
	// VAE — so it is an architecture of its own, not a "qwen" family.
	ArchQwenImage21 Arch = "qwenimage21"
	ArchUnknown     Arch = "unknown"
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
	// Before the "xl"/"3.5" substring cases below: a name such as
	// "qwen_image_2.1-Q4_K" must not fall through to the SDXL default.
	case strings.Contains(n, "qwen-image-2.1"), strings.Contains(n, "qwen_image_2.1"), strings.Contains(n, "qwenimage21"):
		return ArchQwenImage21
	case strings.Contains(n, "sd15"), strings.Contains(n, "v1-5"), strings.Contains(n, "v1.5"), strings.Contains(n, "sd-1.5"):
		return ArchSD15
	case strings.Contains(n, "sd3"), strings.Contains(n, "sd-3"), strings.Contains(n, "3.5"):
		return ArchSD35
	case strings.Contains(n, "flux"):
		return ArchFlux
	case strings.Contains(n, "z-image"), strings.Contains(n, "zimage"):
		return ArchZImage
	// "animagine" is an SDXL model whose name contains "anima" — it must be
	// matched BEFORE the Anima architecture below, or every Animagine checkpoint
	// would be misdetected as Anima.
	case strings.Contains(n, "animagine"):
		return ArchSDXL
	case strings.Contains(n, "anima"):
		return ArchAnima
	case strings.Contains(n, "xl"), strings.Contains(n, "pony"), strings.Contains(n, "illustrious"), strings.Contains(n, "noob"):
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
	case ArchAnima:
		// The turbo release we ship is distilled: "use with CFG 1 and 8-12 steps".
		// At CFG 1 a negative prompt has no effect, so don't invite one.
		return Profile{Arch: a, Prediction: PredEps, Sampler: "euler", Steps: 10, CFG: 1, Width: 1024, Height: 1024, NegativeOK: false}
	case ArchQwenImage21:
		// Not distilled: true CFG with a negative prompt. Sampler and CFG are the
		// values of sd.cpp's own Qwen-Image-2.1 guide (euler, 6.0); 40 steps is
		// the model card's example. 1024x1024 is the default because it renders
		// correctly and much larger does not (see MaxPixels).
		return Profile{Arch: a, Prediction: PredEps, Sampler: "euler", Steps: 40, CFG: 6, Width: 1024, Height: 1024, NegativeOK: true}
	default:
		return Profile{Arch: ArchUnknown, Prediction: PredEps, Sampler: "euler_a", Steps: 25, CFG: 7, Width: 512, Height: 512, NegativeOK: true}
	}
}

// MaxPixels is the largest image area (width x height) an architecture is
// known to render correctly, or 0 when no limit is known.
//
// Qwen-Image-2.1 turns one 16x16 pixel block into one token, and sd.cpp's
// schedule for it is defined up to 8192 tokens (FluxScheduler's
// max_image_seq_len). Renders were checked on both sides of that: 1024x1024
// (4096 tokens), 1280x1280 (6400) and 1440x1440 (8100) are correct; 1536x1536 (9216) and
// 2048x2048 (16384) come out blurred and banded and ignore the prompt, after
// 35 and 51 minutes. Why it breaks past the limit was not established, so the
// limit is the scheduler's own number and not a finer one.
func MaxPixels(a Arch) int {
	if a == ArchQwenImage21 {
		return 8192 * 16 * 16
	}
	return 0
}

// CheckSize refuses a render the architecture is known to draw broken: a base
// size, or a hires.fix target (the base size times hiresScale; pass 0 when
// hires is off), larger than MaxPixels. An architecture with no known limit,
// and an unknown one (""), are never refused.
func CheckSize(a Arch, width, height int, hiresScale float64) error {
	limit := MaxPixels(a)
	if limit == 0 {
		return nil
	}
	side := int(math.Sqrt(float64(limit))) / 32 * 32
	if width*height > limit {
		return fmt.Errorf("%dx%d is too large for %s: it renders a broken image above %d pixels (%dx%d fits)",
			width, height, a, limit, side, side)
	}
	if hiresScale > 0 {
		// sd.cpp rounds each side of the target the same way.
		tw, th := int(math.Round(float64(width)*hiresScale)), int(math.Round(float64(height)*hiresScale))
		if tw*th > limit {
			return fmt.Errorf("hires.fix would render %dx%d (%dx%d x %g), too large for %s: it renders a broken image above %d pixels; lower the hires scale or the size, or turn hires off",
				tw, th, width, height, hiresScale, a, limit)
		}
	}
	return nil
}
