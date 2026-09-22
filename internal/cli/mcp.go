package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nlink-jp/pathguard"

	"github.com/nlink-jp/image-forge/internal/config"
	"github.com/nlink-jp/image-forge/internal/engine"
	"github.com/nlink-jp/image-forge/internal/mcp/job"
	"github.com/nlink-jp/image-forge/internal/mcp/mcpserver"
	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
	"github.com/nlink-jp/image-forge/internal/mcp/tools"
	"github.com/nlink-jp/image-forge/internal/mcp/transport"
	"github.com/nlink-jp/image-forge/internal/mcp/workdir"
	"github.com/nlink-jp/image-forge/internal/mcp/workspace"
	"github.com/nlink-jp/image-forge/internal/store"
)

// mcpVersion is set from main's version at dispatch time so the MCP handshake
// reports the same build string as `image-forge version`.
var mcpVersion = "dev"

// runMCP starts the MCP stdio server. It exposes get_usage, generate,
// check_job, and list_models over JSON-RPC 2.0 on stdin/stdout. Logs go to
// stderr (stdout is the MCP transport). TODO: Streamable HTTP transport is out
// of scope for v1 (stdio only).
func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The embedded sd.cpp / ggml / Metal C code writes progress bars and logs to
	// fd 1 (stdout). MCP stdio requires stdout to carry ONLY the JSON-RPC stream,
	// so hand the transport a private handle to the real stdout and repoint fd 1
	// at stderr — any library (or stray Go) write to stdout then lands on stderr,
	// harmless, instead of corrupting the protocol.
	mcpOut, err := redirectStdoutToStderr()
	if err != nil {
		return fmt.Errorf("mcp: isolate stdout: %w", err)
	}

	// Logs must go to stderr; stdout carries the MCP JSON-RPC stream.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	conf, err := config.Load()
	if err != nil {
		return fmt.Errorf("mcp: config: %w", err)
	}

	// SIGINT/SIGTERM (and stdin EOF) shut the server down; canceling ctx aborts
	// an in-flight render and stops the job worker.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	re := NewResidentEngine()
	defer re.Close()

	srv := mcpserver.New("image-forge-mcp", mcpVersion,
		transport.NewStdioTransport(os.Stdin, mcpOut), logger)
	srv.SetInstructions(tools.Instructions)
	workDir, workspaces, reads := mcpGuards()
	tools.Register(srv, &tools.Deps{
		DefaultModel: conf.DefaultModel,
		WS:           workspaces,
		WorkDir:      workDir,
		Render:       &residentRenderer{re: re, reads: reads},
		Upscale:      &engineUpscaler{},
		ListModels:   func(scope string) (any, error) { return ListModels(scope) },
		// Wire the server's SIGINT/SIGTERM-cancellable ctx so shutdown stops the
		// worker and drops queued jobs (rather than the Background() default).
		Jobs:   job.NewManager(ctx),
		Logger: logger,
	})

	logger.Info("image-forge mcp server ready",
		"default_model", conf.DefaultModel, "engine", engine.Info())
	return srv.Serve(ctx)
}

// residentRenderer adapts the shared cli.ResidentEngine to the tools.Renderer
// interface: it pre-checks that the requested model is installed (returning a
// structured model_not_found), then drives ResidentEngine and translates its
// engine.Event stream into the tools progress callback.
type residentRenderer struct {
	re    *ResidentEngine
	reads workdir.Resolver // judges raw model paths (mcpGuards)
}

// mcpGuards builds the MCP server's path guards — the one place they are
// wired, so a test holds the wiring rather than a copy of it:
//   - the work-directory resolver, refusing this server's own directories as
//     a destination: the data directory, the models directory (which
//     models_dir can move outside it) and the config directory (ADR-0009,
//     ADR-0010);
//   - the workspace manager, which judges every <work_dir>/<workspace_id>
//     with it before making it;
//   - the read guard for raw LoRA / ControlNet / hires model paths: the floor
//     plus the config directory, which may hold hf_token and civitai_token —
//     not the models directory, where LoRAs are read from.
func mcpGuards() (workdir.Resolver, *workspace.Manager, workdir.Resolver) {
	cfg := configPlaces()
	server := append([]pathguard.Place{
		pathguard.ServerDir(store.Home(), ""),
		pathguard.ServerDir(store.ModelsDir(), ""),
	}, cfg...)
	wd := workdir.NewResolverFor(server...)
	return wd, workspace.NewManager(wd.CheckBeneath), workdir.NewResolverFor(cfg...)
}

