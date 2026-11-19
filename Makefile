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
GOVULNCHECK_VERSION     := v1.9.0
CYCLONEDX_GOMOD := bin/tools/$(CYCLONEDX_GOMOD_VERSION)/cyclonedx-gomod
GOVULNCHECK     := bin/tools/$(GOVULNCHECK_VERSION)/govulncheck

.PHONY: all build build-windows build-linux dist notices sbom package test fuzz vulncheck lint fmt tidy clean

all: lint test build

## build: build for the current platform into bin/
build:
	$(GOBUILD) -o bin/$(BINARY)$(shell go env GOEXE) ./cmd/runnel

## build-windows: cross-compile bin/runnel-windows-amd64.exe
build-windows:
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY)-windows-amd64.exe ./cmd/runnel

## build-linux: cross-compile bin/runnel-linux-amd64
build-linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY)-linux-amd64 ./cmd/runnel

## dist: build all release targets
dist: build-windows build-linux

## notices: write license texts of compiled-in dependencies to bin/THIRD_PARTY_NOTICES.txt
notices:
	mkdir -p bin
	./scripts/third-party-notices.sh > bin/THIRD_PARTY_NOTICES.txt

## sbom: write CycloneDX SBOMs (JSON) for each release target to bin/
sbom: $(CYCLONEDX_GOMOD)
	GOOS=windows GOARCH=amd64 $(CYCLONEDX_GOMOD) app -json -licenses -assert-licenses -std \
		-main cmd/runnel -output bin/runnel-windows-amd64.cdx.json .
	GOOS=linux GOARCH=amd64 $(CYCLONEDX_GOMOD) app -json -licenses -assert-licenses -std \
		-main cmd/runnel -output bin/runnel-linux-amd64.cdx.json .

## package: build release archives (zip/tar.gz), SBOMs and SHA256SUMS into dist/
package: dist notices sbom
	./scripts/package.sh $(VERSION)

## test: run all tests with the race detector
test:
	go test -race ./...

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
	rm -rf bin dist
