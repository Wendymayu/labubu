# Configurable OTLP Ports — Design

- **Date:** 2026-06-25
- **Status:** Approved (pending spec review)
- **Scope sub-project:** B (interfaces configurable) — minimal cut
- **Related future work:** Sub-project C (logs as first-class citizen), tracked separately

## Background

Labubu's OTLP receiver listens on two hardcoded ports:

- gRPC on `0.0.0.0:4317` — `internal/receiver/otlp.go:73`
- HTTP on `0.0.0.0:4318` — `internal/receiver/otlp.go:86`

The API/UI port is configurable via `--port` (default 8080), but the two OTLP
ports are not. There is no flag and no YAML key to change them. Operators who
need different ports (port conflict, multi-instance, restricted port range)
currently must edit source and rebuild.

The `labubu.yaml` config file (`storage.LoadConfig`) only covers storage,
retention, and pricing — it does not touch the listening surface.

## Goal

Make the two OTLP listening ports configurable via CLI flags, with the current
values as defaults so existing `labubu serve` behavior is unchanged.

## Non-goals (explicitly out of scope)

To keep this spec minimal, the following are **not** included:

- Bind-address configurability (still hardcoded `0.0.0.0`).
- Per-signal enable/disable toggles (traces/metrics/logs all stay always-on,
  modulo the existing `--metrics-enabled` store toggle).
- `port=0` "disable this listener" semantics.
- YAML configuration for the receiver (flags only).
- API/UI bind-address configurability (`--port` stays port-only).
- Anything in sub-project C (log retention, severity fidelity, structured
  body/attributes, `/v1/logs` JSON support, logs-in-pipeline).

These may be revisited in later specs; they are deliberately deferred here.

## Design

### Configuration

Two new flags on the `serve` subcommand, defined in `cmd/labubu/main.go`
alongside the existing `--port`:

```go
otlpGRPCPort := fs.Int("otlp-grpc-port", 4317, "OTLP gRPC listen port")
otlpHTTPPort := fs.Int("otlp-http-port", 4318, "OTLP HTTP listen port")
```

Defaults (4317 / 4318) match today's hardcoded values. Flags are passed
through to the receiver at start time:

```go
if err := recv.Start(*otlpGRPCPort, *otlpHTTPPort); err != nil {
    log.Fatalf("Failed to start OTLP receiver: %v", err)
}
```

### Code changes

**`cmd/labubu/main.go`**

1. Add the two flags after `--port` (around `main.go:76`).
2. Pass them to `recv.Start(...)` (`main.go:163`).
3. Update the startup banner (`main.go:112-113`) to print the configured ports:
   ```go
   fmt.Printf("  OTLP gRPC:      http://localhost:%d\n", *otlpGRPCPort)
   fmt.Printf("  OTLP HTTP:      http://localhost:%d\n", *otlpHTTPPort)
   ```

**`internal/receiver/otlp.go`**

1. Change `Start()` signature (`otlp.go:49`):
   ```go
   func (r *Receiver) Start(grpcPort, httpPort int) error
   ```
2. Bind both listeners **synchronously** in `Start` (see Error handling), using
   the provided ports:
   ```go
   net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", grpcPort))
   net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", httpPort))
   ```
3. `New()` signature is unchanged — construction and configuration stay
   separated, minimizing the diff.

### Error handling (fail-fast on port conflict)

**Current behavior (bug-adjacent):** `Start` binds inside goroutines
(`otlp.go:72-95`). If a port is already in use, the goroutine prints a line and
returns — Labubu continues running but **silently does not listen** on that
port. The API port, by contrast, is pre-checked and fails fast
(`main.go:94-100`).

Making ports configurable raises the chance of a misconfigured or conflicting
port, so this spec includes a targeted fix: bind both listeners synchronously
in `Start` before launching the serve goroutines. If either bind fails, close
the other (if already bound) and return an error. `main.go` treats the error as
fatal via `log.Fatalf`, matching the API-port behavior.

Sketch:

```go
func (r *Receiver) Start(grpcPort, httpPort int) error {
    r.grpcSrv = grpc.NewServer()
    // ...register trace/metrics/logs services (unchanged)...

    mux := http.NewServeMux()
    // ...register /v1/traces, /v1/metrics, /v1/logs (unchanged)...
    r.httpSrv = &http.Server{Handler: mux}

    grpcLis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", grpcPort))
    if err != nil {
        return fmt.Errorf("OTLP gRPC listen on :%d: %w", grpcPort, err)
    }
    httpLis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", httpPort))
    if err != nil {
        grpcLis.Close()
        return fmt.Errorf("OTLP HTTP listen on :%d: %w", httpPort, err)
    }

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

### Testing

CLAUDE.md requires new behavior to have tests. `internal/receiver/otlp_test.go`
already exists and defines a `memTestStore` stub implementing `storage.Store`;
reuse it for the store argument so service registration has a non-nil store.
Add two tests there:

1. **Custom-port binding:** the test grabs two free ports via
   `net.Listen("tcp", ":0")`, closes them, passes the port numbers to
   `Start(grpcPort, httpPort)`, then dials each address to confirm the receiver
   is actually listening. Defer `Shutdown`.
   - Note: `:0` here is the standard "let the OS pick a free port" testing
     technique for obtaining an ephemeral port number — it is **not** the
     `port=0` "disable this listener" feature, which is explicitly out of
     scope. `Start` itself always binds a real listener.
2. **Fail-fast on conflict:** the test holds a listener open on a free port,
   passes that same port as the gRPC port to `Start`, and asserts `Start`
   returns a non-nil error. (When the gRPC port is the conflict, `grpcLis`
   fails first and `httpLis` is never bound — so no listener leaks.)

### Backward compatibility

- Defaults 4317 / 4318 → `labubu serve` with no extra flags behaves exactly as
  today.
- `labubu serve --help` lists the new flags automatically (stdlib `flag`
  package).
- The only behavior change for existing users is the fail-fast on port
  conflict — which previously failed silently. This is an intentional
  correctness improvement, not a regression.

### Documentation

- `README.md`: where it says to send OTLP traces to `http://localhost:4318`,
  add a one-liner noting the ports are adjustable via `--otlp-grpc-port` /
  `--otlp-http-port`.
- `docs/deployment.md`: show a systemd `ExecStart` example passing the flags.

## Out of scope / future work

- **Rest of sub-project B:** bind-address config, per-signal toggles, YAML
  receiver section. Defer until a concrete need appears.
- **Sub-project C (logs as first-class citizen):** independent log retention,
  original-severity preservation, structured/queryable body & attributes,
  `/v1/logs` JSON support (OTLP HTTP spec compliance), moving logs into the
  async pipeline. This is a separate, larger spec — do not bundle here.
