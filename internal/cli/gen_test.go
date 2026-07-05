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
