# Trace Retention with YAML Config — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add automatic trace data cleanup based on time and count limits, configured via a YAML file, with a background goroutine enforcing retention policies.

**Architecture:** Extend the `Store` interface with a `Purge()` method. Both `memStore` (in-memory map/slice cleanup) and `chDBStore` (DELETE SQL) implement it. A new `internal/storage/config.go` handles YAML config loading with `gopkg.in/yaml.v3`. `main.go` loads config at startup and starts a retention cleanup goroutine that periodically calls `store.Purge()`.

**Tech Stack:** Go 1.19, `gopkg.in/yaml.v3`, existing Store interface + memStore/chDBStore

---

### Task 1: Config Types and Loading (TDD)

**Files:**
- Create: `internal/storage/config.go`
- Create: `internal/storage/config_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add yaml.v3 dependency**

Run:
```bash
go get gopkg.in/yaml.v3
```
Expected: `go.mod` and `go.sum` updated with `gopkg.in/yaml.v3`.

- [ ] **Step 2: Write the failing config tests**

Create `internal/storage/config_test.go`:

```go
package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefault(t *testing.T) {
	cfg := LoadConfig("/nonexistent/path/labubu.yaml")

	if cfg.Trace.Retention.MaxAge != 24*time.Hour {
		t.Errorf("MaxAge: want 24h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Trace.Retention.MaxCount != 10000 {
		t.Errorf("MaxCount: want 10000, got %d", cfg.Trace.Retention.MaxCount)
	}
	if cfg.Trace.Retention.CleanupInterval != 5*time.Minute {
		t.Errorf("CleanupInterval: want 5m, got %v", cfg.Trace.Retention.CleanupInterval)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labubu.yaml")
	err := os.WriteFile(path, []byte(`trace:
  retention:
    max_age: 48h
    max_count: 5000
    cleanup_interval: 10m
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(path)

	if cfg.Trace.Retention.MaxAge != 48*time.Hour {
		t.Errorf("MaxAge: want 48h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Trace.Retention.MaxCount != 5000 {
		t.Errorf("MaxCount: want 5000, got %d", cfg.Trace.Retention.MaxCount)
	}
	if cfg.Trace.Retention.CleanupInterval != 10*time.Minute {
		t.Errorf("CleanupInterval: want 10m, got %v", cfg.Trace.Retention.CleanupInterval)
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labubu.yaml")
	err := os.WriteFile(path, []byte("{{invalid yaml:::"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(path)

	// Should return defaults on parse error.
	if cfg.Trace.Retention.MaxAge != 24*time.Hour {
		t.Errorf("MaxAge: want default 24h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Trace.Retention.MaxCount != 10000 {
		t.Errorf("MaxCount: want default 10000, got %d", cfg.Trace.Retention.MaxCount)
	}
	if cfg.Trace.Retention.CleanupInterval != 5*time.Minute {
		t.Errorf("CleanupInterval: want default 5m, got %v", cfg.Trace.Retention.CleanupInterval)
	}
}

func TestLoadConfigPartialFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labubu.yaml")
	err := os.WriteFile(path, []byte(`trace:
  retention:
    max_age: 12h
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(path)

	if cfg.Trace.Retention.MaxAge != 12*time.Hour {
		t.Errorf("MaxAge: want 12h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Trace.Retention.MaxCount != 10000 {
		t.Errorf("MaxCount: want default 10000, got %d", cfg.Trace.Retention.MaxCount)
	}
	if cfg.Trace.Retention.CleanupInterval != 5*time.Minute {
		t.Errorf("CleanupInterval: want default 5m, got %v", cfg.Trace.Retention.CleanupInterval)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -v ./internal/storage/ -run TestLoadConfig`
Expected: FAIL — `LoadConfig` not defined.

- [ ] **Step 4: Implement config.go**

Create `internal/storage/config.go`:

```go
package storage

import (
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level Labubu configuration loaded from YAML.
type Config struct {
	Trace TraceConfig `yaml:"trace"`
}

// TraceConfig holds trace-specific configuration.
type TraceConfig struct {
	Retention RetentionConfig `yaml:"retention"`
}

// RetentionConfig controls how old trace data is cleaned up.
type RetentionConfig struct {
	MaxAge          time.Duration // traces older than this are deleted
	MaxCount        int           // keep only the newest MaxCount traces; 0 = unlimited
	CleanupInterval time.Duration // how often the cleanup goroutine runs
}

// yamlConfig mirrors Config but uses string fields for durations
// so that YAML values like "24h" are parsed via time.ParseDuration.
type yamlConfig struct {
	Trace struct {
		Retention struct {
			MaxAge          string `yaml:"max_age"`
			MaxCount        *int   `yaml:"max_count"`
			CleanupInterval string `yaml:"cleanup_interval"`
		} `yaml:"retention"`
	} `yaml:"trace"`
}

// DefaultConfig returns a Config with production defaults.
func DefaultConfig() Config {
	return Config{
		Trace: TraceConfig{
			Retention: RetentionConfig{
				MaxAge:          24 * time.Hour,
				MaxCount:        10000,
				CleanupInterval: 5 * time.Minute,
			},
		},
	}
}

// LoadConfig reads a YAML config file at path and returns a Config.
// If the file does not exist, DefaultConfig is returned silently.
// If the file contains invalid YAML, a warning is logged and defaults are returned.
func LoadConfig(path string) Config {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	var raw yamlConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		log.Printf("Warning: failed to parse %s: %v (using defaults)", path, err)
		return DefaultConfig()
	}

	if raw.Trace.Retention.MaxAge != "" {
		if d, err := time.ParseDuration(raw.Trace.Retention.MaxAge); err == nil {
			cfg.Trace.Retention.MaxAge = d
		}
	}
	if raw.Trace.Retention.MaxCount != nil {
		cfg.Trace.Retention.MaxCount = *raw.Trace.Retention.MaxCount
	}
	if raw.Trace.Retention.CleanupInterval != "" {
		if d, err := time.ParseDuration(raw.Trace.Retention.CleanupInterval); err == nil {
			cfg.Trace.Retention.CleanupInterval = d
		}
	}

	return cfg
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -v ./internal/storage/ -run TestLoadConfig`
Expected: All 4 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/storage/config.go internal/storage/config_test.go go.mod go.sum
git commit -m "feat: add YAML config loading for trace retention"
```

---

### Task 2: memStore Purge Implementation (TDD)

**Files:**
- Modify: `internal/storage/storage.go` (add Purge to Store interface)
- Modify: `internal/storage/memstore.go` (implement Purge)
- Modify: `internal/storage/chdb.go` (add Purge implementation for chDBStore)
- Create: `internal/storage/memstore_purge_test.go`

- [ ] **Step 1: Write the failing Purge tests**

Create `internal/storage/memstore_purge_test.go`:

```go
//go:build !cgo || !local_engine

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/storage/ -run TestPurge`
Expected: FAIL — `Purge` method not found on `*memStore`, and `Store` interface doesn't include `Purge`.

- [ ] **Step 3: Add Purge to the Store interface**

In `internal/storage/storage.go`, add `time` to imports and add the Purge method to the `Store` interface:

```go
import (
	"context"
	"fmt"
	"time"
)
```

Add after `Close() error` in the `Store` interface:

```go
	// Purge removes traces (and their spans) that exceed the retention policy.
	// maxAge: delete traces with start_time_ms older than (now - maxAge). 0 = no age limit.
	// maxCount: keep only the newest maxCount traces. 0 = no count limit.
	// Returns the number of deleted traces and spans.
	Purge(ctx context.Context, maxAge time.Duration, maxCount int) (deletedTraces int, deletedSpans int, err error)
```

- [ ] **Step 4: Implement Purge for memStore**

In `internal/storage/memstore.go`, add `"time"` to the imports and add this method after `Close()`:

```go
func (m *memStore) Purge(ctx context.Context, maxAge time.Duration, maxCount int) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := uint64(time.Now().UnixMilli())
	cutoffMS := uint64(0)
	if maxAge > 0 {
		cutoffMS = now - uint64(maxAge.Milliseconds())
	}

	deletedTraces := 0
	deletedSpans := 0

	// Phase 1: collect trace IDs to keep based on age.
	keepTraces := make(map[[16]byte]bool)
	for traceID, trace := range m.traces {
		if cutoffMS > 0 && trace.StartTimeMS < cutoffMS {
			continue // too old, skip
		}
		keepTraces[traceID] = true
	}

	// Phase 2: if maxCount > 0, further restrict to newest maxCount traces.
	if maxCount > 0 && len(keepTraces) > maxCount {
		type timedTrace struct {
			id    [16]byte
			start uint64
		}
		sorted := make([]timedTrace, 0, len(keepTraces))
		for id := range keepTraces {
			sorted = append(sorted, timedTrace{id, m.traces[id].StartTimeMS})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].start > sorted[j].start
		})
		keepTraces = make(map[[16]byte]bool)
		for _, tv := range sorted[:maxCount] {
			keepTraces[tv.id] = true
		}
	}

	// Delete traces not in keepTraces.
	for id := range m.traces {
		if !keepTraces[id] {
			delete(m.traces, id)
			deletedTraces++
		}
	}

	// Delete spans belonging to deleted traces.
	newSpans := make([]Span, 0, len(m.spans))
	for _, s := range m.spans {
		if keepTraces[s.TraceID] {
			newSpans = append(newSpans, s)
		} else {
			deletedSpans++
		}
	}
	m.spans = newSpans

	return deletedTraces, deletedSpans, nil
}
```

- [ ] **Step 5: Add Purge implementation to chDBStore**

In `internal/storage/chdb.go`, add `"time"` to the imports and add this method after `Close()`:

```go
func (s *chDBStore) Purge(ctx context.Context, maxAge time.Duration, maxCount int) (int, int, error) {
	now := uint64(time.Now().UnixMilli())

	// Get current counts.
	countResult, err := s.querySQL("SELECT count(*) AS count FROM traces FORMAT JSONEachRow")
	if err != nil {
		return 0, 0, fmt.Errorf("count traces: %w", err)
	}
	traceCountBefore := parseCount(countResult)

	spanCountResult, err := s.querySQL("SELECT count(*) AS count FROM spans FORMAT JSONEachRow")
	if err != nil {
		return 0, 0, fmt.Errorf("count spans: %w", err)
	}
	spanCountBefore := parseCount(spanCountResult)

	// Phase 1: delete by age.
	if maxAge > 0 {
		cutoffMS := now - uint64(maxAge.Milliseconds())
		if err := s.execSQL(fmt.Sprintf(
			"ALTER TABLE traces DELETE WHERE start_time_ms < %d", cutoffMS)); err != nil {
			return 0, 0, fmt.Errorf("delete old traces: %w", err)
		}
		if err := s.execSQL(fmt.Sprintf(
			"ALTER TABLE spans DELETE WHERE trace_id IN (SELECT trace_id FROM traces WHERE start_time_ms < %d)", cutoffMS)); err != nil {
			return 0, 0, fmt.Errorf("delete old spans: %w", err)
		}
	}

	// Phase 2: delete by count (keep newest maxCount).
	if maxCount > 0 {
		// Re-count after age deletion.
		countResult2, err := s.querySQL("SELECT count(*) AS count FROM traces FORMAT JSONEachRow")
		if err != nil {
			return 0, 0, fmt.Errorf("recount traces: %w", err)
		}
		currentCount := parseCount(countResult2)
		if currentCount > maxCount {
			if err := s.execSQL(fmt.Sprintf(
				"ALTER TABLE spans DELETE WHERE trace_id NOT IN (SELECT trace_id FROM traces ORDER BY start_time_ms DESC LIMIT %d)", maxCount)); err != nil {
				return 0, 0, fmt.Errorf("delete excess spans: %w", err)
			}
			if err := s.execSQL(fmt.Sprintf(
				"ALTER TABLE traces DELETE WHERE trace_id NOT IN (SELECT trace_id FROM traces ORDER BY start_time_ms DESC LIMIT %d)", maxCount)); err != nil {
				return 0, 0, fmt.Errorf("delete excess traces: %w", err)
			}
		}
	}

	// Estimate deletions (MergeTree mutations are async, exact counts unavailable).
	traceCountAfter, _ := s.querySQL("SELECT count(*) AS count FROM traces FORMAT JSONEachRow")
	spanCountAfter, _ := s.querySQL("SELECT count(*) AS count FROM spans FORMAT JSONEachRow")
	tracesAfter := parseCount(traceCountAfter)
	spansAfter := parseCount(spanCountAfter)

	deletedTraces := traceCountBefore - tracesAfter
	if deletedTraces < 0 {
		deletedTraces = 0
	}
	deletedSpans := spanCountBefore - spansAfter
	if deletedSpans < 0 {
		deletedSpans = 0
	}

	return deletedTraces, deletedSpans, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -v ./internal/storage/ -run TestPurge`
Expected: All 6 tests PASS.

- [ ] **Step 7: Run full storage tests**

Run: `go test -v ./internal/storage/`
Expected: All tests PASS (config tests + purge tests).

- [ ] **Step 8: Commit**

```bash
git add internal/storage/storage.go internal/storage/memstore.go internal/storage/chdb.go internal/storage/memstore_purge_test.go
git commit -m "feat: add Purge method to Store interface with memStore and chDBStore implementations"
```

---

### Task 3: main.go Integration — Config Flag and Retention Goroutine

**Files:**
- Modify: `cmd/labubu/main.go`
- Create: `cmd/labubu/main_config_test.go`

- [ ] **Step 1: Write the integration test**

Create `cmd/labubu/main_config_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

func TestLoadConfigIntegration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labubu.yaml")
	err := os.WriteFile(path, []byte(`trace:
  retention:
    max_age: 12h
    max_count: 500
    cleanup_interval: 1m
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := storage.LoadConfig(path)

	if cfg.Trace.Retention.MaxAge.Hours() != 12 {
		t.Errorf("MaxAge: want 12h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Trace.Retention.MaxCount != 500 {
		t.Errorf("MaxCount: want 500, got %d", cfg.Trace.Retention.MaxCount)
	}
	if cfg.Trace.Retention.CleanupInterval.Minutes() != 1 {
		t.Errorf("CleanupInterval: want 1m, got %v", cfg.Trace.Retention.CleanupInterval)
	}
}
```

- [ ] **Step 2: Run test to verify it passes (config loading already works from Task 1)**

Run: `go test -v ./cmd/labubu/ -run TestLoadConfigIntegration`
Expected: PASS.

- [ ] **Step 3: Modify runServe to load config and start retention goroutine**

In `cmd/labubu/main.go`:

Add `"time"` import (already present) and ensure `storage` is imported (already present).

In `runServe()`, add `--config` flag after the existing flags (around line 84):

```go
	configPath := fs.String("config", "labubu.yaml", "config file path")
```

After `fs.Parse(args)` (line 86), add config loading:

```go
	// Load YAML config.
	cfg := storage.LoadConfig(*configPath)
```

After the storage initialization and storage banner (after line 115, `fmt.Println()`), add retention banner:

```go
	fmt.Printf("  Trace retention:  max_age=%s, max_count=%d, cleanup=%s\n",
		cfg.Trace.Retention.MaxAge, cfg.Trace.Retention.MaxCount, cfg.Trace.Retention.CleanupInterval)
	fmt.Println()
```

After store initialization (after `defer store.Close()`, around line 123), add retention cleanup goroutine:

```go
	// Start retention cleanup goroutine.
	retentionCtx, retentionCancel := context.WithCancel(context.Background())
	defer retentionCancel()
	go runRetentionCleanup(retentionCtx, store, cfg.Trace.Retention)
```

Add the `runRetentionCleanup` function at the end of the file (after `runServe`):

```go
func runRetentionCleanup(ctx context.Context, store storage.Store, ret storage.RetentionConfig) {
	ticker := time.NewTicker(ret.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			deleted, spans, err := store.Purge(ctx, ret.MaxAge, ret.MaxCount)
			if err != nil {
				log.Printf("Trace cleanup error: %v", err)
			} else if deleted > 0 {
				log.Printf("Trace cleanup: removed %d traces, %d spans", deleted, spans)
			}
		case <-ctx.Done():
			return
		}
	}
}
```

- [ ] **Step 4: Verify the build compiles**

Run: `go build -tags dev ./cmd/labubu`
Expected: No errors.

- [ ] **Step 5: Run all cmd tests**

Run: `go test -v ./cmd/labubu/`
Expected: All tests PASS (existing 4 + new 1).

- [ ] **Step 6: Commit**

```bash
git add cmd/labubu/main.go cmd/labubu/main_config_test.go
git commit -m "feat: integrate YAML config and retention cleanup goroutine into serve command"
```

---

### Task 4: Final Verification

- [ ] **Step 1: Run all Go tests**

Run: `go test -v ./internal/... ./cmd/...`
Expected: All tests PASS.

- [ ] **Step 2: Verify the binary builds and runs**

Run:
```bash
go build -tags dev -o /tmp/labubu ./cmd/labubu
```

Create a test config:
```bash
cat > /tmp/test-labubu.yaml << 'EOF'
trace:
  retention:
    max_age: 48h
    max_count: 5000
    cleanup_interval: 2m
EOF
```

Run with the config (verify banner output, then Ctrl+C):
```bash
/tmp/labubu serve --config /tmp/test-labubu.yaml --port 19999
```
Expected output includes:
```
  Trace retention:  max_age=48h0m0s, max_count=5000, cleanup=2m0s
```

- [ ] **Step 3: Run TypeScript check (ensure frontend not broken)**

Run: `cd web && npx vue-tsc --noEmit`
Expected: No errors.

- [ ] **Step 4: Commit any final cleanup (if needed)**

```bash
git add -A
git commit -m "chore: final cleanup for trace retention feature"
```

(Only if there are changes to commit.)
