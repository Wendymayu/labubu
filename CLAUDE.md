# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Labubu is a local-first LLM observability platform. It receives OTLP traces and metrics, stores them, and provides a Vue 3 web UI for exploration.

**Stack:** Go 1.19 backend + Vue 3 (TypeScript) frontend + chDB storage (CGO, optional) + tstorage for metrics

## Build Commands

```bash
make build          # Build frontend + Go binary (with embedded frontend)
make build-nocgo    # Build without CGO (for linting, no embed)
make test           # Run all Go tests
make test-nocgo     # Run tests excluding chDB integration tests
make run            # Run in dev mode (reads frontend from disk)
make dev            # Start Vite dev server (frontend only)
make web-build      # Build Vue frontend only
```

## Test Commands

```bash
go test -v ./internal/... ./web/... ./cmd/...   # All Go tests
cd labubu-python && python -m pytest tests/ -v   # Python CLI tests
cd web && npx vue-tsc --noEmit                   # TypeScript check
```

## Architecture

```
cmd/labubu/main.go    — CLI entry point (serve/version/help subcommands)
internal/
  api/                — HTTP API handlers (traces, sessions, metrics, dashboards)
  receiver/           — OTLP gRPC + HTTP receiver (traces, metrics)
  pipeline/           — Async ingestion pipeline with backpressure
  storage/            — Store interface + chDB (CGO) / memstore (non-CGO) implementations
  metrics/            — Metrics store interface + tstorage implementation
  log/                — Structured logging
web/src/              — Vue 3 SPA (views, components, router, API client, i18n)
labubu-python/        — Python package shell for pip distribution
```

## Key Patterns

- **Build tags:** `!dev` (production, embeds frontend), `dev` (development, reads from disk)
- **Storage:** `Store` interface with chDB (CGO + `local_engine` tag) and in-memory implementations
- **Frontend:** `web.StaticFS` exposed from `web` package, used by `router.go`
- **Version:** Set at build time via `-ldflags "-X main.Version=..."`

## Project Roadmap

See `docs/roadmap.md` for the feature plan and completion status.
