# Labubu

A trace observability platform for AI agents, built on OpenTelemetry and chDB.

## Overview

Labubu receives OTLP trace data from instrumented AI agents, stores it in an embedded ClickHouse database (chDB), and provides a web UI for exploring traces with waterfall visualization.

**Phase 1 features:**
- OTLP trace ingestion via gRPC (port 4317) and HTTP (port 4318)
- Embedded chDB storage (no external database required)
- In-memory storage fallback (no CGO needed for development)
- Trace list with search, filtering, and pagination
- Trace detail with waterfall view and span inspection
- LLM span token tracking (input/output/total tokens)

## Quick Start

### Prerequisites

- Go 1.19+
- Node.js 18+
- chDB (`libchdb.so`) on the library path (optional — in-memory store works without it)

### Build

```bash
# Install frontend dependencies
cd web && npm install && cd ..

# Build frontend
make web-build

# Build backend (without CGO — uses in-memory store)
make build-nocgo

# Or, with chDB support (requires libchdb):
# CGO_ENABLED=1 go build -tags "cgo,local_engine" -o bin/labubu ./cmd/labubu
```

### Run

```bash
# Start with default settings
./bin/labubu

# Or run directly with Go
go run ./cmd/labubu
```

Then open http://localhost:8080 to view the UI.

### Development

```bash
# Terminal 1: Start backend
go run ./cmd/labubu --data-dir="" --api-addr=0.0.0.0:8080

# Terminal 2: Start frontend dev server (with HMR)
cd web && npm run dev
```

Visit http://localhost:3001 for the Vite dev server (proxies API to :8080).

## Architecture

```
OTLP (gRPC/HTTP) → Receiver → Pipeline → Storage (chDB or in-memory)
                              ↓
                         REST API ← Vue SPA
```

```
labubu/
├── cmd/labubu/main.go              # Entry point
├── internal/
│   ├── receiver/otlp.go            # OTLP ingestion (gRPC + HTTP)
│   ├── pipeline/pipeline.go        # Async batch processing
│   ├── storage/
│   │   ├── storage.go              # Store interface + model types
│   │   ├── chdb.go                 # chDB CGO implementation
│   │   ├── memstore.go             # In-memory store (dev fallback)
│   │   ├── chdb_query.go           # SQL query builder
│   │   └── schema.sql              # chDB DDL (traces + spans)
│   ├── api/
│   │   ├── router.go               # HTTP router + SPA serving
│   │   └── trace_handler.go        # Trace API handlers
│   └── mcp/                        # Reserved for Phase 2 AI integration
├── web/                            # Vue 3 + TypeScript SPA
│   └── src/
│       ├── views/
│       │   ├── TraceList.vue       # Trace list page
│       │   └── TraceDetail.vue     # Trace detail + waterfall
│       ├── components/
│       │   ├── WaterfallChart.vue  # Waterfall timeline
│       │   └── SpanDetail.vue      # Span detail panel
│       └── api/client.ts           # Typed API client
├── Makefile
└── docs/superpowers/
    ├── specs/                      # Design spec
    └── plans/                      # Implementation plan
```

## API

| Endpoint | Description |
|----------|-------------|
| `GET /api/health` | Health check |
| `GET /api/v1/traces` | List traces (with filters) |
| `GET /api/v1/traces/:id` | Trace detail with all spans |
| `GET /api/v1/services` | List known service names |

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--api-addr` | `0.0.0.0:8080` | API and UI listen address |
| `--data-dir` | `./data` | chDB data directory (empty = in-memory) |
| `--buffer-size` | `1000` | Pipeline buffer capacity |
| `--flush-interval` | `200ms` | Pipeline flush interval |

## License

MIT
