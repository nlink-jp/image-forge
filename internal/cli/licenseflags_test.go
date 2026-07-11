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
	}
	for _, e := range catalog.Default() {
		for _, f := range e.LicenseFlags {
			if !known[f] {
				t.Errorf("%s has unknown license flag %q", e.Name, f)
			}
		}
		// A flagged entry must not claim to be permissive in its text, and vice
		// versa — a cheap guard against the flags and the human License drifting.
		if contains(e.LicenseFlags, catalog.LicenseNonCommercial) &&
			e.License != "" && !mentionsNonCommercial(e.License) {
			t.Errorf("%s flagged non-commercial but License text doesn't reflect it: %q", e.Name, e.License)
		}
	}
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
	for _, needle := range []string{"NC", "non-commercial", "rent-on-Civitai only", "rent on Civitai only"} {
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
