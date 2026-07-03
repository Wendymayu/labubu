//go:build !local_engine && nosqlite

package storage

import (
	"context"
	"testing"
)

// TestMemstoreGetSessionPaginatesTraces is the memStore counterpart of the
// SQLite test: GetSession returns a page of traces with Pagination, while the
// summary trace_count reflects the total.
func TestMemstoreGetSessionPaginatesTraces(t *testing.T) {
	dir := t.TempDir()
	s, err := NewChDBStore(dir) // memstore constructor in nosqlite builds
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
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
	if err := s.InsertSpans(ctx, res, ScopeInfo{}, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	d, err := s.GetSession(ctx, sess, 1, 2)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if d == nil {
		t.Fatal("GetSession returned nil")
	}
	if d.Session.TraceCount != 5 {
		t.Errorf("summary TraceCount = %d, want 5", d.Session.TraceCount)
	}
	if len(d.Traces) != 2 {
		t.Fatalf("page traces = %d, want 2", len(d.Traces))
	}
	if d.Pagination.Total != 5 || d.Pagination.Page != 1 || d.Pagination.PageSize != 2 {
		t.Errorf("pagination = %+v, want {1,2,5}", d.Pagination)
	}
	if d.Traces[0].StartTimeMS != 1000 {
		t.Errorf("first page first trace start = %d, want 1000", d.Traces[0].StartTimeMS)
	}

	d3, err := s.GetSession(ctx, sess, 3, 2)
	if err != nil {
		t.Fatalf("GetSession page 3: %v", err)
	}
	if len(d3.Traces) != 1 {
		t.Errorf("page 3 traces = %d, want 1", len(d3.Traces))
	}
}
