//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
)

// TestSQLiteSessionErrorRateIsRatio guards the unit contract for
// SessionListItem.ErrorRate: it must be a ratio in [0,1]. The frontend
// (SessionList.vue / SessionDetail.vue) multiplies by 100 to render a
// percentage, and the memstore and chDB backends both return a ratio.
// SQLite previously computed it as a percentage (0-100) via "*100.0", so a
// 100%-error session rendered as 10000% and a 50%-error session as 5000%.
// With 1 ERROR trace of 2, error_rate must be 0.5 — not 50.
func TestSQLiteSessionErrorRateIsRatio(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const sess = "sess-errrate"
	sessAttr := map[string]string{"jiuwenclaw.session.id": sess}
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}

	// Two single-span traces in the session: one OK, one ERROR.
	tidOK := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	tidErr := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}
	spans := []Span{
		{TraceID: tidOK, SpanID: [8]byte{1}, ParentSpanID: [8]byte{}, Name: "root-ok", Kind: 2,
			StartTimeMS: 10, EndTimeMS: 20, DurationMS: 10, StatusCode: 1, Attributes: sessAttr},
		{TraceID: tidErr, SpanID: [8]byte{2}, ParentSpanID: [8]byte{}, Name: "root-err", Kind: 2,
			StartTimeMS: 30, EndTimeMS: 40, DurationMS: 10, StatusCode: 2, Attributes: sessAttr},
	}
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	// GetSession (detail path).
	detail, err := store.GetSession(ctx, sess)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if detail.Session.TraceCount != 2 {
		t.Errorf("TraceCount = %d, want 2", detail.Session.TraceCount)
	}
	if detail.Session.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", detail.Session.ErrorCount)
	}
	if detail.Session.ErrorRate != 0.5 {
		t.Errorf("GetSession ErrorRate = %v, want 0.5 (ratio; frontend multiplies by 100, so 50 would render as 5000%%)", detail.Session.ErrorRate)
	}

	// ListSessions (list path) — same SQL, must also be a ratio.
	list, err := store.ListSessions(ctx, SessionQuery{Page: 1, PageSize: 10})
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
		t.Fatalf("session %q not found in list", sess)
	}
	if got.ErrorRate != 0.5 {
		t.Errorf("ListSessions ErrorRate = %v, want 0.5 (ratio; 50 would render as 5000%%)", got.ErrorRate)
	}
}
