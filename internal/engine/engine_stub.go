//go:build !cgo_sdcpp

package engine

// Open reports ErrNoRuntime in toolchain-less builds so scaffold work and CI stay
// green without cmake/Metal.
func Open(p OpenParams) (Session, error) {
	return nil, ErrNoRuntime
}

// Quantize reports ErrNoRuntime in toolchain-less builds.
func Quantize(inputPath, vaePath, outputPath, quantType string) error {
	return ErrNoRuntime
}

// Upscale reports ErrNoRuntime in toolchain-less builds.
func Upscale(p UpscaleParams) error {
	return ErrNoRuntime
}

// Info reports that no diffusion runtime is linked into this build.
func Info() string { return "engine: none (built without cgo_sdcpp)" }

// Samplers / Schedulers have no runtime to reflect in a toolchain-less build.
func Samplers() []string   { return nil }
func Schedulers() []string { return nil }

// ValidSampler / ValidScheduler are permissive without a runtime (nothing to
// validate against, and this build cannot render anyway).
func ValidSampler(string) bool   { return true }
func ValidScheduler(string) bool { return true }

// QuantTypes / ValidWType: the scaffold build has no quantTypes map; mirror the
// canonical list so CLI help/validation still works without the sd.cpp runtime.
func QuantTypes() []string {
	return []string{"f16", "f32", "q2_k", "q3_k", "q4_0", "q4_1", "q4_k", "q5_0", "q5_1", "q5_k", "q6_k", "q8_0"}
}

func ValidWType(name string) bool {
	for _, t := range QuantTypes() {
		if t == name {
			return true
		}
	}
	return false
}
