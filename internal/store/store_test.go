package store

import (
	"testing"

	"github.com/nlink-jp/image-forge/internal/profile"
)

func TestRegistryRoundTrip(t *testing.T) {
	t.Setenv("IMAGE_FORGE_HOME", t.TempDir())

	r, err := Load()
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if len(r.Models) != 0 {
		t.Fatalf("fresh registry should be empty, got %d", len(r.Models))
	}

	m := InstalledModel{
		Name:    "animagine-xl-4",
		Path:    "/models/animagine.gguf",
		Profile: profile.ArchDefaults(profile.ArchSDXL),
		Rating:  profile.RatingQuestionable,
		License: "Fair AI Public License 1.0-SD",
	}
	r.Add(m)
	if err := r.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	r2, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := r2.Get("animagine-xl-4")
	if !ok {
		t.Fatal("model not found after reload")
	}
	if got.Path != m.Path || got.Profile.ClipSkip != 2 {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	if !r2.Remove("animagine-xl-4") {
		t.Error("remove should report true")
	}
	if r2.Remove("animagine-xl-4") {
		t.Error("second remove should report false")
	}
}
