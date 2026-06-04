# Session Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add session-level observability by grouping traces via `jiuwenclaw.session.id` span attribute, providing a session list with aggregated quality metrics (tokens, latency, error rate).

**Architecture:** Extract `session_id` from root span attributes during trace aggregation, store as a column on the traces table. Session queries use `GROUP BY session_id` for real-time aggregation. Two new REST endpoints (`/api/v1/sessions`, `/api/v1/sessions/:id`) and two new Vue pages.

**Tech Stack:** Go (storage, API), Vue 3 + TypeScript (frontend), chDB (ClickHouse embedded) / in-memory store

---

### Task 1: Add session_id to data model

**Files:**
- Modify: `internal/storage/storage.go:47-65` (Trace struct)
- Modify: `internal/storage/storage.go:99-104` (after Pagination, add session types)
- Modify: `internal/storage/storage.go:145-163` (Store interface)

- [ ] **Step 1: Add SessionID to Trace struct**

In `internal/storage/storage.go`, add `SessionID` to the `Trace` struct after `TotalTokens`:

```go
// Trace is the trace-level aggregate stored in the traces table.
type Trace struct {
	TraceID           [16]byte
	TraceIDHex        string
	RootSpanID        [8]byte
	RootName          string
	SpanCount         uint16
	StartTimeMS       uint64
	EndTimeMS         uint64
	DurationMS        uint64
	ResourceAttrs     map[string]string
	ResourceSchemaURL string
	ScopeName         string
	ScopeVersion      string
	ScopeAttrs        map[string]string
	ScopeSchemaURL    string
	StatusCode        int32
	StatusMessage     string
	TotalTokens       *uint32
	SessionID         string
}
```

- [ ] **Step 2: Add session query and response types**

In `internal/storage/storage.go`, add the following types after the `Pagination` struct (after line 104):

```go
// SessionQuery defines filters for listing sessions.
type SessionQuery struct {
	Page        int
	PageSize    int
	Service     string
	Query       string // session_id fuzzy search
	StartTimeMS uint64
	EndTimeMS   uint64
}

// SessionListItem is a session summary for the list view.
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

// SessionDetail is a session with all its traces.
type SessionDetail struct {
	Session SessionListItem `json:"session"`
	Traces  []TraceListItem `json:"traces"`
}

// SessionListResult holds a page of session summaries.
type SessionListResult struct {
	Sessions   []SessionListItem `json:"sessions"`
	Pagination Pagination        `json:"pagination"`
}
```

- [ ] **Step 3: Extend Store interface with session methods**

In `internal/storage/storage.go`, add two methods to the `Store` interface before `Close()`:

```go
	// ListSessions returns a paginated list of session summaries.
	ListSessions(ctx context.Context, q SessionQuery) (*SessionListResult, error)

	// GetSession returns a session summary and all its traces.
	GetSession(ctx context.Context, sessionID string) (*SessionDetail, error)
```

- [ ] **Step 4: Verify compilation fails (expected)**

Run: `go build ./internal/storage/...`
Expected: compilation errors in memstore.go and chdb.go because they don't implement the new Store methods yet.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/storage.go
git commit -m "feat: add session types and extend Store interface"
```

---

### Task 2: Update schema and query layer

**Files:**
- Modify: `internal/storage/schema.sql:4-26` (traces table)
- Modify: `internal/storage/chdb_query.go:45-77` (buildUpsertTraceSQL)
- Modify: `internal/storage/chdb_query.go:209-258` (aggregateTraces)
- Modify: `internal/storage/chdb_query.go` (add session SQL builders)

- [ ] **Step 1: Add session_id to schema.sql**

In `internal/storage/schema.sql`, add `session_id` column to the traces table. Replace the full traces table definition:

```sql
CREATE TABLE IF NOT EXISTS traces (
    trace_id               FixedString(16),
    trace_id_hex           String,
    root_span_id           FixedString(8),
    root_name              String,
    span_count             UInt16,
    start_time_ms          UInt64,
    end_time_ms            UInt64,
    duration_ms            UInt64,
    resource_attributes    Map(String, String),
    resource_schema_url    String,
    scope_name             String,
    scope_version          String,
    scope_attributes       Map(String, String),
    scope_schema_url       String,
    trace_state            String,
    dropped_span_count     UInt32,
    status_code            Enum8('UNSET'=0, 'OK'=1, 'ERROR'=2),
    status_message         String,
    total_tokens           Nullable(UInt32),
    session_id             String DEFAULT ''
)
ENGINE = MergeTree
ORDER BY (start_time_ms);
```

- [ ] **Step 2: Update buildUpsertTraceSQL to include session_id**

In `internal/storage/chdb_query.go`, update `buildUpsertTraceSQL` to include `session_id` in both the column list and values:

```go
func buildUpsertTraceSQL(trace Trace) string {
	totalTokens := nullUint32(trace.TotalTokens)

	return fmt.Sprintf(
		`INSERT INTO traces (
			trace_id, trace_id_hex, root_span_id, root_name, span_count,
			start_time_ms, end_time_ms, duration_ms,
			resource_attributes, resource_schema_url,
			scope_name, scope_version, scope_attributes, scope_schema_url,
			trace_state, dropped_span_count,
			status_code, status_message, total_tokens, session_id
		) VALUES (
			unhex('%s'), '%s', unhex('%s'), '%s', %d,
			%d, %d, %d,
			%s, '%s',
			'%s', '%s', %s, '%s',
			'%s', 0,
			%d, '%s', %s, '%s'
		)`,
		trace.TraceIDHex, trace.TraceIDHex,
		trace.RootSpanID,
		escapeSQL(trace.RootName), trace.SpanCount,
		trace.StartTimeMS, trace.EndTimeMS, trace.DurationMS,
		mapToSQL(trace.ResourceAttrs), escapeSQL(trace.ResourceSchemaURL),
		escapeSQL(trace.ScopeName), escapeSQL(trace.ScopeVersion),
		mapToSQL(trace.ScopeAttrs), escapeSQL(trace.ScopeSchemaURL),
		"",
		trace.StatusCode, escapeSQL(trace.StatusMessage),
		totalTokens, escapeSQL(trace.SessionID),
	)
}
```

- [ ] **Step 3: Update aggregateTraces to extract session_id from root span**

In `internal/storage/chdb_query.go`, inside the `aggregateTraces` function, add session_id extraction in the root span block. Find this section:

```go
		if isRootSpan(span.ParentSpanID) {
			t.RootSpanID = span.SpanID
			t.RootName = span.Name
			t.StatusCode = span.StatusCode
			t.StatusMessage = span.StatusMessage
		}
