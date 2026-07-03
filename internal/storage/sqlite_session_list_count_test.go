//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
)

// TestSQLiteSessionListCountMatchesDetailAcrossTimeFilter guards the
// contract that the session list's per-session trace_count (and other
// aggregates) must reflect the WHOLE session — identical to GetSession —
// not just the traces that fall inside the list's time/service/search
// filter. The filter must only gate WHICH sessions appear (sessions that
// have at least one matching trace).
//
// Repro: SessionList.vue defaults to period 'today', so a session spanning
// midnight is undercounted on the list while SessionDetail.vue shows the
// full session. The two pages must agree.
func TestSQLiteSessionListCountMatchesDetailAcrossTimeFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const sess = "sess-span"
	sessAttr := map[string]string{"jiuwenclaw.session.id": sess}
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}

	// 3 traces "early" (before the filter window), 2 traces "late" (inside).
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
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	// Detail path: no time filter → whole session.
	detail, err := store.GetSession(ctx, sess, 1, 50)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if detail.Session.TraceCount != 5 {
		t.Fatalf("GetSession TraceCount = %d, want 5", detail.Session.TraceCount)
	}
	if len(detail.Traces) != 5 {
		t.Fatalf("GetSession traces len = %d, want 5", len(detail.Traces))
	}

	// List path filtered to the late window (start >= 200). The session
	// must still appear (it has matching traces) and its TraceCount must
	// equal the whole-session count from the detail path.
	list, err := store.ListSessions(ctx, SessionQuery{Page: 1, PageSize: 10, StartTimeMS: 200})
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
		t.Fatalf("session %q missing from filtered list (should appear: has traces in window)", sess)
	}
	if got.TraceCount != detail.Session.TraceCount {
		t.Errorf("filtered list TraceCount = %d, want %d (whole-session count; must match detail)",
			got.TraceCount, detail.Session.TraceCount)
	}
	if got.FirstActiveMS != detail.Session.FirstActiveMS {
		t.Errorf("filtered list FirstActiveMS = %d, want %d (whole-session first active)",
			got.FirstActiveMS, detail.Session.FirstActiveMS)
	}
	if got.LastActiveMS != detail.Session.LastActiveMS {
		t.Errorf("filtered list LastActiveMS = %d, want %d (whole-session last active)",
			got.LastActiveMS, detail.Session.LastActiveMS)
	}
}
