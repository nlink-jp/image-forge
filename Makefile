BINARY  := image-forge
DIST    := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

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

# macOS Developer ID signing / notarization (see scripts/). Defaults match any
# Developer ID Application cert and the org-standard notary profile.
CODESIGN_IDENTITY ?= Developer ID Application
NOTARY_PROFILE    ?= nlink-jp-notary

.PHONY: build build-engine build-all package deps test fmt vet clean clean-deps

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

## build-all: release binary. This tool is CGO + Metal, so Apple Silicon
## (darwin/arm64) ONLY — cross-compilation is impossible (Metal has no
## Linux/Windows/amd64 target), a deliberate scope decision (see the RFP).
build-all: build-engine
	@scripts/codesign-darwin.sh $(DIST)/$(BINARY) "$(CODESIGN_IDENTITY)"

## package: signed + notarized release zip
## (image-forge-<version>-darwin-arm64.zip, canonical binary name inside).
package: build-all
	@cd $(DIST) && cp ../README.md . \
		&& zip -j $(BINARY)-$(VERSION)-darwin-arm64.zip $(BINARY) README.md \
		&& rm -f README.md
	@scripts/notarize-darwin.sh $(DIST)/$(BINARY)-$(VERSION)-darwin-arm64.zip "$(NOTARY_PROFILE)"

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
