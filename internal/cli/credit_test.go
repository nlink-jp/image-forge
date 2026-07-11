package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/engine"
)

// buildCredit gathers the attribution text of every model that shaped a render —
// base model plus each LoRA — de-duplicated and joined.
func TestBuildCredit(t *testing.T) {
	credits := map[string]string{
		"illustrious-xl-v1": "Illustrious XL by ONOMAAI (Civitai)",
		"genba-neko":        "Genba Neko Like by HypnotistDolphin (Civitai)",
		"shared":            "Same Studio",
		"shared-lora":       "Same Studio", // duplicate text, different identifier
	}
	names := map[string]string{
		"/m/genba.safetensors":  "genba-neko",
		"/m/shared.safetensors": "shared-lora",
		"/m/free.safetensors":   "free-lora", // not in credits: no attribution
	}

	tests := []struct {
		name    string
		model   string
		loras   []engine.LoRA
		credits map[string]string
		want    string
	}{
		{
			name:    "no credits map yields empty",
			model:   "illustrious-xl-v1",
			credits: nil,
			want:    "",
		},
		{
			name:    "base model only",
			model:   "illustrious-xl-v1",
			credits: credits,
			want:    "Illustrious XL by ONOMAAI (Civitai)",
		},
		{
			name:    "permissive base, no attribution",
			model:   "juggernaut-xl",
			credits: credits,
			want:    "",
		},
		{
			name:    "base plus a crediting LoRA",
			model:   "illustrious-xl-v1",
			loras:   []engine.LoRA{{Path: "/m/genba.safetensors", Weight: 0.8}},
			credits: credits,
			want:    "Illustrious XL by ONOMAAI (Civitai) · Genba Neko Like by HypnotistDolphin (Civitai)",
		},
		{
			name:    "a free LoRA contributes nothing",
			model:   "juggernaut-xl",
			loras:   []engine.LoRA{{Path: "/m/free.safetensors", Weight: 1}},
			credits: credits,
			want:    "",
		},
		{
			name:    "identical credit text is de-duplicated",
			model:   "shared",
			loras:   []engine.LoRA{{Path: "/m/shared.safetensors", Weight: 1}},
			credits: credits,
			want:    "Same Studio",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := engine.Request{LoRAs: tt.loras}
			if got := buildCredit(req, tt.model, names, tt.credits); got != tt.want {
				t.Errorf("buildCredit = %q, want %q", got, tt.want)
			}
		})
	}
}

// The credit is embedded into the image-forge JSON record, and omitted entirely
// when nothing in use requires attribution.
func TestCreditInMetadataJSON(t *testing.T) {
	req := engine.Request{Prompt: "p", LoRAs: []engine.LoRA{{Path: "/m/genba.safetensors", Weight: 0.8}}}
	names := map[string]string{"/m/genba.safetensors": "genba-neko"}
	credits := map[string]string{
		"illustrious-xl-v1": "Illustrious XL by ONOMAAI (Civitai)",
		"genba-neko":        "Genba Neko Like by HypnotistDolphin (Civitai)",
	}

	raw := imageForgeJSON(req, "illustrious-xl-v1", "", names, credits)
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	got, _ := m["credit"].(string)
	if !strings.Contains(got, "ONOMAAI") || !strings.Contains(got, "HypnotistDolphin") {
		t.Errorf("credit = %q, want both model and LoRA attributions", got)
	}

	// A permissive render carries no credit key at all.
	rawFree := imageForgeJSON(engine.Request{Prompt: "p"}, "juggernaut-xl", "", nil, nil)
	var mf map[string]any
	if err := json.Unmarshal([]byte(rawFree), &mf); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := mf["credit"]; ok {
		t.Errorf("permissive render should omit credit, got %v", mf["credit"])
	}
}

// attributionByName exposes the real catalog attributions, backfilled onto the
// identifier a generation records. The base model and LoRA names must resolve.
func TestAttributionByNameFromCatalog(t *testing.T) {
	credits := attributionByName()
	for _, name := range []string{"illustrious-xl-v1", "genba-neko-illustrious", "sd35-medium"} {
		if credits[name] == "" {
			t.Errorf("attributionByName missing credit for %s", name)
		}
	}
}
