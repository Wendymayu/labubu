//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
)

// insertTestTrace inserts a trace with one span via InsertSpans so the trace
// row is created through the normal ingestion path.
func insertTestTrace(t *testing.T, store Store, tid [16]byte) {
	t.Helper()
	var sid [8]byte
	sid[7] = tid[15]
	span := Span{
		TraceID: tid, SpanID: sid,
		Name: "test-span", Kind: 1,
		StartTimeMS: 1000, EndTimeMS: 2000, DurationMS: 1000,
		Attributes: map[string]string{},
	}
	res := ResourceInfo{Attributes: map[string]string{"service.name": "test"}}
	if err := store.InsertSpans(context.Background(), res, ScopeInfo{}, []Span{span}); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}
}

func TestSqliteDeleteTraces(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tid1 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	tid2 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}
	insertTestTrace(t, store, tid1)
	insertTestTrace(t, store, tid2)

	logs := []LogRecord{
		{TraceID: tid1, Timestamp: 100, Severity: "INFO", Body: "{}"},
		{TraceID: tid1, Timestamp: 200, Severity: "INFO", Body: "{}"},
		{TraceID: tid2, Timestamp: 300, Severity: "INFO", Body: "{}"},
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	deletedTraces, deletedLogs, err := store.DeleteTraces(ctx, [][16]byte{tid1})
	if err != nil {
		t.Fatalf("DeleteTraces: %v", err)
	}
	if deletedTraces != 1 {
		t.Errorf("deletedTraces: want 1, got %d", deletedTraces)
	}
	if deletedLogs != 2 {
		t.Errorf("deletedLogs: want 2, got %d", deletedLogs)
	}

	// tid1 gone, tid2 still present with its span and log.
	if _, err := store.GetTrace(ctx, tid1); err == nil {
		t.Error("tid1 should be deleted (GetTrace should error)")
	}
	if d, err := store.GetTrace(ctx, tid2); err != nil {
		t.Fatalf("GetTrace tid2: %v", err)
	} else if d == nil {
		t.Error("tid2 should still exist")
	}

	remainLogs, err := store.GetLogsByTrace(ctx, tid1)
	if err != nil {
		t.Fatalf("GetLogsByTrace tid1: %v", err)
	}
	if len(remainLogs) != 0 {
		t.Errorf("tid1 logs: want 0, got %d", len(remainLogs))
	}
	remainLogs2, err := store.GetLogsByTrace(ctx, tid2)
	if err != nil {
		t.Fatalf("GetLogsByTrace tid2: %v", err)
	}
	if len(remainLogs2) != 1 {
		t.Errorf("tid2 logs: want 1, got %d", len(remainLogs2))
	}
}

func TestSqliteDeleteTracesEmptyAndUnknown(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Empty input is a no-op.
	dt, dl, err := store.DeleteTraces(ctx, nil)
	if err != nil {
		t.Fatalf("DeleteTraces empty: %v", err)
	}
	if dt != 0 || dl != 0 {
		t.Errorf("empty: want 0/0, got %d/%d", dt, dl)
	}

	// Unknown ID is silently ignored.
	tid := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9}
	dt, dl, err = store.DeleteTraces(ctx, [][16]byte{tid})
	if err != nil {
		t.Fatalf("DeleteTraces unknown: %v", err)
	}
	if dt != 0 || dl != 0 {
		t.Errorf("unknown: want 0/0, got %d/%d", dt, dl)
	}
}

func TestSqliteDeleteTracesBatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tid1 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	tid2 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}
	tid3 := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}
	for _, tid := range [][16]byte{tid1, tid2, tid3} {
		insertTestTrace(t, store, tid)
	}
	if err := store.InsertLogs(ctx, []LogRecord{
		{TraceID: tid1, Timestamp: 1, Severity: "INFO", Body: "{}"},
		{TraceID: tid2, Timestamp: 1, Severity: "INFO", Body: "{}"},
	}); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	dt, dl, err := store.DeleteTraces(ctx, [][16]byte{tid1, tid3})
	if err != nil {
		t.Fatalf("DeleteTraces: %v", err)
	}
	if dt != 2 {
		t.Errorf("deletedTraces: want 2, got %d", dt)
	}
	if dl != 1 {
		t.Errorf("deletedLogs: want 1, got %d", dl)
	}

	list, err := store.ListTraces(ctx, TraceQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	if len(list.Traces) != 1 {
		t.Errorf("traces remaining: want 1, got %d", len(list.Traces))
	}
}
