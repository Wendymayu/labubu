# Project Structure

Detailed directory tree for the Labubu codebase.

## Top Level

```
labubu/
├── cmd/labubu/main.go           # CLI entry point (serve/version/help subcommands)
├── internal/                    # Go backend packages
├── web/                         # Vue 3 + TypeScript SPA frontend
├── labubu-python/               # Python package for pip distribution
├── docs/                        # Documentation (roadmap, deployment, integration guides)
├── Makefile                     # Build targets
├── README.md                    # Human-facing project overview
└── CLAUDE.md                    # AI agent instructions
```

## Backend — `internal/`

```
internal/
├── api/                         # HTTP API handlers
│   ├── router.go                # HTTP router + SPA serving (static files / SPA fallback)
│   ├── trace_handler.go         # Trace list, detail, export OTLP, services
│   ├── trace_handler_test.go
│   ├── session_handler.go       # Session list, detail
│   ├── session_handler_test.go
│   ├── metrics_handler.go       # Metrics/PromQL queries, metric names, labels
│   ├── metrics_handler_test.go
│   ├── dashboard_handler.go     # Dashboard + panel CRUD
│   ├── dashboard_handler_test.go
│   ├── log_handler.go           # Log list, by-trace, event names
│   ├── pricing_handler.go       # Model pricing CRUD + recalc
│   ├── llm_config_handler.go    # LLM model config CRUD + default management
│   ├── llm_config_handler_test.go
│   └── otlp_trace.go            # OTLP trace conversion utilities
├── receiver/                    # OTLP ingestion
│   ├── grpc_receiver.go         # gRPC receiver (port 4317)
│   └── http_receiver.go         # HTTP receiver (port 4318)
├── pipeline/                    # Async batch processing with backpressure
│   └── pipeline.go              # Buffer + flush-interval pipeline
├── storage/                     # Trace storage
│   ├── storage.go               # Store interface + model types (Trace, Span, LLMConfig, etc.)
│   ├── chdb.go                  # chDB CGO implementation (production)
│   ├── chdb_query.go            # SQL query builder for chDB
│   ├── memstore.go              # In-memory store (dev fallback, no CGO needed)
│   ├── memstore_purge_test.go   # Retention purge tests
│   ├── config.go                # YAML config loading + retention settings
│   ├── config_test.go
│   └── schema.sql               # chDB DDL (traces, spans, logs, sessions tables)
├── metrics/                     # Metrics store
│   └── tstorage/                # tstorage-backed Prometheus-compatible metrics
└── log/                         # Structured logging (slog-based)
```

## Frontend — `web/src/`

```
web/
├── package.json
├── tsconfig.json
├── vite.config.ts
├── index.html
├── dist/                        # Build output (embedded by Go binary)
└── src/
    ├── main.ts                  # Vue app entry + router + i18n setup
    ├── App.vue                  # Root layout (sidebar + router-view)
    ├── router.ts                # Client-side routes
    ├── api/
    │   └── client.ts            # Typed API client (all endpoints, all types)
    ├── composables/
    │   └── useTheme.ts          # Dark/light theme composable
    ├── components/
    │   ├── WaterfallChart.vue   # Span waterfall visualization
    │   ├── SpanDetail.vue       # Slide-in drawer for span details
    │   ├── TokenPieChart.vue    # Context window pie chart
    │   ├── PanelChart.vue       # Dashboard panel chart (line/bar/stat)
    │   ├── PanelForm.vue        # Dashboard panel editor
    │   ├── ThemeToggle.vue      # Dark/light mode toggle
    │   └── LanguageToggle.vue   # Chinese/English language toggle
    ├── views/
    │   ├── TraceList.vue        # Trace explorer (search, filter, paginate)
    │   ├── TraceDetail.vue      # Single trace detail (waterfall + metadata)
    │   ├── SessionList.vue      # Session list (search, filter, stats)
    │   ├── SessionDetail.vue    # Session detail (traces + context window)
    │   ├── LogList.vue          # Log explorer (search, filter, severity)
    │   ├── Dashboard.vue        # Dashboard view (panels + layout)
    │   ├── PricingManager.vue   # Model pricing management
    │   └── LlmConfig.vue        # LLM model configuration CRUD
    ├── i18n/
    │   ├── index.ts             # vue-i18n setup (locale detection, fallback)
    │   └── locales/
    │       ├── en.ts            # English translations
    │       └── zh.ts            # Chinese translations
    └── utils/
        └── format.ts            # Duration, timestamp, number formatters
```

## Python Package — `labubu-python/`

```
labubu-python/
├── pyproject.toml               # Package metadata + build config
├── setup.py                     # Fallback setup script
├── labubu/
│   ├── __init__.py              # Version + exports
│   ├── cli.py                   # CLI commands (otlp-send, etc.)
│   └── otlp_exporter.py         # OTLP HTTP exporter
└── tests/                       # pytest test suite
```
