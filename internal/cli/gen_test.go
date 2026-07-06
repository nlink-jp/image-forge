package cli

import "testing"

func TestParseLoras(t *testing.T) {
	got, err := parseLoras([]string{"style.safetensors:0.8", "detail"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d loras, want 2", len(got))
	}
	if got[0].Path != "style.safetensors" || got[0].Weight != 0.8 {
		t.Errorf("lora[0] = %+v, want {style.safetensors 0.8}", got[0])
	}
	if got[1].Path != "detail" || got[1].Weight != 1.0 {
		t.Errorf("lora[1] = %+v, want {detail 1}", got[1])
	}
}

func TestParseLoras_BadWeight(t *testing.T) {
	if _, err := parseLoras([]string{"x:notanumber"}); err == nil {
		t.Fatal("expected error for non-numeric weight")
	}
}

func TestSeededOutput(t *testing.T) {
	if got := seededOutput("out.png", 42, 1); got != "out.png" {
		t.Errorf("count 1 should be unchanged: %q", got)
	}
	if got := seededOutput("a/b/pic.png", 7, 3); got != "a/b/pic-7.png" {
		t.Errorf("count>1: %q", got)
	}
	if got := seededOutput("noext", 5, 2); got != "noext-5.png" {
		t.Errorf("missing ext: %q", got)
	}
}

func TestResolveSeed(t *testing.T) {
	if got := resolveSeed(42); got != 42 {
		t.Errorf("fixed seed changed: %d", got)
	}
	a, b := resolveSeed(-1), resolveSeed(-1)
	if a < 0 || b < 0 {
		t.Error("random seed must be non-negative")
	}
	if a == b {
		t.Error("two random seeds should differ")
	}
}
