# Labubu

A local-first LLM observability platform. It receives OTLP traces and metrics from instrumented AI agents, stores them locally, and provides a Vue 3 web UI for exploration.

## Features

- **OTLP Ingestion** — gRPC (port 4317) and HTTP (port 4318) for traces and metrics; ports configurable via `--otlp-grpc-port` / `--otlp-http-port`
- **Embedded chDB Storage** — ClickHouse-compatible, no external database required (optional, with in-memory fallback)
- **Trace Explorer** — searchable trace list with service/status/duration filters
- **Waterfall View** — span-level waterfall chart with slide-in detail drawer
- **LLM Token Tracking** — capture input/output/total tokens, with context window pie charts
- **Session Observability** — group traces by session, monitor error rates and token usage
- **Metrics & Dashboards** — auto-generated charts from OTLP metrics, custom dashboards
- **Trace Retention** — YAML-configurable max age and max count policies, background cleanup
- **Internationalization** — Chinese/English UI with vue-i18n
- **Zero Dependencies** — single binary embeds the entire frontend

## Quick Start

### Prerequisites

- **Go** 1.25+
- **Node.js** 18+ (frontend development only)

No external database required — SQLite is used by default. chDB (`libchdb.so`) is optional for ClickHouse-compatible storage on Linux/macOS.

### 1. Clone & install

```bash
git clone https://github.com/labubu/labubu.git
cd labubu

# Install frontend dependencies
cd web && npm install && cd ..
```

### 2. Start backend

```bash
go run -tags dev ./cmd/labubu serve
```

> Uses SQLite storage by default. For chDB (Linux/macOS with CGO): `go run -tags "dev local_engine" ./cmd/labubu serve`

The backend starts at:
- **API & UI:** http://localhost:8080
- **OTLP gRPC:** http://localhost:4317
- **OTLP HTTP:** http://localhost:4318

### 3. Start frontend (optional — for hot-reload dev)

Open a second terminal:

```bash
cd web && npm run dev
```

Vite dev server starts at http://localhost:3001 and proxies API requests to `:8080`.

> **Alternatively**, skip step 3: `make run` starts the backend with dev tags, serving the full app at http://localhost:8080 without a separate frontend server.

### Build a single binary

```bash
# Build with chDB (requires CGO)
make build

# Build without chDB (no CGO, for Windows or CI)
make build-nocgo

# Binary at bin/labubu
./bin/labubu serve
```

The binary embeds all frontend assets — ship it and run it anywhere.

### Send some test data

```bash
# Install from GitHub Release (replace <version> and <platform> as needed)
pip install https://github.com/Wendymayu/labubu/releases/download/v0.1.0/labubu-0.1.0-py3-none-<platform>.whl

# Or download the standalone binary from:
# https://github.com/Wendymayu/labubu/releases

# Or use any OpenTelemetry SDK configured to export to:
#   OTLP endpoint: http://localhost:4318
```

Open http://localhost:8080 to explore traces, sessions, and metrics.

## Configuration

### CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8080` | API and UI listen port |
| `--data-dir` | `""` | chDB data directory (empty = in-memory) |
| `--config` | `labubu.yaml` | YAML config file path |
| `--buffer-size` | `1000` | Pipeline buffer capacity |
| `--flush-interval` | `200ms` | Pipeline flush interval |
| `--metrics-enabled` | `true` | Enable/disable metrics ingestion |
| `--metrics-data-dir` | `""` | tstorage data directory (empty = memory only) |
| `--metrics-retention` | `2h` | tstorage retention duration |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, `error` |

### YAML config (`labubu.yaml`)

Place a `labubu.yaml` in the working directory (or use `--config`):

```yaml
trace:
  retention:
    max_age: 24h          # delete traces older than this (0 = unlimited)
    max_count: 10000      # keep only the newest N traces (0 = unlimited)
    cleanup_interval: 5m  # how often the cleanup goroutine runs
```

No file? Built-in defaults are used silently — everything still works.

## API

Full endpoint reference: **[docs/api.md](docs/api.md)**

Quick overview:

| Section | Endpoints |
|---------|-----------|
| Traces | `GET /api/v1/traces`, `GET /api/v1/traces/:id`, `POST /api/v1/traces/export`, `GET /api/v1/services`, `GET /api/v1/traces/:id/diagnosis`, `POST /api/v1/traces/:id/diagnose` |
| Sessions | `GET /api/v1/sessions`, `GET /api/v1/sessions/:id`, `GET /api/v1/sessions/:id/agent-stats` |
| Logs | `GET /api/v1/logs`, `GET /api/v1/logs/:traceId`, `GET /api/v1/log-event-names` |
| Metrics | `GET /api/v1/query`, `/query_range`, `/labels`, `/label/:name/values`, `/metadata`, `/metric-names` |
| Dashboards | Full CRUD at `/api/v1/dashboards` + panels sub-resource |
| Pricing | `GET/POST/DELETE /api/v1/model-pricing`, `POST /api/v1/model-pricing/recalc` |
| LLM Configs | Full CRUD at `/api/v1/llm-configs` |
| System | `GET /api/health` |

## Architecture

```
OTLP (gRPC/HTTP) → Receiver → Pipeline → Storage (chDB, SQLite, or in-memory)
                    │                        │
                    ▼                        ▼
               Metric Store           REST API ← Vue 3 SPA
              (tstorage)          (traces/sessions/metrics/dashboards)
```

Detailed project structure: **[docs/project-structure.md](docs/project-structure.md)**

## Development

```bash
# Run all Go tests (requires CGO)
make test

# Run tests without CGO (recommended for Windows/CI)
make test-nocgo

# Or run directly:
go test -tags nosqlite -v ./internal/...

# TypeScript type check
cd web && npx vue-tsc --noEmit

# Build frontend only
make web-build

# Build without CGO (linting check, no embed)
make build-nocgo
```

## License

MIT
