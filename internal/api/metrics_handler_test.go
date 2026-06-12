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
	points      []metrics.MetricPoint
	labelNames  []string
	labelValues []string
	insertErr   error
	selectErr   error
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
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/query?query=cpu_usage{host=%22a%22}&time="+strconv.FormatInt(now/1000, 10), nil)
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
	end := (now + 1000) / 1000
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/query_range?query=cpu_usage{host=%%22a%%22}&start=%d&end=%d&step=1", start, end), nil)
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
		t.Errorf("expected status 'success', got %q - body: %s", resp.Status, rec.Body.String())
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

func TestMetricsHandler_MetricNames(t *testing.T) {
	store := &metricsMockStore{
		labelValues: []string{"cpu_usage", "memory_usage"},
	}

	handler := NewMetricsHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metric-names", nil)
	rec := httptest.NewRecorder()

	handler.MetricNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "success" {
		t.Errorf("expected status 'success', got %v", resp["status"])
	}
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data to be array, got %T", resp["data"])
	}
	if len(data) != 2 {
		t.Errorf("expected 2 metric names, got %d", len(data))
	}
}

func TestParsePromQLSimple(t *testing.T) {
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
		name, labels := parsePromQLCompat(tt.input)
		if name != tt.expectedName {
			t.Errorf("parsePromQLCompat(%q) name: got %q, want %q", tt.input, name, tt.expectedName)
		}
		if tt.expectedLabel != "" {
			if val, ok := labels[tt.expectedLabel]; !ok || val != tt.expectedValue {
				t.Errorf("parsePromQLCompat(%q) label %s: got %q, want %q", tt.input, tt.expectedLabel, val, tt.expectedValue)
			}
		}
	}
}

func TestParsePromQLExtended(t *testing.T) {
	tests := []struct {
		input       string
		wantName    string
		wantFunc    string
		wantAgg     string
		wantGroupBy string
		wantRatio   bool
		wantNumName string
	}{
		{"cpu_usage{host=\"a\"}", "cpu_usage", "", "", "", false, ""},
		{"rate(cpu_usage{host=\"a\"}[5m])", "cpu_usage", "rate", "", "", false, ""},
		{"increase(cpu_usage[5m])", "cpu_usage", "increase", "", "", false, ""},
		{"sum(rate(cpu_usage{host=\"a\"}[5m]))", "cpu_usage", "rate", "sum", "", false, ""},
		{"sum(rate(cpu_usage{host=\"a\"}[5m])) by (service)", "cpu_usage", "rate", "sum", "service", false, ""},
		{"avg(rate(requests[5m])) by (model)", "requests", "rate", "avg", "model", false, ""},
		{"rate(success[5m]) / rate(total[5m])", "total", "rate", "", "", true, "success"},
		{"sum(rate(success[5m])) by (service) / sum(rate(total[5m])) by (service)", "total", "rate", "sum", "service", true, "success"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			q := parsePromQL(tt.input)
			if q.MetricName != tt.wantName {
				t.Errorf("MetricName: got %q, want %q", q.MetricName, tt.wantName)
			}
			if q.Func != tt.wantFunc {
				t.Errorf("Func: got %q, want %q", q.Func, tt.wantFunc)
			}
			if q.Aggregation != tt.wantAgg {
				t.Errorf("Aggregation: got %q, want %q", q.Aggregation, tt.wantAgg)
			}
			if q.GroupBy != tt.wantGroupBy {
				t.Errorf("GroupBy: got %q, want %q", q.GroupBy, tt.wantGroupBy)
			}
			if q.IsRatio != tt.wantRatio {
				t.Errorf("IsRatio: got %v, want %v", q.IsRatio, tt.wantRatio)
			}
			if q.NumMetricName != tt.wantNumName {
				t.Errorf("NumMetricName: got %q, want %q", q.NumMetricName, tt.wantNumName)
			}
		})
	}
}
