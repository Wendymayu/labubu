package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	ilog "github.com/labubu/labubu/internal/log"
	"github.com/labubu/labubu/internal/metrics"
	"github.com/labubu/labubu/internal/receiver"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// Prometheus-compatible API response types.

type prometheusResponse struct {
	Status string         `json:"status"`
	Data   prometheusData `json:"data,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type prometheusData struct {
	ResultType string             `json:"resultType"`
	Result     []prometheusResult `json:"result"`
}

type prometheusResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value,omitempty"`  // [timestamp, "value"]
	Values [][]interface{}   `json:"values,omitempty"` // range query rows
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

	closeIdx := strings.LastIndexByte(rest, '}')
	if closeIdx < 0 {
		return name, labels
	}
	rest = rest[:closeIdx]

	// Parse label pairs: key="value", key2="value2"
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

// parseTime parses time from query string (seconds Unix), returns milliseconds.
func parseTime(r *http.Request, key string) (int64, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0, fmt.Errorf("missing parameter %q", key)
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %q: %w", key, err)
	}
	return sec * 1000, nil
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

	ts, err := parseTime(r, "time")
	if err != nil {
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

	start := ts - 5000
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
	bestDiff := abs64(best.Timestamp - target)
	for i := 1; i < len(points); i++ {
		diff := abs64(points[i].Timestamp - target)
		if diff < bestDiff {
			bestDiff = diff
			best = &points[i]
		}
	}
	return best
}

func abs64(x int64) int64 {
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

	start, err := parseTime(r, "start")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, prometheusResponse{Status: "error", Error: fmt.Sprintf("invalid start: %v", err)})
		return
	}
	end, err := parseTime(r, "end")
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

	writeLabelsJSON(w, http.StatusOK, names)
}

// writeLabelsJSON writes a Prometheus labels/values response.
func writeLabelsJSON(w http.ResponseWriter, status int, data []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"data":   data,
	})
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
	writeLabelsJSON(w, http.StatusOK, values)
}

// Metadata handles GET /api/v1/metadata
func (h *MetricsHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
		"data":   map[string]interface{}{},
	})
}

// IngestOTLP handles POST /api/v1/otlp/v1/metrics — Prometheus-compatible OTLP metrics ingestion.
func (h *MetricsHandler) IngestOTLP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req colmetricspb.ExportMetricsServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("failed to unmarshal: %v", err), http.StatusBadRequest)
		return
	}

	points := receiver.TranslateMetrics(&req)
	ilog.Debug("metrics OTLP API: received %d metric points", len(points))
	for i, p := range points {
		if i >= 10 {
			ilog.Debug("metrics OTLP API: ... (%d more points omitted)", len(points)-10)
			break
		}
		ilog.Debug("metrics OTLP API:   %s{%v} = %f @ %d", p.Name, p.Labels, p.Value, p.Timestamp)
	}

	if len(points) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}

	if err := h.store.Insert(r.Context(), points); err != nil {
		log.Printf("metrics: otlp ingest error: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store insert failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{})
}
