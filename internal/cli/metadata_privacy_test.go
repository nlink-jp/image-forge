package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/engine"
)

// Generated images are meant to be shared. Embedded metadata must therefore never
// carry a filesystem path: it leaks the machine's layout (and, via the home
// directory, the user's name) and is useless on anyone else's machine. Models are
// recorded by identifier instead — which is also what reproduces the image, since
// `-m` / `--lora` / `--control-net` resolve installed names. See ADR-0005/0006.
func TestImageForgeJSON_NeverEmbedsPaths(t *testing.T) {
	withVersion(t, "v9")
	req := engine.Request{
		Prompt:    "a cat",
		ModelPath: "/Volumes/Works/models/animagine-xl-4.0.safetensors",
		VAEPath:   "/Volumes/Works/models/sdxl.vae.safetensors",
		InitImage: "/Users/alice/Pictures/private-photo.png",
		Seed:      7, Steps: 6, CFG: 1.5, Width: 1024, Height: 1024,
		Sampler:  "lcm",
		Strength: 0.55,
		LoRAs: []engine.LoRA{
			{Path: "/Volumes/Works/models/pytorch_lora_weights.safetensors", Weight: 1},
			{Path: "/Users/alice/loras/secret-style.safetensors", Weight: 0.5},
		},
		ControlImage:    "/Users/alice/Pictures/edges.png",
		ControlStrength: 0.9,
		Canny:           true,
		Hires:           true,
		HiresScale:      1.5,
		HiresUpscaler:   "model",
		HiresModel:      "/Volumes/Works/models/RealESRGAN_x4plus.pth",
	}
	// Only the first LoRA and the base model are installed under a registry name.
	names := map[string]string{
		"/Volumes/Works/models/pytorch_lora_weights.safetensors": "lcm-lora-sdxl",
		"/Volumes/Works/models/RealESRGAN_x4plus.pth":            "realesrgan-x4plus",
	}

	raw := imageForgeJSON(req, "animagine-xl-4", "", names, nil)

	// (a) The blunt guarantee: no absolute path, and nothing resembling one.
	for _, leak := range []string{"/Volumes", "/Users", "alice", ".safetensors", ".pth", "private-photo", "edges.png"} {
		if strings.Contains(raw, leak) {
			t.Errorf("metadata leaks %q:\n%s", leak, raw)
		}
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// (b) model_path / vae_path are gone entirely.
	if _, ok := m["model_path"]; ok {
		t.Error("model_path must not be embedded")
	}
	if _, ok := m["vae_path"]; ok {
		t.Error("vae_path must not be embedded")
	}

	// (c) Models are still identified — by registry name, else base name.
	if m["model"] != "animagine-xl-4" {
		t.Errorf("model = %v", m["model"])
	}
	if m["vae"] != "sdxl.vae" {
		t.Errorf("vae = %v, want the base name", m["vae"])
	}
	loras, _ := m["loras"].([]any)
	if len(loras) != 2 || loras[0] != "lcm-lora-sdxl:1" || loras[1] != "secret-style:0.5" {
		t.Errorf("loras = %v; want registry name then base name", loras)
	}
	hires, _ := m["hires"].(map[string]any)
	if hires["model"] != "realesrgan-x4plus" {
		t.Errorf("hires.model = %v", hires["model"])
	}

	// (d) Input images are not recorded at all — only how they shaped the render.
	img2img, _ := m["img2img"].(map[string]any)
	if img2img["strength"].(float64) != 0.55 {
		t.Errorf("img2img.strength = %v", img2img["strength"])
	}
	if _, ok := img2img["init"]; ok {
		t.Error("img2img must not record the init image")
	}
	cn, _ := m["controlnet"].(map[string]any)
	if cn["strength"].(float64) != 0.9 || cn["canny"] != true {
		t.Errorf("controlnet = %v", cn)
	}
	if _, ok := cn["image"]; ok {
		t.Error("controlnet must not record the control image")
	}
}

// modelIdent prefers the registry name, falls back to the base name, and never
// returns a path.
func TestModelIdent(t *testing.T) {
	names := map[string]string{"/m/lcm.safetensors": "lcm-lora-sdxl"}
	if got := modelIdent("/m/lcm.safetensors", names); got != "lcm-lora-sdxl" {
		t.Errorf("registry name = %q", got)
	}
	if got := modelIdent("/some/deep/dir/mystery.safetensors", names); got != "mystery" {
		t.Errorf("base-name fallback = %q", got)
	}
	if got := modelIdent("", names); got != "" {
		t.Errorf("empty = %q", got)
	}
	// An unnamed entry must not fall through to the path.
	if got := modelIdent("/m/x.pth", map[string]string{"/m/x.pth": ""}); got != "x" {
		t.Errorf("blank registry name should fall back to base name, got %q", got)
	}
}

// The A1111 `parameters` chunk was already path-free; keep it that way.
func TestA1111ParametersNeverEmbedsPaths(t *testing.T) {
	withVersion(t, "v9")
	req := engine.Request{
		Prompt: "a cat", ModelPath: "/Users/alice/models/m.safetensors",
		InitImage: "/Users/alice/in.png", Strength: 0.4,
		Seed: 1, Steps: 20, CFG: 7, Width: 512, Height: 512, Sampler: "euler_a",
	}
	got := a1111Parameters(req, "juggernaut-xl")
	for _, leak := range []string{"/Users", "alice", ".safetensors", "in.png"} {
		if strings.Contains(got, leak) {
			t.Errorf("parameters chunk leaks %q:\n%s", leak, got)
		}
	}
}
