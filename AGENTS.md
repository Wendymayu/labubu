# AGENTS.md

Guidance for Codex in this repository.

## Behavioral Guidelines

- **Think before coding.** State assumptions. If multiple interpretations exist, present them. If unclear, ask — don't guess.
- **Simplicity first.** No features beyond what was asked. No abstractions for single-use code. If 200 lines could be 50, rewrite.
- **Surgical changes.** Don't refactor or "improve" adjacent code. Match existing style even if you'd do it differently. Every changed line should trace to the request.
- **Goal-driven.** Transform tasks into verifiable goals: add tests first, loop until verified.

## Project Overview

Labubu is a local-first LLM observability platform. It receives OTLP traces, metrics, and logs from instrumented AI agents, stores them locally, and provides a Vue 3 web UI for exploration.

**Stack:** Go 1.19 backend + Vue 3 (TypeScript) frontend + chDB storage (CGO, optional) + tstorage for metrics

## Important Rules

- **TypeScript:** Strict typing — avoid `any`. All new types go in `web/src/api/client.ts`.
- **Go:** Follow standard conventions. Backend storage must go through the `Store` interface — never access chDB directly from handlers.
- **i18n:** All user-facing text must use `vue-i18n`. Add keys to both `web/src/i18n/locales/en.ts` and `zh.ts`, then reference via `t('section.key')` in templates.
- **Frontend state:** Use Vue 3 Composition API (`ref`, `reactive`, composables). No global state outside composables.
- **Tests:** New API endpoints require tests in `internal/api/`. Go table-driven tests preferred.
- **API keys:** Never log or expose full API keys in responses. The masked sentinel `***` in update requests means "keep existing value".

## Build & Test Commands

```bash
make build          # Build frontend + Go binary (with embedded frontend)
make build-nocgo    # Build without CGO (for linting, no embed)
make test           # Run all Go tests
make test-nocgo     # Run tests excluding chDB integration tests
make run            # Run in dev mode (reads frontend from disk)
make dev            # Start Vite dev server (frontend only)
make web-build      # Build Vue frontend only
```

## Manual Test & Lint Commands

```bash
go test -v ./internal/... ./web/... ./cmd/...   # All Go tests
cd labubu-python && python -m pytest tests/ -v   # Python CLI tests
cd web && npx vue-tsc --noEmit                   # TypeScript type check
```

## Architecture

```
OTLP (gRPC/HTTP) → Receiver → Pipeline → Storage (chDB or in-memory)
                       │                        │
                       ▼                        ▼
                  tstorage              REST API ← Vue 3 SPA
```

```
cmd/labubu/main.go    — CLI entry point (serve/version/help subcommands)
internal/
  api/                — HTTP handlers (traces, sessions, metrics, dashboards, logs, pricing, llm-configs)
  receiver/           — OTLP gRPC + HTTP receiver (traces, metrics, logs)
  pipeline/           — Async ingestion pipeline with backpressure
  storage/            — Store interface + chDB (CGO) / memstore (non-CGO) implementations
  metrics/            — tstorage-backed metrics store
  log/                — Structured logging (slog)
web/src/              — Vue 3 SPA (views, components, router, API client, i18n)
labubu-python/        — Python package shell for pip distribution
```

Full directory tree: `docs/project-structure.md`

## Key Patterns

- **Build tags:** `!dev` (production, embeds frontend), `dev` (development, reads from disk)
- **Storage:** `Store` interface (see `internal/storage/storage.go`). chDB implementation requires `local_engine` build tag; memstore works everywhere.
- **Frontend serving:** `web.StaticFS` (embed or disk) → `fs.Sub` → `serveSPA` in `internal/api/router.go`
- **Version:** Set at build time via `-ldflags "-X main.Version=..."`
- **Router pattern:** Go 1.22 `http.ServeMux` with path-based routing. Handlers subroute with `strings.TrimPrefix`.
- **API client:** Single `client.ts` exports typed async functions and interfaces — all frontend API calls go through it.
- **CSS theming:** CSS custom properties (`--bg-primary`, `--text-secondary`, etc.) in `App.vue` scoped styles + `useTheme` composable.
- **i18n:** Locale from `localStorage`, fallback to `'en'`. All UI strings in locale files under `nav.*`, `traceList.*`, `sessionList.*`, etc.

## Development Workflow

1. Make code changes
2. TypeScript check: `cd web && npx vue-tsc --noEmit`
3. Run relevant Go tests: `make test-nocgo` (fast) or `make test` (full)
4. Build check: `make build-nocgo`
5. Full integration test: `make run` and verify in browser at http://localhost:8080

## Documentation

| Document | Content |
|----------|---------|
| `docs/project-structure.md` | Full directory tree with file descriptions |
| `docs/api.md` | Complete API endpoint reference |
| `docs/roadmap.md` | Feature plan and completion status |
| `docs/deployment.md` | Server deployment + systemd setup |
| `docs/integrations/` | Integration guides (e.g., Codex metrics) |
