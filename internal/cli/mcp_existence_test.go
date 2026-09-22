package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
)

// A raw LoRA, ControlNet or hires model path is judged before anything opens
// or stats it, so whether the file exists is never the difference between two
// answers. Each case names one path twice — once while a file is there and
// once after it is removed — and both answers must be the same refusal.
func TestModelPathExistenceIsNotRevealed(t *testing.T) {
	home, cfgDir := guardHome(t)
	_, _, reads := mcpGuards()
	none := func(string) bool { return false }
	sync := filepath.Join(home, "sync")
	plant := filepath.Join(home, "plant")
	for _, d := range []string{filepath.Join(home, ".aws"), filepath.Join(home, ".ssh"), sync, plant} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for target, at := range map[string]string{
		filepath.Join(sync, "ssh_config"):                  filepath.Join(home, ".ssh", "config"),
		filepath.Join(home, ".aws", "planted.safetensors"): filepath.Join(plant, "lnk.safetensors"),
		filepath.Join(home, ".aws"):                        filepath.Join(plant, "lnk_dir"),
	} {
		if err := os.Symlink(target, at); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	answer := func(which, p string) string {
		var err error
		switch which {
		case "lora":
			err = mcpReadRefused(reads, none, []string{p + ":0.8"}, "", "")
		case "control_net":
			err = mcpReadRefused(reads, none, nil, p, "")
		default:
			err = mcpReadRefused(reads, none, nil, "", p)
		}
		var te *toolerr.Error
		if !errors.As(err, &te) {
			return fmt.Sprint(err)
		}
		return te.Code + " | " + te.Message
	}
	for _, c := range []struct{ name, arg, leaf string }{
		{"in a credential directory", filepath.Join(home, ".aws", "m.safetensors"), filepath.Join(home, ".aws", "m.safetensors")},
		{"a planted link to a credential file", filepath.Join(plant, "lnk.safetensors"), filepath.Join(home, ".aws", "planted.safetensors")},
		{"through a planted link to a credential directory", filepath.Join(plant, "lnk_dir", "via.safetensors"), filepath.Join(home, ".aws", "via.safetensors")},
		{"where a link in ~/.ssh leads", filepath.Join(sync, "ssh_config"), filepath.Join(sync, "ssh_config")},
		{"this server's config", filepath.Join(cfgDir, "config.toml"), filepath.Join(cfgDir, "config.toml")},
	} {
		for _, which := range []string{"lora", "control_net", "hires_model"} {
			write(t, c.leaf)
			e := answer(which, c.arg)
			if err := os.Remove(c.leaf); err != nil {
				t.Fatal(err)
			}
			m := answer(which, c.arg)
			if e != m || !strings.HasPrefix(e, toolerr.CodePathNotAllowed+" ") {
				t.Errorf("%s %s:\n  existing: %s\n  missing:  %s\n  want the same path_not_allowed", which, c.name, e, m)
			}
		}
	}
}
