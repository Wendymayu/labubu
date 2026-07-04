//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
)

// TestSQLiteGetSessionContextSpans verifies end-to-end that the SQLite store
// returns only the main agent's LLM spans for a session, excluding subagent
// LLM spans. Mirrors TestSQLiteGetSessionAgentStats's InsertSpans -> query flow.
func TestSQLiteGetSessionContextSpans(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const sess = "sess-C"

	tid1 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	tid2 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}

	sessAttr := map[string]string{"jiuwenclaw.session.id": sess}
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}

	u := func(v uint32) *uint32 { return &v }
	model := "glm-5.2"

	// tid1: root agent.invoke (id 1) + two main-agent LLM spans (3,4) +
	// a subagent.invoke (5) + one subagent LLM span (6) under it.
	root1 := Span{TraceID: tid1, SpanID: [8]byte{1}, ParentSpanID: [8]byte{}, Name: "jiuwenclaw.agent.invoke", Kind: 2,
		StartTimeMS: 0, EndTimeMS: 700, DurationMS: 700, StatusCode: 1, Attributes: sessAttr}
	mAllm := func(id byte, parent byte, start uint64, tokens uint32) Span {
		return Span{TraceID: tid1, SpanID: [8]byte{id}, ParentSpanID: [8]byte{parent}, Name: "chat", Kind: 2,
			StartTimeMS: start, EndTimeMS: start + 50, DurationMS: 50, StatusCode: 1,
			Attributes:  sessAttr,
			InputTokens: u(tokens), OutputTokens: u(tokens / 4), TotalTokens: u(tokens),
			GenAIRequestModel: &model}
	}
	subInvoke := Span{TraceID: tid1, SpanID: [8]byte{5}, ParentSpanID: [8]byte{1}, Name: "jiuwenclaw.subagent.invoke", Kind: 2,
		StartTimeMS: 150, EndTimeMS: 300, DurationMS: 150, StatusCode: 1, Attributes: sessAttr}

	// tid2: root agent.invoke (id 2) + one main-agent LLM span (7).
	root2 := Span{TraceID: tid2, SpanID: [8]byte{2}, ParentSpanID: [8]byte{}, Name: "jiuwenclaw.agent.invoke", Kind: 2,
		StartTimeMS: 350, EndTimeMS: 600, DurationMS: 250, StatusCode: 1, Attributes: sessAttr}
	b1 := Span{TraceID: tid2, SpanID: [8]byte{7}, ParentSpanID: [8]byte{2}, Name: "chat", Kind: 2,
		StartTimeMS: 400, EndTimeMS: 450, DurationMS: 50, StatusCode: 1, Attributes: sessAttr,
		InputTokens: u(3000), OutputTokens: u(750), TotalTokens: u(3000), GenAIRequestModel: &model}

	spans := []Span{
		root1,
		mAllm(3, 1, 100, 1000), // main, included
		mAllm(4, 1, 200, 2000), // main, included
		subInvoke,
		mAllm(6, 5, 160, 5000), // subagent, excluded
		root2,
		b1, // main, included
	}
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	got, err := store.GetSessionContextSpans(ctx, sess)
	if err != nil {
		t.Fatalf("GetSessionContextSpans: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d spans, want 3: %+v", len(got), got)
	}

	// Sorted by start time: span 3 (100), span 4 (200), span 7 (400).
	wantStart := []uint64{100, 200, 400}
	wantSpan := []byte{3, 4, 7}
	for i, s := range got {
		if s.StartTimeMS != wantStart[i] {
			t.Errorf("got[%d].StartTimeMS = %d, want %d", i, s.StartTimeMS, wantStart[i])
		}
		if s.TraceIDHex == "" {
			t.Errorf("got[%d].TraceIDHex empty", i)
		}
		// span_id_hex is the 8-byte id rendered as hex; the id byte is first
		// (e.g. [8]byte{3} -> "0300000000000000").
		if gotID := firstHexByte(s.SpanIDHex); gotID != wantSpan[i] {
			t.Errorf("got[%d] span id = %s, want leading %02x", i, s.SpanIDHex, wantSpan[i])
		}
	}
	if got[2].TotalTokens == nil || *got[2].TotalTokens != 3000 {
		t.Errorf("got[2].TotalTokens = %v, want 3000", got[2].TotalTokens)
	}

	// Unknown session -> (nil, nil) -> handler returns 404 no_context_data.
	got2, err := store.GetSessionContextSpans(ctx, "no-such-session")
	if err != nil {
		t.Errorf("GetSessionContextSpans(unknown): %v, want nil error", err)
	}
	if got2 != nil {
		t.Errorf("GetSessionContextSpans(unknown) = %+v, want nil", got2)
	}
}

// firstHexByte returns the numeric value of the first byte of a hex span id.
func firstHexByte(hexID string) byte {
	if len(hexID) < 2 {
		return 0
	}
	lo := hexID[:2]
	var b byte
	for i := 0; i < 2; i++ {
		c := lo[i]
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		}
		b = b<<4 | v
	}
	return b
}
