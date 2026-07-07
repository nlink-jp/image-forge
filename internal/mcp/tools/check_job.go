package tools

import (
	"context"
	"encoding/json"

	"github.com/nlink-jp/image-forge/internal/mcp/mcpserver"
	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
)

func registerCheckJob(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "check_job",
		Description: "Poll an async generate job: state (queued/running/done/error), progress, and — when done — " +
			"the output PNG path(s) and seed(s). Jobs do not survive a server restart; if the job_id is unknown, " +
			"re-submit generate (it re-renders from the same workspace).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "required": ["job_id"],
  "properties": {
    "job_id": {"type": "string"}
  },
  "additionalProperties": false
}`),
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		var in struct {
			JobID string `json:"job_id"`
		}
		if err := unmarshalStrict(args, &in); err != nil {
			return nil, err
		}
		if in.JobID == "" {
			return nil, toolerr.New(toolerr.CodeMissingArgument, "job_id is required")
		}
		return d.Jobs.Get(in.JobID)
	})
}
