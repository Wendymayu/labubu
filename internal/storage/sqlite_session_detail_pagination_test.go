//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
)

// TestSQLiteGetSessionPaginatesTraces verifies GetSession returns a page of
// traces (not the whole session) along with Pagination, while the session
// summary trace_count still reflects the total. The session detail page
// paginates its "Turns" list via these params.
func TestSQLiteGetSessionPaginatesTraces(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const sess = "sess-page"
	sessAttr := map[string]string{"jiuwenclaw.session.id": sess}
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}

	spans := make([]Span, 0, 5)
	for i := 0; i < 5; i++ {
		var tid [16]byte
		tid[15] = byte(i + 1)
		var sid [8]byte
		sid[7] = byte(i + 1)
		spans = append(spans, Span{
			TraceID: tid, SpanID: sid, Name: "root", Kind: 2,
			StartTimeMS: uint64(1000 + i*100), EndTimeMS: uint64(1000 + i*100 + 10),
			DurationMS: 10, StatusCode: 1, Attributes: sessAttr,
		})
	}
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	// Page 1, size 2 → earliest 2 traces; total 5.
	d, err := store.GetSession(ctx, sess, 1, 2)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if d == nil {
		t.Fatal("GetSession returned nil")
	}
	if d.Session.TraceCount != 5 {
		t.Errorf("summary TraceCount = %d, want 5 (total, not page)", d.Session.TraceCount)
	}
	if len(d.Traces) != 2 {
		t.Fatalf("page traces = %d, want 2", len(d.Traces))
	}
	if d.Pagination.Total != 5 || d.Pagination.Page != 1 || d.Pagination.PageSize != 2 {
		t.Errorf("pagination = %+v, want {1,2,5}", d.Pagination)
	}
	if d.Traces[0].StartTimeMS != 1000 {
		t.Errorf("first page first trace start = %d, want 1000 (ascending)", d.Traces[0].StartTimeMS)
	}

	// Page 3, size 2 → 1 trace remaining (offset 4).
	d3, err := store.GetSession(ctx, sess, 3, 2)
	if err != nil {
		t.Fatalf("GetSession page 3: %v", err)
	}
	if len(d3.Traces) != 1 {
		t.Errorf("page 3 traces = %d, want 1", len(d3.Traces))
	}
	if d3.Pagination.Total != 5 {
		t.Errorf("page 3 total = %d, want 5", d3.Pagination.Total)
	}
}
