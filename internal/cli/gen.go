package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nlink-jp/image-forge/internal/engine"
)

// runGen resolves the model (registry name or direct path), layers the model's
// profile under any explicitly-set flags, then streams progress while rendering.
func runGen(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	var (
		prompt    = fs.String("p", "", "prompt (required)")
		negative  = fs.String("n", "", "negative prompt")
		modelName = fs.String("m", "", "installed model name (see `models list`)")
		modelPath = fs.String("model-path", "", "path to a model file (bypasses the registry)")
		vae       = fs.String("vae", "", "external VAE path (overrides the profile)")
		out       = fs.String("o", "out.png", "output image path")
		seed      = fs.Int64("seed", 42, "seed")
		steps     = fs.Int("steps", 0, "sampling steps (overrides the profile)")
		cfg       = fs.Float64("cfg", 0, "CFG scale (overrides the profile)")
		width     = fs.Int("W", 0, "width (overrides the profile)")
		height    = fs.Int("H", 0, "height (overrides the profile)")
		sampler   = fs.String("sampler", "", "sampler (overrides the profile)")
		clipSkip  = fs.Int("clip-skip", 0, "CLIP skip (overrides the profile)")
		batch     = fs.Int("batch", 1, "number of images")
		initImg   = fs.String("init", "", "init image for img2img (PNG/JPEG)")
		strength  = fs.Float64("strength", 0.6, "img2img denoise strength, 0..1 (with --init)")
	)
	var loraArgs multiFlag
	fs.Var(&loraArgs, "lora", "LoRA as <path>:<weight> (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *prompt == "" {
		return fmt.Errorf("gen: -p <prompt> is required")
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	loras, err := parseLoras(loraArgs)
	if err != nil {
		return err
	}

	path, regVAE, prof, err := resolveModel(*modelName, *modelPath)
	if err != nil {
		return fmt.Errorf("gen: %w", err)
	}

	// Explicitly-set flags override the profile.
	var ov genOverrides
	if set["n"] {
		ov.Negative = negative
	}
	if set["steps"] {
		ov.Steps = steps
	}
	if set["cfg"] {
		ov.CFG = cfg
	}
	if set["W"] {
		ov.Width = width
	}
	if set["H"] {
		ov.Height = height
	}
	if set["sampler"] {
		ov.Sampler = sampler
	}
	if set["clip-skip"] {
		ov.ClipSkip = clipSkip
	}
	if set["vae"] {
		ov.VAE = vae
	}
	req := applyProfile(path, regVAE, *prompt, *seed, *batch, *initImg, *strength, loras, *out, prof, ov)

	sess, err := engine.Open(path, req.VAEPath)
	if err != nil {
		return err
	}
	defer sess.Close()

	events := make(chan engine.Event, 8)
	errc := make(chan error, 1)
	go func() {
		errc <- sess.Render(context.Background(), req, events)
		close(events)
	}()

	enc := json.NewEncoder(os.Stderr)
	for ev := range events {
		_ = enc.Encode(ev)
		if ev.Kind == "done" {
			fmt.Fprintln(os.Stdout, ev.Output)
		}
	}
	return <-errc
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// parseLoras turns "path:weight" entries into engine.LoRA (weight defaults to 1).
func parseLoras(vals []string) ([]engine.LoRA, error) {
	var out []engine.LoRA
	for _, v := range vals {
		path, wStr, found := strings.Cut(v, ":")
		w := 1.0
		if found {
			f, err := strconv.ParseFloat(wStr, 64)
			if err != nil {
				return nil, fmt.Errorf("lora %q: invalid weight: %w", v, err)
			}
			w = f
		}
		out = append(out, engine.LoRA{Path: path, Weight: w})
	}
	return out, nil
}
