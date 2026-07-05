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
	Output    string  // output path; index is inserted before the extension for batches
}

// Event is a progress event streamed during generation (serialized as one JSON
// line per event on stderr by the CLI).
type Event struct {
	Kind     string  `json:"kind"` // "load" | "progress" | "done" | "error"
	Progress float64 `json:"progress,omitempty"`
	Message  string  `json:"message,omitempty"`
	Output   string  `json:"output,omitempty"` // image path on "done"
}

// Engine renders images. Implementations: the CGO sd.cpp binding (real, Phase 1
// build spike) and a fake used in tests.
type Engine interface {
	Generate(ctx context.Context, req Request, events chan<- Event) error
	Close() error
}
