# Accurate Token & Cost Aggregation with Cache Breakdown — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cost-page token totals accurate (`total = input + cache_creation + cache_read + output` by construction), fix the `by_model` fan-out bug, add a cache-token breakdown to the cost page, and make `traces.total_tokens` reliable.

**Architecture:** Spans table becomes the single source of truth for all token aggregation. A new `storage.DeriveTokenBuckets` helper centralizes token-bucket derivation (removes the self-reported `total_tokens` override). A new span-level `cost` column lets `by_model` aggregate cost and tokens from the same spans query with no fan-out join. `UpdateTraceCost` writes per-span cost and recomputes `traces.total_tokens`.

**Tech Stack:** Go 1.19 (sqlite store, default build), Vue 3 + TypeScript, vue-i18n. Build tags: default `!local_engine && !nosqlite` = sqlite (what the user runs); `nosqlite` = memstore (fast test path). chDB (`cgo && local_engine`) is out of scope per spec.

**Spec:** `docs/superpowers/specs/2026-06-25-accurate-token-cost-aggregation-design.md`

**Windows test note:** This repo's `make` targets don't run in Git Bash. Use `go test` directly. WDAC may intermittently block `go test` temp binaries — if a test run fails with a create-process/exec error, use `go test -c -o /tmp/st.test ./internal/storage && /tmp/st.test` (compile-then-run) as the workaround.

---

## File Structure

| File | Responsibility | Change |
|------|----------------|--------|
| `internal/storage/tokens.go` (new) | `DeriveTokenBuckets` + `readUint32` helper | create |
| `internal/storage/tokens_test.go` (new) | tests for the helper | create |
| `internal/storage/storage.go` | `Span`, `CostOverview`, `ModelCostItem` types | add fields |
| `internal/storage/sqlite_schema.sql` | sqlite spans table DDL | add `cost`/`cost_currency` cols |
| `internal/storage/sqlite_store.go` | sqlite Store impl | migration, InsertSpans span cost, UpdateTraceCost span cost + total recompute, merge accumulate, GetCostSummary rewrite |
| `internal/storage/sqlite_cost_test.go` (new) | GetCostSummary + UpdateTraceCost tests | create |
| `internal/storage/chdb_query.go` | shared `aggregateTraces` | (no change — already correct per-batch) |
| `internal/storage/memstore.go` | memstore Store impl | span cost field use, GetCostSummary rewrite |
| `internal/receiver/otlp.go` | `translateSpan` | use `DeriveTokenBuckets`, remove override |
| `internal/receiver/otlp_test.go` | receiver tests | update override expectation |
| `web/src/api/client.ts` | `CostOverview`/`ModelCost` types | add cache fields |
| `web/src/views/CostDashboard.vue` | cost page UI | show cache breakdown |
| `web/src/i18n/locales/en.ts`, `zh.ts` | i18n strings | add cache keys |

---

### Task 1: `storage.DeriveTokenBuckets` helper

**Files:**
- Create: `internal/storage/tokens.go`
- Test: `internal/storage/tokens_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/storage/tokens_test.go` (no build tag — runs in every build):

```go
package storage

import "testing"

func TestDeriveTokenBuckets(t *testing.T) {
	u := func(v uint32) *uint32 { return &v }
	tests := []struct {
		name   string
		attrs  map[string]string
		in, out, cc, cr, tot *uint32
	}{
		{
			name:  "claude-code style: input non-cached, cache separate",
			attrs: map[string]string{
				"gen_ai.usage.input_tokens":              "2",
				"gen_ai.usage.output_tokens":             "100",
				"gen_ai.usage.cache_creation_input_tokens": "189194",
				"gen_ai.usage.cache_read_input_tokens":     "5000",
			},
			in: u(2), out: u(100), cc: u(189194), cr: u(5000), tot: u(194296),
		},
		{
			name:  "jiuwenclaw style: no cache, fallback keys",
			attrs: map[string]string{"input_tokens": "13011", "output_tokens": "53"},
			in: u(13011), out: u(53), cc: nil, cr: nil, tot: u(13064),
		},
		{
			name:  "self-reported total_tokens is IGNORED",
			attrs: map[string]string{
				"gen_ai.usage.input_tokens":  "100",
				"gen_ai.usage.output_tokens": "50",
				"gen_ai.usage.total_tokens":  "999",
			},
			in: u(100), out: u(50), cc: nil, cr: nil, tot: u(150),
		},
		{
			name:  "no token keys -> all nil",
			attrs: map[string]string{"other": "x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, out, cc, cr, tot := DeriveTokenBuckets(tt.attrs)
			if !u32eq(in, tt.in) { t.Errorf("input: got %v want %v", in, tt.in) }
			if !u32eq(out, tt.out) { t.Errorf("output: got %v want %v", out, tt.out) }
			if !u32eq(cc, tt.cc) { t.Errorf("cacheCreation: got %v want %v", cc, tt.cc) }
			if !u32eq(cr, tt.cr) { t.Errorf("cacheRead: got %v want %v", cr, tt.cr) }
			if !u32eq(tot, tt.tot) { t.Errorf("total: got %v want %v", tot, tt.tot) }
		})
	}
}

func u32eq(a, b *uint32) bool {
	if a == nil && b == nil { return true }
	if a == nil || b == nil { return false }
	return *a == *b
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestDeriveTokenBuckets -v`
Expected: FAIL — `DeriveTokenBuckets undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/storage/tokens.go` (no build tag):

```go
package storage

import "strconv"

// DeriveTokenBuckets extracts disjoint token buckets from an attribute map.
// total = input + output + cacheCreation + cacheRead (always; any self-reported
// gen_ai.usage.total_tokens is IGNORED). Assumes input_tokens is non-cached
// (cache reported in separate buckets). If a future agent reports input that
// already includes cache (OpenAI-style prompt_tokens), add a rule HERE to derive
// non-cached input before summing — this function is the single extension point.
func DeriveTokenBuckets(attrs map[string]string) (input, output, cacheCreation, cacheRead, total *uint32) {
	input = readUint32(attrs,
		"gen_ai.usage.input_tokens", "input_tokens", "llm.usage.input_tokens")
	output = readUint32(attrs,
		"gen_ai.usage.output_tokens", "output_tokens", "llm.usage.output_tokens")
	cacheCreation = readUint32(attrs,
		"gen_ai.usage.cache_creation_input_tokens", "cache_creation_tokens", "cache_creation_input_tokens")
	cacheRead = readUint32(attrs,
		"gen_ai.usage.cache_read_input_tokens", "cache_read_tokens", "cache_read_input_tokens")

	if input == nil && output == nil && cacheCreation == nil && cacheRead == nil {
		return
	}
	var sum uint32
	for _, p := range []*uint32{input, output, cacheCreation, cacheRead} {
		if p != nil {
			sum += *p
		}
	}
	total = &sum
	return
}

// readUint32 returns the first present, parseable attribute as *uint32.
func readUint32(attrs map[string]string, keys ...string) *uint32 {
	for _, k := range keys {
		if v, ok := attrs[k]; ok {
			if n, err := strconv.ParseUint(v, 10, 32); err == nil {
				nv := uint32(n)
				return &nv
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -run TestDeriveTokenBuckets -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/tokens.go internal/storage/tokens_test.go
git commit -m "feat(storage): add DeriveTokenBuckets helper for disjoint token buckets"
```

