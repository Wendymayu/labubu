# Metrics Retention YAML Configuration

**Date:** 2026-06-05
**Status:** draft
**Scope:** Add YAML-configurable retention for metrics data, matching the trace retention pattern. Default retention: 1 day (24h).

## Motivation

Currently the metrics retention is set via a CLI flag `--metrics-retention` with a default of 2 hours. There is no YAML configuration for metrics. The user wants:

1. Default retention of 1 day (24 hours) instead of 2 hours
2. YAML-based configuration, matching how trace retention is configured
3. No CLI flag — YAML is the canonical configuration source

## Design

### 1. YAML Schema

Add a `metric` section to `labubu.yaml`, mirroring the existing `trace` section:

```yaml
trace:
  retention:
    max_age: 24h
    max_count: 10000
    cleanup_interval: 5m
metric:
  retention:
    max_age: 24h
```

`metric.retention.max_age` is the retention duration for metric data points. The tstorage library drops in-memory partitions when all their data points are older than this duration.

### 2. Go Structs (`internal/storage/config.go`)

Add to the existing config types, following the trace pattern:

```go
// Config — add new field
type Config struct {
    Trace  TraceConfig  `yaml:"trace"`
    Metric MetricConfig `yaml:"metric"`
}

// MetricConfig holds metric-specific configuration.
type MetricConfig struct {
    Retention MetricRetentionConfig `yaml:"retention"`
}

// MetricRetentionConfig controls how long metric data is retained.
type MetricRetentionConfig struct {
    MaxAge time.Duration // metrics older than this are dropped by tstorage
}

// yamlConfig — add new nested struct
type yamlConfig struct {
    // ... existing Trace ...
    Metric struct {
        Retention struct {
            MaxAge string `yaml:"max_age"`
        } `yaml:"retention"`
    } `yaml:"metric"`
}
```

**Default** (in `DefaultConfig()`):
```go
Metric: MetricConfig{
    Retention: MetricRetentionConfig{
        MaxAge: 24 * time.Hour,
    },
},
```

### 3. YAML Parsing (`LoadConfig`)

Add loading logic for the new fields, mirroring the trace parsing pattern:

```go
if raw.Metric.Retention.MaxAge != "" {
    if d, err := time.ParseDuration(raw.Metric.Retention.MaxAge); err == nil {
        cfg.Metric.Retention.MaxAge = d
    }
}
```

### 4. Wiring in `cmd/labubu/main.go`

Remove the `--metrics-retention` CLI flag. Read retention from the YAML config:

```go
// Remove this:
metricsRetention := fs.Duration("metrics-retention", 2*time.Hour, "tstorage retention duration")

// Change this:
ms, err := metrics.NewTStorageStore(metrics.TStorageConfig{
    DataDir:   *metricsDataDir,
    Retention: *metricsRetention,
})

// To this:
ms, err := metrics.NewTStorageStore(metrics.TStorageConfig{
    DataDir:   *metricsDataDir,
    Retention: cfg.Metric.Retention.MaxAge,
})
```

Add a startup banner line for metrics retention (next to the existing trace retention line):
```go
fmt.Printf("  Metric retention: max_age=%s\n", cfg.Metric.Retention.MaxAge)
```

### 5. What Does Not Change

- `TStorageConfig.Retention` already exists and is passed to `tstorage.WithRetention()` — no changes to `internal/metrics/tstorage_store.go`
- The `Store` interface in `internal/metrics/store.go` does not change — no `Purge` method needed
- No background cleanup goroutine — tstorage handles retention internally via `WithRetention()`
- The `--metrics-data-dir` and `--metrics-enabled` CLI flags remain unchanged
- The existing tests in `internal/metrics/tstorage_store_test.go` already pass `Retention: 1 * time.Hour` in `TStorageConfig`, so they continue to work without modification

### 6. Files Changed

| File | Change |
|------|--------|
| `internal/storage/config.go` | Add `MetricConfig`, `MetricRetentionConfig`, YAML parsing, defaults |
| `cmd/labubu/main.go` | Remove `--metrics-retention` flag, read from config, add banner line |

### 7. Edge Cases

- **Missing YAML file or missing `metric` section:** `DefaultConfig()` provides `max_age: 24h` — metrics work with the default retention.
- **Invalid `max_age` value (e.g., "xyz"):** `time.ParseDuration` fails silently; the default 24h is used (matches trace pattern).
- **`max_age: 0`:** An empty string `""` is treated as "not set", so the default applies. To disable retention entirely, this would need explicit handling — out of scope for now (tstorage's `WithRetention(0)` behavior would need testing).
- **Existing deployments using `--metrics-retention`:** The flag is removed. Users relying on it will need to set `metric.retention.max_age` in `labubu.yaml` instead. Print a deprecation note? No — this is a pre-1.0 product, breaking CLI changes are acceptable.

### 8. Testing

- **Unit tests:** Extend `internal/storage/config_test.go` with test cases for parsing `metric.retention.max_age` from YAML.
- **Integration:** Run `make test` to ensure existing tstorage tests still pass. The retention value in tests (`1 * time.Hour`) is set directly in code, so they are unaffected by config changes.
