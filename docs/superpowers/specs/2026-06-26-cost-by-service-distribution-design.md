# Cost Breakdown by Service — Design

**Date:** 2026-06-26
**Status:** Approved (pending implementation)
**Branch:** feat/accurate-token-cost-aggregation

## Goal

On the cost dashboard (`/cost`), the page currently shows a single "Cost by Model" table at the bottom. Add a parallel **"Cost by Service"** breakdown, with a toggle that switches between the two views. Toggling is a server round-trip (the `group_by` query param selects the dimension); only the requested breakdown is computed and returned.

## Current Behavior (context)

- `GET /api/v1/cost-summary?period=today|7d|30d` → `CostHandler.summary` ([`internal/api/cost_handler.go`](../../../internal/api/cost_handler.go)) → `store.GetCostSummary(CostQuery{StartTimeMS, EndTimeMS})`.
- Response shape ([`internal/storage/storage.go`](../../../internal/storage/storage.go)): `CostSummaryResult{ Period, Currency, Overview, ByModel []ModelCostItem }`. `ModelCostItem` carries `model, cost, tokens, input_tokens, cache_creation_tokens, cache_read_tokens, output_tokens, trace_count, avg_cost`.
- `by_model` SQL ([`sqlite_store.go:1337-1356`](../../../internal/storage/sqlite_store.go#L1337)): groups `spans` by `gen_ai_request_model` directly — **no join to `traces`** (the comment explicitly says "no fan-out join"). Filtered to traces in the time window with `cost > 0`.
- `service.name` lives on **`traces.resource_attributes`** (JSON), read via `json_extract(resource_attributes, '$."service.name"')` — see `GetServices` ([`sqlite_store.go:646-650`](../../../internal/storage/sqlite_store.go#L646)). It is **not** on `spans`. So a by-service breakdown *must* join `spans` → `traces`, unlike by-model.
- Frontend ([`CostDashboard.vue`](../../../web/src/views/CostDashboard.vue)): period preset bar → 5 overview cards → `costByModel` table (columns: model, cost, tokens, cache, traces, avgCost). Reads `summary.by_model`.
- `GetCostSummary` is implemented only in **sqlite_store** and **memstore**. `chDBStore` (`//go:build cgo && local_engine`) does **not** implement `GetCostSummary` at all — a pre-existing gap unrelated to this feature. Default builds (`make build-nocgo`, `make test-nocgo`) exclude chDB, so this gap does not affect the supported path.

## Architecture

### Backend contract

- `GET /api/v1/cost-summary?period=&group_by=model|service`. `group_by` defaults to `model` (so the page opens exactly as today).
- Response always includes `overview` and echoes `group_by`. Only the requested breakdown is populated:
  - `group_by=model` → `by_model` filled, `by_service` omitted/`null`.
  - `group_by=service` → `by_service` filled, `by_model` omitted/`null`.

### Types (`internal/storage/storage.go` + `web/src/api/client.ts`)

- New `ServiceCostItem` struct, fields identical to `ModelCostItem` except the dimension field is `service string` (not `model`). JSON tag `service`.
- `CostSummaryResult` gains `ByService []ServiceCostItem` (json `by_service`) and `GroupBy string` (json `group_by`).
- `CostQuery` gains `GroupBy string`. The `GetCostSummary` signature is unchanged (the param is already a struct).
- `client.ts`: `CostSummary` adds optional `by_service?: ServiceCostItem[]` and `group_by: string`; add the `ServiceCostItem` interface mirroring `ModelCostItem`.

### Storage — `GetCostSummary`

`GetCostSummary` computes `overview` (always, exactly as today) plus **only** the requested breakdown:

- `q.GroupBy == "service"` → compute `by_service`; leave `by_model` nil.
- otherwise (default / `"model"`) → compute `by_model` as today; leave `by_service` nil.

**SQLite `by_service` query** (joins `spans` → `traces` for `service.name`; otherwise mirrors `by_model`'s select list, filter, and `ORDER BY cost DESC`):

```sql
SELECT
    COALESCE(json_extract(t.resource_attributes, '$."service.name"'), '(unknown)') AS service,
    COALESCE(sum(s.cost), 0) AS cost,
    COALESCE(sum(s.input_tokens), 0) AS input_tokens,
    COALESCE(sum(s.cache_creation_tokens), 0) AS cache_creation_tokens,
    COALESCE(sum(s.cache_read_tokens), 0) AS cache_read_tokens,
    COALESCE(sum(s.output_tokens), 0) AS output_tokens,
    count(DISTINCT s.trace_id_hex) AS trace_count
FROM spans s
JOIN traces t ON t.trace_id_hex = s.trace_id_hex
WHERE s.total_tokens IS NOT NULL
  AND t.start_time_ms >= ? AND t.start_time_ms <= ?
  AND t.cost IS NOT NULL AND t.cost > 0
GROUP BY json_extract(t.resource_attributes, '$."service.name"')
ORDER BY cost DESC
```

Scan into `ServiceCostItem`; derive `tokens` and `avg_cost` exactly as `by_model` does. Empty result → `[]ServiceCostItem{}` (never nil), matching the `by_model` convention.

**memstore `by_service`**: aggregate in memory over stored spans joined to their trace's `service.name`, mirroring the existing `by_model` aggregation logic in [`memstore.go:947`](../../../internal/storage/memstore.go#L947). Same `(unknown)` fallback for traces lacking `service.name`.

**chDB**: out of scope (see "Out of scope").

### Handler (`internal/api/cost_handler.go`)

- Parse `group_by` from query string; validate against `{model, service}` (empty → `model`; unknown value → `400`). Copy into `CostQuery.GroupBy`.
- The rest of `summary` is unchanged; the result already carries `GroupBy` and the populated slice.
- Result JSON: because the unpopulated slice is `nil`, it serializes as `null`/omitted — the frontend reads the slice indicated by `group_by`.

### Frontend (`web/src/views/CostDashboard.vue`)

- Add a segmented toggle directly below the `<h3>` heading, reusing the existing `.btn-preset` style (consistent with the period bar): `[By Model | By Service]`. Default `model`.
- The `<h3>` heading becomes dynamic so it always reflects the current view: `summary.group_by === 'service' ? t('costDashboard.costByService') : t('costDashboard.costByModel')`. The existing static `costByModel` heading is replaced by this computed one.
- State: `groupBy = ref<'model'|'service'>('model')`. On toggle: set `groupBy`, refetch via `getCostSummary(activePeriod, groupBy)`.
- `getCostSummary(period, groupBy)` in `client.ts`: append `&group_by=${groupBy}` to the request URL.
- Table: keep the same columns. The first column header + the row key/first-cell field swap based on `summary.group_by`:
  - `model` → header `t('costDashboard.model')`, rows from `summary.by_model`, first cell `m.model`.
  - `service` → header `t('costDashboard.service')`, rows from `summary.by_service`, first cell `s.service`.
  - Implement as `const rows = summary.group_by === 'service' ? summary.by_service : summary.by_model` and a computed first-column label/value, so the table markup is shared, not duplicated.
- Empty handling mirrors today: `rows.length === 0` → `costDashboard.noData`.

### i18n (`web/src/i18n/locales/en.ts` + `zh.ts`)

Add under `costDashboard`:
- `byModel` — "By Model" / "按模型" (toggle button)
- `byService` — "By Service" / "按服务" (toggle button)
- `costByService` — "Cost by Service" / "按服务成本" (dynamic heading; pairs with the existing `costByModel`)
- `service` — "Service" / "服务" (column header)

## Testing

- `sqlite_cost_test.go`: extend the existing `TestGetCostSummaryReconcilesAndBreaksOutCache` (or add a sibling test) to call `GetCostSummary` with `GroupBy: "service"`; assert grouping by `service.name`, the `(unknown)` fallback for a trace with no service, `trace_count` is distinct across services, tokens/avg_cost derive correctly, and rows are ordered by `cost DESC`.
- `memstore_cost_test.go`: parallel assertions for the in-memory path.
- `cost_handler_test.go`: `group_by` omitted → response has `by_model`, `by_service` null, `group_by=="model"`; `group_by=service` → reverse; `group_by=bogus` → `400`.

## Out of scope (YAGNI)

- No chart — the breakdown stays a table, matching the existing by-model view.
- No changes to overview cards (they are dimension-independent totals).
- No new endpoint — reuse `/cost-summary` with a query param.
- No chDB implementation of `by_service` — chDB does not implement `GetCostSummary` at all today (pre-existing gap); adding it is a separate task.
- No persistence of the selected dimension — default `model` on page load; toggle state is in-memory only.
