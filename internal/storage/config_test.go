package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigDefault(t *testing.T) {
	cfg := LoadConfig("/nonexistent/path/labubu.yaml")

	if cfg.Trace.Retention.MaxAge != 7*24*time.Hour {
		t.Errorf("MaxAge: want 168h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Trace.Retention.MaxCount != 10000 {
		t.Errorf("MaxCount: want 10000, got %d", cfg.Trace.Retention.MaxCount)
	}
	if cfg.Trace.Retention.CleanupInterval != 5*time.Minute {
		t.Errorf("CleanupInterval: want 5m, got %v", cfg.Trace.Retention.CleanupInterval)
	}
	if cfg.Log.Retention.MaxAge != 7*24*time.Hour {
		t.Errorf("Log.MaxAge: want 168h, got %v", cfg.Log.Retention.MaxAge)
	}
	if cfg.Metric.Retention.MaxAge != 7*24*time.Hour {
		t.Errorf("Metric.MaxAge: want 168h, got %v", cfg.Metric.Retention.MaxAge)
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
metric:
  retention:
    max_age: 72h
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
	if cfg.Metric.Retention.MaxAge != 72*time.Hour {
		t.Errorf("Metric.MaxAge: want 72h, got %v", cfg.Metric.Retention.MaxAge)
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
	if cfg.Trace.Retention.MaxAge != 7*24*time.Hour {
		t.Errorf("MaxAge: want default 168h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Trace.Retention.MaxCount != 10000 {
		t.Errorf("MaxCount: want default 10000, got %d", cfg.Trace.Retention.MaxCount)
	}
	if cfg.Trace.Retention.CleanupInterval != 5*time.Minute {
		t.Errorf("CleanupInterval: want default 5m, got %v", cfg.Trace.Retention.CleanupInterval)
	}
	if cfg.Log.Retention.MaxAge != 7*24*time.Hour {
		t.Errorf("Log.MaxAge: want default 168h, got %v", cfg.Log.Retention.MaxAge)
	}
	if cfg.Metric.Retention.MaxAge != 7*24*time.Hour {
		t.Errorf("Metric.MaxAge: want default 168h, got %v", cfg.Metric.Retention.MaxAge)
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
	if cfg.Trace.Retention.MaxAge != 7*24*time.Hour {
		t.Errorf("Trace.MaxAge: want default 168h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Trace.Retention.MaxCount != 10000 {
		t.Errorf("Trace.MaxCount: want default 10000, got %d", cfg.Trace.Retention.MaxCount)
	}
}

func TestLoadConfigLogRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labubu.yaml")
	err := os.WriteFile(path, []byte(`log:
  retention:
    max_age: 48h
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg := LoadConfig(path)

	if cfg.Log.Retention.MaxAge != 48*time.Hour {
		t.Errorf("Log.MaxAge: want 48h, got %v", cfg.Log.Retention.MaxAge)
	}
	// Other defaults should still apply.
	if cfg.Trace.Retention.MaxAge != 7*24*time.Hour {
		t.Errorf("Trace.MaxAge: want default 168h, got %v", cfg.Trace.Retention.MaxAge)
	}
	if cfg.Metric.Retention.MaxAge != 7*24*time.Hour {
		t.Errorf("Metric.MaxAge: want default 168h, got %v", cfg.Metric.Retention.MaxAge)
	}
}
