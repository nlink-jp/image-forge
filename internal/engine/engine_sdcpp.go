//go:build cgo_sdcpp

package engine

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/stable-diffusion.cpp/include
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/libstable-diffusion.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/libggml.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/libggml-cpu.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/ggml-metal/libggml-metal.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/ggml-blas/libggml-blas.a
#cgo LDFLAGS: ${SRCDIR}/../../third_party/stable-diffusion.cpp/build/ggml/src/libggml-base.a
#cgo LDFLAGS: -lc++ -framework Foundation -framework Metal -framework MetalKit -framework Accelerate
#include <stable-diffusion.h>
*/
import "C"

import (
	"context"
	"errors"
)

// Info returns build/system info from the linked stable-diffusion.cpp runtime.
// It is the build bring-up spike's proof that Go <-> C <-> ggml/Metal links and
// runs (no model required).
func Info() string {
	return "engine: stable-diffusion.cpp — " + C.GoString(C.sd_get_system_info())
}

// New returns the sd.cpp-backed engine.
func New() (Engine, error) {
	return &sdEngine{}, nil
}

type sdEngine struct{}

// Generate is not implemented yet — the spike proves linking; the real
// new_sd_ctx/generate_image wiring lands next in Phase 1.
func (e *sdEngine) Generate(ctx context.Context, req Request, events chan<- Event) error {
	return errors.New("sdcpp: Generate not implemented yet (build spike proves linking only)")
}

func (e *sdEngine) Close() error { return nil }