```

Replace with:

```go
		if isRootSpan(span.ParentSpanID) {
			t.RootSpanID = span.SpanID
			t.RootName = span.Name
			t.StatusCode = span.StatusCode
			t.StatusMessage = span.StatusMessage
			if sid, ok := span.Attributes["jiuwenclaw.session.id"]; ok {
				t.SessionID = sid
			}
		}
```

- [ ] **Step 4: Add session SQL builder functions**

In `internal/storage/chdb_query.go`, add these functions at the end of the file (before the helper section or after existing SQL builders):

```go
// buildSessionListWhereClause builds the WHERE clause for session queries.
func buildSessionListWhereClause(q SessionQuery) string {
	clauses := []string{"session_id != ''"}

	if q.Service != "" {
		clauses = append(clauses, fmt.Sprintf(
			"resource_attributes['service.name'] = '%s'", escapeSQL(q.Service),
		))
	}
	if q.Query != "" {
		clauses = append(clauses, fmt.Sprintf(
			"session_id LIKE '%%%s%%'", escapeSQL(q.Query),
		))
	}
	if q.StartTimeMS > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"start_time_ms >= %d", q.StartTimeMS,
		))
	}
	if q.EndTimeMS > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"start_time_ms <= %d", q.EndTimeMS,
		))
	}

	return " WHERE " + strings.Join(clauses, " AND ")
}

// buildSessionCountSQL builds a count query for distinct sessions.
func buildSessionCountSQL(q SessionQuery) string {
	return "SELECT count() AS count FROM (SELECT DISTINCT session_id FROM traces" + buildSessionListWhereClause(q) + ")"
}

// buildSessionListSQL builds a query that aggregates session metrics.
func buildSessionListSQL(q SessionQuery) string {
	offset := (q.Page - 1) * q.PageSize
	return fmt.Sprintf(
		`SELECT
			session_id,
			count() AS trace_count,
			sum(total_tokens) AS total_tokens,
			sum(duration_ms) AS total_duration_ms,
			max(duration_ms) AS max_duration_ms,
			round(avg(duration_ms), 1) AS avg_duration_ms,
			sum(if(status_code = 'ERROR', 1, 0)) AS error_count,
			round(sum(if(status_code = 'ERROR', 1, 0)) / count(), 4) AS error_rate,
			min(start_time_ms) AS first_active_ms,
			max(start_time_ms) AS last_active_ms
		FROM traces%s
		GROUP BY session_id
		ORDER BY last_active_ms DESC
		LIMIT %d OFFSET %d`,
		buildSessionListWhereClause(q), q.PageSize, offset,
	)
}

// buildSessionSummarySQL builds a query for a single session's aggregated summary.
func buildSessionSummarySQL(sessionID string) string {
	return fmt.Sprintf(
		`SELECT
			session_id,
			count() AS trace_count,
			sum(total_tokens) AS total_tokens,
			sum(duration_ms) AS total_duration_ms,
			max(duration_ms) AS max_duration_ms,
			round(avg(duration_ms), 1) AS avg_duration_ms,
			sum(if(status_code = 'ERROR', 1, 0)) AS error_count,
			round(sum(if(status_code = 'ERROR', 1, 0)) / count(), 4) AS error_rate,
			min(start_time_ms) AS first_active_ms,
			max(start_time_ms) AS last_active_ms
		FROM traces
		WHERE session_id = '%s'
		GROUP BY session_id`,
		escapeSQL(sessionID),
	)
}

// buildSessionTracesSQL builds a query to fetch all traces for a session.
func buildSessionTracesSQL(sessionID string) string {
	return fmt.Sprintf(
		`SELECT
			trace_id_hex, root_name, root_span_id,
			resource_attributes['service.name'] AS root_service,
			start_time_ms, duration_ms, span_count,
			toString(status_code) AS status,
			total_tokens
		FROM traces
		WHERE session_id = '%s'
		ORDER BY start_time_ms ASC`,
		escapeSQL(sessionID),
	)
}
```

- [ ] **Step 5: Commit**

```bash
git add internal/storage/schema.sql internal/storage/chdb_query.go
git commit -m "feat: add session_id to schema and query layer"
```

---

### Task 3: Implement memstore session methods

**Files:**
- Modify: `internal/storage/memstore.go:34-83` (InsertSpans — merge SessionID)
- Modify: `internal/storage/memstore.go` (add ListSessions and GetSession)

- [ ] **Step 1: Update InsertSpans to merge SessionID**

In `internal/storage/memstore.go`, inside the merge block of `InsertSpans`, add SessionID merging when RootName is updated. Find this block:

```go
			if trace.RootName != "" {
				existing.RootSpanID = trace.RootSpanID
				existing.RootName = trace.RootName
				existing.StatusCode = trace.StatusCode
				existing.StatusMessage = trace.StatusMessage
			}
