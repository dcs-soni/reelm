BINARY_NAME=reelm
MODULE=github.com/dcs-soni/reelm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0-dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS=-ldflags "-s -w \
	-X $(MODULE)/pkg/version.Version=$(VERSION) \
	-X $(MODULE)/pkg/version.GitCommit=$(COMMIT) \
	-X $(MODULE)/pkg/version.BuildDate=$(BUILD_DATE)"

.PHONY: all build clean test test-unit test-integration test-all test-coverage lint run tidy

all: lint test build

build:
	@echo "==> Building $(BINARY_NAME)..."
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/reelm

clean:
	@echo "==> Cleaning build artifacts..."
	rm -rf bin/ dist/ coverage.*

# Fast unit tests only (tier 1: in-package unit tests)
test: test-unit

test-unit:
	@echo "==> Running fast unit tests (tier 1)..."
	go test -race -cover ./pkg/...

# Full integration tests (tier 2: proxy + upstream mock + disk store)
test-integration:
	@echo "==> Running integration tests (tier 2)..."
	go test -tags=integration -race -v ./test/integration/...

# Complete test suite
test-all: test-unit test-integration

test-coverage:
	@echo "==> Generating coverage profile..."
	go test -race -coverprofile=coverage.out ./pkg/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report saved to coverage.html"

lint:
	@echo "==> Running golangci-lint..."
	golangci-lint run ./... || true

tidy:
	@echo "==> Tidying dependencies..."
	go mod tidy

run: build
	./bin/$(BINARY_NAME) serve --config configs/default.yaml
