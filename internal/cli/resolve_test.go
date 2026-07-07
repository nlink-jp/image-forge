package cli

import (
	"testing"

	"github.com/nlink-jp/image-forge/internal/profile"
)

func f64(v float64) *float64 { return &v }
func strp(v string) *string  { return &v }

func TestResolveHires_Mode(t *testing.T) {
	on := profile.Profile{HiresEnabled: true}
	off := profile.Profile{HiresEnabled: false}
	cases := []struct {
		name        string
		mode        string
		prof        profile.Profile
		wantEnabled bool
	}{
		{"auto follows profile (on)", "auto", on, true},
		{"auto follows profile (off)", "auto", off, false},
		{"empty mode == auto (on)", "", on, true},
		{"on forces enabled over profile off", "on", off, true},
		{"off forces disabled over profile on", "off", on, false},
	}
	for _, c := range cases {
		if got := resolveHires(c.mode, c.prof, hiresOverrides{}, hiresEnv{}); got.Enabled != c.wantEnabled {
			t.Errorf("%s: enabled = %v, want %v", c.name, got.Enabled, c.wantEnabled)
		}
	}
}

func TestResolveHires_OpinionatedDefaults(t *testing.T) {
	// Enabled with an empty profile/env => image-forge's conservative defaults
	// (latent / 1.5 / 0.5), not sd.cpp's heavier 2.0 / 0.7.
	got := resolveHires("on", profile.Profile{}, hiresOverrides{}, hiresEnv{})
	if !got.Enabled {
		t.Fatal("mode on should enable hires")
	}
	if profile.DefaultHiresUpscaler != "latent" || profile.DefaultHiresScale != 1.5 || profile.DefaultHiresDenoise != 0.5 {
		t.Fatalf("opinionated defaults drifted: upscaler=%q scale=%v denoise=%v",
			profile.DefaultHiresUpscaler, profile.DefaultHiresScale, profile.DefaultHiresDenoise)
	}
	if got.Upscaler != "latent" || got.Scale != 1.5 || got.Denoise != 0.5 || got.Steps != 0 || got.Model != "" {
		t.Errorf("defaults not applied: %+v", got)
	}
}

func TestResolveHires_ProfileValuesUsed(t *testing.T) {
	// scale/denoise/steps come from the profile; a profile "model" upscaler
	// resolves to a concrete model when one is installed.
	prof := profile.Profile{
		HiresEnabled: true, HiresScale: 2.0, HiresDenoise: 0.65,
		HiresUpscaler: "model", HiresSteps: 12,
	}
	env := hiresEnv{Installed: map[string]string{"esr": "/x/e.pth"}}
	got := resolveHires("auto", prof, hiresOverrides{}, env)
	if got.Scale != 2.0 || got.Denoise != 0.65 || got.Steps != 12 {
		t.Errorf("profile scale/denoise/steps not used: %+v", got)
	}
	if got.Upscaler != "model" || got.Model != "/x/e.pth" {
		t.Errorf("profile model upscaler not resolved to the sole installed model: %+v", got)
	}
}

func TestResolveHires_OverridesWin(t *testing.T) {
	prof := profile.Profile{
		HiresEnabled: true, HiresScale: 2.0, HiresDenoise: 0.65, HiresUpscaler: "latent",
	}
	ov := hiresOverrides{
		Scale: f64(1.25), Denoise: f64(0.4), Upscaler: strp("model"), Model: "/x/esrgan.pth",
	}
	got := resolveHires("auto", prof, ov, hiresEnv{})
	// CLI --hires-upscaler model + --hires-model path win over the profile's latent.
	if got.Scale != 1.25 || got.Denoise != 0.4 || got.Upscaler != "model" || got.Model != "/x/esrgan.pth" {
		t.Errorf("overrides did not win: %+v", got)
	}
}

func TestResolveHires_DisabledReturnsZero(t *testing.T) {
	got := resolveHires("off", profile.Profile{HiresEnabled: true, HiresScale: 2}, hiresOverrides{Scale: f64(3)}, hiresEnv{})
	if got.Enabled || got.Scale != 0 || got.Upscaler != "" || got.Model != "" {
		t.Errorf("disabled hires should be zero-valued: %+v", got)
	}
}

func TestResolveHires_ConfigUpscalerAndAuto(t *testing.T) {
	prof := profile.Profile{HiresEnabled: true} // no profile upscaler
	oneInstalled := map[string]string{"realesrgan-x4-anime": "/m/anime.pth"}

	// config "auto" + an installed ESRGAN => use it.
	got := resolveHires("on", prof, hiresOverrides{}, hiresEnv{ConfigUpscaler: "auto", Installed: oneInstalled})
	if got.Upscaler != "model" || got.Model != "/m/anime.pth" {
		t.Errorf("config auto with an installed ESRGAN should pick it: %+v", got)
	}
	// config "auto" + none installed => latent.
	got = resolveHires("on", prof, hiresOverrides{}, hiresEnv{ConfigUpscaler: "auto"})
	if got.Upscaler != "latent" || got.Model != "" {
		t.Errorf("config auto with no ESRGAN should fall back to latent: %+v", got)
	}
	// config forcing latent even though an ESRGAN is installed.
	got = resolveHires("on", prof, hiresOverrides{}, hiresEnv{ConfigUpscaler: "latent", Installed: oneInstalled})
	if got.Upscaler != "latent" || got.Model != "" {
		t.Errorf("config latent should stay built-in: %+v", got)
	}
	// profile beats config: profile latent wins over config auto.
	got = resolveHires("on", profile.Profile{HiresEnabled: true, HiresUpscaler: "latent"},
		hiresOverrides{}, hiresEnv{ConfigUpscaler: "auto", Installed: oneInstalled})
	if got.Upscaler != "latent" {
		t.Errorf("profile upscaler should beat config: %+v", got)
	}
}

func TestResolveHiresUpscalerSpec(t *testing.T) {
	two := map[string]string{"a": "/a.pth", "b": "/b.pth"}
	one := map[string]string{"a": "/a.pth"}
	cases := []struct {
		name, spec, cliModel, defModel string
		installed                      map[string]string
		wantMode, wantPath             string
	}{
		{"latent", "latent", "", "", two, "latent", ""},
		{"lanczos", "lanczos", "", "", two, "lanczos", ""},
		{"empty->latent", "", "", "", two, "latent", ""},
		{"auto none->latent", "auto", "", "", nil, "latent", ""},
		{"auto sole->model", "auto", "", "", one, "model", "/a.pth"},
		{"auto default_model", "auto", "", "b", two, "model", "/b.pth"},
		{"auto ambiguous->latent", "auto", "", "", two, "latent", ""},
		{"model cli path wins", "model", "/cli.pth", "b", two, "model", "/cli.pth"},
		{"model no candidate->latent", "model", "", "", two, "latent", ""},
		{"name resolves", "a", "", "", two, "model", "/a.pth"},
		{"unknown name->latent", "zzz", "", "", two, "latent", ""},
	}
	for _, c := range cases {
		mode, path := resolveHiresUpscalerSpec(c.spec, c.cliModel, c.defModel, c.installed)
		if mode != c.wantMode || path != c.wantPath {
			t.Errorf("%s: got (%q,%q), want (%q,%q)", c.name, mode, path, c.wantMode, c.wantPath)
		}
	}
}
