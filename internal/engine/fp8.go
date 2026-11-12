package engine

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// FP8 weights on Metal.
//
// sd.cpp keeps a safetensors FP8 tensor (dtype F8_E4M3 / F8_E5M2) at one byte
// per element and expects each backend either to multiply it directly or to
// say that it cannot, in which case sd.cpp casts the active layer for the
// duration of the call. The Metal backend of the ggml it vendors answers "yes"
// to a matrix product with an FP8 weight and has no kernel for it, so the first
// such product aborts the process ("kernel not found ... kernel_mul_mm_f8_e4m3_f32").
// Every FLUX and SD3.5 render reaches it through t5xxl_fp8_e4m3fn.
//
// Until the backend is fixed, the weights of an FP8 file are converted to F16
// as they are read — what sd.cpp itself did for these files before it gained
// the one-byte path, so memory use is what it was. The conversion is requested
// per component, by the name prefix sd.cpp gives that component's tensors: a
// catch-all rule would also expand the quantized GGUF components loaded beside
// it.

// fp8Components pairs each weight file of p with the tensor-name prefixes
// sd.cpp assigns to the tensors of that file.
func fp8Components(p OpenParams) []struct{ path, pattern string } {
	return []struct{ path, pattern string }{
		{p.T5XXL, `^text_encoders\.t5xxl\.`},
		{p.ClipL, `^text_encoders\.clip_l\.`},
		{p.ClipG, `^text_encoders\.clip_g\.`},
		{p.LLM, `^text_encoders\.llm\.`},
		{p.DiffusionModel, `^model\.diffusion_model\.`},
		// A single-file checkpoint carries its diffusion model and its text
		// encoders under these prefixes. When separate encoder files are also
		// given they share the text_encoders prefix and are converted with it;
		// that costs memory, never correctness.
		{p.ModelPath, `^model\.diffusion_model\.`},
		{p.ModelPath, `^text_encoders\.`},
		{p.ModelPath, `^cond_stage_model\.`},
		{p.ModelPath, `^conditioner\.`},
	}
}

// fp8TypeRules returns the sd.cpp tensor-type rules ("pattern=type,...") that
// convert the FP8 components of p to F16 at load, or "" when none is FP8.
// hasFP8 reports whether a file holds FP8 tensors; it is a parameter so the
// rule building is testable without model files.
func fp8TypeRules(p OpenParams, hasFP8 func(path string) bool) string {
	var rules []string
	seen := map[string]bool{}
	fp8 := map[string]bool{}
	for _, c := range fp8Components(p) {
		if c.path == "" {
			continue
		}
		is, ok := fp8[c.path]
		if !ok {
			is = hasFP8(c.path)
			fp8[c.path] = is
		}
		if !is || seen[c.pattern] {
			continue
		}
		seen[c.pattern] = true
		rules = append(rules, c.pattern+"=f16")
	}
	return strings.Join(rules, ",")
}

// maxSafetensorsHeader bounds the header read. Real headers are a few MB at
// most; a larger length means the file is not a safetensors file.
const maxSafetensorsHeader = 256 << 20

// fileHasFP8 reports whether path is a safetensors file with at least one FP8
// tensor. Only the JSON header is read. Anything that is not a readable
// safetensors file (a GGUF, a missing file) is "no": sd.cpp will report on it.
func fileHasFP8(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	return headerHasFP8(f)
}

func headerHasFP8(r io.Reader) bool {
	var n uint64
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil || n == 0 || n > maxSafetensorsHeader {
		return false
	}
	var header map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r, int64(n))).Decode(&header); err != nil {
		return false
	}
	for name, raw := range header {
		if name == "__metadata__" {
			continue
		}
		var t struct {
			DType string `json:"dtype"`
		}
		if json.Unmarshal(raw, &t) == nil && (t.DType == "F8_E4M3" || t.DType == "F8_E5M2") {
			return true
		}
	}
	return false
}
