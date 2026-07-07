package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nlink-jp/image-forge/internal/config"
	"github.com/nlink-jp/image-forge/internal/engine"
)

// runUpscale runs a standalone ESRGAN super-resolution pass over an image:
//
//	image-forge upscale <input> -o <output> [--scale N] [--model <name> | --model-path <path>]
//
// The ESRGAN model resolves from an installed upscaler-kind model (--model) or a
// direct file (--model-path). Progress is streamed as JSON on stderr (like gen);
// the output path is printed on stdout.
func runUpscale(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("upscale: usage: image-forge upscale <input> -o <output> [--scale N] [--model <name> | --model-path <path>]")
	}
	input := args[0]

	fs := flag.NewFlagSet("upscale", flag.ContinueOnError)
	out := fs.String("o", "", "output image path (required)")
	modelName := fs.String("model", "", "installed upscaler model name (see `models list`)")
	modelPath := fs.String("model-path", "", "ESRGAN model file path (bypasses the registry)")
	scale := fs.Int("scale", 0, "upscale factor (default: the model's native factor, typically 4)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("upscale: -o <output> is required")
	}

	conf, err := config.Load()
	if err != nil {
		return fmt.Errorf("upscale: config: %w", err)
	}
	esrgan, err := resolveUpscalerModel(*modelName, *modelPath, conf)
	if err != nil {
		return fmt.Errorf("upscale: %w", err)
	}

	enc := json.NewEncoder(os.Stderr)
	events := make(chan engine.Event, 8)
	errc := make(chan error, 1)
	go func() {
		errc <- engine.Upscale(engine.UpscaleParams{
			InputPath:  input,
			ESRGANPath: esrgan,
			OutputPath: *out,
			Factor:     *scale,
			Events:     events,
		})
		close(events)
	}()
	for ev := range events {
		_ = enc.Encode(ev)
		if ev.Kind == "done" {
			fmt.Fprintln(os.Stdout, ev.Output)
		}
	}
	return <-errc
}
