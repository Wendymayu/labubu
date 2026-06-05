# Pip Packaging Design

**Date**: 2026-06-05
**Status**: designed, not yet planned

## Motivation

Labubu is a Go + Vue 3 observability platform. To make it easy for developers to install and run locally (like Arize Phoenix: `pip install arize-phoenix` → `phoenix serve`), we want to distribute Labubu as a pip package.

**User experience:**
```bash
pip install labubu
labubu serve
# OTLP receiver: http://localhost:4318
# API & UI:        http://localhost:8080
```

Target users are multi-language developers building AI agents. Pip is the first distribution channel; Docker, Homebrew, and other channels may follow later.

## Approach: Pre-compiled Platform Wheels

Same pattern used by `ruff`, `maturin`, and other Python-distributed Go/Rust binaries:

1. Use `go:embed` to embed the Vue frontend into the Go binary
2. CI cross-compiles a Go binary per platform (CGO_ENABLED=0, in-memory storage)
3. Each binary is packaged into a platform-specific Python wheel
4. Wheels are uploaded to PyPI
5. `pip install labubu` downloads the correct wheel automatically
6. A thin Python shell provides the `labubu` CLI entry point, delegating to the Go binary

## Architecture

```
PyPI (labubu)
├── labubu-0.1.0-py3-none-linux_x86_64.whl
│   ├── labubu/__init__.py
│   ├── labubu/__main__.py
│   ├── labubu/cli.py
│   └── labubu/bin/labubu          # Go binary (~10-15MB)
│
├── labubu-0.1.0-py3-none-win_amd64.whl
│   ├── labubu/__init__.py
│   ├── labubu/__main__.py
│   ├── labubu/cli.py
│   └── labubu/bin/labubu.exe      # Go binary (~10-15MB)
```

Python shell is minimal: locate the Go binary in `labubu/bin/`, then `os.execv()` to replace the process. All arguments pass through directly to Go.

## Target Platforms (Initial Release)

| Platform | Wheel Tag | Priority |
|----------|-----------|----------|
| Linux x86_64 | `linux_x86_64` | P0 |
| Windows x86_64 | `win_amd64` | P0 |

macOS support will be added in a later iteration.

## CGO Strategy

**Initial release: No CGO (in-memory storage)**

- Build with `CGO_ENABLED=0`
- The existing build tag (`//go:build !cgo || !local_engine`) automatically selects `memstore.go`
- No C compiler needed — cross-compilation is trivial
- In-memory storage is sufficient for local agent development (user runs agent, observes traces, data disappears on exit)
- Binary size: ~10-15MB

**Future: Add chDB persistence**

- Pre-compile libchdb static library per platform
- Link during CI build, produce CGO-enabled wheels
- Same Python package structure, just different binary

## Go Binary Changes

### Frontend Embedding

New file `web/embed.go` (production builds):

```go
//go:build !dev

package web

import "embed"

//go:embed dist/*
var StaticFS embed.FS
```

New file `web/embed_dev.go` (development builds):

```go
//go:build dev

package web

import "os"

// StaticFS reads from disk in dev mode so npm run dev works.
var StaticFS = os.DirFS("web/dist")
```

Modify `router.go` to serve SPA files from `web.StaticFS` instead of hardcoded filesystem paths. Default `go build` embeds the frontend; `go build -tags dev` reads from disk.

### Subcommand Structure

Refactor `cmd/labubu/main.go` from `flag`-only to subcommands:

```go
func main() {
    if len(os.Args) < 2 {
        printUsage()
        os.Exit(1)
    }
    switch os.Args[1] {
    case "serve":
        runServe(os.Args[2:])
    case "version":
        fmt.Printf("labubu %s\n", Version)
    case "help":
        printUsage()
    default:
        printUsage()
        os.Exit(1)
    }
}
```

### `serve` subcommand flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8080` | API & UI listen port |
| `--data-dir` | `./data` | Data directory (empty = in-memory) |
| `--log-level` | `info` | Log level: debug, info, warn, error |
| `--buffer-size` | `1000` | Pipeline buffer capacity |
| `--flush-interval` | `200ms` | Pipeline flush interval |
| `--metrics-enabled` | `true` | Enable/disable metrics ingestion |
| `--metrics-data-dir` | `./data/metrics` | tstorage data directory |
| `--metrics-retention` | `2h` | tstorage retention duration |

### Version injection

Compile-time injection via ldflags:

```bash
go build -ldflags "-X main.Version=0.1.0" -o bin/labubu ./cmd/labubu
```

The `version` subcommand prints: `labubu 0.1.0 (linux/amd64, storage=memory)`

## Python Package

### Directory structure

```
labubu-python/
├── pyproject.toml
├── README.md
├── LICENSE                  # Copy from project root
├── labubu/
│   ├── __init__.py          # Version string
│   ├── __main__.py          # python -m labubu support
│   └── cli.py               # Locate and exec Go binary
```

### `pyproject.toml`

```toml
[project]
name = "labubu"
version = "0.1.0"
description = "Local-first LLM observability platform"
requires-python = ">=3.8"

[project.scripts]
labubu = "labubu.cli:main"

[build-system]
requires = ["setuptools>=68.0"]
build-backend = "setuptools.build_meta"
```

