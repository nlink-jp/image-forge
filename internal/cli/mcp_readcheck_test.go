package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nlink-jp/image-forge/internal/mcp/toolerr"
	"github.com/nlink-jp/image-forge/internal/mcp/tools"
	"github.com/nlink-jp/image-forge/internal/mcp/workdir"
	"github.com/nlink-jp/image-forge/internal/store"
)

// guardHome points HOME, the config and the data directory at directories the
// test owns, so the guards are built as the server builds them.
func guardHome(t *testing.T) (home, cfgDir string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("IMAGE_FORGE_CONFIG", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("IMAGE_FORGE_HOME", filepath.Join(home, "if-data"))
	cfgDir = filepath.Join(home, ".config", "image-forge")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return home, cfgDir
}

func write(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func wantRefused(t *testing.T, name string, err error) {
	t.Helper()
	var te *toolerr.Error
	if !errors.As(err, &te) || te.Code != toolerr.CodePathNotAllowed {
		t.Errorf("%s: err = %v, want %s", name, err, toolerr.CodePathNotAllowed)
	}
}

// A LoRA, ControlNet or hires model given as a raw path is read wherever it
// lies, so an MCP call may not point one at a credential or agent-control
// location, nor at this server's config directory, which may hold hf_token
// (organization ADR-021 §7; ADR-0010).
func TestMCPReadRefusesCredentialAndConfigLocations(t *testing.T) {
	home, cfgDir := guardHome(t)
	_, _, reads := mcpGuards()
	none := func(string) bool { return false }
	key := filepath.Join(home, ".ssh", "id_rsa")
	write(t, key)
	link := filepath.Join(t.TempDir(), "style.safetensors")
	if err := os.Symlink(key, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for name, call := range map[string]func() error{
		"lora": func() error { return mcpReadRefused(reads, none, []string{key + ":0.8"}, "", "") },
		"lora without a weight": func() error {
			return mcpReadRefused(reads, none, []string{filepath.Join(home, ".aws", "x.safetensors")}, "", "")
		},
		"control_net": func() error {
			return mcpReadRefused(reads, none, nil, filepath.Join(home, ".SSH", "cn.safetensors"), "")
		},
		"a link to a key":      func() error { return mcpReadRefused(reads, none, []string{link + ":1"}, "", "") },
		"the config directory": func() error { return mcpReadRefused(reads, none, nil, filepath.Join(cfgDir, "config.toml"), "") },
		// C stops at the NUL: this is judged as one string and would be opened as ~/.netrc.
		"a NUL byte": func() error {
			return mcpReadRefused(reads, none, []string{filepath.Join(home, ".netrc") + "\x00.safetensors:1"}, "", "")
		},
		// Refused whether or not it exists: resolving it first would tell the caller which.
		"a hires model that does not exist": func() error {
			return mcpReadRefused(reads, none, nil, "", filepath.Join(home, ".ssh", "id_ed25519"))
		},
	} {
		wantRefused(t, name, call())
	}

	elsewhere := filepath.Join(t.TempDir(), "detail.safetensors")
	if err := mcpReadRefused(reads, none, []string{elsewhere + ":1"}, elsewhere, elsewhere); err != nil {
		t.Errorf("an ordinary file was refused: %v", err)
	}
}

// An installed name is resolved by the registry to the file it names; judging
// the name as a path would judge the server's working directory instead, and
// a runtime started inside ~/.claude would find every installed LoRA refused.
func TestMCPReadLeavesInstalledNamesToTheRegistry(t *testing.T) {
	home, _ := guardHome(t)
	_, _, reads := mcpGuards()
	protected := filepath.Join(home, ".claude")
	if err := os.MkdirAll(protected, 0o700); err != nil {
		t.Fatal(err)
	}
	saved, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(protected); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(saved) })
	installed := func(n string) bool {
		return n == "detail-tweaker" || n == "controlnet-canny-sd15" || n == "4x-ultrasharp"
	}
	if err := mcpReadRefused(reads, installed, []string{"detail-tweaker:0.7"}, "controlnet-canny-sd15", "4x-ultrasharp"); err != nil {
		t.Errorf("installed names were refused: %v", err)
	}
	if err := mcpReadRefused(reads, installed, []string{"not-installed:1"}, "", ""); err == nil {
		t.Error("a name the registry does not know was not judged as the path it is (~/.claude/not-installed)")
	}
}

// The server's own wiring: the work directory refuses the data, models and
// config directories; the workspace manager judges the directory a call
// actually uses; the read guard admits the models directory, where LoRAs are
// read from.
func TestTheMCPGuardsAreWiredAsTheServerUsesThem(t *testing.T) {
	home, cfgDir := guardHome(t)
	models := filepath.Join(t.TempDir(), "relocated-models")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	store.SetModelsDir(models)
	t.Cleanup(func() { store.SetModelsDir("") })
	data := store.Home()
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	wd, ws, reads := mcpGuards()
	var te *toolerr.Error
	for name, dir := range map[string]string{"data": data, "models": models, "config": cfgDir} {
		if _, err := wd.Validate(dir); !errors.As(err, &te) || te.Code != toolerr.CodeWorkDirDenied {
			t.Errorf("Validate(%s directory) = %v, want %s", name, err, toolerr.CodeWorkDirDenied)
		}
	}
	cfg := filepath.Join(home, ".config")
	if _, err := ws.EnsureUnder(cfg, "gh"); !errors.As(err, &te) || te.Code != toolerr.CodeWorkDirDenied {
		t.Errorf("EnsureUnder(~/.config, gh) = %v, want %s", err, toolerr.CodeWorkDirDenied)
	}
	lora := filepath.Join(models, "style.safetensors")
	if why := reads.LocalPath(lora, lora); why != "" {
		t.Errorf("a LoRA in the models directory was refused: %s", why)
	}
}

// The renderer judges the raw paths before anything is loaded: a refused one
// returns path_not_allowed without reaching the engine (re is nil here).
func TestTheRendererJudgesRawPathsBeforeTheEngine(t *testing.T) {
	home, _ := guardHome(t)
	reg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	modelFile := filepath.Join(t.TempDir(), "m.gguf")
	write(t, modelFile)
	reg.Add(store.InstalledModel{Name: "m", Path: modelFile})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	_, _, reads := mcpGuards()
	r := &residentRenderer{reads: reads}
	_, err = r.Render(context.Background(), tools.RenderRequest{
		Model: "m", LoRAs: []string{filepath.Join(home, ".ssh", "id_rsa") + ":1"},
	}, nil)
	wantRefused(t, "Render with a LoRA in ~/.ssh", err)
	// Judged before hires_model is resolved: resolving stats it, and "not a
	// file" versus "refused" would say whether the key exists.
	_, err = r.Render(context.Background(), tools.RenderRequest{
		Model: "m", HiresModel: filepath.Join(home, ".ssh", "id_ed25519"),
	}, nil)
	wantRefused(t, "Render with a missing hires model in ~/.ssh", err)
	var zero workdir.Resolver
	r = &residentRenderer{reads: zero}
	_, err = r.Render(context.Background(), tools.RenderRequest{
		Model: "m", ControlNet: filepath.Join(t.TempDir(), "cn.safetensors"),
	}, nil)
	wantRefused(t, "Render with an unset read guard", err)
}

// A config file set with IMAGE_FORGE_CONFIG outside image-forge's own
// directory protects the file, not the directory holding it — ~/image-forge.toml
// must not make the home directory a server directory. The legacy file in the
// data directory is protected too.
func TestAConfigFileProtectsItselfNotTheDirectoryItSitsIn(t *testing.T) {
	home, _ := guardHome(t)
	file := filepath.Join(home, "image-forge.toml")
	write(t, file)
	t.Setenv("IMAGE_FORGE_CONFIG", file)
	wd, _, reads := mcpGuards()
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := wd.Validate(project); err != nil {
		t.Errorf("a work directory beside the config file was refused: %v", err)
	}
	none := func(string) bool { return false }
	wantRefused(t, "the config file as a LoRA", mcpReadRefused(reads, none, []string{file + ":1"}, "", ""))
	legacy := filepath.Join(store.Home(), "config.toml")
	write(t, legacy)
	wantRefused(t, "the legacy config file as a ControlNet", mcpReadRefused(reads, none, nil, legacy, ""))
	lora := filepath.Join(home, "loras", "style.safetensors")
	if err := mcpReadRefused(reads, none, []string{lora + ":1"}, "", ""); err != nil {
		t.Errorf("a LoRA in the home directory was refused: %v", err)
	}
}

// A relative IMAGE_FORGE_CONFIG is judged where the loader reads it, from the
// working directory, and protects that file only; and a config file kept in a
// directory that merely happens to be named image-forge does not make that
// directory a server directory.
func TestARelativeOrSourceCheckoutConfigProtectsOnlyTheFile(t *testing.T) {
	home, _ := guardHome(t)
	checkout := filepath.Join(home, "src", "image-forge")
	file := filepath.Join(checkout, "config.toml")
	write(t, file)
	saved, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(checkout); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(saved) })
	t.Setenv("IMAGE_FORGE_CONFIG", "config.toml")
	wd, _, reads := mcpGuards()
	none := func(string) bool { return false }
	wantRefused(t, "the relative config file, named absolutely", mcpReadRefused(reads, none, nil, file, ""))
	lora := filepath.Join(checkout, "lora.safetensors")
	if err := mcpReadRefused(reads, none, []string{lora + ":1"}, "", ""); err != nil {
		t.Errorf("a file beside the config file in a checkout was refused: %v", err)
	}
	if _, err := wd.Validate(checkout); err != nil {
		t.Errorf("the checkout holding the config file was refused as a work_dir: %v", err)
	}
}
