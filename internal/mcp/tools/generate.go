package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nlink-jp/image-forge/internal/engine"
	"github.com/nlink-jp/image-forge/internal/mcp/job"
	"github.com/nlink-jp/image-forge/internal/mcp/mcpserver"
	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
	"github.com/nlink-jp/image-forge/internal/mcp/workspace"
)

// generateArgs is the decoded generate tool input.
type generateArgs struct {
	WorkspaceID   string   `json:"workspace_id"`
	WorkspaceRoot string   `json:"workspace_root"`
	Prompt        string   `json:"prompt"`
	Model         string   `json:"model"`
	Negative      *string  `json:"negative"`
	Seed          *int64   `json:"seed"`
	Steps         *int     `json:"steps"`
	CFG           *float64 `json:"cfg"`
	Width         *int     `json:"width"`
	Height        *int     `json:"height"`
	Sampler       *string  `json:"sampler"`
	Scheduler     *string  `json:"scheduler"`
	ClipSkip      *int     `json:"clip_skip"`
	Batch         *int     `json:"batch"`
	Init          string   `json:"init"`
	Mask          string   `json:"mask"`
	Strength      *float64 `json:"strength"`
	OutputName    string   `json:"output_name"`
	Hires         string   `json:"hires"`
	HiresScale    *float64 `json:"hires_scale"`
	HiresDenoise  *float64 `json:"hires_denoise"`
	HiresUpscaler *string  `json:"hires_upscaler"`
	HiresModel    string   `json:"hires_model"`
}

// Output is one produced image, returned on job done.
type Output struct {
	Path    string `json:"path"`     // workspace-relative (output/<name>-<seed>.png)
	AbsPath string `json:"abs_path"` // absolute path on this host
	Seed    int64  `json:"seed"`
}

// GenerateResult is the check_job "done" payload for a generate job.
type GenerateResult struct {
	WorkspaceID string   `json:"workspace_id"`
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	Outputs     []Output `json:"outputs"`
}

