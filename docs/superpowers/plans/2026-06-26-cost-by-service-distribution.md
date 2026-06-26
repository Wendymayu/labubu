# Cost Breakdown by Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** [`docs/superpowers/specs/2026-06-26-cost-by-service-distribution-design.md`](../specs/2026-06-26-cost-by-service-distribution-design.md)

**Goal:** Add a "Cost by Service" breakdown to the cost dashboard, switchable with the existing "Cost by Model" view via a `group_by` query param on `/cost-summary`.

**Architecture:** `GET /cost-summary?group_by=model|service` (default `model`) returns `overview` plus only the requested breakdown (`by_model` or `by_service`), echoing `group_by`. The store reads `CostQuery.GroupBy` to pick the dimension; `by_service` joins `spans`→`traces` for `service.name` (by_model groups spans directly, no join). Frontend renders a segmented toggle and a shared table whose first column swaps with the dimension.

**Tech Stack:** Go 1.22 `http.ServeMux` + `storage.Store` interface (SQLite default, memstore fallback); Vue 3 `<script setup>` + `vue-i18n`; `vue-tsc --noEmit` for type checks.

**Windows/test notes (from project memory):** No `make` in Git Bash — use `go` commands directly. WDAC may intermittently block `go test` temp binaries; if a test run fails with a permissions/exec error, fall back to `go test -c <pkg>` then run the produced `.exe` directly.

---

## File Structure

- `internal/storage/storage.go` — add `ServiceCostItem`; extend `CostSummaryResult` (`ByService`, `GroupBy`) and `CostQuery` (`GroupBy`).
- `internal/storage/sqlite_store.go` — `GetCostSummary`: branch on `q.GroupBy`, add the `by_service` query (spans↔traces join).
- `internal/storage/memstore.go` — `GetCostSummary`: branch on `q.GroupBy`, add in-memory `by_service` aggregation.
- `internal/api/cost_handler.go` — parse/validate `group_by`, pass through `CostQuery`, echo `GroupBy` on the result.
- `internal/api/trace_handler_test.go` — `handlerMockStore`: capture the last `CostQuery` so handler tests can assert pass-through.
- `internal/api/cost_handler_test.go` — `group_by` dispatch tests.
- `internal/storage/sqlite_cost_test.go` — `TestGetCostSummaryByService` (build tag `!local_engine && !nosqlite`).
- `internal/storage/memstore_cost_test.go` — `TestMemstoreGetCostSummaryByService` (build tag `!local_engine && nosqlite`).
- `web/src/api/client.ts` — add `ServiceCost`; extend `CostSummary`; `getCostSummary(period, groupBy)`.
- `web/src/views/CostDashboard.vue` — segmented toggle, dynamic heading, shared table.
- `web/src/i18n/locales/en.ts` + `zh.ts` — `byModel` / `byService` / `costByService` / `service` keys.

---

## Task 1: Storage types

**Files:**
- Modify: `internal/storage/storage.go:90-127`

- [ ] **Step 1: Extend `CostQuery` with `GroupBy`**

In `internal/storage/storage.go`, replace the `CostQuery` struct (lines 90-94):

```go
// CostQuery defines filters for cost summary aggregation.
type CostQuery struct {
	StartTimeMS uint64
	EndTimeMS   uint64
	GroupBy     string // "model" (default) or "service"
}
```

- [ ] **Step 2: Add `ServiceCostItem` type**

In `internal/storage/storage.go`, insert immediately after the `ModelCostItem` struct (after line 119, before the blank line preceding `CostSummaryResult`):

```go
// ServiceCostItem holds cost aggregation for a single service.
// Mirrors ModelCostItem; the dimension value is the trace's service.name.
type ServiceCostItem struct {
	Service             string  `json:"service"`
	Cost                float64 `json:"cost"`
	Tokens              uint64  `json:"tokens"`
	InputTokens         uint64  `json:"input_tokens"`
	CacheCreationTokens uint64  `json:"cache_creation_tokens"`
	CacheReadTokens     uint64  `json:"cache_read_tokens"`
	OutputTokens        uint64  `json:"output_tokens"`
	TraceCount          int     `json:"trace_count"`
	AvgCost             float64 `json:"avg_cost"`
}
```

- [ ] **Step 3: Extend `CostSummaryResult` with `GroupBy` and `ByService`**

In `internal/storage/storage.go`, replace the `CostSummaryResult` struct (lines 121-127):

```go
// CostSummaryResult holds the full cost dashboard response.
type CostSummaryResult struct {
	Period    string            `json:"period"`
	Currency  string            `json:"currency"`
	Overview  CostOverview      `json:"overview"`
	GroupBy   string            `json:"group_by"`
	ByModel   []ModelCostItem   `json:"by_model"`
	ByService []ServiceCostItem `json:"by_service"`
}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/storage/...`
Expected: no output, exit 0.

