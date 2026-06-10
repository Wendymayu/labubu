# Trace Persistence with YAML-Configured Retention

> **Date:** 2026-06-05
> **Status:** Approved
> **Roadmap:** #14 — Trace 支持持久化，只保留一天或 1 万条，支持 YAML 配置

## Goal

Enable trace data to be persisted to disk (via chDB) and automatically cleaned up based on time and count limits configured through a YAML file.

## Background

Currently, Labubu has two trace storage implementations:

1. **memStore** (default, `CGO_ENABLED=0`): Pure in-memory, all data lost on restart.
2. **chDBStore** (`CGO && local_engine`): Embedded ClickHouse, supports persistent storage via `--data-dir` flag.

Neither implementation has automatic data cleanup. As traces accumulate over time, the dataset grows unbounded. This spec adds retention policies (max age + max count) with a YAML configuration file, and a background goroutine to enforce them.

## Architecture

```
┌─────────────────────────────────────────────────┐
│                    main.go                       │
│                                                  │
│  loadConfig(path) → Config                       │
│         │                                        │
│         ▼                                        │
│  store = NewChDBStore(dataDir)                   │
│  go runRetentionCleanup(ctx, store, cfg)         │
│         │                                        │
│         ▼  (every cleanup_interval)              │
│  store.Purge(ctx, maxAge, maxCount)              │
│         │                                        │
│         ├── chDBStore:  DELETE SQL               │
│         └── memStore:   map/slice cleanup        │
└─────────────────────────────────────────────────┘
```

## 1. YAML Configuration File

### File location

- Default path: `./labubu.yaml` (current working directory)
- Override with `--config <path>` CLI flag
- If file does not exist, use built-in defaults silently (no error)

### Format

```yaml
# labubu.yaml — Labubu 配置文件
trace:
  retention:
    max_age: 24h          # 保留时间，默认 24h（1天）
    max_count: 10000      # 最大保留条数，默认 10000
    cleanup_interval: 5m  # 清理间隔，默认 5m
```

### Field definitions

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_age` | Go duration string | `24h` | Traces older than this are deleted. Supports `h`, `m`, `s` suffixes. |
| `max_count` | int | `10000` | Maximum number of traces to keep (newest first). `0` = unlimited. |
| `cleanup_interval` | Go duration string | `5m` | How often the cleanup goroutine runs. |

### Config loading logic

```go
type Config struct {
    Trace TraceConfig `yaml:"trace"`
}

type TraceConfig struct {
    Retention RetentionConfig `yaml:"retention"`
}

