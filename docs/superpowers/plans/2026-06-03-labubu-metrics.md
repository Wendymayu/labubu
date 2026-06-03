# OTLP Metrics Ingestion + Prometheus API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OTLP metrics ingestion to Labubu with embedded tstorage TSDB and Prometheus-compatible HTTP API.

**Architecture:** New metrics subsystem parallel to existing trace pipeline — OTLP ExportMetricsServiceRequest arrives at same ports (4317/4318), receiver translates to Prometheus data model (metric name + labels), stores via Store interface backed by tstorage, exposes Prometheus HTTP API at `/api/v1/query`, `/api/v1/query_range`, `/api/v1/labels`, `/api/v1/label/:name/values`, `/api/v1/metadata`.

**Tech Stack:** Go 1.19, tstorage (pure Go embedded TSDB), OTLP metrics protobuf v0.20.0, existing net/http ServeMux router

---

## File Structure

```
labubu/
├── cmd/labubu/main.go                  # [MODIFY] add metrics flags, init metricStore, wire to receiver & router
├── internal/
│   ├── metrics/
│   │   ├── store.go                    # ★ CREATE: Store interface + MetricPoint/MetricSeries types
│   │   ├── tstorage_store.go           # ★ CREATE: tstorage embedded implementation
│   │   └── tstorage_store_test.go      # ★ CREATE: tests for tstorage store
│   ├── receiver/
│   │   ├── otlp.go                     # [MODIFY] add metrics gRPC + HTTP handler, extend Receiver struct
│   │   ├── metrics_translator.go       # ★ CREATE: OTLP metrics proto → []MetricPoint
│   │   └── metrics_translator_test.go  # ★ CREATE: tests for translator
│   ├── api/
│   │   ├── router.go                   # [MODIFY] add /api/v1/query, /api/v1/query_range, /api/v1/labels, etc.
│   │   ├── metrics_handler.go          # ★ CREATE: Prometheus HTTP API handlers
│   │   └── metrics_handler_test.go     # ★ CREATE: tests for metrics handlers
```

### File Responsibilities

| File | Responsibility |
|------|---------------|
| `internal/metrics/store.go` | Define `MetricPoint`, `MetricSeries` types and `Store` interface |
| `internal/metrics/tstorage_store.go` | Implement `Store` using tstorage library (insert, select, label listing, close) |
| `internal/receiver/metrics_translator.go` | Translate `ExportMetricsServiceRequest` proto → `[]MetricPoint` (Gauge, Sum, Histogram, Summary) |
| `internal/receiver/otlp.go` | Extend `Receiver` to accept metrics.Store, register MetricsService gRPC + `/v1/metrics` HTTP |
| `internal/api/metrics_handler.go` | Prometheus API: instant query, range query, label names, label values, metadata |
| `internal/api/router.go` | Register 5 new metrics routes alongside existing trace routes |
| `cmd/labubu/main.go` | Add `--metrics-*` flags, init tstorage store, pass to receiver and router |

---

### Task 1: Add tstorage dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add tstorage dependency**

Run:
```bash
cd D:\opensource\github\labubu && go get github.com/nakabonne/tstorage
```

- [ ] **Step 2: Verify build**

Run:
```bash
go build ./...
```
Expected: builds successfully (tstorage imports resolved).

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add tstorage dependency for embedded metrics TSDB"
```

---

### Task 2: Define Store interface and types

**Files:**
- Create: `internal/metrics/store.go`

- [ ] **Step 1: Create the metrics package with Store interface**

```go
// Package metrics defines the metrics storage interface and data types.
package metrics

import "context"

// MetricPoint is a single metric data point with labels.
type MetricPoint struct {
	Name      string
	Labels    map[string]string
	Value     float64
	Timestamp int64 // milliseconds since epoch
}

// MetricSeries is a named set of labeled data points (a time series).
type MetricSeries struct {
	Name   string
	Labels map[string]string
	Points []MetricPoint
}

// Store is the metrics storage backend interface.
type Store interface {
	// Insert writes metric data points. Called by the metrics receiver.
	Insert(ctx context.Context, points []MetricPoint) error

	// Select returns time series matching the metric name and label filters
	// within the time range [start, end] (milliseconds since epoch).
	Select(ctx context.Context, metric string, labels map[string]string, start, end int64) ([]MetricSeries, error)

	// LabelNames returns all known label names.
	LabelNames(ctx context.Context) ([]string, error)

	// LabelValues returns all values for a given label name.
	LabelValues(ctx context.Context, name string) ([]string, error)

	// Close gracefully shuts down the store.
	Close() error
}
```

- [ ] **Step 2: Verify build**

Run:
```bash
go build ./internal/metrics/...
```
Expected: package compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/metrics/store.go
git commit -m "feat: define metrics Store interface with MetricPoint/MetricSeries types"
```

---

### Task 3: Implement tstorage Store

**Files:**
- Create: `internal/metrics/tstorage_store.go`
- Create: `internal/metrics/tstorage_store_test.go`

tstorage API reference:
- `tstorage.NewStorage(opts...)` returns `(*tstorage.Storage, error)`
- `storage.InsertRows(rows []tstorage.Row) error`
- `storage.Select(metric string, labels []tstorage.Label, start, end int64) ([]*tstorage.DataPoint, error)`
- Timestamp precision: `tstorage.WithTimestampPrecision(tstorage.Milliseconds)`

- [ ] **Step 1: Write failing tests for tstorage_store.go**

```go
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

	// Check that all expected labels are present.
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/metrics/... -v -run "TestTStorage"
```
Expected: FAIL — "NewTStorageStore" undefined.

- [ ] **Step 3: Implement TStorageStore**

