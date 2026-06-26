# Paginated Trace Logs + Full Download — Design

**Date:** 2026-06-25
**Status:** Approved (pending implementation)
**Branch:** feat/accurate-token-cost-aggregation

## Goal

On the trace detail page, stop loading every log of a trace into the UI at once. Instead:

1. **Display** logs paginated (a trace may have many logs).
2. **Download** all logs of a trace as a plain-text file.
3. **Preserve** the existing per-span log-count badges on the waterfall and the click-to-filter-by-span interaction — both currently depend on having all logs in memory, so they need backend support to survive pagination.

## Current Behavior (context)

- `GET /api/v1/logs/{traceIdHex}` (`Store.GetLogsByTrace`) returns **all** logs for a trace, no pagination, ordered by timestamp ASC. All three stores (chDB, SQLite, memstore) implement it with no `LIMIT`.
- `GET /api/v1/logs` (`Store.ListLogs`) is paginated (`page`/`page_size`, cap 100) and supports `trace_id`, `severity`, `event_name`, `q`, `start`, `end` — but **no `span_id` filter**.
- `TraceDetail.vue` fetches all logs into `traceLogs` on mount. Two things derive from that full in-memory set:
  - `logCounts` (per-span counts) → passed to `WaterfallChart`, which renders a `📋 N` badge per span.
  - `filterLogsBySpan` → clicking a span's badge filters logs client-side by `span_id_hex`.
- `downloadTraceOTLP` is the precedent download pattern: `fetch(url) → blob → downloadBlob()`.

## Architecture (Approach: maximize reuse, minimal new backend surface)

### Backend — new surface

**1. `span_id` filter on `GET /api/v1/logs`**

- Add `SpanID [8]byte` to `storage.LogQuery` (zero value = no span filter, mirroring the existing `TraceID [16]byte` convention). File: `internal/storage/storage.go`.
- `parseLogQuery` (in `internal/api/log_handler.go`): parse `span_id` query param as hex; accept only when `len(b) == 8`; `copy(q.SpanID[:], b)`.
- All three stores' `ListLogs`: when `q.SpanID != [8]byte{}`, add `AND span_id_hex = ?` (SQLite, memstore) / `AND span_id = unhex(...)` (chDB) to the existing WHERE clause.

**2. `GET /api/v1/logs/{traceIdHex}/counts` → `{ "counts": { "<span_id_hex>": N, … } }`**

- New `Store.GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error)` added to the `Store` interface.
  - SQLite: `SELECT span_id_hex, COUNT(*) FROM logs WHERE trace_id_hex = ? GROUP BY span_id_hex`.
  - memstore: iterate `m.logs` for matching `TraceID`, count per `SpanIDHex` (no SQL).
  - chDB: `SELECT hex(span_id) AS span_id_hex, COUNT(*) AS n FROM logs WHERE trace_id = unhex('%x') GROUP BY span_id_hex` (+ ` FORMAT JSONEachRow`), parsed into the map.
- Handler `LogHandler.GetLogCountsByTrace(w, r, traceIDHex)`: validate hex (16 bytes), call store, return `{"counts": map}` (empty map, not nil, when no logs).
- Dispatch in `LogHandler.ServeHTTP`: after trimming the `/api/v1/logs` prefix and obtaining `traceIDHex`, check for a `/counts` suffix:
  - `/{id}/counts` → `GetLogCountsByTrace`
  - `/{id}` → `GetLogsByTrace` (unchanged)
- No router change (`LogHandler` already owns the `/api/v1/logs` prefix).

### Backend — reused unchanged

- `GET /api/v1/logs/{traceIdHex}` (`getLogsByTrace`) — returns **all** logs; used for the download (needs the full set).
- `GET /api/v1/logs?trace_id=…&span_id=…&page=…&page_size=…` (`listLogs`) — paginated display.

### Frontend — `TraceDetail.vue`

Replace the all-logs `traceLogs` + client-side `filteredLogs` model with a paginated one:

