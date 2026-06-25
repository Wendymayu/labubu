# Paginated Trace Logs + Full Download Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop loading every log of a trace into the trace-detail UI; display logs paginated, download all logs of a trace as plain text, and preserve the per-span log-count badges + click-to-filter via new backend support.

**Architecture:** Paginated display reuses the existing `GET /api/v1/logs?trace_id=…&page=…` (`listLogs`), extended with a `span_id` filter. A new lightweight `GET /api/v1/logs/{id}/counts` endpoint returns per-span counts (GROUP BY span_id, no bodies) for the waterfall badges + button label. Download reuses the existing all-logs `GET /api/v1/logs/{id}` (`getLogsByTrace`), formatted as plain text client-side. No new download endpoint.

**Tech Stack:** Go 1.22 `http.ServeMux` + `Store` interface (SQLite default / memstore fallback / chDB CGO); Vue 3 + TypeScript; vue-i18n.

**Spec:** `docs/superpowers/specs/2026-06-25-paginated-trace-logs-download-design.md`

**Build/test conventions (from CLAUDE.md + repo):**
- Default build = SQLite (non-CGO). `go test ./internal/...` runs SQLite-backed store tests.
- `go test -tags nosqlite ./internal/storage/...` runs memstore-backed store tests.
- chDB code is behind `//go:build cgo && local_engine` ([chdb.go:1](internal/storage/chdb.go#L1)) — NOT compiled in default tests. Implement it by mirroring existing patterns; verify with `go build -tags local_engine ./internal/storage/` only on a CGO/chDB-capable machine (skip if unavailable — the nocgo tests cover SQLite+memstore).
- Frontend checks: `cd web && npx vue-tsc --noEmit` and `cd web && npx vite build`. No frontend test framework.
- `NewChDBStore(dir)` is the store factory: returns SQLite by default, memstore with `-tags nosqlite`, chDB with `-tags local_engine`.

---

## File Structure

**Backend:**
- `internal/storage/storage.go` — add `LogQuery.SpanID`, `Store.GetLogCountsByTrace`.
- `internal/storage/sqlite_store.go` — span filter in `buildSqliteLogWhereClause`; `GetLogCountsByTrace`.
- `internal/storage/memstore.go` — span filter in `ListLogs`; `GetLogCountsByTrace`.
- `internal/storage/chdb_query.go` — span filter in `buildLogWhereClause`; `buildLogCountsByTraceSQL`.
- `internal/storage/chdb.go` — `GetLogCountsByTrace`; `parseLogCounts`.
- `internal/storage/log_trace_test.go` — NEW store-level tests (tagged `!local_engine`, runs for SQLite+memstore).
- `internal/api/log_handler.go` — `span_id` parsing in `parseLogQuery`; `GetLogCountsByTrace` handler; `/counts` dispatch in `ServeHTTP`.
- `internal/api/log_handler_test.go` — NEW API-level tests.
- `internal/api/trace_handler_test.go` — add `logCounts` field + `GetLogCountsByTrace` to `handlerMockStore`.
- `internal/pipeline/pipeline_test.go` — add `GetLogCountsByTrace` stub to `mockStore`.
- (Any other `Store` mock surfaced by `go build ./...` — add the same stub.)

**Frontend:**
- `web/src/api/client.ts` — `LogQuery.span_id`; pass it in `listLogs`; add `getLogCounts`.
- `web/src/i18n/locales/en.ts` + `zh.ts` — new `logList` keys.
- `web/src/views/TraceDetail.vue` — paginated state/fetch/UX, counts fetch, plain-text download.

---

## Task 1: `span_id` filter on `ListLogs` (store layer)

Adding a struct field is compile-safe; the filter additions are additive. TDD at the store level covers SQLite (default) and memstore (`-tags nosqlite`) via the `!local_engine` tag.

**Files:**
- Modify: `internal/storage/storage.go` (`LogQuery` struct, ~line 467)
- Modify: `internal/storage/sqlite_store.go` (`buildSqliteLogWhereClause`, ~line 1591)
- Modify: `internal/storage/memstore.go` (`ListLogs` filter loop, ~line 197)
- Modify: `internal/storage/chdb_query.go` (`buildLogWhereClause`, ~line 376)
- Test: `internal/storage/log_trace_test.go` (NEW)

- [ ] **Step 1: Write the failing test**

Create `internal/storage/log_trace_test.go`:

```go
//go:build !local_engine

package storage

import (
	"context"
	"testing"
)

func TestListLogsFiltersBySpanID(t *testing.T) {
	s, err := NewChDBStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	tid := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 7}
	spanA := [8]byte{1}
	spanB := [8]byte{2}
	logs := []LogRecord{
		{TraceID: tid, SpanID: spanA, Timestamp: 100, Severity: "INFO", EventName: "a1", Body: "{}"},
		{TraceID: tid, SpanID: spanA, Timestamp: 200, Severity: "INFO", EventName: "a2", Body: "{}"},
		{TraceID: tid, SpanID: spanB, Timestamp: 150, Severity: "INFO", EventName: "b1", Body: "{}"},
	}
	if err := s.InsertLogs(ctx, logs); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	res, err := s.ListLogs(ctx, LogQuery{Page: 1, PageSize: 100, TraceID: tid, SpanID: spanA})
	if err != nil {
		t.Fatalf("ListLogs: %v", err)
	}
	if len(res.Logs) != 2 {
		t.Fatalf("got %d logs, want 2 (spanA only)", len(res.Logs))
	}
	for _, l := range res.Logs {
		if l.SpanIDHex != SpanIDToHex(spanA) {
			t.Errorf("got span %s, want only spanA", l.SpanIDHex)
		}
	}
	if res.Pagination.Total != 2 {
		t.Errorf("total = %d, want 2", res.Pagination.Total)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestListLogsFiltersBySpanID -v`
Expected: FAIL / compile error — `q.SpanID` undefined (field not on `LogQuery` yet).

- [ ] **Step 3: Add `SpanID` to `LogQuery`**

In `internal/storage/storage.go`, change the `LogQuery` struct (~line 467) to:

```go
type LogQuery struct {
	Page      int
	PageSize  int
	Severity  string   // "" = all
	EventName string   // "" = all
	Query     string   // full-text search on body
	TraceID   [16]byte // zero value = no trace filter
	SpanID    [8]byte  // zero value = no span filter
	StartTime uint64
	EndTime   uint64
}
```

- [ ] **Step 4: Add the span filter to the SQLite WHERE builder**

In `internal/storage/sqlite_store.go`, in `buildSqliteLogWhereClause` (~line 1591), insert immediately after the `q.TraceID` block:

```go
	if q.TraceID != [16]byte{} {
		clauses = append(clauses, `trace_id_hex = ?`)
		args = append(args, TraceIDToHex(q.TraceID))
	}
	if q.SpanID != [8]byte{} {
		clauses = append(clauses, `span_id_hex = ?`)
		args = append(args, SpanIDToHex(q.SpanID))
	}
```

- [ ] **Step 5: Add the span filter to the memstore `ListLogs`**

In `internal/storage/memstore.go`, in `ListLogs`'s filter loop (~line 197), insert immediately after the `q.TraceID` check:

```go
	var zeroTrace [16]byte
	if q.TraceID != zeroTrace && l.TraceID != q.TraceID {
		continue
	}
	var zeroSpan [8]byte
	if q.SpanID != zeroSpan && l.SpanID != q.SpanID {
		continue
	}
```

- [ ] **Step 6: Add the span filter to the chDB WHERE builder**

In `internal/storage/chdb_query.go`, in `buildLogWhereClause` (~line 376), insert immediately after the `q.TraceID` block:

```go
	var zeroTrace [16]byte
	if q.TraceID != zeroTrace {
		clauses = append(clauses, fmt.Sprintf(
			"trace_id = unhex('%x')", q.TraceID,
		))
	}
	var zeroSpan [8]byte
	if q.SpanID != zeroSpan {
		clauses = append(clauses, fmt.Sprintf(
			"span_id = unhex('%x')", q.SpanID,
		))
	}
```

- [ ] **Step 7: Run tests to verify they pass (SQLite + memstore)**

Run: `go test ./internal/storage/ -run TestListLogsFiltersBySpanID -v`
Expected: PASS (SQLite)
Run: `go test -tags nosqlite ./internal/storage/ -run TestListLogsFiltersBySpanID -v`
Expected: PASS (memstore)

- [ ] **Step 8: Build the whole module**

Run: `go build ./...`
Expected: succeeds (no compile errors).

- [ ] **Step 9: Commit**

```bash
git add internal/storage/storage.go internal/storage/sqlite_store.go internal/storage/memstore.go internal/storage/chdb_query.go internal/storage/log_trace_test.go
git commit -m "feat(storage): add span_id filter to ListLogs"
```

---

## Task 2: `GetLogCountsByTrace` store method + interface (store layer)

Adding a method to the `Store` interface forces every implementor to gain it. The store-level test drives the real impls; the test mocks get stubs so the module still compiles.

**Files:**
- Modify: `internal/storage/storage.go` (`Store` interface, after `GetLogsByTrace` ~line 368)
- Modify: `internal/storage/sqlite_store.go` (new method, near `GetLogsByTrace` ~line 933)
- Modify: `internal/storage/memstore.go` (new method, near `GetLogsByTrace` ~line 254)
- Modify: `internal/storage/chdb_query.go` (new `buildLogCountsByTraceSQL`, near `buildGetLogsByTraceSQL` ~line 400)
- Modify: `internal/storage/chdb.go` (new `GetLogCountsByTrace` + `parseLogCounts`, near `parseCount` ~line 746)
- Modify: `internal/api/trace_handler_test.go` (`handlerMockStore`: add field + method)
- Modify: `internal/pipeline/pipeline_test.go` (`mockStore`: add stub)
- Test: `internal/storage/log_trace_test.go` (append test)

- [ ] **Step 1: Append the failing test**

Append to `internal/storage/log_trace_test.go`:

```go
func TestGetLogCountsByTrace(t *testing.T) {
	s, err := NewChDBStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	tid := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9}
	spanA := [8]byte{1}
	spanB := [8]byte{2}
	logs := []LogRecord{
		{TraceID: tid, SpanID: spanA, Timestamp: 100, Severity: "INFO", Body: "{}"},
		{TraceID: tid, SpanID: spanA, Timestamp: 200, Severity: "INFO", Body: "{}"},
		{TraceID: tid, SpanID: spanB, Timestamp: 150, Severity: "INFO", Body: "{}"},
	}
	if err := s.InsertLogs(ctx, logs); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	counts, err := s.GetLogCountsByTrace(ctx, tid)
	if err != nil {
		t.Fatalf("GetLogCountsByTrace: %v", err)
	}
	if counts[SpanIDToHex(spanA)] != 2 {
		t.Errorf("spanA count = %d, want 2", counts[SpanIDToHex(spanA)])
	}
	if counts[SpanIDToHex(spanB)] != 1 {
		t.Errorf("spanB count = %d, want 1", counts[SpanIDToHex(spanB)])
	}
	if len(counts) != 2 {
		t.Errorf("got %d spans, want 2", len(counts))
	}

	// Empty trace returns a non-nil empty map (handler relies on this).
	other := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 99}
	c2, err := s.GetLogCountsByTrace(ctx, other)
	if err != nil {
		t.Fatalf("GetLogCountsByTrace empty: %v", err)
	}
	if c2 == nil || len(c2) != 0 {
		t.Errorf("empty trace counts = %#v, want non-nil empty map", c2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestGetLogCountsByTrace -v`
Expected: compile error — `s.GetLogCountsByTrace undefined`.

- [ ] **Step 3: Add the method to the `Store` interface**

In `internal/storage/storage.go`, immediately after the `GetLogsByTrace` interface line (~line 368), add:

```go
	// GetLogsByTrace returns all log records for a given trace, ordered by timestamp.
	GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]LogListItem, error)

	// GetLogCountsByTrace returns the number of logs per span_id_hex for a trace.
	GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error)
```

- [ ] **Step 4: Implement SQLite `GetLogCountsByTrace`**

In `internal/storage/sqlite_store.go`, immediately after the existing `func (s *sqliteStore) GetLogsByTrace(...)` method (~line 933), add:

```go
// GetLogCountsByTrace returns the per-span log count for a trace.
func (s *sqliteStore) GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT span_id_hex, COUNT(*) FROM logs WHERE trace_id_hex = ? GROUP BY span_id_hex`,
		TraceIDToHex(traceID),
	)
	if err != nil {
		return nil, fmt.Errorf("count logs by trace: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var spanIDHex string
		var n int
		if err := rows.Scan(&spanIDHex, &n); err != nil {
			return nil, fmt.Errorf("scan log count: %w", err)
		}
		counts[spanIDHex] = n
	}
	return counts, nil
}
```

- [ ] **Step 5: Implement memstore `GetLogCountsByTrace`**

In `internal/storage/memstore.go`, immediately after the existing `func (m *memStore) GetLogsByTrace(...)` method (~line 254), add:

```go
// GetLogCountsByTrace returns the per-span log count for a trace.
func (m *memStore) GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := make(map[string]int)
	for _, l := range m.logs {
		if l.TraceID == traceID {
			counts[SpanIDToHex(l.SpanID)]++
		}
	}
	return counts, nil
}
```

- [ ] **Step 6: Add the chDB SQL builder**

In `internal/storage/chdb_query.go`, immediately after `buildGetLogsByTraceSQL` (~line 411), add:

```go
// buildLogCountsByTraceSQL builds a query returning per-span log counts for a trace.
func buildLogCountsByTraceSQL(traceID [16]byte) string {
	return fmt.Sprintf(
		`SELECT hex(span_id) AS span_id_hex, COUNT(*) AS n
		FROM logs
		WHERE trace_id = unhex('%x')
		GROUP BY span_id`,
		traceID,
	)
}
```

- [ ] **Step 7: Add chDB `GetLogCountsByTrace` + parser**

In `internal/storage/chdb.go`, immediately after the existing `func (s *chDBStore) GetLogsByTrace(...)` (~line 282), add:

```go
// GetLogCountsByTrace returns the per-span log count for a trace.
func (s *chDBStore) GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error) {
	sql := buildLogCountsByTraceSQL(traceID) + " FORMAT JSONEachRow"
	result, err := s.querySQL(sql)
	if err != nil {
		return nil, fmt.Errorf("get log counts by trace: %w", err)
	}
	return parseLogCounts(result), nil
}
```

And immediately after `parseCount` (~line 760), add:

```go
// parseLogCounts parses JSONEachRow rows of {span_id_hex, n} into a map.
func parseLogCounts(result string) map[string]int {
	counts := make(map[string]int)
	for _, line := range splitLines(result) {
		if line == "" {
			continue
		}
		var row struct {
			SpanIDHex string `json:"span_id_hex"`
			N         int    `json:"n"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		counts[row.SpanIDHex] = row.N
	}
	return counts
}
```

- [ ] **Step 8: Add the method + field to the `handlerMockStore` (api tests)**

In `internal/api/trace_handler_test.go`:

8a. Add a field to the `handlerMockStore` struct (after the `diagnosisResultErr` line):

```go
type handlerMockStore struct {
	traces            *storage.TraceListResult
	detail            *storage.TraceDetail
	services          []string
	listErr           error
	detailErr         error
	costSummary       *storage.CostSummaryResult
	costSummaryErr    error
	llmConfigs        []storage.LLMConfig
	llmConfigsErr     error
	diagnosisResult   *storage.DiagnosisResult
	diagnosisResultErr error
	logCounts         map[string]int
}
```

8b. Add the method immediately after `handlerMockStore.GetLogsByTrace` (~line 66):

```go
func (m *handlerMockStore) GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error) {
	return m.logCounts, nil
}
```

- [ ] **Step 9: Add the stub to the pipeline `mockStore`**

In `internal/pipeline/pipeline_test.go`, immediately after `func (m *mockStore) GetLogsByTrace(...)` (~line 60), add:

```go
func (m *mockStore) GetLogCountsByTrace(ctx context.Context, traceID [16]byte) (map[string]int, error) {
	return nil, nil
}
```

- [ ] **Step 10: Build everything — catch any other Store mock that needs the stub**

Run: `go build ./...`
Expected: succeeds. If a compile error reports `X` does not implement `storage.Store` (missing `GetLogCountsByTrace`) for some other test mock, add the same stub shown in Step 9 to that mock type, then re-run until `go build ./...` succeeds.

- [ ] **Step 11: Run the store tests (SQLite + memstore)**

Run: `go test ./internal/storage/ -run TestGetLogCountsByTrace -v`
Expected: PASS (SQLite)
Run: `go test -tags nosqlite ./internal/storage/ -run TestGetLogCountsByTrace -v`
Expected: PASS (memstore)

- [ ] **Step 12: (If CGO+chDB available) verify chDB compiles**

Run: `go build -tags local_engine ./internal/storage/`
Expected: succeeds. Skip this step if the chDB C library is unavailable on this machine — the chDB code mirrors existing patterns and is covered by code review.

- [ ] **Step 13: Commit**

```bash
git add internal/storage/storage.go internal/storage/sqlite_store.go internal/storage/memstore.go internal/storage/chdb.go internal/storage/chdb_query.go internal/storage/log_trace_test.go internal/api/trace_handler_test.go internal/pipeline/pipeline_test.go
git commit -m "feat(storage): add GetLogCountsByTrace per-span log counts"
```

---

## Task 3: `span_id` parsing + counts HTTP endpoint + dispatch (API layer)

**Files:**
- Modify: `internal/api/log_handler.go` (`parseLogQuery` ~line 116; `ServeHTTP` ~line 23; new `GetLogCountsByTrace` handler)
- Test: `internal/api/log_handler_test.go` (NEW)

- [ ] **Step 1: Write the failing tests**

Create `internal/api/log_handler_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseLogQuerySpanID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs?span_id=0102030405060708&trace_id=e16c42c68388d1d891d3d0c80a9892ca", nil)
	q := parseLogQuery(req)
	want := [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if q.SpanID != want {
		t.Errorf("SpanID = %x, want %x", q.SpanID, want)
	}
}

func TestParseLogQuerySpanIDInvalid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs?span_id=not-hex", nil)
	q := parseLogQuery(req)
	if q.SpanID != [8]byte{} {
		t.Errorf("SpanID = %x, want zero (invalid hex ignored)", q.SpanID)
	}
}

func TestGetLogCountsByTraceHandler(t *testing.T) {
	store := &handlerMockStore{logCounts: map[string]int{"50800eb5931cb62d": 3, "aabbccddeeff0011": 1}}
	h := NewLogHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/e16c42c68388d1d891d3d0c80a9892ca/counts", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Counts["50800eb5931cb62d"] != 3 {
		t.Errorf("count = %d, want 3", body.Counts["50800eb5931cb62d"])
	}
	if len(body.Counts) != 2 {
		t.Errorf("got %d spans, want 2", len(body.Counts))
	}
}

func TestGetLogCountsByTraceInvalidID(t *testing.T) {
	h := NewLogHandler(&handlerMockStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/not-hex/counts", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// Ensure the bare /logs/{id} path still routes to GetLogsByTrace (download path regression guard).
func TestGetLogsByTraceStillRouted(t *testing.T) {
	h := NewLogHandler(&handlerMockStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/e16c42c68388d1d891d3d0c80a9892ca", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestParseLogQuerySpanID|TestGetLogCountsByTrace|TestGetLogsByTraceStillRouted' -v`
Expected: FAIL — `parseLogQuery` doesn't set `SpanID`; `/counts` route not dispatched (handler returns 400 invalid trace_id for `e16c…/counts`).

- [ ] **Step 3: Parse `span_id` in `parseLogQuery`**

In `internal/api/log_handler.go`, in `parseLogQuery`, immediately after the `trace_id` block (~line 120), add:

```go
	if v := r.URL.Query().Get("trace_id"); v != "" {
		if b, err := hex.DecodeString(v); err == nil && len(b) == 16 {
			copy(q.TraceID[:], b)
		}
	}
	if v := r.URL.Query().Get("span_id"); v != "" {
		if b, err := hex.DecodeString(v); err == nil && len(b) == 8 {
			copy(q.SpanID[:], b)
		}
	}
```

- [ ] **Step 4: Add the `/counts` dispatch in `ServeHTTP`**

In `internal/api/log_handler.go`, replace the body of `ServeHTTP` with:

```go
// ServeHTTP dispatches requests to the appropriate handler based on URL path.
func (h *LogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/logs")
	if path == "" || path == "/" {
		h.ListLogs(w, r)
		return
	}
	traceIDHex := strings.TrimPrefix(path, "/")
	if strings.HasSuffix(traceIDHex, "/counts") {
		id := strings.TrimSuffix(traceIDHex, "/counts")
		h.GetLogCountsByTrace(w, r, id)
		return
	}
	h.GetLogsByTrace(w, r, traceIDHex)
}
```

- [ ] **Step 5: Add the `GetLogCountsByTrace` handler**

In `internal/api/log_handler.go`, immediately after `GetLogsByTrace` (~line 76), add:

```go
// GetLogCountsByTrace handles GET /api/v1/logs/{traceIdHex}/counts.
func (h *LogHandler) GetLogCountsByTrace(w http.ResponseWriter, r *http.Request, traceIDHex string) {
	traceID, err := hex.DecodeString(traceIDHex)
	if err != nil || len(traceID) != 16 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid trace_id"})
		return
	}

	var tid [16]byte
	copy(tid[:], traceID)

	counts, err := h.store.GetLogCountsByTrace(r.Context(), tid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if counts == nil {
		counts = map[string]int{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"counts": counts,
	})
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestParseLogQuerySpanID|TestGetLogCountsByTrace|TestGetLogsByTraceStillRouted' -v`
Expected: PASS.

- [ ] **Step 7: Run the full api package tests (regression)**

Run: `go test ./internal/api/ -v`
Expected: PASS (existing tests unaffected).

- [ ] **Step 8: Commit**

```bash
git add internal/api/log_handler.go internal/api/log_handler_test.go
git commit -m "feat(api): span_id filter + per-span log counts endpoint"
```

---

## Task 4: Frontend client — `span_id` param + `getLogCounts`

**Files:**
- Modify: `web/src/api/client.ts` (`LogQuery` ~line 475; `listLogs` ~line 491; new `getLogCounts` after `getLogsByTrace` ~line 506)

- [ ] **Step 1: Add `span_id` to `LogQuery` and `listLogs`**

In `web/src/api/client.ts`, replace the `LogQuery` interface and `listLogs` function with:

```ts
export interface LogQuery {
  page?: number
  page_size?: number
  severity?: string
  event_name?: string
  q?: string
  trace_id?: string
  span_id?: string
  start?: number
  end?: number
}

export interface LogListResponse {
  logs: LogRecord[]
  pagination: Pagination
}

export async function listLogs(query: LogQuery): Promise<LogListResponse> {
  return get<LogListResponse>(`${BASE_URL}/logs`, {
    page: query.page,
    page_size: query.page_size,
    severity: query.severity,
    event_name: query.event_name,
    q: query.q,
    trace_id: query.trace_id,
    span_id: query.span_id,
    start: query.start,
    end: query.end,
  })
}
```

(Only `span_id` is new in `LogQuery`; the rest is reproduced verbatim so the engineer doesn't have to merge.)

- [ ] **Step 2: Add `getLogCounts`**

In `web/src/api/client.ts`, immediately after `getLogsByTrace`, add:

```ts
export async function getLogCounts(traceIdHex: string): Promise<{ counts: Record<string, number> }> {
  return get<{ counts: Record<string, number> }>(`${BASE_URL}/logs/${traceIdHex}/counts`)
}
```

- [ ] **Step 3: Type-check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat(web): add span_id param + getLogCounts client"
```

---

## Task 5: i18n keys

**Files:**
- Modify: `web/src/i18n/locales/en.ts` (`logList` ~line 98)
- Modify: `web/src/i18n/locales/zh.ts` (`logList` ~line 98)

- [ ] **Step 1: Add keys to `en.ts`**

In `web/src/i18n/locales/en.ts`, replace the `logList` block with:

```ts
  logList: {
    searchPlaceholder: 'Search logs...',
    allSeverity: 'All severities',
    allEvents: 'All events',
    timestamp: 'Timestamp',
    severity: 'Severity',
    event: 'Event',
    body: 'Body',
    trace: 'Trace',
    noLogs: 'No logs found.',
    filteredBySpan: 'Filtered: {name}',
    logCount: 'Logs ({count})',
    download: 'Download Logs',
    prev: 'Prev',
    next: 'Next',
    pageOf: 'Page {page} of {total}',
  },
```

- [ ] **Step 2: Add keys to `zh.ts`**

In `web/src/i18n/locales/zh.ts`, replace the `logList` block with:

```ts
  logList: {
    searchPlaceholder: '搜索日志...',
    allSeverity: '所有级别',
    allEvents: '所有事件',
    timestamp: '时间',
    severity: '级别',
    event: '事件',
    body: '正文',
    trace: '链路',
    noLogs: '未找到日志。',
    filteredBySpan: '已过滤: {name}',
    logCount: '日志 ({count})',
    download: '下载日志',
    prev: '上一页',
    next: '下一页',
    pageOf: '第 {page} / {total} 页',
  },
```

- [ ] **Step 3: Type-check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat(web): i18n keys for log pagination + download"
```

---

## Task 6: TraceDetail.vue — paginated log state, fetch, counts, overlay rendering, pagination controls

**Files:**
- Modify: `web/src/views/TraceDetail.vue` (imports, state, computeds, fetch/lifecycle, template overlay body, CSS)

- [ ] **Step 1: Update imports**

In `web/src/views/TraceDetail.vue` (~line 183), replace the api-client import line with:

```ts
import { getTrace, getLogsByTrace, getLogCounts, listLogs, getDiagnosisResult, diagnoseTrace, type TraceDetailResponse, type SpanDetail as SpanDetailType, type LogRecord, type DiagnosisResult } from '../api/client'
```

- [ ] **Step 2: Replace log state refs**

In the script (~lines 201-202), replace:

```ts
const traceLogs = ref<LogRecord[]>([])
const logsLoading = ref(false)
```

with:

```ts
const pageLogs = ref<LogRecord[]>([])
const logsLoading = ref(false)
const logPage = ref(1)
const logPageSize = 50
const logTotal = ref(0)
const logCounts = ref<Record<string, number>>({})
```

- [ ] **Step 3: Replace `logCounts`/`totalLogCount`/`filteredLogs` computeds**

Replace the three computeds (~lines 265-280):

```ts
const logCounts = computed(() => {
  const counts: Record<string, number> = {}
  for (const l of traceLogs.value) {
    if (l.span_id_hex) {
      counts[l.span_id_hex] = (counts[l.span_id_hex] || 0) + 1
    }
  }
  return counts
})

const totalLogCount = computed(() => traceLogs.value.length)

const filteredLogs = computed(() => {
  if (!logSpanFilter.value) return traceLogs.value
  return traceLogs.value.filter(l => l.span_id_hex === logSpanFilter.value)
})
```

with:

```ts
const totalLogCount = computed(() => {
  let n = 0
  for (const k in logCounts.value) n += logCounts.value[k]
  return n
})

const filteredSpanName = computed(() => {
  if (!logSpanFilter.value || !trace.value) return logSpanFilter.value
  const span = trace.value.spans.find(s => s.span_id === logSpanFilter.value)
  return span?.name || logSpanFilter.value
})
```

(`logCounts` is now the ref from Step 2, not a computed. `filteredLogs` is removed — the backend paginated query replaces it.)

- [ ] **Step 4: Replace `fetchTraceLogs` with `fetchLogCounts` + `fetchLogPage`**

Replace the `fetchTraceLogs` function (~line 409):

```ts
async function fetchTraceLogs() {
  logsLoading.value = true
  try {
    const result = await getLogsByTrace(traceIdHex)
    traceLogs.value = result.logs || []
  } catch {
    traceLogs.value = []
  } finally {
    logsLoading.value = false
  }
}
```

with:

```ts
async function fetchLogCounts() {
  try {
    const result = await getLogCounts(traceIdHex)
    logCounts.value = result.counts || {}
  } catch {
    logCounts.value = {}
  }
}

async function fetchLogPage() {
  logsLoading.value = true
  try {
    const result = await listLogs({
      trace_id: traceIdHex,
      span_id: logSpanFilter.value || undefined,
      page: logPage.value,
      page_size: logPageSize,
    })
    pageLogs.value = result.logs || []
    logTotal.value = result.pagination?.total ?? 0
  } catch {
    pageLogs.value = []
    logTotal.value = 0
  } finally {
    logsLoading.value = false
  }
}

function prevLogPage() {
  if (logPage.value > 1) {
    logPage.value--
    fetchLogPage()
  }
}

function nextLogPage() {
  if (logPage.value * logPageSize < logTotal.value) {
    logPage.value++
    fetchLogPage()
  }
}
```

- [ ] **Step 5: Rewire `fetchTrace`, `filterLogsBySpan`, `clearLogFilter`**

Replace `fetchTrace` (~line 322):

```ts
async function fetchTrace() {
  loading.value = true
  error.value = ''
  try {
    const result = await getTrace(traceIdHex)
    trace.value = result.trace
    fetchLogCounts()
    fetchLogPage()
  } catch (e: any) {
    error.value = e.message || 'Failed to load trace'
  } finally {
    loading.value = false
  }
}
```

Replace `filterLogsBySpan` (~line 421):

```ts
function filterLogsBySpan(spanId: string) {
  logSpanFilter.value = spanId
  logPage.value = 1
  activeInsight.value = 'logs'
  fetchLogPage()
}
```

Replace `clearLogFilter` (~line 426):

```ts
function clearLogFilter() {
  logSpanFilter.value = ''
  logPage.value = 1
  fetchLogPage()
}
```

- [ ] **Step 6: Update the overlay body template — render `pageLogs`, resolved filter name, pagination bar**

Replace the entire `<div v-if="activeInsight === 'logs'" class="log-overlay">…</div>` block in the overlay body with:

```html
          <div v-if="activeInsight === 'logs'" class="log-overlay">
            <div v-if="logSpanFilter" class="log-filter-tag">
              {{ t('logList.filteredBySpan', { name: filteredSpanName }) }}
              <button class="filter-clear" @click="clearLogFilter">✕</button>
            </div>
            <div v-if="logsLoading" class="loading-state">{{ t('common.loading') }}</div>
            <div v-else-if="pageLogs.length === 0" class="empty-state">{{ t('logList.noLogs') }}</div>
            <div v-else class="log-list-inline">
              <div
                v-for="(log, idx) in pageLogs"
                :key="idx"
                class="log-item"
              >
                <span class="log-item-time">{{ formatLogTime(log.timestamp) }}</span>
                <span :class="['severity-badge', log.severity.toLowerCase()]">{{ log.severity }}</span>
                <span class="log-item-event">{{ log.event_name || '-' }}</span>
                <span v-if="log.body" class="log-item-body">{{ formatLogBody(log.body) }}</span>
              </div>
            </div>
            <div v-if="!logsLoading && pageLogs.length > 0" class="log-pagination">
              <button class="page-btn" :disabled="logPage <= 1" @click="prevLogPage">◀ {{ t('logList.prev') }}</button>
              <span class="page-info">{{ t('logList.pageOf', { page: logPage, total: Math.max(1, Math.ceil(logTotal / logPageSize)) }) }}</span>
              <button class="page-btn" :disabled="logPage * logPageSize >= logTotal" @click="nextLogPage">{{ t('logList.next') }} ▶</button>
              <span class="page-total">{{ t('logList.logCount', { count: logTotal }) }}</span>
            </div>
          </div>
```

- [ ] **Step 7: Add pagination CSS**

In the `<style scoped>` section, immediately after the `.log-item-body { … }` rule, add:

```css
.log-pagination {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  border-top: 1px solid var(--border-default);
  font-size: 12px;
  color: var(--text-secondary);
}
.page-btn {
  background: none;
  border: 1px solid var(--border-group);
  border-radius: 4px;
  color: var(--text-secondary);
  padding: 3px 10px;
  font-size: 12px;
  cursor: pointer;
}
.page-btn:hover:not(:disabled) {
  border-color: var(--accent-blue);
  color: var(--accent-blue);
}
.page-btn:disabled {
  opacity: 0.4;
  cursor: default;
}
.page-info { color: var(--text-primary); }
.page-total { margin-left: auto; }
```

- [ ] **Step 8: Type-check and build**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors.
Run: `cd web && npx vite build`
Expected: builds successfully.

- [ ] **Step 9: Commit**

```bash
git add web/src/views/TraceDetail.vue
git commit -m "feat(web): paginated trace logs in overlay + counts-driven badges"
```

---

## Task 7: TraceDetail.vue — download all logs as plain text

**Files:**
- Modify: `web/src/views/TraceDetail.vue` (overlay header template, new functions, CSS)

- [ ] **Step 1: Add the download button to the overlay header**

In the overlay header, replace:

```html
          <button class="insight-overlay-close" @click="activeInsight = null" title="Close">✕</button>
```

with:

```html
          <div class="insight-overlay-actions">
            <button
              v-if="activeInsight === 'logs'"
              class="insight-overlay-action"
              @click="downloadTraceLogs"
              :title="t('logList.download')"
            >⬇</button>
            <button class="insight-overlay-close" @click="activeInsight = null" title="Close">✕</button>
          </div>
```

- [ ] **Step 2: Add the download + text-format functions**

In the script, immediately after the existing `downloadTraceOTLP` function, add:

```ts
async function downloadTraceLogs() {
  try {
    const result = await getLogsByTrace(traceIdHex)
    const logs = result.logs || []
    const text = formatLogsAsText(logs)
    downloadBlob(text, `trace-${traceIdHex}-logs.txt`)
  } catch (e: any) {
    alert(`Log download failed: ${e.message}`)
  }
}

function formatLogsAsText(logs: LogRecord[]): string {
  const lines: string[] = []
  lines.push(`# trace ${traceIdHex} — ${logs.length} logs`)
  lines.push('')
  for (const log of logs) {
    const ts = formatLogTimestamp(log.timestamp)
    const event = log.event_name || '-'
    lines.push(`[${ts}] ${log.severity}  span=${log.span_id_hex || '-'}  event=${event}`)
    if (log.body) {
      lines.push(log.body)
    }
    if (log.attributes && Object.keys(log.attributes).length > 0) {
      const attrs = Object.entries(log.attributes).map(([k, v]) => `${k}=${v}`).join(', ')
      lines.push(`attrs: ${attrs}`)
    }
    lines.push('---')
  }
  return lines.join('\n')
}

function formatLogTimestamp(ts: number): string {
  const d = new Date(ts)
  const pad = (n: number, l = 2) => String(n).padStart(l, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}
```

- [ ] **Step 3: Add the actions CSS**

In the `<style scoped>` section, immediately after the `.insight-overlay-close:hover { … }` rule, add:

```css
.insight-overlay-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
.insight-overlay-action {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 14px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
  line-height: 1;
}
.insight-overlay-action:hover {
  color: var(--text-primary);
  background: var(--bg-surface-hover-subtle);
}
```

- [ ] **Step 4: Type-check and build**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors.
Run: `cd web && npx vite build`
Expected: builds successfully.

- [ ] **Step 5: Commit**

```bash
git add web/src/views/TraceDetail.vue
git commit -m "feat(web): download all trace logs as plain text"
```

---

## Task 8: Final verification

- [ ] **Step 1: Run all Go tests**

Run: `go test ./internal/... ./cmd/...`
Expected: PASS (SQLite-backed).
Run: `go test -tags nosqlite ./internal/storage/...`
Expected: PASS (memstore-backed).

- [ ] **Step 2: Build the whole project (nocgo)**

Run: `go build ./...`
Expected: succeeds.

- [ ] **Step 3: Frontend checks**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors.
Run: `cd web && npx vite build`
Expected: builds successfully.

- [ ] **Step 4: (If CGO+chDB available) chDB build**

Run: `go build -tags local_engine ./internal/storage/`
Expected: succeeds. Skip if chDB unavailable.

- [ ] **Step 5: Manual smoke test**

Run the app (e.g. `go run -tags dev ./cmd/labubu serve`), open a trace detail page:
- The `📋 Logs (N)` button shows the total.
- The waterfall spans show `📋 N` badges (counts from the new endpoint).
- Opening Logs shows page 1 (50/page) with prev/next + "page X of Y".
- Clicking a span's `📋` badge opens the overlay filtered to that span, page 1.
- The `⬇` button downloads `trace-<id>-logs.txt` containing all logs (header + one block per log + `---` separators), regardless of any active span filter.

---

## Self-Review (completed by plan author)

**Spec coverage:**
- `span_id` filter on `GET /logs` → Task 1 (stores) + Task 3 (parse). ✓
- `GET /logs/{id}/counts` endpoint → Task 2 (store) + Task 3 (handler+dispatch). ✓
- Paginated display via `listLogs` → Task 6. ✓
- Per-span badges preserved (counts from endpoint) → Task 6 (`logCounts` ref from `getLogCounts`). ✓
- Click-to-filter survives pagination → Task 6 (`filterLogsBySpan` → `listLogs` with `span_id`). ✓
- Download all logs as plain text → Task 7. ✓
- i18n keys → Task 5. ✓
- Tests (API table-driven + store-level, chDB behind nocgo) → Tasks 1-3. ✓

**Placeholder scan:** None — every code step shows the exact code; every test step shows the test and the run command with expected output.

**Type consistency:** `GetLogCountsByTrace(ctx, [16]byte) (map[string]int, error)` is identical across the interface (Task 2 Step 3), SQLite/memstore/chDB impls (Task 2 Steps 4-7), and the handler mock (Task 2 Step 8). `LogQuery.SpanID [8]byte` is identical in storage.go (Task 1 Step 3) and used consistently in all three stores. Frontend `getLogCounts` returns `{ counts: Record<string, number> }` (Task 4 Step 2), matching the handler's `{"counts": map}` (Task 3 Step 5) and the `logCounts` ref (Task 6 Step 2). `pageLogs`/`logPage`/`logTotal`/`logPageSize` names are consistent across Task 6 steps.
