package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/nlink-jp/image-forge/internal/engine"
)

// serveRequest is one line of the serve protocol (JSON per line on stdin).
// Pointer fields are overrides: absent => use the model profile's default.
type serveRequest struct {
	Prompt     string   `json:"prompt"`
	Negative   *string  `json:"negative,omitempty"`
	Model      string   `json:"model,omitempty"`      // registry name
	ModelPath  string   `json:"model_path,omitempty"` // direct path
	VAE        *string  `json:"vae,omitempty"`
	Output     string   `json:"output,omitempty"`
	Seed       *int64   `json:"seed,omitempty"`
	Steps      *int     `json:"steps,omitempty"`
	CFG        *float64 `json:"cfg,omitempty"`
	Width      *int     `json:"width,omitempty"`
	Height     *int     `json:"height,omitempty"`
	Sampler    *string  `json:"sampler,omitempty"`
	Scheduler  *string  `json:"scheduler,omitempty"`
	Prediction *string  `json:"prediction,omitempty"`
	ClipSkip   *int     `json:"clip_skip,omitempty"`
	Batch      *int     `json:"batch,omitempty"`
	Init       string   `json:"init,omitempty"`
	Mask       string   `json:"mask,omitempty"`
	Strength   *float64 `json:"strength,omitempty"`
	LoRAs      []string `json:"loras,omitempty"` // "path:weight"

	ControlNet      string   `json:"control_net,omitempty"` // ControlNet model path (ctx-level)
	Control         string   `json:"control,omitempty"`     // control image
	ControlStrength *float64 `json:"control_strength,omitempty"`
	Canny           bool     `json:"canny,omitempty"`

	Hires         string   `json:"hires,omitempty"` // "auto" | "on" | "off"
	HiresScale    *float64 `json:"hires_scale,omitempty"`
	HiresDenoise  *float64 `json:"hires_denoise,omitempty"`
	HiresUpscaler *string  `json:"hires_upscaler,omitempty"`
	HiresModel    string   `json:"hires_model,omitempty"` // ESRGAN path (with hires_upscaler=model)
}

// renderRequest maps this serve line onto the shared cli.RenderRequest.
func (r serveRequest) renderRequest() RenderRequest {
	return RenderRequest{
		Prompt: r.Prompt, Negative: r.Negative, Model: r.Model, ModelPath: r.ModelPath,
		VAE: r.VAE, Output: r.Output, Seed: r.Seed, Steps: r.Steps, CFG: r.CFG,
		Width: r.Width, Height: r.Height, Sampler: r.Sampler, Scheduler: r.Scheduler,
		Prediction: r.Prediction, ClipSkip: r.ClipSkip, Batch: r.Batch,
		Init: r.Init, Mask: r.Mask, Strength: r.Strength, LoRAs: r.LoRAs,
		ControlNet: r.ControlNet, Control: r.Control, ControlStrength: r.ControlStrength, Canny: r.Canny,
		Hires: r.Hires, HiresScale: r.HiresScale, HiresDenoise: r.HiresDenoise,
		HiresUpscaler: r.HiresUpscaler, HiresModel: r.HiresModel,
	}
}

// runServe is the resident mode: it loads a model once and renders many requests
// against it, reloading only when the requested model changes. This avoids
// paying the model load + Metal init on every generation. The resident-session
// and request-building logic lives in ResidentEngine (render.go), shared with
// the MCP render worker.
func runServe(args []string) error {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	emit := func(ev engine.Event) { _ = enc.Encode(ev) }

	re := NewResidentEngine()
	defer re.Close()

	emit(engine.Event{Kind: "ready", Message: "send one JSON request per line"})
	for {
		var r serveRequest
		if err := dec.Decode(&r); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			emit(engine.Event{Kind: "error", Message: "bad request: " + err.Error()})
			return err // the JSON stream position is unreliable after a decode error
		}
		if r.Prompt == "" {
			emit(engine.Event{Kind: "error", Message: "prompt is required"})
			continue
		}

		events := make(chan engine.Event, 8)
		done := make(chan struct{})
		go func() {
			for ev := range events {
				emit(ev)
			}
			close(done)
		}()
		_, _, rerr := re.Render(context.Background(), r.renderRequest(), events)
		if rerr != nil {
			// Render may have already emitted events; surface the failure too.
			events <- engine.Event{Kind: "error", Message: rerr.Error()}
		}
		close(events)
		<-done
	}
}