// configPlaces are this server's configuration: the file config.Path() names
// and the legacy file in the data directory, either of which may hold hf_token
// and civitai_token; and the directory holding the config file only when it
// is image-forge's own (the default or $XDG_CONFIG_HOME form) — a config file
// put at ~/image-forge.toml must not make the home directory a server
// directory. A relative config path is made absolute the way the loader reads
// it, from the working directory.
func configPlaces() []pathguard.Place {
	file := config.Path()
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	configFile := func(p string) pathguard.Place {
		return pathguard.Place{Path: p, Kind: pathguard.Protected, Reason: "server_dir",
			Why: "it is this server's config file " + p + ", which may hold tokens"}
	}
	places := []pathguard.Place{configFile(file), configFile(config.LegacyPath())}
	if dir := filepath.Dir(file); filepath.Base(dir) == "image-forge" {
		places = append(places, pathguard.ServerDir(dir, "which holds its configuration"))
	}
	return places
}

func (a *residentRenderer) Render(ctx context.Context, req tools.RenderRequest, report func(fraction float64, message string)) (int64, error) {
	if err := ensureInstalled(req.Model); err != nil {
		return 0, err
	}
	// Judge the raw paths before anything opens or stats them: resolving
	// hires_model stats a raw path, and "not a file" versus "refused" would
	// tell a caller whether a credential file exists.
	reg, err := store.Load()
	if err != nil {
		return 0, toolerr.Newf(toolerr.CodeRenderFailed, "load registry: %v", err)
	}
	installed := func(n string) bool { _, ok := reg.Get(n); return ok }
	if err := mcpReadRefused(a.reads, installed, req.LoRAs, req.ControlNet, req.HiresModel); err != nil {
		return 0, err
	}

	// hires_model may be an installed upscaler name; resolve it to a path.
	hiresModel, err := resolveHiresModel(req.HiresModel)
	if err != nil {
		return 0, toolerr.Newf(toolerr.CodeInvalidArguments, "%v", err)
	}

	rr := RenderRequest{
		Prompt: req.Prompt, Negative: req.Negative, Model: req.Model,
		Output: req.Output, Seed: req.Seed, Steps: req.Steps, CFG: req.CFG,
		Width: req.Width, Height: req.Height, Sampler: req.Sampler, Scheduler: req.Scheduler,
		ClipSkip: req.ClipSkip, Batch: req.Batch,
		Init: req.Init, Mask: req.Mask, Strength: req.Strength,
		// LoRA/ControlNet names→paths are resolved downstream by buildRender
		// (parseLoras + resolveAuxRefs); pass them through verbatim here.
		LoRAs: req.LoRAs, ControlNet: req.ControlNet, Control: req.Control,
		ControlStrength: req.ControlStrength, Canny: req.Canny,
		Guidance: req.Guidance, FlowShift: req.FlowShift, SLGScale: req.SLGScale, ImgCFG: req.ImgCFG,
		Hires: req.Hires, HiresScale: req.HiresScale, HiresDenoise: req.HiresDenoise,
		HiresUpscaler: req.HiresUpscaler, HiresModel: hiresModel,
	}

	events := make(chan engine.Event, 8)
	done := make(chan struct{})
	go func() {
		for ev := range events {
			switch ev.Kind {
			case "load":
				if report != nil {
					report(0, "loading model")
				}
			case "progress":
				if report != nil {
					report(ev.Progress, ev.Message)
				}
			}
		}
		close(done)
	}()
	_, seed, err := a.re.Render(ctx, rr, events)
	close(events)
	<-done
	return seed, err
}

// mcpReadRefused judges the raw paths an MCP call names for the engine to
// read. LoRA and ControlNet references are installed names or raw paths
// (ADR-0006), and a hires model is an installed upscaler or a file; a raw path
// is read wherever it lies, so it must not be a credential or agent-control
// location, nor this server's config directory (organization ADR-021 §7;
// pathguard's Local policy, which follows links and refuses a NUL byte, which
// C would stop at). An installed name is skipped: the registry resolves it to
// the file it names, which is what is opened, and judging the name as a path
// would judge the server's working directory instead. The CLI and the GUI's
// serve loop are a person's own choice and are not judged.
func mcpReadRefused(reads workdir.Resolver, installed func(string) bool, loras []string, controlNet, hiresModel string) error {
	refs := make([]string, 0, len(loras)+2)
	for _, l := range loras {
		p, _, _ := strings.Cut(l, ":")
		refs = append(refs, p)
	}
	refs = append(refs, controlNet, hiresModel)
	for _, ref := range refs {
		if ref == "" || installed(ref) {
			continue
		}
		if why := reads.LocalPath(ref, ref); why != "" {
			return toolerr.Newf(toolerr.CodePathNotAllowed, "%q is refused: %s", ref, why)
		}
	}
	return nil
}

