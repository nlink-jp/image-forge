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

.PHONY: build build-engine build-all package verify-release deps test fmt vet clean clean-deps

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
	@scripts/codesign-darwin.sh $(DIST)/$(BINARY) "$(CODESIGN_IDENTITY)" "$(BINARY)"

## package: signed + notarized release zip
## (image-forge-v<version>-darwin-arm64.zip; canonical binary + README.md
## + LICENSE inside, per the org Release Archive Standard).
package: build-all
	@cd $(DIST) && cp ../README.md ../LICENSE . \
		&& zip -j $(BINARY)-$(VERSION)-darwin-arm64.zip $(BINARY) README.md LICENSE \
		&& rm -f README.md LICENSE
	@scripts/notarize-darwin.sh $(DIST)/$(BINARY)-$(VERSION)-darwin-arm64.zip "$(NOTARY_PROFILE)"

## verify-release: refuse to release an un-notarized zip (marker gate)
## verify-release: refuse to release a zip that is un-notarized, stale, does
## not unpack, does not run, or holds a build from another tag. Every step
## fails closed; only the spctl line is informational.
verify-release:
	@test -f "$(DIST)/$(BINARY)-$(VERSION)-darwin-arm64.zip.notarized" || { \
		echo "verify-release: FAIL — $(BINARY)-$(VERSION)-darwin-arm64.zip has no notarization marker."; \
		echo "  make package must end with '[notarize] ...: Accepted'. Do not upload this zip."; \
		exit 1; }
	@test "$(DIST)/$(BINARY)-$(VERSION)-darwin-arm64.zip.notarized" -nt "$(DIST)/$(BINARY)-$(VERSION)-darwin-arm64.zip" || { \
		echo "verify-release: FAIL — the zip was rebuilt after its marker (re-run make package)."; \
		exit 1; }
	@tmp=$$(mktemp -d); rc=0; \
		if ! unzip -oq "$(DIST)/$(BINARY)-$(VERSION)-darwin-arm64.zip" -d "$$tmp"; then \
			echo "verify-release: FAIL — the zip does not unpack. Do not upload it."; rc=1; \
		elif ! out=$$("$$tmp/$(BINARY)" --version 2>&1); then \
			echo "verify-release: FAIL — the packaged binary does not run:"; \
			echo "  $$out"; rc=1; \
		elif ! printf '%s\n' "$$out" | grep -qF "$(VERSION)"; then \
			echo "verify-release: FAIL — the packaged binary reports \"$$out\", not $(VERSION)."; \
			echo "  The zip holds a build from another tag (re-run make package)."; rc=1; \
		else \
			echo "  $$out"; \
			spctl -a -vv -t install "$$tmp/$(BINARY)" 2>&1 | head -2 || true; \
		fi; \
		rm -rf "$$tmp"; \
		exit $$rc
	@echo "verify-release: OK ($(VERSION), notarized, unpacks, runs, reports its version)"

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

# Homebrew tap generation (see scripts/release-brew.mk). After `make package`,
# `make brew` generates this formula from the built darwin-arm64 zip into the
# local nlink-jp/homebrew-tap checkout. The package target is unchanged.
BREW_KIND := formula
BREW_DESC := Local diffusion image-generation engine and model manager
include scripts/release-brew.mk
