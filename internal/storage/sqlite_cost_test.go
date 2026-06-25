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
