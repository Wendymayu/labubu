//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
)

// TestSQLiteListTracesSurfacesRootSpanInputMessages verifies the trace list
// surfaces the root span's gen_ai.input.messages attribute (the data source
// for the "Input" column). The attribute is persisted inside the root span's
// attributes blob in the spans table; ListTraces must look it up and attach
// it to the matching item.
func TestSQLiteListTracesSurfacesRootSpanInputMessages(t *testing.T) {
	store := newTestStore(t)
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
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	list, err := store.ListTraces(ctx, TraceQuery{Page: 1, PageSize: 10})
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

	// A trace whose root span lacks the attribute must leave it unset (column
	// renders empty), not error.
	var tid2 [16]byte
	tid2[15] = 7
	tid2Hex := TraceIDToHex(tid2)
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, []Span{
		{TraceID: tid2, SpanID: rootSID, Name: "root2", Kind: 2,
			StartTimeMS: 2000, EndTimeMS: 2050, DurationMS: 50, StatusCode: 1,
			Attributes: map[string]string{"other": "x"}},
	}); err != nil {
		t.Fatalf("InsertSpans second: %v", err)
	}
	list2, err := store.ListTraces(ctx, TraceQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListTraces second: %v", err)
	}
	for _, tr := range list2.Traces {
		if tr.TraceIDHex == tid2Hex && tr.InputMessages != nil {
			t.Errorf("trace without gen_ai.input.messages got InputMessages=%q", *tr.InputMessages)
		}
	}
}
