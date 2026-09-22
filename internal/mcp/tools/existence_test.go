package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/mcp/job"
	"github.com/nlink-jp/image-forge/internal/mcp/mcpserver"
	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
	"github.com/nlink-jp/image-forge/internal/mcp/transport"
	"github.com/nlink-jp/image-forge/internal/mcp/workdir"
	"github.com/nlink-jp/image-forge/internal/mcp/workspace"
)

// Whether an input image exists is never the difference between two answers.
// Input images are workspace-relative and read through an os.Root, but a place
// the floor refuses can lie inside a workspace: a .env, this server's own
// directory (work_dir=~/.local with workspace_id=share holds its data
// directory), or the file a link in ~/.ssh leads to when the workspace is in
// that sync folder. Each case names one path twice — once while a file is
// there and once after it is removed — and the whole answer (code, message,
// details) must be the same both times, and a refusal.
//
// The layer observed is the tool call, the answer a caller receives. The home
// directory is a temporary one: nothing is created, read or written in a real
// credential directory (pathguard still lists the account's own, for the links
// inside them).
func TestExistenceIsNotRevealed(t *testing.T) {
	base := realDir(t, t.TempDir())
	home := filepath.Join(base, "home")
	t.Setenv("HOME", home)
	work := filepath.Join(base, "sync")
	ws := filepath.Join(work, "ws")
	server := filepath.Join(ws, "srv") // this server's own directory, inside the workspace
	for _, d := range []string{filepath.Join(home, ".aws"), filepath.Join(home, ".ssh"), ws, server} {
		mkdirAll(t, d)
	}
	symlink(t, filepath.Join(ws, "ssh_config.png"), filepath.Join(home, ".ssh", "config"))
	symlink(t, filepath.Join(home, ".aws", "planted.png"), filepath.Join(ws, "lnk_file.png"))
	symlink(t, filepath.Join(home, ".aws"), filepath.Join(ws, "lnk_dir"))

	srv := serverWithDir(t, server)
	answer := func(tool, key, rel string) string {
		args := map[string]any{"work_dir": work, "workspace_id": "ws", key: rel, "model": "m", "prompt": "p"}
		if tool == "upscale" {
			delete(args, "prompt")
		}
		raw, _ := json.Marshal(args)
		_, err := srv.Call(context.Background(), tool, raw)
		if err == nil {
			return "accepted"
		}
		var te *toolerr.Error
		if !errors.As(err, &te) {
			return "untyped: " + err.Error()
		}
		d, _ := json.Marshal(te.Details)
		return fmt.Sprintf("%s | %s | %s", te.Code, te.Message, d)
	}
	for _, c := range []struct{ name, rel, leaf string }{
		{"a .env file", filepath.Join("sub", ".env"), filepath.Join(ws, "sub", ".env")},
		{"this server's own directory", filepath.Join("srv", "config.toml"), filepath.Join(server, "config.toml")},
		{"where a link in ~/.ssh leads", "ssh_config.png", filepath.Join(ws, "ssh_config.png")},
		{"a planted link to a credential file", "lnk_file.png", filepath.Join(home, ".aws", "planted.png")},
		{"through a planted link to a credential directory", filepath.Join("lnk_dir", "via.png"), filepath.Join(home, ".aws", "via.png")},
	} {
		for _, call := range []struct{ tool, key string }{{"generate", "init"}, {"upscale", "input"}} {
			t.Run(call.tool+"/"+c.name, func(t *testing.T) {
				writeFileAt(t, c.leaf)
				e := answer(call.tool, call.key, c.rel)
				if err := os.Remove(c.leaf); err != nil {
					t.Fatal(err)
				}
				m := answer(call.tool, call.key, c.rel)
				if !strings.HasPrefix(e, toolerr.CodePathNotAllowed+" ") {
					t.Errorf("existing: %s\n  want path_not_allowed", e)
				}
				if e != m {
					t.Errorf("the answer tells them apart\n  existing: %s\n  missing:  %s", e, m)
				}
			})
		}
	}
	// The control: an ordinary missing input is still reported missing.
	if a := answer("generate", "init", "typo.png"); !strings.HasPrefix(a, toolerr.CodeInputNotFound+" ") {
		t.Errorf("an ordinary missing input: %s", a)
	}
}

// serverWithDir is newHarness's server with this server's own directory at dir.
func serverWithDir(t *testing.T, dir string) *mcpserver.Server {
	t.Helper()
	resolver := workdir.NewResolver(dir)
	srv := mcpserver.New("image-forge-mcp", "test",
		transport.NewStdioTransport(strings.NewReader(""), io.Discard), nil)
	Register(srv, &Deps{
		WS:         workspace.NewManager(resolver.CheckBeneath),
		WorkDir:    resolver,
		Render:     &fakeRenderer{seed: 1},
		Upscale:    &fakeUpscaler{},
		ListModels: func(scope string) (any, error) { return map[string]any{"scope": scope}, nil },
		Jobs:       job.NewManager(context.Background()),
	})
	return srv
}

func realDir(t *testing.T, d string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(d)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mkdirAll(t *testing.T, d string) {
	t.Helper()
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, at string) {
	t.Helper()
	if err := os.Symlink(target, at); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func writeFileAt(t *testing.T, p string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte("not really an image"), 0o600); err != nil {
		t.Fatal(err)
	}
}
