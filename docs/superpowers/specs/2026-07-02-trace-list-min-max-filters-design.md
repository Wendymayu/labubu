# Trace List Min/Max Filters

**Date:** 2026-07-02
**Status:** Approved

## Goal

Let users filter the trace list (`/traces`) by **span count**, **duration**, and **cost** using greater-than / less-than thresholds, so they can quickly find large, slow, or expensive traces.

## Why

The trace list already displays `span_count`, `duration_ms`, and `cost` columns, but they are display-only — the only filters are service, status, text search, and time range. Sifting through hundreds of traces to find the few that are slow or costly is tedious. All three target fields are already materialized on each trace row at the list level (no aggregation needed), so the filters are low-effort.

## Scope

Min/Max filters for **Duration**, **Spans**, and **Cost**, rendered as column-header funnel popovers on the existing trace list table. This matches the established service/status column-filter pattern.

### Out of scope

- chDB post-aggregation (`HAVING`) filtering — the existing duration filter operates pre-`GROUP BY` on raw per-batch rows; the new span-count and cost filters will follow the same semantics. Consistent, not a regression.
- Currency-aware cost filtering — a numeric threshold filters regardless of currency.
- New pagination/sorting changes.

## Architecture

The data path is already complete for display. Three layers need changes:

```
Vue popover → client.ts TraceQuery → handler query params → storage.TraceQuery → store WHERE clause
```

`duration_ms` is already fully wired through every layer except the UI. `span_count` and `cost` need the same plumbing added at the storage struct, handler, and SQL/memstore layers.

## Backend Changes

### `storage.TraceQuery` (`internal/storage/storage.go:78`)

Add fields:

```go
MinSpanCount, MaxSpanCount uint16
MinCost,       MaxCost       float64
```

`MinDuration`/`MaxDuration` already exist.

### Handler (`internal/api/trace_handler.go:51-66`)

Parse and set on the query:

- `min_spans` / `max_spans` — `strconv.ParseUint`, cast to `uint16`
- `min_cost` / `max_cost` — `strconv.ParseFloat`

`min_duration` / `max_duration` are already parsed.

### OpenAPI (`internal/api/openapi.go:43-60`)

Document `min_spans`, `max_spans`, `min_cost`, `max_cost` (numeric, optional). `min_duration`/`max_duration` are already documented.

### SQLite (`internal/storage/sqlite_store.go` — `buildSqliteTraceWhereClause` ~line 1657)

Add clauses, mirroring the existing duration block:

```go
if q.MinSpanCount > 0 {
    clauses = append(clauses, `span_count >= ?`)
    args = append(args, q.MinSpanCount)
}
if q.MaxSpanCount > 0 {
    clauses = append(clauses, `span_count <= ?`)
    args = append(args, q.MaxSpanCount)
}
if q.MinCost > 0 {
    clauses = append(clauses, `cost >= ?`)
    args = append(args, q.MinCost)
}
if q.MaxCost > 0 {
    clauses = append(clauses, `cost <= ?`)
    args = append(args, q.MaxCost)
}
```

NULL `cost` is excluded by SQLite automatically — acceptable (a NULL-cost trace has no cost to compare).

### chDB (`internal/storage/chdb_query.go` — `buildTraceWhereClause` ~line 137)

Add the same four clauses on the raw columns, following the existing `duration_ms >= / <=` pattern. Pre-`GROUP BY` semantics match the existing duration filter.

### Memstore (`internal/storage/memstore.go` ~line 353)

Add the four checks in the in-memory filter loop, mirroring `MinDuration`/`MaxDuration`:

```go
if q.MinSpanCount > 0 && t.SpanCount < q.MinSpanCount { continue }
if q.MaxSpanCount > 0 && t.SpanCount > q.MaxSpanCount { continue }
if q.MinCost > 0 && (t.Cost == nil || *t.Cost < q.MinCost) { continue }
if q.MaxCost > 0 && (t.Cost == nil || *t.Cost > q.MaxCost) { continue }
```

Also fill in the currently-missing `Cost`/`CostCurrency` on the memstore `TraceListItem` builder (~line 385) so cost displays consistently on the memstore backend.

## Frontend Changes

### `client.ts` (`web/src/api/client.ts:82`)

Add to the `TraceQuery` interface:

```ts
min_spans?: number
max_spans?: number
min_cost?: number
max_cost?: number
```

`min_duration?` / `max_duration?` already exist. `listTraces` already serializes all `TraceQuery` fields to query params, so no change needed there.

### `TraceList.vue`

- **Filter state** (~line 242): extend `filters` ref with `min_duration, max_duration, min_spans, max_spans, min_cost, max_cost` (typed `number | ''`, empty string = unset).
- **`openFilter` union** (~line 248): extend to include `'duration' | 'spans' | 'cost'`.
- **Column-header popovers**: add a funnel button + popover on the Duration, Spans, and Cost column headers, copying the service/status popover pattern (~lines 36-61). Each popover contains two `<input type="number">` (Min, Max) plus **Apply** (close popover, reset page to 1, refetch) and **Clear** (blank both inputs, refetch).
- **Cost header**: currently a hardcoded string (~line 64) — replace with `t('traceList.cost')`.
- **`fetchTraces`** already spreads `filters.value` into `listTraces`, so the new fields flow through automatically once the ref has them.

## i18n

Add keys to `traceList.*` in both `web/src/i18n/locales/en.ts` and `zh.ts`:

- `cost`
- `minDuration`, `maxDuration`
- `minSpans`, `maxSpans`
- `minCost`, `maxCost`
- `apply`, `clear`

## Testing

- **Go**: table-driven test covering span-count and cost filtering on the SQLite store, mirroring the existing duration filter test. Add `min_spans`/`max_spans`/`min_cost`/`max_cost` cases.
- **TypeScript**: `cd web && npx vue-tsc --noEmit`.
- **Manual**: `make run`, verify each popover filters and clears correctly at http://localhost:8080.

## Risks

- **chDB multi-batch traces**: pre-`GROUP BY` filtering on raw rows can filter partial rows for traces split across ingest batches. This is identical to the existing duration-filter behavior — consistent, not a new issue. Documented here for awareness.
- **Nullable cost**: NULL-cost traces are excluded by `cost >= ?` in all backends. A user filtering for "cheap" traces (max_cost) won't see cost-less traces; acceptable since cost-less traces carry no cost signal.
