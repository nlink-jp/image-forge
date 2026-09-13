package tools

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/nlink-jp/image-forge/internal/mcp/mcpserver"
)

// usageMarkdown is the client-neutral operating manual returned by get_usage:
// clients should not have to operate this stateful, file-mediated server by
// trial and error. Coherence with the real tools/errors/schema is pinned by
// tools_test.go.
//
//go:embed usage.md
var usageMarkdown string

// Instructions is the short initialize-time hint that makes get_usage
// discoverable (surfaced via the MCP `instructions` field).
const Instructions = "image-forge mcp generates images locally via an embedded diffusion engine " +
	"(stable-diffusion.cpp on Apple Silicon / Metal). Every call names work_dir: the absolute path of a " +
	"directory you can read back (your session or working directory). It is required and has no default, " +
	"and the workspace is <work_dir>/<workspace_id>/. It is stateful, file-mediated, and async: " +
	"generated PNGs are written under that workspace and returned as file paths (never image bytes); " +
	"generate enqueues a job and returns a job_id, which you poll with check_job. " +
	"Call the get_usage tool before your first generation to learn the workspace model, the generate " +
	"parameters, the job lifecycle, and the error recovery table."

func registerGetUsage(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "get_usage",
		Description: "Return this server's operating manual (markdown): the work_dir contract and the workspace model, " +
			"the generate parameters, the async job lifecycle (generate -> job_id -> check_job), how to " +
			"reference input images, and the error recovery table. Call it once before your first generation.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		var in struct{}
		if err := unmarshalStrict(args, &in); err != nil {
			return nil, err
		}
		return mcpserver.RawResult{
			Content: []mcpserver.ContentBlock{{Type: "text", Text: usageMarkdown}},
		}, nil
	})
}
