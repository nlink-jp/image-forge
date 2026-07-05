package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/download"
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
		return fmt.Errorf("models quantize: %w", ErrNotImplemented)
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
	allowNSFW := fs.Bool("allow-nsfw", false, "allow pulling questionable/explicit models")
	nameOverride := fs.String("name", "", "override the registry name")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	srcRef := ref
	var (
		prof    profile.Profile
		rating  profile.Rating
		license string
		regName = *nameOverride
		known   bool
	)
	if e, ok := catalog.Find(ref); ok {
		known = true
		if e.NeedsOptIn() && !*allowNSFW {
			return fmt.Errorf("models pull: %q is rated %q; re-run with --allow-nsfw", e.Name, e.Rating)
		}
		if e.Experimental {
			fmt.Fprintf(os.Stderr, "warning: %q is experimental: %s\n", e.Name, e.Notes)
		}
		switch {
		case e.Source.HF != "":
			srcRef = "hf:" + e.Source.HF
		case e.Source.URL != "":
			srcRef = e.Source.URL
		default:
			return fmt.Errorf("models pull: catalog entry %q has no downloadable source", e.Name)
		}
		prof, rating, license = e.Profile(), e.Rating, e.License
		if regName == "" {
			regName = e.Name
		}
	}

	url, filename, err := download.Resolve(srcRef)
	if err != nil {
		if known {
			return fmt.Errorf("models pull: %q is not directly pullable yet (%w); use `models import` with a local file", ref, err)
		}
		return err
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
	fmt.Fprintf(os.Stderr, "pulling %s\n  -> %s\n", url, dest)
	lastBucket := -1
	err = download.Fetch(url, dest, os.Getenv("HF_TOKEN"), func(f float64) {
		if b := int(f * 100); b/5 != lastBucket {
			lastBucket = b / 5
			fmt.Fprintf(os.Stderr, "\r  %3d%%", int(f*100))
		}
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}

	reg, err := store.Load()
	if err != nil {
		return err
	}
	reg.Add(store.InstalledModel{Name: regName, Path: dest, Profile: prof, Rating: rating, License: license})
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("installed %q (%s) -> %s\n", regName, prof.Arch, dest)
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
