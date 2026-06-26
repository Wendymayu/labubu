# API Docs Page (Swagger UI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a left-sidebar menu item (above "Light Mode") that opens a Swagger UI page rendering Labubu's REST API from a hand-written OpenAPI 3.0 spec, so developers can wrap endpoints into MCP tools.

**Architecture:** Go backend serves a hand-written OpenAPI 3.0 JSON at `GET /api/v1/openapi.json`; a new Vue route `/api-docs` mounts the official `swagger-ui` npm package in read-only mode loading that URL; a drift-detection Go test keeps the spec's endpoint list in sync with a canonical route table.

**Tech Stack:** Go 1.19 `net/http`, Go 1.22 `http.ServeMux`, Vue 3 + TypeScript, vue-router 4, `swagger-ui` npm package, vue-i18n.

## Global Constraints

- Go backend follows `internal/api` conventions: handlers dispatch via `strings.TrimPrefix`, JSON via the existing `writeJSON(w, status, v)` helper (see `internal/api/trace_handler.go:378`).
- Storage/handlers must NOT be accessed outside the `Store` interface — N/A here (no storage touched).
- TypeScript strict, no `any`; new frontend types go in `web/src/api/client.ts` (N/A here — no new API client functions needed).
- i18n: every user-facing string added to BOTH `web/src/i18n/locales/en.ts` and `zh.ts`, referenced via `t('...')`.
- OTLP receiver endpoints are excluded — REST routes in `internal/api/router.go` only.
- Spec is hand-written OpenAPI 3.0 JSON (no swaggo/codegen). Read-only rendering (try-it-out disabled).
- Frequent commits; each task ends with a commit.

## File Structure

**Backend:**
- `internal/api/openapi.go` (CREATE) — holds the OpenAPI 3.0 JSON constant + `OpenAPIHandler`. Single responsibility: serve the spec.
- `internal/api/openapi_test.go` (CREATE) — parses the spec, asserts valid OpenAPI 3.0 structure, and runs two-way drift check (canonical route table ⇄ spec paths).
- `internal/api/router.go` (MODIFY) — register `/api/v1/openapi.json`.

**Frontend:**
- `web/src/views/ApiDocs.vue` (CREATE) — mounts SwaggerUI, read-only, spec URL `/api/v1/openapi.json`.
- `web/src/router.ts` (MODIFY) — add `/api-docs` route.
- `web/src/App.vue` (MODIFY) — add footer menu link above `ThemeToggle`.
- `web/src/i18n/locales/en.ts` (MODIFY) — add `nav.apiDocs`.
- `web/src/i18n/locales/zh.ts` (MODIFY) — add `nav.apiDocs`.
- `web/package.json` (MODIFY) — add `swagger-ui` dependency.

---

## Task 1: Backend — OpenAPI spec, handler, and drift test (TDD)

**Files:**
- Create: `internal/api/openapi.go`
- Create: `internal/api/openapi_test.go`
- Modify: `internal/api/router.go` (register the new endpoint)

**Interfaces:**
- Consumes: the existing `writeJSON(w http.ResponseWriter, status int, v interface{})` helper from `internal/api/trace_handler.go:378`.
- Produces: `func OpenAPIHandler(w http.ResponseWriter, r *http.Request)` — referenced by `router.go`; serves `Content-Type: application/json` and the spec body. Also produces the unexported `openAPISpecJSON` string constant consumed by the test.

**Canonical route table** (the source of truth the test enforces against the spec). These are the exact REST routes registered in `internal/api/router.go` and their sub-handlers:

| Method | Path (OpenAPI template) |
|--------|--------------------------|
| GET | /api/v1/traces |
| GET | /api/v1/traces/{id} |
| GET | /api/v1/traces/{id}/diagnosis |
| POST | /api/v1/traces/{id}/diagnose |
| POST | /api/v1/traces/export |
| POST | /api/v1/traces/import |
| GET | /api/v1/services |
| GET | /api/v1/sessions |
| GET | /api/v1/sessions/{id} |
| GET | /api/v1/sessions/{id}/agent-stats |
| GET | /api/v1/logs |
| GET | /api/v1/logs/{traceId} |
| GET | /api/v1/log-event-names |
| GET | /api/v1/query |
| GET | /api/v1/query_range |
| GET | /api/v1/labels |
| GET | /api/v1/label/{name}/values |
| GET | /api/v1/metadata |
| GET | /api/v1/metric-names |
| POST | /api/v1/otlp/v1/metrics |
| GET | /api/v1/dashboards |
| POST | /api/v1/dashboards |
| PUT | /api/v1/dashboards/{id} |
| DELETE | /api/v1/dashboards/{id} |
| POST | /api/v1/dashboards/{id}/panels |
| PUT | /api/v1/dashboards/{id}/panels/{panelId} |
| DELETE | /api/v1/dashboards/{id}/panels/{panelId} |
| GET | /api/v1/model-pricing |
| POST | /api/v1/model-pricing |
| POST | /api/v1/model-pricing/recalc |
| DELETE | /api/v1/model-pricing/{name} |
| GET | /api/v1/llm-configs |
| POST | /api/v1/llm-configs |
| PUT | /api/v1/llm-configs/{id} |
| DELETE | /api/v1/llm-configs/{id} |
| GET | /api/v1/alerts/rules |
| POST | /api/v1/alerts/rules |
| GET | /api/v1/alerts/rules/{id} |
| PUT | /api/v1/alerts/rules/{id} |
| DELETE | /api/v1/alerts/rules/{id} |
| GET | /api/v1/alerts/states |
| GET | /api/v1/alerts/notifications |
| GET | /api/v1/cost-summary |
| GET | /api/health |

- [ ] **Step 1: Write the failing test**

Create `internal/api/openapi_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// expectedEndpoints is the canonical list of REST routes registered in router.go
// and its sub-handlers. Adding a new API requires adding a row here AND an entry
// in openAPISpecJSON — the two-way check below fails if they disagree.
var expectedEndpoints = []struct {
	method string
	path   string
}{
	{"get", "/api/v1/traces"},
	{"get", "/api/v1/traces/{id}"},
	{"get", "/api/v1/traces/{id}/diagnosis"},
	{"post", "/api/v1/traces/{id}/diagnose"},
	{"post", "/api/v1/traces/export"},
	{"post", "/api/v1/traces/import"},
	{"get", "/api/v1/services"},
	{"get", "/api/v1/sessions"},
	{"get", "/api/v1/sessions/{id}"},
	{"get", "/api/v1/sessions/{id}/agent-stats"},
	{"get", "/api/v1/logs"},
	{"get", "/api/v1/logs/{traceId}"},
	{"get", "/api/v1/log-event-names"},
	{"get", "/api/v1/query"},
	{"get", "/api/v1/query_range"},
	{"get", "/api/v1/labels"},
	{"get", "/api/v1/label/{name}/values"},
	{"get", "/api/v1/metadata"},
	{"get", "/api/v1/metric-names"},
	{"post", "/api/v1/otlp/v1/metrics"},
	{"get", "/api/v1/dashboards"},
	{"post", "/api/v1/dashboards"},
	{"put", "/api/v1/dashboards/{id}"},
	{"delete", "/api/v1/dashboards/{id}"},
	{"post", "/api/v1/dashboards/{id}/panels"},
	{"put", "/api/v1/dashboards/{id}/panels/{panelId}"},
	{"delete", "/api/v1/dashboards/{id}/panels/{panelId}"},
	{"get", "/api/v1/model-pricing"},
	{"post", "/api/v1/model-pricing"},
	{"post", "/api/v1/model-pricing/recalc"},
	{"delete", "/api/v1/model-pricing/{name}"},
	{"get", "/api/v1/llm-configs"},
	{"post", "/api/v1/llm-configs"},
	{"put", "/api/v1/llm-configs/{id}"},
	{"delete", "/api/v1/llm-configs/{id}"},
	{"get", "/api/v1/alerts/rules"},
	{"post", "/api/v1/alerts/rules"},
	{"get", "/api/v1/alerts/rules/{id}"},
	{"put", "/api/v1/alerts/rules/{id}"},
	{"delete", "/api/v1/alerts/rules/{id}"},
	{"get", "/api/v1/alerts/states"},
	{"get", "/api/v1/alerts/notifications"},
	{"get", "/api/v1/cost-summary"},
	{"get", "/api/health"},
}

func TestOpenAPIHandler_ServesValidSpec(t *testing.T) {
	rec := httptest.NewRecorder()
	OpenAPIHandler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if spec["openapi"] != "3.0.3" {
		t.Fatalf("openapi = %v, want 3.0.3", spec["openapi"])
	}
	if _, ok := spec["info"].(map[string]interface{}); !ok {
		t.Fatalf("missing info object")
	}
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing paths object")
	}

	// Two-way drift check: every expected endpoint must be in the spec,
	// and every spec path+method must be in the expected list.
	specMethods := map[string]map[string]bool{}
	for p, v := range paths {
		item, ok := v.(map[string]interface{})
		if !ok {
			t.Fatalf("path %q is not an object", p)
		}
		specMethods[p] = map[string]bool{}
		for m := range item {
			switch m {
			case "get", "post", "put", "delete", "patch":
				specMethods[p][m] = true
			}
		}
	}

	expected := map[string]map[string]bool{}
	for _, e := range expectedEndpoints {
		if expected[e.path] == nil {
			expected[e.path] = map[string]bool{}
		}
		expected[e.path][e.method] = true
	}

	for p, methods := range expected {
		got, ok := specMethods[p]
		if !ok {
			t.Errorf("spec missing path %q", p)
			continue
		}
		for m := range methods {
			if !got[m] {
				t.Errorf("spec missing %s %s", m, p)
			}
		}
	}
	for p, methods := range specMethods {
		for m := range methods {
			if !expected[p][m] {
				t.Errorf("spec has unexpected %s %s (not in expected route table)", m, p)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestOpenAPIHandler_ServesValidSpec -v`