---

### Task 2: Receiver uses `DeriveTokenBuckets`, removes override

**Files:**
- Modify: `internal/receiver/otlp.go:424-471`
- Modify: `internal/receiver/otlp_test.go:284-416`

- [ ] **Step 1: Write the failing test (override ignored)**

In `internal/receiver/otlp_test.go`, append a new test:

```go
// TestTranslateSpanIgnoresSelfReportedTotal verifies that a self-reported
// gen_ai.usage.total_tokens is IGNORED — total is always the bucket sum.
func TestTranslateSpanIgnoresSelfReportedTotal(t *testing.T) {
	span := &tracepb.Span{
		TraceId: make([]byte, 16),
		SpanId:  make([]byte, 8),
		Name:    "claude_code.llm_request",
		Attributes: []*commonpb.KeyValue{
			intKV("input_tokens", 100),
			intKV("output_tokens", 50),
			intKV("total_tokens", 999),
		},
	}
	got := translateSpan(span)
	// total = 100 + 50 = 150 (the 999 must be ignored)
	if !uint32PtrEqual(got.TotalTokens, uint32Ptr(150)) {
		t.Errorf("TotalTokens: got %v, want 150 (self-reported 999 ignored)", got.TotalTokens)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/receiver/ -run TestTranslateSpanIgnoresSelfReportedTotal -v`
Expected: FAIL — `got 999, want 150` (current override at otlp.go:443-445 uses the self-reported value).

- [ ] **Step 3: Replace token derivation in `translateSpan`**

In `internal/receiver/otlp.go`, replace lines 424-447 (the `inputTokens`/`outputTokens`/`cacheCreationTokens`/`cacheReadTokens`/`totalTokens` block) with:

```go
	inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens, totalTokens :=
		storage.DeriveTokenBuckets(attrs)
```

Leave the rest of `translateSpan` (line 448 onward: `genAIModel := getStringAttrFromMap(...)`, the `return storage.Span{...}`) unchanged. The `Span` fields `InputTokens`/`OutputTokens`/`TotalTokens`/`CacheCreationTokens`/`CacheReadTokens` already reference these variables.

- [ ] **Step 4: Update the stale inline-replication test**

In `internal/receiver/otlp_test.go`, replace the body of `TestTokenExtractionAfterNormalize` (lines 284-360) so it calls the shared helper instead of replicating the (now-removed) override logic:

```go
func TestTokenExtractionAfterNormalize(t *testing.T) {
	tests := []struct {
		name         string
		input        map[string]string
		expectInput  *uint32
		expectOutput *uint32
		expectTotal  *uint32
	}{
		{
			name:         "Claude Code input_tokens → typed column",
			input:        map[string]string{"input_tokens": "100", "output_tokens": "50"},
			expectInput:  uint32Ptr(100),
			expectOutput: uint32Ptr(50),
			expectTotal:  uint32Ptr(150),
		},
		{
			name:         "OpenInference llm keys → typed column",
			input:        map[string]string{"llm.usage.input_tokens": "200", "llm.usage.output_tokens": "100"},
			expectInput:  uint32Ptr(200),
			expectOutput: uint32Ptr(100),
			expectTotal:  uint32Ptr(300),
		},
		{
			name:         "standard keys already present",
			input:        map[string]string{"gen_ai.usage.input_tokens": "300"},
			expectInput:  uint32Ptr(300),
			expectOutput: nil,
			expectTotal:  uint32Ptr(300),
		},
		{
			name:         "no token keys → nil",
			input:        map[string]string{"other_attr": "value"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := make(map[string]string, len(tt.input))
			for k, v := range tt.input {
				attrs[k] = v
			}
			normalizeAttributes(attrs)
			in, out, _, _, tot := storage.DeriveTokenBuckets(attrs)
			if !uint32PtrEqual(in, tt.expectInput) {
				t.Errorf("inputTokens: got %v, want %v", in, tt.expectInput)
			}
			if !uint32PtrEqual(out, tt.expectOutput) {
				t.Errorf("outputTokens: got %v, want %v", out, tt.expectOutput)
			}
			if !uint32PtrEqual(tot, tt.expectTotal) {
				t.Errorf("totalTokens: got %v, want %v", tot, tt.expectTotal)
			}
		})
	}
}
```

Add `"github.com/labubu/labubu/internal/storage"` to the test file's imports if missing.

- [ ] **Step 5: Run receiver tests**

Run: `go test ./internal/receiver/ -v`
Expected: PASS — including `TestTranslateSpanCacheTokens` (total still 194296), `TestTranslateSpanIgnoresSelfReportedTotal`, and the updated `TestTokenExtractionAfterNormalize`.

- [ ] **Step 6: Commit**

```bash
git add internal/receiver/otlp.go internal/receiver/otlp_test.go
git commit -m "feat(receiver): derive tokens via storage.DeriveTokenBuckets, ignore self-reported total"
```

---

### Task 3: Add `Cost`/`CostCurrency` to `Span`, cache fields to cost result types

**Files:**
- Modify: `internal/storage/storage.go:14-35` (Span struct), `:94-113` (CostOverview, ModelCostItem)

- [ ] **Step 1: Add fields to `Span`**

In `internal/storage/storage.go`, in the `Span` struct, add after `GenAIRequestModel` (line 33):

```go
	Cost         *float64 // per-span cost (differential cache rates applied)
	CostCurrency string   // currency of Cost, empty if no cost
```

- [ ] **Step 2: Add cache fields to `CostOverview`**

Replace the `CostOverview` struct (lines 95-102) with:

