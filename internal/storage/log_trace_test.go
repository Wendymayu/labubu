//go:build !local_engine

package storage

import (
	"context"
	"testing"
)

func TestListLogsFiltersBySpanID(t *testing.T) {
	s, err := NewChDBStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	tid := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 7}
	spanA := [8]byte{1}
	spanB := [8]byte{2}
	logs := []LogRecord{
		{TraceID: tid, SpanID: spanA, Timestamp: 100, Severity: "INFO", EventName: "a1", Body: "{}"},
		{TraceID: tid, SpanID: spanA, Timestamp: 200, Severity: "INFO", EventName: "a2", Body: "{}"},
		{TraceID: tid, SpanID: spanB, Timestamp: 150, Severity: "INFO", EventName: "b1", Body: "{}"},
	}
	if err := s.InsertLogs(ctx, logs); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	res, err := s.ListLogs(ctx, LogQuery{Page: 1, PageSize: 100, TraceID: tid, SpanID: spanA})
	if err != nil {
		t.Fatalf("ListLogs: %v", err)
	}
	if len(res.Logs) != 2 {
		t.Fatalf("got %d logs, want 2 (spanA only)", len(res.Logs))
	}
	for _, l := range res.Logs {
		if l.SpanIDHex != SpanIDToHex(spanA) {
			t.Errorf("got span %s, want only spanA", l.SpanIDHex)
		}
	}
	if res.Pagination.Total != 2 {
		t.Errorf("total = %d, want 2", res.Pagination.Total)
	}
}
