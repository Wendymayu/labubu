# Pip Packaging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Distribute Labubu as a pip-installable package so users can `pip install labubu` then `labubu serve` to start a local observability server.

**Architecture:** Pre-compile the Go binary (CGO_ENABLED=0, in-memory storage, frontend embedded via `go:embed`) per platform, package each into a platform-specific Python wheel, upload to PyPI. A thin Python CLI shell locates the bundled Go binary and delegates all commands to it via `subprocess.run`.

**Tech Stack:** Go 1.19 (`go:embed`), Python 3.8+ (`subprocess`, `pyproject.toml`), setuptools, GitHub Actions

---

### Task 1: Frontend Embedding

Embed the Vue frontend into the Go binary using `go:embed` for production builds, while keeping disk-based file serving for development mode.

**Files:**
- Create: `web/embed.go`
- Create: `web/embed_dev.go`
- Create: `web/embed_test.go`
- Create: `web/dist/.gitkeep`
- Modify: `internal/api/router.go:68-124`

**Context:** The Go module is `github.com/labubu/labubu`. Currently, `router.go` serves the Vue SPA from `web/dist` on disk via `http.ServeFile`. The new approach exposes a `StaticFS` variable from the `web` package that is either an `embed.FS` (production) or `os.DirFS` (development), both as `fs.FS`.

- [ ] **Step 1: Create `web/dist/.gitkeep`**

Ensure the `web/dist/` directory exists in git so `go:embed dist` never fails due to a missing directory:

```bash
mkdir -p web/dist
touch web/dist/.gitkeep
```

- [ ] **Step 2: Create `web/embed.go`**

```go
//go:build !dev

package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// StaticFS serves the embedded frontend files.
var StaticFS fs.FS = distFS
```

- [ ] **Step 3: Create `web/embed_dev.go`**

```go
//go:build dev

package web

import (
	"io/fs"
	"os"
)

// StaticFS reads from disk in dev mode so npm run dev works.
var StaticFS fs.FS = os.DirFS("web/dist")
```

- [ ] **Step 4: Create `web/embed_test.go`**

```go
//go:build !dev

package web

import (
	"io/fs"
	"testing"
)

func TestStaticFSContainsIndexHTML(t *testing.T) {
	f, err := StaticFS.Open("dist/index.html")
	if err != nil {
		t.Fatalf("Open dist/index.html: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat dist/index.html: %v", err)
	}
	if info.IsDir() {
		t.Fatal("dist/index.html is a directory, expected a file")
	}
	if info.Size() == 0 {
		t.Fatal("dist/index.html is empty")
	}
}

func TestStaticFSCanListDist(t *testing.T) {
	entries, err := fs.ReadDir(StaticFS, "dist")
	if err != nil {
		t.Fatalf("ReadDir dist: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("dist directory is empty in embedded FS")
	}
}
```

- [ ] **Step 5: Build the frontend and run the embed test**

```bash
cd web && npm run build && cd ..
go test ./web/...
```

Expected: PASS (both tests pass, `dist/index.html` is found in embedded FS)

- [ ] **Step 6: Modify `router.go` to use `web.StaticFS`**

Replace the disk-based SPA serving block in `internal/api/router.go`. The current code (lines 68-124) uses `os.Stat` and `http.ServeFile` on `web/dist`. Replace it with:

Add import to the existing import block:

```go
import (
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/labubu/labubu/web"
)
```

Replace the SPA serving section (from `distPath := filepath.Join(...)` through the end of `NewRouter`) with:

```go
	// Serve Vue SPA from embedded or disk-based FS.
	spaFS, err := fs.Sub(web.StaticFS, "dist")
	if err == nil {
		if _, err := fs.Stat(spaFS, "index.html"); err == nil {
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				serveSPA(spaFS, w, r)
			})
			return mux
		}
	}

	// Fallback: frontend not built yet (dev mode without dist).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(devFallbackHTML))
	})

	return mux
}
```

Add the `serveSPA` helper function after `NewRouter`:

