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

// Kind classifies a catalog entry. The empty string means a diffusion model (the
// default); the rest are auxiliary models that are not renderable on their own.
// LoRA / ControlNet entries are bound to a base architecture (see ADR-0006);
// upscalers are architecture-agnostic.
const (
	KindDiffusion  = ""
	KindUpscaler   = "upscaler"
	KindLoRA       = "lora"
	KindControlNet = "controlnet"
)

// Entry is a catalog record.
type Entry struct {
	Name         string
	Kind         string // "" (diffusion, default) | upscaler | lora | controlnet
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

	// TriggerWords are the activation tokens a LoRA needs in the prompt to take
	// effect (Civitai calls them "trained words"). Without them the LoRA loads but
	// does nothing, so they must survive installation — they are copied onto the
	// registry entry and surfaced by `models list --json`. Empty for LoRAs that
	// need no trigger (LCM, sliders) and for non-LoRA kinds.
	TriggerWords []string

	// hires.fix defaults surfaced through the profile. Set HiresEnabled on a model
	// whose upstream notes recommend "always use hires".
	HiresEnabled  bool
	HiresScale    float64
	HiresDenoise  float64
	HiresUpscaler string
	HiresSteps    int
}

// NeedsOptIn reports whether pulling this entry requires an explicit NSFW opt-in.
func (e Entry) NeedsOptIn() bool {
	return e.Rating == profile.RatingQuestionable || e.Rating == profile.RatingExplicit
}

// IsUpscaler reports whether this entry is a standalone ESRGAN upscaler rather
// than a diffusion model.
func (e Entry) IsUpscaler() bool { return e.Kind == KindUpscaler }

// IsLoRA reports whether this entry is a LoRA adapter applied on top of a base
// diffusion model of the same Arch.
func (e Entry) IsLoRA() bool { return e.Kind == KindLoRA }

// IsControlNet reports whether this entry is a ControlNet model loaded alongside
// a base diffusion model of the same Arch.
func (e Entry) IsControlNet() bool { return e.Kind == KindControlNet }