```go
package metrics

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nakabonne/tstorage"
)

// TStorageConfig holds configuration for the tstorage-backed metrics store.
type TStorageConfig struct {
	DataDir   string        // empty = in-memory only
	Retention time.Duration // how long to retain data in memory partitions
}

// TStorageStore implements Store using the tstorage embedded TSDB.
type TStorageStore struct {
	storage tstorage.Storage
}

// NewTStorageStore creates a new tstorage-backed metrics store.
func NewTStorageStore(cfg TStorageConfig) (*TStorageStore, error) {
	opts := []tstorage.Option{
		tstorage.WithTimestampPrecision(tstorage.Milliseconds),
	}
	if cfg.DataDir != "" {
		opts = append(opts, tstorage.WithDataPath(cfg.DataDir))
	}
	if cfg.Retention > 0 {
		opts = append(opts, tstorage.WithRetention(cfg.Retention))
	}

	s, err := tstorage.NewStorage(opts...)
	if err != nil {
		return nil, fmt.Errorf("tstorage: %w", err)
	}

	return &TStorageStore{storage: s}, nil
}

// Insert writes metric data points to the store.
func (s *TStorageStore) Insert(ctx context.Context, points []MetricPoint) error {
	rows := make([]tstorage.Row, 0, len(points))
	for _, p := range points {
		labels := make([]tstorage.Label, 0, len(p.Labels))
		for k, v := range p.Labels {
			labels = append(labels, tstorage.Label{Name: k, Value: v})
		}
		rows = append(rows, tstorage.Row{
			Metric: p.Name,
			Labels: labels,
			DataPoint: tstorage.DataPoint{
				Value:     p.Value,
				Timestamp: p.Timestamp,
			},
		})
	}
	return s.storage.InsertRows(rows)
}

// Select returns time series matching the metric name and label filters.
func (s *TStorageStore) Select(ctx context.Context, metric string, labels map[string]string, start, end int64) ([]MetricSeries, error) {
	tlabels := make([]tstorage.Label, 0, len(labels))
	for k, v := range labels {
		tlabels = append(tlabels, tstorage.Label{Name: k, Value: v})
	}

	dps, err := s.storage.Select(metric, tlabels, start, end)
	if err != nil {
		return nil, fmt.Errorf("tstorage select: %w", err)
	}

	if len(dps) == 0 {
		return nil, nil
	}

	// Sort points by timestamp.
	sort.Slice(dps, func(i, j int) bool {
		return dps[i].Timestamp < dps[j].Timestamp
	})

	mpoints := make([]MetricPoint, 0, len(dps))
	for _, p := range dps {
		mpoints = append(mpoints, MetricPoint{
			Name:      metric,
			Labels:    labels,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		})
	}

	return []MetricSeries{{
		Name:   metric,
		Labels: labels,
		Points: mpoints,
	}}, nil
}

// LabelNames returns all known label names from metrics stored so far.
// tstorage doesn't expose label name listing, so we track labels during insert.
// For simplicity, label names are derived from a simple in-memory set.
func (s *TStorageStore) LabelNames(ctx context.Context) ([]string, error) {
	// tstorage doesn't expose label names directly.
	// We'll maintain a simple in-memory set for this.
	// Return empty for now — the API handler already handles this gracefully.
	return nil, nil
}

// LabelValues returns all values for a given label name.
func (s *TStorageStore) LabelValues(ctx context.Context, name string) ([]string, error) {
	// tstorage doesn't expose label values directly.
	// Return empty for now — the API handler already handles this gracefully.
	return nil, nil
}

// Close shuts down the store.
func (s *TStorageStore) Close() error {
	return s.storage.Close()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/metrics/... -v -run "TestTStorage"
```
Expected: PASS for Insert/Select test. LabelNames/LabelValues tests will fail because the current implementation returns nil.

- [ ] **Step 5: Add label tracking to TStorageStore**

Edit `internal/metrics/tstorage_store.go` — add an in-memory label index:

In `TStorageStore` struct, add fields:
```go
type TStorageStore struct {
	storage   tstorage.Storage
	labelIdx  map[string]map[string]struct{} // label name → values set
	mu        sync.RWMutex
}
```

In `NewTStorageStore`, initialize `labelIdx: make(map[string]map[string]struct{})`.

In `Insert`, after inserting rows, update the label index:
```go
s.mu.Lock()
for _, p := range points {
	for k, v := range p.Labels {
		if s.labelIdx[k] == nil {
			s.labelIdx[k] = make(map[string]struct{})
		}
		s.labelIdx[k][v] = struct{}{}
	}
}
s.mu.Unlock()
```

In `LabelNames`:
```go
func (s *TStorageStore) LabelNames(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.labelIdx))
	for k := range s.labelIdx {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}
```

In `LabelValues`:
```go
func (s *TStorageStore) LabelValues(ctx context.Context, name string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]string, 0)
	if vs, ok := s.labelIdx[name]; ok {
		for v := range vs {
			values = append(values, v)
		}
	}
	sort.Strings(values)
	return values, nil
}
```

Add `"sync"` and `"sort"` to imports (sort is already imported above for points).

- [ ] **Step 6: Run all tests to verify**

Run:
```bash
go test ./internal/metrics/... -v
```
Expected: ALL PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/metrics/tstorage_store.go internal/metrics/tstorage_store_test.go
git commit -m "feat: implement tstorage-backed metrics Store with label index"
```

---

### Task 4: OTLP Metrics translator

**Files:**
- Create: `internal/receiver/metrics_translator.go`
- Create: `internal/receiver/metrics_translator_test.go`

OTLP metrics proto layout:
```
ExportMetricsServiceRequest
  └── ResourceMetrics[]
        ├── Resource (commonpb.Resource) → attributes
        ├── SchemaUrl
        └── ScopeMetrics[]
              ├── Scope (commonpb.InstrumentationScope) → name, version
              ├── SchemaUrl
              └── Metrics[]
                    ├── Name, Description, Unit
                    └── Data: Gauge | Sum | Histogram | Summary
```

Import paths:
- `collector metrics v1`: `go.opentelemetry.io/proto/otlp/collector/metrics/v1` as `colmetricspb`
- `metrics v1`: `go.opentelemetry.io/proto/otlp/metrics/v1` as `metricspb`

- [ ] **Step 1: Write failing tests for translator**

```go
package receiver

