package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/nlink-jp/image-forge/internal/store"
)

func touch(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// confirmers for the destructive-core tests. A real terminal is never involved.
var (
	confirmYes = func(string) bool { return true }
	confirmNo  = func(string) bool { return false }
)

// gcTestDirs sets up an isolated data home and models dir, and pins ModelsDir via
// SetModelsDir (not just IMAGE_FORGE_HOME) so a test is immune to the global
// override cli.Run() leaks from a real config. Every destructive test operates
// under this throwaway dir.
func gcTestDirs(t *testing.T) (home, md string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("IMAGE_FORGE_HOME", home)
	md = filepath.Join(home, "models")
	if err := os.MkdirAll(md, 0o755); err != nil {
		t.Fatal(err)
	}
	store.SetModelsDir(md)
	t.Cleanup(func() { store.SetModelsDir("") })
	return home, md
}

// TestModelsGcForce_NoTTY_DeletesNothing is the regression test for the incident
// that motivated the HITL gate: a `models gc --force` invoked in a non-interactive
// context (a test, a script) must delete NOTHING, because it can't get a "yes" at
// a terminal. This calls the REAL command path (as the buggy test once did) with
// an orphan present and an empty registry — the maximally dangerous case.
func TestModelsGcForce_NoTTY_DeletesNothing(t *testing.T) {
	_, md := gcTestDirs(t)
	orphan := filepath.Join(md, "would-be-deleted.safetensors")
	touch(t, orphan, 100)
	reg, _ := store.Load() // empty registry → orphan is unreferenced
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := modelsGc([]string{"--force"}); err != nil {
		t.Fatalf("modelsGc --force: %v", err)
	}
	if !exists(orphan) {
		t.Fatal("REGRESSION: `gc --force` deleted a file with no interactive confirmation (no TTY)")
	}
}

// TestModelsRmPurge_NoTTY_DeletesNothing: the real `rm --purge` path also refuses
// without a terminal, leaving both the file and the registry entry intact.
func TestModelsRmPurge_NoTTY_DeletesNothing(t *testing.T) {
	_, md := gcTestDirs(t)
	ckpt := filepath.Join(md, "keeper.safetensors")
	touch(t, ckpt, 100)
	reg, _ := store.Load()
	reg.Add(store.InstalledModel{Name: "keeper", Path: ckpt})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := modelsRm([]string{"keeper", "--purge"}); err != nil {
		t.Fatalf("modelsRm --purge: %v", err)
	}
	if !exists(ckpt) {
		t.Fatal("REGRESSION: `rm --purge` deleted a file with no interactive confirmation (no TTY)")
	}
	if reg2, _ := store.Load(); func() bool { _, ok := reg2.Get("keeper"); return ok }() == false {
		t.Error("declined/blocked purge should leave the registry entry intact")
	}
}

func TestRunGc_DeletesOnlyOnConfirm(t *testing.T) {
	_, md := gcTestDirs(t)
	keep := filepath.Join(md, "keep.safetensors")
	orphan := filepath.Join(md, "orphan.safetensors")
	partial := filepath.Join(md, "big.safetensors.part")
	touch(t, keep, 100)
	touch(t, orphan, 200)
	touch(t, partial, 50)
	reg := &store.Registry{Models: map[string]store.InstalledModel{
		"m": {Name: "m", Path: keep},
	}}

	// Dry-run (force=false): never deletes, never consults confirm.
	if err := runGc(io.Discard, reg, md, false, confirmYes); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	for _, p := range []string{keep, orphan, partial} {
		if !exists(p) {
			t.Errorf("dry-run deleted %s", p)
		}
	}

	// force + declined: still deletes nothing.
	if err := runGc(io.Discard, reg, md, true, confirmNo); err != nil {
		t.Fatalf("force+declined: %v", err)
	}
	if !exists(orphan) || !exists(partial) {
		t.Error("declined confirmation still deleted orphans")
	}

	// force + confirmed: orphans reclaimed, referenced file untouched.
	if err := runGc(io.Discard, reg, md, true, confirmYes); err != nil {
		t.Fatalf("force+confirmed: %v", err)
	}
	if !exists(keep) {
		t.Error("gc deleted a referenced file")
	}
	if exists(orphan) || exists(partial) {
		t.Error("gc did not delete the orphans after confirmation")
	}
}

func TestRunRmPurge_SharedAndOutsideKept(t *testing.T) {
	home, md := gcTestDirs(t)
	aCkpt := filepath.Join(md, "a.safetensors")
	bCkpt := filepath.Join(md, "b.safetensors")
	sharedVAE := filepath.Join(md, "shared.vae.safetensors")
	outside := filepath.Join(home, "my-own.safetensors") // imported in place
	for _, p := range []string{aCkpt, bCkpt, sharedVAE, outside} {
		touch(t, p, 100)
	}
	reg := &store.Registry{Models: map[string]store.InstalledModel{
		"a":   {Name: "a", Path: aCkpt, VAEPath: sharedVAE},
		"b":   {Name: "b", Path: bCkpt, VAEPath: sharedVAE},
		"ext": {Name: "ext", Path: outside},
	}}

	// Confirmed purge of a: deletes a's checkpoint, keeps the VAE b shares.
	if err := runRm(io.Discard, reg, "a", true, md, confirmYes); err != nil {
		t.Fatalf("purge a: %v", err)
	}
	if exists(aCkpt) {
		t.Error("purge did not delete a's own checkpoint")
	}
	if !exists(sharedVAE) {
		t.Error("purge deleted a VAE still referenced by b")
	}
	if _, ok := reg.Get("a"); ok {
		t.Error("a should be removed from the registry after a confirmed purge")
	}

	// Purge ext: its file is outside the managed dir → kept (entry still dropped).
	if err := runRm(io.Discard, reg, "ext", true, md, confirmYes); err != nil {
		t.Fatalf("purge ext: %v", err)
	}
	if !exists(outside) {
		t.Error("purge deleted a file outside the managed models dir")
	}

	// Purge b: now the shared VAE has no other user → it goes too.
	if err := runRm(io.Discard, reg, "b", true, md, confirmYes); err != nil {
		t.Fatalf("purge b: %v", err)
	}
	if exists(sharedVAE) {
		t.Error("purge kept the shared VAE after its last user was removed")
	}
}

func TestRunRmPurge_DeclineIsFullNoOp(t *testing.T) {
	_, md := gcTestDirs(t)
	ckpt := filepath.Join(md, "c.safetensors")
	touch(t, ckpt, 100)
	reg := &store.Registry{Models: map[string]store.InstalledModel{
		"c": {Name: "c", Path: ckpt},
	}}
	if err := runRm(io.Discard, reg, "c", true, md, confirmNo); err != nil {
		t.Fatalf("declined purge: %v", err)
	}
	if !exists(ckpt) {
		t.Error("declined purge deleted the file")
	}
	if _, ok := reg.Get("c"); !ok {
		t.Error("declined purge should leave the registry entry intact (full no-op)")
	}
}

func TestRunRmNoPurgeKeepsFile(t *testing.T) {
	_, md := gcTestDirs(t)
	ckpt := filepath.Join(md, "d.safetensors")
	touch(t, ckpt, 100)
	reg := &store.Registry{Models: map[string]store.InstalledModel{
		"d": {Name: "d", Path: ckpt},
	}}
	if err := runRm(io.Discard, reg, "d", false, md, confirmNo); err != nil {
		t.Fatalf("rm d: %v", err)
	}
	if !exists(ckpt) {
		t.Error("rm without --purge deleted the model file")
	}
	if _, ok := reg.Get("d"); ok {
		t.Error("d should be removed from the registry")
	}
}
