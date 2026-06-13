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
		name, labels := parsePromQLSimple(tt.input)
		if name != tt.expectedName {
			t.Errorf("parsePromQLSimple(%q) name: got %q, want %q", tt.input, name, tt.expectedName)
		}
		if tt.expectedLabel != "" {
			if val, ok := labels[tt.expectedLabel]; !ok || val != tt.expectedValue {
				t.Errorf("parsePromQLSimple(%q) label %s: got %q, want %q", tt.input, tt.expectedLabel, val, tt.expectedValue)
			}
		}
	}
}

func TestApplyRate(t *testing.T) {
	points := []metrics.MetricPoint{
		{Name: "requests", Value: 100, Timestamp: 600_000},
		{Name: "requests", Value: 110, Timestamp: 900_000},
		{Name: "requests", Value: 120, Timestamp: 1_200_000},
		{Name: "requests", Value: 130, Timestamp: 1_500_000},
	}
	result := applyRate(points, 300_000)
	if len(result) < 1 {
		t.Fatal("expected at least 1 rate point")
	}
	approxEqual(t, result[0].Value, 10.0/300.0, 0.001)
}

func TestApplyIncrease(t *testing.T) {
	points := []metrics.MetricPoint{
		{Name: "requests", Value: 100, Timestamp: 600_000},
		{Name: "requests", Value: 110, Timestamp: 900_000},
		{Name: "requests", Value: 120, Timestamp: 1_200_000},
	}
	result := applyIncrease(points, 300_000)
	if len(result) < 1 {
		t.Fatal("expected at least 1 increase point")
	}
	approxEqual(t, result[0].Value, 10.0, 0.001)
}

func TestApplyAggregation(t *testing.T) {
	series := []metrics.MetricSeries{
		{Name: "requests", Labels: map[string]string{"service": "api"}, Points: []metrics.MetricPoint{
			{Value: 10, Timestamp: 1000}, {Value: 20, Timestamp: 2000},
		}},
		{Name: "requests", Labels: map[string]string{"service": "web"}, Points: []metrics.MetricPoint{
			{Value: 30, Timestamp: 1000}, {Value: 40, Timestamp: 2000},
		}},
	}
	sumResult := applyAggregation(series, "sum", "")
	if len(sumResult) != 1 {
		t.Fatalf("expected 1 series for sum without groupBy, got %d", len(sumResult))
	}
	ts1000 := findPointAt(sumResult[0].Points, 1000)
	ts2000 := findPointAt(sumResult[0].Points, 2000)
	approxEqual(t, ts1000, 40.0, 0.001)
	approxEqual(t, ts2000, 60.0, 0.001)

	sumGrouped := applyAggregation(series, "sum", "service")
	if len(sumGrouped) != 2 {
		t.Fatalf("expected 2 series for sum by service, got %d", len(sumGrouped))
	}
}

func TestApplyRatio(t *testing.T) {
	numSeries := []metrics.MetricSeries{
		{Name: "success", Labels: nil, Points: []metrics.MetricPoint{
			{Value: 80, Timestamp: 1000}, {Value: 90, Timestamp: 2000},
		}},
	}
	denSeries := []metrics.MetricSeries{
		{Name: "total", Labels: nil, Points: []metrics.MetricPoint{
			{Value: 100, Timestamp: 1000}, {Value: 100, Timestamp: 2000},
		}},
	}
	result := applyRatio(numSeries, denSeries)
	if len(result) != 1 {
		t.Fatalf("expected 1 ratio series, got %d", len(result))
	}
	if len(result[0].Points) != 2 {
		t.Fatalf("expected 2 ratio points, got %d", len(result[0].Points))
	}
	approxEqual(t, result[0].Points[0].Value, 0.8, 0.001)
	approxEqual(t, result[0].Points[1].Value, 0.9, 0.001)
}

