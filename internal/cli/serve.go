package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

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
	Prediction *string  `json:"prediction,omitempty"`
	ClipSkip   *int     `json:"clip_skip,omitempty"`
	Batch      *int     `json:"batch,omitempty"`
	Init       string   `json:"init,omitempty"`
	Mask       string   `json:"mask,omitempty"`
	Strength   *float64 `json:"strength,omitempty"`
	LoRAs      []string `json:"loras,omitempty"` // "path:weight"
}

// runServe is the resident mode: it loads a model once and renders many requests
// against it, reloading only when the requested model changes. This avoids
// paying the model load + Metal init on every generation.
func runServe(args []string) error {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	emit := func(ev engine.Event) { _ = enc.Encode(ev) }

	var (
		sess   engine.Session
		curKey string // identity of the loaded model (path/components + vae + prediction)
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

		res, err := resolveModel(r.Model, r.ModelPath)
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
		req := applyProfile(res.Path, res.VAEPath, r.Prompt, seed, batch, r.Init, strength, loras, out, res.Profile, ov)
		req.Mask = r.Mask

		pred := predArg(res.Profile.Prediction)
		if r.Prediction != nil {
			pred = normPrediction(*r.Prediction)
		}

		op := engine.OpenParams{
			ModelPath:      res.Path,
			DiffusionModel: res.Components.DiffusionModel,
			ClipL:          res.Components.ClipL,
			ClipG:          res.Components.ClipG,
			T5XXL:          res.Components.T5XXL,
			VAEPath:        req.VAEPath,
			Prediction:     pred,
		}
		key := strings.Join([]string{op.ModelPath, op.DiffusionModel, op.ClipL, op.ClipG, op.T5XXL, op.VAEPath, op.Prediction}, "\x00")

		// (Re)load the model only when its identity changes.
		if sess == nil || key != curKey {
			if sess != nil {
				sess.Close()
				sess = nil
			}
			label := op.ModelPath
			if label == "" {
				label = op.DiffusionModel
			}
			emit(engine.Event{Kind: "load", Message: label})
			s, oerr := engine.Open(op)
			if oerr != nil {
				emit(engine.Event{Kind: "error", Message: oerr.Error()})
				continue
			}
			sess, curKey = s, key
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
