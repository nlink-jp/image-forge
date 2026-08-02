package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/store"
)

// fakeFS builds an `exists` predicate over a fixed set of paths, so the
// relocate/missing-file surfaces are testable without touching disk (ADR-0008).
func fakeFS(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return func(p string) bool { return set[p] }
}

// relocRegistry is the reported scenario: the model files were moved to a new
// volume and config's models_dir updated, but the registry still records the old
// absolute paths.
func relocRegistry() *store.Registry {
	return &store.Registry{Models: map[string]store.InstalledModel{
		"animagine-xl-4": {Name: "animagine-xl-4", Path: "/old/animagine-xl-4.safetensors"},
		"gone":           {Name: "gone", Path: "/old/gone.safetensors"},
	}}
}

const relocNewDir = "/Volumes/Models/image-forge/models"

func relocFS() func(string) bool {
	return fakeFS(relocNewDir + "/animagine-xl-4.safetensors")
}

// A dry run must describe the change and write nothing — `save` must not fire.
func TestRunRelocateDryRunDoesNotSave(t *testing.T) {
	reg := relocRegistry()
	var out bytes.Buffer
	saved := false
	save := func() error { saved = true; return nil }

	if err := runRelocate(&out, reg, relocNewDir, false, relocFS(), save); err != nil {
		t.Fatalf("runRelocate: %v", err)
	}
	if saved {
		t.Error("a dry run must not save the registry")
	}
	if got := reg.Models["animagine-xl-4"].Path; got != "/old/animagine-xl-4.safetensors" {
		t.Errorf("a dry run mutated the registry: path = %q", got)
	}
	s := out.String()
	for _, want := range []string{
		"/old/animagine-xl-4.safetensors",
		relocNewDir + "/animagine-xl-4.safetensors",
		"--apply",
		"gone",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, s)
		}
	}
}

func TestRunRelocateApplyWritesAndReportsUnresolved(t *testing.T) {
	reg := relocRegistry()
	var out bytes.Buffer
	saved := false
	save := func() error { saved = true; return nil }

	if err := runRelocate(&out, reg, relocNewDir, true, relocFS(), save); err != nil {
		t.Fatalf("runRelocate: %v", err)
	}
	if !saved {
		t.Error("--apply must save the registry")
	}
	if got := reg.Models["animagine-xl-4"].Path; got != relocNewDir+"/animagine-xl-4.safetensors" {
		t.Errorf("path = %q, want the rebased path", got)
	}
	if got := reg.Models["gone"].Path; got != "/old/gone.safetensors" {
		t.Errorf("an unresolvable path was rewritten to %q; it must be left alone", got)
	}
	s := out.String()
	if !strings.Contains(s, "rewrote 1 path") {
		t.Errorf("output should report the write:\n%s", s)
	}
	if !strings.Contains(s, "still missing") {
		t.Errorf("output should report the unresolved file:\n%s", s)
	}
}

// Nothing missing => a clear "nothing to do", and no write.
func TestRunRelocateHealthyRegistry(t *testing.T) {
	reg := &store.Registry{Models: map[string]store.InstalledModel{
		"a": {Name: "a", Path: relocNewDir + "/a.safetensors"},
	}}
	var out bytes.Buffer
	saved := false

	if err := runRelocate(&out, reg, relocNewDir, true, fakeFS(relocNewDir+"/a.safetensors"), func() error { saved = true; return nil }); err != nil {
		t.Fatalf("runRelocate: %v", err)
	}
	if saved {
		t.Error("an unchanged registry must not be rewritten")
	}
	if !strings.Contains(out.String(), "nothing to do") {
		t.Errorf("output = %q, want a nothing-to-do message", out.String())
	}
}

// A save failure must surface, not be swallowed into a success message.
func TestRunRelocateSaveErrorSurfaces(t *testing.T) {
	var out bytes.Buffer
	err := runRelocate(&out, relocRegistry(), relocNewDir, true, relocFS(), func() error {
		return errors.New("disk full")
	})
	if err == nil {
		t.Fatal("a failed save must return an error")
	}
	if strings.Contains(out.String(), "rewrote") {
		t.Errorf("a failed save must not report success:\n%s", out.String())
	}
}

// The listing surfaces (CLI table, --json, MCP list_models) must all report a
// model whose weights are gone, instead of presenting unverified state as fact.
func TestInstalledViewsReportMissingFiles(t *testing.T) {
	views := installedViewsWith(relocRegistry(), relocFS())

	byName := map[string]installedView{}
	for _, v := range views {
		byName[v.Name] = v
	}
	if v := byName["animagine-xl-4"]; !v.IsMissing() {
		t.Errorf("animagine-xl-4's file is absent; view should be missing: %+v", v)
	}
	if v := byName["gone"]; !v.IsMissing() {
		t.Errorf("gone's file is absent; view should be missing: %+v", v)
	}

	healthy := installedViewsWith(relocRegistry(), fakeFS(
		"/old/animagine-xl-4.safetensors", "/old/gone.safetensors"))
	for _, v := range healthy {
		if v.IsMissing() {
			t.Errorf("%s: every file exists, yet reported missing: %v", v.Name, v.MissingFiles)
		}
	}
}

func TestPrintMissingFooter(t *testing.T) {
	var out bytes.Buffer
	printMissingFooter(&out, installedViewsWith(relocRegistry(), relocFS()), relocNewDir)

	s := out.String()
	for _, want := range []string{
		"2 installed model(s) have missing weight files",
		"/old/animagine-xl-4.safetensors",
		relocNewDir,
		"models relocate --apply",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("footer missing %q:\n%s", want, s)
		}
	}

	// A healthy registry prints nothing at all — no warning noise in the common case.
	var quiet bytes.Buffer
	healthy := installedViewsWith(relocRegistry(), fakeFS(
		"/old/animagine-xl-4.safetensors", "/old/gone.safetensors"))
	printMissingFooter(&quiet, healthy, relocNewDir)
	if quiet.Len() != 0 {
		t.Errorf("healthy registry should print no footer, got:\n%s", quiet.String())
	}
}

// The reported bug end to end: an installed model whose weight file is gone must
// fail at resolution with an actionable message, not reach the engine and fail
// there on a stale path.
func TestResolveModelRejectsMissingWeights(t *testing.T) {
	t.Setenv("IMAGE_FORGE_HOME", t.TempDir())
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	reg.Add(store.InstalledModel{Name: "moved", Path: "/old/moved.safetensors"})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	_, err = resolveModel("moved", "")
	if err == nil {
		t.Fatal("resolving a model with missing weights should fail")
	}
	if !strings.Contains(err.Error(), "models relocate") {
		t.Errorf("error should point at the fix: %v", err)
	}
}

// The generation-time error is what the user actually hits; it must name the
// missing file and the fix, not just fail on a stale path inside the engine.
func TestMissingFilesErrorNamesTheFix(t *testing.T) {
	err := missingFilesError("animagine-xl-4",
		[]string{"/old/animagine-xl-4.safetensors"}, relocNewDir)
	s := err.Error()
	for _, want := range []string{
		"animagine-xl-4",
		"/old/animagine-xl-4.safetensors",
		relocNewDir,
		"models relocate --apply",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("error missing %q: %s", want, s)
		}
	}
}