// engineUpscaler adapts engine.Upscale to the tools.Upscaler interface: it
// resolves the requested upscaler model (an installed upscaler name, or a sane
// installed default when none is given), then streams progress to the tools
// report callback.
type engineUpscaler struct{}

func (u *engineUpscaler) Upscale(ctx context.Context, req tools.UpscaleRequest, report func(fraction float64, message string)) error {
	esrgan, err := resolveMCPUpscaler(req.Model)
	if err != nil {
		return err
	}

	// Embed a light metadata record (upscaler / factor / source) unless disabled,
	// mirroring the CLI `upscale` command.
	var meta []engine.PNGText
	if conf, cerr := config.Load(); cerr != nil || conf.EmbedMetadata() {
		meta = buildUpscaleMetadata(req.Model, esrgan, req.Scale, req.Input)
	}

	events := make(chan engine.Event, 8)
	done := make(chan struct{})
	go func() {
		for ev := range events {
			if ev.Kind == "progress" && report != nil {
				report(ev.Progress, ev.Message)
			}
		}
		close(done)
	}()
	uerr := engine.Upscale(engine.UpscaleParams{
		InputPath:  req.Input,
		ESRGANPath: esrgan,
		OutputPath: req.Output,
		Factor:     req.Scale,
		Events:     events,
		Metadata:   meta,
	})
	close(events)
	<-done
	return uerr
}

// resolveMCPUpscaler resolves the upscaler model for an MCP upscale request. A
// named model must be an installed upscaler; when no name is given it picks the
// single installed upscaler, erroring (with pull guidance) if there are zero or
// several to choose from.
func resolveMCPUpscaler(name string) (string, error) {
	reg, err := store.Load()
	if err != nil {
		return "", toolerr.Newf(toolerr.CodeRenderFailed, "load registry: %v", err)
	}
	if name != "" {
		im, ok := reg.Get(name)
		if !ok {
			return "", toolerr.Newf(toolerr.CodeModelNotFound,
				"upscaler %q is not installed — the user pulls it with `image-forge models pull %s`", name, name)
		}
		if !im.IsUpscaler() {
			return "", toolerr.Newf(toolerr.CodeInvalidArguments, "model %q is a diffusion model, not an upscaler", name)
		}
		return im.Path, nil
	}
	var found []store.InstalledModel
	for _, m := range reg.Models {
		if m.IsUpscaler() {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		return "", toolerr.New(toolerr.CodeModelRequired,
			"no upscaler installed — the user pulls one with `image-forge models pull realesrgan-x4plus`, then pass its name as \"model\"")
	case 1:
		return found[0].Path, nil
	default:
		return "", toolerr.New(toolerr.CodeModelRequired,
			"multiple upscalers installed — pass one as \"model\" (call list_models scope=installed)")
	}
}

// redirectStdoutToStderr duplicates the current stdout into a private *os.File
// (returned, for the MCP transport) and then repoints fd 1 at stderr. After
// this, anything the embedded C engine (or stray Go code) writes to stdout goes
// to stderr, keeping the returned handle — the only writer of the JSON-RPC
// stream — pristine. darwin/arm64 only (image-forge's sole target), where
// syscall.Dup/Dup2 exist.
func redirectStdoutToStderr() (*os.File, error) {
	saved, err := syscall.Dup(int(os.Stdout.Fd()))
	if err != nil {
		return nil, err
	}
	if err := syscall.Dup2(int(os.Stderr.Fd()), int(os.Stdout.Fd())); err != nil {
		_ = syscall.Close(saved)
		return nil, err
	}
	return os.NewFile(uintptr(saved), "mcp-stdout"), nil
}

// ensureInstalled returns model_not_found when the named model is not in the
// registry, so the generate tool surfaces a precise, actionable code (rather
// than a generic render_failed) before any model load is attempted.
func ensureInstalled(name string) error {
	if name == "" {
		return nil // caller already guaranteed a model is set
	}
	reg, err := store.Load()
	if err != nil {
		return toolerr.Newf(toolerr.CodeRenderFailed, "load registry: %v", err)
	}
	if _, ok := reg.Get(name); !ok {
		return toolerr.Newf(toolerr.CodeModelNotFound,
			"model %q is not installed — call list_models (scope=installed); the user pulls catalog models with `image-forge models pull %s`", name, name)
	}
	return nil
}
