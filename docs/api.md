# API Reference

Base URL: `/api/v1`

## Traces

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/traces` | List traces (query: `page`, `page_size`, `service`, `status`, `q`, `start`, `end`, `min_duration`, `max_duration`) |
| `GET` | `/traces/:id` | Full trace detail with all spans, resource attrs, scope info |
| `POST` | `/traces/export` | Export traces to OTLP JSON (`{ trace_ids: string[], format: string }`) |
| `GET` | `/services` | List known service names |

## Sessions

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/sessions` | List sessions (query: `page`, `page_size`, `service`, `q`, `start`, `end`) |
| `GET` | `/sessions/:id` | Session detail with summary stats and all traces |

## Logs

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/logs` | List logs (query: `page`, `page_size`, `severity`, `event_name`, `q`, `trace_id`, `start`, `end`) |
| `GET` | `/logs/:traceId` | Get all logs for a trace |
| `GET` | `/log-event-names` | List distinct log event names |

## Metrics (Prometheus-compatible)

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/query` | Instant PromQL query (`?query=...`) |
| `GET` | `/query_range` | Range PromQL query (`?query=...&start=...&end=...&step=...`) |
| `GET` | `/labels` | List all label names |
| `GET` | `/label/:name/values` | List values for a given label name |
| `GET` | `/metadata` | Metric metadata |
| `GET` | `/metric-names` | List all metric names |
| `POST` | `/otlp/v1/metrics` | OTLP metrics ingestion (HTTP) |

## Dashboards

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/dashboards` | List all dashboards |
| `POST` | `/dashboards` | Create dashboard (`{ name: string }`) |
| `PUT` | `/dashboards/:id` | Rename dashboard (`{ name: string }`) |
| `DELETE` | `/dashboards/:id` | Delete dashboard |
| `POST` | `/dashboards/:id/panels` | Add panel to dashboard |
| `PUT` | `/dashboards/:id/panels/:panelId` | Update panel |
| `DELETE` | `/dashboards/:id/panels/:panelId` | Remove panel from dashboard |

## Model Pricing

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/model-pricing` | List all model pricing entries |
| `POST` | `/model-pricing` | Create/update a pricing entry (`{ model_name, input_price, output_price, currency }`) |
| `DELETE` | `/model-pricing/:modelName` | Delete a pricing entry |
| `POST` | `/model-pricing/recalc` | Recalculate costs for all stored traces and sessions |

## LLM Configs

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/llm-configs` | List all LLM model configurations |
| `POST` | `/llm-configs` | Create config (`{ model_name, provider_url, api_key, is_default?, temperature?, max_tokens? }`) |
| `PUT` | `/llm-configs/:id` | Update config (api_key value `***` leaves existing key unchanged) |
| `DELETE` | `/llm-configs/:id` | Delete config |

## System

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/health` | Health check (`{ status: "ok" }`) |
