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
	// Pricing is per-million-tokens in the store (cost / 1_000_000.0).
	// Use 1_000_000 so 1 input + 1 output token = cost 2 per span.
	s.UpsertModelPricing(ctx, ModelPricing{ModelName: "m", InputPrice: 1_000_000, OutputPrice: 1_000_000, Currency: "USD"})
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

	mustLen(TraceQuery{Page: 1, PageSize: 100, MinSpanCount: 2}, 1) // only B (3 spans)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MaxSpanCount: 2}, 1) // only A (1 span)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MinCost: 3.0}, 1)    // only B (cost ~6)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MaxCost: 3.0}, 1)    // only A (cost ~2)
}

func traceIDs(items []TraceListItem) []string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.TraceIDHex
	}
	return ids
}
