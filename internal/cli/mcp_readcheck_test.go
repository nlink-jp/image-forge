package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
)

// A LoRA, ControlNet or hires model given as a raw path is read wherever it
// lies, so an MCP call may not point one at a credential or agent-control
// location (organization ADR-021 §7; ADR-0010). Registry names are not paths
// and are left to the registry.
func TestMCPReadRefusesACredentialLocationAsAModelFile(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	key := filepath.Join(home, ".ssh", "id_rsa")
	if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "style.safetensors")
	if err := os.Symlink(key, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	refused := map[string]func() error{
		"lora":                  func() error { return mcpReadRefused([]string{key + ":0.8"}, "", "") },
		"lora without a weight": func() error { return mcpReadRefused([]string{filepath.Join(home, ".aws", "x.safetensors")}, "", "") },
		"control_net":           func() error { return mcpReadRefused(nil, filepath.Join(home, ".SSH", "cn.safetensors"), "") },
		"hires_model":           func() error { return mcpReadRefused(nil, "", filepath.Join(home, ".netrc")) },
		"a link to a key":       func() error { return mcpReadRefused([]string{link + ":1"}, "", "") },
	}
	for name, call := range refused {
		var te *toolerr.Error
		if err := call(); !errors.As(err, &te) || te.Code != toolerr.CodePathNotAllowed {
			t.Errorf("%s: err = %v, want %s", name, err, toolerr.CodePathNotAllowed)
		}
	}

	elsewhere := filepath.Join(t.TempDir(), "detail.safetensors")
	if err := mcpReadRefused([]string{"detail-tweaker:0.7", elsewhere + ":1"}, "controlnet-canny-sd15", elsewhere); err != nil {
		t.Errorf("registry names and an ordinary file were refused: %v", err)
	}
}
