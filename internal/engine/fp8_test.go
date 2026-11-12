package engine

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func safetensors(t *testing.T, header string) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := binary.Write(&b, binary.LittleEndian, uint64(len(header))); err != nil {
		t.Fatal(err)
	}
	b.WriteString(header)
	return b.Bytes()
}

func TestHeaderHasFP8(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"e4m3", `{"__metadata__":{"format":"pt"},"a.weight":{"dtype":"F8_E4M3","shape":[2,2],"data_offsets":[0,4]}}`, true},
		{"e5m2", `{"a.weight":{"dtype":"F8_E5M2","shape":[2,2],"data_offsets":[0,4]}}`, true},
		{"mixed", `{"a.bias":{"dtype":"F16","shape":[2],"data_offsets":[0,4]},"a.weight":{"dtype":"F8_E4M3","shape":[2,2],"data_offsets":[4,8]}}`, true},
		{"f16 only", `{"a.weight":{"dtype":"F16","shape":[2,2],"data_offsets":[0,8]}}`, false},
		// A metadata value that merely mentions the type is not a tensor.
		{"metadata mention", `{"__metadata__":{"note":"F8_E4M3"},"a.weight":{"dtype":"BF16","shape":[2,2],"data_offsets":[0,8]}}`, false},
	}
	for _, c := range cases {
		if got := headerHasFP8(bytes.NewReader(safetensors(t, c.header))); got != c.want {
			t.Errorf("%s: headerHasFP8 = %v, want %v", c.name, got, c.want)
		}
	}
}

// A file that is not safetensors must read as "no FP8", never as an error or a
// huge allocation: GGUF components pass through this check on every load.
func TestHeaderHasFP8_NotSafetensors(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":       nil,
		"gguf magic":  append([]byte("GGUF"), make([]byte, 32)...),
		"huge length": {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f},
		"truncated":   safetensors(t, `{"a.weight":{"dtype":"F8_E4M3"`),
		"zero length": make([]byte, 8),
	} {
		if headerHasFP8(bytes.NewReader(data)) {
			t.Errorf("%s: reported FP8", name)
		}
	}
}

func TestFileHasFP8(t *testing.T) {
	dir := t.TempDir()
	fp8 := filepath.Join(dir, "t5.safetensors")
	if err := os.WriteFile(fp8, safetensors(t, `{"w":{"dtype":"F8_E4M3","shape":[1],"data_offsets":[0,1]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !fileHasFP8(fp8) {
		t.Error("FP8 file not detected")
	}
	if fileHasFP8(filepath.Join(dir, "missing.safetensors")) {
		t.Error("a missing file reported FP8")
	}
}

func TestFP8TypeRules(t *testing.T) {
	isFP8 := func(set ...string) func(string) bool {
		return func(p string) bool { return slices.Contains(set, p) }
	}
	// FLUX as the catalog ships it: a quantized GGUF DiT, an F16 CLIP-L and the
	// FP8 T5. Only the T5 may be converted — a rule that reached the DiT would
	// expand its Q4 weights to F16.
	flux := OpenParams{DiffusionModel: "flux-q4.gguf", ClipL: "clip_l.safetensors", T5XXL: "t5_fp8.safetensors", VAEPath: "ae.safetensors"}
	if got, want := fp8TypeRules(flux, isFP8("t5_fp8.safetensors")), `^text_encoders\.t5xxl\.=f16`; got != want {
		t.Errorf("flux rules = %q, want %q", got, want)
	}
	if got := fp8TypeRules(flux, isFP8()); got != "" {
		t.Errorf("no FP8 component, rules = %q, want none", got)
	}
	// Each component is addressed by its own prefix.
	all := OpenParams{DiffusionModel: "d", ClipL: "l", ClipG: "g", T5XXL: "t", LLM: "m"}
	want := `^text_encoders\.t5xxl\.=f16,^text_encoders\.clip_l\.=f16,^text_encoders\.clip_g\.=f16,^text_encoders\.llm\.=f16,^model\.diffusion_model\.=f16`
	if got := fp8TypeRules(all, isFP8("d", "l", "g", "t", "m")); got != want {
		t.Errorf("all rules = %q\nwant        %q", got, want)
	}
	// A single-file FP8 checkpoint: its diffusion model and its encoders, and
	// no pattern twice. The VAE is never addressed (an F16 SDXL VAE renders black).
	single := OpenParams{ModelPath: "ckpt_fp8.safetensors", VAEPath: "vae.safetensors"}
	got := fp8TypeRules(single, isFP8("ckpt_fp8.safetensors", "vae.safetensors"))
	want = `^model\.diffusion_model\.=f16,^text_encoders\.=f16,^cond_stage_model\.=f16,^conditioner\.=f16`
	if got != want {
		t.Errorf("single-file rules = %q\nwant                %q", got, want)
	}
	// The file is inspected once however many prefixes it carries.
	calls := 0
	fp8TypeRules(single, func(string) bool { calls++; return true })
	if calls != 1 {
		t.Errorf("checkpoint inspected %d times, want 1", calls)
	}
}
