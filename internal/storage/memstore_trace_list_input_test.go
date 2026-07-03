//go:build !local_engine && nosqlite

package storage

import (
	"context"
	"testing"
)

// TestMemstoreListTracesSurfacesRootSpanInputMessages is the memStore
// counterpart of the SQLite test: ListTraces must attach the root span's
// gen_ai.input.messages attribute to the matching item.
func TestMemstoreListTracesSurfacesRootSpanInputMessages(t *testing.T) {
	dir := t.TempDir()
	s, err := NewChDBStore(dir) // memstore constructor in nosqlite builds
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}

	var tid [16]byte
	tid[15] = 9
	var rootSID [8]byte
	rootSID[7] = 1
	var childSID [8]byte
	childSID[7] = 2

	const inputAttr = `[{"role":"user","content":"hello"}]`
	spans := []Span{
		{TraceID: tid, SpanID: rootSID, Name: "root", Kind: 2,
			StartTimeMS: 1000, EndTimeMS: 1100, DurationMS: 100, StatusCode: 1,
			Attributes: map[string]string{"gen_ai.input.messages": inputAttr}},
		{TraceID: tid, SpanID: childSID, ParentSpanID: rootSID, Name: "child", Kind: 1,
			StartTimeMS: 1050, EndTimeMS: 1080, DurationMS: 30, StatusCode: 1,
			Attributes: map[string]string{}},
	}
	if err := s.InsertSpans(ctx, res, ScopeInfo{}, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	list, err := s.ListTraces(ctx, TraceQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	if len(list.Traces) != 1 {
		t.Fatalf("got %d traces, want 1", len(list.Traces))
	}
	got := list.Traces[0]
	if got.InputMessages == nil {
		t.Fatal("InputMessages is nil; want the root span's gen_ai.input.messages")
	}
	if *got.InputMessages != inputAttr {
		t.Errorf("InputMessages = %q, want %q", *got.InputMessages, inputAttr)
	}
}
