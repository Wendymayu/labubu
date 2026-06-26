# Configurable OTLP Ports Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Labubu's two hardcoded OTLP listening ports (gRPC 4317, HTTP 4318) configurable via `serve` CLI flags, and make port conflicts fail fast instead of silently skipping the listener.

**Architecture:** Add two flags (`--otlp-grpc-port`, `--otlp-http-port`) to the `serve` command in `cmd/labubu/main.go`, pass them to `receiver.Start(grpcPort, httpPort int)`. Change `Start` to bind both listeners synchronously before launching serve goroutines, returning an error on conflict so `main.go` can `log.Fatalf`. Defaults stay 4317/4318 — existing behavior is unchanged.

**Tech Stack:** Go 1.19, stdlib `flag` + `net` + `net/http` + `google.golang.org/grpc`, Go table-driven tests.

---

## File Structure

- **Modify:** `internal/receiver/otlp.go` — change `Start()` to `Start(grpcPort, httpPort int) error`; move listener binding out of goroutines into the synchronous body so conflicts return an error.
- **Modify:** `cmd/labubu/main.go` — add two flags; pass them to `recv.Start(...)`; update the startup banner to print configured ports.
- **Modify:** `internal/receiver/otlp_test.go` — add `fmt`/`net` imports, a `freePort`/`dialCheck` helper, and two tests (`TestStartCustomPorts`, `TestStartFailFastOnConflict`).
- **Modify:** `README.md` — note that OTLP ports are configurable.
- **Modify:** `docs/deployment.md` — add the two flags to the parameter reference table and mention overriding them in `ExecStart`.

No new files. No new packages. No interface changes (`Store`, `Pipeline` untouched). The only signature change is `Receiver.Start`, whose sole caller is `main.go:163` (verified — no other production caller).

---

## Task 1: Configurable OTLP ports + fail-fast binding (TDD)

**Files:**
- Modify: `internal/receiver/otlp.go` (function `Start`, currently lines 49-98)
- Modify: `internal/receiver/otlp_test.go` (add imports + 2 tests + helper)
- Modify: `cmd/labubu/main.go` (flags near line 76; banner lines 112-113; call site line 163)

- [ ] **Step 1: Write the two failing tests + helper in `internal/receiver/otlp_test.go`**

First, extend the stdlib import block (currently lines 3-9) to add `fmt` and `net`:

```go
import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labubu/labubu/internal/pipeline"
	"github.com/labubu/labubu/internal/storage"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)
```

Then append these tests at the end of the file (after `TestTranslateSpanNoTokens`, ~line 429):

```go
// freePort returns a currently-free TCP port on localhost. The listener is
// closed before returning, so there is a small race window before the caller
// re-binds it; this is the standard technique for test port allocation and is
// acceptable for test reliability.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// dialCheck verifies that something is listening on the given localhost port.
func dialCheck(port int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

// TestStartCustomPorts verifies Start binds to the provided gRPC and HTTP ports
// (not the hardcoded 4317/4318) and that both listeners actually accept connections.
func TestStartCustomPorts(t *testing.T) {
	grpcPort := freePort(t)
	httpPort := freePort(t)

	store := &memTestStore{}
	r := New(nil, nil, store) // pipeline/metrics nil: we only test binding, not export
	if err := r.Start(grpcPort, httpPort); err != nil {
		t.Fatalf("Start(%d, %d): %v", grpcPort, httpPort, err)
	}
	defer r.Shutdown(context.Background())

	if err := dialCheck(grpcPort); err != nil {
		t.Errorf("gRPC port %d not listening: %v", grpcPort, err)
	}
	if err := dialCheck(httpPort); err != nil {
		t.Errorf("HTTP port %d not listening: %v", httpPort, err)
	}
}

// TestStartFailFastOnConflict verifies that a port conflict returns an error
// from Start (rather than silently starting a non-listening server).
func TestStartFailFastOnConflict(t *testing.T) {
	// Reserve a port on the same address Start binds (0.0.0.0) and keep it held
	// so Start's bind is an exact collision.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("reserve listener: %v", err)
	}
	defer ln.Close()
	conflictPort := ln.Addr().(*net.TCPAddr).Port
	httpPort := freePort(t)

	store := &memTestStore{}
	r := New(nil, nil, store)
	if err := r.Start(conflictPort, httpPort); err == nil {
		r.Shutdown(context.Background())
		t.Errorf("expected error when gRPC port %d is in use, got nil", conflictPort)
	}
}
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `go test ./internal/receiver/... -run TestStart -v`
Expected: COMPILE FAILURE — `too many arguments in call to r.Start` / `not enough arguments in call to r.Start`. This is the expected TDD red state: the tests exercise a `Start` signature that does not exist yet.

- [ ] **Step 3: Implement the new `Start` signature + synchronous fail-fast binding in `internal/receiver/otlp.go`**

Replace the entire `Start` function (currently lines 49-98, from `func (r *Receiver) Start() error {` through its closing `}` before `// Shutdown gracefully stops the receiver.`) with:

