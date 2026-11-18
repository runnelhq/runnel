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

.PHONY: all build build-windows build-linux dist test lint fmt tidy clean

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

## test: run all tests with the race detector
test:
	go test -race ./...

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
	rm -rf bin
