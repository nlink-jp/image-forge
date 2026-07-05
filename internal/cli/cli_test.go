package cli

import (
	"errors"
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

func TestRun_ScaffoldStubs(t *testing.T) {
	for _, sub := range []string{"models", "serve"} {
		if err := Run("dev", []string{sub}); !errors.Is(err, ErrNotImplemented) {
			t.Errorf("%s: want ErrNotImplemented, got %v", sub, err)
		}
	}
}

func TestRun_GenRequiresPrompt(t *testing.T) {
	// gen validates its flags before touching the engine, so this holds in
	// both the stub and cgo_sdcpp builds.
	if err := Run("dev", []string{"gen"}); err == nil {
		t.Fatal("gen without -p should error")
	}
}
