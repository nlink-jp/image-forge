package cli

import (
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/store"
)

// licenceSources pins where every entry's terms were read from.
//
// The catalog's Source is where the *bytes* come from, and for most entries
// that is a quantization or mirror repo. A licence read from there — or, as
// happened with anima-turbo, from the base model the weights were trained on —
// can be a different licence from the one governing the weights. So the
// provenance is recorded per entry and pinned here: changing where a licence
// came from now has to be stated twice, and an entry added without a source
// fails the count check below.
//
// Verified against the publishers' own cards on 2026-09-21 (Hugging Face
// cardData.license / license_name, or the Civitai listing's permission block).
var licenceSources = map[string]string{
	"sd15-emaonly":                     "stable-diffusion-v1-5/stable-diffusion-v1-5",
	"animagine-xl-4":                   "cagliostrolab/animagine-xl-4.0",
	"illustrious-xl-v1":                "civitai:1096723 listing + OnomaAIResearch/Illustrious-XL-v1.0 card",
	"illustrious-xl-v1.1":              "civitai:1411690 listing",
	"akium-unmotivated":                "civitai:3046291 listing",
	"akium-ijin":                       "civitai:3081528 listing",
	"akium-lumen":                      "civitai:2962026 listing",
	"t-ponynai3-v7":                    "civitai:1392706 listing",
	"t-ponynai3-v5.5":                  "civitai:593760 listing",
	"momoiro-pony":                     "civitai:425904 listing",
	"prefect-pony-xl":                  "civitai:2114187 listing",
	"realvisxl-v5":                     "SG161222/RealVisXL_V5.0",
	"juggernaut-xl-v9":                 "RunDiffusion/Juggernaut-XL-v9",
	"flux1-schnell":                    "black-forest-labs/FLUX.1-schnell",
	"flux1-dev":                        "black-forest-labs/FLUX.1-dev",
	"sd35-medium":                      "stabilityai/stable-diffusion-3.5-medium",
	"sd35-large":                       "stabilityai/stable-diffusion-3.5-large",
	"z-image-turbo":                    "Tongyi-MAI/Z-Image",
	"noobai-xl-vpred":                  "Laxhar/noobai-XL-Vpred-1.0",
	"anima-turbo":                      "circlestone-labs/Anima LICENSE.md",
	"realesrgan-x4plus":                "github.com/xinntao/Real-ESRGAN",
	"realesrgan-x4-anime":              "github.com/xinntao/Real-ESRGAN",
	"controlnet-canny-sd15":            "lllyasviel/ControlNet-v1-1",
	"controlnet-canny-sdxl":            "xinsir/controlnet-canny-sdxl-1.0",
	"lcm-lora-sdxl":                    "latent-consistency/lcm-lora-sdxl",
	"lcm-lora-sd15":                    "latent-consistency/lcm-lora-sdv1-5",
	"sdxl-lightning-4step":             "ByteDance/SDXL-Lightning",
	"sdxl-lightning-8step":             "ByteDance/SDXL-Lightning",
	"dmd2-sdxl-4step":                  "tianweiy/DMD2",
	"mythic-fantasy-illustrious":       "civitai:1373674 listing",
	"genba-neko-illustrious":           "civitai:1619987 listing",
	"lighting-slider-illustrious":      "civitai:1444863 listing",
	"s1-dramatic-lighting-illustrious": "civitai:2200691 listing",
	"pov-on-couch-illustrious":         "civitai:1361868 listing",
	"ai-illust-ojisan-noobai":          "civitai:2927805 listing",
	"mythic-fantasy-anima":             "civitai:3084665 listing",
	"genba-neko-anima":                 "civitai:3029956 listing",
	"lighting-slider-anima":            "civitai:3078972 listing",
	"s1-dramatic-lighting-anima":       "civitai:3037397 listing",
	"pov-on-couch-anima":               "civitai:3101268 listing",
	"ai-illust-ojisan-anima":           "civitai:3038551 listing",
	// The two entries that pair a Civitai DiT with the Qwen components re-hosted
	// in the Anima repo: those files are Apache-2.0 by Qwen, not CircleStone's
	// weights, so their terms are not the DiT's and not CircleStone's either.
	"anima-yume":    "civitai:3065644 listing; the shared encoder/VAE are Apache-2.0 (Qwen/Qwen3-0.6B-Base, Qwen/Qwen-Image), re-hosted in circlestone-labs/Anima",
	"nova-anime-am": "civitai:3086321 listing; the shared encoder/VAE are Apache-2.0 (Qwen/Qwen3-0.6B-Base, Qwen/Qwen-Image), re-hosted in circlestone-labs/Anima",
}

