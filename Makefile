MODULE     := github.com/nag-sh/agentbox
BINARY     := agentbox
INIT_BIN   := agentbox-init
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -s -w \
              -X $(MODULE)/pkg/version.Version=$(VERSION) \
              -X $(MODULE)/pkg/version.Commit=$(COMMIT) \
              -X $(MODULE)/pkg/version.BuildDate=$(BUILD_DATE)

GO      := go
GOFLAGS := -trimpath
GOTEST  := $(GO) test

.PHONY: all build build-init install test lint clean fmt vet tidy

all: build build-init

## build: Build the agentbox CLI binary
build:
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/agentbox

## build-init: Build the container init binary (statically linked for containers)
build-init:
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(INIT_BIN) ./library/os/base/agentbox-init

## install: Install the agentbox CLI to $GOPATH/bin
install:
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' ./cmd/agentbox

## test: Run all tests
test:
	$(GOTEST) -race -cover -coverprofile=coverage.txt ./...

## test-integration: Run integration tests
test-integration:
	$(GOTEST) -race -tags=integration ./integration/...

## lint: Run linters
lint:
	golangci-lint run ./...

## fmt: Format all Go files
fmt:
	gofmt -s -w .
	goimports -w .

## vet: Run go vet
vet:
	$(GO) vet ./...

## tidy: Tidy go modules
tidy:
	$(GO) mod tidy

## clean: Remove build artifacts
clean:
	rm -rf bin/ dist/ coverage.txt coverage.html

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
