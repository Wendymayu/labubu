//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"testing"
	"time"
)

func TestSqlitePurgeLogsByAge(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := uint64(time.Now().UnixMilli())
	tid := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9}
	logs := []LogRecord{
		{TraceID: tid, Timestamp: now - 2 * 3600 * 1000, Severity: "INFO", Body: "{}"}, // 2h ago, should be purged
		{TraceID: tid, Timestamp: now - 60 * 1000, Severity: "INFO", Body: "{}"},       // 1m ago, should remain
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	deleted, err := store.PurgeLogs(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("PurgeLogs: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted: want 1, got %d", deleted)
	}

	res, err := store.ListLogs(ctx, LogQuery{Page: 1, PageSize: 100, TraceID: tid})
	if err != nil {
		t.Fatalf("ListLogs: %v", err)
	}
	if len(res.Logs) != 1 {
		t.Errorf("logs remaining: want 1, got %d", len(res.Logs))
	}
}

func TestSqlitePurgeLogsZeroAgeNoOp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tid := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 8}
	if err := store.InsertLogs(ctx, []LogRecord{
		{TraceID: tid, Timestamp: 1, Severity: "INFO", Body: "{}"},
	}); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	deleted, err := store.PurgeLogs(ctx, 0)
	if err != nil {
		t.Fatalf("PurgeLogs: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted: want 0, got %d", deleted)
	}

	res, err := store.ListLogs(ctx, LogQuery{Page: 1, PageSize: 100, TraceID: tid})
	if err != nil {
		t.Fatalf("ListLogs: %v", err)
	}
	if len(res.Logs) != 1 {
		t.Errorf("logs remaining: want 1, got %d", len(res.Logs))
	}
}
