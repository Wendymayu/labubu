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
	var sum float64
	for _, row := range summary.ByService {
		sum += row.Cost
	}
	if sum < summary.Overview.TotalCost-0.000001 || sum > summary.Overview.TotalCost+0.000001 {
		t.Errorf("by_service cost sum = %v, overview = %v (must match)", sum, summary.Overview.TotalCost)
	}
}