Expected: compile failure — `OpenAPIHandler` undefined.

- [ ] **Step 3: Write minimal implementation — `internal/api/openapi.go`**

Create `internal/api/openapi.go` with the full OpenAPI 3.0 JSON constant and the handler. The spec below covers every endpoint in the canonical table with method, path, summary, query parameters, request bodies, and responses.

```go
package api

import "net/http"

// openAPISpecJSON is the hand-written OpenAPI 3.0 document describing every
// REST route registered in router.go. The drift test in openapi_test.go keeps
// this list in sync with the canonical route table. Update both together when
// adding an API.
const openAPISpecJSON = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Labubu API",
    "version": "1.0.0",
    "description": "Local-first LLM observability platform REST API. Base URL: /api/v1."
  },
  "components": {
    "responses": {
      "Error": {
        "description": "Error response",
        "content": {
          "application/json": {
            "schema": { "type": "object", "properties": { "error": { "type": "string" } } }
          }
        }
      }
    }
  },
  "paths": {
    "/api/v1/traces": {
      "get": {
        "summary": "List traces",
        "description": "Paginated list of traces with filtering.",
        "parameters": [
          { "name": "page", "in": "query", "schema": { "type": "integer" }, "description": "Page number (1-based)" },
          { "name": "page_size", "in": "query", "schema": { "type": "integer" }, "description": "Page size" },
          { "name": "service", "in": "query", "schema": { "type": "string" }, "description": "Filter by service name" },
          { "name": "status", "in": "query", "schema": { "type": "string" }, "description": "Filter by status (ok/error)" },
          { "name": "q", "in": "query", "schema": { "type": "string" }, "description": "Full-text search on trace name" },
          { "name": "start", "in": "query", "schema": { "type": "string" }, "description": "Start time (RFC3339)" },
          { "name": "end", "in": "query", "schema": { "type": "string" }, "description": "End time (RFC3339)" },
          { "name": "min_duration", "in": "query", "schema": { "type": "string" }, "description": "Minimum duration (e.g. 100ms)" },
          { "name": "max_duration", "in": "query", "schema": { "type": "string" }, "description": "Maximum duration" }
        ],
        "responses": { "200": { "description": "Trace list" } }
      }
    },
    "/api/v1/traces/{id}": {
      "get": {
        "summary": "Get trace detail",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" }, "description": "Trace ID (hex)" }],
        "responses": { "200": { "description": "Full trace detail with spans" }, "404": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/traces/{id}/diagnosis": {
      "get": {
        "summary": "Get stored diagnosis for a trace",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Diagnosis result" }, "404": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/traces/{id}/diagnose": {
      "post": {
        "summary": "Run LLM diagnosis on a trace",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Diagnosis result" }, "500": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/traces/export": {
      "post": {
        "summary": "Export traces to OTLP JSON",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "trace_ids": { "type": "array", "items": { "type": "string" } }, "format": { "type": "string" } } } } } },
        "responses": { "200": { "description": "Exported OTLP JSON" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/traces/import": {
      "post": {
        "summary": "Import traces from OTLP JSON",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "OTLP JSON trace export" } } } },
        "responses": { "200": { "description": "Import result" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/services": {
      "get": {
        "summary": "List known service names",
        "responses": { "200": { "description": "List of service names" } }
      }
    },
    "/api/v1/sessions": {
      "get": {
        "summary": "List sessions",
        "parameters": [
          { "name": "page", "in": "query", "schema": { "type": "integer" } },
          { "name": "page_size", "in": "query", "schema": { "type": "integer" } },
          { "name": "service", "in": "query", "schema": { "type": "string" } },
          { "name": "q", "in": "query", "schema": { "type": "string" } },
          { "name": "start", "in": "query", "schema": { "type": "string" } },
          { "name": "end", "in": "query", "schema": { "type": "string" } }
        ],
        "responses": { "200": { "description": "Session list" } }
      }
    },
    "/api/v1/sessions/{id}": {
      "get": {
        "summary": "Get session detail",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Session detail with traces" }, "404": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/sessions/{id}/agent-stats": {
      "get": {
        "summary": "Get agent behavior stats for a session",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Agent behavior stats" } }
      }
    },
    "/api/v1/logs": {
      "get": {
        "summary": "List logs",
        "parameters": [
          { "name": "page", "in": "query", "schema": { "type": "integer" } },
          { "name": "page_size", "in": "query", "schema": { "type": "integer" } },
          { "name": "severity", "in": "query", "schema": { "type": "string" } },
          { "name": "event_name", "in": "query", "schema": { "type": "string" } },
          { "name": "q", "in": "query", "schema": { "type": "string" } },
          { "name": "trace_id", "in": "query", "schema": { "type": "string" } },
          { "name": "start", "in": "query", "schema": { "type": "string" } },
          { "name": "end", "in": "query", "schema": { "type": "string" } }
        ],
        "responses": { "200": { "description": "Log list" } }
      }
    },
    "/api/v1/logs/{traceId}": {
      "get": {
        "summary": "Get all logs for a trace",
        "parameters": [{ "name": "traceId", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Logs for the trace" } }
      }
    },
    "/api/v1/log-event-names": {
      "get": {
        "summary": "List distinct log event names",
        "responses": { "200": { "description": "Event name list" } }
      }
    },
    "/api/v1/query": {
      "get": {
        "summary": "Prometheus instant query",
        "parameters": [{ "name": "query", "in": "query", "required": true, "schema": { "type": "string" }, "description": "PromQL expression" }, { "name": "time", "in": "query", "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Prometheus query result" } }
      }
    },
    "/api/v1/query_range": {
      "get": {
        "summary": "Prometheus range query",
        "parameters": [
          { "name": "query", "in": "query", "required": true, "schema": { "type": "string" } },
          { "name": "start", "in": "query", "required": true, "schema": { "type": "string" } },
          { "name": "end", "in": "query", "required": true, "schema": { "type": "string" } },
          { "name": "step", "in": "query", "required": true, "schema": { "type": "string" } }
        ],
        "responses": { "200": { "description": "Prometheus range result" } }
      }
    },
    "/api/v1/labels": {
      "get": { "summary": "List all metric label names", "responses": { "200": { "description": "Label name list" } } }
    },
    "/api/v1/label/{name}/values": {
      "get": {
        "summary": "List values for a label name",
        "parameters": [{ "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Label values" } }
      }
    },
    "/api/v1/metadata": {
      "get": { "summary": "Metric metadata", "responses": { "200": { "description": "Metric metadata" } } }
    },
    "/api/v1/metric-names": {
      "get": { "summary": "List all metric names", "responses": { "200": { "description": "Metric name list" } } }
    },
    "/api/v1/otlp/v1/metrics": {
      "post": {
        "summary": "OTLP metrics ingestion (HTTP)",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "OTLP metrics JSON" } } } },
        "responses": { "200": { "description": "Accepted" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/dashboards": {
      "get": { "summary": "List all dashboards", "responses": { "200": { "description": "Dashboard list" } } },
      "post": {
        "summary": "Create dashboard",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "name": { "type": "string" } } } } } },
        "responses": { "200": { "description": "Created dashboard" } }
      }
    },
    "/api/v1/dashboards/{id}": {
      "put": {
        "summary": "Rename dashboard",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "name": { "type": "string" } } } } } },
        "responses": { "200": { "description": "Updated dashboard" } }
      },
      "delete": {
        "summary": "Delete dashboard",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Deleted" } }
      }
    },
    "/api/v1/dashboards/{id}/panels": {
      "post": {
        "summary": "Add panel to dashboard",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "Panel config" } } } },
        "responses": { "200": { "description": "Added panel" } }
      }
    },
    "/api/v1/dashboards/{id}/panels/{panelId}": {
      "put": {
        "summary": "Update panel",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "panelId", "in": "path", "required": true, "schema": { "type": "string" } }],
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "Panel config" } } } },
        "responses": { "200": { "description": "Updated panel" } }
      },
      "delete": {
        "summary": "Delete panel",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "panelId", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Deleted" } }
      }
    },
    "/api/v1/model-pricing": {
      "get": { "summary": "List model pricing", "responses": { "200": { "description": "Pricing list" } } },
      "post": {
        "summary": "Create or update model pricing (upsert)",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "model_name": { "type": "string" }, "input_price": { "type": "number" }, "output_price": { "type": "number" }, "currency": { "type": "string" } } } } } },
        "responses": { "200": { "description": "Saved pricing" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/model-pricing/recalc": {
      "post": {
        "summary": "Recalculate costs for all traces",
        "responses": { "200": { "description": "Recalculation result" }, "500": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/model-pricing/{name}": {
      "delete": {
        "summary": "Delete model pricing by name",
        "parameters": [{ "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Deleted" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/llm-configs": {
      "get": { "summary": "List LLM configs (API keys masked)", "responses": { "200": { "description": "Config list" } } },
      "post": {
        "summary": "Create LLM config",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "provider_type": { "type": "string" }, "model_name": { "type": "string" }, "provider_url": { "type": "string" }, "api_key": { "type": "string" }, "temperature": { "type": "number" }, "max_tokens": { "type": "integer" } } } } } },
        "responses": { "200": { "description": "Created config" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/llm-configs/{id}": {
      "put": {
        "summary": "Update LLM config (api_key '***' means keep existing)",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "model_name": { "type": "string" }, "provider_url": { "type": "string" }, "api_key": { "type": "string" }, "default": { "type": "boolean" } } } } } },
        "responses": { "200": { "description": "Updated config" }, "400": { "$ref": "#/components/responses/Error" } }
      },
      "delete": {
        "summary": "Delete LLM config",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Deleted" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/alerts/rules": {
      "get": { "summary": "List alert rules", "responses": { "200": { "description": "Rule list" } } },
      "post": { "summary": "Create alert rule", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "Alert rule definition" } } } }, "responses": { "200": { "description": "Created rule" } } }
    },
    "/api/v1/alerts/rules/{id}": {
      "get": { "summary": "Get alert rule", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "Rule" }, "404": { "$ref": "#/components/responses/Error" } } },
      "put": { "summary": "Update alert rule", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "Alert rule definition" } } } }, "responses": { "200": { "description": "Updated rule" } } },
      "delete": { "summary": "Delete alert rule", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "Deleted" } } }
    },
    "/api/v1/alerts/states": {
      "get": { "summary": "List alert states (firing/resolved)", "responses": { "200": { "description": "State list" } } }
    },
    "/api/v1/alerts/notifications": {
      "get": { "summary": "List alert notification history", "responses": { "200": { "description": "Notification history" } } }
    },
    "/api/v1/cost-summary": {
      "get": {
        "summary": "Cost summary",
        "parameters": [{ "name": "period", "in": "query", "schema": { "type": "string", "enum": ["today", "7d", "30d"] } }],
        "responses": { "200": { "description": "Cost summary" } }
      }
    },
    "/api/health": {
      "get": { "summary": "Health check", "responses": { "200": { "description": "Service status" } } }
    }
  }
}`

