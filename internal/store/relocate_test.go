package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// existsIn builds an `exists` predicate over a fixed set of paths, so relocation
// can be exercised against a synthetic filesystem (ADR-0008).
func existsIn(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return func(p string) bool { return set[p] }
}

func TestMissingFilesReportsEveryAbsentField(t *testing.T) {
	m := InstalledModel{
		Name:    "flux1-dev",
		VAEPath: "/new/ae.safetensors",
		Components: Components{
			DiffusionModel: "/old/flux1-dev.gguf",
			ClipL:          "/new/clip_l.safetensors",
			T5XXL:          "/old/t5xxl.safetensors",
		},
	}
	exists := existsIn("/new/ae.safetensors", "/new/clip_l.safetensors")

	got := m.MissingFiles(exists)
	want := []string{"/old/flux1-dev.gguf", "/old/t5xxl.safetensors"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MissingFiles = %v, want %v", got, want)
	}

	// Every file present => no report at all (not an empty-but-non-nil slice, so
	// `omitempty` drops the JSON field).
	all := existsIn("/new/ae.safetensors", "/old/flux1-dev.gguf", "/new/clip_l.safetensors", "/old/t5xxl.safetensors")
	if got := m.MissingFiles(all); got != nil {
		t.Errorf("MissingFiles with everything present = %v, want nil", got)
	}
}

// MissingFiles must skip empty fields — a single-file checkpoint has no
// components, and reporting "" as missing would break every listing.
func TestMissingFilesIgnoresEmptyFields(t *testing.T) {
	m := InstalledModel{Name: "sd15", Path: "/models/sd15.gguf"}
	if got := m.MissingFiles(existsIn("/models/sd15.gguf")); got != nil {
		t.Errorf("MissingFiles = %v, want nil", got)
	}
	if got := m.MissingFiles(existsIn()); !reflect.DeepEqual(got, []string{"/models/sd15.gguf"}) {
		t.Errorf("MissingFiles = %v, want [/models/sd15.gguf]", got)
	}
}

// Files must keep enumerating every weight-file field after being rebuilt on
// fileRefs — `rm --purge` and `gc` depend on it being exhaustive.
func TestFilesCoversEveryComponent(t *testing.T) {
	m := InstalledModel{
		Path: "/m/base.safetensors", VAEPath: "/m/vae.safetensors",
		Components: Components{
			DiffusionModel: "/m/dit.gguf", ClipL: "/m/clip_l.st", ClipG: "/m/clip_g.st",
			T5XXL: "/m/t5.st", LLM: "/m/qwen.st",
		},
	}
	if got := len(m.Files()); got != 7 {
		t.Errorf("Files() returned %d paths, want 7 (every field): %v", got, m.Files())
	}
}

func TestRelocateRebasesOnlyMissingFiles(t *testing.T) {
	const newDir = "/Volumes/Models/image-forge/models"
	r := &Registry{Models: map[string]InstalledModel{
		// Missing at its recorded path, present in the new dir => rebased.
		"animagine-xl-4": {Name: "animagine-xl-4", Path: "/old/animagine-xl-4.safetensors"},
		// Still resolves where it is => must not be touched, even though a
		// same-named file also sits in the new dir.
		"kept-elsewhere": {Name: "kept-elsewhere", Path: "/elsewhere/kept.safetensors"},
	}}
	exists := existsIn(
		"/elsewhere/kept.safetensors",
		newDir+"/animagine-xl-4.safetensors",
		newDir+"/kept.safetensors",
	)

	plan := r.Relocate(newDir, true, exists)

	if len(plan.Unresolved) != 0 {
		t.Errorf("unexpected unresolved: %+v", plan.Unresolved)
	}
	want := []Relocation{{
		Model: "animagine-xl-4", Field: "path",
		From: "/old/animagine-xl-4.safetensors", To: newDir + "/animagine-xl-4.safetensors",
	}}
	if !reflect.DeepEqual(plan.Moves, want) {
		t.Errorf("Moves = %+v, want %+v", plan.Moves, want)
	}
	if got := r.Models["animagine-xl-4"].Path; got != want[0].To {
		t.Errorf("registry path = %q, want %q", got, want[0].To)
	}
	if got := r.Models["kept-elsewhere"].Path; got != "/elsewhere/kept.safetensors" {
		t.Errorf("a resolvable path was rewritten to %q; it must be left alone", got)
	}
}

