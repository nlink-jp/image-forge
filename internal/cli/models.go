package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		return modelsList(args[1:])
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

// catalogView / installedView are the stable, purpose-built shapes rendered by
// `models list` (as a table or, with --json, as JSON) — decoupled from the
// internal catalog.Entry / store.InstalledModel structs.
type catalogView struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind,omitempty"` // "" (diffusion) | upscaler | lora | controlnet
	Arch           string   `json:"arch"`
	Prediction     string   `json:"prediction"`
	Rating         string   `json:"rating"`
	License        string   `json:"license"`
	MinRAMGB       int      `json:"min_ram_gb"`
	RecRAMGB       int      `json:"rec_ram_gb"`
	MultiComponent bool     `json:"multi_component"`
	NeedsOptIn     bool     `json:"needs_opt_in"`
	Experimental   bool     `json:"experimental,omitempty"`
	Installed      bool     `json:"installed"`
	Notes          string   `json:"notes,omitempty"`
	TriggerWords   []string `json:"trigger_words,omitempty"`
}

type installedView struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind,omitempty"` // "" (diffusion) | upscaler | lora | controlnet
	Arch           string   `json:"arch"`
	Rating         string   `json:"rating,omitempty"`
	License        string   `json:"license,omitempty"`
	Path           string   `json:"path,omitempty"`
	VAEPath        string   `json:"vae_path,omitempty"`
	MultiComponent bool     `json:"multi_component"`
	InCatalog      bool     `json:"in_catalog"`
	TriggerWords   []string `json:"trigger_words,omitempty"`
}

// archCell renders the ARCH column: an upscaler has no diffusion architecture,
// so it shows its kind instead.
func archCell(arch, kind string) string {
	if kind == "upscaler" {
		return "upscaler"
	}
	if arch == "" {
		return "-"
	}
	return arch
}

// resolveListMode maps the (possibly conflicting) list flags to which views to
// render. Default (no flag) is installed-only; --catalog is catalog-only; --all
// (or both --catalog and --installed) shows both.
func resolveListMode(all, catalogOnly, installedOnly bool) (showInstalled, showCatalog bool) {
	showCatalog = all || catalogOnly
	showInstalled = all || installedOnly || !catalogOnly
	return
}

func catalogViews(reg *store.Registry) []catalogView {
	entries := catalog.Default()
	out := make([]catalogView, 0, len(entries))
	for _, e := range entries {
		_, installed := reg.Get(e.Name)
		out = append(out, catalogView{
			Name: e.Name, Kind: e.Kind, Arch: string(e.Arch), Prediction: string(e.Prediction),
			Rating: string(e.Rating), License: e.License,
			MinRAMGB: e.MinRAMGB, RecRAMGB: e.RecRAMGB,
			MultiComponent: e.IsMultiComponent(), NeedsOptIn: e.NeedsOptIn(),
			Experimental: e.Experimental, Installed: installed, Notes: e.Notes,
			TriggerWords: e.TriggerWords,
		})
	}
	return out
}

func installedViews(reg *store.Registry) []installedView {
	names := make([]string, 0, len(reg.Models))
	for n := range reg.Models {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]installedView, 0, len(names))
	for _, n := range names {
		m := reg.Models[n]
		_, inCat := catalog.Find(n)
		out = append(out, installedView{
			Name: m.Name, Kind: m.Kind, Arch: string(m.Profile.Arch), Rating: string(m.Rating),
			License: m.License, Path: m.Path, VAEPath: m.VAEPath,
			// Only a base diffusion model can be assembled from components; an
			// upscaler / LoRA / ControlNet with no Path would just be broken.
			MultiComponent: m.Path == "" && m.IsDiffusion(), InCatalog: inCat,
			TriggerWords: m.TriggerWords,
		})
	}
	return out
}

// ModelListing is the JSON shape returned by the MCP list_models tool. Exactly
// the same views that `models list --json` renders (installedView/catalogView),
// so the two surfaces never drift.
type ModelListing struct {
	Installed []installedView `json:"installed,omitempty"`
	Catalog   []catalogView   `json:"catalog,omitempty"`
}

