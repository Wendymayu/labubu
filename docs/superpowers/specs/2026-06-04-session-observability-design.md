# Session Observability

**Date**: 2026-06-04
**Status**: designed, not yet planned

## Motivation

Labubu currently treats each trace as an independent unit. For AI agent applications, a single user conversation spans multiple traces (one per turn), but there is no way to group, view, or analyze them together. Users need session-level observability to understand conversation quality — total token consumption, error rates, and latency across all turns.

This feature references Langfuse's session model: a lightweight grouping mechanism where traces share a `session_id` to form a session. The probe already reports `jiuwenclaw.session.id` as a span attribute on every span.

## Design

### Approach: Real-time aggregation from traces table

Sessions are **not** a separate entity. They emerge from grouping traces by `session_id`. This keeps the implementation minimal and avoids data consistency issues between two tables. When data volumes grow to millions of traces, this can evolve to a materialized sessions table without changing the API layer.

### Data Model Changes

**`Trace` struct** — add one field:

```go
type Trace struct {
    // ... existing fields unchanged ...
    SessionID string // from root span's jiuwenclaw.session.id attribute
}
```

**`Span` struct** — no changes. `jiuwenclaw.session.id` already lives in the `Attributes` map.

**chDB schema** — add column to traces table:

```sql
session_id String DEFAULT ''
```

Empty string means the trace has no session.

### Ingestion Pipeline

No changes to the receiver or pipeline. The only modification is in `aggregateTraces()`:

When identifying the root span (span with zero parent), extract `jiuwenclaw.session.id` from its attributes and set it on the `Trace` struct:

```go
if sid, ok := rootSpan.Attributes["jiuwenclaw.session.id"]; ok {
    trace.SessionID = sid
}
```

Both storage implementations (chDB and memstore) pick up this field automatically through the shared `aggregateTraces()` function.

### Query Layer (chdb_query.go)

Two new SQL builder functions:

**`buildSessionListSQL(filters SessionQuery)`**

```sql
SELECT session_id,
       COUNT(*) as trace_count,
       SUM(total_tokens) as total_tokens,
       SUM(duration_ms) as total_duration_ms,
       MAX(duration_ms) as max_duration_ms,
       AVG(duration_ms) as avg_duration_ms,
       SUM(if(status_code = 'ERROR', 1, 0)) as error_count,
       MIN(start_time_ms) as first_active_ms,
       MAX(start_time_ms) as last_active_ms
FROM traces
WHERE session_id != ''
  AND [service/time filters]
GROUP BY session_id
ORDER BY last_active_ms DESC
LIMIT {page_size} OFFSET {offset}
```

Plus a companion count query: `SELECT COUNT(DISTINCT session_id) FROM traces WHERE session_id != '' ...`

**`buildSessionTracesSQL(sessionID)`**

```sql
SELECT * FROM traces
WHERE session_id = '{sessionID}'
ORDER BY start_time_ms ASC
```

### Store Interface

Add two methods:

```go
type Store interface {
    // ... existing methods unchanged ...
    ListSessions(ctx context.Context, query SessionQuery) (*SessionListResult, error)
    GetSession(ctx context.Context, sessionID string) (*SessionDetail, error)
}
```

### New Types (storage.go)

```go
type SessionQuery struct {
    Page     int
    PageSize int
    Service  string
    Query    string // fuzzy match on session_id
    StartTimeMS uint64
    EndTimeMS   uint64
}

type SessionListItem struct {
    SessionID       string  `json:"session_id"`
    TraceCount      int     `json:"trace_count"`
    TotalTokens     *uint32 `json:"total_tokens"`
    TotalDurationMS uint64  `json:"total_duration_ms"`
    MaxDurationMS   uint64  `json:"max_duration_ms"`
    AvgDurationMS   float64 `json:"avg_duration_ms"`
    ErrorCount      int     `json:"error_count"`
    ErrorRate       float64 `json:"error_rate"`
    FirstActiveMS   uint64  `json:"first_active_ms"`
    LastActiveMS    uint64  `json:"last_active_ms"`
}

type SessionDetail struct {
    Session SessionListItem `json:"session"`
    Traces  []TraceListItem `json:"traces"`
}

type SessionListResult struct {
    Sessions   []SessionListItem `json:"sessions"`
    Pagination Pagination        `json:"pagination"`
}
```

### API Endpoints

**`GET /api/v1/sessions`** — Session list

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | 1 | Page number |
| `page_size` | int | 20 | Items per page, max 100 |
| `service` | string | "" | Filter by service.name |
| `q` | string | "" | Fuzzy search on session_id |
| `start` | uint64 | 0 | Start time filter (ms): only sessions where `last_active_ms >= start` |
| `end` | uint64 | 0 | End time filter (ms): only sessions where `last_active_ms <= end` |

Response:

```json
{
  "sessions": [{
    "session_id": "conv-abc-123",
    "trace_count": 5,
    "total_tokens": 12340,
    "total_duration_ms": 8500,
    "max_duration_ms": 3200,
    "avg_duration_ms": 1700.0,
    "error_count": 1,
    "error_rate": 0.2,
    "first_active_ms": 1717500000000,
    "last_active_ms": 1717500300000
  }],
  "pagination": { "page": 1, "page_size": 20, "total": 42 }
}
```

**`GET /api/v1/sessions/:sessionId`** — Session detail

Response:

