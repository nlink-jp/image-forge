BINARY  := image-forge
DIST    := dist
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

# The real diffusion runtime (stable-diffusion.cpp + ggml + Metal) is linked in
# only under the `cgo_sdcpp` build tag. `make build-engine` builds the sd.cpp
# static libraries first (via `deps`, which needs cmake + the Metal Toolchain),
# then links them into a single Go binary. Without the tag, `make build`
# produces a binary whose engine returns ErrNoRuntime — useful for scaffold work
# and toolchain-less CI.

SD_DIR   := third_party/stable-diffusion.cpp
SD_BUILD := $(SD_DIR)/build
SD_LIB   := $(SD_BUILD)/libstable-diffusion.a

# Exclude the vendored sd.cpp submodule from go tooling (it carries stray Go
# files, e.g. libwebp swig bindings, that are not part of this module).
PKGS := $(shell go list ./... 2>/dev/null | grep -v '/third_party/')

.PHONY: build build-engine deps test fmt vet clean build-all

## build: scaffold binary (no diffusion runtime)
build:
	@mkdir -p $(DIST)
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

## build-engine: full binary with the statically-linked sd.cpp runtime
build-engine: deps
	@mkdir -p $(DIST)
	CGO_ENABLED=1 go build -tags cgo_sdcpp -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

## deps: build stable-diffusion.cpp into static libraries (Metal backend)
deps: $(SD_LIB)

$(SD_LIB):
	cmake -S $(SD_DIR) -B $(SD_BUILD) -DCMAKE_BUILD_TYPE=Release \
		-DGGML_METAL=ON -DSD_BUILD_SHARED_LIBS=OFF -DSD_BUILD_EXAMPLES=OFF
	cmake --build $(SD_BUILD) --config Release -j

test:
	go test $(PKGS)

fmt:
	go fmt $(PKGS)

vet:
	go vet $(PKGS)

clean:
	rm -rf $(DIST)

## clean-deps: remove the sd.cpp build tree
clean-deps:
	rm -rf $(SD_BUILD)

## build-all: multi-arch signed release artifacts (Phase 3)
build-all:
	@echo "multi-arch release build recipe TBD (Phase 3)"
