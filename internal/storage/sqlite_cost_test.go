//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"database/sql"
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

// TestInsertSpansMultiBatchFinalTotal is an end-to-end check that two batches
// for the same trace yield the correct final total. The merge arithmetic itself
// is guarded deterministically by TestMergeTotalTokens.
func TestInsertSpansMultiBatchFinalTotal(t *testing.T) {
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
	// Both spans counted: 1000 + 500 = 1500.
	if tr.Traces[0].TotalTokens == nil || *tr.Traces[0].TotalTokens != 1500 {
		t.Errorf("traces.total_tokens = %v, want 1500", tr.Traces[0].TotalTokens)
	}
}

func TestMergeTotalTokens(t *testing.T) {
	u := func(v uint32) *uint32 { return &v }
	valid := func(n int32) sql.NullInt32 { return sql.NullInt32{Int32: n, Valid: true} }
	invalid := sql.NullInt32{Valid: false}
	tests := []struct {
		name     string
		existing sql.NullInt32
		new      *uint32
		want     *uint32
	}{
		{"accumulate existing+new", valid(1000), u(500), u(1500)},
		{"preserve existing when new nil", valid(1000), nil, u(1000)},
		{"new only when no existing", invalid, u(500), u(500)},
		{"nil when neither", invalid, nil, nil},
		{"accumulate with existing zero", valid(0), u(500), u(500)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeTotalTokens(tt.existing, tt.new)
			if !u32eq(got, tt.want) {
				t.Errorf("mergeTotalTokens(%v, %v) = %v, want %v", tt.existing, tt.new, got, tt.want)
			}
		})
	}
}
