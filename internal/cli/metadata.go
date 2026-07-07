package cli

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nlink-jp/image-forge/internal/engine"
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
	return []engine.PNGText{
		{Keyword: "parameters", Text: a1111Parameters(req, modelName)},
		{Keyword: "image-forge", Text: imageForgeJSON(req, modelName, prediction)},
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

// imgforgeMeta is the flat, lossless JSON record embedded under the "image-forge"
// keyword. Sub-records are pointers so they omit entirely when not applicable.
type imgforgeMeta struct {
	Version    string          `json:"version"`
	Model      string          `json:"model"`
	ModelPath  string          `json:"model_path"`
	Prompt     string          `json:"prompt"`
	Negative   string          `json:"negative"`
	Seed       int64           `json:"seed"`
	Steps      int             `json:"steps"`
	CFG        float64         `json:"cfg"`
	Width      int             `json:"width"`
	Height     int             `json:"height"`
	Sampler    string          `json:"sampler"`
	Scheduler  string          `json:"scheduler,omitempty"`
	ClipSkip   int             `json:"clip_skip"`
	Prediction string          `json:"prediction,omitempty"`
	VAEPath    string          `json:"vae_path,omitempty"`
	LoRAs      []string        `json:"loras,omitempty"`
	Img2Img    *img2imgMeta    `json:"img2img,omitempty"`
	Hires      *hiresMeta      `json:"hires,omitempty"`
	ControlNet *controlNetMeta `json:"controlnet,omitempty"`
}

type img2imgMeta struct {
	Init     string  `json:"init"`
	Strength float64 `json:"strength"`
}

type hiresMeta struct {
	Enabled  bool    `json:"enabled"`
	Scale    float64 `json:"scale"`
	Denoise  float64 `json:"denoise"`
	Upscaler string  `json:"upscaler"`
	Steps    int     `json:"steps"`
	Model    string  `json:"model"`
}

type controlNetMeta struct {
	Image    string  `json:"image"`
	Strength float64 `json:"strength"`
	Canny    bool    `json:"canny"`
}

// imageForgeJSON marshals the lossless generation record to compact JSON.
func imageForgeJSON(req engine.Request, modelName, prediction string) string {
	m := imgforgeMeta{
		Version:    binVersion,
		Model:      modelName,
		ModelPath:  req.ModelPath,
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
		VAEPath:    req.VAEPath,
		LoRAs:      lorasToStrings(req.LoRAs),
	}
	if req.InitImage != "" {
		m.Img2Img = &img2imgMeta{Init: req.InitImage, Strength: req.Strength}
	}
	if req.Hires {
		m.Hires = &hiresMeta{
			Enabled:  true,
			Scale:    req.HiresScale,
			Denoise:  req.HiresDenoise,
			Upscaler: req.HiresUpscaler,
			Steps:    req.HiresSteps,
			Model:    req.HiresModel,
		}
	}
	if req.ControlImage != "" {
		m.ControlNet = &controlNetMeta{
			Image:    req.ControlImage,
			Strength: req.ControlStrength,
			Canny:    req.Canny,
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// buildUpscaleMetadata builds the light "image-forge" record embedded into a
// standalone-upscale output PNG. upscalerName is the friendly upscaler name (or
// its model-file base name when unnamed); factor is the requested factor (0 =
// the model's native factor); input is the source image path.
func buildUpscaleMetadata(upscalerName, esrganPath string, factor int, input string) []engine.PNGText {
	up := upscalerName
	if up == "" {
		up = modelBaseName(esrganPath)
	}
	rec := struct {
		Version  string `json:"version"`
		Upscaler string `json:"upscaler"`
		Factor   int    `json:"factor"`
		Source   string `json:"source"`
	}{
		Version:  binVersion,
		Upscaler: up,
		Factor:   factor,
		Source:   filepath.Base(input),
	}
	b, _ := json.Marshal(rec)
	return []engine.PNGText{{Keyword: "image-forge", Text: string(b)}}
}

// lorasToStrings renders LoRAs as "path:weight" entries (nil when none).
func lorasToStrings(loras []engine.LoRA) []string {
	if len(loras) == 0 {
		return nil
	}
	out := make([]string, len(loras))
	for i, l := range loras {
		out[i] = l.Path + ":" + ftoa(l.Weight)
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
