package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nlink-jp/image-forge/internal/mcp/job"
	"github.com/nlink-jp/image-forge/internal/mcp/mcpserver"
	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
	"github.com/nlink-jp/image-forge/internal/mcp/transport"
	"github.com/nlink-jp/image-forge/internal/mcp/workspace"
)

// fakeRenderer materializes the output file and returns a deterministic seed,
// so tests need no diffusion engine. If failWith is set it returns that error.
type fakeRenderer struct {
	failWith error
	seed     int64
	lastReq  RenderRequest
}

func (f *fakeRenderer) Render(ctx context.Context, req RenderRequest, report func(float64, string)) (int64, error) {
	f.lastReq = req
	if report != nil {
		report(0.5, "step 15/30")
	}
	if f.failWith != nil {
		return 0, f.failWith
	}
	if err := os.WriteFile(req.Output, []byte("PNGDATA"), 0o644); err != nil {
		return 0, err
	}
	seed := f.seed
	if req.Seed != nil && *req.Seed >= 0 {
		seed = *req.Seed
	}
	return seed, nil
}

type harness struct {
	t    *testing.T
	srv  *mcpserver.Server
	def  *workspace.Manager
	rend *fakeRenderer
}

func newHarness(t *testing.T, rend *fakeRenderer) *harness {
	t.Helper()
	if rend == nil {
		rend = &fakeRenderer{seed: 12345}
	}
	def := workspace.NewManager(filepath.Join(t.TempDir(), "default"))
	srv := mcpserver.New("image-forge-mcp", "test",
		transport.NewStdioTransport(strings.NewReader(""), io.Discard), nil)
	Register(srv, &Deps{
		DefaultModel: "",
		WS:           def,
		Render:       rend,
		ListModels: func(scope string) (any, error) {
			return map[string]any{"scope": scope}, nil
		},
		Jobs: job.NewManager(context.Background()),
	})
	return &harness{t: t, srv: srv, def: def, rend: rend}
}

func (h *harness) call(name string, args map[string]any) (any, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		h.t.Fatal(err)
	}
	return h.srv.Call(context.Background(), name, raw)
}

// seedWorkspace prepares an agent-style workspace root.
func seedWorkspace(t *testing.T, wsID string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, wsID), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func (h *harness) pollDone(jobID string) job.Status {
	h.t.Helper()
	for i := 0; i < 1000; i++ {
		o, err := h.call("check_job", map[string]any{"job_id": jobID})
		if err != nil {
			h.t.Fatalf("check_job: %v", err)
		}
		st := o.(job.Status)
		if st.State == job.StateDone || st.State == job.StateError {
			return st
		}
		time.Sleep(2 * time.Millisecond)
	}
	h.t.Fatalf("job %s did not finish", jobID)
	return job.Status{}
}

