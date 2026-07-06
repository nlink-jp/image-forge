// Package catalog is the model catalog: curated, binary-embedded entries plus
// (later) user-extensible ones. Each entry carries the metadata needed to
// surface license and content rating and to build a profile on pull/import.
package catalog

import "github.com/nlink-jp/image-forge/internal/profile"

// Source identifies where a model (and its dedicated VAE) is fetched from.
// A single-file model sets HF / Civitai / URL; a multi-component model (FLUX,
// SD3.5, Z-Image) leaves those empty and sets DiffusionModel + the encoders.
type Source struct {
	HF      string // "org/repo" or "org/repo/file.safetensors"
	Civitai string // model or version id
	URL     string // direct URL
	VAE     string // dedicated VAE source (e.g. madebyollin/sdxl-vae-fp16-fix)

	// Multi-component: each is an hf owner/repo/file reference.
	DiffusionModel string
	ClipL          string
	ClipG          string
	T5XXL          string
	LLM            string // LLM text encoder (e.g. Qwen for Z-Image)
}

// Entry is a catalog record.
type Entry struct {
	Name         string
	Arch         profile.Arch
	Prediction   profile.Prediction
	Rating       profile.Rating
	License      string
	MinRAMGB     int // baseline RAM to run (with the recommended quantization)
	RecRAMGB     int // RAM for a comfortable fp16 / large run
	Source       Source
	ClipSkip     int    // override on top of profile.ArchDefaults(Arch)
	PromptPrefix string // e.g. Pony-family score tags
	Notes        string
	Experimental bool // e.g. v-pred models pending sd.cpp verification
}

// NeedsOptIn reports whether pulling this entry requires an explicit NSFW opt-in.
func (e Entry) NeedsOptIn() bool {
	return e.Rating == profile.RatingQuestionable || e.Rating == profile.RatingExplicit
}

// IsMultiComponent reports whether this model is assembled from separate weight
// files (diffusion model + encoders + VAE) rather than a single checkpoint.
func (e Entry) IsMultiComponent() bool {
	return e.Source.DiffusionModel != ""
}

// Profile builds the generation profile for this entry: architecture defaults
// with the entry's overrides layered on top.
func (e Entry) Profile() profile.Profile {
	p := profile.ArchDefaults(e.Arch)
	p.Name = e.Name
	if e.Prediction != "" {
		p.Prediction = e.Prediction
	}
	if e.ClipSkip != 0 {
		p.ClipSkip = e.ClipSkip
	}
	if e.PromptPrefix != "" {
		p.PromptPrefix = e.PromptPrefix
	}
	return p
}

// Default returns the curated, binary-embedded catalog.
//
// NOTE: Source repo ids below are provisional (RFP stage). Each must be verified
// against the actual HF/Civitai listing before Phase 1 `pull` support ships.
func Default() []Entry {
	return []Entry{
		{
			Name: "sd15-emaonly", Arch: profile.ArchSD15, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "CreativeML OpenRAIL-M",
			MinRAMGB: 8, RecRAMGB: 16,
			Source: Source{HF: "second-state/stable-diffusion-v1-5-GGUF/stable-diffusion-v1-5-pruned-emaonly-Q8_0.gguf"},
			Notes:  "Classic SD1.5 (GGUF, baked VAE). Small; a good smoke-test model.",
		},
		{
			Name: "animagine-xl-4", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Fair AI Public License 1.0-SD",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				HF:  "cagliostrolab/animagine-xl-4.0/animagine-xl-4.0.safetensors",
				VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Anime SDXL, retrained from SDXL 1.0.",
		},
		{
			Name: "illustrious-xl-v1", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "OnomaAI Illustrious license (verify)",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				HF:  "OnomaAIResearch/Illustrious-XL-v1.0/Illustrious-XL-v1.0.safetensors",
				VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Anime SDXL base with a large LoRA ecosystem.",
		},
		{
			Name: "flux1-schnell", Arch: profile.ArchFlux, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "Apache-2.0",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				DiffusionModel: "leejet/FLUX.1-schnell-gguf/flux1-schnell-q4_k.gguf",
				ClipL:          "comfyanonymous/flux_text_encoders/clip_l.safetensors",
				T5XXL:          "comfyanonymous/flux_text_encoders/t5xxl_fp8_e4m3fn.safetensors",
				VAE:            "camenduru/FLUX.1-dev/ae.safetensors", // ungated mirror (bfl repo is gated)
			},
			Notes: "Apache-2.0, fast. Multi-component (GGUF diffusion + CLIP-L + T5 + VAE), ~12 GB.",
		},
		{
			Name: "sd35-medium", Arch: profile.ArchSD35, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "Stability Community License",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				DiffusionModel: "city96/stable-diffusion-3.5-medium-gguf/sd3.5_medium-Q4_K_M.gguf",
				ClipL:          "Comfy-Org/stable-diffusion-3.5-fp8/text_encoders/clip_l.safetensors",
				ClipG:          "Comfy-Org/stable-diffusion-3.5-fp8/text_encoders/clip_g.safetensors",
				T5XXL:          "Comfy-Org/stable-diffusion-3.5-fp8/text_encoders/t5xxl_fp8_e4m3fn.safetensors",
				VAE:            "stabilityai/stable-diffusion-3.5-medium/vae/diffusion_pytorch_model.safetensors",
			},
			Experimental: true,
			Notes:        "SD3.5 Medium (GGUF diffusion + CLIP-L/G + T5). The VAE is gated — set HF_TOKEN. (ComfyUI fp8-scaled builds are not sd.cpp-compatible.)",
		},
		{
			Name: "z-image-turbo", Arch: profile.ArchZImage, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "Apache-2.0 (verify)",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				DiffusionModel: "Comfy-Org/z_image_turbo/split_files/diffusion_models/z_image_turbo_bf16.safetensors",
				LLM:            "Comfy-Org/z_image_turbo/split_files/text_encoders/qwen_3_4b.safetensors",
				VAE:            "Comfy-Org/z_image_turbo/split_files/vae/ae.safetensors",
			},
			Notes: "Efficient turbo model. Multi-component (DiT + Qwen-3-4B LLM + VAE), ~20 GB.",
		},
		{
			Name: "noobai-xl-vpred", Arch: profile.ArchSDXL, Prediction: profile.PredVPred,
			Rating: profile.RatingExplicit, License: "Fair AI Public License 1.0-SD",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				HF:  "Laxhar/noobai-XL-Vpred-1.0/NoobAI-XL-Vpred-v1.0.safetensors",
				VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2,
			Notes:    "v-prediction SDXL (verified; the profile sets prediction=v). NSFW-capable.",
		},
	}
}

// Find returns the entry with the given name, or false.
func Find(name string) (Entry, bool) {
	for _, e := range Default() {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}