```go
// Start begins listening on the given gRPC and HTTP ports for OTLP data.
// Both listeners are bound synchronously so a port conflict fails fast and
// returns an error, rather than starting a server that silently does not listen.
func (r *Receiver) Start(grpcPort, httpPort int) error {
	// gRPC server.
	r.grpcSrv = grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(r.grpcSrv, &traceService{pipeline: r.pipeline})
	if r.metricStore != nil {
		colmetricspb.RegisterMetricsServiceServer(r.grpcSrv, &metricsService{metricStore: r.metricStore})
	}
	if r.store != nil {
		collogspb.RegisterLogsServiceServer(r.grpcSrv, &logsService{store: r.store})
	}

	// HTTP server for OTLP HTTP (/v1/traces, /v1/metrics, /v1/logs).
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", r.handleHTTPTraces)
	if r.metricStore != nil {
		mux.HandleFunc("/v1/metrics", r.handleHTTPMetrics)
	}
	if r.store != nil {
		mux.HandleFunc("/v1/logs", r.handleHTTPLogs)
	}
	r.httpSrv = &http.Server{Handler: mux}

	// Bind synchronously so port conflicts fail fast.
	grpcLis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", grpcPort))
	if err != nil {
		return fmt.Errorf("OTLP gRPC listen on :%d: %w", grpcPort, err)
	}
	httpLis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", httpPort))
	if err != nil {
		grpcLis.Close()
		return fmt.Errorf("OTLP HTTP listen on :%d: %w", httpPort, err)
	}

	// Serve in goroutines; listeners are already bound.
	go func() {
		fmt.Printf("OTLP gRPC listening on %s\n", grpcLis.Addr())
		if err := r.grpcSrv.Serve(grpcLis); err != nil {
			fmt.Printf("receiver: gRPC serve error: %v\n", err)
		}
	}()
	go func() {
		fmt.Printf("OTLP HTTP listening on %s\n", httpLis.Addr())
		if err := r.httpSrv.Serve(httpLis); err != nil && err != http.ErrServerClosed {
			fmt.Printf("receiver: HTTP serve error: %v\n", err)
		}
	}()

	return nil
}
```

Note: `fmt` and `net` are already imported in `otlp.go` (lines 7, 9). No new imports needed here.

- [ ] **Step 4: Wire the flags + call site + banner in `cmd/labubu/main.go`**

4a. Add the two flags immediately after the `--port` flag (after line 76). The block currently reads:

```go
	port := fs.Int("port", 8080, "API and UI listen port")
	dataDir := fs.String("data-dir", "data", "data directory for persistence")
```

Change it to:

```go
	port := fs.Int("port", 8080, "API and UI listen port")
	otlpGRPCPort := fs.Int("otlp-grpc-port", 4317, "OTLP gRPC listen port")
	otlpHTTPPort := fs.Int("otlp-http-port", 4318, "OTLP HTTP listen port")
	dataDir := fs.String("data-dir", "data", "data directory for persistence")
```

4b. Update the startup banner (lines 112-113). Currently:

```go
	fmt.Printf("  OTLP gRPC:      http://localhost:4317\n")
	fmt.Printf("  OTLP HTTP:      http://localhost:4318\n")
```

