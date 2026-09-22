BINARY    := slack-router
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.buildDate=$(BUILD_DATE)

GO_BUILD := go build -trimpath -ldflags "$(LDFLAGS)"

# macOS Developer ID signing / notarization. See nlink-jp/.github
# CONVENTIONS.md §Code Signing. slack-router is the org's only
# project that bundles scripts/ into the release zip (the routed
# command samples live there), so build-time helpers live in
# build-tools/ instead of scripts/ to keep them out of the
# distribution. Both files are verbatim copies of the templates
# in nlink-jp/.github/templates/.
CODESIGN_IDENTITY ?= Developer ID Application
NOTARY_PROFILE    ?= nlink-jp-notary

# Files bundled into each release zip alongside the binary.
# Adjust this list if you add more files worth shipping.
# build-tools/ is intentionally NOT here — it holds the
# codesign/notarize helpers, which are build-time only.
BUNDLE_FILES := README.md LICENSE CHANGELOG.md config.example.yaml .env.example docs scripts

# darwin ships arm64 only (no amd64, no universal). linux keeps its matrix;
# slack-router (a daemon) has no windows build.
PLATFORMS := \
	darwin/arm64 \
	linux/amd64  \
	linux/arm64

.DEFAULT_GOAL := help

# ─── primary targets ────────────────────────────────────────────────────────

.PHONY: build
build: ## Build for the current platform
	@mkdir -p dist
	$(GO_BUILD) -o dist/$(BINARY) .
	@build-tools/codesign-darwin.sh dist/$(BINARY) "$(CODESIGN_IDENTITY)"