func TestEveryEntryRecordsWhereItsLicenceWasRead(t *testing.T) {
	entries := catalog.Default()
	if len(entries) == 0 {
		t.Fatal("empty catalog")
	}
	if len(entries) != len(licenceSources) {
		t.Errorf("catalog has %d entries, %d licence sources pinned — a new entry must state where its terms were read from",
			len(entries), len(licenceSources))
	}
	for _, e := range entries {
		if strings.TrimSpace(e.LicenseSource) == "" {
			t.Errorf("%s: no LicenseSource — record the card or listing the licence was read from", e.Name)
			continue
		}
		want, ok := licenceSources[e.Name]
		if !ok {
			t.Errorf("%s: not pinned in licenceSources", e.Name)
			continue
		}
		if e.LicenseSource != want {
			t.Errorf("%s: LicenseSource = %q, pinned as %q", e.Name, e.LicenseSource, want)
		}
	}
}

// anima-turbo is the entry the class was found on, so it gets its own
// assertion: the weights are CircleStone's, and the NVIDIA Open Model License
// of the base model must not be what the tool reports.
func TestAnimaTurboReportsTheWeightsLicence(t *testing.T) {
	var e catalog.Entry
	for _, x := range catalog.Default() {
		if x.Name == "anima-turbo" {
			e = x
		}
	}
	if e.Name == "" {
		t.Fatal("anima-turbo not in catalog")
	}
	if !strings.Contains(e.License, "CircleStone") {
		t.Errorf("License does not name the publisher of the weights: %q", e.License)
	}
	if strings.Contains(e.License, "NVIDIA") {
		t.Errorf("License still reports the base model's terms: %q", e.License)
	}
	if !contains(e.LicenseFlags, catalog.LicenseNonCommercial) {
		t.Errorf("non-commercial weights must carry the flag, got %v", e.LicenseFlags)
	}
}

// The provenance has to reach the caller, or it only informs whoever reads the
// source. Both views carry it.
func TestLicenceSourceReachesTheViews(t *testing.T) {
	reg := &store.Registry{Models: map[string]store.InstalledModel{}}
	var seen bool
	for _, v := range catalogViews(reg) {
		if v.Name == "anima-turbo" {
			seen = true
			if v.LicenseSource != "circlestone-labs/Anima LICENSE.md" {
				t.Errorf("catalog view license_source = %q", v.LicenseSource)
			}
		}
	}
	if !seen {
		t.Fatal("anima-turbo missing from the catalog view")
	}
}

// A licence correction has to reach a model that is already installed. The
// registry records the licence as it was at install time, so for a cataloged
// model the catalog — not the recording — is what gets reported.
func TestInstalledModelReportsTheCorrectedLicence(t *testing.T) {
	var entry catalog.Entry
	for _, e := range catalog.Default() {
		if e.Name == "anima-turbo" {
			entry = e
		}
	}
	if entry.Name == "" {
		t.Fatal("anima-turbo not in catalog")
	}
	stale := "NVIDIA Open Model License: commercial OK, attribution / notice retention required (see model card)"
	reg := &store.Registry{Models: map[string]store.InstalledModel{
		"anima-turbo": {
			Name:    "anima-turbo",
			Path:    "/m/anima-turbo.safetensors",
			License: stale, // what an install before the correction recorded
		},
	}}
	got := installedViews(reg)[0]
	if got.License == stale {
		t.Errorf("an installed model kept reporting the licence it was installed with: %q", got.License)
	}
	if got.License != entry.License {
		t.Errorf("installed licence = %q, want the catalog's %q", got.License, entry.License)
	}
	if got.LicenseSource != entry.LicenseSource {
		t.Errorf("installed license_source = %q, want %q", got.LicenseSource, entry.LicenseSource)
	}
}
