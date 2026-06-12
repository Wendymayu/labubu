package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
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

// parsedQuery holds the result of parsing a PromQL expression with
// function, aggregation, and ratio support.
type parsedQuery struct {
	MetricName    string
	Labels        map[string]string
	Func          string // "" | "rate" | "increase"
	Window        int64  // lookback in ms (300_000 for 5m)
	Aggregation   string // "" | "sum" | "avg" | "max" | "min"
	GroupBy       string // label key for aggregation grouping
	IsRatio       bool
	NumMetricName string            // numerator metric (ratio only)
	NumLabels     map[string]string // numerator labels (ratio only)
}

// MetricsHandler holds HTTP handlers for Prometheus API endpoints.
type MetricsHandler struct {
	store metrics.Store
}

// NewMetricsHandler creates a new MetricsHandler.
func NewMetricsHandler(store metrics.Store) *MetricsHandler {
	return &MetricsHandler{store: store}
}

// parsePromQLSimple extracts the metric name and label filters from a simple
// PromQL expression (metric_name and metric_name{label="value",...} patterns).
// It is used as an inner helper by the extended parser.
func parsePromQLSimple(query string) (string, map[string]string) {
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

// parsePromQLCompat is a backward-compatible wrapper returning the old
// (string, map[string]string) signature. It uses the new parser but
// discards the extended fields so that existing handlers continue working
// until Task 3 updates them.
func parsePromQLCompat(query string) (string, map[string]string) {
	q := parsePromQL(query)
	return q.MetricName, q.Labels
}

// parsePromQL parses a PromQL expression and returns a parsedQuery struct
// supporting functions (rate, increase), aggregations (sum, avg, max, min),
// and ratio expressions (a / b).
func parsePromQL(query string) parsedQuery {
	q := parsedQuery{Labels: make(map[string]string)}

	// Check for binary division (ratio expression).
	if left, right, ok := splitRatio(query); ok {
		q.IsRatio = true
		leftQ := parseSingleExpr(left)
		rightQ := parseSingleExpr(right)
		q.NumMetricName = leftQ.MetricName
		q.NumLabels = leftQ.Labels
		q.Func = leftQ.Func
		q.Window = leftQ.Window
		q.MetricName = rightQ.MetricName
		q.Labels = rightQ.Labels
		q.Aggregation = leftQ.Aggregation
		q.GroupBy = leftQ.GroupBy
		return q
	}

	sq := parseSingleExpr(query)
	q.MetricName = sq.MetricName
	q.Labels = sq.Labels
	q.Func = sq.Func
	q.Window = sq.Window
	q.Aggregation = sq.Aggregation
	q.GroupBy = sq.GroupBy
	return q
}

// parseSingleExpr parses a single (non-ratio) PromQL expression, extracting
// any aggregation wrapper, function wrapper, time window, and the inner
// metric{labels} selector.
func parseSingleExpr(expr string) parsedQuery {
	q := parsedQuery{Labels: make(map[string]string)}
	expr = strings.TrimSpace(expr)

	// Known aggregation operators.
	aggs := []string{"sum", "avg", "max", "min"}
	for _, agg := range aggs {
		if inner, groupBy, ok := unwrapAggregation(expr, agg); ok {
			q.Aggregation = agg
			q.GroupBy = groupBy
			expr = strings.TrimSpace(inner)
			break
		}
	}

	// Known function wrappers.
	fns := []string{"rate", "increase"}
	for _, fn := range fns {
		if inner, ok := unwrapFunction(expr, fn); ok {
			q.Func = fn
			expr = strings.TrimSpace(inner)
			// Extract and strip the [5m] window.
			windowRe := regexp.MustCompile(`\[\d+m\]`)
			if loc := windowRe.FindStringIndex(expr); loc != nil {
				windowStr := expr[loc[0]:loc[1]]
				// Parse the minute value from e.g. "[5m]".
				minStr := windowStr[1 : len(windowStr)-1]
				mins, err := strconv.ParseInt(minStr, 10, 64)
				if err == nil {
					q.Window = mins * 60 * 1000 // minutes → ms
				}
				expr = expr[:loc[0]] + expr[loc[1]:]
			}
			break
		}
	}

	// Parse the remaining metric{labels} using the simple parser.
	name, labels := parsePromQLSimple(strings.TrimSpace(expr))
	q.MetricName = name
	q.Labels = labels
	return q
}

// splitRatio splits a binary division expression (a / b), respecting
// parentheses depth so that nested functions/aggregations are not broken.
func splitRatio(query string) (left, right string, ok bool) {
	query = strings.TrimSpace(query)

	// Find the top-level '/' operator that is not inside parentheses.
	depth := 0
	slashIdx := -1
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
		} else if ch == '/' && depth == 0 {
			slashIdx = i
			break
		}
	}
	if slashIdx < 0 {
		return "", "", false
	}
	left = strings.TrimSpace(query[:slashIdx])
	right = strings.TrimSpace(query[slashIdx+1:])
	if left == "" || right == "" {
		return "", "", false
	}
	return left, right, true
}

