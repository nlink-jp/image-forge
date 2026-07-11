package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"

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

	// Flow-matching / distilled guidance knobs (Flux & SD3.5); omit => sd.cpp default.
	Guidance  *float64 `json:"guidance,omitempty"`
	FlowShift *float64 `json:"flow_shift,omitempty"`
	SLGScale  *float64 `json:"slg_scale,omitempty"`
	ImgCFG    *float64 `json:"img_cfg,omitempty"`

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
		Guidance: r.Guidance, FlowShift: r.FlowShift, SLGScale: r.SLGScale, ImgCFG: r.ImgCFG,
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

	// SIGINT/SIGTERM cancels the in-flight render (the engine honors ctx) and then
	// serve exits, rather than the render running to completion or dying mid-op.
	// Closing stdin on shutdown also unblocks a Decode that is idle between
	// requests, so a single signal exits promptly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() { <-ctx.Done(); _ = os.Stdin.Close() }()

	re := NewResidentEngine()
	defer re.Close()

	emit(engine.Event{Kind: "ready", Message: "send one JSON request per line"})
	for {
		var r serveRequest
		if err := dec.Decode(&r); err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil // EOF, or shutdown (the signal handler closed stdin)
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
		_, _, rerr := re.Render(ctx, r.renderRequest(), events)
		if rerr != nil {
			// Render may have already emitted events; surface the failure too.
			// Carry the request's output so a front-end can free the exact in-flight
			// entry (an error otherwise has no key to remove it by).
			events <- engine.Event{Kind: "error", Message: rerr.Error(), Output: r.Output}
		}
		close(events)
		<-done
		if ctx.Err() != nil {
			return nil // shutdown signal: exit cleanly after the current render aborts
		}
	}
}