// OpenAPIHandler serves the OpenAPI 3.0 spec as JSON.
func OpenAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(openAPISpecJSON))
}
```

- [ ] **Step 4: Register the route in `internal/api/router.go`**

In `NewRouter`, add after the `/api/health` handler (before the SPA serving block). Insert this line immediately after the `mux.HandleFunc("/api/health", ...)` block:

```go
	// OpenAPI spec for the API docs page.
	mux.HandleFunc("/api/v1/openapi.json", OpenAPIHandler)
```

The exact anchor to insert after (lines 120-122 of `internal/api/router.go`):

```go
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestOpenAPIHandler_ServesValidSpec -v`
Expected: PASS.

- [ ] **Step 6: Run full API test suite to confirm no regressions**

Run: `go test ./internal/api/ ./internal/alerting/ -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/api/openapi.go internal/api/openapi_test.go internal/api/router.go
git commit -m "feat(api): add OpenAPI 3.0 spec endpoint with drift-detection test"
```

---

## Task 2: Frontend — Swagger UI view, route, menu, i18n, dependency

**Files:**
- Modify: `web/package.json` (add `swagger-ui`)
- Create: `web/src/views/ApiDocs.vue`
- Modify: `web/src/router.ts` (add route)
- Modify: `web/src/App.vue` (add footer menu link)
- Modify: `web/src/i18n/locales/en.ts` (add `nav.apiDocs`)
- Modify: `web/src/i18n/locales/zh.ts` (add `nav.apiDocs`)

**Interfaces:**
- Consumes: the `GET /api/v1/openapi.json` endpoint from Task 1.
- Produces: a Vue route `/api-docs` and a sidebar menu entry `nav.apiDocs`.

- [ ] **Step 1: Add the `swagger-ui` dependency**

Run from `web/`:

```bash
cd web && npm install swagger-ui
```

This adds `swagger-ui` to `web/package.json` dependencies. (If TypeScript cannot find types, Step 3 declares a local module shim.)

- [ ] **Step 2: Create `web/src/views/ApiDocs.vue`**

```vue
<template>
  <div class="api-docs">
    <div ref="swaggerContainer" class="api-docs-container"></div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
