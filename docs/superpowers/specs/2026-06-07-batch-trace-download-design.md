# Batch Trace Download — Design Spec

**Date:** 2026-06-07
**Status:** approved

## Overview

Add batch trace download to the Trace List page (`TraceList.vue`). Users can select multiple traces via checkboxes and download them as a single OTLP JSON array file, reusable by existing `convertToOTLP()` backend logic.

## Motivation

- TraceDetail already supports single-trace download (JSON + OTLP)
- No way to export multiple traces at once
- OTLP format enables importing into Jaeger, Grafana, and other observability tools

## API

### New endpoint

```
POST /api/v1/traces/export
```

**Request (JSON body):**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `trace_ids` | `string[]` | yes | Hex-encoded trace IDs, max 100 |
| `format` | `string` | yes | `"otlp"` (reserved for future formats) |

**Response:** JSON array of OTLP resourceSpans objects, one element per trace.

**Status codes:**

| Code | Condition |
|------|-----------|
| 200 | Success (may be empty array if no traces found) |
| 400 | `trace_ids` empty, exceeds 100, or contains invalid ID |
| 500 | Storage/database error |

**Behavior:**
- Missing traces are silently skipped (no error)
- Each trace fetched via `Store.GetTrace()` + converted via `convertToOTLP()`
- Internal format conversion reused from `internal/api/otlp_trace.go`

### Implementation files

- Modify: `internal/api/trace_handler.go` — add `ExportTraces` method
- Modify: `internal/api/router.go` — register `POST /api/v1/traces/export`

## Frontend

### TraceList.vue changes

**Checkbox column:**
- `<input type="checkbox">` at the start of each row
- Header checkbox for select/deselect all on current page
- Selected state tracked in `selectedIds: Set<string>`
- Selection resets on page change (no cross-page state)

**Batch action bar:**
- Appears between filter bar and table when ≥1 trace selected
- Shows count: "已选 N 条" / "N selected"
- Download OTLP button → calls export API → blob download
- Clear selection button

**Download flow:**
1. User selects traces via checkboxes
2. Clicks "Download OTLP" button
3. Frontend POSTs `{ trace_ids [...], format: "otlp" }` to `/api/v1/traces/export`
4. On success: blob-downloads as `labubu-traces-export.json` (reuses existing `downloadBlob()` helper)
5. On error: shows alert with error message
6. Button shows loading state during request

**API client additions (`web/src/api/client.ts`):**
- Add `exportTraces(traceIds: string[], format: string)` function
- Type: `POST /api/v1/traces/export`

### File changes

- Modify: `web/src/views/TraceList.vue` — checkbox column, batch bar, download logic
- Modify: `web/src/api/client.ts` — `exportTraces()` function
- Modify: `web/src/i18n/locales/en.ts` — new strings (batch bar, download button)
- Modify: `web/src/i18n/locales/zh.ts` — new strings

## Edge Cases

| Case | Behavior |
|-------|----------|
| No traces selected | Download button hidden |
| All traces on page selected via header checkbox | Download enabled, all checked |
| Some selected traces deleted/missing on server | Silently skipped in response |
| Page change | Selection cleared |
| Export >100 traces | Frontend caps at current page max (≤100), backend returns 400 if exceeded |
| Network error during export | Alert shown, user can retry |
| Empty page with no traces | No checkboxes, no batch bar |

## Out of Scope

- Cross-page multi-select (keep vs lose selection on page change)
- Export in internal JSON format (OTLP only)
- Export from SessionDetail page
- CSV format
- Progress bar for large exports (100 traces is fast enough)
- New `format` values beyond `"otlp"`

## Visual Design

```
┌─ Filter bar ─────────────────────────────────────┐
│ 🔍 [__________] Service[▼] Status[▼] [Search] [Reset] │
├─ Batch bar ───────────────────────────────────────┤
│  [☑ 3 selected]   [Download OTLP]  [Clear]        │
├─ Table ───────────────────────────────────────────┤
│ ☑  | Name          | Service | Duration | ...     │
│ ☐  | chat-complete | my-app  | 2.3s     | ...     │
│ ☑  | embedding     | my-app  | 150ms    | ...     │
│ ☑  | tool-call     | agent   | 1.1s     | ...     │
│ ☐  | rag-query     | agent   | 800ms     | ...     │
├─ Pagination ──────────────────────────────────────┤
│ ← Prev  Page 1 of 5 (100 items)  Next →          │
└───────────────────────────────────────────────────┘
```