- [ ] **Step 5: Verify existing tests still pass**

Run: `go test ./internal/storage/ -run TestGetCostSummaryReconcilesAndBreaksOutCache -v`
Expected: PASS (the new fields are zero-valued, existing behavior unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/storage/storage.go
git commit -m "feat(storage): add ServiceCostItem + GroupBy to cost summary types"
```

---

## Task 2: SQLite `by_service` (TDD)

**Files:**
- Test: `internal/storage/sqlite_cost_test.go` (append)
- Modify: `internal/storage/sqlite_store.go:1337-1384` (the `by_model` block + return)

- [ ] **Step 1: Write the failing test**

Append to `internal/storage/sqlite_cost_test.go` (which has build tag `//go:build !local_engine && !nosqlite`):

```go
func TestGetCostSummaryByService(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	store.UpsertModelPricing(ctx, ModelPricing{
		ModelName: "glm-5.2", InputPrice: 10.0, OutputPrice: 30.0, Currency: "CNY",
	})
	u := func(v uint32) *uint32 { return &v }
	mdl := "glm-5.2"

	// Three traces: services alpha, beta, and one with no service.name.
	type fixture struct {
		tid [16]byte
		res ResourceInfo
		in  uint32
		out uint32
	}
	fixtures := []fixture{
		{tid: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, res: ResourceInfo{Attributes: map[string]string{"service.name": "alpha"}}, in: 100, out: 10},
		{tid: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}, res: ResourceInfo{Attributes: map[string]string{"service.name": "beta"}}, in: 50, out: 5},
		{tid: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}, res: ResourceInfo{Attributes: map[string]string{}}, in: 20, out: 2},
	}
	for i, f := range fixtures {
		span := Span{
			TraceID: f.tid, SpanID: [8]byte{byte(i + 1)}, Name: "llm", Kind: 2,
			StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
			InputTokens: u(f.in), OutputTokens: u(f.out),
			TotalTokens: u(f.in + f.out), GenAIRequestModel: &mdl,
		}
		if err := store.InsertSpans(ctx, f.res, ScopeInfo{}, []Span{span}); err != nil {
			t.Fatalf("InsertSpans #%d: %v", i, err)
		}
		store.UpdateTraceCost(ctx, f.tid)
	}

	s, err := store.GetCostSummary(ctx, CostQuery{StartTimeMS: 0, EndTimeMS: 100000, GroupBy: "service"})
	if err != nil {
		t.Fatalf("GetCostSummary: %v", err)
	}
	if s.GroupBy != "service" {
		t.Errorf("GroupBy = %s, want service", s.GroupBy)
	}
	if len(s.ByService) != 3 {
		t.Fatalf("by_service rows = %d, want 3", len(s.ByService))
	}
	if len(s.ByModel) != 0 {
		t.Errorf("by_model rows = %d, want 0 when group_by=service", len(s.ByModel))
	}
	// Ordered by cost DESC: alpha > beta > (unknown).
	if s.ByService[0].Service != "alpha" {
		t.Errorf("first service = %s, want alpha", s.ByService[0].Service)
	}
	wantTokens := map[string]uint64{"alpha": 110, "beta": 55, "(unknown)": 22}
	for _, row := range s.ByService {
		want, ok := wantTokens[row.Service]
		if !ok {
			t.Errorf("unexpected service %q", row.Service)
			continue
		}
		if row.Tokens != want {
			t.Errorf("service %s tokens = %d, want %d", row.Service, row.Tokens, want)
		}
		if row.TraceCount != 1 {
			t.Errorf("service %s trace_count = %d, want 1", row.Service, row.TraceCount)
		}
	}
	for i := 1; i < len(s.ByService); i++ {
		if s.ByService[i].Cost > s.ByService[i-1].Cost {
			t.Errorf("by_service not sorted by cost desc at index %d", i)
		}
	}
	var sum float64
	for _, row := range s.ByService {
		sum += row.Cost
	}
	if sum < s.Overview.TotalCost-0.000001 || sum > s.Overview.TotalCost+0.000001 {
		t.Errorf("by_service cost sum = %v, overview = %v (must match)", sum, s.Overview.TotalCost)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/storage/ -run TestGetCostSummaryByService -v`
Expected: FAIL — `by_service rows = 0, want 3` (the store ignores `GroupBy` and still populates `by_model`; `ByService` is nil).

- [ ] **Step 3: Implement the `by_service` branch in SQLite**

In `internal/storage/sqlite_store.go`, the `by_model` section starts at line 1337 with the comment `// by_model: group spans directly (no fan-out join).` and ends at line 1384 with the `return &CostSummaryResult{...}`.

**Insert this block immediately BEFORE that `// by_model:` comment** (i.e., after the overview token-bucket computation that ends at line 1335):

```go
	// by_service: join spans→traces for service.name (service lives on the
	// trace resource, not on spans). Only compute when requested.
	if q.GroupBy == "service" {
		rows, err := s.db.QueryContext(ctx,
			`SELECT
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
			 ORDER BY cost DESC`,
			q.StartTimeMS, q.EndTimeMS,
		)
		if err != nil {
			return nil, fmt.Errorf("query cost by service: %w", err)
		}
		defer rows.Close()

		var byService []ServiceCostItem
		for rows.Next() {
			var sc ServiceCostItem
			if err := rows.Scan(&sc.Service, &sc.Cost, &sc.InputTokens, &sc.CacheCreationTokens,
				&sc.CacheReadTokens, &sc.OutputTokens, &sc.TraceCount); err != nil {
				return nil, fmt.Errorf("scan service cost: %w", err)
			}
			sc.Tokens = sc.InputTokens + sc.CacheCreationTokens + sc.CacheReadTokens + sc.OutputTokens
			if sc.TraceCount > 0 {
				sc.AvgCost = math.Round(sc.Cost/float64(sc.TraceCount)*1e6) / 1e6
			}
			byService = append(byService, sc)
		}
		if byService == nil {
			byService = []ServiceCostItem{}
		}

		return &CostSummaryResult{
			Currency:  currency,
			Overview:  overview,
			GroupBy:   "service",
			ByService: byService,
		}, nil
	}

```

- [ ] **Step 4: Echo `GroupBy: "model"` on the existing by_model return**

In `internal/storage/sqlite_store.go`, the existing by_model return (around line 1380-1384) is:

```go
	return &CostSummaryResult{
		Currency: currency,
		Overview: overview,
		ByModel:  byModel,
	}, nil
```

Change it to:

```go
	return &CostSummaryResult{
		Currency: currency,
		Overview: overview,
		GroupBy:  "model",
		ByModel:  byModel,
	}, nil
```

- [ ] **Step 5: Run the new test to verify it passes**

Run: `go test ./internal/storage/ -run TestGetCostSummaryByService -v`
Expected: PASS.

- [ ] **Step 6: Run the existing by_model test to verify no regression**

Run: `go test ./internal/storage/ -run TestGetCostSummaryReconcilesAndBreaksOutCache -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/storage/sqlite_store.go internal/storage/sqlite_cost_test.go
git commit -m "feat(storage): sqlite by_service cost breakdown via group_by"
```

---

## Task 3: memstore `by_service` (TDD)

**Files:**
- Test: `internal/storage/memstore_cost_test.go` (append)
- Modify: `internal/storage/memstore.go:947-1059` (the `GetCostSummary` body)

- [ ] **Step 1: Write the failing test**

Append to `internal/storage/memstore_cost_test.go` (build tag `//go:build !local_engine && nosqlite`):

```go
func TestMemstoreGetCostSummaryByService(t *testing.T) {
	dir := t.TempDir()
	s, err := NewChDBStore(dir) // memstore constructor in nosqlite builds
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.UpsertModelPricing(ctx, ModelPricing{
		ModelName: "glm-5.2", InputPrice: 10.0, OutputPrice: 30.0, Currency: "CNY",
	})
	u := func(v uint32) *uint32 { return &v }
	mdl := "glm-5.2"

	type fixture struct {
		tid [16]byte
		res ResourceInfo
		in  uint32
		out uint32
	}
	fixtures := []fixture{
		{tid: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, res: ResourceInfo{Attributes: map[string]string{"service.name": "alpha"}}, in: 100, out: 10},
		{tid: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}, res: ResourceInfo{Attributes: map[string]string{"service.name": "beta"}}, in: 50, out: 5},
		{tid: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}, res: ResourceInfo{Attributes: map[string]string{}}, in: 20, out: 2},
	}
	for i, f := range fixtures {
		span := Span{
			TraceID: f.tid, SpanID: [8]byte{byte(i + 1)}, Name: "llm", Kind: 2,
			StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
			InputTokens: u(f.in), OutputTokens: u(f.out),
			TotalTokens: u(f.in + f.out), GenAIRequestModel: &mdl,
		}
		if err := s.InsertSpans(ctx, f.res, ScopeInfo{}, []Span{span}); err != nil {
			t.Fatalf("InsertSpans #%d: %v", i, err)
		}
		s.UpdateTraceCost(ctx, f.tid)
	}

	summary, err := s.GetCostSummary(ctx, CostQuery{StartTimeMS: 0, EndTimeMS: 100000, GroupBy: "service"})
	if err != nil {
		t.Fatalf("GetCostSummary: %v", err)
	}
	if summary.GroupBy != "service" {
		t.Errorf("GroupBy = %s, want service", summary.GroupBy)
	}
	if len(summary.ByService) != 3 {
		t.Fatalf("by_service rows = %d, want 3", len(summary.ByService))
	}
	if len(summary.ByModel) != 0 {
		t.Errorf("by_model rows = %d, want 0 when group_by=service", len(summary.ByModel))
	}
	if summary.ByService[0].Service != "alpha" {
		t.Errorf("first service = %s, want alpha", summary.ByService[0].Service)
	}
	wantTokens := map[string]uint64{"alpha": 110, "beta": 55, "(unknown)": 22}
	for _, row := range summary.ByService {
		want, ok := wantTokens[row.Service]
		if !ok {
			t.Errorf("unexpected service %q", row.Service)
			continue
		}
		if row.Tokens != want {
			t.Errorf("service %s tokens = %d, want %d", row.Service, row.Tokens, want)
		}
		if row.TraceCount != 1 {
			t.Errorf("service %s trace_count = %d, want 1", row.Service, row.TraceCount)
		}
	}
	for i := 1; i < len(summary.ByService); i++ {
		if summary.ByService[i].Cost > summary.ByService[i-1].Cost {
			t.Errorf("by_service not sorted by cost desc at index %d", i)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags nosqlite ./internal/storage/ -run TestMemstoreGetCostSummaryByService -v`
Expected: FAIL — `by_service rows = 0, want 3`.

- [ ] **Step 3: Implement the `by_service` branch in memstore**

In `internal/storage/memstore.go`, locate `func (m *memStore) GetCostSummary` (line 947). The overview/costly-traces loop ends at line 972 (`traceCount := len(costlyTraces)`), and the by_model aggregation begins at line 974 (`type modelAgg struct {`).

**Insert this block immediately AFTER line 972** (`traceCount := len(costlyTraces)`) and BEFORE line 974 (`type modelAgg struct {`):

```go
	if q.GroupBy == "service" {
		type svcAgg struct {
			cost, input, cc, cr, output uint64
			traces                       map[[16]byte]struct{}
		}
		sagg := map[string]*svcAgg{}
		var oIn, oCC, oCR, oOut uint64

		for i := range m.spans {
			s := &m.spans[i]
			if _, ok := costlyTraces[s.TraceID]; !ok {
				continue
			}
			if s.TotalTokens == nil {
				continue
			}
			svc := "(unknown)"
			if t, ok := m.traces[s.TraceID]; ok {
				if name := t.ResourceAttrs["service.name"]; name != "" {
					svc = name
				}
			}
			entry := sagg[svc]
			if entry == nil {
				entry = &svcAgg{traces: map[[16]byte]struct{}{}}
				sagg[svc] = entry
			}
			entry.traces[s.TraceID] = struct{}{}
			if s.Cost != nil {
				entry.cost += uint64(math.Round(*s.Cost * 1e6))
			}
			if s.InputTokens != nil {
				entry.input += uint64(*s.InputTokens)
				oIn += uint64(*s.InputTokens)
			}
			if s.CacheCreationTokens != nil {
				entry.cc += uint64(*s.CacheCreationTokens)
				oCC += uint64(*s.CacheCreationTokens)
			}
			if s.CacheReadTokens != nil {
				entry.cr += uint64(*s.CacheReadTokens)
				oCR += uint64(*s.CacheReadTokens)
			}
			if s.OutputTokens != nil {
				entry.output += uint64(*s.OutputTokens)
				oOut += uint64(*s.OutputTokens)
			}
		}

		byService := make([]ServiceCostItem, 0, len(sagg))
		for svc, e := range sagg {
			tc := len(e.traces)
			costF := float64(e.cost) / 1e6
			avg := 0.0
			if tc > 0 {
				avg = math.Round(costF/float64(tc)*1e6) / 1e6
			}
			byService = append(byService, ServiceCostItem{
				Service: svc, Cost: costF,
				Tokens:              e.input + e.cc + e.cr + e.output,
				InputTokens:         e.input, CacheCreationTokens: e.cc,
				CacheReadTokens:     e.cr, OutputTokens: e.output,
				TraceCount:          tc, AvgCost: avg,
			})
		}
		sort.Slice(byService, func(i, j int) bool { return byService[i].Cost > byService[j].Cost })

		avgPerTrace := 0.0
		if traceCount > 0 {
			avgPerTrace = math.Round(totalCost/float64(traceCount)*1e6) / 1e6
		}

		return &CostSummaryResult{
			Period:   "",
			Currency: currency,
			Overview: CostOverview{
				TotalCost:                totalCost,
				TotalInputTokens:         oIn,
				TotalCacheCreationTokens: oCC,
				TotalCacheReadTokens:     oCR,
				TotalOutputTokens:        oOut,
				TotalTokens:              oIn + oCC + oCR + oOut,
				AvgCostPerTrace:          avgPerTrace,
				TraceCount:               traceCount,
			},
			GroupBy:   "service",
			ByService: byService,
		}, nil
	}

```

- [ ] **Step 4: Echo `GroupBy: "model"` on the existing by_model return**

In `internal/storage/memstore.go`, the existing by_model return (around line 1044-1058) is:

```go
	return &CostSummaryResult{
		Period:   "",
		Currency: currency,
		Overview: CostOverview{
			TotalCost:                totalCost,
			TotalInputTokens:         oIn,
			TotalCacheCreationTokens: oCC,
			TotalCacheReadTokens:     oCR,
			TotalOutputTokens:        oOut,
			TotalTokens:              oIn + oCC + oCR + oOut,
			AvgCostPerTrace:          avgPerTrace,
			TraceCount:               traceCount,
		},
		ByModel: byModel,
	}, nil
```

Add `GroupBy: "model",` to it:

```go
	return &CostSummaryResult{
		Period:   "",
		Currency: currency,
		Overview: CostOverview{
			TotalCost:                totalCost,
			TotalInputTokens:         oIn,
			TotalCacheCreationTokens: oCC,
			TotalCacheReadTokens:     oCR,
			TotalOutputTokens:        oOut,
			TotalTokens:              oIn + oCC + oCR + oOut,
			AvgCostPerTrace:          avgPerTrace,
			TraceCount:               traceCount,
		},
		GroupBy: "model",
		ByModel: byModel,
	}, nil
```

- [ ] **Step 5: Run the new test to verify it passes**

Run: `go test -tags nosqlite ./internal/storage/ -run TestMemstoreGetCostSummaryByService -v`
Expected: PASS.

- [ ] **Step 6: Run the existing memstore by_model test to verify no regression**

Run: `go test -tags nosqlite ./internal/storage/ -run TestMemstoreGetCostSummaryReconciles -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/storage/memstore.go internal/storage/memstore_cost_test.go
git commit -m "feat(storage): memstore by_service cost breakdown via group_by"
```

---

## Task 4: Handler `group_by` parse/validate/echo (TDD)

**Files:**
- Modify: `internal/api/trace_handler_test.go:18-31` (mock struct) and `:102-104` (mock method)
- Test: `internal/api/cost_handler_test.go` (append)
- Modify: `internal/api/cost_handler.go:29-64` (the `summary` func)

- [ ] **Step 1: Capture the passed `CostQuery` in the mock**

In `internal/api/trace_handler_test.go`, add a field to `handlerMockStore` (after the `costSummaryErr` line, within the struct literal at lines 18-31):

```go
	lastCostQuery storage.CostQuery
```

So the struct gains one line:

```go
type handlerMockStore struct {
	traces            *storage.TraceListResult
	detail            *storage.TraceDetail
	services          []string
	listErr           error
	detailErr         error
	costSummary       *storage.CostSummaryResult
	costSummaryErr    error
	lastCostQuery     storage.CostQuery
	llmConfigs        []storage.LLMConfig
	llmConfigsErr     error
	diagnosisResult   *storage.DiagnosisResult
	diagnosisResultErr error
	logCounts         map[string]int
}
```

- [ ] **Step 2: Record the query in the mock `GetCostSummary`**

In `internal/api/trace_handler_test.go`, replace the method (lines 102-104):

```go
func (m *handlerMockStore) GetCostSummary(ctx context.Context, q storage.CostQuery) (*storage.CostSummaryResult, error) {
	return m.costSummary, m.costSummaryErr
}
```

with:

```go
func (m *handlerMockStore) GetCostSummary(ctx context.Context, q storage.CostQuery) (*storage.CostSummaryResult, error) {
	m.lastCostQuery = q
	return m.costSummary, m.costSummaryErr
}
```

- [ ] **Step 3: Write the failing handler tests**

Append to `internal/api/cost_handler_test.go`:

```go
func TestCostSummaryGroupByService(t *testing.T) {
	store := &handlerMockStore{
		costSummary: &storage.CostSummaryResult{
			Currency: "USD",
			Overview: storage.CostOverview{TotalCost: 3.0, TraceCount: 2},
			ByService: []storage.ServiceCostItem{
				{Service: "api", Cost: 2.0, TraceCount: 1, AvgCost: 2.0},
				{Service: "web", Cost: 1.0, TraceCount: 1, AvgCost: 1.0},
			},
		},
	}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary?group_by=service", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result storage.CostSummaryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.GroupBy != "service" {
		t.Errorf("response group_by = %s, want service", result.GroupBy)
	}
	if store.lastCostQuery.GroupBy != "service" {
		t.Errorf("store received group_by = %q, want service", store.lastCostQuery.GroupBy)
	}
	if len(result.ByService) != 2 {
		t.Errorf("by_service rows = %d, want 2", len(result.ByService))
	}
}

func TestCostSummaryGroupByDefaultIsModel(t *testing.T) {
	store := &handlerMockStore{
		costSummary: &storage.CostSummaryResult{
			ByModel: []storage.ModelCostItem{{Model: "m", Cost: 1, TraceCount: 1, AvgCost: 1}},
		},
	}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result storage.CostSummaryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.GroupBy != "model" {
		t.Errorf("default response group_by = %s, want model", result.GroupBy)
	}
	if store.lastCostQuery.GroupBy != "model" {
		t.Errorf("store received default group_by = %q, want model", store.lastCostQuery.GroupBy)
	}
}

func TestCostSummaryInvalidGroupBy(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary?group_by=bogus", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid group_by, got %d", rec.Code)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestCostSummaryGroupBy|TestCostSummaryInvalidGroupBy' -v`
Expected: FAIL — `response group_by = , want service` (handler doesn't parse/echo `group_by`), and `TestCostSummaryInvalidGroupBy` expected 400 but got 200.

- [ ] **Step 5: Implement `group_by` parsing in the handler**

In `internal/api/cost_handler.go`, the `summary` func currently parses `period` then builds the time window (lines 30-50). After the period `switch` block (after line 50, before `result, err := h.store.GetCostSummary`), insert the `group_by` parse/validate:

```go
	groupBy := q.Get("group_by")
	if groupBy == "" {
		groupBy = "model"
	}
	if groupBy != "model" && groupBy != "service" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group_by, use: model or service"})
		return
	}
```

Then change the `GetCostSummary` call (around line 52-55) to pass `GroupBy`:

```go
	result, err := h.store.GetCostSummary(r.Context(), storage.CostQuery{
		StartTimeMS: startMS,
		EndTimeMS:   endMS,
		GroupBy:     groupBy,
	})
```

Then after `result.Period = period` (line 62), add the echo:

```go
	result.GroupBy = groupBy
```

- [ ] **Step 6: Run the new tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestCostSummaryGroupBy|TestCostSummaryInvalidGroupBy' -v`
Expected: PASS (all three).

- [ ] **Step 7: Run all cost handler tests to verify no regression**

Run: `go test ./internal/api/ -run TestCostSummary -v`
Expected: PASS (all existing + new).

- [ ] **Step 8: Commit**

```bash
git add internal/api/cost_handler.go internal/api/cost_handler_test.go internal/api/trace_handler_test.go
git commit -m "feat(api): group_by query param on /cost-summary"
```

---

## Task 5: Frontend — types, toggle, shared table, i18n

**Files:**
- Modify: `web/src/api/client.ts:334-355`
- Modify: `web/src/i18n/locales/en.ts:151-171` (the `costDashboard` block)
- Modify: `web/src/i18n/locales/zh.ts:151-171` (the `costDashboard` block)
- Modify: `web/src/views/CostDashboard.vue` (template, script, style)

- [ ] **Step 1: Add `ServiceCost` and extend `CostSummary` in the client**

In `web/src/api/client.ts`, insert the `ServiceCost` interface immediately after the `ModelCost` interface (after line 344):

```ts
export interface ServiceCost {
  service: string
  cost: number
  tokens: number
  input_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  output_tokens: number
  trace_count: number
  avg_cost: number
}
```

Then replace the `CostSummary` interface and `getCostSummary` function (lines 346-355) with:

```ts
export interface CostSummary {
  period: string
  currency: string
  overview: CostOverview
  group_by: string
  by_model?: ModelCost[]
  by_service?: ServiceCost[]
}

export async function getCostSummary(period: string, groupBy: 'model' | 'service' = 'model'): Promise<CostSummary> {
  return get<CostSummary>(`${BASE_URL}/cost-summary?period=${period}&group_by=${groupBy}`)
}
```

- [ ] **Step 2: Add i18n keys (English)**

In `web/src/i18n/locales/en.ts`, within the `costDashboard` block, make two edits.

Replace:
```ts
    costByModel: 'Cost by Model',
    model: 'Model',
```
with:
```ts
    costByModel: 'Cost by Model',
    costByService: 'Cost by Service',
    byModel: 'By Model',
    byService: 'By Service',
    model: 'Model',
    service: 'Service',
```

- [ ] **Step 3: Add i18n keys (Chinese)**

In `web/src/i18n/locales/zh.ts`, within the `costDashboard` block, make the matching edit.

Replace:
```ts
    costByModel: '模型成本分布',
    model: '模型',
```
with:
```ts
    costByModel: '模型成本分布',
    costByService: '按服务成本',
    byModel: '按模型',
    byService: '按服务',
    model: '模型',
    service: '服务',
```

- [ ] **Step 4: Add `computed` to the Vue import**

In `web/src/views/CostDashboard.vue`, the script import (line 76) is:

```ts
import { ref, onMounted } from 'vue'
```

Change to:

```ts
import { ref, computed, onMounted } from 'vue'
```

- [ ] **Step 5: Add `groupBy` state + computeds + `setGroupBy`**

In `web/src/views/CostDashboard.vue`, after the `activePeriod` ref (line 89) and before the `summary` ref (line 90), insert:

```ts
const groupBy = ref<'model' | 'service'>('model')

function setGroupBy(dim: 'model' | 'service') {
  if (groupBy.value === dim) return
  groupBy.value = dim
  fetchData()
}
```

- [ ] **Step 6: Extend the default `summary` ref with `group_by` and `by_service`**

In `web/src/views/CostDashboard.vue`, the `summary` ref default (lines 90-104) currently has `period`, `currency`, `overview`, `by_model`. Add `group_by: 'model'` and `by_service: []`:

```ts
const summary = ref<CostSummary>({
  period: 'today',
  currency: 'USD',
  group_by: 'model',
  overview: {
    total_cost: 0,
    total_tokens: 0,
    total_input_tokens: 0,
    total_cache_creation_tokens: 0,
    total_cache_read_tokens: 0,
    total_output_tokens: 0,
    avg_cost_per_trace: 0,
    trace_count: 0,
  },
  by_model: [],
  by_service: [],
})
```

- [ ] **Step 7: Pass `groupBy` to `getCostSummary` in `fetchData`**

In `web/src/views/CostDashboard.vue`, the `fetchData` function (lines 109-120) calls `getCostSummary(activePeriod.value)`. Change that line to:

```ts
    const result = await getCostSummary(activePeriod.value, groupBy.value)
```

- [ ] **Step 8: Add the `breakdownRows` / `breakdownTitle` / `breakdownDimensionLabel` computeds**

In `web/src/views/CostDashboard.vue`, insert after the `summary` ref (or anywhere in the `<script setup>` after `summary` is declared, e.g., after `noPricing`):

```ts
const breakdownRows = computed(() => {
  const normalize = (r: { name: string; cost: number; tokens: number; cache_read_tokens: number; cache_creation_tokens: number; trace_count: number; avg_cost: number }) => r
  if (summary.value.group_by === 'service') {
    return (summary.value.by_service ?? []).map(s => normalize({
      name: s.service, cost: s.cost, tokens: s.tokens,
      cache_read_tokens: s.cache_read_tokens, cache_creation_tokens: s.cache_creation_tokens,
      trace_count: s.trace_count, avg_cost: s.avg_cost,
    }))
  }
  return (summary.value.by_model ?? []).map(m => normalize({
    name: m.model, cost: m.cost, tokens: m.tokens,
    cache_read_tokens: m.cache_read_tokens, cache_creation_tokens: m.cache_creation_tokens,
    trace_count: m.trace_count, avg_cost: m.avg_cost,
  }))
})

const breakdownTitle = computed(() =>
  summary.value.group_by === 'service' ? t('costDashboard.costByService') : t('costDashboard.costByModel')
)

const breakdownDimensionLabel = computed(() =>
  summary.value.group_by === 'service' ? t('costDashboard.service') : t('costDashboard.model')
)
```

- [ ] **Step 9: Replace the table section in the template**

In `web/src/views/CostDashboard.vue`, replace the entire `<!-- Cost by model table -->` block (lines 46-70):

```html
      <!-- Cost by model table -->
      <h3>{{ t('costDashboard.costByModel') }}</h3>
      <table v-if="summary.by_model.length > 0" class="cost-table">
        <thead>
          <tr>
            <th>{{ t('costDashboard.model') }}</th>
            <th>{{ t('costDashboard.cost') }}</th>
            <th>{{ t('costDashboard.tokens') }}</th>
            <th>{{ t('costDashboard.cache') }}</th>
            <th>{{ t('costDashboard.traces') }}</th>
            <th>{{ t('costDashboard.avgCost') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in summary.by_model" :key="m.model">
            <td>{{ m.model }}</td>
            <td>{{ formatCost(m.cost, summary.currency) }}</td>
            <td>{{ formatNumber(m.tokens) }}</td>
            <td>{{ formatNumber(m.cache_read_tokens + m.cache_creation_tokens) }}</td>
            <td>{{ m.trace_count }}</td>
            <td>{{ formatCost(m.avg_cost, summary.currency) }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">{{ t('costDashboard.noData') }}</div>
```

with:

```html
      <!-- Cost breakdown table (by model / by service) -->
      <h3>{{ breakdownTitle }}</h3>
      <div class="breakdown-toggle">
        <button
          :class="['btn', 'btn-preset', { active: groupBy === 'model' }]"
          @click="setGroupBy('model')"
        >{{ t('costDashboard.byModel') }}</button>
        <button
          :class="['btn', 'btn-preset', { active: groupBy === 'service' }]"
          @click="setGroupBy('service')"
        >{{ t('costDashboard.byService') }}</button>
      </div>
      <table v-if="breakdownRows.length > 0" class="cost-table">
        <thead>
          <tr>
            <th>{{ breakdownDimensionLabel }}</th>
            <th>{{ t('costDashboard.cost') }}</th>
            <th>{{ t('costDashboard.tokens') }}</th>
            <th>{{ t('costDashboard.cache') }}</th>
            <th>{{ t('costDashboard.traces') }}</th>
            <th>{{ t('costDashboard.avgCost') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in breakdownRows" :key="r.name">
            <td>{{ r.name }}</td>
            <td>{{ formatCost(r.cost, summary.currency) }}</td>
            <td>{{ formatNumber(r.tokens) }}</td>
            <td>{{ formatNumber(r.cache_read_tokens + r.cache_creation_tokens) }}</td>
            <td>{{ r.trace_count }}</td>
            <td>{{ formatCost(r.avg_cost, summary.currency) }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">{{ t('costDashboard.noData') }}</div>
```

- [ ] **Step 10: Add the `.breakdown-toggle` style**

In `web/src/views/CostDashboard.vue` `<style scoped>`, add (e.g., right after the `.period-bar` rule):

```css
.breakdown-toggle {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
```

- [ ] **Step 11: Type-check the frontend**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no output, exit 0 (no type errors).

- [ ] **Step 12: Manual verification in the browser**

Start the dev stack (in two shells):
- `go run -tags dev ./cmd/labubu serve` (backend on :8080 + OTLP receiver)
- `cd web && npm run dev` (Vite on :3001)

Open `http://localhost:3001/cost`. Verify:
- Default view shows "Cost by Model" heading + active By Model button + the model table.
- Click **By Service**: heading changes to "Cost by Service", the first column becomes "Service", rows group by `service.name` (traces with no service fall under `(unknown)`).
- Click **By Model**: returns to the model view.
- Toggle the period bar (Today / 7d / 30d): the current breakdown dimension is preserved (re-fetched with the same `groupBy`).

- [ ] **Step 13: Commit**

```bash
git add web/src/api/client.ts web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts web/src/views/CostDashboard.vue
git commit -m "feat(web): by-model/by-service cost breakdown toggle"
```

---

## Task 6: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run all Go storage tests (sqlite path, default tags)**

Run: `go test ./internal/storage/ -v`
Expected: PASS, including `TestGetCostSummaryReconcilesAndBreaksOutCache` and `TestGetCostSummaryByService`.

- [ ] **Step 2: Run all Go storage tests (memstore path, nosqlite tag)**

Run: `go test -tags nosqlite ./internal/storage/ -v`
Expected: PASS, including `TestMemstoreGetCostSummaryReconciles` and `TestMemstoreGetCostSummaryByService`.

- [ ] **Step 3: Run all API handler tests**

Run: `go test ./internal/api/ -v`
Expected: PASS, including the three new `group_by` tests.

- [ ] **Step 4: TypeScript type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no output, exit 0.

- [ ] **Step 5: Build check (no CGO)**

Run: `go build -tags dev ./cmd/labubu`
Expected: no output, exit 0.

- [ ] **Step 6: If all green, the feature is complete. No extra commit unless Task 5 left the working tree dirty.**

---

## Self-Review (completed by plan author)

**1. Spec coverage:**
- `group_by` query param + echo → Task 4. ✓
- `ServiceCostItem` type + `CostSummaryResult.ByService/GroupBy` + `CostQuery.GroupBy` → Task 1. ✓
- SQLite `by_service` SQL (spans↔traces join) → Task 2. ✓
- memstore `by_service` aggregation → Task 3. ✓
- Handler default `model` + validation + 400 → Task 4. ✓
- Frontend segmented toggle + dynamic heading + shared table → Task 5. ✓
- i18n keys (`byModel`/`byService`/`costByService`/`service`) → Task 5. ✓
- Tests: sqlite by_service, memstore by_service, handler group_by → Tasks 2/3/4. ✓
- chDB out of scope — correctly not touched. ✓

**2. Placeholder scan:** No TBD/TODO/"add validation"/"similar to Task N" — every code step contains full code. ✓

**3. Type consistency:**
- `ServiceCostItem` field names (`Service`, `Cost`, `Tokens`, `InputTokens`, `CacheCreationTokens`, `CacheReadTokens`, `OutputTokens`, `TraceCount`, `AvgCost`) are consistent across storage.go (Task 1), sqlite scan (Task 2), memstore struct literal (Task 3), handler test fixtures (Task 4), and client `ServiceCost` (Task 5, snake_case JSON). ✓
- `CostQuery.GroupBy` used consistently in handler (Task 4), sqlite branch (Task 2), memstore branch (Task 3). ✓
- `CostSummaryResult.GroupBy` set by both store impls and echoed by handler; read by frontend `summary.group_by`. ✓
- Frontend `breakdownRows`/`breakdownTitle`/`breakdownDimensionLabel` referenced in template match the computeds in script. ✓
- `getCostSummary(period, groupBy)` signature matches the call in `fetchData` (`getCostSummary(activePeriod.value, groupBy.value)`). ✓
