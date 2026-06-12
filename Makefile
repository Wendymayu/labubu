.PHONY: build build-embed build-nocgo build-mem test test-nocgo run dev clean web-build build-all dev-setup wheel

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

# Build without CGO (uses SQLite Store by default)
build-nocgo:
	CGO_ENABLED=0 go build -tags "dev" -o /dev/null ./cmd/labubu

# Build without CGO and without SQLite (uses memStore, pure in-memory)
build-mem:
	CGO_ENABLED=0 go build -tags "nosqlite dev" -o /dev/null ./cmd/labubu

# Run all tests
test:
	go test -v ./internal/... ./web/... ./cmd/...

# Run tests (SQLite Store by default on non-CGO)
test-nocgo:
	go test -v ./internal/...

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

# Build Python wheel for current platform (requires Python 3.8+)
wheel: build
	@mkdir -p labubu-python/labubu/bin
	@cp bin/$(BINARY) labubu-python/labubu/bin/
	cd labubu-python && pip install build wheel && \
	  python -m build --wheel && \
	  whl_file=$$(ls dist/*.whl) && \
	  wheel unpack "$$whl_file" -d dist/unpacked && \
	  sed -i 's/Root-Is-Purelib: true/Root-Is-Purelib: false/' dist/unpacked/*/WHEEL 2>/dev/null || \
	    sed -i '' 's/Root-Is-Purelib: true/Root-Is-Purelib: false/' dist/unpacked/*/WHEEL && \
	  rm "$$whl_file" && \
	  wheel pack dist/unpacked/*/ -d dist/ && \
	  rm -rf dist/unpacked/ && \
	  PLATFORM_TAG=$$(python -c "import platform; m=platform.machine(); os_=platform.system().lower(); \
	    print('linux_x86_64' if os_=='linux' and m=='x86_64' else \
	          'linux_aarch64' if os_=='linux' and m=='aarch64' else \
	          'win_amd64' if os_=='windows' else \
	          'macosx_10_9_x86_64' if os_=='darwin' and m=='x86_64' else \
	          'macosx_11_0_arm64' if os_=='darwin' and m=='arm64' else \
	          'unknown')") && \
	  wheel tags --platform-tag $$PLATFORM_TAG dist/*.whl && \
	  rm -f dist/*-py3-none-any.whl
	@echo "Wheel built: labubu-python/dist/"
