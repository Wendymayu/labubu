package storage

import "testing"

func TestAggregateTracesNoDoubleCount(t *testing.T) {
	u := func(v uint32) *uint32 { return &v }
	var tid [16]byte
	tid[15] = 1
	resource := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}
	scope := ScopeInfo{}

	t.Run("single span total not doubled", func(t *testing.T) {
		spans := []Span{{
			TraceID: tid, SpanID: [8]byte{1}, Name: "s", Kind: 2,
			StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
			TotalTokens: u(1150),
		}}
		got := aggregateTraces(resource, scope, spans)
		tr, ok := got[tid]
		if !ok {
			t.Fatalf("trace not found")
		}
		if tr.TotalTokens == nil {
			t.Fatalf("TotalTokens nil, want &1150")
		}
		if *tr.TotalTokens != 1150 {
			t.Errorf("TotalTokens = %d, want 1150 (first span must not be double-counted)", *tr.TotalTokens)
		}
	})

	t.Run("two spans sum correctly", func(t *testing.T) {
		spans := []Span{
			{TraceID: tid, SpanID: [8]byte{1}, Name: "s", Kind: 2,
				StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000, TotalTokens: u(1150)},
			{TraceID: tid, SpanID: [8]byte{2}, Name: "s", Kind: 2,
				StartTimeMS: 2000, EndTimeMS: 3000, DurationMS: 1000, TotalTokens: u(200)},
		}
		got := aggregateTraces(resource, scope, spans)
		tr := got[tid]
		if tr.TotalTokens == nil || *tr.TotalTokens != 1350 {
			t.Errorf("TotalTokens = %v, want 1350", tr.TotalTokens)
		}
	})

	t.Run("span with nil TotalTokens contributes nothing", func(t *testing.T) {
		spans := []Span{
			{TraceID: tid, SpanID: [8]byte{1}, Name: "s", Kind: 2,
				StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000, TotalTokens: u(1150)},
			{TraceID: tid, SpanID: [8]byte{2}, Name: "s", Kind: 2,
				StartTimeMS: 2000, EndTimeMS: 3000, DurationMS: 1000}, // nil TotalTokens
		}
		got := aggregateTraces(resource, scope, spans)
		tr := got[tid]
		if tr.TotalTokens == nil || *tr.TotalTokens != 1150 {
			t.Errorf("TotalTokens = %v, want 1150", tr.TotalTokens)
		}
	})
}
