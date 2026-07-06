package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/config"
	"github.com/nlink-jp/image-forge/internal/download"
	"github.com/nlink-jp/image-forge/internal/engine"
	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

func runModels(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("models: expected a subcommand (list|import|pull|quantize|rm)")
	}
	switch args[0] {
	case "list":
		return modelsList()
	case "import":
		return modelsImport(args[1:])
	case "pull":
		return modelsPull(args[1:])
	case "rm":
		return modelsRm(args[1:])
	case "quantize":
		return modelsQuantize(args[1:])
	default:
		return fmt.Errorf("models: unknown subcommand %q", args[0])
	}
}

func modelsList() error {
	reg, err := store.Load()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tARCH\tRATING\tRAM\tLICENSE\tSTATUS")
	for _, e := range catalog.Default() {
		status := "available"
		if _, ok := reg.Get(e.Name); ok {
			status = "installed"
		}
		if e.Experimental {
			status += " (experimental)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%dGB\t%s\t%s\n",
			e.Name, e.Arch, e.Rating, e.MinRAMGB, e.RecRAMGB, e.License, status)
	}
	for name, m := range reg.Models {
		if _, ok := catalog.Find(name); ok {
			continue // already shown above
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t-\t%s\t%s\n", name, m.Profile.Arch, m.Rating, m.License, "installed")
	}
	return w.Flush()
}

func modelsImport(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("models import: usage: models import <path> [--name N] [--arch A] [--vae V]")
	}
	pathArg := args[0]
	fs := flag.NewFlagSet("models import", flag.ContinueOnError)
	name := fs.String("name", "", "registry name (default: file base name)")
	archFlag := fs.String("arch", "", "architecture: sdxl|sd15|sd35|flux|zimage (default: auto-detect)")
	vae := fs.String("vae", "", "path to an external VAE")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	abs, err := filepath.Abs(pathArg)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("models import: %w", err)
	}

	nm := *name
	if nm == "" {
		nm = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	}
	arch := profile.Detect(nm)
	if *archFlag != "" {
		arch = profile.Arch(*archFlag)
	}
	prof := profile.ArchDefaults(arch)
	prof.Name = nm

	reg, err := store.Load()
	if err != nil {
		return err
	}
	reg.Add(store.InstalledModel{Name: nm, Path: abs, VAEPath: *vae, Profile: prof, Rating: profile.RatingSafe})
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("imported %q (%s) -> %s\n", nm, arch, abs)
	return nil
}

