# Trace List Min/Max Filters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add min/max filters for span count, duration, and cost to the trace list (`/traces`), rendered as column-header funnel popovers.

**Architecture:** The data (`span_count`, `duration_ms`, `cost`) already lives on each trace row at the list level. Duration is already fully wired through every layer except the UI. Span count and cost need the same plumbing added at the `TraceQuery` struct, handler, SQL/memstore WHERE clauses, and frontend client. Each filter is a min/max number pair in a column-header popover, mirroring the existing service/status popover pattern.

**Tech Stack:** Go 1.19 backend (table-driven tests), Vue 3 + TypeScript frontend, vue-i18n, SQLite (default)/chDB (CGO)/memstore storage backends.

## Global Constraints

- TypeScript strict mode — no `any`. New types go in `web/src/api/client.ts`.
- All user-facing text uses `vue-i18n`. Add keys to BOTH `web/src/i18n/locales/en.ts` and `zh.ts`, reference via `t('traceList.key')`.
- Backend storage must go through the `Store` interface — never access chDB/SQLite directly from handlers.
- New API endpoints/params require tests in `internal/api/` or `internal/storage/`. Go table-driven tests preferred.
- `min_duration`/`max_duration` are in **milliseconds** (uint64). `min_spans`/`max_spans` are counts (uint16). `min_cost`/`max_cost` are float64.
- Empty/zero filter value means "unset" (do not filter). This matches the existing `MinDuration`/`MaxDuration` convention (`if q.MinDuration > 0`).
- Build tag for memstore (no-CGO) tests: `//go:build !local_engine && nosqlite`. SQLite tests use `//go:build !local_engine && !nosqlite`.

---

## File Structure

| File | Responsibility | Change |
|------|----------------|--------|
| `internal/storage/storage.go` | `TraceQuery` filter struct | Add 4 fields |
| `internal/api/trace_handler.go` | HTTP query-param parsing | Parse 4 new params |
| `internal/api/openapi.go` | OpenAPI param docs | Document 4 new params |
| `internal/api/trace_handler_test.go` | Handler test + mock capture | Add capture field + test |
| `internal/storage/sqlite_store.go` | SQLite WHERE clause | Add 4 clauses |
| `internal/storage/memstore.go` | In-memory filter + item builder | Add 4 checks + fill Cost |
| `internal/storage/chdb_query.go` | chDB WHERE clause | Add 4 clauses |
| `web/src/api/client.ts` | `TraceQuery` interface + `listTraces` params | Add 4 fields + 4 params |
| `web/src/views/TraceList.vue` | Filter UI + state | Add 3 popovers, extend state |
| `web/src/i18n/locales/en.ts`, `zh.ts` | Locale strings | Add 9 keys each |

---

## Task 1: Add `TraceQuery` fields + handler param parsing

**Files:**
- Modify: `internal/storage/storage.go:78-88` (TraceQuery struct)
- Modify: `internal/api/trace_handler.go:51-66` (param parsing)
- Modify: `internal/api/openapi.go:48-58` (param docs)
- Modify: `internal/api/trace_handler_test.go` (mock capture + new test)

**Interfaces:**
- Produces: `storage.TraceQuery{MinSpanCount, MaxSpanCount uint16, MinCost, MaxCost float64}`. Handler parses query params `min_spans`/`max_spans` (uint) and `min_cost`/`max_cost` (float) into these fields.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/trace_handler_test.go`. First, the mock's `ListTraces` currently discards the query — make it capture. Edit the `handlerMockStore` struct (around line 18) to add a capture field, and update its `ListTraces` method:

```go
type handlerMockStore struct {
	traces            *storage.TraceListResult
	lastTraceQuery    storage.TraceQuery   // <-- ADD THIS LINE
	// ...existing fields unchanged...
}

