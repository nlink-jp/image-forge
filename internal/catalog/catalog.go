// Package catalog is the model catalog: curated, binary-embedded entries plus
// (later) user-extensible ones. Each entry carries the metadata needed to
// surface license and content rating and to build a profile on pull/import.
package catalog

import "github.com/nlink-jp/image-forge/internal/profile"

// Source identifies where a model (and its dedicated VAE) is fetched from.
type Source struct {
	HF      string // "org/repo" or "org/repo/file.safetensors"
	Civitai string // model or version id
	URL     string // direct URL
	VAE     string // dedicated VAE source (e.g. madebyollin/sdxl-vae-fp16-fix)
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

// Default returns the curated, binary-embedded catalog.
//
// NOTE: Source repo ids below are provisional (RFP stage). Each must be verified
// against the actual HF/Civitai listing before Phase 1 `pull` support ships.
func Default() []Entry {
	return []Entry{
		{
			Name: "animagine-xl-4", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "Fair AI Public License 1.0-SD",
			MinRAMGB: 16, RecRAMGB: 32,
			Source:   Source{HF: "cagliostrolab/animagine-xl-4.0", VAE: "madebyollin/sdxl-vae-fp16-fix"},
			ClipSkip: 2, Notes: "Anime SDXL, retrained from SDXL 1.0.",
		},
		{
			Name: "illustrious-xl-v1", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
			Rating: profile.RatingQuestionable, License: "OnomaAI Illustrious license (verify)",
			MinRAMGB: 16, RecRAMGB: 32,
			Source:   Source{HF: "OnomaAIResearch/Illustrious-XL-v1.0", VAE: "madebyollin/sdxl-vae-fp16-fix"},
			ClipSkip: 2, Notes: "Anime SDXL base with a large LoRA ecosystem.",
		},
		{
			Name: "flux1-schnell", Arch: profile.ArchFlux, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "Apache-2.0",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{HF: "black-forest-labs/FLUX.1-schnell"},
			Notes:  "General high quality, fast, output is yours. Q4 quant on 16GB.",
		},
		{
			Name: "z-image-turbo", Arch: profile.ArchZImage, Prediction: profile.PredEps,
			Rating: profile.RatingSafe, License: "permissive (verify)",
			MinRAMGB: 16, RecRAMGB: 32,
			Source: Source{HF: "Tongyi-MAI/Z-Image-Turbo"},
			Notes:  "Efficient, fast general model.",
		},
		{
			Name: "noobai-xl-vpred", Arch: profile.ArchSDXL, Prediction: profile.PredVPred,
			Rating: profile.RatingExplicit, License: "Fair AI Public License 1.0-SD",
			MinRAMGB: 16, RecRAMGB: 32,
			Source:   Source{HF: "Laxhar/noobai-XL-Vpred-1.0", VAE: "madebyollin/sdxl-vae-fp16-fix"},
			ClipSkip: 2, Experimental: true,
			Notes: "v-prediction; requires sd.cpp v-pred/ZSNR support (verify before enabling).",
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
