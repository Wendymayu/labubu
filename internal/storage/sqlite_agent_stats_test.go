//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
)

// TestSQLiteGetSessionAgentStats verifies that the SQLite store computes agent
// stats for a session by delegating to the shared computeAgentStats. It mirrors
// the scenarios in TestComputeAgentStatsToolSuccessRate / LoopDetection /
// RetryDetection, but end-to-end through InsertSpans -> GetSessionAgentStats.
//
// Before the fix, GetSessionAgentStats returned "not implemented" and this
// test failed at the first call.
func TestSQLiteGetSessionAgentStats(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const sess = "sess-A"

	// Two traces in the session: tid1 OK, tid2 ERROR.
	tid1 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	tid2 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}

	sessAttr := map[string]string{"jiuwenclaw.session.id": sess}
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}

	// tid1: root (OK) + 3 file_read tool spans (all OK) -> file_read loop depth 3.
	root1 := Span{TraceID: tid1, SpanID: [8]byte{1}, ParentSpanID: [8]byte{}, Name: "root", Kind: 2,
		StartTimeMS: 50, EndTimeMS: 60, DurationMS: 10, StatusCode: 1, Attributes: sessAttr}
	fr := func(id byte, start uint64) Span {
		return Span{TraceID: tid1, SpanID: [8]byte{id}, ParentSpanID: [8]byte{1}, Name: "tool", Kind: 2,
			StartTimeMS: start, EndTimeMS: start + 10, DurationMS: 10, StatusCode: 1,
			Attributes: map[string]string{"gen_ai.tool.name": "file_read"}}
	}

	// tid2: root (ERROR) + 3 web_search tool spans (ERROR, ERROR, OK) -> 2 retries.
	root2 := Span{TraceID: tid2, SpanID: [8]byte{2}, ParentSpanID: [8]byte{}, Name: "root", Kind: 2,
		StartTimeMS: 350, EndTimeMS: 360, DurationMS: 10, StatusCode: 2, Attributes: sessAttr}
	ws := func(id byte, start uint64, status int32) Span {
		return Span{TraceID: tid2, SpanID: [8]byte{id}, ParentSpanID: [8]byte{2}, Name: "tool", Kind: 2,
			StartTimeMS: start, EndTimeMS: start + 10, DurationMS: 10, StatusCode: status,
			Attributes: map[string]string{"gen_ai.tool.name": "web_search"}}
	}

	spans := []Span{
		root1, fr(3, 100), fr(4, 200), fr(5, 300),
		root2, ws(6, 400, 2), ws(7, 500, 2), ws(8, 600, 1),
	}
	if err := store.InsertSpans(ctx, res, ScopeInfo{}, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	got, err := store.GetSessionAgentStats(ctx, sess)
	if err != nil {
		t.Fatalf("GetSessionAgentStats: %v", err)
	}
	if got == nil {
		t.Fatal("GetSessionAgentStats returned nil for a session with traces")
	}

	// 2 traces (OK, ERROR) -> 0.5 success rate.
	if got.TraceSuccessRate != 0.5 {
		t.Errorf("TraceSuccessRate = %v, want 0.5", got.TraceSuccessRate)
	}
	// 8 spans across 2 traces.
	if got.SpanPerTrace != 4.0 {
		t.Errorf("SpanPerTrace = %v, want 4.0", got.SpanPerTrace)
	}
	// 6 tool calls, 4 successful (3 file_read + 1 web_search).
	if got.TotalToolCalls != 6 {
		t.Errorf("TotalToolCalls = %d, want 6", got.TotalToolCalls)
	}
	if got.SuccessfulToolCalls != 4 {
		t.Errorf("SuccessfulToolCalls = %d, want 4", got.SuccessfulToolCalls)
	}
	if got.AvgToolSuccessRate != 4.0/6.0 {
		t.Errorf("AvgToolSuccessRate = %v, want %v", got.AvgToolSuccessRate, 4.0/6.0)
	}
	// 3 consecutive file_read then 3 consecutive web_search -> max loop 3.
	if got.MaxLoopDepth != 3 {
		t.Errorf("MaxLoopDepth = %d, want 3", got.MaxLoopDepth)
	}
	// web_search retried twice (ERROR,ERROR,OK); file_read had no retries so it
	// has no retry-count entry. Aggregate avg = sumRetries(2) / len(1) = 2.0 —
	// matches computeAgentStats semantics (see TestComputeAgentStatsRetryDetection).
	if got.AvgRetries != 2.0 {
		t.Errorf("AvgRetries = %v, want 2.0", got.AvgRetries)
	}

	// Per-tool breakdown.
	findTool := func(name string) *ToolUsageItem {
		for i := range got.ToolUsage {
			if got.ToolUsage[i].ToolName == name {
				return &got.ToolUsage[i]
			}
		}
		return nil
	}
	if fr := findTool("file_read"); fr == nil {
		t.Error("missing file_read in tool_usage")
	} else {
		if fr.CallCount != 3 || fr.SuccessRate != 1.0 || fr.MaxLoop != 3 || fr.AvgRetries != 0 {
			t.Errorf("file_read = %+v, want {CallCount:3 SuccessRate:1 MaxLoop:3 AvgRetries:0}", fr)
		}
	}
	if ws := findTool("web_search"); ws == nil {
		t.Error("missing web_search in tool_usage")
	} else {
		if ws.CallCount != 3 || ws.MaxLoop != 3 || ws.AvgRetries != 2.0 {
			t.Errorf("web_search = %+v, want {CallCount:3 MaxLoop:3 AvgRetries:2}", ws)
		}
		if ws.SuccessRate < 0.333 || ws.SuccessRate > 0.334 {
			t.Errorf("web_search SuccessRate = %v, want ~0.333", ws.SuccessRate)
		}
	}

	if len(got.Insights) == 0 {
		t.Error("expected at least one insight for loop/low-success scenario")
	}

	// Unknown / empty session -> (nil, nil) so the handler returns 404
	// no_agent_data and the UI hides the section (not a 500).
	got2, err := store.GetSessionAgentStats(ctx, "no-such-session")
	if err != nil {
		t.Errorf("GetSessionAgentStats(unknown): %v, want nil error", err)
	}
	if got2 != nil {
		t.Errorf("GetSessionAgentStats(unknown) = %+v, want nil", got2)
	}
}