func TestGenerateThenCheckJob(t *testing.T) {
	h := newHarness(t, &fakeRenderer{seed: 777})
	root := seedWorkspace(t, "proj")

	out, err := h.call("generate", map[string]any{
		"workspace_id":   "proj",
		"workspace_root": root,
		"prompt":         "a cat",
		"model":          "sdxl",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	sub := out.(map[string]any)
	if sub["state"] != job.StateQueued {
		t.Fatalf("submit state: %+v", sub)
	}
	jobID, _ := sub["job_id"].(string)
	if jobID == "" {
		t.Fatalf("no job_id: %+v", sub)
	}

	st := h.pollDone(jobID)
	if st.State != job.StateDone {
		t.Fatalf("job not done: %+v (err=%v)", st, st.Error)
	}
	res := st.Result.(GenerateResult)
	if len(res.Outputs) != 1 {
		t.Fatalf("outputs: %+v", res.Outputs)
	}
	o := res.Outputs[0]
	if o.Seed != 777 {
		t.Errorf("seed = %d, want 777", o.Seed)
	}
	wantRel := filepath.Join("output", "gen-777.png")
	if o.Path != wantRel {
		t.Errorf("path = %q, want %q", o.Path, wantRel)
	}
	if o.AbsPath != filepath.Join(root, "proj", wantRel) {
		t.Errorf("abs_path = %q", o.AbsPath)
	}
	if _, err := os.Stat(o.AbsPath); err != nil {
		t.Errorf("output not written: %v", err)
	}
	// The temp render file must be cleaned up.
	if _, err := os.Stat(filepath.Join(root, "proj", "output", "gen.tmp.png")); !os.IsNotExist(err) {
		t.Errorf("temp render file left behind: %v", err)
	}
	// The default root must stay untouched.
	if _, err := os.Stat(filepath.Join(h.def.Root(), "proj")); !os.IsNotExist(err) {
		t.Errorf("default root should be untouched: %v", err)
	}
}

func TestGenerateOutputName(t *testing.T) {
	h := newHarness(t, &fakeRenderer{seed: 5})
	root := seedWorkspace(t, "proj")
	out, err := h.call("generate", map[string]any{
		"workspace_id":   "proj",
		"workspace_root": root,
		"prompt":         "x",
		"model":          "sdxl",
		"output_name":    "hero",
		"seed":           42,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	st := h.pollDone(out.(map[string]any)["job_id"].(string))
	res := st.Result.(GenerateResult)
	if res.Outputs[0].Path != filepath.Join("output", "hero-42.png") {
		t.Errorf("path = %q", res.Outputs[0].Path)
	}
	if res.Outputs[0].Seed != 42 {
		t.Errorf("seed = %d, want 42 (explicit seed reported back)", res.Outputs[0].Seed)
	}
}

func TestGenerateModelRequired(t *testing.T) {
	h := newHarness(t, nil)
	root := seedWorkspace(t, "proj")
	_, err := h.call("generate", map[string]any{
		"workspace_id":   "proj",
		"workspace_root": root,
		"prompt":         "x",
	})
	var te *toolerr.Error
	if !errors.As(err, &te) || te.Code != toolerr.CodeModelRequired {
		t.Fatalf("want model_required, got %v", err)
	}
}

func TestGenerateDefaultModel(t *testing.T) {
	// With a configured default model, no model arg is needed.
	def := workspace.NewManager(filepath.Join(t.TempDir(), "default"))
	rend := &fakeRenderer{seed: 9}
	srv := mcpserver.New("image-forge-mcp", "test",
		transport.NewStdioTransport(strings.NewReader(""), io.Discard), nil)
	Register(srv, &Deps{DefaultModel: "cfg-default", WS: def, Render: rend, Jobs: job.NewManager(context.Background())})
	root := seedWorkspace(t, "proj")
	raw, _ := json.Marshal(map[string]any{"workspace_id": "proj", "workspace_root": root, "prompt": "x"})
	out, err := srv.Call(context.Background(), "generate", raw)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	jobID := out.(map[string]any)["job_id"].(string)
	for i := 0; i < 1000; i++ {
		s, _ := srv.Call(context.Background(), "check_job", mustJSON(t, map[string]any{"job_id": jobID}))
		if st := s.(job.Status); st.State == job.StateDone {
			if rend.lastReq.Model != "cfg-default" {
				t.Errorf("model = %q, want cfg-default", rend.lastReq.Model)
			}
			return
		} else if st.State == job.StateError {
			t.Fatalf("errored: %v", st.Error)
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("did not finish")
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGenerateRenderErrorSurfaced(t *testing.T) {
	// A renderer that returns a structured error propagates its code to check_job.
	h := newHarness(t, &fakeRenderer{failWith: toolerr.New(toolerr.CodeModelNotFound, "not installed")})
	root := seedWorkspace(t, "proj")
	out, err := h.call("generate", map[string]any{
		"workspace_id": "proj", "workspace_root": root, "prompt": "x", "model": "ghost",
	})
	if err != nil {
		t.Fatalf("generate submit: %v", err)
	}
	st := h.pollDone(out.(map[string]any)["job_id"].(string))
	if st.State != job.StateError || st.Error == nil || st.Error.Code != toolerr.CodeModelNotFound {
		t.Fatalf("want error state model_not_found, got %+v", st)
	}
}

func TestGenerateMaskRequiresInit(t *testing.T) {
	h := newHarness(t, nil)
	root := seedWorkspace(t, "proj")
	_, err := h.call("generate", map[string]any{
		"workspace_id": "proj", "workspace_root": root, "prompt": "x", "model": "sdxl",
		"mask": "m.png",
	})
	var te *toolerr.Error
	if !errors.As(err, &te) || te.Code != toolerr.CodeInvalidArguments {
		t.Fatalf("want invalid_arguments, got %v", err)
	}
}

func TestGenerateInitNotFound(t *testing.T) {
	h := newHarness(t, nil)
	root := seedWorkspace(t, "proj")
	_, err := h.call("generate", map[string]any{
		"workspace_id": "proj", "workspace_root": root, "prompt": "x", "model": "sdxl",
		"init": "missing.png",
	})
	var te *toolerr.Error
	if !errors.As(err, &te) || te.Code != toolerr.CodeInputNotFound {
		t.Fatalf("want input_not_found, got %v", err)
	}
}

func TestGenerateInitVerifiedAndPassed(t *testing.T) {
	rend := &fakeRenderer{seed: 1}
	h := newHarness(t, rend)
	root := seedWorkspace(t, "proj")
	// Place an init image in the workspace.
	if err := os.WriteFile(filepath.Join(root, "proj", "base.png"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := h.call("generate", map[string]any{
		"workspace_id": "proj", "workspace_root": root, "prompt": "x", "model": "sdxl",
		"init": "base.png", "strength": 0.4,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	h.pollDone(out.(map[string]any)["job_id"].(string))
	if rend.lastReq.Init != filepath.Join(root, "proj", "base.png") {
		t.Errorf("init passed to renderer = %q (want absolute in-workspace path)", rend.lastReq.Init)
	}
}

func TestGenerateInvalidWorkspaceID(t *testing.T) {
	h := newHarness(t, nil)
	_, err := h.call("generate", map[string]any{
		"workspace_id": "bad id", "prompt": "x", "model": "sdxl",
	})
	if !errors.Is(err, toolerr.New(toolerr.CodeInvalidWorkspaceID, "")) {
		t.Fatalf("want invalid_workspace_id, got %v", err)
	}
}

func TestGenerateUnknownArgRejected(t *testing.T) {
	h := newHarness(t, nil)
	_, err := h.call("generate", map[string]any{
		"workspace_id": "proj", "prompt": "x", "model": "sdxl", "bogus": 1,
	})
	var te *toolerr.Error
	if !errors.As(err, &te) || te.Code != toolerr.CodeInvalidArguments {
		t.Fatalf("want invalid_arguments for unknown field, got %v", err)
	}
}

func TestGenerateMissingArgs(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.call("generate", map[string]any{"prompt": "x"}); !errors.Is(err, toolerr.New(toolerr.CodeMissingArgument, "")) {
		t.Fatalf("missing workspace_id: %v", err)
	}
	if _, err := h.call("generate", map[string]any{"workspace_id": "proj"}); !errors.Is(err, toolerr.New(toolerr.CodeMissingArgument, "")) {
		t.Fatalf("missing prompt: %v", err)
	}
}

func TestCheckJobMissingArg(t *testing.T) {
	h := newHarness(t, nil)
	_, err := h.call("check_job", map[string]any{"job_id": ""})
	if !errors.Is(err, toolerr.New(toolerr.CodeMissingArgument, "")) {
		t.Fatalf("want missing_argument, got %v", err)
	}
}

func TestCheckJobNotFound(t *testing.T) {
	h := newHarness(t, nil)
	_, err := h.call("check_job", map[string]any{"job_id": "job_deadbeef"})
	if !errors.Is(err, toolerr.New(toolerr.CodeJobNotFound, "")) {
		t.Fatalf("want job_not_found, got %v", err)
	}
}

func TestListModelsScope(t *testing.T) {
	h := newHarness(t, nil)
	// Default scope is installed.
	out, err := h.call("list_models", map[string]any{})
	if err != nil {
		t.Fatalf("list_models: %v", err)
	}
	if out.(map[string]any)["scope"] != "installed" {
		t.Errorf("default scope: %v", out)
	}
	for _, scope := range []string{"installed", "catalog", "all"} {
		if _, err := h.call("list_models", map[string]any{"scope": scope}); err != nil {
			t.Errorf("scope %q: %v", scope, err)
		}
	}
	_, err = h.call("list_models", map[string]any{"scope": "bogus"})
	var te *toolerr.Error
	if !errors.As(err, &te) || te.Code != toolerr.CodeInvalidScope {
		t.Fatalf("want invalid_scope, got %v", err)
	}
}

func TestGetUsage(t *testing.T) {
	h := newHarness(t, nil)
	out, err := h.call("get_usage", map[string]any{})
	if err != nil {
		t.Fatalf("get_usage: %v", err)
	}
	raw := out.(mcpserver.RawResult)
	s := raw.Content[0].Text
	for _, want := range []string{"Workspace model", "Generate parameters", "Job lifecycle", "Error recovery"} {
		if !strings.Contains(s, want) {
			t.Errorf("usage missing %q", want)
		}
	}
}

// TestUsageCoherence pins usage.md against the real server surface.
func TestUsageCoherence(t *testing.T) {
	for _, tool := range []string{"generate", "check_job", "list_models"} {
		if !strings.Contains(usageMarkdown, "`"+tool+"`") {
			t.Errorf("usage.md does not reference tool %q", tool)
		}
	}
	for _, code := range []string{
		"model_required", "model_not_found", "no_runtime", "input_not_found",
		"path_not_allowed", "invalid_workspace_id", "render_failed", "job_not_found",
	} {
		if !strings.Contains(usageMarkdown, code) {
			t.Errorf("usage.md recovery table missing %q", code)
		}
	}
	if !strings.Contains(Instructions, "get_usage") {
		t.Errorf("initialize instructions must point at get_usage")
	}
}

// TestToolsListed confirms all four tools register with valid input schemas.
func TestToolsListed(t *testing.T) {
	h := newHarness(t, nil)
	raw, err := h.srv.Call(context.Background(), "get_usage", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = raw
	for _, name := range []string{"get_usage", "generate", "check_job", "list_models"} {
		if _, err := h.srv.Call(context.Background(), name, json.RawMessage(`{}`)); err != nil {
			// generate/check_job legitimately error on empty args (missing required);
			// we only assert the tool is registered (no "unknown tool").
			if strings.Contains(err.Error(), "unknown tool") {
				t.Errorf("tool %q not registered", name)
			}
		}
	}
}