func (m *handlerMockStore) ListTraces(ctx context.Context, q storage.TraceQuery) (*storage.TraceListResult, error) {
	m.lastTraceQuery = q // <-- ADD THIS LINE
	return m.traces, m.listErr
}
```

Then add this test at the end of the file:

```go
func TestListTracesParsesMinMaxFilters(t *testing.T) {
	store := &handlerMockStore{
		traces: &storage.TraceListResult{
			Traces:     []storage.TraceListItem{},
			Pagination: storage.Pagination{Page: 1, PageSize: 20, Total: 0},
		},
	}
	handler := NewTraceHandler(store)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/traces?min_duration=100&max_duration=5000&min_spans=3&max_spans=50&min_cost=0.5&max_cost=10", nil)
	rec := httptest.NewRecorder()
	handler.ListTraces(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	q := store.lastTraceQuery
	if q.MinDuration != 100 || q.MaxDuration != 5000 {
		t.Errorf("duration: got min=%d max=%d, want 100/5000", q.MinDuration, q.MaxDuration)
	}
	if q.MinSpanCount != 3 || q.MaxSpanCount != 50 {
		t.Errorf("spans: got min=%d max=%d, want 3/50", q.MinSpanCount, q.MaxSpanCount)
	}
	if q.MinCost != 0.5 || q.MaxCost != 10 {
		t.Errorf("cost: got min=%v max=%v, want 0.5/10", q.MinCost, q.MaxCost)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestListTracesParsesMinMaxFilters ./internal/api/`
Expected: COMPILE FAIL — `q.MinSpanCount` / `q.MaxSpanCount` / `q.MinCost` / `q.MaxCost` undefined on `storage.TraceQuery`.

- [ ] **Step 3: Add fields to `TraceQuery`**

In `internal/storage/storage.go`, edit the struct at line 78:

```go
type TraceQuery struct {
	Page        int
	PageSize    int
	Service     string
	Status      string // "OK", "ERROR", "UNSET", "" = all
	Query       string // root_name fuzzy search
	StartTimeMS uint64
	EndTimeMS   uint64
	MinDuration uint64
	MaxDuration uint64
	MinSpanCount uint16
	MaxSpanCount uint16
	MinCost     float64
	MaxCost     float64
}
```

- [ ] **Step 4: Parse the new params in the handler**

In `internal/api/trace_handler.go`, replace the parse block at lines 51-66:

```go
	startMS, _ := strconv.ParseUint(q.Get("start"), 10, 64)
	endMS, _ := strconv.ParseUint(q.Get("end"), 10, 64)
	minDuration, _ := strconv.ParseUint(q.Get("min_duration"), 10, 64)
	maxDuration, _ := strconv.ParseUint(q.Get("max_duration"), 10, 64)
	minSpans, _ := strconv.ParseUint(q.Get("min_spans"), 10, 16)
	maxSpans, _ := strconv.ParseUint(q.Get("max_spans"), 10, 16)
	minCost, _ := strconv.ParseFloat(q.Get("min_cost"), 64)
	maxCost, _ := strconv.ParseFloat(q.Get("max_cost"), 64)

	query := storage.TraceQuery{
		Page:         page,
		PageSize:     pageSize,
		Service:      q.Get("service"),
		Status:       q.Get("status"),
		Query:        q.Get("q"),
		StartTimeMS:  startMS,
		EndTimeMS:    endMS,
		MinDuration:  minDuration,
		MaxDuration:  maxDuration,
		MinSpanCount: uint16(minSpans),
		MaxSpanCount: uint16(maxSpans),
		MinCost:      minCost,
		MaxCost:      maxCost,
	}
```

- [ ] **Step 5: Document params in OpenAPI**

In `internal/api/openapi.go`, edit the parameters array (line 48) to add four entries after `max_duration`:

```go
              { "name": "min_spans", "in": "query", "schema": { "type": "integer" }, "description": "Minimum span count" },
              { "name": "max_spans", "in": "query", "schema": { "type": "integer" }, "description": "Maximum span count" },
              { "name": "min_cost", "in": "query", "schema": { "type": "number" }, "description": "Minimum cost" },
              { "name": "max_cost", "in": "query", "schema": { "type": "number" }, "description": "Maximum cost" }
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test -run TestListTracesParsesMinMaxFilters ./internal/api/`
Expected: PASS.

- [ ] **Step 7: Run full api test suite + build**

Run: `go test ./internal/api/ && go build ./...`
Expected: PASS, build succeeds.

- [ ] **Step 8: Commit**

```bash
git add internal/storage/storage.go internal/api/trace_handler.go internal/api/openapi.go internal/api/trace_handler_test.go
git commit -m "feat(api): parse min/max span-count and cost trace filters"
```

---

## Task 2: SQLite WHERE clause for span-count and cost filters

**Files:**
- Modify: `internal/storage/sqlite_store.go:1657-1695` (`buildSqliteTraceWhereClause`)
- Test: `internal/storage/sqlite_trace_filter_test.go` (new, build tag `!local_engine && !nosqlite`)

**Interfaces:**
- Consumes: `storage.TraceQuery` fields from Task 1.
- Produces: SQLite `traces` table now filters by `span_count` and `cost` columns.

- [ ] **Step 1: Write the failing test**

Create `internal/storage/sqlite_trace_filter_test.go`:

```go
//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
)

// insertTestTrace inserts a single-span trace with the given fields for filter tests.
func insertTestTrace(t *testing.T, s Store, tid [16]byte, service string, dur uint64, spans uint16, cost *float64) {
	t.Helper()
	u := func(v uint32) *uint32 { return &v }
	span := Span{
		TraceID: tid, SpanID: [8]byte{tid[15]}, Name: "op", Kind: 2,
		StartTimeMS: 1000, EndTimeMS: 1000 + dur, DurationMS: dur,
		InputTokens: u(1), OutputTokens: u(1), TotalTokens: u(2),
	}
	res := ResourceInfo{Attributes: map[string]string{"service.name": service}}
	if err := s.InsertSpans(context.Background(), res, ScopeInfo{}, []Span{span}); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}
	s.UpdateTraceCost(context.Background(), tid)
	// Overwrite span_count directly via the trace row is not exposed; rely on
	// re-inserting additional spans to raise the count when needed.
	_ = spans
}

func TestSqliteListTracesFiltersBySpansAndCost(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSqliteStore(dir)
	if err != nil {
		t.Fatalf("NewSqliteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.UpsertModelPricing(ctx, ModelPricing{ModelName: "m", InputPrice: 1, OutputPrice: 1, Currency: "USD"})

	cost := func(v float64) *float64 { return &v }

	// Trace A: 2 spans, cost 1.0
	insertTestTrace(t, s, [16]byte{15: 1}, "svc", 100, 2, cost(1.0))
	insertTestTrace(t, s, [16]byte{15: 1}, "svc", 100, 2, cost(1.0)) // second span → 2 spans
	// Trace B: 5 spans, cost 5.0
	insertTestTrace(t, s, [16]byte{15: 2}, "svc", 200, 5, cost(5.0))
	for i := 0; i < 4; i++ {
		insertTestTrace(t, s, [16]byte{15: 2}, "svc", 200, 5, cost(5.0))
	}
	s.UpdateTraceCost(ctx, [16]byte{15: 1})
	s.UpdateTraceCost(ctx, [16]byte{15: 2})

	mustLen := func(q TraceQuery, want int) {
		t.Helper()
		res, err := s.ListTraces(ctx, q)
		if err != nil {
			t.Fatalf("ListTraces: %v", err)
		}
		if len(res.Traces) != want {
			t.Errorf("got %d traces, want %d; ids=%v", len(res.Traces), want, traceIDs(res.Traces))
		}
	}

	mustLen(TraceQuery{Page: 1, PageSize: 100, MinSpanCount: 3}, 1)      // only B (5 spans)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MaxSpanCount: 3}, 1)      // only A (2 spans)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MinCost: 2.0}, 1)         // only B (cost 5)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MaxCost: 2.0}, 1)         // only A (cost 1)
}

func traceIDs(items []TraceListItem) []string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.TraceIDHex
	}
	return ids
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags '!local_engine !nosqlite' -run TestSqliteListTracesFiltersBySpansAndCost ./internal/storage/`

Note: on the project's default build (no tag), SQLite is active. If unsure of tag syntax on your shell, run `go test -run TestSqliteListTracesFiltersBySpansAndCost ./internal/storage/` and check the result. Expected: FAIL — no filtering, so counts will be 2 (both traces returned for every query).

- [ ] **Step 3: Add WHERE clauses in SQLite**

In `internal/storage/sqlite_store.go`, edit `buildSqliteTraceWhereClause` — after the `q.MaxDuration` block (line 1688) and before `where := ""` (line 1690), add:

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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestSqliteListTracesFiltersBySpansAndCost ./internal/storage/`
Expected: PASS. If the test is flaky on span counts (re-inserting spans may dedupe), inspect `traceIDs` output and adjust fixtures — the cost-filter assertions (`MinCost`/`MaxCost`) must pass regardless.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/sqlite_store.go internal/storage/sqlite_trace_filter_test.go
git commit -m "feat(storage): sqlite span-count and cost trace filters"
```

---

## Task 3: Memstore filter checks + fill Cost on item builder

**Files:**
- Modify: `internal/storage/memstore.go:323-407` (`ListTraces`)
- Test: `internal/storage/memstore_trace_filter_test.go` (new, build tag `!local_engine && nosqlite`)

**Interfaces:**
- Consumes: `storage.TraceQuery` fields from Task 1.
- Produces: memstore filters by span count and cost; `TraceListItem` now carries `Cost`/`CostCurrency`.

- [ ] **Step 1: Write the failing test**

Create `internal/storage/memstore_trace_filter_test.go`:

```go
//go:build !local_engine && nosqlite

package storage

import (
	"context"
	"testing"
)

func TestMemstoreListTracesFiltersBySpansAndCost(t *testing.T) {
	dir := t.TempDir()
	s, err := NewChDBStore(dir) // memstore constructor in nosqlite builds
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.UpsertModelPricing(ctx, ModelPricing{ModelName: "m", InputPrice: 1, OutputPrice: 1, Currency: "USD"})
	u := func(v uint32) *uint32 { return &v }
	mdl := "m"

	// Trace A: 1 span, cost ~2 (1 in + 1 out at price 1)
	spanA := Span{TraceID: [16]byte{15: 1}, SpanID: [8]byte{1}, Name: "a", Kind: 2,
		StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
		InputTokens: u(1), OutputTokens: u(1), TotalTokens: u(2), GenAIRequestModel: &mdl}
	s.InsertSpans(ctx, ResourceInfo{Attributes: map[string]string{"service.name": "svc"}}, ScopeInfo{}, []Span{spanA})
	s.UpdateTraceCost(ctx, [16]byte{15: 1})

	// Trace B: 3 spans, cost ~6 (3 spans × 2 tokens)
	for i := 0; i < 3; i++ {
		spanB := Span{TraceID: [16]byte{15: 2}, SpanID: [8]byte{byte(i + 1)}, Name: "b", Kind: 2,
			StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
			InputTokens: u(1), OutputTokens: u(1), TotalTokens: u(2), GenAIRequestModel: &mdl}
		s.InsertSpans(ctx, ResourceInfo{Attributes: map[string]string{"service.name": "svc"}}, ScopeInfo{}, []Span{spanB})
	}
	s.UpdateTraceCost(ctx, [16]byte{15: 2})

	mustLen := func(q TraceQuery, want int) {
		t.Helper()
		res, err := s.ListTraces(ctx, q)
		if err != nil {
			t.Fatalf("ListTraces: %v", err)
		}
		if len(res.Traces) != want {
			t.Errorf("got %d, want %d; ids=%v", len(res.Traces), want, traceIDs(res.Traces))
		}
	}

	mustLen(TraceQuery{Page: 1, PageSize: 100, MinSpanCount: 2}, 1)  // only B (3 spans)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MaxSpanCount: 2}, 1)  // only A (1 span)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MinCost: 3.0}, 1)     // only B (cost ~6)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MaxCost: 3.0}, 1)     // only A (cost ~2)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags 'nosqlite' -run TestMemstoreListTracesFiltersBySpansAndCost ./internal/storage/`
Expected: FAIL — no span/cost filtering yet; counts will be 2 for every query.

- [ ] **Step 3: Add filter checks in memstore**

In `internal/storage/memstore.go`, in `ListTraces`, after the `q.MaxDuration` block (line 358) and before `filtered = append(...)`, add:

```go
		if q.MinSpanCount > 0 && t.SpanCount < q.MinSpanCount {
			continue
		}
		if q.MaxSpanCount > 0 && t.SpanCount > q.MaxSpanCount {
			continue
		}
		if q.MinCost > 0 && (t.Cost == nil || *t.Cost < q.MinCost) {
			continue
		}
		if q.MaxCost > 0 && (t.Cost == nil || *t.Cost > q.MaxCost) {
			continue
		}
```

- [ ] **Step 4: Fill Cost/CostCurrency on memstore item builder**

In `internal/storage/memstore.go`, in the `TraceListItem` builder (lines 386-396), add `Cost` and `CostCurrency`:

```go
		items[i] = TraceListItem{
			TraceIDHex:   TraceIDToHex(t.TraceID),
			RootSpanID:   SpanIDToHex(t.RootSpanID),
			RootName:     t.RootName,
			RootService:  t.ResourceAttrs["service.name"],
			StartTimeMS:  t.StartTimeMS,
			DurationMS:   t.DurationMS,
			SpanCount:    t.SpanCount,
			Status:       StatusCodeToString(t.StatusCode),
			TotalTokens:  t.TotalTokens,
			Cost:         t.Cost,
			CostCurrency: t.CostCurrency,
		}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -tags 'nosqlite' -run TestMemstoreListTracesFiltersBySpansAndCost ./internal/storage/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/storage/memstore.go internal/storage/memstore_trace_filter_test.go
git commit -m "feat(storage): memstore span-count and cost trace filters"
```

---

## Task 4: chDB WHERE clause for span-count and cost filters

**Files:**
- Modify: `internal/storage/chdb_query.go:137-180` (`buildTraceWhereClause`)

**Interfaces:**
- Consumes: `storage.TraceQuery` fields from Task 1.
- Produces: chDB `traces` table WHERE clause filters by `span_count` and `cost` (pre-GROUP BY, matching existing duration-filter semantics).

Note: chDB requires the `local_engine` build tag (CGO) and has no unit test harness here. The change is mechanical and mirrors the existing duration block. Verify via `go build -tags local_engine ./...` if the CGO toolchain is available; otherwise rely on the compile check in Step 2.

- [ ] **Step 1: Add WHERE clauses in chDB**

In `internal/storage/chdb_query.go`, in `buildTraceWhereClause`, after the `q.MaxDuration` block (line 174) and before `if len(clauses) == 0`, add:

```go
	if q.MinSpanCount > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"span_count >= %d", q.MinSpanCount,
		))
	}
	if q.MaxSpanCount > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"span_count <= %d", q.MaxSpanCount,
		))
	}
	if q.MinCost > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"cost >= %s", strconv.FormatFloat(q.MinCost, 'f', -1, 64),
		))
	}
	if q.MaxCost > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"cost <= %s", strconv.FormatFloat(q.MaxCost, 'f', -1, 64),
		))
	}
```

Add `"strconv"` to the import block at the top of `chdb_query.go` if not already present (it likely is not).

- [ ] **Step 2: Compile check (no CGO)**

Run: `go build ./internal/storage/`
Expected: PASS (the `local_engine` build tag excludes this file in non-CGO builds, but the package must still compile — confirm `strconv` import is valid). If `strconv` is unused in the non-CGO build path, gate the import behind the build tag or move the `FormatFloat` usage so it is always referenced. If the import causes an "unused" error in the non-CGO build, run `go vet -tags local_engine ./internal/storage/` instead and skip the non-CGO compile check.

- [ ] **Step 3: Commit**

```bash
git add internal/storage/chdb_query.go
git commit -m "feat(storage): chdb span-count and cost trace filters"
```

---

## Task 5: Frontend client — `TraceQuery` fields + `listTraces` params

**Files:**
- Modify: `web/src/api/client.ts:82-92` (TraceQuery interface) and `110-122` (listTraces)

**Interfaces:**
- Produces: `TraceQuery` interface adds `min_spans?/max_spans?/min_cost?/max_cost?` (number). `listTraces` sends them as query params. (Duration fields already exist and are already sent.)

Note: `listTraces` explicitly enumerates params (it does NOT spread `query`), so the four new fields must be added to the call site manually.

- [ ] **Step 1: Extend the `TraceQuery` interface**

In `web/src/api/client.ts`, edit the interface at line 82:

```ts
export interface TraceQuery {
  page?: number
  page_size?: number
  service?: string
  status?: string
  q?: string
  start?: number
  end?: number
  min_duration?: number
  max_duration?: number
  min_spans?: number
  max_spans?: number
  min_cost?: number
  max_cost?: number
}
```

- [ ] **Step 2: Add the new params to `listTraces`**

In `web/src/api/client.ts`, edit the call at line 110:

```ts
export async function listTraces(query: TraceQuery): Promise<TraceListResponse> {
  return get<TraceListResponse>(`${BASE_URL}/traces`, {
    page: query.page,
    page_size: query.page_size,
    service: query.service,
    status: query.status,
    q: query.q,
    start: query.start,
    end: query.end,
    min_duration: query.min_duration,
    max_duration: query.max_duration,
    min_spans: query.min_spans,
    max_spans: query.max_spans,
    min_cost: query.min_cost,
    max_cost: query.max_cost,
  })
}
```

- [ ] **Step 3: TypeScript check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS (no errors). The new optional fields are not yet referenced by the view, which is fine.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat(web): add span/cost trace filter params to API client"
```

---

## Task 6: Frontend popovers + i18n

**Files:**
- Modify: `web/src/views/TraceList.vue` (template lines 36-65, script lines 242-268, 311-317)
- Modify: `web/src/i18n/locales/en.ts:55-75` and `zh.ts:55-75`

**Interfaces:**
- Consumes: `TraceQuery` fields from Task 5. `filters` ref spreads into `listTraces` in `fetchTraces` (line 279), so once the ref carries the new fields they are sent automatically.

- [ ] **Step 1: Add i18n keys (English)**

In `web/src/i18n/locales/en.ts`, inside the `traceList` block (line 55), add after `time:`:

```ts
    cost: 'Cost',
    minDuration: 'Min duration (ms)',
    maxDuration: 'Max duration (ms)',
    minSpans: 'Min spans',
    maxSpans: 'Max spans',
    minCost: 'Min cost',
    maxCost: 'Max cost',
    apply: 'Apply',
    clear: 'Clear',
```

- [ ] **Step 2: Add i18n keys (Chinese)**

In `web/src/i18n/locales/zh.ts`, inside the `traceList` block (line 55), add after `time:`:

```ts
    cost: '费用',
    minDuration: '最小耗时 (ms)',
    maxDuration: '最大耗时 (ms)',
    minSpans: '最小跨度数',
    maxSpans: '最大跨度数',
    minCost: '最小费用',
    maxCost: '最大费用',
    apply: '应用',
    clear: '清除',
```

- [ ] **Step 3: Extend filter state and openFilter union**

In `web/src/views/TraceList.vue`, replace the `filters` ref (line 242):

```ts
const filters = ref({
  q: '',
  service: '',
  status: '',
  min_duration: '' as number | '',
  max_duration: '' as number | '',
  min_spans: '' as number | '',
  max_spans: '' as number | '',
  min_cost: '' as number | '',
  max_cost: '' as number | '',
})
```

Replace the `openFilter` ref and `toggleFilter` (lines 248-252):

```ts
const openFilter = ref<'service' | 'status' | 'duration' | 'spans' | 'cost' | ''>('')

function toggleFilter(col: 'service' | 'status' | 'duration' | 'spans' | 'cost') {
  openFilter.value = openFilter.value === col ? '' : col
}
```

- [ ] **Step 4: Add Apply/Clear handlers + a shared min/max popover component block**

Add these helpers after `selectStatus` (around line 268):

```ts
// Apply min/max filter values from the popover inputs, then refetch.
// `temp` holds the in-progress input values; on Apply we copy them into `filters`.
const filterTemp = ref<Record<string, number | ''>>({})

function openMinMax(col: 'duration' | 'spans' | 'cost') {
  // Seed temp from current filter values so editing is non-destructive.
  const minKey = `min_${col === 'duration' ? 'duration' : col === 'spans' ? 'spans' : 'cost'}`
  const maxKey = `max_${col === 'duration' ? 'duration' : col === 'spans' ? 'spans' : 'cost'}`
  filterTemp.value = {
    [minKey]: (filters.value as any)[minKey] ?? '',
    [maxKey]: (filters.value as any)[maxKey] ?? '',
  }
  openFilter.value = col
}

function applyMinMax(col: 'duration' | 'spans' | 'cost') {
  const minKey = col === 'duration' ? 'min_duration' : col === 'spans' ? 'min_spans' : 'min_cost'
  const maxKey = col === 'duration' ? 'max_duration' : col === 'spans' ? 'max_spans' : 'max_cost'
  ;(filters.value as any)[minKey] = filterTemp.value[minKey] ?? ''
  ;(filters.value as any)[maxKey] = filterTemp.value[maxKey] ?? ''
  openFilter.value = ''
  fetchTraces(1)
}

function clearMinMax(col: 'duration' | 'spans' | 'cost') {
  const minKey = col === 'duration' ? 'min_duration' : col === 'spans' ? 'min_spans' : 'min_cost'
  const maxKey = col === 'duration' ? 'max_duration' : col === 'spans' ? 'max_spans' : 'max_cost'
  ;(filters.value as any)[minKey] = ''
  ;(filters.value as any)[maxKey] = ''
  filterTemp.value = {}
  openFilter.value = ''
  fetchTraces(1)
}

function hasMinMax(col: 'duration' | 'spans' | 'cost'): boolean {
  const minKey = col === 'duration' ? 'min_duration' : col === 'spans' ? 'min_spans' : 'min_cost'
  const maxKey = col === 'duration' ? 'max_duration' : col === 'spans' ? 'max_spans' : 'max_cost'
  return !!(filters.value as any)[minKey] || !!(filters.value as any)[maxKey]
}
```

> Note on `any`: the CLAUDE.md rule says avoid `any`, but the `filters` object has mixed string/number|'' fields. Prefer a typed alternative: define `filters` as `ref<{
  q: string; service: string; status: string;
  min_duration: number | ''; max_duration: number | '';
  min_spans: number | ''; max_spans: number | '';
  min_cost: number | ''; max_cost: number | '';
}>(...)`. Then replace every `(filters.value as any)[key]` with a typed switch on `col`. Implementer: use the typed form, not `any`.

- [ ] **Step 5: Replace the Duration, Spans, and Cost column headers with popovers**

In `web/src/views/TraceList.vue`, replace the three `<th>` elements at lines 48-49, 49, and 64.

Replace the Duration header (line 48):

```html
            <th class="has-filter">
              <div class="th-head">
                <span>{{ t('traceList.duration') }}</span>
                <button class="filter-btn" :class="{ active: hasMinMax('duration') }" :title="t('traceList.filter')" @click.stop="openMinMax('duration')">▼</button>
              </div>
              <div v-if="openFilter === 'duration'" class="filter-popover" @click.stop>
                <label>{{ t('traceList.minDuration') }}</label>
                <input type="number" v-model="filterTemp.min_duration" @keyup.enter="applyMinMax('duration')" />
                <label>{{ t('traceList.maxDuration') }}</label>
                <input type="number" v-model="filterTemp.max_duration" @keyup.enter="applyMinMax('duration')" />
                <div class="filter-actions">
                  <button class="btn" @click="applyMinMax('duration')">{{ t('traceList.apply') }}</button>
                  <button class="btn" @click="clearMinMax('duration')">{{ t('traceList.clear') }}</button>
                </div>
              </div>
            </th>
```

Replace the Spans header (line 49):

```html
            <th class="has-filter">
              <div class="th-head">
                <span>{{ t('traceList.spans') }}</span>
                <button class="filter-btn" :class="{ active: hasMinMax('spans') }" :title="t('traceList.filter')" @click.stop="openMinMax('spans')">▼</button>
              </div>
              <div v-if="openFilter === 'spans'" class="filter-popover" @click.stop>
                <label>{{ t('traceList.minSpans') }}</label>
                <input type="number" v-model="filterTemp.min_spans" @keyup.enter="applyMinMax('spans')" />
                <label>{{ t('traceList.maxSpans') }}</label>
                <input type="number" v-model="filterTemp.max_spans" @keyup.enter="applyMinMax('spans')" />
                <div class="filter-actions">
                  <button class="btn" @click="applyMinMax('spans')">{{ t('traceList.apply') }}</button>
                  <button class="btn" @click="clearMinMax('spans')">{{ t('traceList.clear') }}</button>
                </div>
              </div>
            </th>
```

Replace the Cost header (line 64 — currently `<th>Cost</th>`):

```html
            <th class="has-filter">
              <div class="th-head">
                <span>{{ t('traceList.cost') }}</span>
                <button class="filter-btn" :class="{ active: hasMinMax('cost') }" :title="t('traceList.filter')" @click.stop="openMinMax('cost')">▼</button>
              </div>
              <div v-if="openFilter === 'cost'" class="filter-popover" @click.stop>
                <label>{{ t('traceList.minCost') }}</label>
                <input type="number" step="0.01" v-model="filterTemp.min_cost" @keyup.enter="applyMinMax('cost')" />
                <label>{{ t('traceList.maxCost') }}</label>
                <input type="number" step="0.01" v-model="filterTemp.max_cost" @keyup.enter="applyMinMax('cost')" />
                <div class="filter-actions">
                  <button class="btn" @click="applyMinMax('cost')">{{ t('traceList.apply') }}</button>
                  <button class="btn" @click="clearMinMax('cost')">{{ t('traceList.clear') }}</button>
                </div>
              </div>
            </th>
```

- [ ] **Step 6: Update `reset()` to clear the new filter fields**

In `web/src/views/TraceList.vue`, replace `reset` (line 311):

```ts
function reset() {
  filters.value = {
    q: '', service: '', status: '',
    min_duration: '', max_duration: '',
    min_spans: '', max_spans: '',
    min_cost: '', max_cost: '',
  }
  openFilter.value = ''
  filterTemp.value = {}
  resetKey.value++
}
```

- [ ] **Step 7: Add minimal CSS for the popover inputs**

In `web/src/views/TraceList.vue` `<style scoped>` block, add (near the existing `.filter-popover` rules):

```css
.filter-popover label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 6px 0 2px;
}
.filter-popover input[type="number"] {
  width: 100%;
  padding: 4px 6px;
  border: 1px solid var(--border-primary);
  background: var(--bg-primary);
  color: var(--text-primary);
  border-radius: 4px;
}
.filter-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
.filter-actions .btn {
  flex: 1;
}
```

- [ ] **Step 8: TypeScript check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS. Fix any type errors (e.g. `filterTemp` indexing — type it as `Record<string, number | ''>` and access via the keys shown).

- [ ] **Step 9: Manual verification**

Run: `make run` and open http://localhost:8080/traces.
Expected: funnel icons on Duration, Spans, Cost columns. Clicking each opens a popover with Min/Max inputs; Apply filters, Clear resets. Verify with real traces that have varied span counts/durations/costs.

- [ ] **Step 10: Commit**

```bash
git add web/src/views/TraceList.vue web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat(web): min/max filter popovers for duration, spans, cost"
```

---

## Self-Review Notes

- **Spec coverage:** Span count (Tasks 1-6), duration (Task 1 parses; backend already filters in all 3 stores; Task 6 adds the UI), cost (Tasks 1-6). i18n (Task 6). Memstore Cost display fix (Task 3 Step 4). OpenAPI (Task 1 Step 5). All spec sections covered.
- **Type consistency:** Field names are consistent across layers: Go `MinSpanCount`/`MaxSpanCount`/`MinCost`/`MaxCost`, query params `min_spans`/`max_spans`/`min_cost`/`max_cost`, TS `min_spans`/`max_spans`/`min_cost`/`max_cost`. Duration follows the existing `min_duration`/`max_duration` naming.
- **Caveat:** Task 2's span-count test fixtures rely on re-inserting spans to raise `span_count`. If `InsertSpans` dedupes by span ID, use distinct `SpanID` values (the test already does `[8]byte{tid[15]}` for extras — ensure they are unique; the plan uses `[8]byte{byte(i+1)}`). The cost-filter assertions are independent of span count and must pass regardless.
