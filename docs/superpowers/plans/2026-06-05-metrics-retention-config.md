# Metrics Retention YAML Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add YAML-configurable metrics retention (`metric.retention.max_age`) defaulting to 24h, remove the `--metrics-retention` CLI flag.

**Architecture:** Add `MetricConfig`/`MetricRetentionConfig` types to the existing config system, mirroring the trace pattern. Wire into `main.go` by reading from YAML config instead of CLI flag. tstorage's built-in `WithRetention()` handles the actual data cleanup — no background goroutine needed.

**Tech Stack:** Go 1.19, `gopkg.in/yaml.v3`, tstorage

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/storage/config.go` | Config types, YAML parsing, defaults — add `MetricConfig` + `MetricRetentionConfig` |
| `internal/storage/config_test.go` | Unit tests for config parsing — extend existing tests with metric fields |
| `cmd/labubu/main.go` | CLI entry point — remove `--metrics-retention` flag, read from `cfg.Metric.Retention.MaxAge`, add banner |

---

### Task 1: Add metric config types and YAML parsing

**Files:**
- Modify: `internal/storage/config.go`

- [ ] **Step 1: Add `MetricConfig` and `MetricRetentionConfig` types**

Add these types after the existing `RetentionConfig` type (after line 25):

```go
// MetricConfig holds metric-specific configuration.
type MetricConfig struct {
	Retention MetricRetentionConfig `yaml:"retention"`
}

// MetricRetentionConfig controls how long metric data is retained.
type MetricRetentionConfig struct {
	MaxAge time.Duration // metrics older than this are dropped by tstorage
}
```

- [ ] **Step 2: Add `Metric` field to `Config` struct**

Change the `Config` struct (lines 12-14) from:

```go
type Config struct {
	Trace TraceConfig `yaml:"trace"`
}
```

To:

```go
type Config struct {
	Trace  TraceConfig  `yaml:"trace"`
	Metric MetricConfig `yaml:"metric"`
}
```

- [ ] **Step 3: Add `Metric` section to `yamlConfig`**

Insert after the `Trace` block inside `yamlConfig` (after line 38), before the closing `}` of `yamlConfig`:

```go
	Metric struct {
		Retention struct {
			MaxAge string `yaml:"max_age"`
		} `yaml:"retention"`
	} `yaml:"metric"`
```

- [ ] **Step 4: Add metric defaults to `DefaultConfig()`**

In `DefaultConfig()`, add after the `Trace` field (after line 48's comma):

```go
		Metric: MetricConfig{
			Retention: MetricRetentionConfig{
				MaxAge: 24 * time.Hour,
			},
		},
```

The full `DefaultConfig()` should look like:

```go
func DefaultConfig() Config {
	return Config{
		Trace: TraceConfig{
			Retention: RetentionConfig{
				MaxAge:          24 * time.Hour,
				MaxCount:        10000,
				CleanupInterval: 5 * time.Minute,
			},
		},
		Metric: MetricConfig{
			Retention: MetricRetentionConfig{
				MaxAge: 24 * time.Hour,
			},
		},
	}
}
```

- [ ] **Step 5: Add metric YAML parsing to `LoadConfig()`**

Add after the existing trace retention parsing block (after line 82), before the `return cfg` line (line 84):

```go
	if raw.Metric.Retention.MaxAge != "" {
		if d, err := time.ParseDuration(raw.Metric.Retention.MaxAge); err == nil {
			cfg.Metric.Retention.MaxAge = d
		}
	}
```

- [ ] **Step 6: Commit**

```bash
git add internal/storage/config.go
git commit -m "feat: add MetricConfig and YAML parsing for metrics retention"
```

---

### Task 2: Extend config tests for metric retention

**Files:**
- Modify: `internal/storage/config_test.go`

- [ ] **Step 1: Update `TestLoadConfigDefault` with metric assertions**

Add after the existing `CleanupInterval` assertion (after line 21):

```go
	if cfg.Metric.Retention.MaxAge != 24*time.Hour {
		t.Errorf("Metric.MaxAge: want 24h, got %v", cfg.Metric.Retention.MaxAge)
	}