.PHONY: release
release: ## Cross-compile for all platforms, sign, package, notarize darwin → dist/
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		name="$(BINARY)-$(VERSION)-$$os-$$arch"; \
		stagedir="dist/$$name"; \
		printf "  %-52s" "$$name ..."; \
		mkdir -p "$$stagedir"; \
		GOOS=$$os GOARCH=$$arch $(GO_BUILD) -o "$$stagedir/$(BINARY)" . \
			|| { echo "FAILED"; rm -rf "$$stagedir"; exit 1; }; \
		build-tools/codesign-darwin.sh "$$stagedir/$(BINARY)" "$(CODESIGN_IDENTITY)" || true; \
		cp -r $(BUNDLE_FILES) "$$stagedir/"; \
		if [ "$$os" = linux ]; then \
			( cd dist && COPYFILE_DISABLE=1 tar --no-xattrs -czf "$$name.tar.gz" "$$name/" ); archive="dist/$$name.tar.gz"; \
		else \
			( cd dist && zip -qr "$$name.zip" "$$name/" ); archive="dist/$$name.zip"; \
		fi; \
		rm -rf "$$stagedir"; \
		echo "ok  →  $$archive"; \
	done
	@build-tools/notarize-darwin.sh dist/$(BINARY)-$(VERSION)-darwin-arm64.zip "$(NOTARY_PROFILE)"
	@echo ""
	@echo "Artifacts:"
	@ls -lh dist/*.zip dist/*.tar.gz 2>/dev/null

.PHONY: verify-release
verify-release: ## Refuse to release an un-notarized zip (marker gate)
	@test -f "dist/$(BINARY)-$(VERSION)-darwin-arm64.zip.notarized" || { \
		echo "verify-release: FAIL — $(BINARY)-$(VERSION)-darwin-arm64.zip has no notarization marker."; \
		echo "  make package must end with '[notarize] ...: Accepted'. Do not upload this zip."; \
		exit 1; }
	@test "dist/$(BINARY)-$(VERSION)-darwin-arm64.zip.notarized" -nt "dist/$(BINARY)-$(VERSION)-darwin-arm64.zip" || { \
		echo "verify-release: FAIL — the zip was rebuilt after its marker (re-run make package)."; \
		exit 1; }
	@tmp=$$(mktemp -d); rc=0; \
		if ! unzip -oq "dist/$(BINARY)-$(VERSION)-darwin-arm64.zip" -d "$$tmp"; then \
			echo "verify-release: FAIL — the zip does not unpack. Do not upload it."; rc=1; \
		elif ! out=$$("$$tmp/$(BINARY)-$(VERSION)-darwin-arm64/$(BINARY)" --version 2>&1); then \
			echo "verify-release: FAIL — the packaged binary does not run:"; \
			echo "  $$out"; rc=1; \
		elif ! printf '%s\n' "$$out" | grep -qF "$(VERSION)"; then \
			echo "verify-release: FAIL — the packaged binary reports \"$$out\", not $(VERSION)."; \
			echo "  The zip holds a build from another tag (re-run make package)."; rc=1; \
		else \
			echo "  $$out"; \
			spctl -a -vv -t install "$$tmp/$(BINARY)-$(VERSION)-darwin-arm64/$(BINARY)" 2>&1 | head -2 || true; \
		fi; \
		rm -rf "$$tmp"; \
		exit $$rc
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		[ "$$os" = linux ] || continue; \
		name="$(BINARY)-$(VERSION)-$$os-$$arch"; f="dist/$$name.tar.gz"; \
		names=$$(tar --options 'tar:!mac-ext' -tzf "$$f") || { echo "verify-release: FAIL — $$f does not list."; exit 1; }; \
		if printf '%s\n' "$$names" | grep -qE '(^|/)(\._|PaxHeader|__MACOSX)'; then \
			echo "verify-release: FAIL — $$f carries macOS metadata entries."; \
			echo "  macOS tar writes ._ members unless COPYFILE_DISABLE=1 is set, and lists them only with !mac-ext."; \
			exit 1; fi; \
		xh=$$(python3 -c 'import sys, tarfile; print(" ".join(m.name for m in tarfile.open(sys.argv[1]) if any(k.startswith(("LIBARCHIVE.xattr.", "SCHILY.xattr.")) for k in m.pax_headers)))' "$$f") || { \
			echo "verify-release: FAIL — $$f cannot be read for its pax headers (python3 tarfile)."; exit 1; }; \
		if [ -n "$$xh" ]; then \
			echo "verify-release: FAIL — $$f carries extended attributes as pax headers ($$xh)."; \
			echo "  macOS tar writes them unless called with --no-xattrs; COPYFILE_DISABLE alone does not."; \
			exit 1; fi; \
		if printf '%s\n' "$$names" | grep -qvE "^$$name(/|$$)"; then \
			echo "verify-release: FAIL — $$f holds entries outside $$name/."; exit 1; fi; \
		got=$$(printf '%s\n' "$$names" | sed -E "s|^$$name/?||; s|/.*||" | grep -v '^$$' | LC_ALL=C sort -u | tr '\n' ' '); \
		want=$$(printf '%s\n' "$(BINARY)" $(BUNDLE_FILES) | LC_ALL=C sort | tr '\n' ' '); \
		if [ "$$got" != "$$want" ]; then \
			echo "verify-release: FAIL — $$f holds $$got under $$name/; expected $$want"; exit 1; fi; \
	done
	@echo "verify-release: OK ($(VERSION), notarized, unpacks, runs, reports its version, clean linux archives)"

.PHONY: package
## package: Alias for release — build all platforms and create .zip archives
package: release

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf dist/

# ─── development ────────────────────────────────────────────────────────────

.PHONY: run
run: build ## Build and run with config.yaml
	./dist/$(BINARY) -config config.yaml

.PHONY: test
test: ## Run tests with race detector
	go test -race ./...

.PHONY: lint
lint: ## Run go vet and staticcheck
	go vet ./...
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed (go install honnef.co/go/tools/cmd/staticcheck@latest)"

.PHONY: tidy
tidy: ## Tidy go modules
	go mod tidy

# ─── release helpers ────────────────────────────────────────────────────────

.PHONY: version
version: ## Print the current version (from git describe)
	@echo $(VERSION)

.PHONY: tag
tag: ## Create and push a release tag  (usage: make tag VERSION=v0.2.0)
ifndef VERSION
	$(error VERSION is not set. Usage: make tag VERSION=v0.x.y)
endif
	git tag -a $(VERSION) -m "Release $(VERSION)"
	@echo "Created tag $(VERSION). Push with: git push origin $(VERSION)"

# ─── help ───────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show available targets
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Current version: $(VERSION)"
