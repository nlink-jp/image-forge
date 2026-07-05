// Package cli dispatches image-forge subcommands. It is deliberately thin and
// I/O-injectable so the command surface stays testable without a real engine.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nlink-jp/image-forge/internal/engine"
)

// ErrNotImplemented marks scaffold subcommands that are not wired yet.
var ErrNotImplemented = errors.New("not implemented yet (scaffold)")

// Run dispatches args[0] as a subcommand. version is printed by `version`.
func Run(version string, args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no subcommand given")
	}
	switch args[0] {
	case "gen":
		return runGen(args[1:])
	case "models":
		return runModels(args[1:])
	case "serve":
		return runServe(args[1:])
	case "version", "--version", "-v":
		fmt.Fprintln(os.Stdout, "image-forge", version)
		fmt.Fprintln(os.Stdout, engine.Info())
		return nil
	case "help", "--help", "-h":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `image-forge — local diffusion image-generation engine

Usage:
  image-forge gen    -p "<prompt>" [flags]              generate (txt2img / img2img)
  image-forge models <list|pull|import|quantize|rm>     manage models
  image-forge serve  [flags]                            resident JSON-line API (Phase 2)
  image-forge version                                   print version

Run "image-forge <command> --help" for command details.
`)
}

// runGen lives in gen.go. models/serve are scaffold stubs for now.
func runModels(args []string) error { return fmt.Errorf("models: %w", ErrNotImplemented) }
func runServe(args []string) error  { return fmt.Errorf("serve (Phase 2): %w", ErrNotImplemented) }