```go
// serveSPA serves a single-page app from an fs.FS.
// Static files are served directly; all other paths fall back to index.html
// for client-side routing.
func serveSPA(fsys fs.FS, w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Try serving the requested file.
	f, err := fsys.Open(path)
	if err == nil {
		stat, statErr := f.Stat()
		f.Close()
		if statErr == nil && !stat.IsDir() {
			http.ServeFileFS(w, r, fsys, path)
			return
		}
	}

	// Fallback to index.html for SPA client-side routing.
	http.ServeFileFS(w, r, fsys, "index.html")
}
```

Remove the now-unused `spaHandler` struct and its `ServeHTTP` method (old lines 87-112). Keep the `devFallbackHTML` constant unchanged.

Remove the unused `"path/filepath"` import (it was only used for `filepath.Join("web", "dist")`).

- [ ] **Step 7: Verify the build and all tests pass**

```bash
go build ./cmd/labubu
go test ./internal/... ./web/...
```

Expected: Build succeeds, all tests pass.

- [ ] **Step 8: Verify dev mode still works**

```bash
go build -tags dev ./cmd/labubu
```

Expected: Build succeeds (uses `embed_dev.go`, no embed directive evaluated).

- [ ] **Step 9: Commit**

```bash
git add web/embed.go web/embed_dev.go web/embed_test.go web/dist/.gitkeep internal/api/router.go
git commit -m "feat: embed frontend into Go binary with dev/prod build tags"
```

---

### Task 2: Subcommand Parsing (TDD)

Add `serve`, `version`, and `help` subcommands to the Go binary. This task focuses on the dispatch logic and tests for `version` and `help`. The `serve` implementation stays identical to current behavior in this task.

**Files:**
- Create: `cmd/labubu/main_test.go`
- Modify: `cmd/labubu/main.go`

**Context:** Currently `main.go` uses `flag.Parse()` at the top level with no subcommand dispatch. The refactoring extracts subcommand handling into testable functions while keeping the existing server startup logic intact.

- [ ] **Step 1: Write failing tests for subcommand parsing**

Create `cmd/labubu/main_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestVersionSubcommand(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	Version = "0.1.0-test"
	os.Args = []string{"labubu", "version"}
	run()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "0.1.0-test") {
		t.Errorf("version output missing version: got %q", output)
	}
}

func TestHelpSubcommand(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	os.Args = []string{"labubu", "help"}
	run()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "Usage:") {
		t.Errorf("help output missing 'Usage:': got %q", output)
	}
	if !strings.Contains(output, "serve") {
		t.Errorf("help output missing 'serve': got %q", output)
	}
}

func TestNoArgsReturnsExitCode1(t *testing.T) {
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	os.Args = []string{"labubu"}
	code := run()

	wErr.Close()
	os.Stderr = oldStderr

	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}

	var bufErr bytes.Buffer
	bufErr.ReadFrom(rErr)
	if !strings.Contains(bufErr.String(), "Usage:") {
		t.Errorf("no-args stderr missing 'Usage:': got %q", bufErr.String())
	}
}

func TestUnknownCommandReturnsExitCode1(t *testing.T) {
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	os.Args = []string{"labubu", "foobar"}
	code := run()

	wErr.Close()
	os.Stderr = oldStderr

	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}

	var bufErr bytes.Buffer
	bufErr.ReadFrom(rErr)
	if !strings.Contains(bufErr.String(), "Unknown command") {
		t.Errorf("unknown cmd stderr missing 'Unknown command': got %q", bufErr.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./cmd/labubu/... -run "TestVersionSubcommand|TestHelpSubcommand|TestNoArgsReturnsExitCode1|TestUnknownCommandReturnsExitCode1" -v
```

Expected: FAIL — current `main.go` has no `run()` function; compile error.

- [ ] **Step 3: Implement subcommand dispatch in `main.go`**