```

Replace with:

```go
			if trace.RootName != "" {
				existing.RootSpanID = trace.RootSpanID
				existing.RootName = trace.RootName
				existing.StatusCode = trace.StatusCode
				existing.StatusMessage = trace.StatusMessage
				if trace.SessionID != "" {
					existing.SessionID = trace.SessionID
				}
			}
```

- [ ] **Step 2: Implement ListSessions**

Add this method to `memStore` in `internal/storage/memstore.go`:

```go
func (m *memStore) ListSessions(ctx context.Context, q SessionQuery) (*SessionListResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Group traces by session_id.
	type agg struct {
		traceCount      int
		totalTokens     uint32
		hasTokens       bool
		totalDurationMS uint64
		maxDurationMS   uint64
		errorCount      int
		firstActiveMS   uint64
		lastActiveMS    uint64
	}
	groups := make(map[string]*agg)

	for _, t := range m.traces {
		if t.SessionID == "" {
			continue
		}
		if q.Service != "" {
			if t.ResourceAttrs["service.name"] != q.Service {
				continue
			}
		}
		if q.Query != "" {
			if !containsSubstring(t.SessionID, q.Query) {
				continue
			}
		}
		if q.StartTimeMS > 0 && t.StartTimeMS < q.StartTimeMS {
			continue
		}
		if q.EndTimeMS > 0 && t.StartTimeMS > q.EndTimeMS {
			continue
		}

		g, ok := groups[t.SessionID]
		if !ok {
			g = &agg{firstActiveMS: t.StartTimeMS, lastActiveMS: t.StartTimeMS}
			groups[t.SessionID] = g
		}
		g.traceCount++
		if t.TotalTokens != nil {
			g.totalTokens += *t.TotalTokens
			g.hasTokens = true
		}
		g.totalDurationMS += t.DurationMS
		if t.DurationMS > g.maxDurationMS {
			g.maxDurationMS = t.DurationMS
		}
		if t.StatusCode == 2 { // ERROR
			g.errorCount++
		}
		if t.StartTimeMS < g.firstActiveMS {
			g.firstActiveMS = t.StartTimeMS
		}
		if t.StartTimeMS > g.lastActiveMS {
			g.lastActiveMS = t.StartTimeMS
		}
	}

	// Convert to slice and sort by last_active_ms descending.
	type sessionEntry struct {
		id  string
		agg *agg
	}
	entries := make([]sessionEntry, 0, len(groups))
	for id, g := range groups {
		entries = append(entries, sessionEntry{id, g})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].agg.lastActiveMS > entries[j].agg.lastActiveMS
	})

	total := len(entries)

	// Paginate.
	start := (q.Page - 1) * q.PageSize
	if start > total {
		start = total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	page := entries[start:end]

	items := make([]SessionListItem, len(page))
	for i, e := range page {
		item := SessionListItem{
			SessionID:       e.id,
			TraceCount:      e.agg.traceCount,
			TotalDurationMS: e.agg.totalDurationMS,
			MaxDurationMS:   e.agg.maxDurationMS,
			ErrorCount:      e.agg.errorCount,
			FirstActiveMS:   e.agg.firstActiveMS,
			LastActiveMS:    e.agg.lastActiveMS,
		}
		if e.agg.hasTokens {
			tok := e.agg.totalTokens
			item.TotalTokens = &tok
		}
		if e.agg.traceCount > 0 {
			item.AvgDurationMS = float64(e.agg.totalDurationMS) / float64(e.agg.traceCount)
			item.ErrorRate = float64(e.agg.errorCount) / float64(e.agg.traceCount)
		}
		items[i] = item
	}

	return &SessionListResult{
		Sessions: items,
		Pagination: Pagination{
			Page:     q.Page,
			PageSize: q.PageSize,
			Total:    total,
		},
	}, nil
}
```

- [ ] **Step 3: Implement GetSession**

Add this method to `memStore` in `internal/storage/memstore.go`:

```go
func (m *memStore) GetSession(ctx context.Context, sessionID string) (*SessionDetail, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect traces for this session.
	var sessionTraces []Trace
	for _, t := range m.traces {
		if t.SessionID == sessionID {
			sessionTraces = append(sessionTraces, t)
		}
	}

	if len(sessionTraces) == 0 {
		return nil, nil
	}

	// Sort by start_time_ms ascending.
	sort.Slice(sessionTraces, func(i, j int) bool {
		return sessionTraces[i].StartTimeMS < sessionTraces[j].StartTimeMS
	})

	// Build session summary.
	var totalTokens uint32
	var hasTokens bool
	var totalDurationMS, maxDurationMS uint64
	var errorCount int
	firstActiveMS := sessionTraces[0].StartTimeMS
	lastActiveMS := sessionTraces[0].StartTimeMS

	traces := make([]TraceListItem, len(sessionTraces))
	for i, t := range sessionTraces {
		traces[i] = TraceListItem{
			TraceIDHex:  TraceIDToHex(t.TraceID),
			RootSpanID:  SpanIDToHex(t.RootSpanID),
			RootName:    t.RootName,
			RootService: t.ResourceAttrs["service.name"],
			StartTimeMS: t.StartTimeMS,
			DurationMS:  t.DurationMS,
			SpanCount:   t.SpanCount,
			Status:      StatusCodeToString(t.StatusCode),
			TotalTokens: t.TotalTokens,
		}
		if t.TotalTokens != nil {
			totalTokens += *t.TotalTokens
			hasTokens = true
		}
		totalDurationMS += t.DurationMS
		if t.DurationMS > maxDurationMS {
			maxDurationMS = t.DurationMS
		}
		if t.StatusCode == 2 {
			errorCount++
		}
		if t.StartTimeMS < firstActiveMS {
			firstActiveMS = t.StartTimeMS
		}
		if t.StartTimeMS > lastActiveMS {
			lastActiveMS = t.StartTimeMS
		}
	}

	summary := SessionListItem{
		SessionID:       sessionID,
		TraceCount:      len(sessionTraces),
		TotalDurationMS: totalDurationMS,
		MaxDurationMS:   maxDurationMS,
		ErrorCount:      errorCount,
		FirstActiveMS:   firstActiveMS,
		LastActiveMS:    lastActiveMS,
	}
	if hasTokens {
		summary.TotalTokens = &totalTokens
	}
	if len(sessionTraces) > 0 {
		summary.AvgDurationMS = float64(totalDurationMS) / float64(len(sessionTraces))
		summary.ErrorRate = float64(errorCount) / float64(len(sessionTraces))
	}

	return &SessionDetail{
		Session: summary,
		Traces:  traces,
	}, nil
}
```

- [ ] **Step 4: Verify memstore compiles**

Run: `go build ./internal/storage/...`
Expected: no errors (memstore now implements all Store methods).

- [ ] **Step 5: Commit**

```bash
git add internal/storage/memstore.go
git commit -m "feat: implement session methods in memstore"
```

---

### Task 4: Implement chDB session methods

**Files:**
- Modify: `internal/storage/chdb.go` (add ListSessions, GetSession, and parse helpers)

- [ ] **Step 1: Implement ListSessions in chDB store**

Add this method to `chDBStore` in `internal/storage/chdb.go` (after `GetServices`):

```go
func (s *chDBStore) ListSessions(ctx context.Context, q SessionQuery) (*SessionListResult, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}

	countSQL := buildSessionCountSQL(q)
	countResult, err := s.querySQL(countSQL + " FORMAT JSONEachRow")
	if err != nil {
		return nil, fmt.Errorf("count sessions: %w", err)
	}
	total := parseCount(countResult)

	dataSQL := buildSessionListSQL(q)
	dataResult, err := s.querySQL(dataSQL + " FORMAT JSONEachRow")
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	sessions, err := parseSessionListItems(dataResult)
	if err != nil {
		return nil, fmt.Errorf("parse session list: %w", err)
	}

	return &SessionListResult{
		Sessions: sessions,
		Pagination: Pagination{
			Page:     q.Page,
			PageSize: q.PageSize,
			Total:    total,
		},
	}, nil
}
```

- [ ] **Step 2: Implement GetSession in chDB store**

Add this method to `chDBStore` in `internal/storage/chdb.go` (after `ListSessions`):

```go
func (s *chDBStore) GetSession(ctx context.Context, sessionID string) (*SessionDetail, error) {
	// Get the aggregated session summary using exact-match query.
	summarySQL := buildSessionSummarySQL(sessionID) + " FORMAT JSONEachRow"
	summaryResult, err := s.querySQL(summarySQL)
	if err != nil {
		return nil, fmt.Errorf("get session summary: %w", err)
	}
	sessions, err := parseSessionListItems(summaryResult)
	if err != nil {
		return nil, fmt.Errorf("parse session summary: %w", err)
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	// Get all traces for this session.
	tracesSQL := buildSessionTracesSQL(sessionID) + " FORMAT JSONEachRow"
	tracesResult, err := s.querySQL(tracesSQL)
	if err != nil {
		return nil, fmt.Errorf("get session traces: %w", err)
	}
	traces, err := parseTraceListItems(tracesResult)
	if err != nil {
		return nil, fmt.Errorf("parse session traces: %w", err)
	}

	return &SessionDetail{
		Session: sessions[0],
		Traces:  traces,
	}, nil
}
```

- [ ] **Step 3: Add parseSessionListItems helper**

Add this function in `internal/storage/chdb.go` (near the other parse helpers):

```go
func parseSessionListItems(result string) ([]SessionListItem, error) {
	var items []SessionListItem
	lines := splitLines(result)
	for _, line := range lines {
		if line == "" {
			continue
		}
		var raw struct {
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
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse session item: %w (line: %s)", err, line)
		}
		items = append(items, SessionListItem{
			SessionID:       raw.SessionID,
			TraceCount:      raw.TraceCount,
			TotalTokens:     raw.TotalTokens,
			TotalDurationMS: raw.TotalDurationMS,
			MaxDurationMS:   raw.MaxDurationMS,
			AvgDurationMS:   raw.AvgDurationMS,
			ErrorCount:      raw.ErrorCount,
			ErrorRate:       raw.ErrorRate,
			FirstActiveMS:   raw.FirstActiveMS,
			LastActiveMS:    raw.LastActiveMS,
		})
	}
	return items, nil
}
```

- [ ] **Step 4: Verify chDB compiles (build tag check)**

Run: `CGO_ENABLED=1 go build -tags "cgo,local_engine" ./internal/storage/...`
Expected: no errors (if libchdb is available). If not, skip — the build tag prevents this file from being compiled in the nocgo path.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/chdb.go
git commit -m "feat: implement session methods in chDB store"
```

---

### Task 5: Add session handler and router

**Files:**
- Create: `internal/api/session_handler.go`
- Modify: `internal/api/router.go:11-46` (register routes)

- [ ] **Step 1: Create session_handler.go**

Create `internal/api/session_handler.go`:

```go
package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/labubu/labubu/internal/storage"
)

// SessionHandler holds HTTP handlers for session endpoints.
type SessionHandler struct {
	store storage.Store
}

// NewSessionHandler creates a new SessionHandler.
func NewSessionHandler(store storage.Store) *SessionHandler {
	return &SessionHandler{store: store}
}

// ListSessions handles GET /api/v1/sessions.
func (h *SessionHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	startMS, _ := strconv.ParseUint(q.Get("start"), 10, 64)
	endMS, _ := strconv.ParseUint(q.Get("end"), 10, 64)

	query := storage.SessionQuery{
		Page:        page,
		PageSize:    pageSize,
		Service:     q.Get("service"),
		Query:       q.Get("q"),
		StartTimeMS: startMS,
		EndTimeMS:   endMS,
	}

	result, err := h.store.ListSessions(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("list sessions: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// GetSession handles GET /api/v1/sessions/:sessionId.
func (h *SessionHandler) GetSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}

	detail, err := h.store.GetSession(r.Context(), sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get session: %v", err)})
		return
	}
	if detail == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	writeJSON(w, http.StatusOK, detail)
}
```

- [ ] **Step 2: Register session routes in router.go**

In `internal/api/router.go`, update `NewRouter` to accept and register the session handler. First, change the function signature:

```go
func NewRouter(traceHandler *TraceHandler, metricsHandler *MetricsHandler, dashboardHandler *DashboardHandler, sessionHandler *SessionHandler) http.Handler {
```

Then add session routes after the dashboard routes block (before the health check):

```go
	// API routes — sessions.
	if sessionHandler != nil {
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
	}
```

- [ ] **Step 3: Update NewRouter call site in main.go**

In `cmd/labubu/main.go`, find line 108:

```go
	dashboardHandler := api.NewDashboardHandler(*dashboardsDir)
	router := api.NewRouter(traceHandler, metricsHandler, dashboardHandler)
```

Replace with:

```go
	dashboardHandler := api.NewDashboardHandler(*dashboardsDir)
	sessionHandler := api.NewSessionHandler(store)
	router := api.NewRouter(traceHandler, metricsHandler, dashboardHandler, sessionHandler)
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/api/session_handler.go internal/api/router.go cmd/labubu/main.go
git commit -m "feat: add session API handler and router routes"
```

---

### Task 6: Add handler tests

**Files:**
- Create: `internal/api/session_handler_test.go`

- [ ] **Step 1: Update mockStore in trace_handler_test.go to satisfy new Store interface**

In `internal/api/trace_handler_test.go`, add stub methods to `handlerMockStore`:

```go
func (m *handlerMockStore) ListSessions(ctx context.Context, q storage.SessionQuery) (*storage.SessionListResult, error) {
	return nil, nil
}

func (m *handlerMockStore) GetSession(ctx context.Context, sessionID string) (*storage.SessionDetail, error) {
	return nil, nil
}
```

- [ ] **Step 2: Create session_handler_test.go**

Create `internal/api/session_handler_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

// sessionMockStore is a mock for session handler testing.
type sessionMockStore struct {
	handlerMockStore // embed the existing mock for unused methods

	sessions    *storage.SessionListResult
	sessionDetail *storage.SessionDetail
	sessionErr  error
}

func (m *sessionMockStore) ListSessions(ctx context.Context, q storage.SessionQuery) (*storage.SessionListResult, error) {
	return m.sessions, m.sessionErr
}

func (m *sessionMockStore) GetSession(ctx context.Context, sessionID string) (*storage.SessionDetail, error) {
	return m.sessionDetail, m.sessionErr
}

func TestListSessions(t *testing.T) {
	tokens := uint32(5000)
	store := &sessionMockStore{
		sessions: &storage.SessionListResult{
			Sessions: []storage.SessionListItem{
				{
					SessionID:       "conv-123",
					TraceCount:      3,
					TotalTokens:     &tokens,
					TotalDurationMS: 4500,
					MaxDurationMS:   2000,
					AvgDurationMS:   1500,
					ErrorCount:      0,
					ErrorRate:       0,
					FirstActiveMS:   1717500000000,
					LastActiveMS:    1717500300000,
				},
			},
			Pagination: storage.Pagination{Page: 1, PageSize: 20, Total: 1},
		},
	}

	handler := NewSessionHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?page=1", nil)
	rec := httptest.NewRecorder()

	handler.ListSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.SessionListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(result.Sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(result.Sessions))
	}
	if result.Sessions[0].SessionID != "conv-123" {
		t.Errorf("expected session_id 'conv-123', got '%s'", result.Sessions[0].SessionID)
	}
	if result.Sessions[0].TraceCount != 3 {
		t.Errorf("expected trace_count 3, got %d", result.Sessions[0].TraceCount)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	store := &sessionMockStore{
		sessionDetail: nil,
	}

	handler := NewSessionHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/nonexistent", nil)
	rec := httptest.NewRecorder()

	handler.GetSession(rec, req, "nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetSessionEmptyID(t *testing.T) {
	store := &sessionMockStore{}
	handler := NewSessionHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/", nil)
	rec := httptest.NewRecorder()

	handler.GetSession(rec, req, "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty session_id, got %d", rec.Code)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test -v ./internal/api/...`
Expected: all tests pass, including existing trace tests and new session tests.

- [ ] **Step 4: Commit**

```bash
git add internal/api/trace_handler_test.go internal/api/session_handler_test.go
git commit -m "test: add session handler tests"
```

---

### Task 7: Frontend — API client, router, and navigation

**Files:**
- Modify: `web/src/api/client.ts` (add session types and functions)
- Modify: `web/src/router.ts` (add session routes)
- Modify: `web/src/App.vue:6-8` (add nav entry)

- [ ] **Step 1: Add session types and API functions to client.ts**

In `web/src/api/client.ts`, add at the end of the file:

```typescript
// --- Session types and API ---

export interface SessionListItem {
  session_id: string
  trace_count: number
  total_tokens?: number
  total_duration_ms: number
  max_duration_ms: number
  avg_duration_ms: number
  error_count: number
  error_rate: number
  first_active_ms: number
  last_active_ms: number
}

export interface SessionDetail {
  session: SessionListItem
  traces: TraceListItem[]
}

export interface SessionQuery {
  page?: number
  page_size?: number
  service?: string
  q?: string
  start?: number
  end?: number
}

export interface SessionListResponse {
  sessions: SessionListItem[]
  pagination: Pagination
}

export async function listSessions(query: SessionQuery): Promise<SessionListResponse> {
  return get<SessionListResponse>(`${BASE_URL}/sessions`, {
    page: query.page,
    page_size: query.page_size,
    service: query.service,
    q: query.q,
    start: query.start,
    end: query.end,
  })
}

export async function getSession(sessionId: string): Promise<SessionDetail> {
  return get<SessionDetail>(`${BASE_URL}/sessions/${encodeURIComponent(sessionId)}`)
}
```

- [ ] **Step 2: Add session routes to router.ts**

Replace `web/src/router.ts` entirely:

```typescript
import { createRouter, createWebHistory } from 'vue-router'
import TraceList from './views/TraceList.vue'
import TraceDetail from './views/TraceDetail.vue'
import SessionList from './views/SessionList.vue'
import SessionDetail from './views/SessionDetail.vue'
import Dashboard from './views/Dashboard.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/traces' },
    { path: '/traces', name: 'trace-list', component: TraceList },
    { path: '/traces/:id', name: 'trace-detail', component: TraceDetail },
    { path: '/sessions', name: 'session-list', component: SessionList },
    { path: '/sessions/:sessionId', name: 'session-detail', component: SessionDetail },
    { path: '/dashboards', name: 'dashboards', component: Dashboard },
  ]
})
```

- [ ] **Step 3: Add Sessions nav entry in App.vue**

In `web/src/App.vue`, add a Sessions link in the nav:

```html
      <nav class="app-nav">
        <router-link to="/traces">Trace</router-link>
        <router-link to="/sessions">Sessions</router-link>
        <router-link to="/dashboards">Metrics</router-link>
      </nav>
```

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts web/src/router.ts web/src/App.vue
git commit -m "feat: add session API client, routes, and navigation"
```

---

### Task 8: Frontend — SessionList.vue

**Files:**
- Create: `web/src/views/SessionList.vue`

- [ ] **Step 1: Create SessionList.vue**

Create `web/src/views/SessionList.vue`:

```vue
<template>
  <div class="session-list">
    <div class="filters">
      <input
        v-model="filters.q"
        type="text"
        placeholder="Search sessions..."
        class="search-input"
        @keyup.enter="search"
      />
      <select v-model="filters.service" class="filter-select">
        <option value="">All services</option>
        <option v-for="svc in services" :key="svc" :value="svc">{{ svc }}</option>
      </select>
      <button @click="search" class="btn btn-primary">Search</button>
      <button @click="reset" class="btn">Reset</button>
    </div>

    <div v-if="loading" class="loading">Loading...</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <template v-else>
      <table class="trace-table" v-if="sessions.length > 0">
        <thead>
          <tr>
            <th>Session ID</th>
            <th>Turns</th>
            <th>Total Tokens</th>
            <th>Avg Latency</th>
            <th>Max Latency</th>
            <th>Error Rate</th>
            <th>Last Active</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="session in sessions"
            :key="session.session_id"
            @click="goToSession(session.session_id)"
            class="trace-row"
          >
            <td class="cell-session-id">{{ session.session_id }}</td>
            <td>{{ session.trace_count }}</td>
            <td class="cell-tokens">{{ formatTokens(session.total_tokens) }}</td>
            <td>{{ formatDuration(session.avg_duration_ms) }}</td>
            <td>{{ formatDuration(session.max_duration_ms) }}</td>
            <td>
              <span :class="['error-rate', errorRateClass(session.error_rate)]">
                {{ (session.error_rate * 100).toFixed(0) }}%
              </span>
            </td>
            <td class="cell-time">{{ formatRelativeTime(session.last_active_ms) }}</td>
          </tr>
        </tbody>
      </table>

      <div v-else class="empty">No sessions found.</div>

      <div class="pagination" v-if="pagination.total > 0">
        <button
          :disabled="pagination.page <= 1"
          @click="goToPage(pagination.page - 1)"
          class="btn"
        >
          ← Prev
        </button>
        <span class="page-info">
          Page {{ pagination.page }} of {{ totalPages }} ({{ pagination.total }} sessions)
        </span>
        <button
          :disabled="pagination.page >= totalPages"
          @click="goToPage(pagination.page + 1)"
          class="btn"
        >
          Next →
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { listSessions, getServices, type SessionListItem, type Pagination } from '../api/client'

const router = useRouter()

const sessions = ref<SessionListItem[]>([])
const pagination = ref<Pagination>({ page: 1, page_size: 20, total: 0 })
const services = ref<string[]>([])
const loading = ref(true)
const error = ref('')

const filters = ref({
  q: '',
  service: '',
})

const totalPages = computed(() => {
  return Math.max(1, Math.ceil(pagination.value.total / pagination.value.page_size))
})

async function fetchSessions(page = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await listSessions({ ...filters.value, page, page_size: 20 })
    sessions.value = result.sessions
    pagination.value = result.pagination
  } catch (e: any) {
    error.value = e.message || 'Failed to load sessions'
  } finally {
    loading.value = false
  }
}

