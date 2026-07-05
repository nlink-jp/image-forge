BINARY  := image-forge
DIST    := dist
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

# The real diffusion runtime (stable-diffusion.cpp + ggml + Metal) is linked in
# only under the `cgo_sdcpp` build tag, which requires:
#   - cmake              (brew install cmake)
#   - Metal Toolchain    (xcodebuild -downloadComponent MetalToolchain)
#   - the sd.cpp submodule built into a static lib (see `make deps`)
# Without the tag, `make build` produces a binary whose engine returns a clear
# "no runtime" error — useful for scaffold work and toolchain-less CI.

.PHONY: build build-engine deps test fmt vet clean build-all

## build: scaffold binary (no diffusion runtime)
build:
	@mkdir -p $(DIST)
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

## build-engine: full binary with the statically-linked sd.cpp runtime
build-engine: deps
	@mkdir -p $(DIST)
	CGO_ENABLED=1 go build -tags cgo_sdcpp -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

## deps: build stable-diffusion.cpp into a static library (Metal backend)
deps:
	@echo "TODO (build spike): configure & build third_party/stable-diffusion.cpp"
	@echo "  cmake -B build -DGGML_METAL=ON -DSD_BUILD_SHARED_LIBS=OFF ..."

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf $(DIST)

## build-all: multi-arch signed release artifacts (Phase 3)
build-all:
	@echo "multi-arch release build recipe TBD (Phase 3)"
