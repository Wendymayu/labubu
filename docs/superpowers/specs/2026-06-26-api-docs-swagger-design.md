# API Docs Page (Swagger UI) — Design

**Date:** 2026-06-26
**Status:** Approved
**Goal:** Add a left-sidebar menu item (above "Light Mode") that opens a page showing all Labubu REST APIs in Swagger format, so developers can wrap them into MCP tools locally.

## Background & Motivation

Labubu currently documents its REST API in `docs/api.md` as a hand-maintained markdown table. There is no machine-readable API description, which makes it hard for developers to wrap endpoints into MCP (Model Context Protocol) tools.

Arize Phoenix exposes an interactive Swagger UI page for its API. We mirror that experience: a new SPA route rendering the official Swagger UI from an OpenAPI 3.0 spec served by the Go backend.

## Non-Goals

- OTLP ingestion endpoints (gRPC/HTTP receiver in `internal/receiver/`) are **not** covered — they are not REST and not suitable for MCP wrapping.
- No try-it-out / live request execution (read-only rendering).
- No automatic spec generation from Go annotations (e.g. swaggo). Spec is hand-written; drift is caught by tests.

## Architecture & Data Flow

```
Go backend                                 Vue frontend
┌────────────────────────────┐            ┌──────────────────────────┐
│ internal/api/openapi.go     │            │ route /api-docs          │
│  - hand-written OpenAPI 3.0 │ ──spec──→  │  └─ SwaggerUI component  │
│  - GET /api/v1/openapi.json │   URL      │     (read-only)          │
└────────────────────────────┘            └──────────────────────────┘
        ▲                                            ▲
        │ drift test                                  │ menu item "API Docs"
        │ asserts spec covers all router.go routes   │ (above Light Mode toggle)
┌────────────────────────────┐            ┌──────────────────────────┐
│ internal/api/openapi_test.go│           │ App.vue sidebar-footer   │
└────────────────────────────┘            └──────────────────────────┘
```

- Backend serves a hand-written OpenAPI 3.0 JSON at `GET /api/v1/openapi.json`.
- Frontend route `/api-docs` mounts the official `swagger-ui` npm package, loading the spec URL.
- Sidebar menu item placed in `sidebar-footer` above the `ThemeToggle` ("Light Mode") button.

## Spec Source & Maintenance

**Decision:** Hand-written OpenAPI 3.0 JSON + drift-detection test.

Rationale (vs. swaggo auto-generation):
- swaggo requires annotating ~30 handlers, introduces a CLI codegen step and a new Go dependency — conflicts with CLAUDE.md's "Surgical changes / Simplicity first".
- API changes are infrequent; the drift test makes "forgot to update spec" a failing test, aligning with the existing "New API endpoints require tests" convention.
- Parameter/request schema descriptions must be hand-authored regardless of approach.

## Components

### Backend (Go)

1. **`internal/api/openapi.go`**
   - Builds and returns the OpenAPI 3.0 JSON document.
   - Hand-written content covering every `/api/v1/*` REST route registered in `router.go`.
   - Handler signature: `func OpenAPIHandler(w http.ResponseWriter, r *http.Request)`.
   - Sets `Content-Type: application/json` and returns the document.

2. **`internal/api/openapi_test.go`**
   - Drift detection: parses the generated spec, then runs a table of (method, path) cases — one per real route — and asserts each has a matching path item + method in the spec.
   - The route cases are hand-listed in the test (not scraped from source), so adding a new API requires adding a test row AND a spec entry — both must agree or the test fails.

3. **`internal/api/router.go`**
   - Register `mux.HandleFunc("/api/v1/openapi.json", OpenAPIHandler)`.

### Frontend (Vue/TS)

1. **`web/src/views/ApiDocs.vue`**
   - Mounts `SwaggerUI` from the `swagger-ui` npm package.
   - Props: `url="/api/v1/openapi.json"`, `docExpansion="list"`, `tryItOutEnabled=false`, `supportedSubmitMethods=[]` (disables try-it-out).
   - Fills the main content area.

2. **`web/src/router.ts`**
   - Add `{ path: '/api-docs', name: 'api-docs', component: ApiDocs }`.

3. **`web/src/App.vue`**
   - In `sidebar-footer`, add `<router-link to="/api-docs" class="footer-link">{{ t('nav.apiDocs') }}</router-link>` as the first child (above `ThemeToggle`).

4. **`web/src/i18n/locales/en.ts` and `zh.ts`**
   - Add `nav.apiDocs`: en = `"API Docs"`, zh = `"API 文档"`.

5. **`web/package.json`**
   - Add dependency `swagger-ui` (and `@types/swagger-ui` if available, else a local module declaration).

## Spec Coverage & Depth

**Covered route groups** (all under `/api/v1`):
- traces (list, get, export, import, diagnose, diagnosis)
- services
- sessions (list, get, agent-stats)
- logs (list, get by trace, event names)
- metrics (query, query_range, labels, label values, metadata, metric-names, otlp metrics ingest)
- dashboards (CRUD + panels)
- model-pricing (CRUD + recalc)
- llm-configs (CRUD + set-default)
- alerts (rules CRUD + history)
- cost-summary
- health

**Per-endpoint depth** (the minimum needed to wrap into an MCP tool):
- HTTP method + path (with path params templated as `{id}`)
- `summary` (short) + `description` (longer, from `docs/api.md`)
- Query parameters: name, type, required, description
- Request body schema for POST/PUT (JSON schema with properties)
- Response 200: schema or example payload
- Common error responses (400/404/500) where relevant

## Drift Detection Test Logic

The test does NOT parse `router.go` source (fragile). Instead:

1. Construct the spec document (call the same function the handler uses).
2. Define a table of `(method, pathPattern)` cases — one per real REST route.
3. For each case, assert the spec's `paths[pathPattern][method.toLowerCase()]` exists.
4. Also assert the overall spec parses as valid OpenAPI 3.0 (has `openapi`, `info`, `paths`).

When a developer adds a new API, they must add both a spec entry and a test row; missing either fails `make test`.

## i18n / Styling / Testing

- **i18n:** `nav.apiDocs` added to both en and zh locale files; referenced via `t('nav.apiDocs')` in `App.vue`.
- **Styling:** SwaggerUI renders with its default theme inside the SPA main area. The footer menu link reuses the existing footer-button visual style (a styled `<router-link>`).
- **Testing:**
  - Backend: `internal/api/openapi_test.go` — spec parses + drift detection passes.
  - Frontend: `cd web && npx vue-tsc --noEmit` type check passes.
  - Build: `make build-nocgo` succeeds.

## Open Questions (resolved)

1. Menu label: "API Docs" / "API 文档". ✓
2. Spec depth: method/path/summary/query params/request body/response 200/errors — sufficient for MCP. ✓
3. OTLP receiver endpoints excluded. ✓
