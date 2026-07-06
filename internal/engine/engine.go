// Package engine wraps the diffusion runtime (stable-diffusion.cpp, statically
// linked via CGO under the cgo_sdcpp tag). The rest of the tool depends on the
// Engine interface, not the C bindings, so generation logic stays testable.
package engine

import "context"

// LoRA is a LoRA adapter applied at generation time.
type LoRA struct {
	Path   string
	Weight float64
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
	ClipSkip  int
	Batch     int
	ModelPath string
	VAEPath   string
	LoRAs     []LoRA
	InitImage string  // img2img source; empty => txt2img
	Strength  float64 // img2img denoise strength
	Mask      string  // inpaint mask (requires InitImage): white = regenerate, black = keep

	ControlImage    string  // ControlNet guidance image (with an OpenParams.ControlNet model)
	ControlStrength float64 // ControlNet strength
	Canny           bool    // apply canny edge preprocessing to the control image

	Output string // output path; index is inserted before the extension for batches
}

// Event is a progress event streamed during generation (serialized as one JSON
// line per event on stderr by the CLI).
type Event struct {
	Kind     string  `json:"kind"` // "load" | "progress" | "done" | "error"
	Progress float64 `json:"progress,omitempty"`
	Message  string  `json:"message,omitempty"`
	Output   string  `json:"output,omitempty"` // image path on "done"
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
}

// Session is a loaded model, ready to render one or more requests. Open (defined
// per build tag) creates one. The resident `serve` mode keeps a Session alive
// across requests to avoid re-loading the model and re-initializing Metal.
type Session interface {
	Render(ctx context.Context, req Request, events chan<- Event) error
	Close() error
}