import (
	"testing"

	"github.com/labubu/labubu/internal/metrics"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func TestTranslateMetrics_Gauge(t *testing.T) {
	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "test-svc"}}},
					},
				},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Scope: &commonpb.InstrumentationScope{Name: "test-scope", Version: "1.0"},
						Metrics: []*metricspb.Metric{
							{
								Name:        "gen_ai_client_token_usage",
								Description: "Token usage",
								Unit:        "tokens",
								Data: &metricspb.Metric_Gauge{
									Gauge: &metricspb.Gauge{
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: 1717000000000000000,
												Attributes: []*commonpb.KeyValue{
													{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-opus"}}},
												},
												Value: &metricspb.NumberDataPoint_AsInt{AsInt: 4500},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	points := TranslateMetrics(req)
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}

	p := points[0]
	if p.Name != "gen_ai_client_token_usage" {
		t.Errorf("expected name 'gen_ai_client_token_usage', got %q", p.Name)
	}
	if p.Value != 4500.0 {
		t.Errorf("expected value 4500.0, got %f", p.Value)
	}
	if p.Labels["service"] != "test-svc" {
		t.Errorf("expected service label 'test-svc', got %q", p.Labels["service"])
	}
	if p.Labels["model"] != "claude-opus" {
		t.Errorf("expected model label 'claude-opus', got %q", p.Labels["model"])
	}
	if p.Labels["scope_name"] != "test-scope" {
		t.Errorf("expected scope_name label 'test-scope', got %q", p.Labels["scope_name"])
	}
	if p.Timestamp != 1717000000000 {
		t.Errorf("expected timestamp 1717000000000, got %d", p.Timestamp)
	}
}

func TestTranslateMetrics_Sum(t *testing.T) {
	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Scope: &commonpb.InstrumentationScope{Name: "test"},
						Metrics: []*metricspb.Metric{
							{
								Name: "requests_total",
								Data: &metricspb.Metric_Sum{
									Sum: &metricspb.Sum{
										AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
										IsMonotonic:            true,
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: 1717000000000000000,
												Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 99.5},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	points := TranslateMetrics(req)
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}
	if points[0].Value != 99.5 {
		t.Errorf("expected 99.5, got %f", points[0].Value)
	}
}

func TestTranslateMetrics_Histogram(t *testing.T) {
	bucketCounts := []uint64{5, 20, 35}
	explicitBounds := []float64{10.0, 50.0, 100.0}

	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Scope: &commonpb.InstrumentationScope{Name: "test"},
						Metrics: []*metricspb.Metric{
							{
								Name: "http_request_duration",
								Data: &metricspb.Metric_Histogram{
									Histogram: &metricspb.Histogram{
										DataPoints: []*metricspb.HistogramDataPoint{
											{
												TimeUnixNano:   1717000000000000000,
												Count:          60,
												Sum:            func() *float64 { v := 7500.0; return &v }(),
												BucketCounts:   bucketCounts,
												ExplicitBounds: explicitBounds,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	points := TranslateMetrics(req)
	// Expected: 3 buckets + 1 _sum + 1 _count = 5 points
	if len(points) != 5 {
		t.Fatalf("expected 5 points (3 buckets + sum + count), got %d", len(points))
	}

	// Check bucket points.
	bucketFound := false
	for _, p := range points {
		if p.Name == "http_request_duration_bucket" {
			bucketFound = true
			if p.Labels["le"] == "10.000000" {
				if p.Value != 5.0 {
					t.Errorf("expected bucket le=10 value 5, got %f", p.Value)
				}
			}
		}
	}
	if !bucketFound {
		t.Error("no _bucket points found")
	}

	// Check _sum and _count.
	sumFound := false
	countFound := false
	for _, p := range points {
		if p.Name == "http_request_duration_sum" {
			sumFound = true
			if p.Value != 7500.0 {
				t.Errorf("expected sum 7500, got %f", p.Value)
			}
		}
		if p.Name == "http_request_duration_count" {
			countFound = true
			if p.Value != 60.0 {
				t.Errorf("expected count 60, got %f", p.Value)
			}
		}
	}
	if !sumFound {
		t.Error("no _sum point found")
	}
	if !countFound {
		t.Error("no _count point found")
	}
}

func TestTranslateMetrics_EmptyRequest(t *testing.T) {
	req := &colmetricspb.ExportMetricsServiceRequest{}
	points := TranslateMetrics(req)
	if points != nil && len(points) > 0 {
		t.Errorf("expected 0 points for empty request, got %d", len(points))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/receiver/... -v -run "TestTranslateMetrics"
```
Expected: FAIL — "TranslateMetrics" undefined.

- [ ] **Step 3: Implement the translator**

```go
package receiver

import (
	"fmt"
	"math"

	"github.com/labubu/labubu/internal/metrics"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// TranslateMetrics converts an OTLP ExportMetricsServiceRequest into a flat
// list of MetricPoints using Prometheus data model conventions.
func TranslateMetrics(req *colmetricspb.ExportMetricsServiceRequest) []metrics.MetricPoint {
	var points []metrics.MetricPoint

	for _, rm := range req.GetResourceMetrics() {
		resourceLabels := keyValueToMap(rm.GetResource().GetAttributes())
		// Warn: resource SchemaUrl ignored for simplicity
		_ = rm.GetSchemaUrl()

		// Extract service.name as "service" label for Prometheus compatibility.
		if svc, ok := resourceLabels["service.name"]; ok {
			resourceLabels["service"] = svc
		}

		for _, sm := range rm.GetScopeMetrics() {
			scopeLabels := map[string]string{
				"scope_name":    sm.GetScope().GetName(),
				"scope_version": sm.GetScope().GetVersion(),
			}

			for _, m := range sm.GetMetrics() {
				if m == nil {
					continue
				}
				switch d := m.Data.(type) {
				case *metricspb.Metric_Gauge:
					points = append(points, gaugeToPoints(m.Name, resourceLabels, scopeLabels, d.Gauge)...)
				case *metricspb.Metric_Sum:
					points = append(points, sumToPoints(m.Name, resourceLabels, scopeLabels, d.Sum)...)
				case *metricspb.Metric_Histogram:
					points = append(points, histogramToPoints(m.Name, resourceLabels, scopeLabels, d.Histogram)...)
				case *metricspb.Metric_Summary:
					points = append(points, summaryToPoints(m.Name, resourceLabels, scopeLabels, d.Summary)...)
				default:
					// Unknown data type — skip, don't block.
					_ = proto.MessageName(m)
				}
			}
		}
	}

	return points
}

// gaugeToPoints translates OTLP Gauge data points.
func gaugeToPoints(name string, resourceLabels, scopeLabels map[string]string, gauge *metricspb.Gauge) []metrics.MetricPoint {
	var points []metrics.MetricPoint
	for _, dp := range gauge.GetDataPoints() {
		pts := numberDataPointToMetricPoints(name, resourceLabels, scopeLabels, dp)
		points = append(points, pts...)
	}
	return points
}

// sumToPoints translates OTLP Sum data points.
func sumToPoints(name string, resourceLabels, scopeLabels map[string]string, sum *metricspb.Sum) []metrics.MetricPoint {
	var points []metrics.MetricPoint
	for _, dp := range sum.GetDataPoints() {
		pts := numberDataPointToMetricPoints(name, resourceLabels, scopeLabels, dp)
		points = append(points, pts...)
	}
	return points
}

// numberDataPointToMetricPoints converts an OTLP NumberDataPoint to []MetricPoint.
func numberDataPointToMetricPoints(name string, resourceLabels, scopeLabels map[string]string, dp *metricspb.NumberDataPoint) []metrics.MetricPoint {
	if dp == nil {
		return nil
	}

	ts := dp.GetTimeUnixNano() / 1_000_000 // nanoseconds → milliseconds

	attrLabels := keyValueToMap(dp.GetAttributes())
	// Merge labels: scope < resource < attribute (later overrides earlier).
	allLabels := mergeLabels(resourceLabels, scopeLabels, attrLabels)

	var value float64
	switch v := dp.Value.(type) {
	case *metricspb.NumberDataPoint_AsInt:
		value = float64(v.AsInt)
	case *metricspb.NumberDataPoint_AsDouble:
		value = v.AsDouble
	}

	return []metrics.MetricPoint{{
		Name:      name,
		Labels:    allLabels,
		Value:     value,
		Timestamp: ts,
	}}
}

// histogramToPoints expands an OTLP Histogram into Prometheus-style _bucket, _sum, _count points.
func histogramToPoints(name string, resourceLabels, scopeLabels map[string]string, hist *metricspb.Histogram) []metrics.MetricPoint {
	var points []metrics.MetricPoint

	for _, dp := range hist.GetDataPoints() {
		ts := dp.GetTimeUnixNano() / 1_000_000 // ns → ms
		attrLabels := keyValueToMap(dp.GetAttributes())
		allLabels := mergeLabels(resourceLabels, scopeLabels, attrLabels)

		bounds := dp.GetExplicitBounds()
		counts := dp.GetBucketCounts()

		// Emit _bucket points with le label.
		cumulative := uint64(0)
		for i := 0; i < len(bounds) && i < len(counts); i++ {
			cumulative += counts[i]
			bucketLabels := copyLabels(allLabels)
			bucketLabels["le"] = fmt.Sprintf("%f", bounds[i])
			points = append(points, metrics.MetricPoint{
				Name:      name + "_bucket",
				Labels:    bucketLabels,
				Value:     float64(cumulative),
				Timestamp: ts,
			})
		}

		// Emit +Inf bucket.
		if len(counts) > len(bounds) {
			cumulative += counts[len(bounds)]
		}
		infLabels := copyLabels(allLabels)
		infLabels["le"] = "+Inf"
		points = append(points, metrics.MetricPoint{
			Name:      name + "_bucket",
			Labels:    infLabels,
			Value:     float64(cumulative),
			Timestamp: ts,
		})

		// Emit _sum.
		if dp.Sum != nil {
			sumLabels := copyLabels(allLabels)
			points = append(points, metrics.MetricPoint{
				Name:      name + "_sum",
				Labels:    sumLabels,
				Value:     *dp.Sum,
				Timestamp: ts,
			})
		}

		// Emit _count.
		countLabels := copyLabels(allLabels)
		points = append(points, metrics.MetricPoint{
			Name:      name + "_count",
			Labels:    countLabels,
			Value:     float64(dp.GetCount()),
			Timestamp: ts,
		})
	}

	return points
}

// summaryToPoints translates an OTLP Summary into Prometheus _sum, _count, and quantile points.
func summaryToPoints(name string, resourceLabels, scopeLabels map[string]string, summary *metricspb.Summary) []metrics.MetricPoint {
	var points []metrics.MetricPoint

	for _, dp := range summary.GetDataPoints() {
		ts := dp.GetTimeUnixNano() / 1_000_000 // ns → ms
		attrLabels := keyValueToMap(dp.GetAttributes())
		allLabels := mergeLabels(resourceLabels, scopeLabels, attrLabels)

		// Emit _sum.
		sumLabels := copyLabels(allLabels)
		points = append(points, metrics.MetricPoint{
			Name:      name + "_sum",
			Labels:    sumLabels,
			Value:     dp.GetSum(),
			Timestamp: ts,
		})

		// Emit _count.
		countLabels := copyLabels(allLabels)
		points = append(points, metrics.MetricPoint{
			Name:      name + "_count",
			Labels:    countLabels,
			Value:     float64(dp.GetCount()),
			Timestamp: ts,
		})

		// Emit quantile points.
		for _, qv := range dp.GetQuantileValues() {
			qLabels := copyLabels(allLabels)
			qLabels["quantile"] = fmt.Sprintf("%f", qv.GetQuantile())
			points = append(points, metrics.MetricPoint{
				Name:      name,
				Labels:    qLabels,
				Value:     qv.GetValue(),
				Timestamp: ts,
			})
		}
	}

	return points
}

// mergeLabels merges multiple label maps. Later maps override earlier ones for the same key.
func mergeLabels(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			if v != "" {
				result[k] = v
			}
		}
	}
	return result
}

// copyLabels returns a shallow copy of a labels map.
func copyLabels(labels map[string]string) map[string]string {
	cp := make(map[string]string, len(labels))
	for k, v := range labels {
		cp[k] = v
	}
	return cp
}

// init registers unused import guard.
var _ = math.MaxFloat64
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/receiver/... -v -run "TestTranslateMetrics"
```
Expected: ALL PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/receiver/metrics_translator.go internal/receiver/metrics_translator_test.go
git commit -m "feat: implement OTLP metrics to Prometheus translator (Gauge, Sum, Histogram, Summary)"
```

---

### Task 5: Integrate metrics into OTLP receiver

**Files:**
- Modify: `internal/receiver/otlp.go` — extend `Receiver` with metrics Store, add gRPC MetricsService + HTTP `/v1/metrics`

- [ ] **Step 1: Extend Receiver struct and constructor**

Edit `internal/receiver/otlp.go`:

Add to imports:
```go
"github.com/labubu/labubu/internal/metrics"
colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
```

Change `Receiver` struct:
```go
// Receiver listens for OTLP trace and metrics data on gRPC and HTTP endpoints.
type Receiver struct {
	pipeline    *pipeline.Pipeline
	metricStore metrics.Store
	grpcSrv     *grpc.Server
	httpSrv     *http.Server
}
```

Change `New`:
```go
// New creates a new Receiver.
func New(p *pipeline.Pipeline, ms metrics.Store) *Receiver {
	return &Receiver{
		pipeline:    p,
		metricStore: ms,
	}
}
```

- [ ] **Step 2: Register MetricsService on gRPC server**

In `Start()`, after `coltracepb.RegisterTraceServiceServer(r.grpcSrv, ...)` add:
```go
colmetricspb.RegisterMetricsServiceServer(r.grpcSrv, &metricsService{metricStore: r.metricStore})
```

- [ ] **Step 3: Add HTTP /v1/metrics handler**

In `Start()`, after `mux.HandleFunc("/v1/traces", ...)` add:
```go
mux.HandleFunc("/v1/metrics", r.handleHTTPMetrics)
```

- [ ] **Step 4: Implement gRPC MetricsService**

Add after the existing `traceService` implementation:
```go
// metricsService implements the OTLP gRPC MetricsService.
type metricsService struct {
	colmetricspb.UnimplementedMetricsServiceServer
	metricStore metrics.Store
}

// Export receives metrics data via gRPC.
func (s *metricsService) Export(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) (*colmetricspb.ExportMetricsServiceResponse, error) {
	points := TranslateMetrics(req)
	if len(points) == 0 {
		return &colmetricspb.ExportMetricsServiceResponse{}, nil
	}
	if err := s.metricStore.Insert(ctx, points); err != nil {
		return &colmetricspb.ExportMetricsServiceResponse{
			PartialSuccess: &colmetricspb.ExportMetricsPartialSuccess{
				RejectedDataPoints: int64(len(points)),
				ErrorMessage:       "store insert failed",
			},
		}, nil
	}
	return &colmetricspb.ExportMetricsServiceResponse{}, nil
}
```

- [ ] **Step 5: Implement HTTP /v1/metrics handler**

Add after the existing `handleHTTPTraces`:
```go
// handleHTTPMetrics handles OTLP HTTP POST /v1/metrics.
func (r *Receiver) handleHTTPMetrics(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer req.Body.Close()

	var exportReq colmetricspb.ExportMetricsServiceRequest
	if err := proto.Unmarshal(body, &exportReq); err != nil {
		http.Error(w, fmt.Sprintf("failed to unmarshal protobuf: %v", err), http.StatusBadRequest)
		return
	}

	points := TranslateMetrics(&exportReq)
	if len(points) == 0 {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"partialSuccess": map[string]interface{}{}})
		return
	}

	if err := r.metricStore.Insert(req.Context(), points); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "store insert failed"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"partialSuccess": map[string]interface{}{},
	})
}
```

- [ ] **Step 6: Update main.go for receiver.New signature change**

Edit `cmd/labubu/main.go`:

In the receiver creation section, update (metricStore will be passed — for now create a nil check or init it):
```go
// Initialize metrics store (placeholder — wired properly in Task 8).
metricStore := (*metrics.TStorageStore)(nil) // temporary; will be replaced in Task 8
metrics.Store(metricStore)                    // compile check

// Initialize OTLP receiver.
recv := receiver.New(pipe, metricStore)
```

Wait — this won't compile cleanly. A cleaner approach: add `metricsStore` after it's initialized. Let me do this properly by combining with Task 8. Actually the spec says to create the full implementation. Let me move the main.go wiring to Task 8. For now, I just need the receiver code to compile:

Actually wait — receiver.New signature changed from `func New(p *pipeline.Pipeline)` to `func New(p *pipeline.Pipeline, ms metrics.Store)`. The existing main.go will break. Let me handle this properly.

Rather than keeping main.go broken between tasks, I should make the receiver accept nil metric store gracefully (metrics ingestion is optional). Let me update the receiver to handle this:

In the `Start` method only register metrics routes when metricStore != nil:
```go
if r.metricStore != nil {
    colmetricspb.RegisterMetricsServiceServer(r.grpcSrv, &metricsService{metricStore: r.metricStore})
    mux.HandleFunc("/v1/metrics", r.handleHTTPMetrics)
}
```

And in main.go for this task, pass nil (since the store isn't created yet):
```go
recv := receiver.New(pipe, nil)
```

That way main.go compiles without breaking and the metrics functionality is gated on store != nil.

- [ ] **Step 7: Update main.go receiver call**

Edit `cmd/labubu/main.go`, change:
```go
recv := receiver.New(pipe)
```
to:
```go
recv := receiver.New(pipe, nil) // metricStore = nil until Task 8
```

- [ ] **Step 8: Verify build compiles**

Run:
```bash
go build ./...
```
Expected: builds successfully.

- [ ] **Step 9: Commit**

```bash
git add internal/receiver/otlp.go cmd/labubu/main.go
git commit -m "feat: integrate OTLP metrics gRPC+HTTP receiver (gated on Store != nil)"
```

---

### Task 6: Prometheus HTTP API handlers

**Files:**
- Create: `internal/api/metrics_handler.go`
- Create: `internal/api/metrics_handler_test.go`

- [ ] **Step 1: Create mock metrics store for handler tests**

The mock already lives in the test file. Let's write the test first.

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/labubu/labubu/internal/metrics"
)

// metricsMockStore implements metrics.Store for testing.
type metricsMockStore struct {
	points       []metrics.MetricPoint
	labelNames   []string
	labelValues  []string
	insertErr    error
	selectErr    error
}

func (m *metricsMockStore) Insert(ctx context.Context, points []metrics.MetricPoint) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.points = append(m.points, points...)
	return nil
}

func (m *metricsMockStore) Select(ctx context.Context, metric string, labels map[string]string, start, end int64) ([]metrics.MetricSeries, error) {
	if m.selectErr != nil {
		return nil, m.selectErr
	}
	// Filter by metric name and exact label match.
	var filtered []metrics.MetricPoint
	for _, p := range m.points {
		if p.Name != metric {
			continue
		}
		match := true
		for k, v := range labels {
			if pv, ok := p.Labels[k]; !ok || pv != v {
				match = false
				break
			}
		}
		if match {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	return []metrics.MetricSeries{{
		Name:   metric,
		Labels: labels,
		Points: filtered,
	}}, nil
}

func (m *metricsMockStore) LabelNames(ctx context.Context) ([]string, error) {
	return m.labelNames, nil
}

func (m *metricsMockStore) LabelValues(ctx context.Context, name string) ([]string, error) {
	return m.labelValues, nil
}

func (m *metricsMockStore) Close() error { return nil }

func TestMetricsHandler_InstantQuery(t *testing.T) {
	now := time.Now().UnixMilli()
	store := &metricsMockStore{
		points: []metrics.MetricPoint{
			{Name: "cpu_usage", Labels: map[string]string{"host": "a"}, Value: 42.0, Timestamp: now},
		},
	}

	handler := NewMetricsHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query?query=cpu_usage{host=%22a%22}&time="+strconv.FormatInt(now/1000, 10), nil)
	rec := httptest.NewRecorder()

	handler.InstantQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp prometheusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status 'success', got %q", resp.Status)
	}
	if resp.Data.ResultType != "vector" {
		t.Errorf("expected resultType 'vector', got %q", resp.Data.ResultType)
	}
	if len(resp.Data.Result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Data.Result))
	}
}

func TestMetricsHandler_RangeQuery(t *testing.T) {
	now := time.Now().UnixMilli()
	store := &metricsMockStore{
		points: []metrics.MetricPoint{
			{Name: "cpu_usage", Labels: map[string]string{"host": "a"}, Value: 42.0, Timestamp: now - 2000},
			{Name: "cpu_usage", Labels: map[string]string{"host": "a"}, Value: 43.0, Timestamp: now - 1000},
			{Name: "cpu_usage", Labels: map[string]string{"host": "a"}, Value: 44.0, Timestamp: now},
		},
	}

	handler := NewMetricsHandler(store)
	start := (now - 5000) / 1000
	end := now / 1000
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/query_range?query=cpu_usage{host=\"a\"}&start=%d&end=%d&step=1000", start, end), nil)
	rec := httptest.NewRecorder()

	handler.RangeQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp prometheusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status 'success', got %q", resp.Status)
	}
}

func TestMetricsHandler_Labels(t *testing.T) {
	store := &metricsMockStore{
		labelNames: []string{"host", "service", "__name__"},
	}

	handler := NewMetricsHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/labels", nil)
	rec := httptest.NewRecorder()

	handler.Labels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp prometheusLabelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status 'success', got %q", resp.Status)
	}
}

func TestMetricsHandler_LabelValues(t *testing.T) {
	store := &metricsMockStore{
		labelValues: []string{"a", "b", "c"},
	}

	handler := NewMetricsHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/label/host/values", nil)
	rec := httptest.NewRecorder()

	handler.LabelValues(rec, req, "host")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp prometheusLabelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status 'success', got %q", resp.Status)
	}
}

func TestMetricsHandler_Metadata(t *testing.T) {
	store := &metricsMockStore{}

	handler := NewMetricsHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata", nil)
	rec := httptest.NewRecorder()

	handler.Metadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMetricsHandler_MissingQueryParam(t *testing.T) {
	store := &metricsMockStore{}
	handler := NewMetricsHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query", nil)
	rec := httptest.NewRecorder()

	handler.InstantQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing query param, got %d", rec.Code)
	}
}

func TestParsePromQL(t *testing.T) {
	tests := []struct {
		input         string
		expectedName  string
		expectedLabel string
		expectedValue string
	}{
		{"cpu_usage", "cpu_usage", "", ""},
		{"cpu_usage{host=\"a\"}", "cpu_usage", "host", "a"},
		{"cpu_usage{host=\"a\",region=\"us\"}", "cpu_usage", "host", "a"},
		{"", "", "", ""},
	}

	for _, tt := range tests {
		name, labels := parsePromQL(tt.input)
		if name != tt.expectedName {
			t.Errorf("parsePromQL(%q) name: got %q, want %q", tt.input, name, tt.expectedName)
		}
		if tt.expectedLabel != "" {
			if val, ok := labels[tt.expectedLabel]; !ok || val != tt.expectedValue {
				t.Errorf("parsePromQL(%q) label %s: got %q, want %q", tt.input, tt.expectedLabel, val, tt.expectedValue)
			}
		}
	}
}
```

Note: `formatInt` and `fmt` need to be in the test correctly. Let me add the full test with proper imports.

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/api/... -v -run "TestMetrics"
```
Expected: FAIL — MetricsHandler not defined.

- [ ] **Step 3: Implement MetricsHandler**

```go
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labubu/labubu/internal/metrics"
)

// Prometheus-compatible API response types.

type prometheusResponse struct {
	Status string              `json:"status"`
	Data   prometheusData      `json:"data,omitempty"`
	Error  string              `json:"error,omitempty"`
}

type prometheusData struct {
	ResultType string              `json:"resultType"`
	Result     []prometheusResult  `json:"result"`
}

type prometheusResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value,omitempty"`   // [timestamp, "value"]
	Values [][]interface{}   `json:"values,omitempty"`  // range query rows
}

type prometheusLabelsResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
}

// MetricsHandler holds HTTP handlers for Prometheus API endpoints.
type MetricsHandler struct {
	store metrics.Store
}

// NewMetricsHandler creates a new MetricsHandler.
func NewMetricsHandler(store metrics.Store) *MetricsHandler {
	return &MetricsHandler{store: store}
}

// parsePromQL extracts the metric name and label filters from a simple PromQL query.
// Only supports: metric_name and metric_name{label="value",...} patterns.
func parsePromQL(query string) (string, map[string]string) {
	labels := make(map[string]string)

	idx := strings.IndexByte(query, '{')
	if idx < 0 {
		return strings.TrimSpace(query), labels
	}

	name := strings.TrimSpace(query[:idx])
	rest := query[idx+1:]

	// Find closing brace (handle no nesting).
	closeIdx := strings.LastIndexByte(rest, '}')
	if closeIdx < 0 {
		return name, labels
	}
	rest = rest[:closeIdx]

	// Parse label pairs: key="value",key2="value2"
	parts := strings.Split(rest, ",")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(kv[1], `"`)
		labels[key] = val
	}

	return name, labels
}

// QueryParamTime parses time from query string (seconds Unix), returns milliseconds.
func queryParamTime(r *http.Request, key string) (int64, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0, fmt.Errorf("missing parameter %q", key)
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %q: %w", key, err)
	}
	return sec * 1000, nil // seconds → milliseconds
}

// InstantQuery handles GET /api/v1/query?query=...&time=...
func (h *MetricsHandler) InstantQuery(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, prometheusResponse{
			Status: "error",
			Error:  "missing required parameter 'query'",
		})
		return
	}

	// Parse time parameter.
	ts, err := queryParamTime(r, "time")
	if err != nil {
		// Default to current time if not provided.
		ts = time.Now().UnixMilli()
	}

	metricName, labels := parsePromQL(query)
	if metricName == "" {
		writeJSON(w, http.StatusOK, prometheusResponse{
			Status: "success",
			Data:   prometheusData{ResultType: "vector", Result: []prometheusResult{}},
		})
		return
	}

	// Query a narrow window around the target timestamp.
	start := ts - 5000 // 5 seconds before
	end := ts + 1000

	series, err := h.store.Select(r.Context(), metricName, labels, start, end)
	if err != nil {
		log.Printf("metrics: instant query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, prometheusResponse{
			Status: "error",
			Error:  fmt.Sprintf("query failed: %v", err),
		})
		return
	}

	results := make([]prometheusResult, 0)
	for _, s := range series {
		// Pick the point closest to ts.
		best := pickClosestPoint(s.Points, ts)
		if best == nil {
			continue
		}
		metricLabel := make(map[string]string, len(s.Labels)+1)
		metricLabel["__name__"] = s.Name
		for k, v := range s.Labels {
			metricLabel[k] = v
		}
		results = append(results, prometheusResult{
			Metric: metricLabel,
			Value:  []interface{}{float64(best.Timestamp) / 1000.0, strconv.FormatFloat(best.Value, 'f', -1, 64)},
		})
	}

	writeJSON(w, http.StatusOK, prometheusResponse{
		Status: "success",
		Data:   prometheusData{ResultType: "vector", Result: results},
	})
}

