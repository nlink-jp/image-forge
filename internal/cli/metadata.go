package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/engine"
	"github.com/nlink-jp/image-forge/internal/store"
)

// binVersion is the binary version string, set at the top of Run (mirroring
// mcpVersion). It is embedded in the generation metadata ("Version: image-forge
// <binVersion>" and the JSON "version" field). Defaults to "dev" so unit tests
// and direct calls have a stable value.
var binVersion = "dev"

// buildImageMetadata builds the PNG text chunks embedded into a generated image:
//
//   - "parameters" — an AUTOMATIC1111-compatible string, so Civitai / A1111 parse
//     the generation data directly.
//   - "image-forge" — a complete, lossless JSON record of everything the tool knows.
//
// modelName is the friendly model name (registry name or model-file base name);
// prediction is the sd.cpp prediction string ("", "eps", "v"). Returns nil when
// embed is false (config [metadata] embed = false, or gen --no-metadata).
func buildImageMetadata(req engine.Request, modelName, prediction string, embed bool) []engine.PNGText {
	if !embed {
		return nil
	}
	names := registryNameByPath()
	credits := attributionByName()
	return []engine.PNGText{
		{Keyword: "parameters", Text: a1111Parameters(req, modelName)},
		{Keyword: "image-forge", Text: imageForgeJSON(req, modelName, prediction, names, credits)},
	}
}

// metadataBuilder returns the per-image metadata function the engine calls once
// per output image. A batch of N produces seeds base..base+N-1 (sd.cpp uses
// base+b for the b-th image), so each image must record *its own* seed — this
// rebuilds the metadata with the given seed rather than baking in one seed for
// the whole batch. Returns nil chunks when embed is false.
func metadataBuilder(base engine.Request, modelName, prediction string, embed bool) func(seed int64) []engine.PNGText {
	return func(seed int64) []engine.PNGText {
		m := base
		m.Seed = seed
		return buildImageMetadata(m, modelName, prediction, embed)
	}
}

// a1111Parameters renders the AUTOMATIC1111 "parameters" string: the prompt, an
// optional negative-prompt line, then a comma-joined line of key:value settings.
func a1111Parameters(req engine.Request, modelName string) string {
	var b strings.Builder
	b.WriteString(req.Prompt)
	if req.Negative != "" {
		b.WriteString("\nNegative prompt: ")
		b.WriteString(req.Negative)
	}
	b.WriteString("\n")

	parts := []string{
		"Steps: " + strconv.Itoa(req.Steps),
		"Sampler: " + req.Sampler,
		"CFG scale: " + ftoa(req.CFG),
		"Seed: " + strconv.FormatInt(req.Seed, 10),
		"Size: " + strconv.Itoa(req.Width) + "x" + strconv.Itoa(req.Height),
		"Model: " + modelName,
	}
	if req.ClipSkip > 1 {
		parts = append(parts, "Clip skip: "+strconv.Itoa(req.ClipSkip))
	}
	if req.InitImage != "" {
		parts = append(parts, "Denoising strength: "+ftoa(req.Strength))
	}
	if req.Hires {
		parts = append(parts,
			"Hires upscale: "+ftoa(req.HiresScale),
			"Hires upscaler: "+req.HiresUpscaler,
		)
	}
	parts = append(parts, "Version: image-forge "+binVersion)

	b.WriteString(strings.Join(parts, ", "))
	return b.String()
}

// imgforgeMeta is the JSON record embedded under the "image-forge" keyword.
// Sub-records are pointers so they omit entirely when not applicable.
//
// **It never carries a filesystem path.** A generated image is meant to be
// shared, and an absolute path leaks the machine's layout (and, via the home
// directory, the user's name) while being useless on anyone else's machine.
// Models are recorded by identifier — a registry name where we have one, else the
// file's base name — which is also what actually reproduces the image, since
// `--lora` / `--control-net` / `-m` resolve installed names (ADR-0005, ADR-0006).
// Input images (img2img init, ControlNet control) are not recorded at all; only
// the parameters that shaped the render.
type imgforgeMeta struct {
	Version    string   `json:"version"`
	Model      string   `json:"model"`
	Prompt     string   `json:"prompt"`
	Negative   string   `json:"negative"`
	Seed       int64    `json:"seed"`
	Steps      int      `json:"steps"`
	CFG        float64  `json:"cfg"`
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Sampler    string   `json:"sampler"`
	Scheduler  string   `json:"scheduler,omitempty"`
	ClipSkip   int      `json:"clip_skip"`
	Prediction string   `json:"prediction,omitempty"`
	VAE        string   `json:"vae,omitempty"` // identifier, never a path
	LoRAs      []string `json:"loras,omitempty"`
	// Credit is the attribution text a model's license requires, combined across
	// every model that shaped the render (base model + LoRAs). Empty when nothing
	// in use requires attribution. It is a record, not machine-readable state:
	// it lets whoever shares the image give the credit the license calls for.
	Credit     string          `json:"credit,omitempty"`
	Img2Img    *img2imgMeta    `json:"img2img,omitempty"`
	Hires      *hiresMeta      `json:"hires,omitempty"`
	ControlNet *controlNetMeta `json:"controlnet,omitempty"`
	Upscale    *upscaleMeta    `json:"upscale,omitempty"`
}

