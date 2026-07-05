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

// runGen parses `gen` flags, builds an engine.Request, and streams progress
// events (JSON lines) to stderr while the engine renders.
func runGen(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	var (
		prompt   = fs.String("p", "", "prompt (required)")
		negative = fs.String("n", "", "negative prompt")
		model    = fs.String("model", "", "path to model checkpoint (.safetensors / .gguf)")
		vae      = fs.String("vae", "", "path to an external VAE")
		out      = fs.String("o", "out.png", "output image path")
		seed     = fs.Int64("seed", 42, "seed")
		steps    = fs.Int("steps", 0, "sampling steps (0 = model/profile default)")
		cfg      = fs.Float64("cfg", 0, "CFG scale (0 = model/profile default)")
		width    = fs.Int("W", 0, "width (0 = default)")
		height   = fs.Int("H", 0, "height (0 = default)")
		sampler  = fs.String("sampler", "", "sampler: euler_a, euler, dpm++2m, ...")
		clipSkip = fs.Int("clip-skip", 0, "CLIP skip (0 = default)")
		batch    = fs.Int("batch", 1, "number of images")
	)
	var loraArgs multiFlag
	fs.Var(&loraArgs, "lora", "LoRA as <path>:<weight> (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *prompt == "" {
		return fmt.Errorf("gen: -p <prompt> is required")
	}
	loras, err := parseLoras(loraArgs)
	if err != nil {
		return err
	}

	eng, err := engine.New()
	if err != nil {
		return err
	}
	defer eng.Close()

	req := engine.Request{
		Prompt: *prompt, Negative: *negative,
		Seed: *seed, Steps: *steps, CFG: *cfg,
		Width: *width, Height: *height, Sampler: *sampler, ClipSkip: *clipSkip,
		Batch: *batch, ModelPath: *model, VAEPath: *vae, LoRAs: loras, Output: *out,
	}

	events := make(chan engine.Event, 8)
	errc := make(chan error, 1)
	go func() {
		errc <- eng.Generate(context.Background(), req, events)
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