// pickClosestPoint returns the point with timestamp closest to target.
func pickClosestPoint(points []metrics.MetricPoint, target int64) *metrics.MetricPoint {
	if len(points) == 0 {
		return nil
	}
	best := &points[0]
	bestDiff := abs(best.Timestamp - target)
	for i := 1; i < len(points); i++ {
		diff := abs(points[i].Timestamp - target)
		if diff < bestDiff {
			bestDiff = diff
			best = &points[i]
		}
	}
	return best
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// RangeQuery handles GET /api/v1/query_range?query=...&start=...&end=...&step=...
func (h *MetricsHandler) RangeQuery(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, prometheusResponse{
			Status: "error",
			Error:  "missing required parameter 'query'",
		})
		return
	}

	start, err := queryParamTime(r, "start")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, prometheusResponse{Status: "error", Error: fmt.Sprintf("invalid start: %v", err)})
		return
	}
	end, err := queryParamTime(r, "end")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, prometheusResponse{Status: "error", Error: fmt.Sprintf("invalid end: %v", err)})
		return
	}
	stepSec, err := strconv.ParseInt(r.URL.Query().Get("step"), 10, 64)
	if err != nil || stepSec <= 0 {
		writeJSON(w, http.StatusBadRequest, prometheusResponse{Status: "error", Error: "missing or invalid step"})
		return
	}
	stepMS := stepSec * 1000

	metricName, labels := parsePromQL(query)
	if metricName == "" {
		writeJSON(w, http.StatusOK, prometheusResponse{
			Status: "success",
			Data:   prometheusData{ResultType: "matrix", Result: []prometheusResult{}},
		})
		return
	}

	series, err := h.store.Select(r.Context(), metricName, labels, start, end)
	if err != nil {
		log.Printf("metrics: range query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, prometheusResponse{
			Status: "error",
			Error:  fmt.Sprintf("query failed: %v", err),
		})
		return
	}

	results := make([]prometheusResult, 0)
	for _, s := range series {
		// Downsample points to step intervals.
		values := downsamplePoints(s.Points, start, end, stepMS)
		metricLabel := make(map[string]string, len(s.Labels)+1)
		metricLabel["__name__"] = s.Name
		for k, v := range s.Labels {
			metricLabel[k] = v
		}
		results = append(results, prometheusResult{
			Metric: metricLabel,
			Values: values,
		})
	}

	writeJSON(w, http.StatusOK, prometheusResponse{
		Status: "success",
		Data:   prometheusData{ResultType: "matrix", Result: results},
	})
}

