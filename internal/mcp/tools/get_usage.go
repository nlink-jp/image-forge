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
	"(stable-diffusion.cpp on Apple Silicon / Metal). It is stateful, file-mediated, and async: " +
	"generated PNGs are written under a workspace directory and returned as file paths (never image bytes); " +
	"generate enqueues a job and returns a job_id, which you poll with check_job. " +
	"Call the get_usage tool before your first generation to learn the workspace model, the generate " +
	"parameters, the job lifecycle, and the error recovery table."

func registerGetUsage(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "get_usage",
		Description: "Return this server's operating manual (markdown): the workspace model and workspace_root, " +
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