// IsAuxiliary reports whether this entry is a non-renderable helper model
// (upscaler / LoRA / ControlNet) rather than a base diffusion model.
func (e Entry) IsAuxiliary() bool { return e.Kind != KindDiffusion }

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
	p.HiresEnabled = e.HiresEnabled
	p.HiresScale = e.HiresScale
	p.HiresDenoise = e.HiresDenoise
	p.HiresUpscaler = e.HiresUpscaler
	p.HiresSteps = e.HiresSteps
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
			Name: "illustrious-xl-v1.1", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "OnomaAI Illustrious license (verify)",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				Civitai: "1411690", // https://civitai.com/models/1252206 (v1.1)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Illustrious XL v1.1 (Civitai): refined v1 anime base. Needs CIVITAI_TOKEN.",
		},
		{
			Name: "akium-unmotivated", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Illustrious-derived; see Civitai listing",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				Civitai: "3046291", // https://civitai.com/models/2711644 (v1.0)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Illustrious-based anime merge (Civitai). Needs CIVITAI_TOKEN.",
		},
		{
			Name: "t-ponynai3-v7", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Pony-derived; see Civitai listing",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				Civitai: "1392706", // https://civitai.com/models/317902 (v7)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip:     2,
			PromptPrefix: "score_9, score_8_up, score_7_up, score_6_up, score_5_up, score_4_up",
			Notes:        "Pony-based anime merge (Civitai), latest tPonynai3. Score tags auto-prefixed. Needs CIVITAI_TOKEN.",
		},
		{
			Name: "t-ponynai3-v5.5", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Pony-derived; see Civitai listing",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				Civitai: "593760", // https://civitai.com/models/317902 (v5.5)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip:     2,
			PromptPrefix: "score_9, score_8_up, score_7_up, score_6_up, score_5_up, score_4_up",
			Notes:        "Pony-based anime merge (Civitai), earlier tPonynai3 revision. Score tags auto-prefixed. Needs CIVITAI_TOKEN.",
		},
		{
			Name: "momoiro-pony", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingExplicit, License: "Pony-derived; see Civitai listing",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				Civitai: "425904", // https://civitai.com/models/381535 (v1.0)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip:     2,
			PromptPrefix: "score_9, score_8_up, score_7_up, score_6_up, score_5_up, score_4_up",
			Notes:        "T-ponynai MomoiroPony (Civitai): Pony-based anime merge, NSFW-leaning. Score tags auto-prefixed. Needs CIVITAI_TOKEN.",
		},
		{
			Name: "prefect-pony-xl", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Pony-derived; see Civitai listing",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				Civitai: "2114187", // https://civitai.com/models/439889 (v6)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip:     2,
			PromptPrefix: "score_9, score_8_up, score_7_up, score_6_up, score_5_up, score_4_up",
			// Its Civitai page recommends hires.fix; ship it on by default so
			// `gen -m prefect-pony-xl` is hires-quality with no extra flags. Users
			// override with --hires off.
			HiresEnabled: true, HiresScale: 1.5, HiresDenoise: 0.5, HiresUpscaler: "latent",
			Notes: "Prefect Pony XL v6 (Civitai): high-quality Pony-based anime/general SDXL. Score tags auto-prefixed. hires.fix on by default. Needs CIVITAI_TOKEN.",
		},
		{
			Name: "realvisxl-v5", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "OpenRAIL++-M",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				HF:  "SG161222/RealVisXL_V5.0/RealVisXL_V5.0_fp16.safetensors",
				VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 1, // photorealistic SDXL: clip-skip 1 (not the anime default of 2)
			Notes:    "Photorealistic SDXL (RealVis V5.0), the realism go-to. NSFW-capable.",
		},
		{
			Name: "juggernaut-xl-v9", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "CreativeML OpenRAIL-M",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				HF:  "RunDiffusion/Juggernaut-XL-v9/Juggernaut-XL_v9_RunDiffusionPhoto_v2.safetensors",
				VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 1, // photorealistic SDXL: clip-skip 1 (not the anime default of 2)
			Notes:    "Photorealistic SDXL (Juggernaut XL v9), versatile realism. NSFW-capable.",
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
				VAE:            "adamo1139/stable-diffusion-3.5-large-turbo-ungated/vae/diffusion_pytorch_model.safetensors",
			},
			Notes: "SD3.5 Medium (GGUF diffusion + CLIP-L/G + T5 + VAE), multi-component. ~2 GB new download (encoders shared with FLUX).",
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
		{
			Name: "realesrgan-x4plus", Kind: KindUpscaler,
			Rating: profile.RatingSafe, License: "BSD-3-Clause",
			MinRAMGB: 4, RecRAMGB: 8,
			Source: Source{HF: "schwgHao/RealESRGAN_x4plus/RealESRGAN_x4plus.pth"},
			Notes:  "Real-ESRGAN x4 general-purpose upscaler (ESRGAN). For `image-forge upscale` and `gen --hires-upscaler model --hires-model realesrgan-x4plus`.",
		},
		{
			Name: "realesrgan-x4-anime", Kind: KindUpscaler,
			Rating: profile.RatingSafe, License: "BSD-3-Clause",
			MinRAMGB: 4, RecRAMGB: 8,
			Source: Source{HF: "utnah/esrgan/RealESRGAN_x4plus_anime_6B.pth"},
			Notes:  "Real-ESRGAN x4 anime-tuned upscaler (ESRGAN, 6B). For `image-forge upscale` and `gen --hires-upscaler model --hires-model realesrgan-x4-anime`.",
		},

		// LoRA adapters (ADR-0006). Bound to a base architecture; applied per
		// render with `gen --lora <name>:<weight>` (no model reload).
		{
			Name: "lcm-lora-sdxl", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingSafe, License: "OpenRAIL++",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{HF: "latent-consistency/lcm-lora-sdxl/pytorch_lora_weights.safetensors"},
			Notes:  "Latent Consistency LoRA for SDXL: few-step sampling. Use ~4-8 steps, CFG ~1-2, sampler lcm. e.g. `gen --lora lcm-lora-sdxl:1.0 --steps 6 --cfg 1.5 --sampler lcm`.",
		},
		{
			Name: "lcm-lora-sd15", Kind: KindLoRA, Arch: profile.ArchSD15,
			Rating: profile.RatingSafe, License: "OpenRAIL++",
			MinRAMGB: 8, RecRAMGB: 16,
			Source: Source{HF: "latent-consistency/lcm-lora-sdv1-5/pytorch_lora_weights.safetensors"},
			Notes:  "Latent Consistency LoRA for SD1.5: few-step sampling. Use ~4-8 steps, CFG ~1-2, sampler lcm.",
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
