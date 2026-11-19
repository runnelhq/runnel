# Runnel build tasks. On Windows without make, use the plain go commands from README.md.

BINARY  := runnel
PKG     := github.com/runnelhq/runnel
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

# Pinned tool versions (installed on demand into bin/tools/<version>/).
CYCLONEDX_GOMOD_VERSION := v1.12.0
CYCLONEDX_NPM_VERSION   := 6.0.1
GOVULNCHECK_VERSION     := v1.9.0
CYCLONEDX_GOMOD := bin/tools/$(CYCLONEDX_GOMOD_VERSION)/cyclonedx-gomod
GOVULNCHECK     := bin/tools/$(GOVULNCHECK_VERSION)/govulncheck

.PHONY: all web web-deps build build-go build-windows build-linux dist notices sbom package test e2e fuzz vulncheck lint fmt tidy clean

all: lint test build

## web-deps: install the editor's npm packages (exact versions from web/package-lock.json)
web-deps:
	cd web && npm ci --no-audit --no-fund

## web: build the editor into internal/webui/dist (embedded into the binary)
web: web-deps
	cd web && npm run build

## build: build the editor and the binary for the current platform into bin/
build: web build-go

## build-go: build only the binary (embeds whatever internal/webui/dist contains)
build-go:
	$(GOBUILD) -o bin/$(BINARY)$(shell go env GOEXE) ./cmd/runnel

## build-windows: cross-compile bin/runnel-windows-amd64.exe (run "make web" first)
build-windows:
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY)-windows-amd64.exe ./cmd/runnel

## build-linux: cross-compile bin/runnel-linux-amd64 (run "make web" first)
build-linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY)-linux-amd64 ./cmd/runnel

## dist: build the editor and all release targets
dist: web
	$(MAKE) build-windows build-linux

## notices: write license texts of compiled-in dependencies to bin/THIRD_PARTY_NOTICES.txt
notices:
	mkdir -p bin
	./scripts/third-party-notices.sh > bin/THIRD_PARTY_NOTICES.txt

## sbom: write CycloneDX SBOMs (JSON) for each release target to bin/, including the editor
sbom: $(CYCLONEDX_GOMOD)
	GOOS=windows GOARCH=amd64 $(CYCLONEDX_GOMOD) app -json -licenses -assert-licenses -std \
		-main cmd/runnel -output bin/runnel-windows-amd64.cdx.json .
	GOOS=linux GOARCH=amd64 $(CYCLONEDX_GOMOD) app -json -licenses -assert-licenses -std \
		-main cmd/runnel -output bin/runnel-linux-amd64.cdx.json .
	cd web && npx --yes @cyclonedx/cyclonedx-npm@$(CYCLONEDX_NPM_VERSION) --omit dev \
		--output-format JSON --spec-version 1.6 --output-file ../bin/web.cdx.json
	node scripts/merge-sbom.mjs bin/runnel-windows-amd64.cdx.json bin/web.cdx.json
	node scripts/merge-sbom.mjs bin/runnel-linux-amd64.cdx.json bin/web.cdx.json

## package: build release archives (zip/tar.gz), SBOMs and SHA256SUMS into dist/
package: dist notices sbom
	./scripts/package.sh $(VERSION)

## test: run all Go tests with the race detector
test:
	go test -race ./...

## e2e: build editor and binary, then run the editor's end-to-end tests (Playwright)
e2e: build
	cd web && npx playwright test

## fuzz: run the fuzz tests for FUZZTIME each (default 30s)
FUZZTIME ?= 30s
fuzz:
	go test ./internal/vars -run '^$$' -fuzz FuzzParseExpand -fuzztime $(FUZZTIME)
	go test ./internal/model -run '^$$' -fuzz FuzzParseWorkflow -fuzztime $(FUZZTIME)

## vulncheck: report known vulnerabilities in dependencies and the Go standard library
vulncheck: $(GOVULNCHECK)
	$(GOVULNCHECK) ./...

$(CYCLONEDX_GOMOD):
	GOBIN=$(CURDIR)/$(dir $@) go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@$(CYCLONEDX_GOMOD_VERSION)

$(GOVULNCHECK):
	GOBIN=$(CURDIR)/$(dir $@) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

## lint: run golangci-lint (includes go vet and formatting checks)
lint:
	golangci-lint run ./...

## fmt: format code
fmt:
	golangci-lint fmt ./...

## tidy: tidy go.mod/go.sum
tidy:
	go mod tidy

## clean: remove build output
clean:
	rm -rf bin dist web/test-results web/playwright-report
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
