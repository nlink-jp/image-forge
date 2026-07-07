package tools

import (
	"context"
	"encoding/json"

	"github.com/nlink-jp/image-forge/internal/mcp/mcpserver"
	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
)

func registerListModels(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "list_models",
		Description: "List diffusion models as JSON. scope=installed (default) lists models ready to generate with; " +
			"scope=catalog lists the curated models available to pull (the user pulls them with the CLI); scope=all " +
			"lists both. Use this to pick a model name for generate's model parameter.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "scope": {"type": "string", "enum": ["installed", "catalog", "all"], "description": "which set to list (default: installed)"}
  },
  "additionalProperties": false
}`),
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		var in struct {
			Scope string `json:"scope"`
		}
		if err := unmarshalStrict(args, &in); err != nil {
			return nil, err
		}
		scope := in.Scope
		if scope == "" {
			scope = "installed"
		}
		switch scope {
		case "installed", "catalog", "all":
		default:
			return nil, toolerr.Newf(toolerr.CodeInvalidScope,
				"invalid scope %q: want installed|catalog|all", scope)
		}
		if d.ListModels == nil {
			return nil, toolerr.New(toolerr.CodeInvalidArguments, "list_models is not available in this build")
		}
		listing, err := d.ListModels(scope)
		if err != nil {
			return nil, toolerr.Newf(toolerr.CodeInvalidArguments, "list models: %v", err)
		}
		return listing, nil
	})
}
