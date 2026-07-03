//go:build !local_engine && nosqlite

package storage

import (
	"context"
	"testing"
	"time"
)

// makeTestTrace creates a Trace with the given ID byte, start time, and 3 spans.
func makeTestTrace(id byte, startTimeMS uint64) (Trace, []Span) {
	traceID := [16]byte{id}
	spanID := [8]byte{id, 1}
	trace := Trace{
		TraceID:       traceID,
		TraceIDHex:    TraceIDToHex(traceID),
		RootSpanID:    spanID,
		RootName:      "test-root",
		SpanCount:     3,
		StartTimeMS:   startTimeMS,
		EndTimeMS:     startTimeMS + 1000,
		DurationMS:    1000,
		ResourceAttrs: map[string]string{"service.name": "test"},
	}
	spans := []Span{
		{
			TraceID:     traceID,
			SpanID:      spanID,
			Name:        "root",
			StartTimeMS: startTimeMS,
			EndTimeMS:   startTimeMS + 1000,
			DurationMS:  1000,
			Attributes:  map[string]string{},
		},
		{
			TraceID:      traceID,
			SpanID:       [8]byte{id, 2},
			ParentSpanID: spanID,
			Name:         "child-1",
			StartTimeMS:  startTimeMS + 100,
			EndTimeMS:    startTimeMS + 500,
			DurationMS:   400,
			Attributes:   map[string]string{},
		},
		{
			TraceID:      traceID,
			SpanID:       [8]byte{id, 3},
			ParentSpanID: spanID,
			Name:         "child-2",
			StartTimeMS:  startTimeMS + 200,
			EndTimeMS:    startTimeMS + 800,
			DurationMS:   600,
			Attributes:   map[string]string{},
		},
	}
	return trace, spans
}