```

- [ ] **Step 2: Update `TestLoadConfigFromFile` with metric YAML and assertions**

Change the YAML content to include the metric section:

```go
	err := os.WriteFile(path, []byte(`trace:
  retention:
    max_age: 48h
    max_count: 5000
    cleanup_interval: 10m
metric:
  retention:
    max_age: 72h
`), 0644)
```

Add assertions at the end of the test (after line 47):

```go
	if cfg.Metric.Retention.MaxAge != 72*time.Hour {
		t.Errorf("Metric.MaxAge: want 72h, got %v", cfg.Metric.Retention.MaxAge)
	}
```

- [ ] **Step 3: Update `TestLoadConfigInvalidYAML` with metric default assertion**

Add after the existing `CleanupInterval` assertion (after line 69):

```go
	if cfg.Metric.Retention.MaxAge != 24*time.Hour {
		t.Errorf("Metric.MaxAge: want default 24h, got %v", cfg.Metric.Retention.MaxAge)
	}
```

- [ ] **Step 4: Add a new test `TestLoadConfigMetricOnly` for partial config with only metric section**

Add a new test function at the end of the file:

```go
func TestLoadConfigMetricOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labubu.yaml")
	err := os.WriteFile(path, []byte(`metric:
  retention:
    max_age: 12h
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(path)

	if cfg.Metric.Retention.MaxAge != 12*time.Hour {
		t.Errorf("Metric.MaxAge: want 12h, got %v", cfg.Metric.Retention.MaxAge)
	}
	// Trace defaults should still apply.
	if cfg.Trace.Retention.MaxAge != 24*time.Hour {
		t.Errorf("Trace.MaxAge: want default 24h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Trace.Retention.MaxCount != 10000 {
		t.Errorf("Trace.MaxCount: want default 10000, got %d", cfg.Trace.Retention.MaxCount)
	}
}
```

- [ ] **Step 5: Run config tests**

```bash
go test -v ./internal/storage/ -run TestLoadConfig
```

Expected: all 5 tests PASS (TestLoadConfigDefault, TestLoadConfigFromFile, TestLoadConfigInvalidYAML, TestLoadConfigPartialFields, TestLoadConfigMetricOnly)

- [ ] **Step 6: Commit**

```bash
git add internal/storage/config_test.go
git commit -m "test: add metric retention config tests"
```

---

### Task 3: Wire metric config into serve command

**Files:**
- Modify: `cmd/labubu/main.go`

- [ ] **Step 1: Remove the `--metrics-retention` CLI flag**

Remove line 82:

```go
	metricsRetention := fs.Duration("metrics-retention", 2*time.Hour, "tstorage retention duration")
```

- [ ] **Step 2: Change metrics store initialization to use YAML config**

Change lines 139-142 from:

```go
		ms, err := metrics.NewTStorageStore(metrics.TStorageConfig{
			DataDir:   *metricsDataDir,
			Retention: *metricsRetention,
		})
```

To:

```go
		ms, err := metrics.NewTStorageStore(metrics.TStorageConfig{
			DataDir:   *metricsDataDir,
			Retention: cfg.Metric.Retention.MaxAge,
		})
```

- [ ] **Step 3: Add metric retention banner line**

After the trace retention banner line (after line 121), add:

```go
	fmt.Printf("  Metric retention: max_age=%s\n", cfg.Metric.Retention.MaxAge)
```

- [ ] **Step 4: Verify the build compiles**

```bash
make build-nocgo
```

Expected: build succeeds with no errors.

- [ ] **Step 5: Commit**

```bash
git add cmd/labubu/main.go
git commit -m "feat: wire metrics retention from YAML config, remove CLI flag"
```

---

### Task 4: Run full test suite

- [ ] **Step 1: Run Go tests**

```bash
go test -v ./internal/... ./web/... ./cmd/...
```

Expected: all tests PASS.

- [ ] **Step 2: Final review of the diff**

```bash
git diff master...HEAD
```

Confirm the changes are:
- `internal/storage/config.go`: New types + YAML parsing + defaults (additions only, no existing code altered)
- `internal/storage/config_test.go`: Extended existing tests + one new test
- `cmd/labubu/main.go`: Remove one CLI flag line, change one config line, add one banner line
