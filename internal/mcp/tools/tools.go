// Package tools implements the MCP tools exposed by `image-forge mcp`.
//
// The server is file-mediated: tools return file PATHS (workspace-relative and
// absolute), never image bytes. Generated PNGs are written under the
// workspace's output/ subdirectory. Generation is async — generate enqueues a
// job and returns a job_id; check_job polls it.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nlink-jp/image-forge/internal/mcp/job"
	"github.com/nlink-jp/image-forge/internal/mcp/mcpserver"
	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
	"github.com/nlink-jp/image-forge/internal/mcp/workspace"
)

// Renderer renders one generation request to an output file, returning the
// concrete seed used. It is an interface so tests can supply a fake and the
// protocol/plumbing tests run under the plain (no-cgo) build. The production
// implementation is cli.ResidentEngine, which keeps one model resident and
// reloads only when the model identity changes.
//
// Render is NOT safe for concurrent use (the Metal engine is not
// concurrent-safe); the job manager serializes calls through a single worker.
type Renderer interface {
	// Render renders req, writing the image to req.Output (an absolute path).
	// report is called with 0..1 progress. Returns the seed actually used.
	Render(ctx context.Context, req RenderRequest, report func(fraction float64, message string)) (seed int64, err error)
}

// RenderRequest is the engine-neutral request the Renderer receives. Pointer
// fields are overrides: nil => use the model profile's default. Output is an
// absolute path inside the workspace's output/ directory.
type RenderRequest struct {
	Prompt    string
	Negative  *string
	Model     string
	Output    string
	Seed      *int64
	Steps     *int
	CFG       *float64
	Width     *int
	Height    *int
	Sampler   *string
	Scheduler *string
	ClipSkip  *int
	Batch     *int
	Init      string // absolute path (verified regular) or empty
	Mask      string // absolute path (verified regular) or empty
	Strength  *float64
}

// ModelLister returns the installed and/or catalog model views as a
// JSON-serializable value for the given scope ("installed"|"catalog"|"all").
// It is injected (rather than importing internal/cli) to avoid an import cycle;
// the cli bootstrap wires in cli.ListModels, which reuses the exact views that
// back `models list`.
type ModelLister func(scope string) (any, error)

// Deps carries the shared dependencies of all tools.
type Deps struct {
	// DefaultModel is used by generate when no model arg is given (from config).
	DefaultModel string
	// WS manages workspaces (default root + agent-prepared roots).
	WS *workspace.Manager
	// Render performs the actual generation (real engine or a test fake).
	Render Renderer
	// ListModels backs the list_models tool (injected; see ModelLister).
	ListModels ModelLister
	// Jobs tracks background (async) renders via a single FIFO worker.
	Jobs *job.Manager
	// Logger is optional.
	Logger *slog.Logger
}

// Register attaches all tools to the MCP server.
func Register(srv *mcpserver.Server, d *Deps) {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Jobs == nil {
		d.Jobs = job.NewManager(context.Background())
	}
	registerGetUsage(srv, d)
	registerGenerate(srv, d)
	registerCheckJob(srv, d)
	registerListModels(srv, d)
}

// unmarshalStrict decodes tool arguments, rejecting unknown fields so agent
// typos surface as invalid_arguments instead of being silently ignored.
func unmarshalStrict(args json.RawMessage, into any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return toolerr.Newf(toolerr.CodeInvalidArguments, "invalid arguments: %v", err)
	}
	return nil
}