```go
// CostOverview holds aggregated cost totals.
type CostOverview struct {
	TotalCost               float64 `json:"total_cost"`
	TotalTokens             uint64  `json:"total_tokens"`
	TotalInputTokens        uint64  `json:"total_input_tokens"`
	TotalCacheCreationTokens uint64 `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens    uint64  `json:"total_cache_read_tokens"`
	TotalOutputTokens       uint64  `json:"total_output_tokens"`
	AvgCostPerTrace         float64 `json:"avg_cost_per_trace"`
	TraceCount              int     `json:"trace_count"`
}
```

- [ ] **Step 3: Add cache fields to `ModelCostItem`**

Replace the `ModelCostItem` struct (lines 105-113) with:

```go
// ModelCostItem holds cost aggregation for a single model.
type ModelCostItem struct {
	Model             string  `json:"model"`
	Cost              float64 `json:"cost"`
	Tokens            uint64  `json:"tokens"`
	InputTokens       uint64  `json:"input_tokens"`
	CacheCreationTokens uint64 `json:"cache_creation_tokens"`
	CacheReadTokens   uint64  `json:"cache_read_tokens"`
	OutputTokens      uint64  `json:"output_tokens"`
	TraceCount        int     `json:"trace_count"`
	AvgCost           float64 `json:"avg_cost"`
}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/...`
Expected: compiles. (Existing code assigning `ModelCostItem{Model:..., Cost:..., Tokens:..., InputTokens:..., OutputTokens:..., TraceCount:..., AvgCost:...}` still works — new fields default to zero. `CostOverview` literal in memstore still works.)

- [ ] **Step 5: Commit**

```bash
git add internal/storage/storage.go
git commit -m "feat(storage): add span cost fields and cache token breakdown to cost result types"
```

---

### Task 4: sqlite schema + migration for span-level cost

**Files:**
- Modify: `internal/storage/sqlite_schema.sql:32-58` (spans table)
- Modify: `internal/storage/sqlite_store.go:61-77` (migration + backfill call)

- [ ] **Step 1: Add columns to spans DDL**

In `internal/storage/sqlite_schema.sql`, in the `CREATE TABLE IF NOT EXISTS spans` block, add `cost` and `cost_currency` columns next to `total_tokens`/`cache_read_tokens`. The spans column list becomes (showing the relevant tail):

```sql
    input_tokens       INTEGER,
    output_tokens      INTEGER,
    total_tokens       INTEGER,
    cache_creation_tokens INTEGER,
    cache_read_tokens  INTEGER,
    gen_ai_request_model TEXT,
    cost               REAL,
    cost_currency      TEXT NOT NULL DEFAULT '',
```

(Place `cost`/`cost_currency` after `gen_ai_request_model` to match the `INSERT` column order updated in Task 5.)

- [ ] **Step 2: Add migration ALTERs**

In `internal/storage/sqlite_store.go`, after line 64 (`db.Exec("ALTER TABLE spans ADD COLUMN cache_read_tokens INTEGER")`), add:

```go
	// Migrate: add per-span cost columns for accurate by-model cost aggregation.
	db.Exec(`ALTER TABLE spans ADD COLUMN cost REAL`)
	db.Exec(`ALTER TABLE spans ADD COLUMN cost_currency TEXT NOT NULL DEFAULT ''`)