func TestPurgeByAge(t *testing.T) {
	store, err := NewChDBStore("")
	if err != nil {
		t.Fatal(err)
	}
	ms := store.(*memStore)

	now := uint64(time.Now().UnixMilli())
	oldTime := now - 2*3600*1000 // 2 hours ago
	newTime := now - 60*1000      // 1 minute ago

	trace1, spans1 := makeTestTrace(1, oldTime)
	trace2, spans2 := makeTestTrace(2, newTime)
	ms.traces[trace1.TraceID] = trace1
	ms.spans = append(ms.spans, spans1...)
	ms.traces[trace2.TraceID] = trace2
	ms.spans = append(ms.spans, spans2...)

	deletedTraces, deletedSpans, err := ms.Purge(context.Background(), 1*time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	if deletedTraces != 1 {
		t.Errorf("deletedTraces: want 1, got %d", deletedTraces)
	}
	if deletedSpans != 3 {
		t.Errorf("deletedSpans: want 3, got %d", deletedSpans)
	}
	if len(ms.traces) != 1 {
		t.Errorf("traces remaining: want 1, got %d", len(ms.traces))
	}
	if len(ms.spans) != 3 {
		t.Errorf("spans remaining: want 3, got %d", len(ms.spans))
	}
	if _, ok := ms.traces[trace2.TraceID]; !ok {
		t.Error("new trace should have been kept")
	}
}

func TestPurgeByCount(t *testing.T) {
	store, err := NewChDBStore("")
	if err != nil {
		t.Fatal(err)
	}
	ms := store.(*memStore)

	now := uint64(time.Now().UnixMilli())
	// Insert 5 traces with increasing start times (trace 5 is newest).
	for i := byte(1); i <= 5; i++ {
		trace, spans := makeTestTrace(i, now-uint64(6-i)*60*1000)
		ms.traces[trace.TraceID] = trace
		ms.spans = append(ms.spans, spans...)
	}

	deletedTraces, deletedSpans, err := ms.Purge(context.Background(), 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if deletedTraces != 2 {
		t.Errorf("deletedTraces: want 2, got %d", deletedTraces)
	}
	if deletedSpans != 6 {
		t.Errorf("deletedSpans: want 6, got %d", deletedSpans)
	}
	if len(ms.traces) != 3 {
		t.Errorf("traces remaining: want 3, got %d", len(ms.traces))
	}
	// Newest 3 (IDs 3, 4, 5) should remain.
	for _, id := range []byte{3, 4, 5} {
		if _, ok := ms.traces[[16]byte{id}]; !ok {
			t.Errorf("trace %d should have been kept", id)
		}
	}
}

func TestPurgeByAgeAndCount(t *testing.T) {
	store, err := NewChDBStore("")
	if err != nil {
		t.Fatal(err)
	}
	ms := store.(*memStore)

	now := uint64(time.Now().UnixMilli())
	// 4 traces: 1 very old, 3 recent.
	trace1, spans1 := makeTestTrace(1, now-48*3600*1000) // 48h ago
	ms.traces[trace1.TraceID] = trace1
	ms.spans = append(ms.spans, spans1...)
	for i := byte(2); i <= 4; i++ {
		trace, spans := makeTestTrace(i, now-uint64(5-i)*60*1000)
		ms.traces[trace.TraceID] = trace
		ms.spans = append(ms.spans, spans...)
	}

	// maxAge=24h removes trace 1, maxCount=2 keeps only 2 of the remaining 3.
	deletedTraces, _, err := ms.Purge(context.Background(), 24*time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deletedTraces != 2 {
		t.Errorf("deletedTraces: want 2, got %d", deletedTraces)
	}
	if len(ms.traces) != 2 {
		t.Errorf("traces remaining: want 2, got %d", len(ms.traces))
	}
}

func TestPurgeWithZeroCount(t *testing.T) {
	store, err := NewChDBStore("")
	if err != nil {
		t.Fatal(err)
	}
	ms := store.(*memStore)

	now := uint64(time.Now().UnixMilli())
	// 5 traces, all recent.
	for i := byte(1); i <= 5; i++ {
		trace, spans := makeTestTrace(i, now-uint64(i)*60*1000)
		ms.traces[trace.TraceID] = trace
		ms.spans = append(ms.spans, spans...)
	}

	// maxCount=0 means unlimited, so nothing should be deleted.
	deletedTraces, _, err := ms.Purge(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if deletedTraces != 0 {
		t.Errorf("deletedTraces: want 0 (unlimited), got %d", deletedTraces)
	}
	if len(ms.traces) != 5 {
		t.Errorf("traces remaining: want 5, got %d", len(ms.traces))
	}
}

func TestPurgeEmptyStore(t *testing.T) {
	store, err := NewChDBStore("")
	if err != nil {
		t.Fatal(err)
	}
	ms := store.(*memStore)

	deletedTraces, deletedSpans, err := ms.Purge(context.Background(), 24*time.Hour, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if deletedTraces != 0 {
		t.Errorf("deletedTraces: want 0, got %d", deletedTraces)
	}
	if deletedSpans != 0 {
		t.Errorf("deletedSpans: want 0, got %d", deletedSpans)
	}
}

func TestPurgeWithZeroAge(t *testing.T) {
	store, err := NewChDBStore("")
	if err != nil {
		t.Fatal(err)
	}
	ms := store.(*memStore)

	now := uint64(time.Now().UnixMilli())
	// 4 traces of varying age.
	trace1, spans1 := makeTestTrace(1, now-72*3600*1000) // 72h ago
	ms.traces[trace1.TraceID] = trace1
	ms.spans = append(ms.spans, spans1...)
	for i := byte(2); i <= 4; i++ {
		trace, spans := makeTestTrace(i, now-uint64(5-i)*3600*1000)
		ms.traces[trace.TraceID] = trace
		ms.spans = append(ms.spans, spans...)
	}

	// maxAge=0 means no age limit, only count applies. Keep newest 2.
	deletedTraces, _, err := ms.Purge(context.Background(), 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deletedTraces != 2 {
		t.Errorf("deletedTraces: want 2, got %d", deletedTraces)
	}
	if len(ms.traces) != 2 {
		t.Errorf("traces remaining: want 2, got %d", len(ms.traces))
	}
}

func TestPurgeLogsByAge(t *testing.T) {
	store, err := NewChDBStore("")
	if err != nil {
		t.Fatal(err)
	}
	ms := store.(*memStore)

	now := uint64(time.Now().UnixMilli())
	oldLog := LogRecord{Timestamp: now - 2*3600*1000, Severity: "INFO", Body: "{}"} // 2h ago
	newLog := LogRecord{Timestamp: now - 60*1000, Severity: "INFO", Body: "{}"}    // 1m ago
	ms.logs = append(ms.logs, oldLog, newLog)

	deleted, err := ms.PurgeLogs(context.Background(), 1*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("deleted: want 1, got %d", deleted)
	}
	if len(ms.logs) != 1 {
		t.Errorf("logs remaining: want 1, got %d", len(ms.logs))
	}
}

func TestPurgeLogsZeroAgeNoOp(t *testing.T) {
	store, err := NewChDBStore("")
	if err != nil {
		t.Fatal(err)
	}
	ms := store.(*memStore)

	ms.logs = append(ms.logs, LogRecord{Timestamp: 1, Severity: "INFO", Body: "{}"})

	deleted, err := ms.PurgeLogs(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Errorf("deleted: want 0, got %d", deleted)
	}
	if len(ms.logs) != 1 {
		t.Errorf("logs remaining: want 1, got %d", len(ms.logs))
	}
}