func TestApplyRatio_DivisionByZero(t *testing.T) {
	numSeries := []metrics.MetricSeries{
		{Name: "success", Labels: nil, Points: []metrics.MetricPoint{
			{Value: 80, Timestamp: 1000}, {Value: 90, Timestamp: 2000},
		}},
	}
	denSeries := []metrics.MetricSeries{
		{Name: "total", Labels: nil, Points: []metrics.MetricPoint{
			{Value: 100, Timestamp: 1000}, {Value: 0, Timestamp: 2000},
		}},
	}
	result := applyRatio(numSeries, denSeries)
	if len(result) != 1 {
		t.Fatalf("expected 1 ratio series, got %d", len(result))
	}
	if len(result[0].Points) != 1 {
		t.Fatalf("expected 1 ratio point (division by zero skips), got %d", len(result[0].Points))
	}
	approxEqual(t, result[0].Points[0].Value, 0.8, 0.001)
}

func TestImprovedDownsampling(t *testing.T) {
	points := []metrics.MetricPoint{
		{Value: 10, Timestamp: 0}, {Value: 20, Timestamp: 1000}, {Value: 30, Timestamp: 2000},
	}
	result := downsamplePoints(points, 0, 2000, 2000)
	if len(result) != 2 {
		t.Fatalf("expected 2 downsampled points, got %d", len(result))
	}
	val0, _ := strconv.ParseFloat(result[0][1].(string), 64)
	val2, _ := strconv.ParseFloat(result[1][1].(string), 64)
	approxEqual(t, val0, 15.0, 0.001)
	approxEqual(t, val2, 25.0, 0.001)
}

func approxEqual(t *testing.T, got, want, tolerance float64) {
	t.Helper()
	diff := got - want
	if diff < -tolerance || diff > tolerance {
		t.Errorf("approxEqual: got %f, want %f (tolerance %f)", got, want, tolerance)
	}
}

func findPointAt(points []metrics.MetricPoint, ts int64) float64 {
	for _, p := range points {
		if p.Timestamp == ts {
			return p.Value
		}
	}
	return -1
}

func TestParsePromQLExtended(t *testing.T) {
	tests := []struct {
		input          string
		wantName       string
		wantFunc       string
		wantWindow     int64
		wantAgg        string
		wantGroupBy    string
		wantRatio      bool
		wantNumName    string
		wantNumLabels  map[string]string
	}{
		{"cpu_usage{host=\"a\"}", "cpu_usage", "", 0, "", "", false, "", nil},
		{"rate(cpu_usage{host=\"a\"}[5m])", "cpu_usage", "rate", 300000, "", "", false, "", nil},
		{"increase(cpu_usage[5m])", "cpu_usage", "increase", 300000, "", "", false, "", nil},
		{"sum(rate(cpu_usage{host=\"a\"}[5m]))", "cpu_usage", "rate", 300000, "sum", "", false, "", nil},
		{"sum(rate(cpu_usage{host=\"a\"}[5m])) by (service)", "cpu_usage", "rate", 300000, "sum", "service", false, "", nil},
		{"avg(rate(requests[5m])) by (model)", "requests", "rate", 300000, "avg", "model", false, "", nil},
		{"rate(success[5m]) / rate(total[5m])", "total", "rate", 300000, "", "", true, "success", nil},
		{"sum(rate(success[5m])) by (service) / sum(rate(total[5m])) by (service)", "total", "rate", 300000, "sum", "service", true, "success", nil},
		{"rate(success{service=\"api\"}[5m]) / rate(total{service=\"api\"}[5m])", "total", "rate", 300000, "", "", true, "success", map[string]string{"service": "api"}},
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
			if q.Window != tt.wantWindow {
				t.Errorf("Window: got %d, want %d", q.Window, tt.wantWindow)
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
			if tt.wantNumLabels != nil {
				if len(q.NumLabels) != len(tt.wantNumLabels) {
					t.Errorf("NumLabels: got %d keys, want %d keys", len(q.NumLabels), len(tt.wantNumLabels))
				}
				for k, v := range tt.wantNumLabels {
					if got, ok := q.NumLabels[k]; !ok || got != v {
						t.Errorf("NumLabels[%q]: got %q, want %q", k, got, v)
					}
				}
			}
		})
	}
}