### `cli.py`

```python
import os, sys

def main():
    bin_dir = os.path.join(os.path.dirname(__file__), "bin")
    binary = os.path.join(bin_dir, "labubu")
    if sys.platform == "win32":
        binary += ".exe"

    if not os.path.isfile(binary):
        print(f"Error: labubu binary not found at {binary}", file=sys.stderr)
        print("Try reinstalling: pip install --force-reinstall labubu", file=sys.stderr)
        sys.exit(1)

    os.execv(binary, [binary] + sys.argv[1:])
```

`os.execv` replaces the Python process with the Go binary, so signals (Ctrl+C) go directly to Go — no signal forwarding needed.

### Wheel tag

`py3-none-{platform}` — pure binary distribution, no Python version dependency. Generated in CI using `wheel tags --platform-tag`.

## CI/CD Pipeline (GitHub Actions)

### Trigger

- **Tag push:** `v0.1.0` triggers release build
- **Manual:** `gh workflow run release.yml -f version=0.1.0`

### Build matrix

```
release.yml
├── step 1: npm run build          → web/dist
├── step 2: matrix build (2 jobs)
│   ├── linux-x64   → CGO_ENABLED=0 go build → bin/labubu
│   └── windows-x64 → CGO_ENABLED=0 go build → bin/labubu.exe
├── step 3: package wheels
│   ├── copy binary → labubu-python/labubu/bin/
│   ├── python -m build --wheel
│   └── wheel tags --platform-tag {platform}
└── step 4: upload to PyPI
    └── twine upload dist/*.whl
```

### Smoke test (per platform, in CI)

After building each wheel:

```bash
pip install ./dist/labubu-*.whl
labubu version                         # verify binary runs
labubu serve --port 19876 &            # start on fixed test port
sleep 2
curl http://localhost:19876/api/health # verify API responds
kill %1
```

## User Experience

### Successful install and start

```bash
$ pip install labubu
Successfully installed labubu-0.1.0

$ labubu version
labubu 0.1.0 (linux/amd64, storage=memory)

$ labubu serve
Labubu v0.1.0 starting...
  OTLP receiver:  http://localhost:4318
  API & UI:       http://localhost:8080
  Storage:        in-memory (data lost on exit)

Press Ctrl+C to stop.
```

### Port conflict

```
Error: port 8080 is already in use.
Try: labubu serve --port 9090
```

### In-memory warning

Startup banner clearly states `Storage: in-memory (data lost on exit)` to prevent users from assuming data persists.

## Testing Strategy

| Layer | What to test |
|-------|-------------|
| Python | `cli.py` unit tests: binary location, argument passthrough, error handling |
| Go | Subcommand parsing tests (`serve`/`version`/`help`) |
| Go | `go:embed` verification: frontend files correctly embedded |
| CI | Per-platform smoke: `labubu version` + `labubu serve` + health check |
| CI | `pip install ./dist/labubu-*.whl && labubu version` — verify wheel integrity |

## Files to Create

| File | Purpose |
|------|---------|
| `web/embed.go` | `go:embed` for frontend static files (production) |
| `web/embed_dev.go` | Disk-based `StaticFS` for development (`-tags dev`) |
| `labubu-python/pyproject.toml` | Python package metadata and build config |
| `labubu-python/labubu/__init__.py` | Version string |
| `labubu-python/labubu/__main__.py` | `python -m labubu` entry point |
| `labubu-python/labubu/cli.py` | CLI entry: locate and exec Go binary |
| `labubu-python/README.md` | PyPI description |
| `.github/workflows/release.yml` | CI: build matrix + wheel packaging + PyPI upload |

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/labubu/main.go` | Add subcommand dispatch (`serve`/`version`/`help`) |
| `internal/api/router.go` | Serve SPA from `embed.FS` instead of disk (with dev fallback) |
| `Makefile` | Add `build-embed` target (builds with embedded frontend) |

## Files Unchanged

- All `internal/` packages except `api/router.go` — no changes
- `web/src/` — no frontend code changes
- All Go backend logic — no changes
- `docs/superpowers/` — existing specs unchanged

## Edge Cases

- **Binary not found in wheel:** `cli.py` prints clear error with reinstall suggestion
- **Wrong platform wheel:** pip's platform resolution prevents installing wrong wheel; if forced, binary won't run and error message directs user to reinstall
- **Signal handling:** `os.execv` replaces process entirely — Go handles SIGINT/SIGTERM natively
- **Python version:** `requires-python = ">=3.8"` — broad compatibility, `os.execv` available everywhere
- **Windows path separators:** `cli.py` uses `os.path.join` and appends `.exe` on win32
- **Missing `serve` arg:** Go binary prints usage and exits with code 1

## What Stays the Same

- Development workflow: `make run` + `npm run dev` still works (dev build tag reads from disk)
- API endpoints and OTLP protocol — unchanged
- Frontend code — unchanged
- Go test suite — unchanged
- Existing Makefile targets — still work
