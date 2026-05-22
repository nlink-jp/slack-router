BINARY    := slack-router
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.buildDate=$(BUILD_DATE) \
           -s -w

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
BUNDLE_FILES := README.md CHANGELOG.md config.example.yaml .env.example docs scripts

PLATFORMS := \
	darwin/amd64 \
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
		zipfile="dist/$$name.zip"; \
		printf "  %-52s" "$$name ..."; \
		mkdir -p "$$stagedir"; \
		GOOS=$$os GOARCH=$$arch $(GO_BUILD) -o "$$stagedir/$(BINARY)" . \
			|| { echo "FAILED"; rm -rf "$$stagedir"; exit 1; }; \
		build-tools/codesign-darwin.sh "$$stagedir/$(BINARY)" "$(CODESIGN_IDENTITY)" || true; \
		cp -r $(BUNDLE_FILES) "$$stagedir/"; \
		cd dist && zip -qr "$$name.zip" "$$name/" && cd ..; \
		rm -rf "$$stagedir"; \
		echo "ok  →  $$zipfile"; \
	done
	@build-tools/notarize-darwin.sh dist/$(BINARY)-$(VERSION)-darwin-amd64.zip "$(NOTARY_PROFILE)"
	@build-tools/notarize-darwin.sh dist/$(BINARY)-$(VERSION)-darwin-arm64.zip "$(NOTARY_PROFILE)"
	@echo ""
	@echo "Artifacts:"
	@ls -lh dist/*.zip

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
