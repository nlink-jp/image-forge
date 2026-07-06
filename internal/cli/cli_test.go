package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRun_Version(t *testing.T) {
	if err := Run("1.2.3", []string{"version"}); err != nil {
		t.Fatalf("version: unexpected error: %v", err)
	}
}

func TestRun_NoArgs(t *testing.T) {
	if err := Run("dev", nil); err == nil {
		t.Fatal("expected error when no subcommand is given")
	}
}

func TestRun_Unknown(t *testing.T) {
	if err := Run("dev", []string{"nope"}); err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRun_ModelsNeedsSubcommand(t *testing.T) {
	if err := Run("dev", []string{"models"}); err == nil {
		t.Fatal("models without a subcommand should error")
	}
}

func TestRun_GenRequiresPrompt(t *testing.T) {
	// gen validates its flags before touching the engine, so this holds in
	// both the stub and cgo_sdcpp builds.
	if err := Run("dev", []string{"gen"}); err == nil {
		t.Fatal("gen without -p should error")
	}
}

func TestHaveFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.safetensors")
	if haveFile(missing) {
		t.Error("missing file reported as present")
	}
	empty := filepath.Join(dir, "empty.safetensors")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if haveFile(empty) {
		t.Error("empty file must not count as an already-downloaded model")
	}
	full := filepath.Join(dir, "full.safetensors")
	if err := os.WriteFile(full, []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !haveFile(full) {
		t.Error("non-empty file should be reused")
	}
}
