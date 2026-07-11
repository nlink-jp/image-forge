// Package engine wraps the diffusion runtime (stable-diffusion.cpp, statically
// linked via CGO under the cgo_sdcpp tag). The rest of the tool depends on the
// Engine interface, not the C bindings, so generation logic stays testable.
package engine

import (
	"context"
	"errors"
)

// ErrNoRuntime is returned by Open/Quantize when the binary was built without
// the diffusion runtime (i.e. without the cgo_sdcpp build tag). It is declared
// here — under both build tags — so callers can branch on it (errors.Is)
// regardless of how the binary was built; only the no-runtime Open actually
// returns it.
var ErrNoRuntime = errors.New("this build has no diffusion runtime: build with -tags cgo_sdcpp (requires cmake + Metal Toolchain + the sd.cpp submodule)")

// LoRA is a LoRA adapter applied at generation time.
type LoRA struct {
	Path   string
	Weight float64
}

// PNGText is one PNG text chunk carrying generation metadata. The CLI builds
// these (friendly model name, AUTOMATIC1111-style parameters, JSON record,
// binary version); the engine writes them into the PNG immediately after IHDR
// via encodePNGWithText. Keyword is the chunk keyword ("parameters",
// "image-forge"); Text is the value (UTF-8-safe — encoded as tEXt when Latin-1,
// else iTXt).
type PNGText struct {
	Keyword string
	Text    string
}

// Request is a single generation request (txt2img, or img2img when InitImage set).
type Request struct {
	Prompt    string
	Negative  string
	Seed      int64
	Steps     int
	CFG       float64
	Width     int
	Height    int
	Sampler   string
	Scheduler string
	ClipSkip  int
	Batch     int

	// Flow-matching / distilled guidance knobs for Flux & SD3.5. Each is 0 = leave
	// sd.cpp's default. Guidance is Flux-dev's distilled guidance (sd.cpp default
	// 3.5 — the most important knob for flux1-dev); FlowShift is the flow-matching
	// timestep shift (SD3.5 / Flux); SLGScale enables skip-layer guidance at the
	// standard SD3.5 skip layers (a quality knob, ~2.5 is good for SD3.5); ImgCFG is
	// a separate image CFG for img2img / instruct edits.
	Guidance  float64
	FlowShift float64
	SLGScale  float64
	ImgCFG    float64
	ModelPath string
	VAEPath   string
	LoRAs     []LoRA
	InitImage string  // img2img source; empty => txt2img
	Strength  float64 // img2img denoise strength
	Mask      string  // inpaint mask (requires InitImage): white = regenerate, black = keep

	ControlImage    string  // ControlNet guidance image (with an OpenParams.ControlNet model)
	ControlStrength float64 // ControlNet strength
	Canny           bool    // apply canny edge preprocessing to the control image

	// VAETiling decodes the final latent in overlapping tiles instead of in one
	// pass, trading a little speed (and near-invisible seams at the default 0.5
	// overlap) for a large drop in VAE-decode memory. It only matters at high
	// resolution / hires, where a full decode can OOM the 16 GB baseline. Off by
	// default (opt-in); native-resolution output is unaffected when it stays off.
	VAETiling bool

	// hires.fix: a second img2img pass at higher resolution that adds detail.
	// Disabled unless Hires is true. HiresScale/Denoise/Steps <= 0 leave sd.cpp's
	// (or the caller's) default; HiresUpscaler "" defaults to "latent".
	Hires         bool
	HiresScale    float64
	HiresDenoise  float64
	HiresUpscaler string // latent | lanczos | nearest | model
	HiresSteps    int
	HiresModel    string // ESRGAN model path, only used when HiresUpscaler == "model"

	// Metadata builds the PNG text chunks (tEXt/iTXt) to embed in the generated
	// image with the given seed, written immediately after IHDR. Built by the CLI
	// (which knows the friendly model name, prediction type, and binary version).
	// It is called once per output image — a batch of N produces seeds
	// base..base+N-1 (sd.cpp uses request.seed + b for the b-th image), so each
	// image records its *own* seed. nil, or a nil return, embeds nothing.
	Metadata func(seed int64) []PNGText

	Output string // output path; index is inserted before the extension for batches
}

// UpscaleParams configures a standalone ESRGAN super-resolution pass. Events is
// optional; when non-nil a "done" event carrying the output path is sent on it.
type UpscaleParams struct {
	InputPath  string       // source image (PNG/JPEG)
	ESRGANPath string       // Real-ESRGAN model file
	OutputPath string       // where to write the upscaled PNG
	Factor     int          // requested upscale factor (the model's native factor governs the actual output)
	Events     chan<- Event // optional progress/done sink; the caller must drain it
	Metadata   []PNGText    // text chunks written into the output PNG (light source/upscaler/factor record)
}

// Event is a progress event streamed during generation (serialized as one JSON
// line per event on stderr by the CLI).
type Event struct {
	Kind     string  `json:"kind"` // "load" | "progress" | "done" | "error"
	Progress float64 `json:"progress,omitempty"`
	Message  string  `json:"message,omitempty"`
	Output   string  `json:"output,omitempty"` // image path on "done"
	Seed     int64   `json:"seed,omitempty"`   // seed used, on "done"
}

// OpenParams configures a model load. Set either ModelPath (a single-file
// checkpoint) or DiffusionModel + the encoders (a multi-component model such as
// FLUX). VAEPath and Prediction ("" = auto-detect, "eps", "v") are optional.
type OpenParams struct {
	ModelPath      string
	DiffusionModel string
	ClipL          string
	ClipG          string
	T5XXL          string
	LLM            string // LLM text encoder (e.g. Qwen for Z-Image)
	VAEPath        string
	ControlNet     string // ControlNet model, loaded alongside the base model
	Prediction     string
	// FlashAttn enables flash attention (both the diffusion model and the text
	// encoders) at load time — a large attention-memory saving on the 16 GB
	// baseline. Default-on is decided by the caller (config/flag).
	FlashAttn bool
}

// Session is a loaded model, ready to render one or more requests. Open (defined
// per build tag) creates one. The resident `serve` mode keeps a Session alive
// across requests to avoid re-loading the model and re-initializing Metal.
type Session interface {
	Render(ctx context.Context, req Request, events chan<- Event) error
	Close() error
}