- **State:** `pageLogs: LogRecord[]` (current page), `logPage: number` (1-based), `logPageSize: number` (= 50), `logTotal: number` (from `pagination.total` of the current query), `logCounts: Record<string, number>` (from counts endpoint), `logSpanFilter: string` (span_id hex or `''`).
- **On mount:** fetch trace (`getTrace`), fetch counts (`getLogCounts(traceIdHex)`) — feeds badges and the button label — and fetch page 1 (`listLogs({ trace_id, page: 1, page_size: 50 })`).
- **Overlay body:** render `pageLogs` with the existing inline-flowing log-item markup (no per-log scrollbar, normal wrapping). Below the list, a pagination bar: `◀ prev  ·  page X of Y  ·  next ▶` plus the current total. `prev`/`next` disabled at bounds; changing page refetches with the current span filter preserved.
- **Filter tag:** when `logSpanFilter` is set, show the existing `📋 Filtered: {name}` tag with `✕`; the name is resolved from `trace.spans` (find the span whose `span_id === logSpanFilter`, use its `name`; fall back to the hex). Clearing resets `logPage = 1`, `logSpanFilter = ''`, refetches.
- **`filterLogsBySpan(spanId)`:** set `logSpanFilter = spanId`, `logPage = 1`, `activeInsight = 'logs'`, fetch `listLogs({ trace_id, span_id, page: 1, page_size })`.
- **Download button** (`⬇`, `:title="t('logList.download')"`) in the insight overlay header, shown only when `activeInsight === 'logs'`. On click: `getLogsByTrace(traceIdHex)` → format **all** logs as plain text (see format below) → `downloadBlob(text, \`trace-${traceIdHex}-logs.txt\`)`. The download ignores the current span filter (always the full trace).
- **Button label** `📋 {{ t('logList.logCount', { count: totalLogCount }) }}` where `totalLogCount = Object.values(logCounts).reduce((a, b) => a + b, 0)` (sum of per-span counts = total trace logs; available as soon as counts load, before the overlay is opened).
- **WaterfallChart:** unchanged — still receives `:log-counts="logCounts"`, now sourced from the counts endpoint instead of client-side aggregation.
- Remove: `traceLogs`, `filteredLogs` computed, client-side `logCounts` aggregation, the all-logs `fetchTraceLogs` (replaced by paginated `fetchLogPage` + `fetchLogCounts`).

### Frontend — `client.ts`

- Add `span_id?: string` to `LogQuery`; pass it in `listLogs`'s query params.
- Add `getLogCounts(traceIdHex: string): Promise<{ counts: Record<string, number> }>` → `GET ${BASE_URL}/logs/${traceIdHex}/counts`.

### i18n

Add to both `web/src/i18n/locales/en.ts` and `zh.ts` under `logList`:

| key | en | zh |
|-----|----|----|
| `download` | `Download Logs` | `下载日志` |
| `prev` | `Prev` | `上一页` |
| `next` | `Next` | `下一页` |
| `pageOf` | `Page {page} of {total}` | `第 {page} / {total} 页` |

(`logList.logCount`, `logList.filteredBySpan`, `logList.noLogs`, `common.loading` already exist and are reused.)

## Plain-text download format

```
# trace e16c42c68388d1d891d3d0c80a9892ca — 42 logs

[2026-06-25 21:06:06.123] INFO  span=50800eb5931cb62d  event=chat.send
[WebSocketAgentServerClient] 发送请求(流式) payload: {"a2a_metadata": {}, …}
attrs: channel=web, user_id=u123
---
[2026-06-25 21:06:07.812] ERROR span=50800eb5931cb62d  event=rpc.timeout
<next log body>
---
```

