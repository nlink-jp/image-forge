package catalog

import (
	"testing"

	"github.com/nlink-jp/image-forge/internal/profile"
)

func TestDefault_NonEmptyAndNamed(t *testing.T) {
	entries := Default()
	if len(entries) == 0 {
		t.Fatal("default catalog is empty")
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Name == "" {
			t.Error("entry with empty name")
		}
		if seen[e.Name] {
			t.Errorf("duplicate entry name %q", e.Name)
		}
		seen[e.Name] = true
		if e.License == "" {
			t.Errorf("%s: license must be surfaced", e.Name)
		}
	}
}

func TestNeedsOptIn(t *testing.T) {
	cases := map[profile.Rating]bool{
		profile.RatingSafe:         false,
		profile.RatingQuestionable: true,
		profile.RatingExplicit:     true,
	}
	for rating, want := range cases {
		if got := (Entry{Rating: rating}).NeedsOptIn(); got != want {
			t.Errorf("rating %q: NeedsOptIn = %v, want %v", rating, got, want)
		}
	}
}

func TestProfilePropagatesPrediction(t *testing.T) {
	// The per-model prediction type must flow into the built profile — this is
	// what makes v-prediction models (e.g. NoobAI) render correctly.
	for _, e := range Default() {
		if e.Prediction != "" && e.Profile().Prediction != e.Prediction {
			t.Errorf("%s: profile prediction = %q, want %q", e.Name, e.Profile().Prediction, e.Prediction)
		}
	}
}

func TestFind(t *testing.T) {
	if _, ok := Find("flux1-schnell"); !ok {
		t.Error("expected to find flux1-schnell")
	}
	if _, ok := Find("does-not-exist"); ok {
		t.Error("did not expect to find a bogus entry")
	}
}