func modelsPull(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("models pull: usage: models pull <name|hf:owner/repo/file|url> [--allow-nsfw] [--name N]")
	}
	ref := args[0]
	fs := flag.NewFlagSet("models pull", flag.ContinueOnError)
	allowNSFWFlag := fs.Bool("allow-nsfw", false, "allow pulling questionable/explicit models")
	nameOverride := fs.String("name", "", "override the registry name")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	conf, err := config.Load()
	if err != nil {
		return fmt.Errorf("models pull: config: %w", err)
	}
	allowNSFW := *allowNSFWFlag || conf.AllowNSFW

	srcRef := ref
	var (
		prof    profile.Profile
		rating  profile.Rating
		license string
		vaeSrc  string
		regName = *nameOverride
		known   bool
	)
	if e, ok := catalog.Find(ref); ok {
		known = true
		if e.NeedsOptIn() && !allowNSFW {
			return fmt.Errorf("models pull: %q is rated %q; re-run with --allow-nsfw or set allow_nsfw in config", e.Name, e.Rating)
		}
		if e.Experimental {
			fmt.Fprintf(os.Stderr, "warning: %q is experimental: %s\n", e.Name, e.Notes)
		}
		if regName == "" {
			regName = e.Name
		}
		if e.IsMultiComponent() {
			return pullMultiComponent(e, regName, conf)
		}
		switch {
		case e.Source.HF != "":
			srcRef = "hf:" + e.Source.HF
		case e.Source.Civitai != "":
			srcRef = "civitai:" + e.Source.Civitai
		case e.Source.URL != "":
			srcRef = e.Source.URL
		default:
			return fmt.Errorf("models pull: catalog entry %q has no downloadable source", e.Name)
		}
		prof, rating, license, vaeSrc = e.Profile(), e.Rating, e.License, e.Source.VAE
		if regName == "" {
			regName = e.Name
		}
	}

	var (
		url        string
		filename   string
		fetchToken string
	)
	if strings.HasPrefix(srcRef, "civitai:") {
		vid := strings.TrimPrefix(srcRef, "civitai:")
		ctok := conf.ResolveCivitaiToken()
		if ctok == "" {
			return fmt.Errorf("models pull: Civitai downloads require a token — set CIVITAI_TOKEN or civitai_token in config")
		}
		url, filename, err = download.CivitaiResolve(vid, ctok)
		if err != nil {
			return err
		}
		// token is embedded in the URL; don't also send a Bearer header
	} else {
		url, filename, err = download.Resolve(srcRef)
		if err != nil {
			if known {
				return fmt.Errorf("models pull: %q is not directly pullable yet (%w); use `models import` with a local file", ref, err)
			}
			return err
		}
		fetchToken = conf.ResolveHFToken()
	}
	if regName == "" {
		regName = strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	if !known {
		prof = profile.ArchDefaults(profile.Detect(regName))
		prof.Name = regName
		rating = profile.RatingSafe
	}

	if err := os.MkdirAll(store.ModelsDir(), 0o755); err != nil {
		return err
	}
	dest := filepath.Join(store.ModelsDir(), filename)
	// Log the filename, not the URL — a Civitai download URL carries the token.
	fmt.Fprintf(os.Stderr, "pulling %s\n  -> %s\n", filename, dest)
	lastBucket := -1
	err = download.Fetch(url, dest, fetchToken, func(f float64) {
		if b := int(f * 100); b/5 != lastBucket {
			lastBucket = b / 5
			fmt.Fprintf(os.Stderr, "\r  %3d%%", int(f*100))
		}
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}

	// Fetch the dedicated VAE too (e.g. the SDXL fp16-fix), so the gotcha stays
	// hidden from the user.
	var vaePath string
	if vaeSrc != "" {
		if vURL, vName, verr := download.Resolve("hf:" + vaeSrc); verr == nil {
			vDest := filepath.Join(store.ModelsDir(), vName)
			fmt.Fprintf(os.Stderr, "pulling VAE %s\n", vName)
			if err := download.Fetch(vURL, vDest, conf.ResolveHFToken(), nil); err != nil {
				return fmt.Errorf("models pull: VAE download: %w", err)
			}
			vaePath = vDest
		} else {
			fmt.Fprintf(os.Stderr, "note: VAE source %q not auto-pullable (%v); skipping\n", vaeSrc, verr)
		}
	}

	reg, err := store.Load()
	if err != nil {
		return err
	}
	reg.Add(store.InstalledModel{Name: regName, Path: dest, VAEPath: vaePath, Profile: prof, Rating: rating, License: license})
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("installed %q (%s) -> %s\n", regName, prof.Arch, dest)
	return nil
}

// pullMultiComponent downloads each weight file of a multi-component model
// (diffusion + encoders + VAE) and registers it with those component paths.
func pullMultiComponent(e catalog.Entry, regName string, conf config.Config) error {
	if err := os.MkdirAll(store.ModelsDir(), 0o755); err != nil {
		return err
	}
	token := conf.ResolveHFToken()
	get := func(hfRef string) (string, error) {
		if hfRef == "" {
			return "", nil
		}
		url, name, err := download.Resolve("hf:" + hfRef)
		if err != nil {
			return "", err
		}
		dest := filepath.Join(store.ModelsDir(), name)
		if fi, serr := os.Stat(dest); serr == nil && fi.Size() > 0 {
			fmt.Fprintf(os.Stderr, "have %s (skipping)\n", name)
			return dest, nil
		}
		fmt.Fprintf(os.Stderr, "pulling %s\n", name)
		last := -1
		err = download.Fetch(url, dest, token, func(f float64) {
			if b := int(f * 100); b/5 != last {
				last = b / 5
				fmt.Fprintf(os.Stderr, "\r  %3d%%", int(f*100))
			}
		})
		fmt.Fprintln(os.Stderr)
		return dest, err
	}

	s := e.Source
	diff, err := get(s.DiffusionModel)
	if err != nil {
		return err
	}
	clipL, err := get(s.ClipL)
	if err != nil {
		return err
	}
	clipG, err := get(s.ClipG)
	if err != nil {
		return err
	}
	t5, err := get(s.T5XXL)
	if err != nil {
		return err
	}
	llm, err := get(s.LLM)
	if err != nil {
		return err
	}
	vae, err := get(s.VAE)
	if err != nil {
		return err
	}

	reg, err := store.Load()
	if err != nil {
		return err
	}
	reg.Add(store.InstalledModel{
		Name:       regName,
		VAEPath:    vae,
		Components: store.Components{DiffusionModel: diff, ClipL: clipL, ClipG: clipG, T5XXL: t5, LLM: llm},
		Profile:    e.Profile(),
		Rating:     e.Rating,
		License:    e.License,
	})
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("installed %q (%s, multi-component) -> %s\n", regName, e.Profile().Arch, store.ModelsDir())
	return nil
}

func modelsQuantize(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("models quantize: usage: models quantize <name> --to <q8_0|q4_k|...> [--name N]")
	}
	name := args[0]
	fs := flag.NewFlagSet("models quantize", flag.ContinueOnError)
	to := fs.String("to", "q8_0", "quant type: q8_0, q5_0, q4_0, q4_1, q4_k, q6_k, f16, ...")
	newName := fs.String("name", "", "registry name for the result (default: <name>-<type>)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	reg, err := store.Load()
	if err != nil {
		return err
	}
	src, ok := reg.Get(name)
	if !ok {
		return fmt.Errorf("models quantize: %q is not installed", name)
	}

	outName := *newName
	if outName == "" {
		outName = name + "-" + *to
	}
	if err := os.MkdirAll(store.ModelsDir(), 0o755); err != nil {
		return err
	}
	outPath := filepath.Join(store.ModelsDir(), outName+".gguf")

	// Bake the model's VAE into the GGUF so the quantized model is self-contained.
	fmt.Fprintf(os.Stderr, "quantizing %s\n  -> %s (%s)\n", src.Path, outPath, *to)
	if err := engine.Quantize(src.Path, src.VAEPath, outPath, *to); err != nil {
		return err
	}

	prof := src.Profile
	prof.Name = outName
	reg.Add(store.InstalledModel{Name: outName, Path: outPath, Profile: prof, Rating: src.Rating, License: src.License})
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("quantized %q -> %q (%s) -> %s\n", name, outName, *to, outPath)
	return nil
}

func modelsRm(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("models rm: <name> required")
	}
	reg, err := store.Load()
	if err != nil {
		return err
	}
	if !reg.Remove(args[0]) {
		return fmt.Errorf("models rm: %q is not installed", args[0])
	}
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("removed %q (the model file, if any, was left on disk)\n", args[0])
	return nil
}