// swagger-ui ships without bundled types; declare a minimal shim below.
// @ts-ignore - no types available for swagger-ui
import SwaggerUI from 'swagger-ui'

const swaggerContainer = ref<HTMLElement | null>(null)

onMounted(() => {
  SwaggerUI({
    domNode: swaggerContainer.value,
    url: '/api/v1/openapi.json',
    docExpansion: 'list',
    tryItOutEnabled: false,
    supportedSubmitMethods: [],
    persistAuth: false,
  })
})
</script>

<style scoped>
.api-docs {
  background: var(--bg-primary);
  min-height: calc(100vh - 48px);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  overflow: hidden;
}
.api-docs-container :deep(.swagger-ui) {
  background: var(--bg-primary);
}
</style>
```

- [ ] **Step 3: Add a TypeScript module shim for `swagger-ui`**

Create `web/src/swagger-ui.d.ts`:

```ts
declare module 'swagger-ui' {
  const SwaggerUI: (config: {
    domNode: HTMLElement | null
    url: string
    docExpansion?: string
    tryItOutEnabled?: boolean
    supportedSubmitMethods?: string[]
    persistAuth?: boolean
  }) => void
  export default SwaggerUI
}
```

- [ ] **Step 4: Register the route in `web/src/router.ts`**

Add the import at the top with the other view imports (after the `AlertHistory` import, line 13):

```ts
import ApiDocs from './views/ApiDocs.vue'
```

Add the route entry in the `routes` array (after the `/` redirect entry):

```ts
    { path: '/api-docs', name: 'api-docs', component: ApiDocs },
