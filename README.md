# Labubu

A local-first LLM observability platform. It receives OTLP traces and metrics from instrumented AI agents, stores them locally, and provides a Vue 3 web UI for exploration.

## Features

- **OTLP Ingestion** — gRPC (port 4317) and HTTP (port 4318) for traces and metrics
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

- **Go** 1.19+
- **Node.js** 18+ (frontend development only)

chDB (`libchdb.so`) is optional — the in-memory store works without it for development.

### 1. Clone & install

```bash
git clone https://github.com/labubu/labubu.git
cd labubu

# Install frontend dependencies
cd web && npm install && cd ..
```

### 2. Start backend

```bash
# Dev mode (reads frontend from disk, in-memory storage)
go run -tags dev ./cmd/labubu serve
```

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
# Build frontend assets + Go binary
make build

# Binary at bin/labubu
./bin/labubu serve
```

The binary embeds all frontend assets — ship it and run it anywhere.

### Send some test data

```bash
# Install a demo OTLP exporter (Python example)
pip install labubu

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

### Traces

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/traces` | List traces (page, page_size, service, status, query, start/end time) |
| `GET /api/v1/traces/:id` | Full trace detail with all spans |
| `GET /api/v1/services` | List known service names |

### Sessions

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/sessions` | List sessions (page, page_size, service, query, time range) |
| `GET /api/v1/sessions/:id` | Session detail with summary stats and all traces |

### Metrics & Dashboards

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/metrics/query` | Instant PromQL query |
| `GET /api/v1/metrics/query_range` | Range PromQL query |
| `GET /api/v1/metrics/labels` | List label names |
| `GET /api/v1/metrics/label/:name/values` | List label values |
| `GET /api/v1/metrics/metadata` | Metric metadata |
| `GET /api/v1/dashboards` | List dashboards |
| `POST /api/v1/dashboards` | Create dashboard |
| `PUT /api/v1/dashboards/:name` | Update dashboard |
| `DELETE /api/v1/dashboards/:name` | Delete dashboard |

### System

| Endpoint | Description |
|----------|-------------|
| `GET /api/health` | Health check |

## Architecture

```
OTLP (gRPC/HTTP) → Receiver → Pipeline → Storage (chDB or in-memory)
                    │                        │
                    ▼                        ▼
               Metric Store           REST API ← Vue 3 SPA
              (tstorage)          (traces/sessions/metrics/dashboards)
```

```
labubu/
├── cmd/labubu/main.go           # CLI entry point (serve/version/help)
├── internal/
│   ├── receiver/                # OTLP ingestion (gRPC + HTTP, traces + metrics)
│   ├── pipeline/                # Async batch processing with backpressure
│   ├── storage/
│   │   ├── storage.go           # Store interface + model types
│   │   ├── chdb.go              # chDB CGO implementation
│   │   ├── memstore.go          # In-memory store (dev fallback)
│   │   ├── config.go            # YAML config + retention settings
│   │   ├── chdb_query.go        # SQL query builder
│   │   └── schema.sql           # chDB DDL (traces + spans)
│   ├── api/
│   │   ├── router.go            # HTTP router + SPA serving
│   │   ├── trace_handler.go     # Trace API handlers
│   │   ├── session_handler.go   # Session API handlers
│   │   ├── metrics_handler.go   # Metrics/PromQL API handlers
│   │   └── dashboard_handler.go # Dashboard CRUD handlers
│   ├── metrics/                 # tstorage-backed metrics store
│   └── log/                     # Structured logging
├── web/                         # Vue 3 + TypeScript SPA
│   └── src/
│       ├── views/               # TraceList, TraceDetail, SessionList, ...
│       ├── components/          # WaterfallChart, TokenPieChart, ...
│       ├── api/                 # Typed API client
│       └── i18n/                # Chinese/English translations
├── labubu-python/               # Python package for pip distribution
├── Makefile
└── docs/                        # Specs, plans, roadmap
```

## Development

```bash
# Run all Go tests
make test

# Run tests excluding chDB integration tests
make test-nocgo

# TypeScript type check
cd web && npx vue-tsc --noEmit

# Build frontend only
make web-build

# Build without CGO (linting check, no embed)
make build-nocgo
```

## License

MIT
