//go:build !cgo_sdcpp

package engine

import "errors"

// ErrNoRuntime is returned when the binary was built without the diffusion
// runtime (i.e. without the cgo_sdcpp build tag).
var ErrNoRuntime = errors.New("this build has no diffusion runtime: build with -tags cgo_sdcpp (requires cmake + Metal Toolchain + the sd.cpp submodule)")

// Open reports ErrNoRuntime in toolchain-less builds so scaffold work and CI stay
// green without cmake/Metal.
func Open(p OpenParams) (Session, error) {
	return nil, ErrNoRuntime
}

// Quantize reports ErrNoRuntime in toolchain-less builds.
func Quantize(inputPath, vaePath, outputPath, quantType string) error {
	return ErrNoRuntime
}

// Info reports that no diffusion runtime is linked into this build.
func Info() string { return "engine: none (built without cgo_sdcpp)" }
