// Package catalog is the model catalog: curated, binary-embedded entries plus
// (later) user-extensible ones. Each entry carries the metadata needed to
// surface license and content rating and to build a profile on pull/import.
package catalog

import (
	"strings"

	"github.com/nlink-jp/image-forge/internal/profile"
)

// Source identifies where a model (and its dedicated VAE) is fetched from.
// A single-file model sets HF / Civitai / URL; a multi-component model (FLUX,
// SD3.5, Z-Image) leaves those empty and sets DiffusionModel + the encoders.
type Source struct {
	HF      string // "org/repo" or "org/repo/file.safetensors"
	Civitai string // model or version id
	URL     string // direct URL
	VAE     string // dedicated VAE source (e.g. madebyollin/sdxl-vae-fp16-fix)

	// Multi-component: each is an hf owner/repo/file reference. DiffusionModel also
	// accepts a "civitai:<versionId>" ref (a Civitai-hosted DiT paired with the
	// HF-hosted encoders/VAE below — e.g. an Anima checkpoint from Civitai).
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

// License flags surface notable usage restrictions of a model to the user (a
// front-end can highlight them). They are stable identifiers derived from the
// source listing's terms — informational, not legal advice. A model may carry
// several, or none (permissive). The License string still holds the full text.
const (
	LicenseNonCommercial = "non-commercial" // commercial use of outputs not permitted
	LicenseNoDerivatives = "no-derivatives" // derivative works not permitted
	LicenseAttribution   = "attribution"    // credit / attribution required
	LicenseShareAlike    = "share-alike"    // derivatives must keep the same license
	LicenseReview        = "review-license" // terms unclear / model-specific — check before relying on it
)

// Entry is a catalog record.
type Entry struct {
	Name         string
	Kind         string // "" (diffusion, default) | upscaler | lora | controlnet
	Arch         profile.Arch
	Prediction   profile.Prediction
	Rating       profile.Rating
	License      string
	LicenseFlags []string // notable restrictions (non-commercial / no-derivatives / …)
	// Attribution is the credit text to give when a license requires it — written
	// into the output PNG's metadata and shown by a front-end. Set it whenever
	// LicenseFlags includes attribution.
	Attribution  string
	MinRAMGB     int // baseline RAM to run (with the recommended quantization)
	RecRAMGB     int // RAM for a comfortable fp16 / large run
	Source       Source
	ClipSkip     int     // override on top of profile.ArchDefaults(Arch)
	Steps        int     // sampling-steps override (0 = arch default); e.g. non-distilled FLUX.1-dev needs ~20, not schnell's 4
	CFG          float64 // CFG override (0 = arch default)
	PromptPrefix string  // e.g. Pony-family score tags
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

// CivitaiRef reports whether a component ref is a Civitai reference of the form
// "civitai:<versionId>", returning the version id. The catalog owns this format
// (a Source field convention), and both the download router (internal/cli) and
// PageURL below rely on it.
func CivitaiRef(ref string) (versionID string, ok bool) {
	if id, found := strings.CutPrefix(ref, "civitai:"); found {
		return id, true
	}
	return "", false
}

// PageURL returns the human-facing web page for this model's source — the
// Civitai model page or the Hugging Face repo — derived from Source, plus true.
// A front-end (or `models open`) can send the user straight there instead of
// making them search. Returns ("", false) when no page can be formed (e.g. a
// bare-URL source, or a model with no web home).
//
// Civitai stores only a version id, but https://civitai.com/model-versions/<id>
// 308-redirects to the canonical model page (…/models/<modelId>?modelVersionId=<id>),
// so the version id alone is enough — no model id or API call needed.
func (e Entry) PageURL() (string, bool) {
	s := e.Source
	// A Civitai source wins: either the single-file Civitai id or a civitai: DiT.
	if s.Civitai != "" {
		return civitaiVersionURL(s.Civitai), true
	}
	if vid, ok := CivitaiRef(s.DiffusionModel); ok && vid != "" {
		return civitaiVersionURL(vid), true
	}
	// Hugging Face: the repo page is owner/repo (any file path is dropped).
	if u, ok := hfRepoURL(s.HF); ok {
		return u, true
	}
	if u, ok := hfRepoURL(s.DiffusionModel); ok {
		return u, true
	}
	return "", false
}

func civitaiVersionURL(versionID string) string {
	return "https://civitai.com/model-versions/" + versionID
}

// hfRepoURL turns an "owner/repo[/file...]" ref into the repo's Hugging Face page.
// A "civitai:" ref or a value without at least owner/repo yields no URL.
func hfRepoURL(ref string) (string, bool) {
	if ref == "" {
		return "", false
	}
	if _, isCivitai := CivitaiRef(ref); isCivitai {
		return "", false
	}
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return "https://huggingface.co/" + parts[0] + "/" + parts[1], true
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
	if e.Steps != 0 {
		p.Steps = e.Steps
	}
	if e.CFG != 0 {
		p.CFG = e.CFG
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
			Rating: profile.RatingQuestionable, License: "CreativeML OpenRAIL++-M (per the HF model card)",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				HF:  "cagliostrolab/animagine-xl-4.0/animagine-xl-4.0.safetensors",
				VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Anime SDXL, retrained from SDXL 1.0.",
		},
		{
			Name: "illustrious-xl-v1", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "OnomaAI Illustrious License: no derivatives, credit required",
			LicenseFlags: []string{LicenseNoDerivatives, LicenseAttribution},
			Attribution:  "Illustrious XL by ONOMAAI (Civitai)",
			MinRAMGB:     16, RecRAMGB: 32,
			Source: Source{
				HF:  "OnomaAIResearch/Illustrious-XL-v1.0/Illustrious-XL-v1.0.safetensors",
				VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Anime SDXL base with a large LoRA ecosystem.",
		},
		{
			Name: "illustrious-xl-v1.1", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "OnomaAI Illustrious License: no derivatives, credit required",
			LicenseFlags: []string{LicenseNoDerivatives, LicenseAttribution},
			Attribution:  "Illustrious XL by ONOMAAI (Civitai)",
			MinRAMGB:     16, RecRAMGB: 32,
			Source: Source{
				Civitai: "1411690", // https://civitai.com/models/1252206 (v1.1)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Illustrious XL v1.1 (Civitai): refined v1 anime base. Needs CIVITAI_TOKEN.",
		},
		{
			Name: "akium-unmotivated", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Civitai listing: images non-commercial (rent-only), derivatives allowed",
			LicenseFlags: []string{LicenseNonCommercial},
			MinRAMGB:     16, RecRAMGB: 32,
			Source: Source{
				Civitai: "3046291", // https://civitai.com/models/2711644 (v1.0)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Illustrious-based anime merge (Civitai). Needs CIVITAI_TOKEN.",
		},
		{
			Name: "akium-ijin", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Civitai listing: images non-commercial (rent-only), derivatives allowed, credit required",
			LicenseFlags: []string{LicenseNonCommercial, LicenseAttribution},
			Attribution:  "Akium (Civitai)",
			MinRAMGB:     16, RecRAMGB: 32,
			Source: Source{
				Civitai: "3081528", // https://civitai.com/models/2740167 (Akium IJIN v1.0)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Illustrious-based anime / 2.5D semi-real SDXL (Akium IJIN, Civitai). Needs CIVITAI_TOKEN.",
		},
		{
			Name: "akium-lumen", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingExplicit, License: "Civitai listing: images non-commercial (rent-only), derivatives allowed",
			LicenseFlags: []string{LicenseNonCommercial},
			MinRAMGB:     16, RecRAMGB: 32,
			Source: Source{
				Civitai: "2962026", // https://civitai.com/models/2385399 (Akium Lumen ILL - base v4.0)
				VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2, Notes: "Illustrious-based anime SDXL base (Akium Lumen ILL, Civitai). Needs CIVITAI_TOKEN.",
		},
		{
			Name: "t-ponynai3-v7", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Civitai listing: NO derivatives; images may be used commercially",
			LicenseFlags: []string{LicenseNoDerivatives},
			MinRAMGB:     16, RecRAMGB: 32,
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
			Rating: profile.RatingQuestionable, License: "Civitai listing: NO derivatives; images may be used commercially",
			LicenseFlags: []string{LicenseNoDerivatives},
			MinRAMGB:     16, RecRAMGB: 32,
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
			Rating: profile.RatingExplicit, License: "Civitai listing: NO commercial use, credit required, derivatives allowed",
			LicenseFlags: []string{LicenseNonCommercial, LicenseAttribution},
			Attribution:  "T-ponynai MomoiroPony by superiorenby (Civitai)",
			MinRAMGB:     16, RecRAMGB: 32,
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
			Rating: profile.RatingQuestionable, License: "Civitai listing: images non-commercial (rent-only), NO derivatives, credit required",
			LicenseFlags: []string{LicenseNonCommercial, LicenseNoDerivatives, LicenseAttribution},
			Attribution:  "Prefect Pony XL by Goofy_Ai (Civitai)",
			MinRAMGB:     16, RecRAMGB: 32,
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
			Name: "flux1-dev", Arch: profile.ArchFlux, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "FLUX.1 [dev] Non-Commercial License: the model weights are for non-commercial use; generated outputs may be used commercially",
			LicenseFlags: []string{LicenseNonCommercial},
			// Not distilled like schnell: needs ~20 steps (sd.cpp's distilled_guidance
			// default of 3.5 is already the standard FLUX.1-dev guidance; CFG stays 1).
			Steps:    20,
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{
				DiffusionModel: "city96/FLUX.1-dev-gguf/flux1-dev-Q4_K_S.gguf",
				ClipL:          "comfyanonymous/flux_text_encoders/clip_l.safetensors",
				T5XXL:          "comfyanonymous/flux_text_encoders/t5xxl_fp8_e4m3fn.safetensors",
				VAE:            "camenduru/FLUX.1-dev/ae.safetensors", // ungated mirror (bfl repo is gated)
			},
			Notes: "FLUX.1 [dev] — higher-quality Flux than schnell (needs guidance, more steps, slower). Multi-component (GGUF diffusion + CLIP-L + T5 + VAE), ~12 GB; encoders shared with flux1-schnell. Non-commercial weights.",
		},
		{
			Name: "sd35-medium", Arch: profile.ArchSD35, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "Stability Community License: free incl. commercial under $1M annual revenue; attribution required; enterprise license above that",
			LicenseFlags: []string{LicenseAttribution},
			Attribution:  "Powered by Stability AI",
			MinRAMGB:     16, RecRAMGB: 32,
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
			Name: "sd35-large", Arch: profile.ArchSD35, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "Stability Community License: free incl. commercial under $1M annual revenue; attribution required; enterprise license above that",
			LicenseFlags: []string{LicenseAttribution},
			Attribution:  "Powered by Stability AI",
			MinRAMGB:     16, RecRAMGB: 32,
			Source: Source{
				DiffusionModel: "city96/stable-diffusion-3.5-large-gguf/sd3.5_large-Q4_0.gguf",
				ClipL:          "Comfy-Org/stable-diffusion-3.5-fp8/text_encoders/clip_l.safetensors",
				ClipG:          "Comfy-Org/stable-diffusion-3.5-fp8/text_encoders/clip_g.safetensors",
				T5XXL:          "Comfy-Org/stable-diffusion-3.5-fp8/text_encoders/t5xxl_fp8_e4m3fn.safetensors",
				VAE:            "adamo1139/stable-diffusion-3.5-large-turbo-ungated/vae/diffusion_pytorch_model.safetensors",
			},
			Notes: "SD3.5 Large — higher quality than Medium (larger diffusion, more compute). Multi-component (GGUF + CLIP-L/G + T5 + VAE); encoders/VAE shared with sd35-medium.",
		},
		{
			Name: "z-image-turbo", Arch: profile.ArchZImage, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "Apache-2.0 (Tongyi-MAI/Z-Image)",
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
			Rating: profile.RatingExplicit, License: "Fair AI Public License 1.0-SD (copyleft: model derivatives must keep the same license)",
			LicenseFlags: []string{LicenseShareAlike},
			MinRAMGB:     16, RecRAMGB: 32,
			Source: Source{
				HF:  "Laxhar/noobai-XL-Vpred-1.0/NoobAI-XL-Vpred-v1.0.safetensors",
				VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
			},
			ClipSkip: 2,
			Notes:    "v-prediction SDXL (verified; the profile sets prediction=v). NSFW-capable.",
		},
		{
			// Anima is its own architecture (sd.cpp VERSION_ANIMA), not an SDXL
			// derivative — see profile.ArchAnima. Like Z-Image it is multi-component:
			// the DiT checkpoint carries only `model.diffusion_model.*`, and sd.cpp's
			// AnimaConditioner wants a Qwen3 LLM under `text_encoders.llm` plus the
			// Qwen-Image VAE. The single-file Civitai download will NOT load.
			Name: "anima-turbo", Arch: profile.ArchAnima, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "NVIDIA Open Model License: commercial OK, attribution / notice retention required (see model card)",
			LicenseFlags: []string{LicenseAttribution},
			Attribution:  "Anima by CircleStone Labs / Comfy Org (NVIDIA Open Model License)",
			MinRAMGB:     8, RecRAMGB: 16,
			Source: Source{
				DiffusionModel: "circlestone-labs/Anima/split_files/diffusion_models/anima-turbo-v1.0.safetensors",
				LLM:            "circlestone-labs/Anima/split_files/text_encoders/qwen_3_06b_base.safetensors",
				VAE:            "circlestone-labs/Anima/split_files/vae/qwen_image_vae.safetensors",
			},
			Notes: "Anima turbo (CircleStone Labs x Comfy Org, 2B): anime / illustration focused, explicitly not for realism. Distilled — CFG 1 and 8-12 steps (the profile sets 10, sampler euler, no negative prompt). Multi-component: DiT + Qwen3-0.6B text encoder + Qwen-Image VAE. Base for Anima LoRAs.",
		},
		{
			Name: "anima-yume", Arch: profile.ArchAnima, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Civitai listing: images non-commercial (rent-only), derivatives allowed",
			LicenseFlags: []string{LicenseNonCommercial},
			MinRAMGB:     8, RecRAMGB: 16,
			// Unlike anima-turbo, the AnimaYume "base final" checkpoint is NOT
			// guidance-distilled: at the arch default (CFG 1, 10 steps) it renders
			// washed-out and incoherent. It needs real CFG and step counts, so
			// override the anima arch defaults here (verified E2E: CFG 5 / 24 steps
			// produces clean output, CFG 1 / 10 does not).
			Steps: 24, CFG: 5,
			Source: Source{
				DiffusionModel: "civitai:3065644", // https://civitai.com/models/2385278 (AnimaYume v1.0 base final)
				LLM:            "circlestone-labs/Anima/split_files/text_encoders/qwen_3_06b_base.safetensors",
				VAE:            "circlestone-labs/Anima/split_files/vae/qwen_image_vae.safetensors",
			},
			Notes: "Anima-based anime model (AnimaYume, Civitai). Multi-component: Civitai DiT + the shared Qwen3-0.6B encoder + Qwen-Image VAE. NOT distilled — the profile sets CFG 5 / 24 steps (overriding the anima arch turbo defaults). Needs CIVITAI_TOKEN.",
		},
		{
			Name: "nova-anime-am", Arch: profile.ArchAnima, Prediction: profile.PredEps,
			Rating: profile.RatingExplicit, License: "Civitai listing: commercial image use OK, derivatives OK, credit optional",
			MinRAMGB: 8, RecRAMGB: 16,
			// Not distilled — same as anima-yume, needs real CFG / steps (verified E2E).
			Steps: 24, CFG: 5,
			Source: Source{
				DiffusionModel: "civitai:3086321", // https://civitai.com/models/2604424 (Nova Anime AM v3.0)
				LLM:            "circlestone-labs/Anima/split_files/text_encoders/qwen_3_06b_base.safetensors",
				VAE:            "circlestone-labs/Anima/split_files/vae/qwen_image_vae.safetensors",
			},
			Notes: "Anima-based anime model (Nova Anime AM by Crody, Civitai). Multi-component: single-file Civitai DiT + the shared Qwen3-0.6B encoder + Qwen-Image VAE. Explicit-capable. Needs CIVITAI_TOKEN.",
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

		// ControlNet models (ADR-0006). Bound to a base architecture and loaded
		// alongside it — changing the ControlNet reloads the base model (it is in
		// the engine's reload key), unlike LoRAs which apply per render. Drive with
		// `gen --control-net <name> --control <image>`; add --canny to preprocess a
		// normal image into an edge map.
		{
			Name: "controlnet-canny-sd15", Kind: KindControlNet, Arch: profile.ArchSD15,
			Rating: profile.RatingSafe, License: "CreativeML OpenRAIL-M (lllyasviel/ControlNet-v1-1)",
			MinRAMGB: 8, RecRAMGB: 16,
			Source: Source{HF: "comfyanonymous/ControlNet-v1-1_fp16_safetensors/control_v11p_sd15_canny_fp16.safetensors"},
			Notes:  "Canny-edge ControlNet for SD1.5. `gen -m <sd15> --control-net controlnet-canny-sd15 --control edge.png` (add --canny to derive edges from a normal image).",
		},
		{
			Name: "controlnet-canny-sdxl", Kind: KindControlNet, Arch: profile.ArchSDXL,
			Rating: profile.RatingSafe, License: "Apache-2.0 (xinsir/controlnet-canny-sdxl-1.0)",
			MinRAMGB: 16, RecRAMGB: 32,
			// A diffusers-format ControlNet. sd.cpp converts its names on load and now
			// sizes the ControlNet graph for SDXL's deep transformers (upstream #1752),
			// so it loads directly — no pre-conversion needed.
			Source: Source{HF: "xinsir/controlnet-canny-sdxl-1.0/diffusion_pytorch_model.safetensors"},
			Notes:  "Canny-edge ControlNet for SDXL (xinsir). `gen -m <sdxl> --control-net controlnet-canny-sdxl --control edge.png` (add --canny to derive edges).",
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
		{
			Name: "sdxl-lightning-4step", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingSafe, License: "OpenRAIL++",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{HF: "ByteDance/SDXL-Lightning/sdxl_lightning_4step_lora.safetensors"},
			Notes:  "SDXL Lightning (ByteDance): 4-step sampling, generally sharper than LCM. Use `--steps 4 --cfg 1 --sampler euler`.",
		},
		{
			Name: "sdxl-lightning-8step", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingSafe, License: "OpenRAIL++",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{HF: "ByteDance/SDXL-Lightning/sdxl_lightning_8step_lora.safetensors"},
			Notes:  "SDXL Lightning (ByteDance): 8-step sampling, higher quality than the 4-step. Use `--steps 8 --cfg 1 --sampler euler`.",
		},
		{
			Name: "dmd2-sdxl-4step", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingSafe, License: "CC BY-NC 4.0 (non-commercial only)",
			LicenseFlags: []string{LicenseNonCommercial, LicenseAttribution},
			Attribution:  "DMD2 by Tianwei Yin et al. (CC BY-NC 4.0)",
			MinRAMGB:     16, RecRAMGB: 32,
			Source: Source{HF: "tianweiy/DMD2/dmd2_sdxl_4step_lora_fp16.safetensors"},
			Notes:  "DMD2 (Improved Distribution Matching Distillation): 4-step sampling. Use `--steps 4 --cfg 1 --sampler euler`. NOTE: CC BY-NC 4.0 — non-commercial use only.",
		},

		// Style / concept LoRAs from Civitai (SDXL-family bases: Illustrious, NoobAI).
		// Each needs its TriggerWords in the prompt or it does nothing. Ratings mirror
		// the Civitai listing's nsfwLevel; questionable/explicit require --allow-nsfw.
		{
			Name: "mythic-fantasy-illustrious", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingQuestionable, License: "Civitai listing: derivatives allowed, commercial image/rent/sell",
			MinRAMGB: 16, RecRAMGB: 32,
			Source:       Source{Civitai: "1373674"}, // https://civitai.com/models/599757 (illustrious)
			TriggerWords: []string{"mythp0rt"},
			Notes:        "Velvet's Mythic Fantasy style (Illustrious). Painterly fantasy portraits.",
		},
		{
			Name: "genba-neko-illustrious", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingSafe, License: "Civitai listing: NO derivatives, credit required, commercial rent-on-Civitai only",
			LicenseFlags: []string{LicenseNonCommercial, LicenseNoDerivatives, LicenseAttribution},
			Attribution:  "Genba Neko Like by HypnotistDolphin (Civitai)",
			MinRAMGB:     16, RecRAMGB: 32,
			Source:       Source{Civitai: "1619987"}, // https://civitai.com/models/1128981 (v2.0 IL)
			TriggerWords: []string{"genba_neko", "chibi", "pointing", "standing on one leg", ":3", "open mouth", "meme", "parody"},
			Notes:        "現場猫風 / Genba Neko meme style (Illustrious).",
		},
		{
			Name: "lighting-slider-illustrious", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingQuestionable, License: "Civitai listing: derivatives allowed, commercial image/rent",
			MinRAMGB: 16, RecRAMGB: 32,
			Source:       Source{Civitai: "1444863"}, // https://civitai.com/models/1280702 (Illustrious)
			TriggerWords: []string{"dark", "late night", "blue hour"},
			Notes:        "Lighting / darkness slider (Illustrious). Adjust the LoRA weight to move the exposure; on this base a positive weight brightened and a negative one darkened (measured mean luma: -1.0 => 22, no LoRA => 40, +1.0 => 108). The direction is base-dependent — the Anima version darkens at positive weight — so try both signs.",
		},
		{
			Name: "s1-dramatic-lighting-illustrious", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingQuestionable, License: "Civitai listing: derivatives allowed, commercial rent-on-Civitai only",
			LicenseFlags: []string{LicenseNonCommercial},
			MinRAMGB:     16, RecRAMGB: 32,
			Source:       Source{Civitai: "2200691"}, // https://civitai.com/models/661736 (Illustrious V1)
			TriggerWords: []string{"s1_dram"},
			Notes:        "S1 Dramatic Lighting (Illustrious V1). In practice it shifts the art style as much as the lighting. V2 exists but its listing is rated explicit; V1 is the same effect at a lower rating.",
		},
		{
			Name: "pov-on-couch-illustrious", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingExplicit, License: "Civitai listing: derivatives allowed, commercial image/rent-on-Civitai",
			MinRAMGB: 16, RecRAMGB: 32,
			Source:       Source{Civitai: "1361868"}, // https://civitai.com/models/1209145 (v1.0)
			TriggerWords: []string{"pov", "on couch"},
			Notes:        "POV on-couch pose LoRA (Illustrious). The trigger is plain English, so the base model already produces a couch scene; the LoRA mainly strengthens the low POV angle and hands. NSFW-capable.",
		},
		{
			Name: "ai-illust-ojisan-noobai", Kind: KindLoRA, Arch: profile.ArchSDXL,
			Rating: profile.RatingExplicit, License: "Civitai listing: derivatives allowed, commercial image/rent-on-Civitai",
			MinRAMGB: 16, RecRAMGB: 32,
			Source:       Source{Civitai: "2927805"}, // https://civitai.com/models/2564226 (v1.0 Chenkin)
			TriggerWords: []string{"@411llust0j1s4n,"},
			Notes:        "AIイラストおじさん / Uncle AI illustration style (NoobAI, an SDXL family base). NSFW-capable.",
		},

		// The same styles trained on the Anima base (Arch: anima). Use with the
		// anima-turbo model. Ratings mirror the Civitai listing's nsfwLevel.
		{
			Name: "mythic-fantasy-anima", Kind: KindLoRA, Arch: profile.ArchAnima,
			Rating: profile.RatingQuestionable, License: "Civitai listing: derivatives allowed, commercial image/rent/sell",
			MinRAMGB: 8, RecRAMGB: 16,
			Source:       Source{Civitai: "3084665"}, // https://civitai.com/models/599757 (Anima Portrait Style)
			TriggerWords: []string{"mythp0rt"},
			Notes:        "Velvet's Mythic Fantasy style (Anima). Painterly fantasy portraits.",
		},
		{
			Name: "genba-neko-anima", Kind: KindLoRA, Arch: profile.ArchAnima,
			Rating: profile.RatingSafe, License: "Civitai listing: NO derivatives, credit required, commercial rent-on-Civitai only",
			LicenseFlags: []string{LicenseNonCommercial, LicenseNoDerivatives, LicenseAttribution},
			Attribution:  "Genba Neko Like by HypnotistDolphin (Civitai)",
			MinRAMGB:     8, RecRAMGB: 16,
			Source:       Source{Civitai: "3029956"}, // https://civitai.com/models/1128981 (v1.0 Anima)
			TriggerWords: []string{"genba_neko", "chibi", "pointing", "standing on one leg", ":3", "open mouth", "meme", "parody"},
			Notes:        "現場猫風 / Genba Neko meme style (Anima).",
		},
		{
			Name: "lighting-slider-anima", Kind: KindLoRA, Arch: profile.ArchAnima,
			Rating: profile.RatingSafe, License: "Civitai listing: derivatives allowed, commercial image/rent",
			MinRAMGB: 8, RecRAMGB: 16,
			Source: Source{Civitai: "3078972"}, // https://civitai.com/models/1280702 (Anima)
			Notes:  "Lighting / darkness slider (Anima). A slider with no trigger word: adjust the LoRA weight to move the exposure (see lighting-slider-illustrious for the measured direction).",
		},
		{
			Name: "s1-dramatic-lighting-anima", Kind: KindLoRA, Arch: profile.ArchAnima,
			Rating: profile.RatingQuestionable, License: "Civitai listing: derivatives allowed, commercial rent-on-Civitai only",
			LicenseFlags: []string{LicenseNonCommercial},
			MinRAMGB:     8, RecRAMGB: 16,
			Source:       Source{Civitai: "3037397"}, // https://civitai.com/models/661736 (Anima v1.0)
			TriggerWords: []string{"s1_dram"},
			Notes:        "S1 Dramatic Lighting (Anima).",
		},
		{
			Name: "pov-on-couch-anima", Kind: KindLoRA, Arch: profile.ArchAnima,
			Rating: profile.RatingExplicit, License: "Civitai listing: derivatives allowed, commercial image/rent-on-Civitai",
			MinRAMGB: 8, RecRAMGB: 16,
			Source:       Source{Civitai: "3101268"}, // https://civitai.com/models/1209145 (v2.0 Anima)
			TriggerWords: []string{"pov", "on couch"},
			Notes:        "POV on-couch pose LoRA (Anima). NSFW-capable.",
		},
		{
			Name: "ai-illust-ojisan-anima", Kind: KindLoRA, Arch: profile.ArchAnima,
			Rating: profile.RatingExplicit, License: "Civitai listing: derivatives allowed, commercial image/rent-on-Civitai",
			MinRAMGB: 8, RecRAMGB: 16,
			Source:       Source{Civitai: "3038551"}, // https://civitai.com/models/2564226 (v1.0 AB1)
			TriggerWords: []string{"@411llust0j1s4n,"},
			Notes:        "AIイラストおじさん / Uncle AI illustration style (Anima). NSFW-capable.",
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
