package api

import "net/http"

// openAPISpecJSON is the hand-written OpenAPI 3.0 document describing every
// REST route registered in router.go. The drift test in openapi_test.go keeps
// this list in sync with the canonical route table. Update both together when
// adding an API. Operations are grouped into modules via the top-level tags
// array (which also fixes the section order); Swagger UI renders one
// collapsible section per tag.
const openAPISpecJSON = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Labubu API",
    "version": "1.0.0",
    "description": "Local-first LLM observability platform REST API. Base URL: /api/v1."
  },
  "tags": [
    { "name": "Traces", "description": "Trace listing, detail, LLM diagnosis, and export/import" },
    { "name": "Sessions", "description": "Session grouping and agent behavior stats" },
    { "name": "Logs", "description": "Log exploration and event names" },
    { "name": "Metrics", "description": "Prometheus-compatible metrics query and OTLP ingestion" },
    { "name": "Dashboards", "description": "Custom dashboard and panel management" },
    { "name": "Model Pricing", "description": "Per-model pricing entries and cost recalculation" },
    { "name": "LLM Configs", "description": "LLM provider configurations for diagnosis" },
    { "name": "Alerts", "description": "Alert rules, states, and notification history" },
    { "name": "Cost", "description": "Cost aggregation and summary" },
    { "name": "System", "description": "Health and system endpoints" }
  ],
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
        "tags": ["Traces"],
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
          { "name": "max_duration", "in": "query", "schema": { "type": "string" }, "description": "Maximum duration" },
          { "name": "min_spans", "in": "query", "schema": { "type": "integer" }, "description": "Minimum span count" },
          { "name": "max_spans", "in": "query", "schema": { "type": "integer" }, "description": "Maximum span count" },
          { "name": "min_cost", "in": "query", "schema": { "type": "number" }, "description": "Minimum cost" },
          { "name": "max_cost", "in": "query", "schema": { "type": "number" }, "description": "Maximum cost" }
        ],
        "responses": { "200": { "description": "Trace list" } }
      }
    },
    "/api/v1/traces/{id}": {
      "get": {
        "tags": ["Traces"],
        "summary": "Get trace detail",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" }, "description": "Trace ID (hex)" }],
        "responses": { "200": { "description": "Full trace detail with spans" }, "404": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/traces/{id}/diagnosis": {
      "get": {
        "tags": ["Traces"],
        "summary": "Get stored diagnosis for a trace",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Diagnosis result" }, "404": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/traces/{id}/diagnose": {
      "post": {
        "tags": ["Traces"],
        "summary": "Run LLM diagnosis on a trace",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Diagnosis result" }, "500": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/traces/export": {
      "post": {
        "tags": ["Traces"],
        "summary": "Export traces to OTLP JSON",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "trace_ids": { "type": "array", "items": { "type": "string" } }, "format": { "type": "string" } } } } } },
        "responses": { "200": { "description": "Exported OTLP JSON" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/traces/import": {
      "post": {
        "tags": ["Traces"],
        "summary": "Import traces from OTLP JSON",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "OTLP JSON trace export" } } } },
        "responses": { "200": { "description": "Import result" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/services": {
      "get": {
        "tags": ["Traces"],
        "summary": "List known service names",
        "responses": { "200": { "description": "List of service names" } }
      }
    },
    "/api/v1/sessions": {
      "get": {
        "tags": ["Sessions"],
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
        "tags": ["Sessions"],
        "summary": "Get session detail",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Session detail with traces" }, "404": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/sessions/{id}/agent-stats": {
      "get": {
        "tags": ["Sessions"],
        "summary": "Get agent behavior stats for a session",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Agent behavior stats" } }
      }
    },
    "/api/v1/sessions/{id}/context": {
      "get": {
        "tags": ["Sessions"],
        "summary": "Get main-agent LLM spans for a session context chart",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Main-agent LLM spans with token breakdown" } }
      }
    },
    "/api/v1/logs": {
      "get": {
        "tags": ["Logs"],
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
        "tags": ["Logs"],
        "summary": "Get all logs for a trace",
        "parameters": [{ "name": "traceId", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Logs for the trace" } }
      }
    },
    "/api/v1/log-event-names": {
      "get": {
        "tags": ["Logs"],
        "summary": "List distinct log event names",
        "responses": { "200": { "description": "Event name list" } }
      }
    },
    "/api/v1/query": {
      "get": {
        "tags": ["Metrics"],
        "summary": "Prometheus instant query",
        "parameters": [{ "name": "query", "in": "query", "required": true, "schema": { "type": "string" }, "description": "PromQL expression" }, { "name": "time", "in": "query", "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Prometheus query result" } }
      }
    },
    "/api/v1/query_range": {
      "get": {
        "tags": ["Metrics"],
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
      "get": { "tags": ["Metrics"], "summary": "List all metric label names", "responses": { "200": { "description": "Label name list" } } }
    },
    "/api/v1/label/{name}/values": {
      "get": {
        "tags": ["Metrics"],
        "summary": "List values for a label name",
        "parameters": [{ "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Label values" } }
      }
    },
    "/api/v1/metadata": {
      "get": { "tags": ["Metrics"], "summary": "Metric metadata", "responses": { "200": { "description": "Metric metadata" } } }
    },
    "/api/v1/metric-names": {
      "get": { "tags": ["Metrics"], "summary": "List all metric names", "responses": { "200": { "description": "Metric name list" } } }
    },
    "/api/v1/otlp/v1/metrics": {
      "post": {
        "tags": ["Metrics"],
        "summary": "OTLP metrics ingestion (HTTP)",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "OTLP metrics JSON" } } } },
        "responses": { "200": { "description": "Accepted" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/dashboards": {
      "get": { "tags": ["Dashboards"], "summary": "List all dashboards", "responses": { "200": { "description": "Dashboard list" } } },
      "post": {
        "tags": ["Dashboards"],
        "summary": "Create dashboard",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "name": { "type": "string" } } } } } },
        "responses": { "200": { "description": "Created dashboard" } }
      }
    },
    "/api/v1/dashboards/{id}": {
      "put": {
        "tags": ["Dashboards"],
        "summary": "Rename dashboard",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "name": { "type": "string" } } } } } },
        "responses": { "200": { "description": "Updated dashboard" } }
      },
      "delete": {
        "tags": ["Dashboards"],
        "summary": "Delete dashboard",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Deleted" } }
      }
    },
    "/api/v1/dashboards/{id}/panels": {
      "post": {
        "tags": ["Dashboards"],
        "summary": "Add panel to dashboard",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "Panel config" } } } },
        "responses": { "200": { "description": "Added panel" } }
      }
    },
    "/api/v1/dashboards/{id}/panels/{panelId}": {
      "put": {
        "tags": ["Dashboards"],
        "summary": "Update panel",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "panelId", "in": "path", "required": true, "schema": { "type": "string" } }],
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "Panel config" } } } },
        "responses": { "200": { "description": "Updated panel" } }
      },
      "delete": {
        "tags": ["Dashboards"],
        "summary": "Delete panel",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "panelId", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Deleted" } }
      }
    },
    "/api/v1/model-pricing": {
      "get": { "tags": ["Model Pricing"], "summary": "List model pricing", "responses": { "200": { "description": "Pricing list" } } },
      "post": {
        "tags": ["Model Pricing"],
        "summary": "Create or update model pricing (upsert)",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "model_name": { "type": "string" }, "input_price": { "type": "number" }, "output_price": { "type": "number" }, "currency": { "type": "string" } } } } } },
        "responses": { "200": { "description": "Saved pricing" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/model-pricing/recalc": {
      "post": {
        "tags": ["Model Pricing"],
        "summary": "Recalculate costs for all traces",
        "responses": { "200": { "description": "Recalculation result" }, "500": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/model-pricing/{name}": {
      "delete": {
        "tags": ["Model Pricing"],
        "summary": "Delete model pricing by name",
        "parameters": [{ "name": "name", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Deleted" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/llm-configs": {
      "get": { "tags": ["LLM Configs"], "summary": "List LLM configs (API keys masked)", "responses": { "200": { "description": "Config list" } } },
      "post": {
        "tags": ["LLM Configs"],
        "summary": "Create LLM config",
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "provider_type": { "type": "string" }, "model_name": { "type": "string" }, "provider_url": { "type": "string" }, "api_key": { "type": "string" }, "temperature": { "type": "number" }, "max_tokens": { "type": "integer" } } } } } },
        "responses": { "200": { "description": "Created config" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/llm-configs/{id}": {
      "put": {
        "tags": ["LLM Configs"],
        "summary": "Update LLM config (api_key '***' means keep existing)",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "model_name": { "type": "string" }, "provider_url": { "type": "string" }, "api_key": { "type": "string" }, "default": { "type": "boolean" } } } } } },
        "responses": { "200": { "description": "Updated config" }, "400": { "$ref": "#/components/responses/Error" } }
      },
      "delete": {
        "tags": ["LLM Configs"],
        "summary": "Delete LLM config",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "Deleted" }, "400": { "$ref": "#/components/responses/Error" } }
      }
    },
    "/api/v1/alerts/rules": {
      "get": { "tags": ["Alerts"], "summary": "List alert rules", "responses": { "200": { "description": "Rule list" } } },
      "post": { "tags": ["Alerts"], "summary": "Create alert rule", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "Alert rule definition" } } } }, "responses": { "200": { "description": "Created rule" } } }
    },
    "/api/v1/alerts/rules/{id}": {
      "get": { "tags": ["Alerts"], "summary": "Get alert rule", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "Rule" }, "404": { "$ref": "#/components/responses/Error" } } },
      "put": { "tags": ["Alerts"], "summary": "Update alert rule", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "description": "Alert rule definition" } } } }, "responses": { "200": { "description": "Updated rule" } } },
      "delete": { "tags": ["Alerts"], "summary": "Delete alert rule", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "Deleted" } } }
    },
    "/api/v1/alerts/states": {
      "get": { "tags": ["Alerts"], "summary": "List alert states (firing/resolved)", "responses": { "200": { "description": "State list" } } }
    },
    "/api/v1/alerts/notifications": {
      "get": { "tags": ["Alerts"], "summary": "List alert notification history", "responses": { "200": { "description": "Notification history" } } }
    },
    "/api/v1/cost-summary": {
      "get": {
        "tags": ["Cost"],
        "summary": "Cost summary",
        "parameters": [
          { "name": "period", "in": "query", "schema": { "type": "string", "enum": ["today", "7d", "30d"] }, "description": "Preset time range (ignored when start/end are given)" },
          { "name": "start", "in": "query", "schema": { "type": "integer" }, "description": "Custom range start, epoch ms; overrides period when paired with end" },
          { "name": "end", "in": "query", "schema": { "type": "integer" }, "description": "Custom range end, epoch ms" },
          { "name": "group_by", "in": "query", "schema": { "type": "string", "enum": ["model", "service"] }, "description": "Breakdown dimension" }
        ],
        "responses": { "200": { "description": "Cost summary" } }
      }
    },
    "/api/health": {
      "get": { "tags": ["System"], "summary": "Health check", "responses": { "200": { "description": "Service status" } } }
    }
  }
}`

// OpenAPIHandler serves the OpenAPI 3.0 spec as JSON.
func OpenAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(openAPISpecJSON))
}