```

- [ ] **Step 3: Add a `backfillSpanCost` startup migration**

Add a migration that, for traces whose spans still have `cost IS NULL`, calls `UpdateTraceCost` (Task 6 makes it write per-span cost). Idempotent — only touches traces with a NULL-cost span. In `internal/storage/sqlite_store.go`, add near `backfillCacheTokens`:

```go
// backfillSpanCost populates spans.cost/cost_currency for pre-existing
// spans (added alongside the span-level cost column). Idempotent: only
// touches traces that still have a span with NULL cost.
func (s *sqliteStore) backfillSpanCost(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT trace_id_hex FROM spans WHERE cost IS NULL`)
	if err != nil {
		return
	}
	var traceIDs []string
	for rows.Next() {
		var hexStr string
		if rows.Scan(&hexStr) == nil {
			traceIDs = append(traceIDs, hexStr)
		}
	}
	rows.Close()
	for _, hexStr := range traceIDs {
		b, err := hex.DecodeString(hexStr)
		if err != nil || len(b) != 16 {
			continue
		}
		var tid [16]byte
		copy(tid[:], b)
		s.UpdateTraceCost(ctx, tid)
	}
}
```

Add `"encoding/hex"` to sqlite_store.go imports if not already present.

Wire it into `NewChDBStore` after the `s.backfillCacheTokens(context.Background())` call (around line 75):

```go
	// Backfill per-span cost for pre-existing spans (idempotent).
	s.backfillSpanCost(context.Background())
```

- [ ] **Step 4: Verify build**

Run: `go build ./internal/storage/`
Expected: compiles. (The migration runs at store construction; its effect is exercised by Task 6 and Task 8 tests, which construct a fresh store via `NewChDBStore` — in a fresh test DB there are no NULL-cost spans, so the migration is a no-op there, and the tests insert spans then call `UpdateTraceCost` directly.)

- [ ] **Step 5: Commit**

```bash
git add internal/storage/sqlite_schema.sql internal/storage/sqlite_store.go
git commit -m "feat(storage): add spans.cost/cost_currency columns + backfill migration"
```

---

### Task 5: sqlite `InsertSpans` writes span cost on insert

**Files:**
- Modify: `internal/storage/sqlite_store.go:258-271` (span INSERT)

- [ ] **Step 1: Extend the span INSERT statement**

In `internal/storage/sqlite_store.go` `InsertSpans`, the INSERT (lines 258-271) currently lists 23 columns. Add `cost, cost_currency` (2 columns → 25 columns, 25 `?` placeholders). Replace the INSERT with:

```go
		_, err := tx.Exec(
			`INSERT OR REPLACE INTO spans (
				trace_id_hex, span_id_hex, parent_span_id_hex, trace_state, name, kind,
				start_time_ms, end_time_ms, duration_ms, attributes,
				dropped_attributes_count, events, dropped_events_count,
				links, dropped_links_count, status_code, status_message,
				input_tokens, output_tokens, total_tokens, cache_creation_tokens, cache_read_tokens, gen_ai_request_model,
				cost, cost_currency
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			TraceIDToHex(sp.TraceID), SpanIDToHex(sp.SpanID), SpanIDToHex(sp.ParentSpanID),
			sp.TraceState, sp.Name, KindToString(sp.Kind),
			sp.StartTimeMS, sp.EndTimeMS, sp.DurationMS, string(attrsJSON),
			0, sp.Events, 0, sp.Links, 0,
			StatusCodeToString(sp.StatusCode), sp.StatusMessage,
			sp.InputTokens, sp.OutputTokens, sp.TotalTokens, sp.CacheCreationTokens, sp.CacheReadTokens, sp.GenAIRequestModel,
			sp.Cost, sp.CostCurrency,
		)
```

New spans arrive without cost (it's computed async by `UpdateTraceCost`, called at line 372-376). So `sp.Cost` is nil at insert — `INSERT OR REPLACE` writes NULL, populated later. This is expected.

- [ ] **Step 2: Verify build**

Run: `go build ./internal/storage/`
Expected: compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/storage/sqlite_store.go
git commit -m "feat(storage): persist span cost/cost_currency on span insert"
```

---

### Task 6: `UpdateTraceCost` writes per-span cost + recomputes `traces.total_tokens`

**Files:**
- Modify: `internal/storage/sqlite_store.go:1081-1195`
- Test: `internal/storage/sqlite_cost_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/storage/sqlite_cost_test.go` (no build tag — sqlite build):

```go
package storage

import (
	"context"
	"testing"
)

// newTestStore creates a sqlite store in a temp dir for the default (non-cgo) build.
func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewChDBStore(dir) // sqlite constructor in non-cgo builds
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpdateTraceCostWritesSpanCostAndTotal(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Price the model so cost is non-zero.
	store.UpsertModelPricing(ctx, ModelPricing{
		ModelName: "glm-5.2", InputPrice: 10.0, OutputPrice: 30.0, Currency: "CNY",
	})

	// A trace with one LLM span: input=100, output=50, cache_read=1000.
	var tid [16]byte
	tid[15] = 1
	var sid [8]byte
	sid[7] = 1
	u := func(v uint32) *uint32 { return &v }
	mdl := "glm-5.2"
	span := Span{
		TraceID: tid, SpanID: sid,
		Name: "gen_ai.chat", Kind: 2,
		StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
		InputTokens: u(100), OutputTokens: u(50), CacheReadTokens: u(1000),
		TotalTokens: u(1150), GenAIRequestModel: &mdl,
	}
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, []Span{span}); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	// InsertSpans fires UpdateTraceCost async; force a sync recompute.
	if err := store.UpdateTraceCost(ctx, tid); err != nil {
		t.Fatalf("UpdateTraceCost: %v", err)
	}

	// traces.total_tokens must equal sum(spans.total_tokens) = 1150.
	tr, err := store.ListTraces(ctx, TraceQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	if len(tr.Traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(tr.Traces))
	}
	if tr.Traces[0].TotalTokens == nil || *tr.Traces[0].TotalTokens != 1150 {
		t.Errorf("traces.total_tokens = %v, want 1150", tr.Traces[0].TotalTokens)
	}
	// Per-span cost write + by_model cost/cache breakdown are verified in
	// TestGetCostSummaryReconcilesAndBreaksOutCache (Task 8), which depends
	// on the GetCostSummary rewrite. This test only asserts the trace-level
	// total_tokens recompute that UpdateTraceCost owns.
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestUpdateTraceCostWritesSpanCostAndTotal -v`
Expected: FAIL — `by_model cost` wrong (fan-out) or `TotalCacheReadTokens` field missing / zero.

- [ ] **Step 3: Rewrite `UpdateTraceCost`**

In `internal/storage/sqlite_store.go`, replace the body of `UpdateTraceCost` (lines 1081-1195). The new version writes per-span `cost`/`cost_currency` AND recomputes `traces.total_tokens`:

```go
func (s *sqliteStore) UpdateTraceCost(ctx context.Context, traceID [16]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	traceIDHex := TraceIDToHex(traceID)

	pricingRows, err := s.db.QueryContext(ctx,
		`SELECT model_name, input_price, output_price, currency FROM model_pricing`)
	if err != nil {
		return fmt.Errorf("query pricing: %w", err)
	}
	pricingMap := make(map[string]ModelPricing)
	for pricingRows.Next() {
		var p ModelPricing
		if err := pricingRows.Scan(&p.ModelName, &p.InputPrice, &p.OutputPrice, &p.Currency); err != nil {
			pricingRows.Close()
			return fmt.Errorf("scan pricing: %w", err)
		}
		pricingMap[p.ModelName] = p
	}
	pricingRows.Close()

	rows, err := s.db.QueryContext(ctx,
		`SELECT span_id_hex, input_tokens, output_tokens, total_tokens,
		        cache_creation_tokens, cache_read_tokens, gen_ai_request_model
		 FROM spans WHERE trace_id_hex = ? AND total_tokens IS NOT NULL`,
		traceIDHex)
	if err != nil {
		return fmt.Errorf("query span tokens: %w", err)
	}

	const cacheCreateRate = 1.25
	const cacheReadRate = 0.1

	var totalCost float64
	var traceTotalTokens uint64
	var currency string
	var unpricedCount int
	type spanCostRow struct {
		spanID            string
		cost              float64
		costCurrency      string
	}
	var spanCosts []spanCostRow

	for rows.Next() {
		var spanID string
		var inputTokens, outputTokens, totalTokens, cacheCreationTokens, cacheReadTokens sql.NullInt32
		var genAIModel sql.NullString
		if err := rows.Scan(&spanID, &inputTokens, &outputTokens, &totalTokens,
			&cacheCreationTokens, &cacheReadTokens, &genAIModel); err != nil {
			rows.Close()
			return fmt.Errorf("scan span token: %w", err)
		}
		// Trace total = sum of all span total_tokens (pricing-independent).
		if totalTokens.Valid {
			traceTotalTokens += uint64(totalTokens.Int32)
		}

		modelName := "(unknown)"
		if genAIModel.Valid {
			modelName = genAIModel.String
		}
		p, ok := pricingMap[modelName]
		if !ok {
			unpricedCount++
			spanCosts = append(spanCosts, spanCostRow{spanID: spanID})
			continue
		}

		inT := uint32(0)
		if inputTokens.Valid {
			inT = uint32(inputTokens.Int32)
		}
		outT := uint32(0)
		if outputTokens.Valid {
			outT = uint32(outputTokens.Int32)
		}
		ccT := uint32(0)
		if cacheCreationTokens.Valid {
			ccT = uint32(cacheCreationTokens.Int32)
		}
		crT := uint32(0)
		if cacheReadTokens.Valid {
			crT = uint32(cacheReadTokens.Int32)
		}

		spanCost := (float64(inT)*p.InputPrice +
			float64(ccT)*p.InputPrice*cacheCreateRate +
			float64(crT)*p.InputPrice*cacheReadRate +
			float64(outT)*p.OutputPrice) / 1_000_000.0
		spanCost = math.Round(spanCost*1e6) / 1e6
		totalCost += spanCost
		if currency == "" {
			currency = p.Currency
		}
		spanCosts = append(spanCosts, spanCostRow{spanID: spanID, cost: spanCost, costCurrency: p.Currency})
	}
	rows.Close()

	// Write per-span cost (including 0 for unpriced, so by_model has no NULLs).
	for _, sc := range spanCosts {
		s.db.ExecContext(ctx,
			`UPDATE spans SET cost = ?, cost_currency = ? WHERE trace_id_hex = ? AND span_id_hex = ?`,
			sc.cost, sc.costCurrency, traceIDHex, sc.spanID)
	}

	if totalCost == 0 && unpricedCount == 0 {
		// Still set trace total_tokens even with no cost.
		if traceTotalTokens > 0 {
			s.db.ExecContext(ctx, `UPDATE traces SET total_tokens = ? WHERE trace_id_hex = ?`,
				traceTotalTokens, traceIDHex)
		}
		return nil
	}

	totalCost = math.Round(totalCost*1e6) / 1e6
	_, err = s.db.ExecContext(ctx,
		`UPDATE traces SET total_tokens = ?, cost = ?, cost_currency = ? WHERE trace_id_hex = ?`,
		traceTotalTokens, totalCost, currency, traceIDHex)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -run TestUpdateTraceCostWritesSpanCostAndTotal -v`
Expected: PASS — `traces.total_tokens == 1150` (the recompute this task adds). Per-span cost write is exercised end-to-end by Task 8's `TestGetCostSummaryReconcilesAndBreaksOutCache`.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/sqlite_store.go internal/storage/sqlite_cost_test.go
git commit -m "feat(storage): UpdateTraceCost writes per-span cost and recomputes trace total_tokens"
```

---

### Task 7: sqlite `InsertSpans` merge accumulates `total_tokens` across batches

**Files:**
- Modify: `internal/storage/sqlite_store.go:328-331`

- [ ] **Step 1: Write the failing test (multi-batch accumulation)**

Append to `internal/storage/sqlite_cost_test.go`:

```go
func TestInsertSpansAccumulatesTraceTotalTokens(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	store.UpsertModelPricing(ctx, ModelPricing{
		ModelName: "glm-5.2", InputPrice: 10.0, OutputPrice: 30.0, Currency: "CNY",
	})

	var tid [16]byte
	tid[15] = 7
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}
	mdl := "glm-5.2"
	u := func(v uint32) *uint32 { return &v }

	// Batch 1: span A with 1000 total tokens.
	sA := Span{TraceID: tid, SpanID: [8]byte{7}, Name: "s", Kind: 2,
		StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
		InputTokens: u(900), OutputTokens: u(100), TotalTokens: u(1000), GenAIRequestModel: &mdl}
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, []Span{sA}); err != nil {
		t.Fatalf("batch1: %v", err)
	}
	// Batch 2: span B with 500 total tokens, same trace.
	sB := Span{TraceID: tid, SpanID: [8]byte{8}, Name: "s", Kind: 2,
		StartTimeMS: 2000, EndTimeMS: 3000, DurationMS: 1000,
		InputTokens: u(450), OutputTokens: u(50), TotalTokens: u(500), GenAIRequestModel: &mdl}
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, []Span{sB}); err != nil {
		t.Fatalf("batch2: %v", err)
	}

	store.UpdateTraceCost(ctx, tid) // sync recompute

	tr, _ := store.ListTraces(ctx, TraceQuery{Page: 1, PageSize: 10})
	if len(tr.Traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(tr.Traces))
	}
	// Both spans counted: 1000 + 500 = 1500 (old bug gave only the last batch = 500).
	if tr.Traces[0].TotalTokens == nil || *tr.Traces[0].TotalTokens != 1500 {
		t.Errorf("traces.total_tokens = %v, want 1500", tr.Traces[0].TotalTokens)
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/storage/ -run TestInsertSpansAccumulatesTraceTotalTokens -v`
Expected: should PASS already after Task 6 (because `UpdateTraceCost` recomputes the full sum). If it fails, the merge is still replacing — continue to Step 3.

- [ ] **Step 3: Make the merge accumulate (defense for traces not yet cost-recomputed)**

In `internal/storage/sqlite_store.go`, replace lines 328-331:

```go
			if trace.TotalTokens == nil && existingTotalTokens.Valid {
				v := uint32(existingTotalTokens.Int32)
				trace.TotalTokens = &v
			}
```

with:

```go
			// Accumulate across batches: a trace often arrives in multiple
			// OTLP batches; aggregateTraces only sums the current batch's
			// spans, so add the previously-stored total. (Re-sending the
			// same span_id would double-count — accepted rare edge case.)
			if existingTotalTokens.Valid {
				ex := uint32(existingTotalTokens.Int32)
				if trace.TotalTokens == nil {
					trace.TotalTokens = &ex
				} else {
					sum := ex + *trace.TotalTokens
					trace.TotalTokens = &sum
				}
			}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -run TestInsertSpansAccumulatesTraceTotalTokens -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/sqlite_store.go internal/storage/sqlite_cost_test.go
git commit -m "fix(storage): accumulate trace total_tokens across insert batches"
```

---

### Task 8: Rewrite sqlite `GetCostSummary` (overview + by_model from spans)

**Files:**
- Modify: `internal/storage/sqlite_store.go:1197-1291`

- [ ] **Step 1: Write the failing test (reconciliation + cache breakdown + no fan-out)**

Append to `internal/storage/sqlite_cost_test.go`:

```go
func TestGetCostSummaryReconcilesAndBreaksOutCache(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	store.UpsertModelPricing(ctx, ModelPricing{
		ModelName: "glm-5.2", InputPrice: 10.0, OutputPrice: 30.0, Currency: "CNY",
	})

	var tid [16]byte
	tid[15] = 9
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}
	mdl := "glm-5.2"
	u := func(v uint32) *uint32 { return &v }
	// span: input=2, output=100, cache_creation=189194, cache_read=5000 -> total=194296
	span := Span{TraceID: tid, SpanID: [8]byte{9}, Name: "llm", Kind: 2,
		StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
		InputTokens: u(2), OutputTokens: u(100),
		CacheCreationTokens: u(189194), CacheReadTokens: u(5000),
		TotalTokens: u(194296), GenAIRequestModel: &mdl}
	store.InsertSpans(ctx, res, ScopeInfo{}, []Span{span})
	store.UpdateTraceCost(ctx, tid)

	s, err := store.GetCostSummary(ctx, CostQuery{StartTimeMS: 0, EndTimeMS: 100000})
	if err != nil {
		t.Fatalf("GetCostSummary: %v", err)
	}
	o := s.Overview
	// total = input + cache_creation + cache_read + output
	if o.TotalTokens != 2+189194+5000+100 {
		t.Errorf("overview total_tokens = %d, want %d", o.TotalTokens, 2+189194+5000+100)
	}
	if o.TotalInputTokens != 2 {
		t.Errorf("input = %d, want 2", o.TotalInputTokens)
	}
	if o.TotalCacheCreationTokens != 189194 {
		t.Errorf("cache_creation = %d, want 189194", o.TotalCacheCreationTokens)
	}
	if o.TotalCacheReadTokens != 5000 {
		t.Errorf("cache_read = %d, want 5000", o.TotalCacheReadTokens)
	}
	if o.TotalOutputTokens != 100 {
		t.Errorf("output = %d, want 100", o.TotalOutputTokens)
	}
	if o.TotalTokens != o.TotalInputTokens+o.TotalCacheCreationTokens+o.TotalCacheReadTokens+o.TotalOutputTokens {
		t.Errorf("invariant broken: total != input+cc+cr+output")
	}
	// by_model: one row, no fan-out.
	if len(s.ByModel) != 1 {
		t.Fatalf("by_model rows = %d, want 1", len(s.ByModel))
	}
	m := s.ByModel[0]
	if m.Model != "glm-5.2" {
		t.Errorf("model = %s, want glm-5.2", m.Model)
	}
	if m.Tokens != o.TotalTokens {
		t.Errorf("by_model tokens = %d, want %d (no fan-out)", m.Tokens, o.TotalTokens)
	}
	if m.CacheReadTokens != 5000 {
		t.Errorf("by_model cache_read = %d, want 5000", m.CacheReadTokens)
	}
	// by_model cost == overview cost (every span lands in exactly one model bucket).
	if m.Cost < o.TotalCost-0.000001 || m.Cost > o.TotalCost+0.000001 {
		t.Errorf("by_model cost = %v, overview = %v (must match)", m.Cost, o.TotalCost)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestGetCostSummaryReconcilesAndBreaksOutCache -v`
Expected: FAIL — `TotalCacheCreationTokens`/`TotalCacheReadTokens` zero, by_model fan-out.

- [ ] **Step 3: Rewrite `GetCostSummary`**

In `internal/storage/sqlite_store.go`, replace the entire `GetCostSummary` body (lines 1197-1291) with:

```go
func (s *sqliteStore) GetCostSummary(ctx context.Context, q CostQuery) (*CostSummaryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Overview cost + count + currency from traces (no join, no fan-out).
	var overview CostOverview
	var currency string
	err := s.db.QueryRowContext(ctx,
		`SELECT
		    COALESCE(sum(cost), 0),
		    count(*),
		    COALESCE(max(cost_currency), 'USD')
		 FROM traces
		 WHERE start_time_ms >= ? AND start_time_ms <= ?
		   AND cost IS NOT NULL AND cost > 0`,
		q.StartTimeMS, q.EndTimeMS,
	).Scan(&overview.TotalCost, &overview.TraceCount, &currency)
	if err != nil {
		return nil, fmt.Errorf("query cost overview: %w", err)
	}
	if overview.TraceCount > 0 {
		overview.AvgCostPerTrace = math.Round(overview.TotalCost/float64(overview.TraceCount)*1e6) / 1e6
	}

	// Token buckets from spans (the single source of truth).
	err = s.db.QueryRowContext(ctx,
		`SELECT
		    COALESCE(sum(input_tokens), 0),
		    COALESCE(sum(cache_creation_tokens), 0),
		    COALESCE(sum(cache_read_tokens), 0),
		    COALESCE(sum(output_tokens), 0)
		 FROM spans
		 WHERE total_tokens IS NOT NULL
		   AND trace_id_hex IN (
		      SELECT trace_id_hex FROM traces
		      WHERE start_time_ms >= ? AND start_time_ms <= ?
		        AND cost IS NOT NULL AND cost > 0
		   )`,
		q.StartTimeMS, q.EndTimeMS,
	).Scan(&overview.TotalInputTokens, &overview.TotalCacheCreationTokens,
		&overview.TotalCacheReadTokens, &overview.TotalOutputTokens)
	if err != nil {
		// Non-critical: leave buckets at zero.
		overview.TotalInputTokens = 0
		overview.TotalCacheCreationTokens = 0
		overview.TotalCacheReadTokens = 0
		overview.TotalOutputTokens = 0
	}
	overview.TotalTokens = overview.TotalInputTokens + overview.TotalCacheCreationTokens +
		overview.TotalCacheReadTokens + overview.TotalOutputTokens

	// by_model: group spans directly (no fan-out join).
	rows, err := s.db.QueryContext(ctx,
		`SELECT
		    COALESCE(gen_ai_request_model, '(unknown)') AS model,
		    COALESCE(sum(cost), 0) AS cost,
		    COALESCE(sum(input_tokens), 0) AS input_tokens,
		    COALESCE(sum(cache_creation_tokens), 0) AS cache_creation_tokens,
		    COALESCE(sum(cache_read_tokens), 0) AS cache_read_tokens,
		    COALESCE(sum(output_tokens), 0) AS output_tokens,
		    count(DISTINCT trace_id_hex) AS trace_count
		 FROM spans
		 WHERE total_tokens IS NOT NULL
		   AND trace_id_hex IN (
		      SELECT trace_id_hex FROM traces
		      WHERE start_time_ms >= ? AND start_time_ms <= ?
		        AND cost IS NOT NULL AND cost > 0
		   )
		 GROUP BY gen_ai_request_model
		 ORDER BY cost DESC`,
		q.StartTimeMS, q.EndTimeMS,
	)
	if err != nil {
		return nil, fmt.Errorf("query cost by model: %w", err)
	}
	defer rows.Close()

	var byModel []ModelCostItem
	for rows.Next() {
		var m ModelCostItem
		if err := rows.Scan(&m.Model, &m.Cost, &m.InputTokens, &m.CacheCreationTokens,
			&m.CacheReadTokens, &m.OutputTokens, &m.TraceCount); err != nil {
			return nil, fmt.Errorf("scan model cost: %w", err)
		}
		m.Tokens = m.InputTokens + m.CacheCreationTokens + m.CacheReadTokens + m.OutputTokens
		if m.TraceCount > 0 {
			m.AvgCost = math.Round(m.Cost/float64(m.TraceCount)*1e6) / 1e6
		}
		byModel = append(byModel, m)
	}
	if byModel == nil {
		byModel = []ModelCostItem{}
	}

	return &CostSummaryResult{
		Currency: currency,
		Overview: overview,
		ByModel:  byModel,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -run "TestGetCostSummaryReconcilesAndBreaksOutCache|TestUpdateTraceCostWritesSpanCostAndTotal" -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/storage/sqlite_store.go internal/storage/sqlite_cost_test.go
git commit -m "feat(storage): rewrite GetCostSummary to aggregate tokens/cost from spans with cache breakdown"
```

---

### Task 9: memstore span cost + `GetCostSummary` rewrite

**Files:**
- Modify: `internal/storage/memstore.go:927-1053` (GetCostSummary), span cost handling in InsertSpans

- [ ] **Step 1: Write the failing test (memstore build)**

Create `internal/storage/memstore_cost_test.go` with build tag `!local_engine && nosqlite`:

```go
//go:build !local_engine && nosqlite

package storage

import (
	"context"
	"testing"
)

func TestMemstoreGetCostSummaryReconciles(t *testing.T) {
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

	var tid [16]byte
	tid[15] = 3
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}
	mdl := "glm-5.2"
	u := func(v uint32) *uint32 { return &v }
	span := Span{TraceID: tid, SpanID: [8]byte{3}, Name: "llm", Kind: 2,
		StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
		InputTokens: u(2), OutputTokens: u(100),
		CacheReadTokens: u(5000), TotalTokens: u(5102), GenAIRequestModel: &mdl}
	if err := s.InsertSpans(ctx, res, ScopeInfo{}, []Span{span}); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}
	s.UpdateTraceCost(ctx, tid)

	summary, err := s.GetCostSummary(ctx, CostQuery{StartTimeMS: 0, EndTimeMS: 100000})
	if err != nil {
		t.Fatalf("GetCostSummary: %v", err)
	}
	o := summary.Overview
	if o.TotalTokens != 2+5000+100 {
		t.Errorf("total = %d, want %d", o.TotalTokens, 2+5000+100)
	}
	if o.TotalCacheReadTokens != 5000 {
		t.Errorf("cache_read = %d, want 5000", o.TotalCacheReadTokens)
	}
	if o.TotalTokens != o.TotalInputTokens+o.TotalCacheCreationTokens+o.TotalCacheReadTokens+o.TotalOutputTokens {
		t.Errorf("invariant broken")
	}
	if len(summary.ByModel) != 1 || summary.ByModel[0].Model != "glm-5.2" {
		t.Errorf("by_model = %+v", summary.ByModel)
	}
	if summary.ByModel[0].Tokens != o.TotalTokens {
		t.Errorf("by_model tokens = %d, want %d", summary.ByModel[0].Tokens, o.TotalTokens)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags nosqlite ./internal/storage/ -run TestMemstoreGetCostSummaryReconciles -v`
Expected: FAIL — `TotalCacheReadTokens` zero / by_model tokens from `t.TotalTokens` (stale) rather than spans.

- [ ] **Step 3: Rewrite memstore `GetCostSummary`**

In `internal/storage/memstore.go`, replace `GetCostSummary` (lines 927-1053) with a spans-based aggregation. Iterate `m.spans` grouped by model, summing `cost`/`input`/`cache_creation`/`cache_read`/`output` and distinct trace count; compute overview totals from the same span pass; `total = input+cc+cr+output`:

```go
func (m *memStore) GetCostSummary(ctx context.Context, q CostQuery) (*CostSummaryResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Traces in range with cost>0.
	costlyTraces := make(map[[16]byte]struct{})
	var totalCost float64
	var currency string
	for tid, t := range m.traces {
		if q.StartTimeMS > 0 && t.StartTimeMS < q.StartTimeMS {
			continue
		}
		if q.EndTimeMS > 0 && t.StartTimeMS > q.EndTimeMS {
			continue
		}
		if t.Cost == nil || *t.Cost == 0 {
			continue
		}
		costlyTraces[tid] = struct{}{}
		totalCost += *t.Cost
		if currency == "" && t.CostCurrency != "" {
			currency = t.CostCurrency
		}
	}
	traceCount := len(costlyTraces)

	type modelAgg struct {
		cost, input, cc, cr, output uint64
		traceCount                   int
		traces                       map[[16]byte]struct{}
	}
	agg := map[string]*modelAgg{}
	var oIn, oCC, oCR, oOut uint64

	for _, s := range m.spans {
		if _, ok := costlyTraces[s.TraceID]; !ok {
			continue
		}
		if s.TotalTokens == nil {
			continue
		}
		model := "(unknown)"
		if s.GenAIRequestModel != nil && *s.GenAIRequestModel != "" {
			model = *s.GenAIRequestModel
		}
		entry := agg[model]
		if entry == nil {
			entry = &modelAgg{traces: map[[16]byte]struct{}{}}
			agg[model] = entry
		}
		entry.traces[s.TraceID] = struct{}{}
		if s.Cost != nil {
			entry.cost += uint64(*s.Cost * 1e6) // store as fixed-point to avoid float keys
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

	byModel := make([]ModelCostItem, 0, len(agg))
	for model, e := range agg {
		e.traceCount = len(e.traces)
		var costF float64
		if e.traceCount > 0 {
			costF = float64(e.cost) / 1e6
		}
		avg := 0.0
		if e.traceCount > 0 {
			avg = math.Round(costF/float64(e.traceCount)*1e6) / 1e6
		}
		byModel = append(byModel, ModelCostItem{
			Model: model, Cost: costF,
			Tokens: e.input + e.cc + e.cr + e.output,
			InputTokens: e.input, CacheCreationTokens: e.cc,
			CacheReadTokens: e.cr, OutputTokens: e.output,
			TraceCount: e.traceCount, AvgCost: avg,
		})
	}
	sort.Slice(byModel, func(i, j int) bool { return byModel[i].Cost > byModel[j].Cost })

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
		ByModel: byModel,
	}, nil
}
```

Add `"math"` to memstore.go imports if missing (it likely already is, for rounding elsewhere).

- [ ] **Step 4: Set per-span `Cost` on the stored memstore spans**

Memstore appends spans by value at `memstore.go:68` (`m.spans = append(m.spans, inSpans...)`), then computes trace cost in the loop at `memstore.go:114-166` by iterating `inSpans` — but it never sets per-span `Span.Cost` on the stored copies. Replace the entire cost-calc loop (`memstore.go:114-166`) with a version that iterates `m.spans` by index (pointer access sets `Cost` on the stored span) and recomputes per-span cost:

```go
	// Calculate costs inline for traces with token data (lock already held).
	// Set per-span Cost on stored spans and aggregate trace cost.
	for traceID := range traceMap {
		merged, ok := m.traces[traceID]
		if !ok || merged.TotalTokens == nil || *merged.TotalTokens == 0 {
			continue
		}
		var totalCost float64
		var currency string
		hasCost := false
		for i := range m.spans {
			s := &m.spans[i]
			if s.TraceID != traceID {
				continue
			}
			if s.TotalTokens == nil || *s.TotalTokens == 0 || s.GenAIRequestModel == nil || *s.GenAIRequestModel == "" {
				continue
			}
			for _, p := range m.pricing {
				if p.ModelName == *s.GenAIRequestModel {
					inputT, outputT, ccT, crT := 0.0, 0.0, 0.0, 0.0
					if s.InputTokens != nil {
						inputT = float64(*s.InputTokens)
					}
					if s.OutputTokens != nil {
						outputT = float64(*s.OutputTokens)
					}
					if s.CacheCreationTokens != nil {
						ccT = float64(*s.CacheCreationTokens)
					}
					if s.CacheReadTokens != nil {
						crT = float64(*s.CacheReadTokens)
					}
					spanCost := (inputT*p.InputPrice+ccT*p.InputPrice*1.25+
						crT*p.InputPrice*0.1+outputT*p.OutputPrice) / 1_000_000.0
					spanCost = math.Round(spanCost*1e6) / 1e6
					s.Cost = &spanCost
					s.CostCurrency = p.Currency
					totalCost += spanCost
					hasCost = true
					if currency == "" {
						currency = p.Currency
					}
					break
				}
			}
		}
		if hasCost {
			merged.Cost = &totalCost
			merged.CostCurrency = currency
			m.traces[traceID] = merged
		}
	}
```

Add `"math"` to memstore.go imports if not already present. (Iterating `m.spans` recomputes cost for all of the trace's spans on each batch — idempotent and reflects current pricing.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -tags nosqlite ./internal/storage/ -run TestMemstoreGetCostSummaryReconciles -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/storage/memstore.go internal/storage/memstore_cost_test.go
git commit -m "feat(storage): memstore GetCostSummary from spans with cache breakdown; set span cost"
```

---

### Task 10: Frontend API types

**Files:**
- Modify: `web/src/api/client.ts:323-340`

- [ ] **Step 1: Add cache fields to `CostOverview`**

```typescript
export interface CostOverview {
  total_cost: number
  total_tokens: number
  total_input_tokens: number
  total_cache_creation_tokens: number
  total_cache_read_tokens: number
  total_output_tokens: number
  avg_cost_per_trace: number
  trace_count: number
}
```

- [ ] **Step 2: Add cache fields to `ModelCost`**

```typescript
export interface ModelCost {
  model: string
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

- [ ] **Step 3: Verify types compile**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors (CostDashboard.vue's `summary` ref literal needs the new fields — add them in Task 11; if vue-tsc errors on the ref literal now, proceed to Task 11 then re-run).

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat(web): add cache token fields to cost summary types"
```

---

### Task 11: Cost page UI — cache breakdown + i18n

**Files:**
- Modify: `web/src/views/CostDashboard.vue:26-30` (overview card), `:43-62` (by_model table), `:83-95` (summary ref default)
- Modify: `web/src/i18n/locales/en.ts:147-163` and `web/src/i18n/locales/zh.ts:147-163`

- [ ] **Step 1: Add i18n keys (en)**

In `web/src/i18n/locales/en.ts`, inside the `costDashboard` block, add after `totalTokens:`:

```typescript
    cacheRead: 'Cache Read',
    cacheWrite: 'Cache Write',
    cache: 'Cache',
    cacheHitRate: 'Cache Hit Rate',
```

- [ ] **Step 2: Add i18n keys (zh)**

In `web/src/i18n/locales/zh.ts`, inside the `costDashboard` block, add after `totalTokens:`:

```typescript
    cacheRead: '缓存读',
    cacheWrite: '缓存写',
    cache: '缓存',
    cacheHitRate: '缓存命中率',
```

- [ ] **Step 3: Add a cache breakdown card to the overview**

In `web/src/views/CostDashboard.vue`, replace the Total Tokens card (lines 26-30) with two cards — keep Total Tokens, add Cache breakdown:

```html
        <div class="card">
          <div class="card-label">{{ t('costDashboard.totalTokens') }}</div>
          <div class="card-value">{{ formatNumber(summary.overview.total_tokens) }}</div>
          <div class="card-sub">{{ formatNumber(summary.overview.total_input_tokens) }} in / {{ formatNumber(summary.overview.total_output_tokens) }} out</div>
        </div>
        <div class="card">
          <div class="card-label">{{ t('costDashboard.cache') }}</div>
          <div class="card-value">{{ formatNumber(summary.overview.total_cache_read_tokens + summary.overview.total_cache_creation_tokens) }}</div>
          <div class="card-sub">{{ formatNumber(summary.overview.total_cache_read_tokens) }} read / {{ formatNumber(summary.overview.total_cache_creation_tokens) }} write</div>
        </div>
```

Update the grid to 5 columns (line 169): `grid-template-columns: repeat(5, 1fr);` and the media query (line 236) to `repeat(2, 1fr)` (already 2 — fine).

- [ ] **Step 4: Add a Cache column to the by_model table**

In the same file, add a `<th>` after the Tokens header (line 48) and a `<td>` after the tokens cell (line 57):

```html
            <th>{{ t('costDashboard.cache') }}</th>
```

```html
            <td>{{ formatNumber(m.cache_read_tokens + m.cache_creation_tokens) }}</td>
```

- [ ] **Step 5: Update the `summary` ref default to include new fields**

In the `summary` ref literal (lines 86-93), add the cache fields:

```typescript
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
```

- [ ] **Step 6: Verify types + build**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors.

Run: `cd web && npm run build`
Expected: build succeeds.

- [ ] **Step 7: Commit**

```bash
git add web/src/views/CostDashboard.vue web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat(web): show cache token breakdown on cost dashboard"
```

---

### Task 12: Full build + test verification

- [ ] **Step 1: Run all Go tests (sqlite build, default)**

Run: `go test ./internal/... ./cmd/...`
Expected: all PASS. (If WDAC blocks the temp binary: `go test -c -o /tmp/labubu.test ./internal/storage && /tmp/labubu.test`, then `go test ./internal/receiver/... ./internal/api/...`.)

- [ ] **Step 2: Run memstore-build tests**

Run: `go test -tags nosqlite ./internal/...`
Expected: all PASS (memstore cost test + existing memstore tests).

- [ ] **Step 3: Build the binary (dev, no CGO)**

Run: `go build -tags "dev nosqlite" -o /dev/null ./cmd/labubu`
Expected: builds with no errors.

- [ ] **Step 4: Manual smoke test against the live DB**

Run the server against the existing `data/labubu.db`:

```bash
go run -tags dev ./cmd/labubu serve
```

Open `http://localhost:8080/cost`, select "Today":
- Overview `Total Tokens` should now equal `input + cache_read + cache_creation + output`.
- A "Cache" card shows cache read/write tokens (claude-code traces will show large cache read; jiuwenclaw shows 0).
- by_model table: each row's tokens no longer ~15× the overview; cost no longer ~56×; a Cache column is populated.
- Hit `/api/v1/model-pricing/recalc` (POST) once to backfill `spans.cost` + `traces.total_tokens` for pre-existing spans, then refresh.

- [ ] **Step 5: Commit any remaining fixes**

```bash
git add -A
git commit -m "test: verification pass for accurate token/cost aggregation"
```

---

## Self-Review Notes

- **Spec coverage:** spec §1 (span cost schema) → Tasks 3-5; §2 (DeriveTokenBuckets, remove override) → Tasks 1-2; §3 (GetCostSummary rewrite, overview+by_model) → Tasks 8-9; §4 (API types) → Tasks 3, 10; §5 (frontend cache breakdown) → Task 11; §6 (traces.total_tokens reliability: accumulate + recompute) → Tasks 6-7. All spec sections covered.
- **Type consistency:** `DeriveTokenBuckets` returns `(input, output, cacheCreation, cacheRead, total *uint32)` — used identically in Task 1 test, Task 2 receiver, Task 2 updated test. `CostOverview.TotalCacheCreationTokens` / `TotalCacheReadTokens` and `ModelCostItem.CacheCreationTokens` / `CacheReadTokens` names match across storage.go (Task 3), SQL Scan (Task 8), memstore (Task 9), and client.ts (Task 10). JSON tags `total_cache_creation_tokens` / `total_cache_read_tokens` / `cache_creation_tokens` / `cache_read_tokens` match frontend field names in Task 10/11.
- **Placeholder scan:** No TBD/TODO/"add appropriate" placeholders remain. Task 4 uses `encoding/hex.DecodeString` inline (concrete, no helper to discover). Task 9 Step 4 gives the full memstore cost-calc loop replacement. All code steps show complete code.
- **chDB (`cgo && local_engine`):** out of scope per spec. chDBStore does not currently define `GetCostSummary` (verified: only sqlite + memstore do, via the `NewChDBStore` build-tag shim). No chDB changes in this plan; if a CGO build is attempted later, add an equivalent chDB `GetCostSummary` then.