Replace the entire `cmd/labubu/main.go` with:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/labubu/labubu/internal/api"
	ilog "github.com/labubu/labubu/internal/log"
	"github.com/labubu/labubu/internal/metrics"
	"github.com/labubu/labubu/internal/pipeline"
	"github.com/labubu/labubu/internal/receiver"
	"github.com/labubu/labubu/internal/storage"
)

// Version is set at build time via ldflags.
var Version = "dev"

func main() {
	os.Exit(run())
}

// run dispatches subcommands and returns an exit code.
// Separated from main() so tests can call it without os.Exit.
func run() int {
	if len(os.Args) < 2 {
		printUsage()
		return 1
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
		return 0
	case "version":
		fmt.Printf("labubu %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		return 0
	case "help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: labubu <command> [options]

Commands:
  serve     Start the Labubu server (OTLP receiver + API + UI)
  version   Print version information
  help      Show this help message

Run "labubu serve --help" for serve options.
`)
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)

	port := fs.Int("port", 8080, "API and UI listen port")
	dataDir := fs.String("data-dir", "", "data directory (empty = in-memory)")
	bufferSize := fs.Int("buffer-size", 1000, "pipeline buffer capacity")
	flushInterval := fs.Duration("flush-interval", 200*time.Millisecond, "pipeline flush interval")

	metricsEnabled := fs.Bool("metrics-enabled", true, "enable/disable metrics ingestion")
	metricsDataDir := fs.String("metrics-data-dir", "", "tstorage data directory (empty = pure memory)")
	metricsRetention := fs.Duration("metrics-retention", 2*time.Hour, "tstorage retention duration")

	logLevel := fs.String("log-level", "info", "log level: debug, info, warn, error")

	fs.Parse(args)

	apiAddr := fmt.Sprintf("0.0.0.0:%d", *port)

	// Check port availability before starting.
	if ln, err := net.Listen("tcp", apiAddr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: port %d is already in use.\nTry: labubu serve --port %d\n", *port, *port+1)
		os.Exit(1)
	} else {
		ln.Close()
	}

	// Set log level.
	lvl, err := ilog.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("Invalid log level: %v", err)
	}
	ilog.SetLevel(lvl)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Print startup banner.
	fmt.Printf("Labubu v%s starting...\n", Version)
	fmt.Printf("  OTLP gRPC:      http://localhost:4317\n")
	fmt.Printf("  OTLP HTTP:      http://localhost:4318\n")
	fmt.Printf("  API & UI:       http://localhost:%d\n", *port)
	fmt.Printf("  Storage:        in-memory (data lost on exit)\n")
	fmt.Println()

	// Initialize storage (in-memory for non-CGO builds).
	store, err := storage.NewChDBStore(*dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	// Initialize metrics store (if enabled).
	var metricStore metrics.Store
	if *metricsEnabled {
		ms, err := metrics.NewTStorageStore(metrics.TStorageConfig{
			DataDir:   *metricsDataDir,
			Retention: *metricsRetention,
		})
		if err != nil {
			log.Fatalf("Failed to initialize metrics store: %v", err)
		}
		defer ms.Close()
		metricStore = ms
	}

	// Initialize pipeline.
	pipe := pipeline.New(store, *bufferSize, *flushInterval)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := pipe.Shutdown(ctx); err != nil {
			log.Printf("Pipeline shutdown error: %v", err)
		}
	}()

	// Initialize OTLP receiver.
	recv := receiver.New(pipe, metricStore)
	if err := recv.Start(); err != nil {
		log.Fatalf("Failed to start OTLP receiver: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := recv.Shutdown(ctx); err != nil {
			log.Printf("Receiver shutdown error: %v", err)
		}
	}()

	// Initialize API router.
	traceHandler := api.NewTraceHandler(store)
	var metricsHandler *api.MetricsHandler
	if metricStore != nil {
		metricsHandler = api.NewMetricsHandler(metricStore)
	}
	dashboardHandler := api.NewDashboardHandler("")
	sessionHandler := api.NewSessionHandler(store)
	router := api.NewRouter(traceHandler, metricsHandler, dashboardHandler, sessionHandler)

	httpSrv := &http.Server{
		Addr:         apiAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start API server.
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server error: %v", err)
		}
	}()

	fmt.Println("Press Ctrl+C to stop.")

	// Wait for shutdown signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(ctx)

	log.Println("Labubu stopped.")
}
```

**Key changes from current `main.go`:**
- Added `Version` variable (set via ldflags)
- Extracted `run()` function that returns exit code (testable, no `os.Exit`)
- Added subcommand dispatch (`serve`/`version`/`help`) in `run()`
- Replaced `--api-addr` flag with `--port` flag
- Removed `--dashboards-dir` and `--metrics-prometheus-addr` flags (unused in pip release)
- Added port availability check before starting
- Added startup banner with OTLP/API URLs and storage mode
- Extracted `printUsage()` and `runServe()` helper functions

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./cmd/labubu/... -run "TestVersionSubcommand|TestHelpSubcommand|TestNoArgsReturnsExitCode1|TestUnknownCommandReturnsExitCode1" -v
```

Expected: PASS (all 4 tests pass)

- [ ] **Step 5: Run the binary manually to verify**

```bash
go run ./cmd/labubu version
go run ./cmd/labubu help
```

Expected:
```
labubu dev (linux/amd64)
```
```
Usage: labubu <command> [options]

Commands:
  serve     Start the Labubu server (OTLP receiver + API + UI)
  version   Print version information
  help      Show this help message

Run "labubu serve --help" for serve options.
```

- [ ] **Step 6: Verify all existing tests still pass**

```bash
go test -tags nocgo ./internal/...
```

Expected: PASS (no regressions in existing test suite)

- [ ] **Step 7: Commit**

```bash
git add cmd/labubu/main.go cmd/labubu/main_test.go
git commit -m "feat: add serve/version/help subcommands to CLI"
```

---

### Task 3: Makefile Updates

Update the Makefile to support the new subcommand structure, frontend embedding, and version injection.

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Update the Makefile**

Replace the entire `Makefile` with:

```makefile
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
```

- [ ] **Step 2: Verify `make build` works**

```bash
make build
```

Expected: Frontend builds, then Go binary builds with embedded frontend. `bin/labubu` exists.

- [ ] **Step 3: Verify the built binary**

```bash
./bin/labubu version
```

Expected: Prints version with git tag (or "dev" if no tags exist).

- [ ] **Step 4: Verify `make build-nocgo` works**

```bash
make build-nocgo
```

Expected: Build succeeds (uses `dev` tag to skip embed, `nocgo` tag to skip CGO storage).

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "feat: update Makefile for subcommands, embedding, and version injection"
```

---

### Task 4: Python Package

Create the thin Python package that wraps the Go binary.

**Files:**
- Create: `labubu-python/pyproject.toml`
- Create: `labubu-python/labubu/__init__.py`
- Create: `labubu-python/labubu/__main__.py`
- Create: `labubu-python/labubu/cli.py`
- Create: `labubu-python/README.md`

- [ ] **Step 1: Create directory structure**

```bash
mkdir -p labubu-python/labubu
```

- [ ] **Step 2: Create `labubu-python/pyproject.toml`**

```toml
[build-system]
requires = ["setuptools>=68.0"]
build-backend = "setuptools.build_meta"

[project]
name = "labubu"
version = "0.1.0"
description = "Local-first LLM observability platform"
requires-python = ">=3.8"

[project.scripts]
labubu = "labubu.cli:main"

[tool.setuptools.packages.find]
where = ["."]

[tool.setuptools.package-data]
labubu = ["bin/*"]
```

- [ ] **Step 3: Create `labubu-python/labubu/__init__.py`**

```python
"""Labubu — Local-first LLM observability platform."""

__version__ = "0.1.0"
```

- [ ] **Step 4: Create `labubu-python/labubu/__main__.py`**

```python
"""Allow running labubu as: python -m labubu"""
from labubu.cli import main

if __name__ == "__main__":
    main()
```

- [ ] **Step 5: Create `labubu-python/labubu/cli.py`**

```python
"""CLI entry point: locate and execute the bundled Go binary."""
import os
import subprocess
import sys


def _get_binary_path():
    """Return the path to the bundled Go binary."""
    pkg_dir = os.path.dirname(os.path.abspath(__file__))
    binary = os.path.join(pkg_dir, "bin", "labubu")
    if sys.platform == "win32":
        binary += ".exe"
    return binary


def main():
    """Locate the Go binary and execute it with all CLI arguments."""
    binary = _get_binary_path()

    if not os.path.isfile(binary):
        print(
            f"Error: labubu binary not found at {binary}",
            file=sys.stderr,
        )
        print(
            "Try reinstalling: pip install --force-reinstall labubu",
            file=sys.stderr,
        )
        sys.exit(1)

    result = subprocess.run([binary] + sys.argv[1:])
    sys.exit(result.returncode)
```

- [ ] **Step 6: Create `labubu-python/README.md`**

```markdown
# Labubu

Local-first LLM observability platform.

## Quick Start

```bash
pip install labubu
labubu serve
```

Then open [http://localhost:8080](http://localhost:8080) for the UI.

Configure your agent to send OTLP traces to `http://localhost:4318`.

## Commands

| Command | Description |
|---------|-------------|
| `labubu serve` | Start the server (default port 8080) |
| `labubu version` | Print version information |
| `labubu help` | Show help |

### Serve Options

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | 8080 | API & UI listen port |
| `--log-level` | info | Log level: debug, info, warn, error |
| `--buffer-size` | 1000 | Pipeline buffer capacity |
| `--flush-interval` | 200ms | Pipeline flush interval |

## Example

```bash
labubu serve --port 9090 --log-level debug
```
```

- [ ] **Step 7: Commit**

```bash
git add labubu-python/
git commit -m "feat: add Python package shell for pip distribution"
```

---

### Task 5: Python CLI Tests (TDD)

Write unit tests for the Python CLI shell, verifying binary location, argument passthrough, and error handling.

**Files:**
- Create: `labubu-python/tests/__init__.py`
- Create: `labubu-python/tests/test_cli.py`

- [ ] **Step 1: Create test directory**

```bash
mkdir -p labubu-python/tests
touch labubu-python/tests/__init__.py
```

- [ ] **Step 2: Write the tests**

Create `labubu-python/tests/test_cli.py`:

```python
import os
import subprocess
import sys
import unittest
from unittest.mock import patch, MagicMock

from labubu import cli


class TestGetBinaryPath(unittest.TestCase):
    """Test binary path resolution."""

    def test_unix_path(self):
        """On non-Windows, binary has no extension."""
        with patch.object(cli.sys, "platform", "linux"):
            path = cli._get_binary_path()
            self.assertTrue(path.endswith(os.path.join("bin", "labubu")))
            self.assertFalse(path.endswith(".exe"))

    def test_windows_path(self):
        """On Windows, binary has .exe extension."""
        with patch.object(cli.sys, "platform", "win32"):
            path = cli._get_binary_path()
            self.assertTrue(path.endswith(os.path.join("bin", "labubu.exe")))

    def test_path_is_absolute(self):
        """Binary path should be absolute."""
        path = cli._get_binary_path()
        self.assertTrue(os.path.isabs(path))


class TestMain(unittest.TestCase):
    """Test the main() CLI entry point."""

    @patch("labubu.cli.subprocess.run")
    @patch("labubu.cli.os.path.isfile", return_value=True)
    def test_passes_all_args_to_binary(self, mock_isfile, mock_run):
        """All CLI arguments after the program name are forwarded."""
        mock_run.return_value = MagicMock(returncode=0)

        with patch.object(cli.sys, "argv", ["labubu", "serve", "--port", "9090"]):
            cli.main()

        mock_run.assert_called_once()
        call_args = mock_run.call_args[0][0]
        # First arg is the binary path, rest are forwarded args.
        self.assertEqual(call_args[1:], ["serve", "--port", "9090"])

    @patch("labubu.cli.subprocess.run")
    @patch("labubu.cli.os.path.isfile", return_value=True)
    def test_exits_with_binary_return_code(self, mock_isfile, mock_run):
        """main() exits with the same return code as the Go binary."""
        mock_run.return_value = MagicMock(returncode=42)

        with patch.object(cli.sys, "argv", ["labubu", "version"]):
            with self.assertRaises(SystemExit) as cm:
                cli.main()
            self.assertEqual(cm.exception.code, 42)

    @patch("labubu.cli.subprocess.run")
    @patch("labubu.cli.os.path.isfile", return_value=True)
    def test_exits_zero_on_success(self, mock_isfile, mock_run):
        """main() exits with 0 when the Go binary succeeds."""
        mock_run.return_value = MagicMock(returncode=0)

        with patch.object(cli.sys, "argv", ["labubu", "help"]):
            with self.assertRaises(SystemExit) as cm:
                cli.main()
            self.assertEqual(cm.exception.code, 0)

    @patch("labubu.cli.os.path.isfile", return_value=False)
    def test_binary_not_found_exits_with_1(self, mock_isfile):
        """main() exits with 1 and prints error when binary is missing."""
        with patch.object(cli.sys, "argv", ["labubu", "serve"]):
            with self.assertRaises(SystemExit) as cm:
                cli.main()
            self.assertEqual(cm.exception.code, 1)

    @patch("labubu.cli.subprocess.run", side_effect=KeyboardInterrupt)
    @patch("labubu.cli.os.path.isfile", return_value=True)
    def test_keyboard_interrupt_propagates(self, mock_isfile, mock_run):
        """Ctrl+C (KeyboardInterrupt) propagates to the caller."""
        with patch.object(cli.sys, "argv", ["labubu", "serve"]):
            with self.assertRaises(KeyboardInterrupt):
                cli.main()


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 3: Run the tests**

```bash
cd labubu-python
python -m pytest tests/ -v
```

Expected: All 8 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add labubu-python/tests/
git commit -m "test: add Python CLI unit tests"
```

---

### Task 6: CI Workflow

Create the GitHub Actions release workflow that cross-compiles the Go binary per platform, packages platform-specific wheels, runs smoke tests, and uploads to PyPI.

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create the workflow directory**

```bash
mkdir -p .github/workflows
```

- [ ] **Step 2: Create `.github/workflows/release.yml`**

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'
  workflow_dispatch:
    inputs:
      version:
        description: 'Version (e.g. 0.1.0)'
        required: true

permissions:
  contents: read

jobs:
  build:
    strategy:
      matrix:
        include:
          - os: ubuntu-latest
            goos: linux
            goarch: amd64
            platform_tag: linux_x86_64
            binary_name: labubu
          - os: windows-latest
            goos: windows
            goarch: amd64
            platform_tag: win_amd64
            binary_name: labubu.exe
    runs-on: ${{ matrix.os }}

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'

      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
          cache-dependency-path: web/package-lock.json

      - name: Install frontend dependencies
        working-directory: web
        run: npm ci

      - name: Build frontend
        working-directory: web
        run: npm run build

      - name: Determine version
        shell: bash
        run: |
          if [ "${{ github.event_name }}" = "workflow_dispatch" ]; then
            echo "VERSION=${{ github.event.inputs.version }}" >> $GITHUB_ENV
          else
            echo "VERSION=${GITHUB_REF_NAME#v}" >> $GITHUB_ENV
          fi

      - name: Build Go binary
        shell: bash
        env:
          CGO_ENABLED: 0
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
        run: |
          go build -ldflags "-X main.Version=${{ env.VERSION }}" \
            -o "bin/${{ matrix.binary_name }}" ./cmd/labubu

      - name: Prepare Python package
        shell: bash
        run: |
          mkdir -p labubu-python/labubu/bin
          cp "bin/${{ matrix.binary_name }}" "labubu-python/labubu/bin/"

      - name: Set up Python
        uses: actions/setup-python@v5
        with:
          python-version: '3.12'

      - name: Install Python build tools
        run: pip install build wheel

      - name: Build wheel
        working-directory: labubu-python
        run: python -m build --wheel

      - name: Tag wheel for platform
        working-directory: labubu-python
        shell: bash
        run: |
          pip install wheel
          wheel tags --platform-tag ${{ matrix.platform_tag }} dist/*.whl
          # Remove the original any-tagged wheel if it still exists.
          rm -f dist/*-py3-none-any.whl

      - name: Smoke test
        shell: bash
        run: |
          pip install labubu-python/dist/*.whl
          labubu version
          # Start server in background, check health, then stop.
          labubu serve --port 19876 &
          SERVER_PID=$!
          sleep 3
          curl -sf http://localhost:19876/api/health
          echo ""
          kill $SERVER_PID 2>/dev/null || true
          wait $SERVER_PID 2>/dev/null || true

      - name: Upload wheel artifact
        uses: actions/upload-artifact@v4
        with:
          name: wheel-${{ matrix.goos }}-${{ matrix.goarch }}
          path: labubu-python/dist/*.whl

  publish:
    needs: build
    runs-on: ubuntu-latest
    if: startsWith(github.ref, 'refs/tags/v')

    steps:
      - uses: actions/download-artifact@v4
        with:
          path: dist
          merge-multiple: true

      - name: Publish to PyPI
        uses: pypa/gh-action-pypi-publish@release/v1
        with:
          password: ${{ secrets.PYPI_API_TOKEN }}
```

- [ ] **Step 3: Validate the YAML syntax**

```bash
python -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))"
```

Expected: No error (valid YAML). If `pyyaml` is not installed, install it first: `pip install pyyaml`.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add GitHub Actions release workflow for pip packaging"
```

---

### Task 7: Final Verification

Run the full build and all tests to verify everything works together end-to-end.

**Files:** none (verification only)

- [ ] **Step 1: Clean and rebuild everything**

```bash
make clean
make build
```

Expected: `bin/labubu` exists, `web/dist/` is populated.

- [ ] **Step 2: Run the full Go test suite**

```bash
make test
```

Expected: All tests pass (internal, web, cmd).

- [ ] **Step 3: Run the Python tests**

```bash
cd labubu-python && python -m pytest tests/ -v
```

Expected: All 8 tests pass.

- [ ] **Step 4: Build a local wheel and smoke test it**

```bash
# Copy the binary into the Python package.
mkdir -p labubu-python/labubu/bin
cp bin/labubu labubu-python/labubu/bin/

# Build the wheel.
cd labubu-python
pip install build wheel
python -m build --wheel

# Install and test.
pip install dist/*.whl --force-reinstall
labubu version
```

Expected: `labubu version` prints version info (e.g., `labubu dev (linux/amd64)`).

- [ ] **Step 5: Test `labubu serve` starts and responds**

```bash
labubu serve --port 19876 &
sleep 3
curl -sf http://localhost:19876/api/health
kill %1
```

Expected: `curl` returns `{"status":"ok"}`.

- [ ] **Step 6: Test `python -m labubu` works**

```bash
python -m labubu version
```

Expected: Same output as `labubu version`.

- [ ] **Step 7: Commit nothing (verification only)**

No new files to commit. All changes were committed in previous tasks.

- [ ] **Step 8: Final commit — update .gitignore**

Add the following to `.gitignore` (create if it doesn't exist):

```gitignore
# Build artifacts
bin/
web/dist/
!web/dist/.gitkeep

# Python
labubu-python/dist/
labubu-python/labubu/bin/
labubu-python/*.egg-info/
__pycache__/
*.pyc
```

```bash
git add .gitignore
git commit -m "chore: update .gitignore for pip packaging artifacts"
```