func registerGenerate(srv *mcpserver.Server, d *Deps) {
	srv.RegisterTool(mcpserver.Tool{
		Name: "generate",
		Description: "Enqueue an image-generation job and return a job_id immediately. The image is rendered in the " +
			"background by the embedded diffusion engine (renders are serialized) and written to the workspace's " +
			"output/ directory as a PNG. Poll check_job with the returned job_id for progress and, on done, the " +
			"output path(s) and seed(s). Reference init/mask images by workspace-relative paths (place them in the " +
			"workspace first). If no model is given and no default is configured, returns model_required — call " +
			"list_models to pick one.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "required": ["workspace_id", "prompt"],
  "properties": {
    "workspace_id": {"type": "string", "description": "One project per workspace; [a-zA-Z0-9_-]{1,64}"},
    "workspace_root": {"type": "string", "description": "Absolute path to an agent-prepared workspace root directory (create it first); omit to use the server default"},
    "prompt": {"type": "string", "description": "Text prompt"},
    "model": {"type": "string", "description": "Installed model registry name (see list_models); omit to use the server's default_model"},
    "negative": {"type": "string", "description": "Negative prompt"},
    "seed": {"type": "integer", "description": "Seed; -1 = random (the concrete seed is reported back)"},
    "steps": {"type": "integer", "description": "Sampling steps (override the model profile)"},
    "cfg": {"type": "number", "description": "CFG scale (override the model profile)"},
    "width": {"type": "integer", "description": "Width in px (override the model profile)"},
    "height": {"type": "integer", "description": "Height in px (override the model profile)"},
    "sampler": {"type": "string", "description": "Sampler (override the model profile)"},
    "scheduler": {"type": "string", "description": "Scheduler: discrete|karras|exponential|ays|..."},
    "clip_skip": {"type": "integer", "description": "CLIP skip (override the model profile)"},
    "batch": {"type": "integer", "description": "Images per run (sequential seeds)"},
    "init": {"type": "string", "description": "img2img init image, workspace-relative path (place it in the workspace first)"},
    "mask": {"type": "string", "description": "inpaint mask, workspace-relative path; requires init (white=regenerate, black=keep)"},
    "strength": {"type": "number", "description": "img2img denoise strength 0..1 (with init)"},
    "output_name": {"type": "string", "description": "Base name for the PNG (default: gen); final file output/<output_name>-<seed>.png"},
    "hires": {"type": "string", "enum": ["auto", "on", "off"], "description": "hires.fix (a second higher-res pass that adds detail): auto (default; follow the model profile) | on | off"},
    "hires_scale": {"type": "number", "description": "hires upscale factor (default: profile or 1.5)"},
    "hires_denoise": {"type": "number", "description": "hires denoise strength 0..1 (default: profile or 0.5)"},
    "hires_upscaler": {"type": "string", "enum": ["latent", "lanczos", "nearest", "model"], "description": "hires upscaler (default: profile or latent)"},
    "hires_model": {"type": "string", "description": "installed upscaler name for hires_upscaler=model (see list_models)"}
  },
  "additionalProperties": false
}`),
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		var in generateArgs
		if err := unmarshalStrict(args, &in); err != nil {
			return nil, err
		}
		if in.WorkspaceID == "" {
			return nil, toolerr.New(toolerr.CodeMissingArgument, "workspace_id is required")
		}
		if in.Prompt == "" {
			return nil, toolerr.New(toolerr.CodeMissingArgument, "prompt is required")
		}
		if in.Mask != "" && in.Init == "" {
			return nil, toolerr.New(toolerr.CodeInvalidArguments, "mask requires init (inpaint needs a base image)")
		}

		model := in.Model
		if model == "" {
			model = d.DefaultModel
		}
		if model == "" {
			return nil, toolerr.New(toolerr.CodeModelRequired,
				"no model given and no default_model configured — call list_models and pass one as \"model\"")
		}

		outputName := in.OutputName
		if outputName == "" {
			outputName = "gen"
		}
		if strings.ContainsAny(outputName, `/\`) || strings.Contains(outputName, "..") {
			return nil, toolerr.Newf(toolerr.CodeInvalidArguments, "output_name must be a plain file name, got %q", in.OutputName)
		}

		ws, err := d.WS.EnsureIn(in.WorkspaceRoot, in.WorkspaceID)
		if err != nil {
			return nil, err
		}

		// Resolve + verify input images (init/mask). They are agent-placed inside
		// the workspace and referenced by workspace-relative paths; hand the engine
		// the absolute path only after os.Root confirms it is a real file inside.
		initAbs, err := resolveInput(ws, in.Init)
		if err != nil {
			return nil, err
		}
		maskAbs, err := resolveInput(ws, in.Mask)
		if err != nil {
			return nil, err
		}

		req := RenderRequest{
			Prompt:    in.Prompt,
			Negative:  in.Negative,
			Model:     model,
			Seed:      in.Seed,
			Steps:     in.Steps,
			CFG:       in.CFG,
			Width:     in.Width,
			Height:    in.Height,
			Sampler:   in.Sampler,
			Scheduler: in.Scheduler,
			ClipSkip:  in.ClipSkip,
			Batch:     in.Batch,
			Init:      initAbs,
			Mask:      maskAbs,
			Strength:  in.Strength,

			Hires:         in.Hires,
			HiresScale:    in.HiresScale,
			HiresDenoise:  in.HiresDenoise,
			HiresUpscaler: in.HiresUpscaler,
			HiresModel:    in.HiresModel,
		}

		wsID := in.WorkspaceID
		jobID := d.Jobs.Submit(func(ctx context.Context, report func(job.Progress)) (any, error) {
			// The final file name depends on the concrete seed, which we only learn
			// after resolveSeed inside the engine. Render to a temp name, then rename
			// to output/<output_name>-<seed>.png once the seed is known.
			tmpRel := filepath.Join(workspace.DirOutput, outputName+".tmp.png")
			r := req
			r.Output = ws.Path(tmpRel)
			seed, rerr := d.Render.Render(ctx, r, func(f float64, msg string) {
				report(job.Progress{Fraction: f, Message: msg})
			})
			if rerr != nil {
				return nil, mapRenderErr(rerr)
			}
			finalRel := filepath.Join(workspace.DirOutput, sprintfSeed(outputName, seed))
			data, err := ws.ReadFile(tmpRel)
			if err != nil {
				return nil, toolerr.Newf(toolerr.CodeRenderFailed, "read rendered image: %v", err)
			}
			if err := ws.WriteFileAtomic(finalRel, data); err != nil {
				return nil, err
			}
			_ = ws.RemoveAll(tmpRel)
			return GenerateResult{
				WorkspaceID: wsID,
				Model:       model,
				Prompt:      in.Prompt,
				Outputs: []Output{{
					Path:    finalRel,
					AbsPath: ws.Path(finalRel),
					Seed:    seed,
				}},
			}, nil
		})
		return map[string]any{"job_id": jobID, "state": job.StateQueued}, nil
	})
}

// resolveInput validates an optional workspace-relative input image and returns
// its absolute path, verifying it is a real file inside the workspace. Empty in
// => empty out (no input).
func resolveInput(ws *workspace.Workspace, rel string) (string, error) {
	if rel == "" {
		return "", nil
	}
	cleaned, err := ws.ResolveInside(rel)
	if err != nil {
		return "", err
	}
	if err := ws.VerifyRegular(cleaned); err != nil {
		return "", err
	}
	return ws.Path(cleaned), nil
}

// sprintfSeed builds "<base>-<seed>.png".
func sprintfSeed(base string, seed int64) string {
	return base + "-" + strconv.FormatInt(seed, 10) + ".png"
}

// mapRenderErr maps an engine error to a structured tool error. A build without
// the diffusion runtime (engine.ErrNoRuntime) becomes no_runtime; anything else
// becomes render_failed (unless already a *toolerr.Error, e.g. model_not_found
// from resolveModel — which the job manager preserves under errors.As).
func mapRenderErr(err error) error {
	var te *toolerr.Error
	if errors.As(err, &te) {
		return te
	}
	if errors.Is(err, engine.ErrNoRuntime) {
		return toolerr.New(toolerr.CodeNoRuntime, err.Error())
	}
	return toolerr.New(toolerr.CodeRenderFailed, err.Error())
}