```

- [ ] **Step 5: Add the menu link in `web/src/App.vue`**

In the `sidebar-footer` div (currently lines 40-43), add a `router-link` as the **first** child, above `<ThemeToggle />`:

```vue
      <div class="sidebar-footer">
        <router-link to="/api-docs" class="footer-link">{{ t('nav.apiDocs') }}</router-link>
        <ThemeToggle />
        <LanguageToggle />
      </div>
```

Add the footer-link style inside the existing `<style scoped>` block (after `.sidebar-footer { ... }`):

```css
.footer-link {
  width: 100%;
  display: flex;
  align-items: center;
  padding: 6px 10px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  color: var(--text-secondary);
  font-size: 13px;
  text-decoration: none;
  box-sizing: border-box;
}
.footer-link:hover { border-color: var(--border-strong); color: var(--text-primary); }
.footer-link.router-link-active { color: var(--accent-blue); border-color: var(--accent-blue); }
```

- [ ] **Step 6: Add i18n keys**

In `web/src/i18n/locales/en.ts`, add `apiDocs` to the `nav` block (after `llmConfigs: 'Model Config',`):

```ts
    llmConfigs: 'Model Config',
    apiDocs: 'API Docs',
```

In `web/src/i18n/locales/zh.ts`, add `apiDocs` to the `nav` block (after `llmConfigs: '模型配置',`):

```ts
    llmConfigs: '模型配置',
    apiDocs: 'API 文档',
