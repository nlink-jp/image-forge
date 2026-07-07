package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/engine"
)

// withVersion pins binVersion for deterministic assertions and restores it.
func withVersion(t *testing.T, v string) {
	t.Helper()
	prev := binVersion
	binVersion = v
	t.Cleanup(func() { binVersion = prev })
}

func TestBuildImageMetadata_EmbedFalseNil(t *testing.T) {
	if got := buildImageMetadata(engine.Request{Prompt: "x"}, "m", "", false); got != nil {
		t.Fatalf("embed=false should return nil, got %v", got)
	}
}

func TestBuildImageMetadata_Txt2Img(t *testing.T) {
	withVersion(t, "v9.9.9")
	req := engine.Request{
		Prompt:   "a cat",
		Seed:     20240707,
		Steps:    26,
		CFG:      7,
		Width:    1024,
		Height:   1024,
		Sampler:  "euler_a",
		ClipSkip: 2,
	}
	texts := buildImageMetadata(req, "prefect-pony-xl", "", true)
	if len(texts) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(texts))
	}
	if texts[0].Keyword != "parameters" || texts[1].Keyword != "image-forge" {
		t.Fatalf("unexpected keywords: %q, %q", texts[0].Keyword, texts[1].Keyword)
	}

	params := texts[0].Text
	// No negative-prompt line when Negative is empty.
	if strings.Contains(params, "Negative prompt:") {
		t.Errorf("unexpected negative-prompt line in:\n%s", params)
	}
	// A1111 settings line present with the expected fields.
	for _, want := range []string{
		"a cat\n",
		"Steps: 26", "Sampler: euler_a", "CFG scale: 7",
		"Seed: 20240707", "Size: 1024x1024", "Model: prefect-pony-xl",
		"Clip skip: 2", "Version: image-forge v9.9.9",
	} {
		if !strings.Contains(params, want) {
			t.Errorf("parameters missing %q in:\n%s", want, params)
		}
	}
	if strings.Contains(params, "Denoising strength") || strings.Contains(params, "Hires") {
		t.Errorf("txt2img should not carry Denoising/Hires:\n%s", params)
	}

	// The image-forge entry is valid JSON containing the model + seed.
	var m map[string]any
	if err := json.Unmarshal([]byte(texts[1].Text), &m); err != nil {
		t.Fatalf("image-forge JSON invalid: %v\n%s", err, texts[1].Text)
	}
	if m["model"] != "prefect-pony-xl" {
		t.Errorf("json model = %v, want prefect-pony-xl", m["model"])
	}
	if m["seed"].(float64) != 20240707 {
		t.Errorf("json seed = %v, want 20240707", m["seed"])
	}
	if _, ok := m["img2img"]; ok {
		t.Errorf("txt2img JSON should omit img2img: %s", texts[1].Text)
	}
	if _, ok := m["hires"]; ok {
		t.Errorf("txt2img JSON should omit hires: %s", texts[1].Text)
	}
}

func TestBuildImageMetadata_ClipSkip1Omitted(t *testing.T) {
	req := engine.Request{Prompt: "p", Steps: 20, Width: 512, Height: 512, ClipSkip: 1}
	params := buildImageMetadata(req, "m", "", true)[0].Text
	if strings.Contains(params, "Clip skip") {
		t.Errorf("clip skip 1 should be omitted:\n%s", params)
	}
}

func TestBuildImageMetadata_Img2ImgAddsDenoising(t *testing.T) {
	req := engine.Request{
		Prompt: "a dog", Negative: "blurry",
		Seed: 42, Steps: 30, CFG: 6.5, Width: 768, Height: 768, Sampler: "dpmpp2m",
		InitImage: "in.png", Strength: 0.4,
	}
	texts := buildImageMetadata(req, "juggernaut-xl", "", true)
	params := texts[0].Text
	if !strings.Contains(params, "\nNegative prompt: blurry\n") {
		t.Errorf("expected negative-prompt line:\n%s", params)
	}
	if !strings.Contains(params, "Denoising strength: 0.4") {
		t.Errorf("expected Denoising strength: 0.4 in:\n%s", params)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(texts[1].Text), &m); err != nil {
		t.Fatal(err)
	}
	sub, ok := m["img2img"].(map[string]any)
	if !ok {
		t.Fatalf("expected img2img object: %s", texts[1].Text)
	}
	if sub["init"] != "in.png" || sub["strength"].(float64) != 0.4 {
		t.Errorf("img2img record wrong: %v", sub)
	}
}

func TestBuildImageMetadata_HiresFields(t *testing.T) {
	req := engine.Request{
		Prompt: "p", Steps: 24, Width: 832, Height: 1216, Sampler: "euler",
		Hires: true, HiresScale: 1.5, HiresDenoise: 0.5, HiresUpscaler: "latent", HiresSteps: 12,
	}
	texts := buildImageMetadata(req, "m", "v", true)
	params := texts[0].Text
	if !strings.Contains(params, "Hires upscale: 1.5") || !strings.Contains(params, "Hires upscaler: latent") {
		t.Errorf("expected hires fields in:\n%s", params)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(texts[1].Text), &m); err != nil {
		t.Fatal(err)
	}
	if m["prediction"] != "v" {
		t.Errorf("json prediction = %v, want v", m["prediction"])
	}
	h, ok := m["hires"].(map[string]any)
	if !ok {
		t.Fatalf("expected hires object: %s", texts[1].Text)
	}
	if h["scale"].(float64) != 1.5 || h["upscaler"] != "latent" {
		t.Errorf("hires record wrong: %v", h)
	}
}

func TestBuildUpscaleMetadata(t *testing.T) {
	withVersion(t, "v1.2.3")
	texts := buildUpscaleMetadata("realesrgan-x4plus", "/models/x4.pth", 4, "/tmp/in.png")
	if len(texts) != 1 || texts[0].Keyword != "image-forge" {
		t.Fatalf("want one image-forge chunk, got %v", texts)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(texts[0].Text), &m); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, texts[0].Text)
	}
	if m["upscaler"] != "realesrgan-x4plus" || m["factor"].(float64) != 4 ||
		m["source"] != "in.png" || m["version"] != "v1.2.3" {
		t.Errorf("upscale record wrong: %v", m)
	}
	// Unnamed upscaler falls back to the model-file base name.
	texts2 := buildUpscaleMetadata("", "/models/x4-anime.pth", 0, "in.jpg")
	var m2 map[string]any
	_ = json.Unmarshal([]byte(texts2[0].Text), &m2)
	if m2["upscaler"] != "x4-anime" {
		t.Errorf("unnamed upscaler = %v, want x4-anime", m2["upscaler"])
	}
}

func TestModelDisplayName(t *testing.T) {
	if got := modelDisplayName("animagine-xl-4", ""); got != "animagine-xl-4" {
		t.Errorf("name case = %q", got)
	}
	if got := modelDisplayName("", "/a/b/model.safetensors"); got != "model" {
		t.Errorf("path case = %q", got)
	}
	if got := modelDisplayName("", ""); got != "" {
		t.Errorf("empty case = %q", got)
	}
}
