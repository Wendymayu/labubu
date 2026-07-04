package storage

import (
	"testing"
)

// ptrU returns a pointer to a uint32 — helper for token fields.
func ptrU(v uint32) *uint32 { return &v }

// ptrS returns a pointer to a string.
func ptrS(v string) *string { return &v }

// TestComputeSessionContextSpansMainThread verifies the main-agent filter:
// only LLM spans whose nearest .invoke ancestor is the trace's root span are
// returned; subagent LLM spans and non-LLM spans are excluded, and results
// are sorted by start time.
func TestComputeSessionContextSpansMainThread(t *testing.T) {
	// Trace A: root invoke (rA) owns two LLM spans (a1, a2); a subagent.invoke
	// (subA) under rA owns one LLM span (a3) which must be excluded.
	// Trace B: root invoke (rB) owns one LLM span (b1).
	rootByTrace := map[string]string{
		"traceA": "rA",
		"traceB": "rB",
	}
	spans := []sessionSpanInput{
		{TraceIDHex: "traceA", SpanIDHex: "rA", ParentSpanIDHex: "", Name: "jiuwenclaw.agent.invoke", StartTimeMS: 0},
		{TraceIDHex: "traceA", SpanIDHex: "a1", ParentSpanIDHex: "rA", Name: "chat", StartTimeMS: 100, TotalTokens: ptrU(1000), GenAIRequestModel: ptrS("gpt-4")},
		{TraceIDHex: "traceA", SpanIDHex: "a2", ParentSpanIDHex: "rA", Name: "chat", StartTimeMS: 200, TotalTokens: ptrU(2000)},
		{TraceIDHex: "traceA", SpanIDHex: "subA", ParentSpanIDHex: "rA", Name: "jiuwenclaw.subagent.invoke", StartTimeMS: 150},
		{TraceIDHex: "traceA", SpanIDHex: "a3", ParentSpanIDHex: "subA", Name: "chat", StartTimeMS: 160, TotalTokens: ptrU(5000)}, // subagent -> excluded
		{TraceIDHex: "traceB", SpanIDHex: "rB", ParentSpanIDHex: "", Name: "jiuwenclaw.agent.invoke", StartTimeMS: 300},
		{TraceIDHex: "traceB", SpanIDHex: "b1", ParentSpanIDHex: "rB", Name: "chat", StartTimeMS: 400, TotalTokens: ptrU(3000)},
		// Non-LLM span under root (total_tokens nil) -> excluded.
		{TraceIDHex: "traceA", SpanIDHex: "tool1", ParentSpanIDHex: "rA", Name: "tool", StartTimeMS: 120},
		// LLM span with total_tokens 0 -> excluded.
		{TraceIDHex: "traceA", SpanIDHex: "zero", ParentSpanIDHex: "rA", Name: "chat", StartTimeMS: 130, TotalTokens: ptrU(0)},
	}

	got := computeSessionContextSpans(rootByTrace, spans)

	if len(got) != 3 {
		t.Fatalf("got %d spans, want 3: %+v", len(got), got)
	}
	// Sorted by start time: a1(100), a2(200), b1(400).
	wantIDs := []string{"a1", "a2", "b1"}
	wantTokens := []uint32{1000, 2000, 3000}
	for i, s := range got {
		if s.SpanIDHex != wantIDs[i] {
			t.Errorf("got[%d].SpanIDHex = %q, want %q", i, s.SpanIDHex, wantIDs[i])
		}
		if s.TotalTokens == nil || *s.TotalTokens != wantTokens[i] {
			t.Errorf("got[%d].TotalTokens = %v, want %d", i, s.TotalTokens, wantTokens[i])
		}
	}
	// a1 carries the model; a2/b1 do not.
	if got[0].GenAIRequestModel == nil || *got[0].GenAIRequestModel != "gpt-4" {
		t.Errorf("got[0].GenAIRequestModel = %v, want gpt-4", got[0].GenAIRequestModel)
	}
	if got[1].GenAIRequestModel != nil {
		t.Errorf("got[1].GenAIRequestModel = %v, want nil", got[1].GenAIRequestModel)
	}
}

// TestComputeSessionContextSpansEmpty covers the no-data path.
func TestComputeSessionContextSpansEmpty(t *testing.T) {
	if got := computeSessionContextSpans(nil, nil); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
	if got := computeSessionContextSpans(map[string]string{"t": "r"}, nil); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// TestComputeSessionContextSpansNoInvokeAncestor: an LLM span with no .invoke
// ancestor (malformed tree) is excluded rather than crashing.
func TestComputeSessionContextSpansNoInvokeAncestor(t *testing.T) {
	rootByTrace := map[string]string{"t": "r"}
	spans := []sessionSpanInput{
		{TraceIDHex: "t", SpanIDHex: "r", ParentSpanIDHex: "", Name: "root", StartTimeMS: 0},
		{TraceIDHex: "t", SpanIDHex: "x", ParentSpanIDHex: "r", Name: "chat", StartTimeMS: 10, TotalTokens: ptrU(100)},
		// Orphan LLM span whose parent is missing from the map.
		{TraceIDHex: "t", SpanIDHex: "y", ParentSpanIDHex: "missing", Name: "chat", StartTimeMS: 20, TotalTokens: ptrU(200)},
	}
	got := computeSessionContextSpans(rootByTrace, spans)
	// "x" walks r (not invoke) -> no owner -> excluded; "y" parent missing -> excluded.
	if len(got) != 0 {
		t.Errorf("got %d spans, want 0: %+v", len(got), got)
	}
}
