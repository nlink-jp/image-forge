//go:build !cgo_sdcpp

package engine

import "errors"

// ErrNoRuntime is returned when the binary was built without the diffusion
// runtime (i.e. without the cgo_sdcpp build tag).
var ErrNoRuntime = errors.New("this build has no diffusion runtime: build with -tags cgo_sdcpp (requires cmake + Metal Toolchain + the sd.cpp submodule)")

// New returns the runtime engine. In toolchain-less builds it reports ErrNoRuntime
// so scaffold work and CI stay green without cmake/Metal.
func New() (Engine, error) {
	return nil, ErrNoRuntime
}

// Info reports that no diffusion runtime is linked into this build.
func Info() string { return "engine: none (built without cgo_sdcpp)" }
