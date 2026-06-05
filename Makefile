.PHONY: build build-embed build-nocgo test test-nocgo run dev clean web-build build-all dev-setup

# Binary name
BINARY=labubu

# Version from git or fallback
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

LDFLAGS=-ldflags "-X main.Version=$(VERSION)"

# Build Go binary (requires web/dist to exist for frontend embedding)
build: web-build
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/labubu

# Alias for build (explicit name for embedded frontend)
build-embed: build

# Build without CGO (for linting/analysis — no embed, no storage CGO)
build-nocgo:
	CGO_ENABLED=0 go build -tags "nocgo dev" -o /dev/null ./cmd/labubu

# Run all tests
test:
	go test -v ./internal/... ./web/... ./cmd/...

# Run tests excluding chDB integration tests
test-nocgo:
	go test -v -tags nocgo ./internal/...

# Run with dev mode (reads frontend from disk, no embed)
run:
	go run -tags dev ./cmd/labubu serve

# Start Vite dev server for frontend development
dev:
	cd web && npm run dev

# Build Vue frontend
web-build:
	cd web && npm run build

# Build Vue + Go binary together
build-all: build

# Clean build artifacts
clean:
	rm -rf bin/ web/dist/

# Install dev dependencies
dev-setup:
	cd web && npm install