```

- [ ] **Step 7: Type check the frontend**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add web/package.json web/package-lock.json web/src/views/ApiDocs.vue web/src/swagger-ui.d.ts web/src/router.ts web/src/App.vue web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat(web): add Swagger UI API docs page with sidebar menu entry"
```

---

## Task 3: Build verification

**Files:** none (verification only).

- [ ] **Step 1: Build the Go binary without CGO**

Run: `make build-nocgo`
Expected: build succeeds (the embedded frontend is built first by `web-build`).

- [ ] **Step 2: Run the full test suite**

Run: `make test-nocgo`
Expected: all PASS (includes the new `TestOpenAPIHandler_ServesValidSpec`).

- [ ] **Step 3: Manual smoke test (optional, if a browser is available)**

Run: `make run`, then open `http://localhost:8080/api-docs`.
Expected: Swagger UI renders with all endpoint groups listed (read-only, no "Try it out" button). Verify `http://localhost:8080/api/v1/openapi.json` returns the raw JSON.

- [ ] **Step 4: Commit any lockfile/build artifacts if changed** (usually none beyond Task 2)

```bash
git status
# if clean, nothing to commit
```

---

## Self-Review Notes

- **Spec coverage:** The design spec's components (openapi.go, openapi_test.go, router registration, ApiDocs.vue, router.ts, App.vue, en/zh i18n, package.json) all map to tasks. ✓
- **Drift test scope:** The test enforces a two-way agreement between the canonical route table and the spec. It does NOT auto-detect routes added to `router.go` source — a developer adding an API must add both a spec entry and a test row. This matches the approved design spec ("both must agree or the test fails"). ✓
- **Type consistency:** `OpenAPIHandler` signature is identical in Task 1's test, implementation, and router registration. The `swagger-ui` shim's config shape matches the call in `ApiDocs.vue`. ✓
- **No placeholders:** every code step contains complete, copy-pasteable code; no TBD/TODO. ✓
