package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nlink-jp/image-forge/internal/mcp/job"
)

// plantEngineTmpLink prepares a workspace whose engine temp output name is a
// symlink to a file outside the workspace, and returns the work directory and
// the victim path. The engine opens that name with plain os.Create (it cannot
// inherit os.Root), so a link left in place means the render lands outside.
func plantEngineTmpLink(t *testing.T, tmpName string) (root, victim string) {
	t.Helper()
	root = seedWorkspace(t, "proj")
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	victim = filepath.Join(outside, "victim.png")
	if err := os.WriteFile(victim, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(root, "proj", "output")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(outDir, tmpName)); err != nil {
		t.Fatal(err)
	}
	return root, victim
}

// TestGenerateDoesNotWriteThroughPlantedTmpLink: the temp render path is handed
// to the engine as an absolute path, so the workspace's os.Root cannot protect
// it. generate must therefore unlink whatever sits at that name *through* the
// root before handing the path out. Without that removal the fake renderer's
// os.WriteFile follows the link and the victim file outside the workspace reads
// "PNGDATA".
func TestGenerateDoesNotWriteThroughPlantedTmpLink(t *testing.T) {
	h := newHarness(t, &fakeRenderer{seed: 777})
	root, victim := plantEngineTmpLink(t, "gen.tmp.png")

	out, err := h.call("generate", map[string]any{
		"workspace_id": "proj",
		"work_dir":     root,
		"prompt":       "a cat",
		"model":        "sdxl",
	})
	if err == nil {
		st := h.pollDone(out.(map[string]any)["job_id"].(string))
		if st.State == job.StateError {
			t.Logf("job refused (acceptable): %v", st.Error)
		}
	} else {
		t.Logf("call refused (acceptable): %v", err)
	}

	assertVictimIntact(t, victim)
}

// TestUpscaleDoesNotWriteThroughPlantedTmpLink is the same containment check on
// the upscale path, which hands the engine its own absolute temp output path.
func TestUpscaleDoesNotWriteThroughPlantedTmpLink(t *testing.T) {
	h := newHarness(t, nil)
	root, victim := plantEngineTmpLink(t, "upscaled.tmp.png")
	if err := os.WriteFile(filepath.Join(root, "proj", "in.png"), []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := h.call("upscale", map[string]any{
		"workspace_id": "proj",
		"work_dir":     root,
		"input":        "in.png",
	})
	if err == nil {
		st := h.pollDone(out.(map[string]any)["job_id"].(string))
		if st.State == job.StateError {
			t.Logf("job refused (acceptable): %v", st.Error)
		}
	} else {
		t.Logf("call refused (acceptable): %v", err)
	}

	assertVictimIntact(t, victim)
}

func assertVictimIntact(t *testing.T, victim string) {
	t.Helper()
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("victim file outside the workspace is unreadable: %v", err)
	}
	if string(got) != "ORIGINAL" {
		t.Errorf("engine wrote through the planted link: victim = %q, want %q", got, "ORIGINAL")
	}
}