// The multi-component case is the one a per-field walk can silently half-fix.
func TestRelocateRebasesComponentsAndVAE(t *testing.T) {
	const newDir = "/vol/models"
	r := &Registry{Models: map[string]InstalledModel{
		"flux1-dev": {
			Name:    "flux1-dev",
			VAEPath: "/old/ae.safetensors",
			Components: Components{
				DiffusionModel: "/old/flux1-dev.gguf",
				ClipL:          "/old/clip_l.safetensors",
				T5XXL:          "/old/t5xxl.safetensors",
			},
		},
	}}
	exists := existsIn(
		newDir+"/ae.safetensors",
		newDir+"/flux1-dev.gguf",
		newDir+"/clip_l.safetensors",
		newDir+"/t5xxl.safetensors",
	)

	plan := r.Relocate(newDir, true, exists)
	if len(plan.Moves) != 4 || len(plan.Unresolved) != 0 {
		t.Fatalf("plan = %d moves / %d unresolved, want 4 / 0: %+v", len(plan.Moves), len(plan.Unresolved), plan)
	}
	m := r.Models["flux1-dev"]
	if m.VAEPath != newDir+"/ae.safetensors" ||
		m.Components.DiffusionModel != newDir+"/flux1-dev.gguf" ||
		m.Components.ClipL != newDir+"/clip_l.safetensors" ||
		m.Components.T5XXL != newDir+"/t5xxl.safetensors" {
		t.Errorf("components not fully rebased: %+v", m)
	}
	if got := m.MissingFiles(exists); got != nil {
		t.Errorf("after relocate the model should load; still missing: %v", got)
	}
}

// A dry run must report exactly what an apply would do, and change nothing.
func TestRelocateDryRunDoesNotMutate(t *testing.T) {
	const newDir = "/vol/models"
	mk := func() *Registry {
		return &Registry{Models: map[string]InstalledModel{
			"a": {Name: "a", Path: "/old/a.safetensors"},
		}}
	}
	exists := existsIn(newDir + "/a.safetensors")

	dry, apply := mk(), mk()
	dryPlan := dry.Relocate(newDir, false, exists)
	applyPlan := apply.Relocate(newDir, true, exists)

	if !reflect.DeepEqual(dryPlan, applyPlan) {
		t.Errorf("dry-run plan %+v differs from applied plan %+v", dryPlan, applyPlan)
	}
	if got := dry.Models["a"].Path; got != "/old/a.safetensors" {
		t.Errorf("dry run mutated the registry: path = %q", got)
	}
	if got := apply.Models["a"].Path; got != newDir+"/a.safetensors" {
		t.Errorf("apply did not write: path = %q", got)
	}
}

// A missing file with no candidate in the target dir must be reported, never
// silently rewritten to a path that does not exist either.
func TestRelocateReportsUnresolved(t *testing.T) {
	const newDir = "/vol/models"
	r := &Registry{Models: map[string]InstalledModel{
		"gone": {Name: "gone", Path: "/old/gone.safetensors"},
	}}

	plan := r.Relocate(newDir, true, existsIn())

	if len(plan.Moves) != 0 {
		t.Errorf("nothing is resolvable, yet Moves = %+v", plan.Moves)
	}
	want := []MissingFile{{Model: "gone", Field: "path", Path: "/old/gone.safetensors"}}
	if !reflect.DeepEqual(plan.Unresolved, want) {
		t.Errorf("Unresolved = %+v, want %+v", plan.Unresolved, want)
	}
	if got := r.Models["gone"].Path; got != "/old/gone.safetensors" {
		t.Errorf("unresolved path was rewritten to %q; it must be left alone", got)
	}
}

// A healthy registry reports an empty plan, which is what `relocate` keys its
// "nothing to do" message off.
func TestRelocateEmptyPlanWhenHealthy(t *testing.T) {
	r := &Registry{Models: map[string]InstalledModel{
		"a": {Name: "a", Path: "/vol/models/a.safetensors"},
	}}
	plan := r.Relocate("/vol/models", true, existsIn("/vol/models/a.safetensors"))
	if !plan.Empty() {
		t.Errorf("plan should be empty, got %+v", plan)
	}
}

// Results must be ordered by model name so a dry run and the following apply
// read identically (Go map iteration is randomized).
func TestRelocateOrderIsDeterministic(t *testing.T) {
	const newDir = "/vol/models"
	models := map[string]InstalledModel{}
	var files []string
	for _, n := range []string{"c", "a", "d", "b"} {
		models[n] = InstalledModel{Name: n, Path: "/old/" + n + ".safetensors"}
		files = append(files, newDir+"/"+n+".safetensors")
	}
	r := &Registry{Models: models}

	plan := r.Relocate(newDir, false, existsIn(files...))
	var got []string
	for _, m := range plan.Moves {
		got = append(got, m.Model)
	}
	if !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("Moves order = %v, want sorted by model name", got)
	}
}

// FileExists is the one place that touches the real filesystem, so pin it.
func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "weights.safetensors")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !FileExists(f) {
		t.Error("FileExists should be true for an existing file")
	}
	if FileExists(filepath.Join(dir, "nope.safetensors")) {
		t.Error("FileExists should be false for an absent file")
	}
}