Rules:
- Header line: `# trace {traceIdHex} — {N} logs` (N = total from `getLogsByTrace` result length).
- One block per log, blocks separated by a `---` line.
- Per block: `[<ts>] <SEVERITY>  span=<span_id_hex>  event=<event_name>` then the body on its own line(s), then `attrs: k=v, k2=v2` if attributes are non-empty.
- Timestamp `YYYY-MM-DD HH:mm:ss.SSS` derived from `new Date(log.timestamp)` (timestamp is milliseconds, matching the existing `formatLogTime` usage).
- Empty `body` → omit the body line. Empty `attributes` → omit the `attrs:` line. Empty `event_name` → render `-`.
- Body is emitted verbatim (not JSON-reformatted); pretty-printing is left to the reader's tooling.

## Data Flow

```
mount → getTrace ─────────────────────────────────────────────► trace
      → getLogCounts(traceId) ──► logCounts (badges + button label sum)
      → listLogs(trace_id, page=1, page_size=50) ──► pageLogs + logTotal

open Logs overlay ──► shows pageLogs + pagination bar

click span 📋 badge ──► filterLogsBySpan(spanId)
   → logSpanFilter=spanId, logPage=1
   → listLogs(trace_id, span_id, page=1) ──► pageLogs (that span) + logTotal

prev/next ──► logPage±1 → listLogs(trace_id, [span_id], page) ──► pageLogs

✕ clear filter ──► logSpanFilter='', logPage=1 → listLogs(trace_id, page=1)

⬇ download ──► getLogsByTrace(traceId) ──► format text ──► downloadBlob(.txt)
```

## Testing

- **Backend (Go table-driven):** in `internal/api/` (new or existing log handler test), cover:
  - `GET /api/v1/logs/{id}/counts` returns correct per-span counts and empty-map for a trace with no logs.
  - `GET /api/v1/logs?trace_id=…&span_id=…` returns only that span's logs; `span_id` + `trace_id` combine correctly; invalid `span_id` hex is ignored safely.
  - `GET /api/v1/logs/{id}` still returns all logs (regression guard for the download path).
- **Store-level:** `GetLogCountsByTrace` for sqlite + memstore (chDB behind the `nocgo` build tag, as usual).
- **Frontend:** `cd web && npx vue-tsc --noEmit` and `npx vite build` (no frontend test framework in the repo).

## Files Touched

**Backend:**
- `internal/storage/storage.go` — `LogQuery.SpanID`, `Store.GetLogCountsByTrace`.
- `internal/storage/sqlite_store.go` — `ListLogs` span filter, `GetLogCountsByTrace`.
- `internal/storage/memstore.go` — `ListLogs` span filter, `GetLogCountsByTrace`.
- `internal/storage/chdb.go` + `internal/storage/chdb_query.go` — `ListLogs` span filter, `GetLogCountsByTrace` + SQL builder.
- `internal/api/log_handler.go` — `parseLogQuery` span_id, `GetLogCountsByTrace` handler, `ServeHTTP` `/counts` dispatch.
- `internal/api/*_test.go` — tests above.

**Frontend:**
- `web/src/api/client.ts` — `LogQuery.span_id`, `listLogs` passes it, `getLogCounts`.
- `web/src/views/TraceDetail.vue` — paginated state/UX, download, counts fetch; remove all-logs model.
- `web/src/i18n/locales/en.ts` + `zh.ts` — new `logList` keys.

## Non-goals

- No changes to the standalone LogList view (`/logs`).
- No new full-text search UI in the overlay (the backend `q` param already exists; can be wired in later).
- No streaming/chunked download endpoint — the text is formatted client-side from the full `getLogsByTrace` result. Download inherently materializes the full set; pagination addresses the *display* memory concern, which is what was asked.
- No changes to the span-detail drawer.

## Decisions Resolved

- **Badges + click-to-filter:** preserved via a lightweight counts endpoint + `span_id` filter (Option A). The counts query loads no bodies, so it does not reintroduce the "many logs" memory cost.
- **Download format:** plain text (user choice), over JSON.
- **Page size:** 50.
- **Pagination UX:** prev/next + `page X of Y` + total.
- **Download mechanism:** client-side fetch + `downloadBlob`, reusing `getLogsByTrace`; no new download endpoint.
