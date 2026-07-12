// Package cli dispatches image-forge subcommands. It is deliberately thin and
// I/O-injectable so the command surface stays testable without a real engine.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nlink-jp/image-forge/internal/config"
	"github.com/nlink-jp/image-forge/internal/engine"
	"github.com/nlink-jp/image-forge/internal/store"
)

// ErrNotImplemented marks scaffold subcommands that are not wired yet.
var ErrNotImplemented = errors.New("not implemented yet (scaffold)")

// Run dispatches args[0] as a subcommand. version is printed by `version` and
// embedded in generation metadata (via the binVersion package var).
func Run(version string, args []string) error {
	binVersion = version // record for embedded PNG metadata (mirrors mcpVersion)
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no subcommand given")
	}
	// Honor a config-relocated models directory (e.g. onto a bigger disk) before
	// any subcommand resolves paths. Best-effort: a missing/invalid config must
	// not break `version`/`help`.
	if conf, err := config.Load(); err == nil {
		if d := conf.ModelsDirResolved(); d != "" {
			store.SetModelsDir(d)
		}
	}
	switch args[0] {
	case "gen":
		return runGen(args[1:])
	case "upscale":
		return runUpscale(args[1:])
	case "models":
		return runModels(args[1:])
	case "serve":
		return runServe(args[1:])
	case "mcp":
		mcpVersion = version
		return runMCP(args[1:])
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
  image-forge gen     -p "<prompt>" [flags]             generate (txt2img / img2img; --hires auto|on|off)
  image-forge upscale <in> -o <out> [--model <name>]    ESRGAN super-resolution of an existing image
  image-forge models  <list|pull|open|import|quantize|rm|gc> manage models (open = model's web page)
  image-forge serve   [flags]                           resident JSON-line API (Phase 2)
  image-forge mcp     [--workspace-root <dir>]          MCP stdio server (AI image generation)
  image-forge version                                   print version

Run "image-forge <command> --help" for command details.
`)
}

// runGen lives in gen.go, runUpscale in upscale.go, runModels in models.go,
// runServe in serve.go.
