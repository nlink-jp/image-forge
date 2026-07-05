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
	Prompt    string   `json:"prompt"`
	Negative  *string  `json:"negative,omitempty"`
	Model     string   `json:"model,omitempty"`      // registry name
	ModelPath string   `json:"model_path,omitempty"` // direct path
	VAE       *string  `json:"vae,omitempty"`
	Output    string   `json:"output,omitempty"`
	Seed      *int64   `json:"seed,omitempty"`
	Steps     *int     `json:"steps,omitempty"`
	CFG       *float64 `json:"cfg,omitempty"`
	Width     *int     `json:"width,omitempty"`
	Height    *int     `json:"height,omitempty"`
	Sampler   *string  `json:"sampler,omitempty"`
	ClipSkip  *int     `json:"clip_skip,omitempty"`
	Batch     *int     `json:"batch,omitempty"`
	Init      string   `json:"init,omitempty"`
	Strength  *float64 `json:"strength,omitempty"`
	LoRAs     []string `json:"loras,omitempty"` // "path:weight"
}

// runServe is the resident mode: it loads a model once and renders many requests
// against it, reloading only when the requested model changes. This avoids
// paying the model load + Metal init on every generation.
func runServe(args []string) error {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	emit := func(ev engine.Event) { _ = enc.Encode(ev) }

	var (
		sess    engine.Session
		curPath string
		curVAE  string
	)
	defer func() {
		if sess != nil {
			sess.Close()
		}
	}()

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

		path, regVAE, prof, err := resolveModel(r.Model, r.ModelPath)
		if err != nil {
			emit(engine.Event{Kind: "error", Message: err.Error()})
			continue
		}
		loras, err := parseLoras(r.LoRAs)
		if err != nil {
			emit(engine.Event{Kind: "error", Message: err.Error()})
			continue
		}

		seed := int64(42)
		if r.Seed != nil {
			seed = *r.Seed
		}
		batch := 1
		if r.Batch != nil {
			batch = *r.Batch
		}
		strength := 0.6
		if r.Strength != nil {
			strength = *r.Strength
		}
		out := r.Output
		if out == "" {
			out = "out.png"
		}
		ov := genOverrides{
			Negative: r.Negative, Steps: r.Steps, CFG: r.CFG,
			Width: r.Width, Height: r.Height, Sampler: r.Sampler,
			ClipSkip: r.ClipSkip, VAE: r.VAE,
		}
		req := applyProfile(path, regVAE, r.Prompt, seed, batch, r.Init, strength, loras, out, prof, ov)

		// (Re)load the model only when it (or its VAE) changes.
		if sess == nil || path != curPath || req.VAEPath != curVAE {
			if sess != nil {
				sess.Close()
				sess = nil
			}
			emit(engine.Event{Kind: "load", Message: path})
			s, oerr := engine.Open(path, req.VAEPath)
			if oerr != nil {
				emit(engine.Event{Kind: "error", Message: oerr.Error()})
				continue
			}
			sess, curPath, curVAE = s, path, req.VAEPath
		}

		events := make(chan engine.Event, 8)
		done := make(chan struct{})
		go func() {
			for ev := range events {
				emit(ev)
			}
			close(done)
		}()
		if rerr := sess.Render(context.Background(), req, events); rerr != nil {
			// Render may have already emitted events; surface the failure too.
			events <- engine.Event{Kind: "error", Message: rerr.Error()}
		}
		close(events)
		<-done
	}
}
