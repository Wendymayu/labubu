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