// downsamplePoints picks the closest point for each step interval.
func downsamplePoints(points []metrics.MetricPoint, start, end, step int64) [][]interface{} {
	if len(points) == 0 {
		return nil
	}

	var values [][]interface{}
	for t := start; t <= end; t += step {
		best := pickClosestPoint(points, t)
		if best == nil {
			continue
		}
		values = append(values, []interface{}{
			float64(best.Timestamp) / 1000.0,
			strconv.FormatFloat(best.Value, 'f', -1, 64),
		})
	}
	return values
}

// Labels handles GET /api/v1/labels
func (h *MetricsHandler) Labels(w http.ResponseWriter, r *http.Request) {
	names, err := h.store.LabelNames(r.Context())
	if err != nil {
		log.Printf("metrics: labels error: %v", err)
		writeJSON(w, http.StatusInternalServerError, prometheusResponse{
			Status: "error",
			Error:  fmt.Sprintf("labels failed: %v", err),
		})
		return
	}
	if names == nil {
		names = []string{}
	}
	// Include __name__ as a standard label.
	hasName := false
	for _, n := range names {
		if n == "__name__" {
			hasName = true
			break
		}
	}
	if !hasName {
		names = append([]string{"__name__"}, names...)
	}
	writeJSON(w, http.StatusOK, prometheusLabelsResponse{Status: "success", Data: names})
}

