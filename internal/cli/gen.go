package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nlink-jp/image-forge/internal/config"
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
		seed      = fs.Int64("seed", 42, "seed (-1 = random)")
		count     = fs.Int("count", 1, "number of images (with --seed -1, each gets a fresh random seed)")
		steps     = fs.Int("steps", 0, "sampling steps (overrides the profile)")
		cfg       = fs.Float64("cfg", 0, "CFG scale (overrides the profile)")
		width     = fs.Int("W", 0, "width (overrides the profile)")
		height    = fs.Int("H", 0, "height (overrides the profile)")
		sampler   = fs.String("sampler", "", "sampler (overrides the profile)")
		scheduler = fs.String("scheduler", "", "scheduler: discrete|karras|exponential|ays|... (default: sd.cpp default)")
		clipSkip  = fs.Int("clip-skip", 0, "CLIP skip (overrides the profile)")
		batch     = fs.Int("batch", 1, "images per run (sd.cpp batch, sequential seeds)")
		initImg   = fs.String("init", "", "init image for img2img (PNG/JPEG)")
		strength  = fs.Float64("strength", 0.6, "img2img denoise strength, 0..1 (with --init)")
		maskImg   = fs.String("mask", "", "inpaint mask, same size as --init (white=regenerate, black=keep)")
		predict   = fs.String("prediction", "", "prediction override: eps | v | auto (default: from profile)")
		ctrlNet   = fs.String("control-net", "", "ControlNet installed name or model path (loaded with the base model)")
		ctrlImg   = fs.String("control", "", "control image for ControlNet (with --control-net)")
		ctrlStr   = fs.Float64("control-strength", 0.9, "ControlNet strength")
		canny     = fs.Bool("canny", false, "apply canny edge preprocessing to the control image")

		hires         = fs.String("hires", "auto", "hires.fix: auto (follow the model profile) | on | off")
		hiresScale    = fs.Float64("hires-scale", 0, "hires upscale factor (default: profile or 1.5)")
		hiresDenoise  = fs.Float64("hires-denoise", 0, "hires denoise strength 0..1 (default: profile or 0.5)")
		hiresUpscaler = fs.String("hires-upscaler", "", "hires upscaler: latent|lanczos|nearest|model (default: profile or latent)")
		hiresModel    = fs.String("hires-model", "", "ESRGAN model (installed upscaler name or path) for --hires-upscaler model")

		flashAttn = fs.Bool("flash-attn", false, "flash attention: faster/leaner on large & hires renders (default off; also config [performance] flash_attn)")

		noMetadata = fs.Bool("no-metadata", false, "do not embed generation metadata (prompt/params/model) into the PNG")
	)
	var loraArgs multiFlag
	fs.Var(&loraArgs, "lora", "LoRA as <installed-name-or-path>:<weight> (repeatable)")
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
	// LoRA / ControlNet accept an installed registry name or a raw path (ADR-0006).
	loras, ctrlNetPath, err := resolveAuxRefs(loras, *ctrlNet)
	if err != nil {
		return err
	}

	conf, err := config.Load()
	if err != nil {
		return fmt.Errorf("gen: config: %w", err)
	}

	// Fall back to the configured default model when none is given.
	mName := *modelName
	if mName == "" && *modelPath == "" {
		mName = conf.DefaultModel
	}
	res, err := resolveModel(mName, *modelPath)
	if err != nil {
		return fmt.Errorf("gen: %w", err)
	}
	if res.Kind == "upscaler" {
		return fmt.Errorf("gen: %q is an upscaler, not a diffusion model — use `image-forge upscale`", mName)
	}

	outPath := *out
	if !set["o"] && conf.Output != "" {
		outPath = conf.Output
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
	req := applyProfile(res.Path, res.VAEPath, *prompt, *seed, *batch, *initImg, *strength, loras, outPath, res.Profile, ov)
	req.Mask = *maskImg
	req.ControlImage = *ctrlImg
	req.ControlStrength = *ctrlStr
	req.Canny = *canny
	req.Scheduler = *scheduler
	if err := validateSamplerScheduler(req.Sampler, req.Scheduler); err != nil {
		return fmt.Errorf("gen: %w", err)
	}

	// hires.fix: --hires auto|on|off drives whether it runs; the fine-grained
	// flags override the profile / opinionated defaults only when set.
	var hov hiresOverrides
	if set["hires-scale"] {
		hov.Scale = hiresScale
	}
	if set["hires-denoise"] {
		hov.Denoise = hiresDenoise
	}
	if set["hires-upscaler"] {
		hov.Upscaler = hiresUpscaler
	}
	hiresModelPath, err := resolveHiresModel(*hiresModel)
	if err != nil {
		return fmt.Errorf("gen: %w", err)
	}
	hov.Model = hiresModelPath
	hr := resolveHires(*hires, res.Profile, hov, hiresEnvFromConfig(conf))
	req.Hires = hr.Enabled
	req.HiresScale = hr.Scale
	req.HiresDenoise = hr.Denoise
	req.HiresUpscaler = hr.Upscaler
	req.HiresSteps = hr.Steps
	req.HiresModel = hr.Model

	pred := predArg(res.Profile.Prediction)
	if set["prediction"] {
		pred = normPrediction(*predict)
	}

	// Generation metadata is embedded by default; config [metadata] embed = false
	// or --no-metadata suppresses it. The concrete seed differs per image, so the
	// metadata is (re)built inside the render loop below.
	embed := conf.EmbedMetadata() && !*noMetadata
	metaModelName := modelDisplayName(mName, *modelPath)

	// Flash attention defaults from config; --flash-attn overrides for this run.
	flashOn := conf.FlashAttn()
	if set["flash-attn"] {
		flashOn = *flashAttn
	}

	sess, err := engine.Open(engine.OpenParams{
		ModelPath:      res.Path,
		DiffusionModel: res.Components.DiffusionModel,
		ClipL:          res.Components.ClipL,
		ClipG:          res.Components.ClipG,
		T5XXL:          res.Components.T5XXL,
		LLM:            res.Components.LLM,
		VAEPath:        req.VAEPath,
		ControlNet:     ctrlNetPath,
		Prediction:     pred,
		FlashAttn:      flashOn,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	n := *count
	if n < 1 {
		n = 1
	}
	enc := json.NewEncoder(os.Stderr)
	for i := 0; i < n; i++ {
		r := req
		if *seed < 0 {
			r.Seed = resolveSeed(-1) // a fresh random seed per image
		} else {
			r.Seed = *seed + int64(i) // sequential variations for a fixed seed
		}
		r.Output = seededOutput(outPath, r.Seed, n)
		// Per-image metadata so a --batch records each image's own seed (base+b).
		r.Metadata = metadataBuilder(r, metaModelName, pred, embed)

		events := make(chan engine.Event, 8)
		errc := make(chan error, 1)
		go func() {
			errc <- sess.Render(context.Background(), r, events)
			close(events)
		}()
		for ev := range events {
			_ = enc.Encode(ev)
			if ev.Kind == "done" {
				fmt.Fprintf(os.Stdout, "%s\tseed=%d\n", ev.Output, ev.Seed)
			}
		}
		if e := <-errc; e != nil {
			return e
		}
	}
	return nil
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
