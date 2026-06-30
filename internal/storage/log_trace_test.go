//go:build !local_engine

package storage

import (
	"context"
	"reflect"
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

func TestGetLogCountsByTrace(t *testing.T) {
	s, err := NewChDBStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	tid := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9}
	spanA := [8]byte{1}
	spanB := [8]byte{2}
	logs := []LogRecord{
		{TraceID: tid, SpanID: spanA, Timestamp: 100, Severity: "INFO", Body: "{}"},
		{TraceID: tid, SpanID: spanA, Timestamp: 200, Severity: "INFO", Body: "{}"},
		{TraceID: tid, SpanID: spanB, Timestamp: 150, Severity: "INFO", Body: "{}"},
	}
	if err := s.InsertLogs(ctx, logs); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	counts, err := s.GetLogCountsByTrace(ctx, tid)
	if err != nil {
		t.Fatalf("GetLogCountsByTrace: %v", err)
	}
	if counts[SpanIDToHex(spanA)] != 2 {
		t.Errorf("spanA count = %d, want 2", counts[SpanIDToHex(spanA)])
	}
	if counts[SpanIDToHex(spanB)] != 1 {
		t.Errorf("spanB count = %d, want 1", counts[SpanIDToHex(spanB)])
	}
	if len(counts) != 2 {
		t.Errorf("got %d spans, want 2", len(counts))
	}

	// Empty trace returns a non-nil empty map (handler relies on this).
	other := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 99}
	c2, err := s.GetLogCountsByTrace(ctx, other)
	if err != nil {
		t.Fatalf("GetLogCountsByTrace empty: %v", err)
	}
	if c2 == nil || len(c2) != 0 {
		t.Errorf("empty trace counts = %#v, want non-nil empty map", c2)
	}
}

func TestListLogsAscOrder(t *testing.T) {
	s, err := NewChDBStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewChDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()

	tid := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 8}
	spanA := [8]byte{1}
	if err := s.InsertLogs(ctx, []LogRecord{
		{TraceID: tid, SpanID: spanA, Timestamp: 300, Severity: "INFO", Body: "{}"},
		{TraceID: tid, SpanID: spanA, Timestamp: 100, Severity: "INFO", Body: "{}"},
		{TraceID: tid, SpanID: spanA, Timestamp: 200, Severity: "INFO", Body: "{}"},
	}); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	ts := func(logs []LogListItem) []uint64 {
		out := make([]uint64, len(logs))
		for i, l := range logs {
			out[i] = l.Timestamp
		}
		return out
	}

	// Default: newest-first (DESC).
	desc, err := s.ListLogs(ctx, LogQuery{Page: 1, PageSize: 100, TraceID: tid})
	if err != nil {
		t.Fatalf("ListLogs desc: %v", err)
	}
	if got := ts(desc.Logs); !reflect.DeepEqual(got, []uint64{300, 200, 100}) {
		t.Errorf("desc order = %v, want [300 200 100]", got)
	}

	// Asc: oldest-first (ASC).
	asc, err := s.ListLogs(ctx, LogQuery{Page: 1, PageSize: 100, TraceID: tid, Asc: true})
	if err != nil {
		t.Fatalf("ListLogs asc: %v", err)
	}
	if got := ts(asc.Logs); !reflect.DeepEqual(got, []uint64{100, 200, 300}) {
		t.Errorf("asc order = %v, want [100 200 300]", got)
	}
}
