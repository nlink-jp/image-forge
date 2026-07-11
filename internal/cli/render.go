package cli

import (
	"context"
	"strings"

	"github.com/nlink-jp/image-forge/internal/config"
	"github.com/nlink-jp/image-forge/internal/engine"
)

// RenderRequest is one generation request in engine-neutral terms. It mirrors
// the resident `serve` line protocol (serveRequest) but is exported so other
// packages (the MCP server) can drive the shared resident engine. Pointer
// fields are overrides: nil => use the model profile's default.
type RenderRequest struct {
	Prompt     string
	Negative   *string
	Model      string // registry name
	ModelPath  string // direct path (bypasses the registry)
	VAE        *string
	Output     string
	Seed       *int64
	Steps      *int
	CFG        *float64
	Width      *int
	Height     *int
	Sampler    *string
	Scheduler  *string
	Prediction *string
	ClipSkip   *int
	Batch      *int
	Init       string
	Mask       string
	Strength   *float64
	LoRAs      []string // "path:weight"

	ControlNet      string
	Control         string
	ControlStrength *float64
	Canny           bool

	// hires.fix. Hires is the mode: "" / "auto" (follow the profile), "on", "off".
	// The pointer/Model fields are fine-grained overrides (nil => use the profile
	// or the opinionated default). HiresModel is a resolved ESRGAN path.
	Hires         string
	HiresScale    *float64
	HiresDenoise  *float64
	HiresUpscaler *string
	HiresModel    string
}

// buildRender resolves the model and merges profile + overrides into an
// engine.Request plus the engine.OpenParams and a reload key identifying the
// model. Shared by the resident `serve` loop and the MCP render worker so their
// request-building and reload semantics stay identical.
func buildRender(r RenderRequest) (engine.Request, engine.OpenParams, string, int64, error) {
	res, err := resolveModel(r.Model, r.ModelPath)
	if err != nil {
		return engine.Request{}, engine.OpenParams{}, "", 0, err
	}
	loras, err := parseLoras(r.LoRAs)
	if err != nil {
		return engine.Request{}, engine.OpenParams{}, "", 0, err
	}
	// LoRA / ControlNet may be given as registry names or as raw paths (ADR-0006).
	loras, controlNet, err := resolveAuxRefs(loras, r.ControlNet)
	if err != nil {
		return engine.Request{}, engine.OpenParams{}, "", 0, err
	}

	seed := int64(42)
	if r.Seed != nil {
		seed = *r.Seed
	}
	seed = resolveSeed(seed) // -1 => a concrete random seed (reported back)
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
	if r.Scheduler != nil {
		req.Scheduler = *r.Scheduler
	}
	if err := validateSamplerScheduler(req.Sampler, req.Scheduler); err != nil {
		return engine.Request{}, engine.OpenParams{}, "", 0, err
	}
	req.Mask = r.Mask
	req.ControlImage = r.Control
	req.Canny = r.Canny
	req.ControlStrength = 0.9
	if r.ControlStrength != nil {
		req.ControlStrength = *r.ControlStrength
	}

	hiresModelPath, herr := resolveHiresModel(r.HiresModel)
	if herr != nil {
		return engine.Request{}, engine.OpenParams{}, "", 0, herr
	}
	conf, _ := config.Load()
	hr := resolveHires(r.Hires, res.Profile, hiresOverrides{
		Scale: r.HiresScale, Denoise: r.HiresDenoise, Upscaler: r.HiresUpscaler, Model: hiresModelPath,
	}, hiresEnvFromConfig(conf))
	req.Hires = hr.Enabled
	req.HiresScale = hr.Scale
	req.HiresDenoise = hr.Denoise
	req.HiresUpscaler = hr.Upscaler
	req.HiresSteps = hr.Steps
	req.HiresModel = hr.Model

	pred := predArg(res.Profile.Prediction)
	if r.Prediction != nil {
		pred = normPrediction(*r.Prediction)
	}

	// Embed generation metadata into the PNG unless config [metadata] embed =
	// false. serve/mcp have no per-call opt-out flag; the config governs.
	if conf.EmbedMetadata() {
		req.Metadata = metadataBuilder(req, modelDisplayName(r.Model, r.ModelPath), pred, true)
	}

	op := engine.OpenParams{
		ModelPath:      res.Path,
		DiffusionModel: res.Components.DiffusionModel,
		ClipL:          res.Components.ClipL,
		ClipG:          res.Components.ClipG,
		T5XXL:          res.Components.T5XXL,
		LLM:            res.Components.LLM,
		VAEPath:        req.VAEPath,
		ControlNet:     controlNet,
		Prediction:     pred,
		// Flash attention is a process-global setting (config), constant across
		// requests, so it stays out of reloadKey — it never triggers a reload.
		FlashAttn: conf.FlashAttn(),
	}
	key := reloadKey(op)
	return req, op, key, seed, nil
}

// reloadKey is the identity of a loaded model: joining the model files,
// components, VAE, ControlNet, and prediction. The resident engine reloads only
// when this key changes.
func reloadKey(op engine.OpenParams) string {
	return strings.Join([]string{
		op.ModelPath, op.DiffusionModel, op.ClipL, op.ClipG, op.T5XXL,
		op.LLM, op.VAEPath, op.ControlNet, op.Prediction,
	}, "\x00")
}

// ResidentEngine keeps one loaded model alive across renders, reloading only
// when the requested model's identity changes. This avoids paying the model
// load + Metal init on every generation. It is NOT safe for concurrent use: the
// underlying Metal engine is not concurrent-safe, so callers must serialize
// Render (the resident `serve` loop is inherently serial; the MCP worker uses a
// single goroutine).
type ResidentEngine struct {
	sess   engine.Session
	curKey string
}

// NewResidentEngine returns an empty resident engine (no model loaded yet).
func NewResidentEngine() *ResidentEngine { return &ResidentEngine{} }

// Close releases the loaded session, if any.
func (e *ResidentEngine) Close() {
	if e.sess != nil {
		e.sess.Close()
		e.sess = nil
		e.curKey = ""
	}
}

// Render builds the request from r, (re)loads the model only when its identity
// changes, and streams progress to events (which the caller owns and closes).
// It returns the output path and the concrete seed used. events may be nil to
// discard progress.
func (e *ResidentEngine) Render(ctx context.Context, r RenderRequest, events chan<- engine.Event) (string, int64, error) {
	req, op, key, seed, err := buildRender(r)
	if err != nil {
		return "", 0, err
	}

	if e.sess == nil || key != e.curKey {
		if e.sess != nil {
			e.sess.Close()
			e.sess = nil
			e.curKey = ""
		}
		if events != nil {
			label := op.ModelPath
			if label == "" {
				label = op.DiffusionModel
			}
			events <- engine.Event{Kind: "load", Message: label}
		}
		s, oerr := engine.Open(op)
		if oerr != nil {
			return "", 0, oerr
		}
		e.sess, e.curKey = s, key
	}

	if err := e.sess.Render(ctx, req, events); err != nil {
		return "", 0, err
	}
	return req.Output, seed, nil
}
