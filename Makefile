.PHONY: build test run dev clean

# Binary name
BINARY=labubu

# Build the Go binary (with CGO enabled for chDB)
build:
	CGO_ENABLED=1 go build -o bin/$(BINARY) ./cmd/labubu

# Build without CGO (for linting/analysis — storage/chdb.go is excluded via build tags)
build-nocgo:
	CGO_ENABLED=0 go build -tags nocgo -o /dev/null ./cmd/labubu

# Run all tests
test:
	go test -v ./internal/...

# Run tests excluding chDB integration tests (requires libchdb)
test-nocgo:
	go test -v -tags nocgo ./internal/...

# Run with dev mode (requires Vite dev server separately)
run:
	go run ./cmd/labubu

# Build Vue frontend
web-build:
	cd web && npm run build

# Build Vue + Go binary together
build-all: web-build build

# Clean build artifacts
clean:
	rm -rf bin/ web/dist/

# Install dev dependencies
dev-setup:
	cd web && npm install