// unwrapAggregation extracts the inner expression and group-by label from
// an aggregation wrapper like "sum(rate(...)) by (service)".
// Returns (inner, groupBy, true) if the expression starts with agg(...).
func unwrapAggregation(expr, agg string) (inner, groupBy string, ok bool) {
	prefix := agg + "("
	if !strings.HasPrefix(expr, prefix) {
		return "", "", false
	}

	// Find the closing ')' that matches the opening '(' after agg.
	depth := 1
	openIdx := len(prefix) - 1 // index of '(' after agg
	closeIdx := -1
	for i := openIdx + 1; i < len(expr); i++ {
		if expr[i] == '(' {
			depth++
		} else if expr[i] == ')' {
			depth--
			if depth == 0 {
				closeIdx = i
				break
			}
		}
	}
	if closeIdx < 0 {
		return "", "", false
	}

	innerExpr := expr[openIdx+1:closeIdx]
	rest := strings.TrimSpace(expr[closeIdx+1:])

	// Check for "by (label)" or "by(label)" suffix.
	groupBy = ""
	byRe := regexp.MustCompile(`^\s*by\s*\(\s*([^)]+)\s*\)`)
	if m := byRe.FindStringSubmatch(rest); m != nil {
		groupBy = strings.TrimSpace(m[1])
	}

	return innerExpr, groupBy, true
}

// unwrapFunction extracts the inner expression from a function wrapper like
// "rate(metric{labels}[5m])". Returns (inner, true) if expr starts with fn(...).
func unwrapFunction(expr, fn string) (inner string, ok bool) {
	prefix := fn + "("
	if !strings.HasPrefix(expr, prefix) {
		return "", false
	}

	// Find the matching closing ')'.
	depth := 1
	openIdx := len(prefix) - 1
	closeIdx := -1
	for i := openIdx + 1; i < len(expr); i++ {
		if expr[i] == '(' {
			depth++
		} else if expr[i] == ')' {
			depth--
			if depth == 0 {
				closeIdx = i
				break
			}
		}
	}
	if closeIdx < 0 {
		return "", false
	}

	return expr[openIdx+1:closeIdx], true
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

	metricName, labels := parsePromQLCompat(query)
	if metricName == "" {
		writeJSON(w, http.StatusOK, prometheusResponse{
			Status: "success",
			Data:   prometheusData{ResultType: "vector", Result: []prometheusResult{}},
		})
		return
	}

	// Look back up to 1 hour to find the most recent data point.
	// A narrow window (e.g. 5 s) would miss data ingested more than
	// a few seconds ago and cause stat cards to show "No data".
	start := ts - 3600_000
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

	metricName, labels := parsePromQLCompat(query)
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
		var val float64
		if best != nil && abs64(best.Timestamp-t) <= step {
			val = best.Value
		}
		// Always emit a point for each step; 0 when no data nearby.
		values = append(values, []interface{}{
			float64(t) / 1000.0,
			strconv.FormatFloat(val, 'f', -1, 64),
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

// MetricNames handles GET /api/v1/metric-names
func (h *MetricsHandler) MetricNames(w http.ResponseWriter, r *http.Request) {
	values, err := h.store.LabelValues(r.Context(), "__name__")
	if err != nil {
		log.Printf("metrics: metric-names error: %v", err)
		writeJSON(w, http.StatusInternalServerError, prometheusResponse{
			Status: "error",
			Error:  fmt.Sprintf("metric names failed: %v", err),
		})
		return
	}
	if values == nil {
		values = []string{}
	}
	writeLabelsJSON(w, http.StatusOK, values)
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