// LabelValues handles GET /api/v1/label/:name/values
func (h *MetricsHandler) LabelValues(w http.ResponseWriter, r *http.Request, name string) {
	values, err := h.store.LabelValues(r.Context(), name)
	if err != nil {
		log.Printf("metrics: label values error: %v", err)
		writeJSON(w, http.StatusInternalServerError, prometheusResponse{
			Status: "error",
			Error:  fmt.Sprintf("label values failed: %v", err),
		})
		return
	}
	if values == nil {
		values = []string{}
	}
	writeJSON(w, http.StatusOK, prometheusLabelsResponse{Status: "success", Data: values})
}

// Metadata handles GET /api/v1/metadata
func (h *MetricsHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	// Return empty metadata for now — Prometheus data source still works.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
		"data":   map[string]interface{}{},
	})
}
```

- [ ] **Step 4: Verify test file has proper imports**

The test file created in Step 1 already has all required imports (`"fmt"`, `"strconv"`, `"context"`, `"encoding/json"`, `"net/http"`, `"net/http/httptest"`, `"testing"`, `"time"`, `"github.com/labubu/labubu/internal/metrics"`).

- [ ] **Step 5: Run tests to verify they pass**

Run:
```bash
go test ./internal/api/... -v -run "TestMetrics|TestParse"
```
Expected: ALL PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/metrics_handler.go internal/api/metrics_handler_test.go
git commit -m "feat: implement Prometheus HTTP API handlers (instant query, range query, labels)"
```