// ListModels returns the installed and/or catalog views for the given scope
// ("installed", "catalog", or "all"). It reuses the exact installedViews /
// catalogViews that back `models list`, so the MCP tool and the CLI stay in
// lockstep. An unknown scope returns an error.
func ListModels(scope string) (ModelListing, error) {
	reg, err := store.Load()
	if err != nil {
		return ModelListing{}, err
	}
	var out ModelListing
	switch scope {
	case "installed":
		out.Installed = installedViews(reg)
	case "catalog":
		out.Catalog = catalogViews(reg)
	case "all":
		out.Installed = installedViews(reg)
		out.Catalog = catalogViews(reg)
	default:
		return ModelListing{}, fmt.Errorf("unknown scope %q (want installed|catalog|all)", scope)
	}
	return out, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func modelsList(args []string) error {
	fs := flag.NewFlagSet("models list", flag.ContinueOnError)
	catalogOnly := fs.Bool("catalog", false, "list the curated catalog (models available to pull)")
	installedOnly := fs.Bool("installed", false, "list installed models (the default)")
	all := fs.Bool("all", false, "list both installed models and the catalog")
	asJSON := fs.Bool("json", false, "output JSON instead of a table")
	kindFlag := fs.String("kind", "", "only this kind: diffusion|lora|controlnet|upscaler")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := store.Load()
	if err != nil {
		return err
	}
	showInstalled, showCatalog := resolveListMode(*all, *catalogOnly, *installedOnly)
	inst := installedViews(reg)
	cat := catalogViews(reg)
	if *kindFlag != "" {
		kind, err := normalizeKind(*kindFlag)
		if err != nil {
			return fmt.Errorf("models list: %w", err)
		}
		inst = filterByKind(inst, kind, func(v installedView) string { return v.Kind })
		cat = filterByKind(cat, kind, func(v catalogView) string { return v.Kind })
	}

	if *asJSON {
		switch {
		case showInstalled && showCatalog:
			return printJSON(struct {
				Installed []installedView `json:"installed"`
				Catalog   []catalogView   `json:"catalog"`
			}{inst, cat})
		case showCatalog:
			return printJSON(cat)
		default:
			return printJSON(inst)
		}
	}

	both := showInstalled && showCatalog
	if showInstalled {
		printInstalledTable(inst, both)
	}
	if showCatalog {
		if showInstalled {
			fmt.Println()
		}
		printCatalogTable(cat, both)
	}
	return nil
}

func printInstalledTable(views []installedView, titled bool) {
	if titled {
		fmt.Println("INSTALLED")
	}
	if len(views) == 0 {
		fmt.Println("(no models installed — browse the catalog with `models list --catalog`)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tARCH\tRATING\tLICENSE\tPATH")
	for _, v := range views {
		rating := v.Rating
		if rating == "" {
			rating = "-"
		}
		loc := v.Path
		if loc == "" {
			loc = "(multi-component)"
		}
		license := v.License
		if license == "" {
			license = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", v.Name, archCell(v.Arch, v.Kind), rating, license, loc)
	}
	w.Flush()
}

func printCatalogTable(views []catalogView, titled bool) {
	if titled {
		fmt.Println("CATALOG")
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tARCH\tRATING\tRAM\tLICENSE\tINSTALLED")
	for _, v := range views {
		installed := "no"
		if v.Installed {
			installed = "yes"
		}
		if v.Experimental {
			installed += " (experimental)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%dGB\t%s\t%s\n",
			v.Name, archCell(v.Arch, v.Kind), v.Rating, v.MinRAMGB, v.RecRAMGB, v.License, installed)
	}
	w.Flush()
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
	kindFlag := fs.String("kind", "", "model kind: lora|controlnet|upscaler (default: a base diffusion model)")
	triggerFlag := fs.String("trigger", "", "comma-separated LoRA trigger words (prompt tokens that activate it)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	triggers := splitTriggers(*triggerFlag)
	kind, err := normalizeKind(*kindFlag)
	if err != nil {
		return fmt.Errorf("models import: %w", err)
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

	// Base models get the full architecture defaults. Auxiliary models are not
	// renderable, so they carry only what identifies them: a name, and — for
	// LoRA / ControlNet — the base architecture they're bound to (ADR-0006).
	var prof profile.Profile
	switch kind {
	case catalog.KindDiffusion:
		prof = profile.ArchDefaults(arch)
		prof.Name = nm
	case catalog.KindUpscaler:
		prof = profile.Profile{Name: nm}
	default: // lora | controlnet
		prof = profile.Profile{Name: nm, Arch: arch}
	}

	reg, err := store.Load()
	if err != nil {
		return err
	}
	vaePath := *vae
	if kind != catalog.KindDiffusion {
		vaePath = ""
	}
	reg.Add(store.InstalledModel{
		Name: nm, Kind: kind, Path: abs, VAEPath: vaePath,
		Profile: prof, Rating: profile.RatingSafe, TriggerWords: triggers,
	})
	if err := reg.Save(); err != nil {
		return err
	}
	if kind == catalog.KindUpscaler {
		fmt.Printf("imported %q (%s) -> %s\n", nm, kind, abs)
	} else if kind != catalog.KindDiffusion {
		fmt.Printf("imported %q (%s, %s) -> %s\n", nm, kind, arch, abs)
	} else {
		fmt.Printf("imported %q (%s) -> %s\n", nm, arch, abs)
	}
	printTriggerWords(triggers)
	return nil
}

// filterByKind keeps only the views whose kind (read by `kindOf`) equals `kind`.
// Pure, so `models list --kind` filtering is unit-testable.
func filterByKind[T any](views []T, kind string, kindOf func(T) string) []T {
	out := make([]T, 0, len(views))
	for _, v := range views {
		if kindOf(v) == kind {
			out = append(out, v)
		}
	}
	return out
}

// printTriggerWords tells the user what to put in the prompt. A LoRA whose
// trigger is missing loads without error and silently does nothing, so this is
// worth surfacing right after install rather than leaving it to `models list`.
func printTriggerWords(triggers []string) {
	if len(triggers) == 0 {
		return
	}
	fmt.Printf("  trigger words (add to your prompt): %s\n", strings.Join(triggers, ", "))
}

// splitTriggers parses a comma-separated --trigger value into activation tokens,
// dropping blanks. A trigger may itself contain spaces ("pov, on couch" is two
// tokens by this rule, which matches how Civitai stores them). Pure, for tests.
func splitTriggers(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// normalizeKind validates a --kind flag value, mapping "" (and "diffusion") to
// the default base-model kind.
func normalizeKind(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "diffusion":
		return catalog.KindDiffusion, nil
	case catalog.KindUpscaler:
		return catalog.KindUpscaler, nil
	case catalog.KindLoRA:
		return catalog.KindLoRA, nil
	case catalog.KindControlNet, "control-net", "control_net":
		return catalog.KindControlNet, nil
	default:
		return "", fmt.Errorf("unknown kind %q (want lora|controlnet|upscaler)", s)
	}
}

// haveFile reports whether path is an already-downloaded, non-empty file we can
// reuse instead of re-fetching (checkpoints run to several GB).
func haveFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
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
		prof     profile.Profile
		rating   profile.Rating
		license  string
		vaeSrc   string
		kind     string
		triggers []string
		regName  = *nameOverride
		known    bool
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
		prof, rating, license, vaeSrc, kind = e.Profile(), e.Rating, e.License, e.Source.VAE, e.Kind
		triggers = e.TriggerWords
		switch {
		case e.IsUpscaler():
			// Upscalers are architecture-agnostic and carry no diffusion profile / VAE.
			prof, vaeSrc = profile.Profile{Name: e.Name}, ""
		case e.IsLoRA(), e.IsControlNet():
			// LoRA / ControlNet carry no diffusion profile or VAE either, but they
			// ARE bound to a base architecture — keep Arch so `models list` can
			// report it and callers can filter incompatible combinations (ADR-0006).
			prof, vaeSrc = profile.Profile{Name: e.Name, Arch: e.Arch}, ""
		}
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
	if haveFile(dest) {
		// Already downloaded (possibly under another registered name) — reuse it
		// instead of re-fetching several GB.
		fmt.Fprintf(os.Stderr, "have %s (skipping download)\n", filename)
	} else {
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
	}

	// Fetch the dedicated VAE too (e.g. the SDXL fp16-fix), so the gotcha stays
	// hidden from the user.
	var vaePath string
	if vaeSrc != "" {
		if vURL, vName, verr := download.Resolve("hf:" + vaeSrc); verr == nil {
			vDest := filepath.Join(store.ModelsDir(), vName)
			if haveFile(vDest) {
				fmt.Fprintf(os.Stderr, "have VAE %s (skipping download)\n", vName)
			} else {
				fmt.Fprintf(os.Stderr, "pulling VAE %s\n", vName)
				if err := download.Fetch(vURL, vDest, conf.ResolveHFToken(), nil); err != nil {
					return fmt.Errorf("models pull: VAE download: %w", err)
				}
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
	reg.Add(store.InstalledModel{
		Name: regName, Kind: kind, Path: dest, VAEPath: vaePath,
		Profile: prof, Rating: rating, License: license, TriggerWords: triggers,
	})
	if err := reg.Save(); err != nil {
		return err
	}
	if kind == "upscaler" {
		fmt.Printf("installed %q (upscaler) -> %s\n", regName, dest)
	} else if kind != catalog.KindDiffusion {
		fmt.Printf("installed %q (%s, %s) -> %s\n", regName, kind, prof.Arch, dest)
	} else {
		fmt.Printf("installed %q (%s) -> %s\n", regName, prof.Arch, dest)
	}
	printTriggerWords(triggers)
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
		if haveFile(dest) {
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
