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
