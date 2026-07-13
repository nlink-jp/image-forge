package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
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
		return fmt.Errorf("models: expected a subcommand (list|import|pull|open|quantize|rm|gc)")
	}
	switch args[0] {
	case "list":
		return modelsList(args[1:])
	case "import":
		return modelsImport(args[1:])
	case "pull":
		return modelsPull(args[1:])
	case "open":
		return modelsOpen(args[1:])
	case "rm":
		return modelsRm(args[1:])
	case "gc":
		return modelsGc(args[1:])
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
	LicenseFlags   []string `json:"license_flags,omitempty"`
	Attribution    string   `json:"attribution,omitempty"`
	TriggerWords   []string `json:"trigger_words,omitempty"`
	// PageURL is the model's web home (Civitai model page / HF repo), for a
	// front-end "open model page" link or `models open`. Empty when none applies.
	PageURL string `json:"page_url,omitempty"`
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
	LicenseFlags   []string `json:"license_flags,omitempty"`
	Attribution    string   `json:"attribution,omitempty"`
	TriggerWords   []string `json:"trigger_words,omitempty"`
	// PageURL is the catalog model's web home (Civitai / HF); empty for a
	// user-local model that isn't in the catalog.
	PageURL string `json:"page_url,omitempty"`
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
		pageURL, _ := e.PageURL()
		out = append(out, catalogView{
			Name: e.Name, Kind: e.Kind, Arch: string(e.Arch), Prediction: string(e.Prediction),
			Rating: string(e.Rating), License: e.License,
			MinRAMGB: e.MinRAMGB, RecRAMGB: e.RecRAMGB,
			MultiComponent: e.IsMultiComponent(), NeedsOptIn: e.NeedsOptIn(),
			Experimental: e.Experimental, Installed: installed, Notes: e.Notes,
			LicenseFlags: e.LicenseFlags, Attribution: e.Attribution, TriggerWords: e.TriggerWords,
			PageURL: pageURL,
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
		e, inCat := catalog.Find(n)
		// Surfaced metadata (license terms, trigger words) is descriptive, not part
		// of the installed bytes. For a cataloged model the catalog is the current
		// source of truth — use it so entries installed before these fields existed
		// (or corrected in the catalog since) are still reported accurately.
		license, flags, triggers, attribution := m.License, m.LicenseFlags, m.TriggerWords, m.Attribution
		var pageURL string
		if inCat {
			flags, triggers, attribution = e.LicenseFlags, e.TriggerWords, e.Attribution
			pageURL, _ = e.PageURL()
			if license == "" {
				license = e.License
			}
		}
		out = append(out, installedView{
			Name: m.Name, Kind: m.Kind, Arch: string(m.Profile.Arch), Rating: string(m.Rating),
			License: license, Path: m.Path, VAEPath: m.VAEPath,
			// Only a base diffusion model can be assembled from components; an
			// upscaler / LoRA / ControlNet with no Path would just be broken.
			MultiComponent: m.Path == "" && m.IsDiffusion(), InCatalog: inCat,
			LicenseFlags: flags, Attribution: attribution, TriggerWords: triggers,
			PageURL: pageURL,
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

// resolveModelPageURL returns the web page (Civitai model page / Hugging Face
// repo) for a catalog model by name. It is the pure core of `models open`, so it
// is unit-tested without touching a browser. An unknown name, or a model with no
// known page, is an error.
func resolveModelPageURL(name string) (string, error) {
	e, ok := catalog.Find(name)
	if !ok {
		return "", fmt.Errorf("open: no catalog model named %q (see `image-forge models list --catalog`)", name)
	}
	url, ok := e.PageURL()
	if !ok {
		return "", fmt.Errorf("open: no web page is known for %q", name)
	}
	return url, nil
}

// modelsOpen opens a catalog model's source page (Civitai / Hugging Face) in the
// default browser, so the model card can be read without searching for it. With
// --print it writes the URL to stdout instead (for scripting / headless use).
func modelsOpen(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("models open: usage: models open <name> [--print]")
	}
	name := args[0]
	fs := flag.NewFlagSet("models open", flag.ContinueOnError)
	printOnly := fs.Bool("print", false, "print the URL instead of opening a browser")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	url, err := resolveModelPageURL(name)
	if err != nil {
		return err
	}
	if *printOnly {
		fmt.Println(url)
		return nil
	}
	// darwin/arm64-only tool: `open` is the platform URL handler.
	if err := exec.Command("open", url).Run(); err != nil {
		return fmt.Errorf("open %s: %w", url, err)
	}
	fmt.Fprintf(os.Stderr, "opened %s\n", url)
	return nil
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
	archFlag := fs.String("arch", "", "architecture: sdxl|sd15|sd35|flux|zimage|anima (default: auto-detect)")
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

	prof := auxProfile(kind, nm, arch)

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

// auxProfile builds the registry profile for a model of the given (normalized)
// kind. Base (diffusion) models get the full architecture defaults; upscalers
// are architecture-agnostic and carry only a name; LoRA / ControlNet keep the
// base architecture they're bound to and nothing else (ADR-0006). Pure, and
// shared by `models import` and `models pull` (ADR-0007), so the kind→profile
// invariant lives in one place and is unit-testable without touching the network.
func auxProfile(kind, name string, arch profile.Arch) profile.Profile {
	switch kind {
	case catalog.KindUpscaler:
		return profile.Profile{Name: name}
	case catalog.KindLoRA, catalog.KindControlNet:
		return profile.Profile{Name: name, Arch: arch}
	default: // diffusion
		p := profile.ArchDefaults(arch)
		p.Name = name
		return p
	}
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
		return fmt.Errorf("models pull: usage: models pull <name|hf:owner/repo/file|url> [--allow-nsfw] [--name N] [--kind K] [--arch A] [--trigger W]")
	}
	ref := args[0]
	fs := flag.NewFlagSet("models pull", flag.ContinueOnError)
	allowNSFWFlag := fs.Bool("allow-nsfw", false, "allow pulling questionable/explicit models")
	nameOverride := fs.String("name", "", "override the registry name")
	// Overrides for a ref that is NOT a catalog name (raw hf:/civitai:/url). A
	// catalog entry carries its own kind/arch/triggers and ignores these (ADR-0007).
	kindFlag := fs.String("kind", "", "kind for a non-catalog ref: lora|controlnet|upscaler (default: base diffusion)")
	archFlag := fs.String("arch", "", "architecture for a non-catalog ref: sdxl|sd15|sd35|flux|zimage|anima (default: auto-detect)")
	triggerFlag := fs.String("trigger", "", "comma-separated LoRA trigger words for a non-catalog ref")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	overrideKind, err := normalizeKind(*kindFlag)
	if err != nil {
		return fmt.Errorf("models pull: %w", err)
	}
	overridden := *kindFlag != "" || *archFlag != "" || *triggerFlag != ""

	conf, err := config.Load()
	if err != nil {
		return fmt.Errorf("models pull: config: %w", err)
	}
	allowNSFW := *allowNSFWFlag || conf.AllowNSFW

	srcRef := ref
	var (
		prof        profile.Profile
		rating      profile.Rating
		license     string
		licFlags    []string
		attribution string
		vaeSrc      string
		kind        string
		triggers    []string
		regName     = *nameOverride
		known       bool
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
		triggers, licFlags, attribution = e.TriggerWords, e.LicenseFlags, e.Attribution
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

	if known && overridden {
		fmt.Fprintf(os.Stderr, "note: %q is a catalog entry; --kind/--arch/--trigger overrides are ignored\n", ref)
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
		// A raw ref (hf:/civitai:/url) carries no catalog metadata: honor the
		// --kind/--arch/--trigger overrides so a LoRA/ControlNet isn't silently
		// registered as a base diffusion model (ADR-0007). Defaults reproduce the
		// prior behavior — base diffusion, arch auto-detected from the name.
		kind = overrideKind
		arch := profile.Detect(regName)
		if *archFlag != "" {
			arch = profile.Arch(*archFlag)
		}
		prof = auxProfile(kind, regName, arch)
		rating = profile.RatingSafe
		triggers = splitTriggers(*triggerFlag)
	}

	if err := os.MkdirAll(store.ModelsDir(), 0o755); err != nil {
		return err
	}
	// Store under the registry name (keeping the extension), so two models that share
	// a generic upstream basename — e.g. diffusion_pytorch_model.safetensors (xinsir
	// ControlNet) or pytorch_lora_weights.safetensors (the LCM LoRAs) — don't collide
	// in the models dir and silently reuse each other's bytes.
	dest := filepath.Join(store.ModelsDir(), regName+filepath.Ext(filename))
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
		Profile: prof, Rating: rating, License: license,
		LicenseFlags: licFlags, Attribution: attribution, TriggerWords: triggers,
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
	hfToken := conf.ResolveHFToken()
	// get downloads one component. A plain "owner/repo/file" is a Hugging Face ref;
	// a "civitai:<versionId>" ref is resolved via the Civitai API (used for a
	// Civitai-hosted diffusion model paired with HF-hosted encoders/VAE, e.g. an
	// Anima DiT from Civitai + the shared Qwen encoder + Qwen-Image VAE).
	get := func(ref string) (string, error) {
		if ref == "" {
			return "", nil
		}
		var (
			url, name string
			err       error
			fetchTok  = hfToken
		)
		if vid, ok := catalog.CivitaiRef(ref); ok {
			ctok := conf.ResolveCivitaiToken()
			if ctok == "" {
				return "", fmt.Errorf("Civitai component requires a token — set CIVITAI_TOKEN or civitai_token in config")
			}
			url, name, err = download.CivitaiResolve(vid, ctok)
			fetchTok = "" // the token is embedded in the URL
		} else {
			url, name, err = download.Resolve("hf:" + ref)
		}
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
		err = download.Fetch(url, dest, fetchTok, func(f float64) {
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
		Name:         regName,
		VAEPath:      vae,
		Components:   store.Components{DiffusionModel: diff, ClipL: clipL, ClipG: clipG, T5XXL: t5, LLM: llm},
		Profile:      e.Profile(),
		Rating:       e.Rating,
		License:      e.License,
		LicenseFlags: e.LicenseFlags,
		Attribution:  e.Attribution,
		TriggerWords: e.TriggerWords,
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
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("models rm: usage: models rm <name> [--purge]")
	}
	name := args[0]
	fs := flag.NewFlagSet("models rm", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also delete the model's weight files from disk (files shared with another installed model, or outside the managed models dir, are kept)")
	frontend := fs.Bool("confirmed-by-frontend", false, "skip the interactive prompt because a trusted front-end (the GUI) already confirmed with the user; for front-ends only, not interactive use")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	reg, err := store.Load()
	if err != nil {
		return err
	}
	return runRm(os.Stdout, reg, name, *purge, filepath.Clean(store.ModelsDir()), resolveConfirm(*frontend))
}

// runRm is the testable core of `models rm`. Without purge it just forgets the
// registry entry. With purge it works out which of the model's files it may
// delete — excluding files another installed model still references and files
// outside the managed dir — then asks confirm BEFORE changing anything, so a
// declined `rm --purge` is a complete no-op (the entry stays too).
func runRm(out io.Writer, reg *store.Registry, name string, purge bool, dir string, confirm confirmFunc) error {
	m, ok := reg.Get(name)
	if !ok {
		return fmt.Errorf("models rm: %q is not installed", name)
	}
	if !purge {
		reg.Remove(name)
		if err := reg.Save(); err != nil {
			return err
		}
		fmt.Fprintf(out, "removed %q (the model file, if any, was left on disk; use --purge to delete)\n", name)
		return nil
	}
	// Files still referenced by *other* installed models must be kept. Compute
	// that set without removing this entry yet, so declining leaves everything.
	usedByOthers := map[string]bool{}
	for n, mm := range reg.Models {
		if n == name {
			continue
		}
		for _, f := range mm.Files() {
			usedByOthers[f] = true
		}
	}
	var deletable []string
	sizes := map[string]int64{}
	var total int64
	for _, f := range m.Files() {
		switch {
		case usedByOthers[f]:
			fmt.Fprintf(out, "  keep %s (shared with another installed model)\n", f)
		case !underDir(f, dir):
			fmt.Fprintf(out, "  keep %s (outside the managed models dir; delete it yourself if intended)\n", f)
		default:
			deletable = append(deletable, f)
			if info, err := os.Stat(f); err == nil {
				sizes[f] = info.Size()
				total += info.Size()
			}
			fmt.Fprintf(out, "  delete %s (%s)\n", f, humanBytes(sizes[f]))
		}
	}
	if len(deletable) == 0 {
		// Nothing to purge; still drop the registry entry (non-destructive).
		reg.Remove(name)
		if err := reg.Save(); err != nil {
			return err
		}
		fmt.Fprintf(out, "removed %q (no owned files to delete)\n", name)
		return nil
	}
	summary := fmt.Sprintf("models rm --purge: about to permanently delete %d file(s) totaling %s for %q.", len(deletable), humanBytes(total), name)
	if !confirm(summary) {
		fmt.Fprintf(out, "Aborted; %q is untouched (still installed, files kept).\n", name)
		return nil
	}
	reg.Remove(name)
	if err := reg.Save(); err != nil {
		return err
	}
	var freed int64
	for _, f := range deletable {
		if _, err := removeFile(f); err != nil {
			fmt.Fprintf(out, "  could not delete %s: %v\n", f, err)
			continue
		}
		freed += sizes[f]
	}
	fmt.Fprintf(out, "removed %q; freed %s\n", name, humanBytes(freed))
	return nil
}

// modelsGc reclaims orphaned files in the models dir — files no installed model
// references (leftover .part downloads, files whose model was `rm`'d without
// --purge, quantize/convert intermediates). Dry-run by default; `--force` enters
// the delete flow, which ALWAYS asks for an interactive "yes" first.
func modelsGc(args []string) error {
	fs := flag.NewFlagSet("models gc", flag.ContinueOnError)
	force := fs.Bool("force", false, "enter the delete flow (still asks for interactive confirmation); default: only report")
	frontend := fs.Bool("confirmed-by-frontend", false, "skip the interactive prompt because a trusted front-end (the GUI) already confirmed with the user; for front-ends only, not interactive use")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := store.Load()
	if err != nil {
		return err
	}
	return runGc(os.Stdout, reg, filepath.Clean(store.ModelsDir()), *force, resolveConfirm(*frontend))
}

// runGc is the testable core of `models gc`: it lists the orphaned files in dir,
// and when force is set, deletes them ONLY after confirm approves. Stdin/TTY is
// kept out via the injected confirm, so a test can exercise the delete path on a
// throwaway dir without ever risking a real terminal or real files.
func runGc(out io.Writer, reg *store.Registry, dir string, force bool, confirm confirmFunc) error {
	referenced := reg.ReferencedFiles()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		fmt.Fprintf(out, "models gc: no models dir yet (%s); nothing to reclaim\n", dir)
		return nil
	}
	if err != nil {
		return err
	}
	var orphans []string
	sizes := map[string]int64{}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue // gc handles top-level files only; never removes directories
		}
		p := filepath.Join(dir, e.Name())
		if referenced[filepath.Clean(p)] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		orphans = append(orphans, p)
		sizes[p] = info.Size()
		total += info.Size()
	}
	if len(orphans) == 0 {
		fmt.Fprintf(out, "models gc: no orphaned files in %s\n", dir)
		return nil
	}
	sort.Strings(orphans)
	for _, p := range orphans {
		fmt.Fprintf(out, "  %s (%s)\n", p, humanBytes(sizes[p]))
	}
	if !force {
		fmt.Fprintf(out, "models gc: %d orphaned file(s), %s reclaimable. Re-run with --force to delete (you'll be asked to confirm).\n", len(orphans), humanBytes(total))
		return nil
	}
	summary := fmt.Sprintf("models gc: about to permanently delete %d file(s) totaling %s from %s.", len(orphans), humanBytes(total), dir)
	if !confirm(summary) {
		fmt.Fprintln(out, "Aborted; no files deleted.")
		return nil
	}
	var freed int64
	for _, p := range orphans {
		if _, err := removeFile(p); err != nil {
			fmt.Fprintf(out, "  could not delete %s: %v\n", p, err)
			continue
		}
		freed += sizes[p]
		fmt.Fprintf(out, "  deleted %s (%s)\n", p, humanBytes(sizes[p]))
	}
	fmt.Fprintf(out, "models gc: freed %s\n", humanBytes(freed))
	return nil
}

// underDir reports whether file p lies within directory dir (both cleaned).
func underDir(p, dir string) bool {
	rel, err := filepath.Rel(dir, filepath.Clean(p))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// removeFile deletes a file and returns the bytes freed (0 if it was already gone).
func removeFile(p string) (int64, error) {
	info, err := os.Stat(p)
	if os.IsNotExist(err) {
		return 0, nil
	}
	var size int64
	if err == nil {
		size = info.Size()
	}
	if err := os.Remove(p); err != nil {
		return 0, err
	}
	return size, nil
}

// humanBytes formats a byte count as a human-readable size (e.g. "6.5 GB").
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
