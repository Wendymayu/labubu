//go:build !local_engine && nosqlite

package storage

import (
	"context"
	"testing"
)

// TestMemstoreSessionListCountMatchesDetailAcrossTimeFilter is the memStore
// counterpart of the SQLite test: the list's per-session trace_count must
// reflect the WHOLE session (matching GetSession), not the time-filtered
// subset. The filter only gates which sessions appear.
func TestMemstoreSessionListCountMatchesDetailAcrossTimeFilter(t *testing.T) {
	dir := t.TempDir()
	s, err := NewChDBStore(dir) // memstore constructor in nosqlite builds
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	const sess = "sess-span"
	sessAttr := map[string]string{"jiuwenclaw.session.id": sess}
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}

	mk := func(n byte, start uint64) Span {
		var tid [16]byte
		tid[15] = n
		return Span{
			TraceID: tid, SpanID: [8]byte{n}, Name: "root", Kind: 2,
			StartTimeMS: start, EndTimeMS: start + 10, DurationMS: 10,
			StatusCode: 1, Attributes: sessAttr,
		}
	}
	spans := []Span{
		mk(1, 100), mk(2, 110), mk(3, 120), // before window
		mk(4, 5000), mk(5, 5100),           // inside window
	}
	if err := s.InsertSpans(ctx, res, ScopeInfo{}, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	detail, err := s.GetSession(ctx, sess, 1, 50)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if detail.Session.TraceCount != 5 {
		t.Fatalf("GetSession TraceCount = %d, want 5", detail.Session.TraceCount)
	}

	list, err := s.ListSessions(ctx, SessionQuery{Page: 1, PageSize: 10, StartTimeMS: 200})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	var got *SessionListItem
	for i := range list.Sessions {
		if list.Sessions[i].SessionID == sess {
			got = &list.Sessions[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("session %q missing from filtered list", sess)
	}
	if got.TraceCount != detail.Session.TraceCount {
		t.Errorf("filtered list TraceCount = %d, want %d (whole-session; must match detail)",
			got.TraceCount, detail.Session.TraceCount)
	}
	if got.FirstActiveMS != detail.Session.FirstActiveMS {
		t.Errorf("filtered list FirstActiveMS = %d, want %d", got.FirstActiveMS, detail.Session.FirstActiveMS)
	}
}