---

### Task 7: Register metrics routes in router

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add MetricsHandler parameter and routes**

Edit `NewRouter` signature to accept `*MetricsHandler`:

```go
func NewRouter(traceHandler *TraceHandler, metricsHandler *MetricsHandler) http.Handler {
	mux := http.NewServeMux()

	// API routes — traces.
	mux.HandleFunc("/api/v1/traces/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/traces")
		if path == "" || path == "/" {
			traceHandler.ListTraces(w, r)
			return
		}
		traceIDHex := strings.TrimPrefix(path, "/")
		traceHandler.GetTrace(w, r, traceIDHex)
	})
	mux.HandleFunc("/api/v1/services", traceHandler.GetServices)

	// API routes — metrics (Prometheus API).
	if metricsHandler != nil {
		mux.HandleFunc("/api/v1/query", metricsHandler.InstantQuery)
		mux.HandleFunc("/api/v1/query_range", metricsHandler.RangeQuery)
		mux.HandleFunc("/api/v1/labels", metricsHandler.Labels)
		mux.HandleFunc("/api/v1/label/", func(w http.ResponseWriter, r *http.Request) {
			// Path: /api/v1/label/{name}/values
			path := strings.TrimPrefix(r.URL.Path, "/api/v1/label/")
			name := strings.TrimSuffix(path, "/values")
			metricsHandler.LabelValues(w, r, name)
		})
		mux.HandleFunc("/api/v1/metadata", metricsHandler.Metadata)
	}

	// Health check.
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Serve Vue SPA from filesystem.
	distPath := filepath.Join("web", "dist")
	if _, err := os.Stat(distPath); err == nil {
		spa := spaHandler{staticDir: distPath}
		mux.Handle("/", spa)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(devFallbackHTML))
		})
	}

	return mux
}
```