async function fetchServices() {
  try {
    services.value = await getServices()
  } catch {
    // Non-critical.
  }
}

function search() {
  fetchSessions(1)
}

function reset() {
  filters.value = { q: '', service: '' }
  fetchSessions(1)
}

function goToPage(page: number) {
  fetchSessions(page)
}

function goToSession(sessionId: string) {
  router.push({ name: 'session-detail', params: { sessionId } })
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
  const mins = Math.floor(ms / 60000)
  const secs = ((ms % 60000) / 1000).toFixed(0)
  return `${mins}m ${secs}s`
}

function formatTokens(tokens?: number): string {
  if (tokens == null) return '-'
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return String(tokens)
}

function formatRelativeTime(ms: number): string {
  const now = Date.now()
  const diff = now - ms
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)} min ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return new Date(ms).toLocaleDateString()
}

function errorRateClass(rate: number): string {
  if (rate > 0.5) return 'error-high'
  if (rate > 0) return 'error-medium'
  return 'error-none'
}

onMounted(() => {
  fetchSessions()
  fetchServices()
})
</script>

<style scoped>
.session-list { max-width: 1400px; }
.filters { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; }
.search-input { flex: 1; min-width: 200px; padding: 8px 12px; background: #1e293b; border: 1px solid #334155; border-radius: 6px; color: #e2e8f0; font-size: 14px; }
.filter-select { padding: 8px 12px; background: #1e293b; border: 1px solid #334155; border-radius: 6px; color: #e2e8f0; font-size: 14px; }
.btn { padding: 8px 16px; background: #334155; border: 1px solid #475569; border-radius: 6px; color: #e2e8f0; cursor: pointer; font-size: 14px; }
.btn:hover { background: #475569; }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-primary { background: #2563eb; border-color: #2563eb; }
.btn-primary:hover { background: #1d4ed8; }
.loading, .error, .empty { text-align: center; padding: 60px 20px; color: #94a3b8; }
.error { color: #f87171; }
.trace-table { width: 100%; border-collapse: collapse; }
.trace-table th { text-align: left; padding: 10px 12px; font-size: 12px; color: #94a3b8; text-transform: uppercase; border-bottom: 1px solid #334155; }
.trace-table td { padding: 10px 12px; font-size: 14px; border-bottom: 1px solid #1e293b; }
.trace-row { cursor: pointer; }
.trace-row:hover { background: #1e293b; }
.cell-session-id { font-family: 'Courier New', monospace; font-size: 13px; color: #38bdf8; max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cell-tokens { color: #c4b5fd; font-weight: 600; }
.cell-time { color: #94a3b8; font-size: 13px; white-space: nowrap; }
.error-rate { font-weight: 600; font-size: 13px; }
.error-high { color: #fca5a5; }
.error-medium { color: #fbbf24; }
.error-none { color: #6ee7b7; }
.pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 20px; }
.page-info { font-size: 14px; color: #94a3b8; }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/SessionList.vue
git commit -m "feat: add SessionList.vue page"
```

---

### Task 9: Frontend — SessionDetail.vue

**Files:**
- Create: `web/src/views/SessionDetail.vue`

- [ ] **Step 1: Create SessionDetail.vue**

Create `web/src/views/SessionDetail.vue`:

```vue
<template>
  <div class="session-detail">
    <div class="back-link">
      <router-link to="/sessions">← Back to sessions</router-link>
    </div>

    <div v-if="loading" class="loading">Loading...</div>
    <div v-else-if="error" class="error">{{ error }}</div>

    <template v-else-if="detail">
      <div class="session-summary">
        <h2>Session: {{ detail.session.session_id }}</h2>
        <div class="summary-grid">
          <div class="summary-item">
            <span class="summary-label">Turns</span>
            <span class="summary-value">{{ detail.session.trace_count }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Total Tokens</span>
            <span class="summary-value token-highlight">{{ formatTokens(detail.session.total_tokens) }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Error Rate</span>
            <span :class="['summary-value', errorRateClass(detail.session.error_rate)]">
              {{ (detail.session.error_rate * 100).toFixed(0) }}%
            </span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Avg Latency</span>
            <span class="summary-value">{{ formatDuration(detail.session.avg_duration_ms) }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Max Latency</span>
            <span class="summary-value">{{ formatDuration(detail.session.max_duration_ms) }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Duration</span>
            <span class="summary-value">{{ formatDuration(detail.session.last_active_ms - detail.session.first_active_ms) }}</span>
          </div>
        </div>
      </div>

      <h3 class="turns-heading">Turns ({{ detail.traces.length }})</h3>

      <div class="turns-list">
        <div
          v-for="(trace, idx) in detail.traces"
          :key="trace.trace_id_hex"
          class="turn-row"
          @click="goToTrace(trace.trace_id_hex)"
        >
          <span class="turn-number">#{{ idx + 1 }}</span>
          <span class="turn-name">{{ trace.root_name }}</span>
          <span :class="['status-badge', statusClass(trace.status)]">{{ trace.status }}</span>
          <span class="turn-duration">{{ formatDuration(trace.duration_ms) }}</span>
          <span class="turn-tokens">{{ formatTokens(trace.total_tokens) }}</span>
          <span class="turn-service">{{ trace.root_service }}</span>
          <span class="turn-time">{{ formatTime(trace.start_time_ms) }}</span>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getSession, type SessionDetail } from '../api/client'

const route = useRoute()
const router = useRouter()
const sessionId = route.params.sessionId as string

const detail = ref<SessionDetail | null>(null)
const loading = ref(true)
const error = ref('')

async function fetchSession() {
  loading.value = true
  error.value = ''
  try {
    detail.value = await getSession(sessionId)
  } catch (e: any) {
    error.value = e.message || 'Failed to load session'
  } finally {
    loading.value = false
  }
}

function goToTrace(traceIdHex: string) {
  router.push({ name: 'trace-detail', params: { id: traceIdHex } })
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
  const mins = Math.floor(ms / 60000)
  const secs = ((ms % 60000) / 1000).toFixed(0)
  return `${mins}m ${secs}s`
}

function formatTokens(tokens?: number): string {
  if (tokens == null) return '-'
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return String(tokens)
}

function formatTime(ms: number): string {
  return new Date(ms).toLocaleTimeString()
}

function statusClass(status: string): string {
  switch (status) {
    case 'OK': return 'status-ok'
    case 'ERROR': return 'status-error'
    default: return ''
  }
}

function errorRateClass(rate: number): string {
  if (rate > 0.5) return 'error-high'
  if (rate > 0) return 'error-medium'
  return 'error-ok'
}

onMounted(() => {
  fetchSession()
})
</script>

<style scoped>
.session-detail { max-width: 1200px; }
.back-link { margin-bottom: 16px; }
.back-link a { color: #94a3b8; text-decoration: none; font-size: 14px; }
.back-link a:hover { color: #e2e8f0; }
.loading, .error { text-align: center; padding: 60px; color: #94a3b8; }
.error { color: #f87171; }
.session-summary { margin-bottom: 24px; }
.session-summary h2 { font-size: 20px; margin-bottom: 12px; word-break: break-all; }
.summary-grid { display: flex; gap: 24px; flex-wrap: wrap; }
.summary-item { display: flex; flex-direction: column; }
.summary-label { font-size: 11px; color: #94a3b8; text-transform: uppercase; }
.summary-value { font-size: 14px; }
.token-highlight { color: #c4b5fd; font-weight: 600; }
.error-high { color: #fca5a5; }
.error-medium { color: #fbbf24; }
.error-ok { color: #6ee7b7; }

.turns-heading { font-size: 16px; margin-bottom: 12px; color: #e2e8f0; }

.turns-list { display: flex; flex-direction: column; gap: 2px; }
.turn-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 12px;
  border-bottom: 1px solid #1e293b;
  cursor: pointer;
  font-size: 14px;
}
.turn-row:hover { background: #1e293b; }
.turn-number { color: #64748b; font-size: 12px; font-weight: 600; min-width: 32px; }
.turn-name { flex: 1; font-weight: 600; color: #38bdf8; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.status-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.status-ok { background: #065f46; color: #6ee7b7; }
.status-error { background: #7f1d1d; color: #fca5a5; }
.turn-duration { color: #94a3b8; min-width: 70px; text-align: right; }
.turn-tokens { color: #c4b5fd; font-weight: 600; min-width: 60px; text-align: right; }
.turn-service { color: #94a3b8; font-size: 13px; min-width: 100px; }
.turn-time { color: #64748b; font-size: 13px; min-width: 80px; text-align: right; }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/SessionDetail.vue
git commit -m "feat: add SessionDetail.vue page"
```

---

### Task 10: Build and verify end-to-end

- [ ] **Step 1: Build frontend**

Run: `cd web && npm run build`
Expected: no TypeScript or build errors.

- [ ] **Step 2: Build backend**

Run: `make build-nocgo`
Expected: successful compilation.

- [ ] **Step 3: Run all tests**

Run: `make test-nocgo`
Expected: all tests pass.

- [ ] **Step 4: Start the app and verify manually**

Run: `go run ./cmd/labubu --data-dir=""`
Open: `http://localhost:8080` (or run Vite dev server on port 3001)

Verify:
1. Sidebar shows "Sessions" between "Trace" and "Metrics"
2. Click "Sessions" → session list page loads (may be empty if no traces with `jiuwenclaw.session.id`)
3. Send test traces with `jiuwenclaw.session.id` attribute on spans
4. Verify sessions appear in the list with correct aggregated metrics
5. Click a session → detail page shows all traces chronologically
6. Click a turn → navigates to trace detail waterfall view

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: session observability MVP complete"
```
