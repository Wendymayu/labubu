package metrics

import (
	"context"
	"testing"
	"time"
)

func TestTStorageStore_InsertAndSelect(t *testing.T) {
	store, err := NewTStorageStore(TStorageConfig{
		DataDir:   "", // empty = in-memory only
		Retention: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("failed to create tstorage store: %v", err)
	}
	defer store.Close()

	now := time.Now().UnixMilli()
	points := []MetricPoint{
		{Name: "test_metric", Labels: map[string]string{"service": "api"}, Value: 42.0, Timestamp: now},
		{Name: "test_metric", Labels: map[string]string{"service": "api"}, Value: 43.0, Timestamp: now + 1000},
		{Name: "test_metric", Labels: map[string]string{"service": "web"}, Value: 10.0, Timestamp: now},
	}

	ctx := context.Background()
	if err := store.Insert(ctx, points); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Select with label filter.
	series, err := store.Select(ctx, "test_metric", map[string]string{"service": "api"}, now-1000, now+2000)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
	if len(series[0].Points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(series[0].Points))
	}
	if series[0].Points[0].Value != 42.0 {
		t.Errorf("expected value 42.0, got %f", series[0].Points[0].Value)
	}
}

func TestTStorageStore_LabelNames(t *testing.T) {
	store, err := NewTStorageStore(TStorageConfig{
		DataDir:   "",
		Retention: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("failed to create tstorage store: %v", err)
	}
	defer store.Close()

	now := time.Now().UnixMilli()
	points := []MetricPoint{
		{Name: "cpu_usage", Labels: map[string]string{"host": "a", "region": "us"}, Value: 1.0, Timestamp: now},
	}

	ctx := context.Background()
	if err := store.Insert(ctx, points); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	names, err := store.LabelNames(ctx)
	if err != nil {
		t.Fatalf("labelNames failed: %v", err)
	}
	if len(names) == 0 {
		t.Error("expected at least 1 label name")
	}

	found := make(map[string]bool)
	for _, n := range names {
		found[n] = true
	}
	for _, want := range []string{"host", "region"} {
		if !found[want] {
			t.Errorf("expected label name %q not found in %v", want, names)
		}
	}
}

func TestTStorageStore_LabelValues(t *testing.T) {
	store, err := NewTStorageStore(TStorageConfig{
		DataDir:   "",
		Retention: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("failed to create tstorage store: %v", err)
	}
	defer store.Close()

	now := time.Now().UnixMilli()
	points := []MetricPoint{
		{Name: "cpu", Labels: map[string]string{"host": "a"}, Value: 1.0, Timestamp: now},
		{Name: "cpu", Labels: map[string]string{"host": "b"}, Value: 2.0, Timestamp: now},
	}

	ctx := context.Background()
	if err := store.Insert(ctx, points); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	values, err := store.LabelValues(ctx, "host")
	if err != nil {
		t.Fatalf("labelValues failed: %v", err)
	}

	found := make(map[string]bool)
	for _, v := range values {
		found[v] = true
	}
	if !found["a"] || !found["b"] {
		t.Errorf("expected values [a, b], got %v", values)
	}
}
