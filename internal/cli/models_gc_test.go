package cli

import (
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

// gcTestDirs sets up an isolated data home and models dir. It pins ModelsDir via
// SetModelsDir (not just IMAGE_FORGE_HOME) so the test is immune to the global
// override another test may have leaked by calling Run() with a real config.
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

func TestModelsGc(t *testing.T) {
	_, md := gcTestDirs(t)
	keep := filepath.Join(md, "keep.safetensors")
	orphan := filepath.Join(md, "orphan.safetensors")
	partial := filepath.Join(md, "big.safetensors.part") // leftover download
	touch(t, keep, 100)
	touch(t, orphan, 200)
	touch(t, partial, 50)

	reg, _ := store.Load()
	reg.Add(store.InstalledModel{Name: "m", Path: keep})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	// Dry-run (no args): reports but deletes nothing.
	if err := modelsGc(nil); err != nil {
		t.Fatalf("gc dry-run: %v", err)
	}
	for _, p := range []string{keep, orphan, partial} {
		if !exists(p) {
			t.Errorf("dry-run gc deleted %s", p)
		}
	}

	// --force: orphans reclaimed, the referenced file untouched.
	if err := modelsGc([]string{"--force"}); err != nil {
		t.Fatalf("gc --force: %v", err)
	}
	if !exists(keep) {
		t.Error("gc --force deleted a referenced file")
	}
	if exists(orphan) || exists(partial) {
		t.Error("gc --force left orphaned files behind")
	}
}

func TestModelsRmPurge(t *testing.T) {
	home, md := gcTestDirs(t)
	aCkpt := filepath.Join(md, "a.safetensors")
	bCkpt := filepath.Join(md, "b.safetensors")
	sharedVAE := filepath.Join(md, "shared.vae.safetensors")
	outside := filepath.Join(home, "my-own.safetensors") // imported in place, not under md
	for _, p := range []string{aCkpt, bCkpt, sharedVAE, outside} {
		touch(t, p, 100)
	}
	reg, _ := store.Load()
	reg.Add(store.InstalledModel{Name: "a", Path: aCkpt, VAEPath: sharedVAE})
	reg.Add(store.InstalledModel{Name: "b", Path: bCkpt, VAEPath: sharedVAE}) // shares the VAE
	reg.Add(store.InstalledModel{Name: "ext", Path: outside})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	// Purge a: deletes a's checkpoint, keeps the VAE b still needs, never touches b.
	if err := modelsRm([]string{"a", "--purge"}); err != nil {
		t.Fatalf("rm a --purge: %v", err)
	}
	if exists(aCkpt) {
		t.Error("purge did not delete a's own checkpoint")
	}
	if !exists(sharedVAE) {
		t.Error("purge deleted a VAE still referenced by b")
	}
	if !exists(bCkpt) {
		t.Error("purge touched an unrelated model's file")
	}
	if reg, _ := store.Load(); func() bool { _, ok := reg.Get("a"); return ok }() {
		t.Error("a is still in the registry after rm")
	}

	// Purge ext: its file is outside the managed models dir → kept.
	if err := modelsRm([]string{"ext", "--purge"}); err != nil {
		t.Fatalf("rm ext --purge: %v", err)
	}
	if !exists(outside) {
		t.Error("purge deleted a file outside the managed models dir")
	}

	// Purge b: now the shared VAE has no other user → it goes too.
	if err := modelsRm([]string{"b", "--purge"}); err != nil {
		t.Fatalf("rm b --purge: %v", err)
	}
	if exists(bCkpt) {
		t.Error("purge did not delete b's checkpoint")
	}
	if exists(sharedVAE) {
		t.Error("purge kept the shared VAE after its last user was removed")
	}
}

func TestModelsRmNoPurgeKeepsFile(t *testing.T) {
	_, md := gcTestDirs(t)
	ckpt := filepath.Join(md, "c.safetensors")
	touch(t, ckpt, 100)
	reg, _ := store.Load()
	reg.Add(store.InstalledModel{Name: "c", Path: ckpt})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := modelsRm([]string{"c"}); err != nil {
		t.Fatalf("rm c: %v", err)
	}
	if !exists(ckpt) {
		t.Error("rm without --purge deleted the model file")
	}
	if reg, _ := store.Load(); func() bool { _, ok := reg.Get("c"); return ok }() {
		t.Error("c is still in the registry after rm")
	}
}
