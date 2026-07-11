package cli

import (
	"testing"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

// License flags are the machine-readable restrictions a front-end highlights.
// They must reach both `models list --json` views.
func TestLicenseFlagsSurfacedInViews(t *testing.T) {
	// Catalog view: a known-restricted entry carries its flags.
	var seen bool
	for _, v := range catalogViews(&store.Registry{Models: map[string]store.InstalledModel{}}) {
		if v.Name == "dmd2-sdxl-4step" {
			seen = true
			if !contains(v.LicenseFlags, catalog.LicenseNonCommercial) {
				t.Errorf("dmd2-sdxl-4step should be flagged non-commercial, got %v", v.LicenseFlags)
			}
		}
	}
	if !seen {
		t.Fatal("dmd2-sdxl-4step not in catalog")
	}

	// Installed view: flags recorded on the registry entry are reported.
	reg := &store.Registry{Models: map[string]store.InstalledModel{
		"nc-lora": {
			Name: "nc-lora", Kind: catalog.KindLoRA, Path: "/m/x.safetensors",
			Profile:      profile.Profile{Name: "nc-lora", Arch: profile.ArchSDXL},
			LicenseFlags: []string{catalog.LicenseNonCommercial, catalog.LicenseAttribution},
		},
	}}
	got := installedViews(reg)[0].LicenseFlags
	if len(got) != 2 || got[0] != "non-commercial" || got[1] != "attribution" {
		t.Errorf("installed license flags = %v", got)
	}
}

// Every flag in the catalog must be one of the four known identifiers — a typo'd
// flag would never highlight and never error otherwise.
func TestLicenseFlagsAreKnownAndConsistentWithText(t *testing.T) {
	known := map[string]bool{
		catalog.LicenseNonCommercial: true, catalog.LicenseNoDerivatives: true,
		catalog.LicenseAttribution: true, catalog.LicenseShareAlike: true,
		catalog.LicenseReview: true,
	}
	for _, e := range catalog.Default() {
		for _, f := range e.LicenseFlags {
			if !known[f] {
				t.Errorf("%s has unknown license flag %q", e.Name, f)
			}
		}
		// A non-commercial flag must be reflected in the License text — a cheap
		// guard against the machine flags and the human License string drifting.
		if contains(e.LicenseFlags, catalog.LicenseNonCommercial) &&
			e.License != "" && !mentionsNonCommercial(e.License) {
			t.Errorf("%s flagged non-commercial but License text doesn't reflect it: %q", e.Name, e.License)
		}
	}
}

// Attribution flag and Attribution text must move together: a model that
// requires credit needs the text to give, and text is pointless without the flag
// that tells a front-end to surface it.
func TestAttributionFlagMatchesText(t *testing.T) {
	for _, e := range catalog.Default() {
		flagged := contains(e.LicenseFlags, catalog.LicenseAttribution)
		hasText := e.Attribution != ""
		if flagged && !hasText {
			t.Errorf("%s is flagged attribution but has no Attribution text", e.Name)
		}
		if hasText && !flagged {
			t.Errorf("%s has Attribution text %q but is not flagged attribution", e.Name, e.Attribution)
		}
	}
}

// A spot-check of specific catalog entries so a wrong or dropped flag is caught,
// not just an unknown-identifier typo. Base-model coverage (this feature's point).
func TestBaseModelLicenseFlags(t *testing.T) {
	want := map[string][]string{
		"prefect-pony-xl":     {catalog.LicenseNonCommercial, catalog.LicenseNoDerivatives, catalog.LicenseAttribution},
		"momoiro-pony":        {catalog.LicenseNonCommercial, catalog.LicenseAttribution},
		"akium-unmotivated":   {catalog.LicenseNonCommercial},
		"illustrious-xl-v1":   {catalog.LicenseNoDerivatives, catalog.LicenseAttribution},
		"illustrious-xl-v1.1": {catalog.LicenseNoDerivatives, catalog.LicenseAttribution},
		"t-ponynai3-v7":       {catalog.LicenseNoDerivatives},
		"noobai-xl-vpred":     {catalog.LicenseShareAlike},
		"sd35-medium":         {catalog.LicenseAttribution},
		"anima-turbo":         {catalog.LicenseAttribution},
		// Permissive base models carry no flags.
		"z-image-turbo":    nil, // Apache-2.0 (Tongyi-MAI/Z-Image)
		"flux1-schnell":    nil,
		"realvisxl-v5":     nil,
		"juggernaut-xl-v9": nil,
		"animagine-xl-4":   nil,
		"sd15-emaonly":     nil,
	}
	byName := map[string]catalog.Entry{}
	for _, e := range catalog.Default() {
		byName[e.Name] = e
	}
	for name, exp := range want {
		e, ok := byName[name]
		if !ok {
			t.Errorf("%s not in catalog", name)
			continue
		}
		if !equalStrings(e.LicenseFlags, exp) {
			t.Errorf("%s license flags = %v, want %v", name, e.LicenseFlags, exp)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func mentionsNonCommercial(s string) bool {
	for _, needle := range []string{"NC", "non-commercial", "no commercial", "rent-on-Civitai only", "rent on Civitai only", "rent-only"} {
		if containsFold(s, needle) {
			return true
		}
	}
	return false
}

func containsFold(s, sub string) bool {
	// tiny case-insensitive contains without importing strings twice
	ls, lsub := []rune(s), []rune(sub)
	if len(lsub) == 0 {
		return true
	}
	for i := 0; i+len(lsub) <= len(ls); i++ {
		ok := true
		for j := range lsub {
			a, b := ls[i+j], lsub[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
