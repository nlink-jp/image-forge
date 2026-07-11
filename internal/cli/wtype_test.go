package cli

import (
	"testing"

	"github.com/nlink-jp/image-forge/internal/engine"
)

func TestValidateWType(t *testing.T) {
	if err := validateWType(""); err != nil {
		t.Errorf("empty wtype should be allowed (keep original): %v", err)
	}
	if err := validateWType("q4_k"); err != nil {
		t.Errorf("q4_k should be valid: %v", err)
	}
	if err := validateWType("bogus"); err == nil {
		t.Error("an unknown wtype should be rejected")
	}
}

// wtype changes the loaded weights, so it must be part of the model's reload
// identity — two otherwise-identical loads at different wtypes must not share a
// resident session.
func TestReloadKeyIncludesWType(t *testing.T) {
	base := engine.OpenParams{ModelPath: "/m/x.safetensors"}
	q4 := base
	q4.WType = "q4_k"
	q8 := base
	q8.WType = "q8_0"
	if reloadKey(base) == reloadKey(q4) {
		t.Error("wtype q4_k should change the reload key")
	}
	if reloadKey(q4) == reloadKey(q8) {
		t.Error("different wtypes should have different reload keys")
	}
	// FlashAttn, by contrast, is deliberately NOT part of the identity.
	fa := base
	fa.FlashAttn = true
	if reloadKey(base) != reloadKey(fa) {
		t.Error("FlashAttn should not change the reload key")
	}
}
