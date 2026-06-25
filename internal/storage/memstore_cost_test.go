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
	// by_model cost must equal overview cost (every span lands in one model bucket).
	if summary.ByModel[0].Cost < summary.Overview.TotalCost-0.000000001 ||
		summary.ByModel[0].Cost > summary.Overview.TotalCost+0.000000001 {
		t.Errorf("by_model cost = %v, overview = %v (must match)", summary.ByModel[0].Cost, summary.Overview.TotalCost)
	}
}