Change to:

```go
	fmt.Printf("  OTLP gRPC:      http://localhost:%d\n", *otlpGRPCPort)
	fmt.Printf("  OTLP HTTP:      http://localhost:%d\n", *otlpHTTPPort)
```

4c. Update the call site (line 163). Currently:

```go
	if err := recv.Start(); err != nil {
		log.Fatalf("Failed to start OTLP receiver: %v", err)
	}
```

Change the first line to (the `log.Fatalf` line stays unchanged):

```go
	if err := recv.Start(*otlpGRPCPort, *otlpHTTPPort); err != nil {
```

- [ ] **Step 5: Run the new tests to verify they pass, then build the whole project**

Run: `go test ./internal/receiver/... -run TestStart -v`
Expected: PASS — both `TestStartCustomPorts` and `TestStartFailFastOnConflict` pass.

Run: `go build ./...`
Expected: no errors (confirms the `Start` signature change is consistent across the only caller, `main.go`).

- [ ] **Step 6: Commit**

```bash
git add internal/receiver/otlp.go internal/receiver/otlp_test.go cmd/labubu/main.go
git commit -m "feat(receiver): make OTLP ports configurable via --otlp-grpc-port/--otlp-http-port

Bind both listeners synchronously in Start so a port conflict fails fast
with an error instead of silently skipping the listener. Defaults remain
4317/4318 so existing behavior is unchanged."
```

---

## Task 2: Documentation

**Files:**
- Modify: `README.md` (line 7)
- Modify: `docs/deployment.md` (parameter table lines 88-97; note after line 104)

- [ ] **Step 1: Note configurable ports in `README.md`**

Line 7 currently reads:

```
- **OTLP Ingestion** — gRPC (port 4317) and HTTP (port 4318) for traces and metrics
```

Change to:

```
- **OTLP Ingestion** — gRPC (port 4317) and HTTP (port 4318) for traces and metrics; ports configurable via `--otlp-grpc-port` / `--otlp-http-port`
```

- [ ] **Step 2: Add the two flags to the parameter table in `docs/deployment.md`**

The table starts at line 88. Insert two rows immediately after the `--port` row (after line 90). The `--port` row is:

```
| `--port` | 8080 | Web UI 和 API 端口 |
```

Insert these two rows directly after it:

```
| `--otlp-grpc-port` | 4317 | OTLP gRPC 接收端口 |
| `--otlp-http-port` | 4318 | OTLP HTTP 接收端口 |
```

- [ ] **Step 3: Add an ExecStart override note in `docs/deployment.md`**

After the `systemctl restart labubu` block (after line 104, which closes that bash fence), append a new paragraph:

```markdown
如需修改 OTLP 接收端口（默认 4317/4318），在 `ExecStart` 中加上 `--otlp-grpc-port <port>` 和/或 `--otlp-http-port <port>`，然后执行 `systemctl daemon-reload && systemctl restart labubu`。
```

- [ ] **Step 4: Commit**

```bash
git add README.md docs/deployment.md
git commit -m "docs: document configurable OTLP ports"
```

---

## Task 3: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full non-CGO test suite**

Run: `make test-nocgo`
Expected: all tests pass, including the two new `TestStart*` tests in `internal/receiver`.

- [ ] **Step 2: Build without CGO**

Run: `make build-nocgo`
Expected: builds successfully with no errors.

- [ ] **Step 3: Confirm the flags appear in `serve --help`**

Run: `go run -tags dev ./cmd/labubu serve --help 2>&1 | grep otlp`
Expected: two lines printed, one for `-otlp-grpc-port` (default 4317) and one for `-otlp-http-port` (default 4318).

- [ ] **Step 4: Smoke-test a custom port**

Run: `go run -tags dev ./cmd/labubu serve --otlp-http-port 14318 --port 18080`
Expected: banner prints `OTLP HTTP: http://localhost:14318` and `API & UI: http://localhost:18080`; server starts without "port in use" error. Stop with Ctrl+C.

(No commit — this task is verification only. If any step fails, fix the offending change and re-run from Task 1 / Task 2 as needed before committing the fix.)