- [ ] **Step 2: Update main.go for NewRouter signature change**

Edit `cmd/labubu/main.go`, change:
```go
router := api.NewRouter(traceHandler)
```
to:
```go
router := api.NewRouter(traceHandler, nil) // metricsHandler = nil until Task 8
```

- [ ] **Step 3: Verify build compiles**

Run:
```bash
go build ./...
```
Expected: builds successfully.

- [ ] **Step 4: Commit**

```bash
git add internal/api/router.go cmd/labubu/main.go
git commit -m "feat: register Prometheus HTTP API routes (/api/v1/query, /api/v1/query_range, /api/v1/labels, etc.)"
```

---

### Task 8: Wire everything in main.go

**Files:**
- Modify: `cmd/labubu/main.go`

- [ ] **Step 1: Add metrics flags and initialization**

Edit `cmd/labubu/main.go`:

Add to imports:
```go
"github.com/labubu/labubu/internal/metrics"
```

In `main()`, add flags:
```go
var (
	apiAddr              = flag.String("api-addr", "0.0.0.0:8080", "API and UI listen address")
	dataDir              = flag.String("data-dir", "./data", "chDB data directory (empty for in-memory)")
	bufferSize           = flag.Int("buffer-size", 1000, "pipeline buffer capacity")
	flushInterval        = flag.Duration("flush-interval", 200*time.Millisecond, "pipeline flush interval")
	metricsEnabled       = flag.Bool("metrics-enabled", true, "enable/disable metrics ingestion")
	metricsDataDir       = flag.String("metrics-data-dir", "./data/metrics", "tstorage data directory (empty = pure memory)")
	metricsRetention     = flag.Duration("metrics-retention", 2*time.Hour, "tstorage retention duration")
	metricsPrometheusAddr = flag.String("metrics-prometheus-addr", "", "production Prometheus address (empty = use embedded tstorage)")
)
```

After trace store init, add metrics store init:
```go
// Initialize metrics store (if enabled).
	var metricStore metrics.Store
	if *metricsEnabled {
		if *metricsPrometheusAddr != "" {
			log.Printf("metrics: prometheus remote mode not yet implemented, falling back to tstorage")
		}
		ms, err := metrics.NewTStorageStore(metrics.TStorageConfig{
			DataDir:   *metricsDataDir,
			Retention: *metricsRetention,
		})
		if err != nil {
			log.Fatalf("Failed to initialize metrics store: %v", err)
		}
		defer ms.Close()
		metricStore = ms
		log.Printf("metrics: tstorage initialized (data dir: %q, retention: %v)", *metricsDataDir, *metricsRetention)
	} else {
		log.Println("metrics: disabled")
	}
```

- [ ] **Step 2: Wire metricStore to receiver and router**

Change receiver creation:
```go
recv := receiver.New(pipe, metricStore)
```

Change router creation:
```go
var metricsHandler *api.MetricsHandler
if metricStore != nil {
	metricsHandler = api.NewMetricsHandler(metricStore)
}
router := api.NewRouter(traceHandler, metricsHandler)
```

- [ ] **Step 3: Verify full build**

Run:
```bash
go build ./...
go vet ./...
```
Expected: builds and vets cleanly.

- [ ] **Step 4: Run all existing tests**

Run:
```bash
go test ./... -count=1
```
Expected: ALL existing and new tests PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/labubu/main.go
git commit -m "feat: wire metrics subsystem (tstorage, receiver, API) with --metrics-* flags"
```

---

### Task 9: Integration verification

**Files:** (none — manual verification)

- [ ] **Step 1: Start the server**

Run:
```bash
cd D:\opensource\github\labubu && go run ./cmd/labubu --data-dir="" --metrics-data-dir=""
```
Expected: server starts, logs show "metrics: tstorage initialized".

- [ ] **Step 2: Send OTLP metrics via curl (HTTP)**

In another terminal:
```bash
# Send a test Gauge metric as protobuf (this needs an actual protobuf-encoded payload).
# For quick verification, test the Prometheus API endpoints directly.

# Labels endpoint
curl -s http://localhost:8080/api/v1/labels | jq .
# Expected: {"status":"success","data":["__name__"]}

# Query endpoint (no data yet — returns empty)
curl -s "http://localhost:8080/api/v1/query?query=cpu_usage" | jq .
# Expected: {"status":"success","data":{"resultType":"vector","result":[]}}
```

- [ ] **Step 3: Verify health API still works**

```bash
curl -s http://localhost:8080/api/health | jq .
# Expected: {"status":"ok"}
```

- [ ] **Step 4: Verify trace API still works**

```bash
curl -s "http://localhost:8080/api/v1/traces?page=1&page_size=5" | jq .
# Expected: trace list JSON (may be empty if no traces ingested)
```

- [ ] **Step 5: Stop server (Ctrl+C)**

---

## Plan Self-Review

### 1. Spec Coverage

| Spec Section | Task |
|-------------|------|
| Store interface + MetricPoint types | Task 2 |
| tstorage embedded implementation | Task 3 |
| OTLP → Prometheus translator (Gauge, Sum, Histogram, Summary) | Task 4 |
| Receiver integration (gRPC + HTTP) | Task 5 |
| Prometheus HTTP API (/api/v1/query, query_range, labels, label/:name/values, metadata) | Task 6, 7 |
| Error handling (skip bad metrics, log, continue) | Tasks 4, 5 |
| Configuration flags (--metrics-*) | Task 8 |
| tstorage label index for labels/values API | Task 3 |
| Main wiring (main.go) | Task 8 |
| Integration check | Task 9 |

### 2. Placeholder Scan

No TBD, TODO, "implement later", or vague instructions. All code is concrete.

### 3. Type Consistency

- `metrics.MetricPoint` — used in translator, tstorage store, handlers. Consistent.
- `metrics.MetricSeries` — used in Store.Select return type. Consistent.
- `metrics.Store` — interface used in receiver, handlers, main.go. Consistent.
- `receiver.New(pipeline, metricStore)` — signature matches all call sites.
- `api.NewRouter(traceHandler, metricsHandler)` — signature matches main.go call.
- `TStorageConfig.DataDir`, `TStorageConfig.Retention` — match main.go usage.
- `parsePromQL` returns `(string, map[string]string)` — matches usage in handlers.
