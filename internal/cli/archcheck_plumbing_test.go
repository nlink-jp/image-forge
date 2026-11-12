package cli

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/catalog"
	"github.com/nlink-jp/image-forge/internal/engine"
	"github.com/nlink-jp/image-forge/internal/profile"
	"github.com/nlink-jp/image-forge/internal/store"
)

// archRegistry installs an SD1.5 base and an SDXL LoRA, both with --arch, into
// a throwaway registry, with real (empty) weight files so resolution gets as
// far as the check. The config points at an empty file so nothing of the
// operator's is read.
func archRegistry(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("IMAGE_FORGE_HOME", home)
	cfg := filepath.Join(home, "config.toml")
	if err := os.WriteFile(cfg, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_FORGE_CONFIG", cfg)
	weights := func(name string) string {
		p := filepath.Join(home, name)
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	reg.Add(store.InstalledModel{Name: "base-sd15", Path: weights("base.safetensors"),
		Profile: profile.ArchDefaults(profile.ArchSD15), ArchSource: store.ArchFromFlag})
	reg.Add(store.InstalledModel{Name: "lora-sdxl", Kind: catalog.KindLoRA, Path: weights("lora.safetensors"),
		Profile: profile.Profile{Name: "lora-sdxl", Arch: profile.ArchSDXL}, ArchSource: store.ArchFromFlag})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
}

// The check is wired into both ways a render is built: buildRender (the
// resident serve loop and the MCP worker) and gen. Removing the arch from
// resolveModel, or passing none at either call site, fails here — the review
// found all three could be switched off with the unit tests still green.
func TestEveryRenderPathRefusesAnArchMismatch(t *testing.T) {
	archRegistry(t)

	_, _, _, _, err := buildRender(RenderRequest{Model: "base-sd15", LoRAs: []string{"lora-sdxl:1"}})
	if err == nil || !strings.Contains(err.Error(), "is for sdxl and the model is sd15") {
		t.Errorf("buildRender: err = %v, want the architecture mismatch", err)
	}

	err = runGen([]string{"-p", "x", "-m", "base-sd15", "--lora", "lora-sdxl:1", "-o", filepath.Join(t.TempDir(), "o.png")})
	if err == nil || !strings.Contains(err.Error(), "is for sdxl and the model is sd15") {
		t.Errorf("gen: err = %v, want the architecture mismatch before anything loads", err)
	}
}

// The write side: what a registration records. archFor is the one decision
// import and a raw-ref pull share; derivedArchSource carries trust through
// quantize. A guess recorded as a fact would bring back the dreamshaper_8
// refusal, so each branch is pinned.
func TestArchForRecordsAGuessAsAGuess(t *testing.T) {
	a, src, err := archFor("dreamshaper_8", "")
	if err != nil || a != profile.ArchSDXL || src != store.ArchDetected {
		t.Errorf("no --arch: %q %q %v, want sdxl detected", a, src, err)
	}
	a, src, err = archFor("dreamshaper_8", "SD15")
	if err != nil || a != profile.ArchSD15 || src != store.ArchFromFlag {
		t.Errorf("--arch SD15: %q %q %v, want sd15 flag", a, src, err)
	}
	if _, _, err := archFor("x", "pony"); err == nil {
		t.Error("--arch pony was accepted")
	}
}

func TestDerivedArchSourceKeepsTheSourcesTrust(t *testing.T) {
	cases := []struct {
		name string
		src  store.InstalledModel
		want string
	}{
		{"flag", store.InstalledModel{Name: "a", Profile: profile.Profile{Arch: profile.ArchSD15}, ArchSource: store.ArchFromFlag}, store.ArchFromFlag},
		{"detected", store.InstalledModel{Name: "a", Profile: profile.Profile{Arch: profile.ArchSDXL}, ArchSource: store.ArchDetected}, store.ArchDetected},
		{"old catalog install", store.InstalledModel{Name: "lcm-lora-sd15", Profile: profile.Profile{Arch: profile.ArchSD15}}, store.ArchFromCatalog},
		{"old, not the catalog's", store.InstalledModel{Name: "dreamshaper_8", Profile: profile.Profile{Arch: profile.ArchSDXL}}, ""},
	}
	for _, tc := range cases {
		if got := derivedArchSource(tc.src); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// modelsImport end to end against a throwaway registry: what is written for
// no --arch, a valid one, and an invalid one.
func TestModelsImportRecordsWhereTheArchCameFrom(t *testing.T) {
	archRegistry(t)
	file := filepath.Join(t.TempDir(), "dreamshaper_8.safetensors")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := modelsImport([]string{file}); err != nil {
		t.Fatal(err)
	}
	if err := modelsImport([]string{file, "--name", "ds8-sd15", "--arch", "SD15"}); err != nil {
		t.Fatal(err)
	}
	if err := modelsImport([]string{file, "--name", "ds8-pony", "--arch", "pony"}); err == nil {
		t.Error("--arch pony was imported")
	}
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if im, _ := reg.Get("dreamshaper_8"); im.ArchSource != store.ArchDetected || trustedArch(im) != "" {
		t.Errorf("no --arch: source %q trusted %q; want a guess that is not trusted", im.ArchSource, trustedArch(im))
	}
	if im, _ := reg.Get("ds8-sd15"); im.ArchSource != store.ArchFromFlag || trustedArch(im) != profile.ArchSD15 {
		t.Errorf("--arch SD15: source %q trusted %q; want sd15 from the flag", im.ArchSource, trustedArch(im))
	}
	if _, ok := reg.Get("ds8-pony"); ok {
		t.Error("an invalid --arch left a registry entry")
	}
}

// models list --json says which arches are facts, so a front-end can filter
// LoRAs / ControlNets by the same rule the render path applies.
func TestModelsListReportsWhetherAnArchIsTrusted(t *testing.T) {
	reg := &store.Registry{Models: map[string]store.InstalledModel{
		"flagged": {Name: "flagged", Path: "/m/a", Profile: profile.Profile{Arch: profile.ArchSD15}, ArchSource: store.ArchFromFlag},
		"guessed": {Name: "guessed", Path: "/m/b", Profile: profile.Profile{Arch: profile.ArchSDXL}, ArchSource: store.ArchDetected},
	}}
	got := map[string]bool{}
	for _, v := range installedViews(reg) {
		got[v.Name] = v.ArchTrusted
	}
	if !got["flagged"] || got["guessed"] {
		t.Errorf("arch_trusted = %v, want flagged true and guessed false", got)
	}
}

// The size limit is wired into both ways a render is built, for the base size
// and for the hires.fix target, and only for an architecture that is a fact.
func TestEveryRenderPathRefusesASizeTheArchRendersBroken(t *testing.T) {
	archRegistry(t)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "w.gguf")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	reg.Add(store.InstalledModel{Name: "qwen-fact", Path: file,
		Profile: profile.ArchDefaults(profile.ArchQwenImage21), ArchSource: store.ArchFromFlag})
	reg.Add(store.InstalledModel{Name: "qwen-guess", Path: file,
		Profile: profile.ArchDefaults(profile.ArchQwenImage21), ArchSource: store.ArchDetected})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	big, scale := 1536, 1.5
	off := "off"

	_, _, _, _, err = buildRender(RenderRequest{Model: "qwen-fact", Width: &big, Height: &big, Hires: off})
	if err == nil || !strings.Contains(err.Error(), "1536x1536 is too large for qwenimage21") {
		t.Errorf("buildRender: err = %v, want the size refusal", err)
	}
	_, _, _, _, err = buildRender(RenderRequest{Model: "qwen-fact", Hires: "on", HiresScale: &scale})
	if err == nil || !strings.Contains(err.Error(), "hires.fix would render 1536x1536") {
		t.Errorf("buildRender hires: err = %v, want the hires refusal", err)
	}
	if _, _, _, _, err = buildRender(RenderRequest{Model: "qwen-fact", Hires: off}); err != nil {
		t.Errorf("buildRender at the default size: %v", err)
	}
	if _, _, _, _, err = buildRender(RenderRequest{Model: "qwen-guess", Width: &big, Height: &big, Hires: off}); err != nil {
		t.Errorf("a guessed arch was refused: %v", err)
	}

	out := filepath.Join(t.TempDir(), "o.png")
	err = runGen([]string{"-p", "x", "-m", "qwen-fact", "-W", "1536", "-H", "1536", "-hires", "off", "-o", out})
	if err == nil || !strings.Contains(err.Error(), "1536x1536 is too large for qwenimage21") {
		t.Errorf("gen: err = %v, want the size refusal before anything loads", err)
	}
	err = runGen([]string{"-p", "x", "-m", "qwen-fact", "-hires", "on", "-hires-scale", "1.5", "-o", out})
	if err == nil || !strings.Contains(err.Error(), "hires.fix would render 1536x1536") {
		t.Errorf("gen hires: err = %v, want the hires refusal", err)
	}
}

// The engine sizes the output to the init image, then to the control image;
// the requested size counts only when neither is given. The check must judge
// the same size, or a large init image renders the broken case unrefused.
func TestRenderSizeFollowsTheEnginesOrder(t *testing.T) {
	sizes := map[string][2]int{"init.png": {2048, 2048}, "ctrl.png": {768, 512}}
	sizeOf := func(p string) (int, int, bool) { s, ok := sizes[p]; return s[0], s[1], ok }
	cases := []struct {
		name string
		req  engine.Request
		w, h int
	}{
		{"neither", engine.Request{Width: 1024, Height: 1024}, 1024, 1024},
		{"init", engine.Request{Width: 1024, Height: 1024, InitImage: "init.png"}, 2048, 2048},
		{"control wins over init", engine.Request{Width: 1024, Height: 1024, InitImage: "init.png", ControlImage: "ctrl.png"}, 768, 512},
		{"unreadable image is the engine's to report", engine.Request{Width: 1024, Height: 1024, InitImage: "gone.png"}, 1024, 1024},
	}
	for _, c := range cases {
		if w, h := renderSize(c.req, sizeOf); w != c.w || h != c.h {
			t.Errorf("%s: %dx%d, want %dx%d", c.name, w, h, c.w, c.h)
		}
	}
}

// End to end: a 1536x1536 init image at the default requested size is refused
// on both paths, read from a real PNG header.
func TestEveryRenderPathRefusesAnInitImageTheArchRendersBroken(t *testing.T) {
	archRegistry(t)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	weights := filepath.Join(dir, "w.gguf")
	if err := os.WriteFile(weights, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	reg.Add(store.InstalledModel{Name: "qwen-fact", Path: weights,
		Profile: profile.ArchDefaults(profile.ArchQwenImage21), ArchSource: store.ArchFromFlag})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewGray(image.Rect(0, 0, 1536, 1536))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if w, h, ok := imageSize(big); !ok || w != 1536 || h != 1536 {
		t.Fatalf("imageSize = %dx%d %v", w, h, ok)
	}

	_, _, _, _, err = buildRender(RenderRequest{Model: "qwen-fact", Init: big, Hires: "off"})
	if err == nil || !strings.Contains(err.Error(), "1536x1536 is too large for qwenimage21") {
		t.Errorf("buildRender: err = %v, want the size refusal", err)
	}
	err = runGen([]string{"-p", "x", "-m", "qwen-fact", "-init", big, "-hires", "off", "-o", filepath.Join(dir, "o.png")})
	if err == nil || !strings.Contains(err.Error(), "1536x1536 is too large for qwenimage21") {
		t.Errorf("gen: err = %v, want the size refusal", err)
	}
}

// A hires scale that is not positive is not "no hires": the engine drops it
// and sd.cpp renders at its own default of 2.0, which is over the limit from
// the default size. Zero, negative and NaN must all be judged as that.
func TestANonPositiveHiresScaleIsJudgedAsSdcppsDefault(t *testing.T) {
	for _, scale := range []float64{0, -1, math.NaN()} {
		req := engine.Request{Width: 1024, Height: 1024, Hires: true, HiresScale: scale}
		err := checkRenderSize(profile.ArchQwenImage21, req)
		if err == nil || !strings.Contains(err.Error(), "hires.fix would render 2048x2048") {
			t.Errorf("scale %v: err = %v, want the refusal of sd.cpp's default 2x", scale, err)
		}
	}
	if err := checkRenderSize(profile.ArchQwenImage21, engine.Request{Width: 1024, Height: 1024, HiresScale: 0}); err != nil {
		t.Errorf("hires off: %v", err)
	}
}