```json
{
  "session": { /* SessionListItem */ },
  "traces": [
    {
      "trace_id_hex": "abc123...",
      "root_name": "chat.completion",
      "root_service": "my-agent",
      "duration_ms": 1700,
      "span_count": 8,
      "status": "OK",
      "total_tokens": 2468,
      "start_time_ms": 1717500100000
    }
  ]
}
```

### Handler Structure

New file `internal/api/session_handler.go`, parallel to `trace_handler.go`:

```
internal/api/
├── router.go            ← add session route registration
├── trace_handler.go     ← unchanged
├── session_handler.go   ← new
│   ├── ListSessions(w, r)
│   └── GetSession(w, r, sessionID)
└── ...
```

Router registration:

```go
mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
    path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions")
    if path == "" || path == "/" {
        sessionHandler.ListSessions(w, r)
        return
    }
    sessionID := strings.TrimPrefix(path, "/")
    sessionHandler.GetSession(w, r, sessionID)
})
mux.HandleFunc("/api/v1/sessions", sessionHandler.ListSessions)
```

### Frontend

**Router additions:**

```
/sessions            → SessionList.vue
/sessions/:sessionId → SessionDetail.vue
```

**Navigation:** Add "Sessions" entry in sidebar, between Traces and Dashboards.

**SessionList.vue:**

Filters (same style as TraceList):
- Text search: fuzzy match on session_id
- Service dropdown: reuses `/api/v1/services`
- Time range selector

Table columns:

| Column | Field | Notes |
|--------|-------|-------|
| Session ID | `session_id` | Monospace, truncated |
| Turns | `trace_count` | Number of traces in session |
| Total Tokens | `total_tokens` | Sum across all turns |
| Avg Latency | `avg_duration_ms` | Per-turn average |
| Max Latency | `max_duration_ms` | Slowest turn |
| Error Rate | `error_rate` | Color-coded: >50% red, >0 amber, 0 green |
| Last Active | `last_active_ms` | Relative time (e.g., "5 min ago") |

Click row → navigate to `/sessions/:sessionId`.

**SessionDetail.vue:**

Top summary card:
- Session ID, turn count, total tokens, error rate, average/max latency, session duration (last_active - first_active)

Below — chronological list of turns:
- Each turn shows: sequence number, root span name, status badge, duration, token count, service, timestamp
- Click turn → navigate to `/traces/:traceIdHex` for full waterfall view

**API client (client.ts):**

```typescript
interface SessionListItem {
  session_id: string
  trace_count: number
  total_tokens: number
  total_duration_ms: number
  max_duration_ms: number
  avg_duration_ms: number
  error_count: number
  error_rate: number
  first_active_ms: number
  last_active_ms: number
}

interface SessionDetail {
  session: SessionListItem
  traces: TraceListItem[]
}

interface SessionQuery {
  page?: number
  page_size?: number
  service?: string
  q?: string
  start?: number
  end?: number
}

function listSessions(query: SessionQuery): Promise<{ sessions: SessionListItem[], pagination: Pagination }>
function getSession(sessionId: string): Promise<SessionDetail>
```

### Future Evolution Path

When data volume grows beyond what GROUP BY can handle efficiently:

1. Add a `sessions` materialized table (ReplacingMergeTree)
2. On trace insert, asynchronously upsert the session's aggregated metrics
3. API layer unchanged — `ListSessions` reads from the materialized table instead of GROUP BY
4. The `session_id` column on traces table remains the foundation — no wasted work

## Files to Change

| File | Changes |
|------|---------|
| `internal/storage/storage.go` | Add `SessionID` to `Trace`, add `SessionQuery`, `SessionListItem`, `SessionDetail`, `SessionListResult` types, add `ListSessions`/`GetSession` to `Store` interface |
| `internal/storage/schema.sql` | Add `session_id` column to traces table |
| `internal/storage/chdb_query.go` | Add `buildSessionListSQL`, `buildSessionTracesSQL`, update `aggregateTraces()` to extract session_id, update insert SQL to include session_id |
| `internal/storage/chdb.go` | Implement `ListSessions`, `GetSession` using new SQL builders |
| `internal/storage/memstore.go` | Implement `ListSessions`, `GetSession` with in-memory grouping |
| `internal/api/router.go` | Register session routes |
| `internal/api/session_handler.go` | New file: `ListSessions`, `GetSession` handlers |
| `web/src/router.ts` | Add `/sessions` and `/sessions/:sessionId` routes |
| `web/src/App.vue` | Add Sessions nav entry in sidebar |
| `web/src/api/client.ts` | Add session types and API functions |
| `web/src/views/SessionList.vue` | New file: session list page |
| `web/src/views/SessionDetail.vue` | New file: session detail page |

## Edge Cases

- **Trace without session_id**: `session_id` defaults to empty string, excluded from session list queries (`WHERE session_id != ''`).
- **Very long session_id**: No enforced limit at storage level; UI truncates display with ellipsis.
- **Empty session**: A session_id that only appears in one trace still shows as a session with `trace_count=1`.
- **Null total_tokens**: When no spans have token data, `SUM(total_tokens)` returns NULL → handled as `null` in JSON, displayed as "—" in UI.
- **chDB vs memstore divergence**: Both implementations share `aggregateTraces()` for session_id extraction, ensuring consistent behavior.

## What Stays the Same

- All existing trace list/detail functionality
- OTLP ingestion pipeline (receiver, pipeline)
- Span-level data model
- Metrics and dashboard features
- Trace download/export features