type RetentionConfig struct {
    MaxAge          time.Duration `yaml:"max_age"`
    MaxCount        int           `yaml:"max_count"`
    CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

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

func loadConfig(path string) Config {
    cfg := DefaultConfig()
    data, err := os.ReadFile(path)
    if err != nil {
        return cfg  // file not found → silent defaults
    }
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        log.Printf("Warning: failed to parse %s: %v (using defaults)", path, err)
        return DefaultConfig()
    }
    return cfg
}
```

### YAML library

Use `gopkg.in/yaml.v3` (widely used, stable, supports Go duration strings via custom unmarshaling if needed).

## 2. Store Layer: Purge Method

### Store interface extension

Add one method to the `Store` interface:

```go
// Purge removes traces (and their spans) that exceed the retention policy.
// maxAge: delete traces with start_time_ms older than (now - maxAge). Duration of 0 means no age limit.
// maxCount: keep only the newest maxCount traces. 0 means no count limit.
// Returns the number of deleted traces and spans.
Purge(ctx context.Context, maxAge time.Duration, maxCount int) (deletedTraces int, deletedSpans int, err error)
```

### chDBStore implementation

Two-phase SQL execution:

**Phase 1 — Delete by age:**
```sql
ALTER TABLE traces DELETE WHERE start_time_ms < {cutoff_ms};
ALTER TABLE spans DELETE WHERE trace_id IN (
    SELECT trace_id FROM traces WHERE start_time_ms < {cutoff_ms}
);
```

**Phase 2 — Delete by count (keep newest max_count):**
```sql
ALTER TABLE spans DELETE WHERE trace_id NOT IN (
    SELECT trace_id FROM traces ORDER BY start_time_ms DESC LIMIT {max_count}
);
ALTER TABLE traces DELETE WHERE trace_id NOT IN (
    SELECT trace_id FROM traces ORDER BY start_time_ms DESC LIMIT {max_count}
);
```

**Return values:** Count deleted traces/spans by querying `count()` before and after the DELETE operations.

### memStore implementation

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

    // Collect trace IDs to keep (by age)
    keepTraces := make(map[[16]byte]bool)
    for traceID, trace := range m.traces {
        if cutoffMS > 0 && trace.StartTimeMS < cutoffMS {
            continue  // too old, don't keep
        }
        keepTraces[traceID] = true
    }

    // If maxCount > 0, further restrict to newest maxCount
    if maxCount > 0 && len(keepTraces) > maxCount {
        // Sort by start_time_ms descending, keep only maxCount
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

    // Delete traces not in keepTraces
    for id := range m.traces {
        if !keepTraces[id] {
            delete(m.traces, id)
            deletedTraces++
        }
    }

    // Delete spans belonging to deleted traces
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

## 3. main.go Integration

### New CLI flag

```go
configPath := fs.String("config", "labubu.yaml", "config file path")
```

### Retention goroutine

```go
func runRetentionCleanup(ctx context.Context, store storage.Store, cfg RetentionConfig) {
    ticker := time.NewTicker(cfg.CleanupInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            deleted, spans, err := store.Purge(ctx, cfg.MaxAge, cfg.MaxCount)
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

**Lifecycle:**
- Started after `store` is initialized
- Uses a cancellable context derived from a `context.WithCancel`
- Cancelled during shutdown, goroutine exits cleanly

### Startup banner addition

```
  Trace retention:  max_age=24h, max_count=10000, cleanup=5m
```

Printed after the storage line, regardless of which store implementation is active.

## 4. File Structure

| File | Action | Description |
|------|--------|-------------|
| `internal/storage/storage.go` | Modify | Add `Purge` method to `Store` interface |
| `internal/storage/chdb.go` | Modify | Implement `Purge` for chDBStore (DELETE SQL) |
| `internal/storage/memstore.go` | Modify | Implement `Purge` for memStore (map/slice cleanup) |
| `internal/storage/config.go` | Create | `Config`, `RetentionConfig`, `loadConfig()` |
| `internal/storage/config_test.go` | Create | Tests for config loading |
| `internal/storage/memstore_purge_test.go` | Create | Tests for memStore Purge |
| `internal/storage/chdb_purge_test.go` | Create | Tests for chDBStore Purge (CGO-only, build tag) |
| `cmd/labubu/main.go` | Modify | Add `--config` flag, load config, start retention goroutine |
| `go.mod` / `go.sum` | Modify | Add `gopkg.in/yaml.v3` dependency |

## 5. Testing

### memStore Purge tests (pure Go, always runs)

| Test | Scenario |
|------|----------|
| `TestPurgeByAge` | Insert old trace + new trace, Purge with maxAge, verify only new remains |
| `TestPurgeByCount` | Insert 5 traces, Purge with maxCount=3, verify 2 oldest deleted |
| `TestPurgeByAgeAndCount` | Both limits active, verify both enforced |
| `TestPurgeWithZeroCount` | maxCount=0 means no count limit, only age applies |
| `TestPurgeEmptyStore` | Purge on empty store returns (0, 0, nil) |
| `TestPurgeWithZeroAge` | maxAge=0 means no age limit, only count applies |

### Config loading tests

| Test | Scenario |
|------|----------|
| `TestLoadConfigDefault` | File doesn't exist → returns DefaultConfig() |
| `TestLoadConfigFromFile` | Valid YAML → fields parsed correctly |
| `TestLoadConfigInvalidYAML` | Malformed YAML → falls back to defaults, logs warning |
| `TestLoadConfigPartialFields` | Only `max_age` set → other fields get defaults |

### main.go integration test

| Test | Scenario |
|------|----------|
| `TestServeWithConfigFlag` | `--config testdata/test.yaml` → verify config loaded (banner output) |

### chDBStore Purge tests (CGO-only)

Same scenarios as memStore, but with `//go:build cgo && local_engine` tag and actual chDB SQL verification.

## 6. Non-Goals

- **Metric retention**: Already handled by tstorage's built-in `--metrics-retention` flag. Out of scope for this spec (tracked as roadmap item #15 separately).
- **Config for non-trace settings**: Port, buffer-size, log-level etc. remain CLI flags only. YAGNI.
- **Config hot-reload**: Config is loaded once at startup. Restart to apply changes.
- **chDB ALTER TABLE DELETE performance**: MergeTree's ALTER TABLE DELETE is async and efficient for background cleanup. No optimization needed for the 10K-trace scale.
