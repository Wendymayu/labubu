//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
)

// insertFilterSpan inserts one span with a distinct SpanID into trace tid.
// cost is driven by tokens: 1 in + 1 out at price 1e6 (per-million) = cost 2 per span.
func insertFilterSpan(t *testing.T, s Store, tid [16]byte, spanIdx byte, dur uint64) {
	t.Helper()
	u := func(v uint32) *uint32 { return &v }
	mdl := "m"
	span := Span{
		TraceID: tid, SpanID: [8]byte{spanIdx}, Name: "op", Kind: 2,
		StartTimeMS: 1000, EndTimeMS: 1000 + dur, DurationMS: dur,
		InputTokens: u(1), OutputTokens: u(1), TotalTokens: u(2), GenAIRequestModel: &mdl,
	}
	res := ResourceInfo{Attributes: map[string]string{"service.name": "svc"}}
	if err := s.InsertSpans(context.Background(), res, ScopeInfo{}, []Span{span}); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}
}

func TestSqliteListTracesFiltersBySpansAndCost(t *testing.T) {
	dir := t.TempDir()
	s, err := NewChDBStore(dir)
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.UpsertModelPricing(ctx, ModelPricing{ModelName: "m", InputPrice: 1_000_000, OutputPrice: 1_000_000, Currency: "USD"})

	tidA := [16]byte{15: 1}
	tidB := [16]byte{15: 2}

	// Trace A: 2 spans (cost ~4)
	insertFilterSpan(t, s, tidA, 1, 100)
	insertFilterSpan(t, s, tidA, 2, 100)
	s.UpdateTraceCost(ctx, tidA)

	// Trace B: 5 spans (cost ~10)
	for i := byte(1); i <= 5; i++ {
		insertFilterSpan(t, s, tidB, i, 200)
	}
	s.UpdateTraceCost(ctx, tidB)

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

	mustLen(TraceQuery{Page: 1, PageSize: 100, MinSpanCount: 3}, 1) // only B (5 spans)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MaxSpanCount: 3}, 1) // only A (2 spans)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MinCost: 5.0}, 1)    // only B (cost ~10)
	mustLen(TraceQuery{Page: 1, PageSize: 100, MaxCost: 5.0}, 1)    // only A (cost ~4)
}

func traceIDs(items []TraceListItem) []string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.TraceIDHex
	}
	return ids
}