type upscaleMeta struct {
	Upscaler string `json:"upscaler"`
	Factor   int    `json:"factor"`
	Source   string `json:"source"` // base name of the source image, never a path
}

// img2imgMeta records that this was an img2img render and how strongly the init
// image was denoised. The init image itself is deliberately not recorded.
type img2imgMeta struct {
	Strength float64 `json:"strength"`
}

type hiresMeta struct {
	Enabled  bool    `json:"enabled"`
	Scale    float64 `json:"scale"`
	Denoise  float64 `json:"denoise"`
	Upscaler string  `json:"upscaler"`
	Steps    int     `json:"steps"`
	Model    string  `json:"model,omitempty"` // identifier, never a path
}

// controlNetMeta records the ControlNet parameters. The control image itself is
// deliberately not recorded.
type controlNetMeta struct {
	Strength float64 `json:"strength"`
	Canny    bool    `json:"canny"`
}

// imageForgeJSON marshals the generation record to compact JSON. `names` maps an
// absolute model path back to its registry name; it is injected so this stays a
// pure function. Every model reference is rendered as an identifier, never a path.
func imageForgeJSON(req engine.Request, modelName, prediction string, names, credits map[string]string) string {
	m := imgforgeMeta{
		Version:    binVersion,
		Model:      modelName,
		Prompt:     req.Prompt,
		Negative:   req.Negative,
		Seed:       req.Seed,
		Steps:      req.Steps,
		CFG:        req.CFG,
		Width:      req.Width,
		Height:     req.Height,
		Sampler:    req.Sampler,
		Scheduler:  req.Scheduler,
		ClipSkip:   req.ClipSkip,
		Prediction: prediction,
		VAE:        modelIdent(req.VAEPath, names),
		LoRAs:      lorasToStrings(req.LoRAs, names),
		Credit:     buildCredit(req, modelName, names, credits),
	}
	// The init / control images are NOT recorded — only how they shaped the render.
	if req.InitImage != "" {
		m.Img2Img = &img2imgMeta{Strength: req.Strength}
	}
	if req.Hires {
		m.Hires = &hiresMeta{
			Enabled:  true,
			Scale:    req.HiresScale,
			Denoise:  req.HiresDenoise,
			Upscaler: req.HiresUpscaler,
			Steps:    req.HiresSteps,
			Model:    modelIdent(req.HiresModel, names),
		}
	}
	if req.ControlImage != "" {
		m.ControlNet = &controlNetMeta{
			Strength: req.ControlStrength,
			Canny:    req.Canny,
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// modelIdent renders a model file as an identifier that never reveals the
// filesystem: its registry name when installed, else the file's base name.
// Empty in, empty out.
func modelIdent(path string, names map[string]string) string {
	if path == "" {
		return ""
	}
	if n, ok := names[path]; ok && n != "" {
		return n
	}
	return modelBaseName(path)
}

// registryNameByPath builds an absolute-path -> registry-name lookup, so embedded
// metadata can name the models it used instead of pointing at them.
func registryNameByPath() map[string]string {
	reg, err := store.Load()
	if err != nil {
		return nil
	}
	names := make(map[string]string, len(reg.Models))
	for _, im := range reg.Models {
		if im.Path != "" {
			names[im.Path] = im.Name
		}
	}
	return names
}

// attributionByName builds a model-identifier -> attribution lookup so embedded
// metadata can record the credit a license requires. The catalog is the current
// source of truth for cataloged models (attribution may be corrected there after
// install); installed-only models contribute whatever was recorded at install.
func attributionByName() map[string]string {
	out := map[string]string{}
	if reg, err := store.Load(); err == nil {
		for _, im := range reg.Models {
			if im.Attribution != "" {
				out[im.Name] = im.Attribution
			}
		}
	}
	for _, e := range catalog.Default() {
		if e.Attribution != "" {
			out[e.Name] = e.Attribution
		}
	}
	return out
}

// buildCredit assembles the attribution text a license requires: the credit
// strings of every model that shaped the render (base model + LoRAs),
// de-duplicated and joined. Returns "" when no model in use requires attribution.
// `credits` maps a model identifier to its attribution text; `names` resolves a
// LoRA's path back to that identifier. Injected for purity.
func buildCredit(req engine.Request, modelName string, names, credits map[string]string) string {
	if len(credits) == 0 {
		return ""
	}
	var out []string
	seen := map[string]bool{}
	add := func(ident string) {
		c := credits[ident]
		if c == "" || seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	add(modelName)
	for _, l := range req.LoRAs {
		add(modelIdent(l.Path, names))
	}
	return strings.Join(out, " · ")
}

// buildUpscaleMetadata builds the "image-forge" record embedded into a
// standalone-upscale output PNG. upscalerName is the friendly upscaler name (or
// its model-file base name when unnamed); factor is the requested factor (0 =
// the model's native factor); input is the source image path.
//
// The source image's own generation metadata (prompt / seed / params) is carried
// through so the upscaled PNG retains its provenance, with an `upscale`
// sub-record noting how it was produced. When the source carries no image-forge
// metadata, a light record with just the `upscale` sub-record is written. The
// source's AUTOMATIC1111 `parameters` chunk (if any) is carried through verbatim.
func buildUpscaleMetadata(upscalerName, esrganPath string, factor int, input string) []engine.PNGText {
	up := upscalerName
	if up == "" {
		up = modelBaseName(esrganPath)
	}
	upRec := &upscaleMeta{Upscaler: up, Factor: factor, Source: filepath.Base(input)}

	src, params, ok := readForgeMetadata(input)
	var forgeJSON string
	if ok {
		src.Version = binVersion // stamp the upscaling binary
		src.Upscale = upRec
		b, _ := json.Marshal(src)
		forgeJSON = string(b)
	} else {
		rec := struct {
			Version string       `json:"version"`
			Upscale *upscaleMeta `json:"upscale"`
		}{Version: binVersion, Upscale: upRec}
		b, _ := json.Marshal(rec)
		forgeJSON = string(b)
	}

	texts := []engine.PNGText{{Keyword: "image-forge", Text: forgeJSON}}
	if params != "" {
		texts = append(texts, engine.PNGText{Keyword: "parameters", Text: params})
	}
	return texts
}

// readForgeMetadata reads a PNG's embedded metadata: the `image-forge` JSON record
// (decoded into imgforgeMeta) and the raw `parameters` string. ok is true only
// when the image-forge JSON was present and decoded. Missing file / not a PNG /
// no metadata returns zero values.
func readForgeMetadata(path string) (meta imgforgeMeta, params string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return imgforgeMeta{}, "", false
	}
	chunks := engine.ReadPNGText(data)
	params = chunks["parameters"]
	if js, has := chunks["image-forge"]; has {
		if json.Unmarshal([]byte(js), &meta) == nil {
			return meta, params, true
		}
	}
	return imgforgeMeta{}, params, false
}

// lorasToStrings renders LoRAs as "<identifier>:<weight>" entries (nil when
// none) — a registry name where we have one, else the file's base name. Never a
// path: `gen --lora <name>:<weight>` resolves the name back, so this is both
// safe to share and what actually reproduces the image.
func lorasToStrings(loras []engine.LoRA, names map[string]string) []string {
	if len(loras) == 0 {
		return nil
	}
	out := make([]string, len(loras))
	for i, l := range loras {
		out[i] = modelIdent(l.Path, names) + ":" + ftoa(l.Weight)
	}
	return out
}

// modelDisplayName picks the friendly model name for metadata: the registry name
// if set, else the model-file base name (extension stripped), else "".
func modelDisplayName(name, path string) string {
	if name != "" {
		return name
	}
	return modelBaseName(path)
}

// modelBaseName is the file base name with its extension stripped ("" for "").
func modelBaseName(path string) string {
	if path == "" {
		return ""
	}
	b := filepath.Base(path)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// ftoa formats a float without a trailing-zero mantissa (7, 1.5, 0.4).
func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
