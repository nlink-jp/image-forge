package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/nlink-jp/image-forge/internal/mcp/job"
	"github.com/nlink-jp/image-forge/internal/mcp/mcpserver"
	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
	"github.com/nlink-jp/image-forge/internal/mcp/workspace"
)

// upscaleArgs is the decoded upscale tool input.
type upscaleArgs struct {
	WorkspaceID string `json:"workspace_id"`
	WorkDir     string `json:"work_dir"`
	Input       string `json:"input"`
	Model       string `json:"model"`
	Scale       *int   `json:"scale"`
	OutputName  string `json:"output_name"`
}

// UpscaleResult is the check_job "done" payload for an upscale job.
type UpscaleResult struct {
	WorkspaceID string   `json:"workspace_id"`
	Model       string   `json:"model"`
	Outputs     []Output `json:"outputs"`
}

func registerUpscale(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "upscale",
		Description: "Enqueue a standalone ESRGAN super-resolution job (upscale an existing image) and return a job_id " +
			"immediately. The upscaled PNG is written to the workspace's output/ directory. Poll check_job for the " +
			"result. Reference the input by a workspace-relative path (place it in the workspace first). If no model " +
			"is given and exactly one upscaler is installed it is used; otherwise you get model_required — call " +
			"list_models (scope=installed) and pass an upscaler name.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "required": ["work_dir", "workspace_id", "input"],
  "properties": {
    "workspace_id": {"type": "string", "description": "One project per workspace; [a-zA-Z0-9_-]{1,64}"},
    "work_dir": {"type": "string", "description": "Absolute path to a directory you can read back \u2014 your session or working directory. The workspace is <work_dir>/<workspace_id>/ and every image lands under it, so a directory you cannot open leaves you holding a path to nothing. It must already exist, and nothing here expands ~ or resolves a relative path."},
    "input": {"type": "string", "description": "Image to upscale, workspace-relative path (place it in the workspace first)"},
    "model": {"type": "string", "description": "Installed upscaler name (see list_models scope=installed); omit to use the sole installed upscaler"},
    "scale": {"type": "integer", "description": "Upscale factor (default: the model's native factor, typically 4)"},
    "output_name": {"type": "string", "description": "Base name for the PNG (default: upscaled); final file output/<output_name>.png"}
  },
  "additionalProperties": false
}`),
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		var in upscaleArgs
		if err := unmarshalStrict(args, &in); err != nil {
			return nil, err
		}
		if in.WorkspaceID == "" {
			return nil, toolerr.New(toolerr.CodeMissingArgument, "workspace_id is required")
		}
		if in.Input == "" {
			return nil, toolerr.New(toolerr.CodeMissingArgument, "input is required")
		}
		if d.Upscale == nil {
			return nil, toolerr.New(toolerr.CodeNoRuntime, "upscaling is not available in this build")
		}

		outputName := in.OutputName
		if outputName == "" {
			outputName = "upscaled"
		}
		if strings.ContainsAny(outputName, `/\`) || strings.Contains(outputName, "..") {
			return nil, toolerr.Newf(toolerr.CodeInvalidArguments, "output_name must be a plain file name, got %q", in.OutputName)
		}

		workDir, err := d.WorkDir.Resolve(ctx, in.WorkDir)
		if err != nil {
			return nil, err
		}

		ws, err := d.WS.EnsureUnder(workDir, in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		inputAbs, err := resolveInput(ws, d.WorkDir, in.Input)
		if err != nil {
			return nil, err
		}

		scale := 0
		if in.Scale != nil {
			scale = *in.Scale
		}
		wsID := in.WorkspaceID
		model := in.Model
		jobID := d.Jobs.Submit(func(ctx context.Context, report func(job.Progress)) (any, error) {
			// Engine writes with plain os.Create (outside os.Root); render to a temp
			// name inside the workspace, then re-materialize it atomically via os.Root.
			tmpRel := filepath.Join(workspace.DirOutput, outputName+".tmp.png")
			finalRel := filepath.Join(workspace.DirOutput, outputName+".png")
			// os.Create follows a symlink planted at that name, so the upscale
			// would land outside the workspace. Clear the name through the
			// containment root first — an os.Root remove unlinks the link
			// itself instead of following it — so the engine always creates the
			// file fresh, and a removal the root refuses stops the job before
			// the path is handed out.
			if err := ws.RemoveAll(tmpRel); err != nil {
				return nil, err
			}
			uerr := d.Upscale.Upscale(ctx, UpscaleRequest{
				Input:  inputAbs,
				Output: ws.Path(tmpRel),
				Model:  model,
				Scale:  scale,
			}, func(f float64, msg string) {
				report(job.Progress{Fraction: f, Message: msg})
			})
			if uerr != nil {
				return nil, mapRenderErr(uerr)
			}
			data, err := ws.ReadFile(tmpRel)
			if err != nil {
				return nil, toolerr.Newf(toolerr.CodeRenderFailed, "read upscaled image: %v", err)
			}
			if err := ws.WriteFileAtomic(finalRel, data); err != nil {
				return nil, err
			}
			_ = ws.RemoveAll(tmpRel)
			return UpscaleResult{
				WorkspaceID: wsID,
				Model:       model,
				Outputs: []Output{{
					Path:    finalRel,
					AbsPath: ws.Path(finalRel),
				}},
			}, nil
		})
		return map[string]any{"job_id": jobID, "state": job.StateQueued}, nil
	})
}
