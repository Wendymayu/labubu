# Dashboard PromQL Functions & Ratio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend Dashboard panels to support PromQL functions (rate/increase), aggregations (sum/avg/max/min), and ratio expressions (metric_a / metric_b), enabling request rates, success rates, and grouped aggregations.

**Architecture:** Backend extends `parsePromQL` in `metrics_handler.go` to parse function/aggregation/ratio expressions, then computes rate/increase/aggregation/ratio in the `RangeQuery` and `InstantQuery` handlers. Frontend `PanelForm.vue` adds expression type, function, aggregation, and group-by selectors; `PanelChart.vue` constructs richer PromQL strings. `dashboard_handler.go` PanelConfig struct gains new fields with backward-compatible defaults.

**Tech Stack:** Go backend (metrics_handler.go) + Vue 3 frontend (PanelForm + PanelChart) + Chart.js (unchanged)

---

## File Structure

| File | Responsibility | Change Type |
|------|---------------|-------------|
| `internal/api/metrics_handler.go` | Extended PromQL parser + rate/increase/aggregation/ratio computation in RangeQuery & InstantQuery + improved downsampling | Modify |
| `internal/api/metrics_handler_test.go` | Tests for extended parsePromQL, rate/increase computation, aggregation, ratio, downsampling | Modify |
| `internal/api/dashboard_handler.go` | PanelConfig struct extended with new fields; createPanel validation updated | Modify |
| `internal/api/dashboard_handler_test.go` | Tests for ratio panel validation + backward compatibility | Modify |
| `web/src/api/client.ts` | PanelConfig TypeScript interface extended | Modify |
| `web/src/components/PanelForm.vue` | Expression type, numerator/denominator, function, aggregation, group-by selectors | Modify |
| `web/src/components/PanelChart.vue` | buildPromQL replaced with structured builder; ratio stat display | Modify |
| `web/src/i18n/locales/en.ts` | panelForm i18n keys | Modify |
| `web/src/i18n/locales/zh.ts` | panelForm i18n keys | Modify |

---

## Task 1: Extended PanelConfig Struct (Backend)

**Files:**
- Modify: `internal/api/dashboard_handler.go:17-24`
- Modify: `internal/api/dashboard_handler_test.go`

- [ ] **Step 1: Extend PanelConfig struct in dashboard_handler.go**

Replace the current struct (lines 17-24) with:

```go
type PanelConfig struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	ExpressionType  string            `json:"expressionType"`           // "single" | "ratio"
	Metric          string            `json:"metric"`                   // Single: main metric; Ratio: denominator
	NumeratorMetric string            `json:"numeratorMetric,omitempty"` // Ratio: numerator
	Labels          map[string]string `json:"labels"`
	Func            string            `json:"func"`                     // "none" | "rate" | "increase"
	Aggregation     string            `json:"aggregation"`              // "none" | "sum" | "avg" | "max" | "min"
	GroupBy         string            `json:"groupBy,omitempty"`        // label key for aggregation grouping
	ChartType       string            `json:"chartType"`
	Step            int               `json:"step,omitempty"`
}
```

- [ ] **Step 2: Update createPanel validation (lines 361-389)**

Replace the current `createPanel` method with extended validation:

```go
func (h *DashboardHandler) createPanel(w http.ResponseWriter, r *http.Request, dashboardID string) {
	var pc PanelConfig
	if err := json.NewDecoder(r.Body).Decode(&pc); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if pc.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if pc.Metric == "" {
		http.Error(w, "metric is required", http.StatusBadRequest)
		return
	}
	// Default expressionType to "single" for backward compatibility.
	if pc.ExpressionType == "" {
		pc.ExpressionType = "single"
	}
	if pc.ExpressionType != "single" && pc.ExpressionType != "ratio" {
		http.Error(w, "expressionType must be single or ratio", http.StatusBadRequest)
		return
	}
	if pc.ExpressionType == "ratio" && pc.NumeratorMetric == "" {
		http.Error(w, "numeratorMetric is required for ratio expressions", http.StatusBadRequest)
		return
	}
	// Default func and aggregation to "none" for backward compatibility.
	if pc.Func == "" {
		pc.Func = "none"
	}
	if pc.Aggregation == "" {
		pc.Aggregation = "none"
	}
	if pc.ChartType != "line" && pc.ChartType != "bar" && pc.ChartType != "stat" {
		http.Error(w, "chartType must be line, bar, or stat", http.StatusBadRequest)
		return
	}

	pc.ID = uuid.NewString()

	if err := h.savePanel(dashboardID, &pc); err != nil {
		log.Printf("dashboards: save panel error: %v", err)
		http.Error(w, "failed to save panel", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, pc)
}
```

- [ ] **Step 3: Write test for ratio panel validation**

Add to `dashboard_handler_test.go`:

```go
func TestDashboardHandler_RatioPanelValidation(t *testing.T) {
	dir := setupDashboardTestDir(t)
	handler := NewDashboardHandler(dir)

	// Create dashboard.
	rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards", map[string]string{"name": "Test"})
	handler.ServeHTTP(rec, req)
	var dash struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &dash)

	tests := []struct {
		name string
		body map[string]interface{}
		code int
	}{
		{
			"valid ratio panel",
			map[string]interface{}{
				"title": "Success Rate", "expressionType": "ratio",
				"metric": "total_requests", "numeratorMetric": "success_requests",
				"func": "rate", "aggregation": "none", "chartType": "line", "step": 60,
			},
			http.StatusOK,
		},
		{
			"ratio missing numeratorMetric",
			map[string]interface{}{
				"title": "Bad Ratio", "expressionType": "ratio",
				"metric": "total_requests", "chartType": "line",
			},
			http.StatusBadRequest,
		},
		{
			"single panel defaults",
			map[string]interface{}{
				"title": "CPU", "metric": "cpu_usage", "chartType": "line",
			},
			http.StatusOK,
		},
		{
			"invalid expressionType",
			map[string]interface{}{
				"title": "Bad", "expressionType": "unknown",
				"metric": "cpu", "chartType": "line",
			},
			http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, req := doJSON(t, http.MethodPost, "/api/v1/dashboards/"+dash.ID+"/panels", tt.body)
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.code {
				t.Errorf("expected %d, got %d: %s", tt.code, rec.Code, rec.Body.String())
			}
		})
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd D:/code/opensource/github/labubu && go test -v -run TestDashboardHandler ./internal/api/
```

Expected: All existing + new tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/api/dashboard_handler.go internal/api/dashboard_handler_test.go
git commit -m "feat: extend PanelConfig with expressionType, func, aggregation, groupBy fields"
```

---

## Task 2: Extended PromQL Parser (Backend)

**Files:**
- Modify: `internal/api/metrics_handler.go:49-81`
- Modify: `internal/api/metrics_handler_test.go:228-252`

- [ ] **Step 1: Add parsedQuery struct**

Add after the `prometheusResult` struct (around line 37) in `metrics_handler.go`:

```go
// parsedQuery represents a structured PromQL expression.
type parsedQuery struct {
	MetricName    string
	Labels        map[string]string
	Func          string // "" | "rate" | "increase"
	Window        int64  // lookback in ms (300_000 for 5m)
	Aggregation   string // "" | "sum" | "avg" | "max" | "min"
	GroupBy       string // label key for aggregation grouping
	IsRatio       bool
	NumMetricName string            // numerator metric (ratio only)
	NumLabels     map[string]string // numerator labels (ratio only, same as Labels)
}
```

- [ ] **Step 2: Replace parsePromQL with parsePromQLExtended**

Add the new function after the existing `parsePromQL` (keep the old one for backward compat but rename it):

```go
// parsePromQLSimple extracts the metric name and label filters from a simple PromQL query.
// Only supports: metric_name and metric_name{label="value",...} patterns.
// This is the original parser, kept for simple expressions.
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

// parsePromQL parses a PromQL expression into a structured parsedQuery.
// Supports: metric{labels}, func(metric{labels}[5m]), agg(func(metric{labels}[5m])),
// agg(func(metric{labels}[5m])) by (dim), and binary division for ratio expressions.
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

// parseSingleExpr parses a single (non-ratio) PromQL expression.
func parseSingleExpr(expr string) parsedQuery {
	q := parsedQuery{Labels: make(map[string]string)}
	expr = strings.TrimSpace(expr)

	// Try to extract aggregation wrapper: agg(...) or agg(...) by (dim)
	aggregationNames := []string{"sum", "avg", "max", "min"}
	for _, agg := range aggregationNames {
		if !strings.HasPrefix(expr, agg+"(") {
			continue
		}
		// Find the matching closing paren.
		inner, groupBy, ok := unwrapAggregation(expr, agg)
		if !ok {
			continue
		}
		q.Aggregation = agg
		q.GroupBy = groupBy
		expr = strings.TrimSpace(inner)
		break
	}

	// Try to extract function wrapper: rate(...) or increase(...)
	funcNames := []string{"rate", "increase"}
	for _, fn := range funcNames {
		if !strings.HasPrefix(expr, fn+"(") {
			continue
		}
		inner, ok := unwrapFunction(expr, fn)
		if !ok {
			continue
		}
		q.Func = fn
		q.Window = 300_000 // fixed 5m lookback
		expr = strings.TrimSpace(inner)
		break
	}

	// Strip [5m] window suffix if present.
	expr = strings.TrimSuffix(expr, "[5m]")
	expr = strings.TrimSpace(expr)

	// Parse remaining metric{labels} with the simple parser.
	q.MetricName, q.Labels = parsePromQLSimple(expr)
	return q
}

// splitRatio splits a binary division expression like "a / b".
func splitRatio(query string) (left, right string, ok bool) {
	// Find the division operator. Must not be inside parentheses.
	depth := 0
	for i := 0; i < len(query); i++ {
		switch query[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '/':
			if depth == 0 {
				left = strings.TrimSpace(query[:i])
				right = strings.TrimSpace(query[i+1:])
				if left != "" && right != "" {
					return left, right, true
				}
			}
		}
	}
	return "", "", false
}

// unwrapAggregation extracts inner expression and group-by from agg(expr) or agg(expr) by (dim).
func unwrapAggregation(expr, agg string) (inner, groupBy string, ok bool) {
	// Pattern: agg(inner) by (dim)  or  agg(inner)
	prefix := agg + "("
	if !strings.HasPrefix(expr, prefix) {
		return "", "", false
	}

	// Find matching close paren for agg(...).
	depth := 1
	closeIdx := -1
	for i := len(prefix); i < len(expr); i++ {
		switch expr[i] {
		case '(':
			depth++
		case ')':
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

	inner = expr[len(prefix):closeIdx]

	// Check for "by (dim)" after the closing paren.
	rest := strings.TrimSpace(expr[closeIdx+1:])
	if strings.HasPrefix(rest, "by (") {
		byOpen := strings.Index(rest, "(")
		byClose := strings.Index(rest, ")")
		if byClose > byOpen {
			groupBy = strings.TrimSpace(rest[byOpen+1:byClose])
		}
	}

	return inner, groupBy, true
}

// unwrapFunction extracts inner expression from func(expr).
func unwrapFunction(expr, fn string) (inner string, ok bool) {
	prefix := fn + "("
	if !strings.HasPrefix(expr, prefix) {
		return "", false
	}

	// Find matching close paren.
	depth := 1
	closeIdx := -1
	for i := len(prefix); i < len(expr); i++ {
		switch expr[i] {
		case '(':
			depth++
		case ')':
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

	return expr[len(prefix):closeIdx], true
}
```

- [ ] **Step 3: Update the existing parsePromQL callers**

The existing code in `InstantQuery` and `RangeQuery` calls `parsePromQL(query)` returning `(string, map[string]string)`. This must be updated to use the new `parsedQuery` return type. We'll do this in Task 3 when we add computation logic. For now, add a compatibility wrapper:

```go
// parsePromQLCompat is a backward-compatible wrapper that returns (metricName, labels).
// Used only by legacy callers until they are updated in Task 3.
func parsePromQLCompat(query string) (string, map[string]string) {
	q := parsePromQL(query)
	return q.MetricName, q.Labels
}
```

And rename the original function body to `parsePromQLSimple` as shown above.

- [ ] **Step 4: Write tests for the extended parser**

Add to `metrics_handler_test.go`:

```go
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
```

- [ ] **Step 5: Run tests**

```bash
cd D:/code/opensource/github/labubu && go test -v -run TestParsePromQL ./internal/api/
```

Expected: All existing `TestParsePromQL` tests still pass (using the simple parser path), new `TestParsePromQLExtended` tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/api/metrics_handler.go internal/api/metrics_handler_test.go
git commit -m "feat: extend parsePromQL to support rate/increase, aggregation, and ratio expressions"
```

---

## Task 3: Rate/Increase/Aggregation/Ratio Computation (Backend)

**Files:**
- Modify: `internal/api/metrics_handler.go` (RangeQuery, InstantQuery, downsamplePoints)

This is the core computation task. The steps are ordered: first downsample improvement, then rate/increase, then aggregation, then ratio. Each is layered on the previous.

- [ ] **Step 1: Improve downsamplePoints**

Replace the current `downsamplePoints` function (lines 252-271) with window-averaging version:

```go
// downsamplePoints computes one value per step interval using window averaging.
// For each step timestamp t, it averages all points within [t-step/2, t+step/2].
// Points with no nearby data are skipped (not filled with 0).
func downsamplePoints(points []metrics.MetricPoint, start, end, step int64) [][]interface{} {
	if len(points) == 0 {
		return nil
	}

	var values [][]interface{}
	for t := start; t <= end; t += step {
		windowStart := t - step/2
		windowEnd := t + step/2
		sum := 0.0
		count := 0
		for _, p := range points {
			if p.Timestamp >= windowStart && p.Timestamp <= windowEnd {
				sum += p.Value
				count++
			}
		}
		if count == 0 {
			continue // skip gaps instead of filling 0
		}
		values = append(values, []interface{}{
			float64(t) / 1000.0,
			strconv.FormatFloat(sum/float64(count), 'f', -1, 64),
		})
	}
	return values
}
```

- [ ] **Step 2: Add rate/increase computation function**

Add after `downsamplePoints`:

```go
const lookbackWindowMS = 300_000 // 5 minutes in ms

// applyRate computes rate values: (value_at_t - value_at_t-lookback) / lookback_seconds.
// Returns points only where a lookback point exists within the window.
func applyRate(points []metrics.MetricPoint, lookbackMS int64) []metrics.MetricPoint {
	if len(points) == 0 {
		return nil
	}
	lookbackSec := float64(lookbackMS) / 1000.0
	result := make([]metrics.MetricPoint, 0)
	for _, p := range points {
		// Find the closest point at p.Timestamp - lookback.
		target := p.Timestamp - lookbackMS
		bestIdx := -1
		bestDiff := int64(60_000) // allow 60s tolerance
		for i, pp := range points {
			diff := abs64(pp.Timestamp - target)
			if diff < bestDiff {
				bestDiff = diff
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			continue // no lookback point available
		}
		delta := p.Value - points[bestIdx].Value
		if delta < 0 {
			delta = 0 // counter resets: treat as 0 delta
		}
		rateVal := delta / lookbackSec
		result = append(result, metrics.MetricPoint{
			Name:      p.Name,
			Labels:    p.Labels,
			Value:     rateVal,
			Timestamp: p.Timestamp,
		})
	}
	return result
}

// applyIncrease computes increase values: value_at_t - value_at_t-lookback.
// Returns points only where a lookback point exists within the window.
func applyIncrease(points []metrics.MetricPoint, lookbackMS int64) []metrics.MetricPoint {
	if len(points) == 0 {
		return nil
	}
	result := make([]metrics.MetricPoint, 0)
	for _, p := range points {
		target := p.Timestamp - lookbackMS
		bestIdx := -1
		bestDiff := int64(60_000) // allow 60s tolerance
		for i, pp := range points {
			diff := abs64(pp.Timestamp - target)
			if diff < bestDiff {
				bestDiff = diff
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			continue
		}
		delta := p.Value - points[bestIdx].Value
		if delta < 0 {
			delta = 0
		}
		result = append(result, metrics.MetricPoint{
			Name:      p.Name,
			Labels:    p.Labels,
			Value:     delta,
			Timestamp: p.Timestamp,
		})
	}
	return result
}
```

- [ ] **Step 3: Add aggregation computation function**

```go
// applyAggregation merges multiple series into grouped result series.
// If groupBy is empty, all series are merged into one.
// If groupBy is a label key, series are grouped by that label's value.
func applyAggregation(series []metrics.MetricSeries, agg string, groupBy string) []metrics.MetricSeries {
	if len(series) == 0 {
		return nil
	}

	// Group series by the groupBy label value.
	groups := make(map[string][]metrics.MetricSeries) // groupKey -> list of series
	for _, s := range series {
		key := ""
		if groupBy != "" {
			key = s.Labels[groupBy]
		}
		groups[key] = append(groups[key], s)
	}

	results := make([]metrics.MetricSeries, 0, len(groups))
	for groupKey, groupSeries := range groups {
		// Collect all points from all series in the group.
		var allPoints []metrics.MetricPoint
		for _, gs := range groupSeries {
			allPoints = append(allPoints, gs.Points...)
		}

		// Sort by timestamp.
		sort.Slice(allPoints, func(i, j int) bool {
			return allPoints[i].Timestamp < allPoints[j].Timestamp
		})

		// Compute aggregated points: merge points with same timestamp.
		merged := mergePointsByTimestamp(allPoints, agg)

		// Build result series labels.
		resultLabels := make(map[string]string)
		if groupBy != "" {
			resultLabels[groupBy] = groupKey
		}

		results = append(results, metrics.MetricSeries{
			Name:   series[0].Name,
			Labels: resultLabels,
			Points: merged,
		})
	}
	return results
}

// mergePointsByTimestamp aggregates points that share the same timestamp.
func mergePointsByTimestamp(points []metrics.MetricPoint, agg string) []metrics.MetricPoint {
	if len(points) == 0 {
		return nil
	}

	// Group by timestamp.
	tsGroups := make(map[int64][]float64)
	for _, p := range points {
		tsGroups[p.Timestamp] = append(tsGroups[p.Timestamp], p.Value)
	}

	// Sort timestamps.
	timestamps := make([]int64, 0, len(tsGroups))
	for ts := range tsGroups {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	result := make([]metrics.MetricPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		vals := tsGroups[ts]
		var aggVal float64
		switch agg {
		case "sum":
			for _, v := range vals {
				aggVal += v
			}
		case "avg":
			for _, v := range vals {
				aggVal += v
			}
			aggVal /= float64(len(vals))
		case "max":
			aggVal = vals[0]
			for _, v := range vals {
				if v > aggVal {
					aggVal = v
				}
			}
		case "min":
			aggVal = vals[0]
			for _, v := range vals {
				if v < aggVal {
					aggVal = v
				}
			}
		default:
			aggVal = vals[0]
		}
		result = append(result, metrics.MetricPoint{
			Value:     aggVal,
			Timestamp: ts,
		})
	}
	return result
}
```

- [ ] **Step 4: Add ratio computation function**

```go
// applyRatio divides numerator series by denominator series point-by-point.
// Series are matched by their label values (for grouping alignment).
// Returns null (skipped point) when denominator is 0.
func applyRatio(numSeries, denSeries []metrics.MetricSeries) []metrics.MetricSeries {
	if len(numSeries) == 0 || len(denSeries) == 0 {
		return nil
	}

	// Build a map of denominator points by timestamp for fast lookup.
	// For grouped series, match by group labels.
	results := make([]metrics.MetricSeries, 0)

	for _, ns := range numSeries {
		// Find matching denominator series by labels.
		var matchedDen *metrics.MetricSeries
		for _, ds := range denSeries {
			if labelsMatchForRatio(ns.Labels, ds.Labels) {
				matchedDen = &ds
				break
			}
		}
		if matchedDen == nil {
			continue
		}

		// Build denominator timestamp -> value map.
		denByTS := make(map[int64]float64)
		for _, dp := range matchedDen.Points {
			denByTS[dp.Timestamp] = dp.Value
		}

		// Compute ratio points.
		var ratioPoints []metrics.MetricPoint
		for _, np := range ns.Points {
			dv, ok := denByTS[np.Timestamp]
			if !ok || dv == 0 {
				continue // skip: denominator missing or zero
			}
			ratioPoints = append(ratioPoints, metrics.MetricPoint{
				Name:      ns.Name,
				Labels:    ns.Labels,
				Value:     np.Value / dv,
				Timestamp: np.Timestamp,
			})
		}

		if len(ratioPoints) > 0 {
			results = append(results, metrics.MetricSeries{
				Name:   ns.Name,
				Labels: ns.Labels,
				Points: ratioPoints,
			})
		}
	}
	return results
}

// labelsMatchForRatio checks if two label sets match for ratio pairing.
// For grouped series, only the groupBy label needs to match.
// For ungrouped series, all labels must match.
func labelsMatchForRatio(a, b map[string]string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	// If both have the same keys, check all values match.
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	for k, v := range b {
		if _, ok := a[k]; !ok {
			return false
		}
	}
	return true
}
```

- [ ] **Step 5: Rewrite RangeQuery to use parsedQuery and computation pipeline**

Replace the current `RangeQuery` method (lines 185-249) with:

```go
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

	pq := parsePromQL(query)

	// Compute effective start: extend lookback for rate/increase.
	effectiveStart := start
	if pq.Func == "rate" || pq.Func == "increase" {
		effectiveStart = start - lookbackWindowMS
	}

	if pq.IsRatio {
		results := h.computeRatioRange(r, pq, effectiveStart, end, stepMS)
		writeJSON(w, http.StatusOK, prometheusResponse{
			Status: "success",
			Data:   prometheusData{ResultType: "matrix", Result: results},
		})
		return
	}

	if pq.MetricName == "" {
		writeJSON(w, http.StatusOK, prometheusResponse{
			Status: "success",
			Data:   prometheusData{ResultType: "matrix", Result: []prometheusResult{}},
		})
		return
	}

	series, err := h.store.Select(r.Context(), pq.MetricName, pq.Labels, effectiveStart, end)
	if err != nil {
		log.Printf("metrics: range query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, prometheusResponse{
			Status: "error",
			Error:  fmt.Sprintf("query failed: %v", err),
		})
		return
	}

	// Apply function to each series.
	if pq.Func == "rate" {
		for i, s := range series {
			series[i].Points = applyRate(s.Points, lookbackWindowMS)
		}
	} else if pq.Func == "increase" {
		for i, s := range series {
			series[i].Points = applyIncrease(s.Points, lookbackWindowMS)
		}
	}

	// Apply aggregation.
	if pq.Aggregation != "" {
		series = applyAggregation(series, pq.Aggregation, pq.GroupBy)
	}

	results := make([]prometheusResult, 0)
	for _, s := range series {
		values := downsamplePoints(s.Points, start, end, stepMS)
		if len(values) == 0 {
			continue
		}
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
```

- [ ] **Step 6: Add computeRatioRange helper**

```go
func (h *MetricsHandler) computeRatioRange(r *http.Request, pq parsedQuery, start, end, stepMS int64) []prometheusResult {
	// Fetch numerator series.
	var numSeries []metrics.MetricSeries
	if pq.NumMetricName != "" {
		numLabels := pq.NumLabels
		if numLabels == nil {
			numLabels = pq.Labels
		}
		ns, err := h.store.Select(r.Context(), pq.NumMetricName, numLabels, start, end)
		if err != nil {
			log.Printf("metrics: ratio numerator query error: %v", err)
			return nil
		}
		numSeries = ns
	}

	// Fetch denominator series.
	var denSeries []metrics.MetricSeries
	if pq.MetricName != "" {
		ds, err := h.store.Select(r.Context(), pq.MetricName, pq.Labels, start, end)
		if err != nil {
			log.Printf("metrics: ratio denominator query error: %v", err)
			return nil
		}
		denSeries = ds
	}

	// Apply function to both sides.
	if pq.Func == "rate" {
		for i, s := range numSeries {
			numSeries[i].Points = applyRate(s.Points, lookbackWindowMS)
		}
		for i, s := range denSeries {
			denSeries[i].Points = applyRate(s.Points, lookbackWindowMS)
		}
	} else if pq.Func == "increase" {
		for i, s := range numSeries {
			numSeries[i].Points = applyIncrease(s.Points, lookbackWindowMS)
		}
		for i, s := range denSeries {
			denSeries[i].Points = applyIncrease(s.Points, lookbackWindowMS)
		}
	}

	// Apply aggregation to both sides.
	if pq.Aggregation != "" {
		numSeries = applyAggregation(numSeries, pq.Aggregation, pq.GroupBy)
		denSeries = applyAggregation(denSeries, pq.Aggregation, pq.GroupBy)
	}

	// Compute ratio.
	ratioSeries := applyRatio(numSeries, denSeries)

	results := make([]prometheusResult, 0)
	for _, s := range ratioSeries {
		values := downsamplePoints(s.Points, start, end, stepMS)
		if len(values) == 0 {
			continue
		}
		metricLabel := make(map[string]string, len(s.Labels)+1)
		metricLabel["__name__"] = pq.NumMetricName + "_per_" + pq.MetricName
		for k, v := range s.Labels {
			metricLabel[k] = v
		}
		results = append(results, prometheusResult{
			Metric: metricLabel,
			Values: values,
		})
	}
	return results
}
```

- [ ] **Step 7: Rewrite InstantQuery to use parsedQuery**

Replace the current `InstantQuery` method (lines 97-158) with:

```go
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

	pq := parsePromQL(query)

	// Look back up to 1 hour to find the most recent data point.
	start := ts - 3600_000
	end := ts + 1000

	// Extend lookback for rate/increase.
	if pq.Func == "rate" || pq.Func == "increase" {
		start = ts - 3600_000 - lookbackWindowMS
	}

	if pq.IsRatio {
		results := h.computeRatioInstant(r, pq, ts, start, end)
		writeJSON(w, http.StatusOK, prometheusResponse{
			Status: "success",
			Data:   prometheusData{ResultType: "vector", Result: results},
		})
		return
	}

	if pq.MetricName == "" {
		writeJSON(w, http.StatusOK, prometheusResponse{
			Status: "success",
			Data:   prometheusData{ResultType: "vector", Result: []prometheusResult{}},
		})
		return
	}

	series, err := h.store.Select(r.Context(), pq.MetricName, pq.Labels, start, end)
	if err != nil {
		log.Printf("metrics: instant query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, prometheusResponse{
			Status: "error",
			Error:  fmt.Sprintf("query failed: %v", err),
		})
		return
	}

	// Apply function.
	if pq.Func == "rate" {
		for i, s := range series {
			series[i].Points = applyRate(s.Points, lookbackWindowMS)
		}
	} else if pq.Func == "increase" {
		for i, s := range series {
			series[i].Points = applyIncrease(s.Points, lookbackWindowMS)
		}
	}

	// Apply aggregation.
	if pq.Aggregation != "" {
		series = applyAggregation(series, pq.Aggregation, pq.GroupBy)
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
```

- [ ] **Step 8: Add computeRatioInstant helper**

```go
func (h *MetricsHandler) computeRatioInstant(r *http.Request, pq parsedQuery, ts, start, end int64) []prometheusResult {
	var numSeries []metrics.MetricSeries
	if pq.NumMetricName != "" {
		numLabels := pq.NumLabels
		if numLabels == nil {
			numLabels = pq.Labels
		}
		ns, err := h.store.Select(r.Context(), pq.NumMetricName, numLabels, start, end)
		if err != nil {
			return nil
		}
		numSeries = ns
	}

	var denSeries []metrics.MetricSeries
	if pq.MetricName != "" {
		ds, err := h.store.Select(r.Context(), pq.MetricName, pq.Labels, start, end)
		if err != nil {
			return nil
		}
		denSeries = ds
	}

	if pq.Func == "rate" {
		for i, s := range numSeries {
			numSeries[i].Points = applyRate(s.Points, lookbackWindowMS)
		}
		for i, s := range denSeries {
			denSeries[i].Points = applyRate(s.Points, lookbackWindowMS)
		}
	} else if pq.Func == "increase" {
		for i, s := range numSeries {
			numSeries[i].Points = applyIncrease(s.Points, lookbackWindowMS)
		}
		for i, s := range denSeries {
			denSeries[i].Points = applyIncrease(s.Points, lookbackWindowMS)
		}
	}

	if pq.Aggregation != "" {
		numSeries = applyAggregation(numSeries, pq.Aggregation, pq.GroupBy)
		denSeries = applyAggregation(denSeries, pq.Aggregation, pq.GroupBy)
	}

	ratioSeries := applyRatio(numSeries, denSeries)

	results := make([]prometheusResult, 0)
	for _, s := range ratioSeries {
		best := pickClosestPoint(s.Points, ts)
		if best == nil {
			continue
		}
		metricLabel := make(map[string]string, len(s.Labels)+1)
		metricLabel["__name__"] = pq.NumMetricName + "_per_" + pq.MetricName
		for k, v := range s.Labels {
			metricLabel[k] = v
		}
		results = append(results, prometheusResult{
			Metric: metricLabel,
			Value:  []interface{}{float64(best.Timestamp) / 1000.0, strconv.FormatFloat(best.Value, 'f', -1, 64)},
		})
	}
	return results
}
```

- [ ] **Step 9: Update the existing TestParsePromQL to use parsePromQLCompat**

The existing `TestParsePromQL` test (lines 228-252) calls `parsePromQL(query)` returning `(string, map[string]string)`. Since we renamed `parsePromQL` to return `parsedQuery`, update the test:

```go
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
```

- [ ] **Step 10: Add computation tests**

Add to `metrics_handler_test.go`:

```go
func TestApplyRate(t *testing.T) {
	points := []metrics.MetricPoint{
		{Name: "requests", Labels: nil, Value: 100, Timestamp: 600_000},   // t=10m
		{Name: "requests", Labels: nil, Value: 110, Timestamp: 900_000},   // t=15m
		{Name: "requests", Labels: nil, Value: 120, Timestamp: 1_200_000}, // t=20m
		{Name: "requests", Labels: nil, Value: 130, Timestamp: 1_500_000}, // t=25m
	}
	result := applyRate(points, 300_000) // 5m lookback

	// First point (t=10m) has no 5m-before point → should be skipped.
	// Second point (t=15m): lookback target t=10m → point at t=10m (value=100).
	//   rate = (110 - 100) / 300 = 0.0333...
	if len(result) < 1 {
		t.Fatal("expected at least 1 rate point")
	}
	// Check rate for t=15m (900_000).
	approxEqual(t, result[0].Value, 10.0/300.0, 0.001)
}

func TestApplyIncrease(t *testing.T) {
	points := []metrics.MetricPoint{
		{Name: "requests", Labels: nil, Value: 100, Timestamp: 600_000},
		{Name: "requests", Labels: nil, Value: 110, Timestamp: 900_000},
		{Name: "requests", Labels: nil, Value: 120, Timestamp: 1_200_000},
	}
	result := applyIncrease(points, 300_000)

	if len(result) < 1 {
		t.Fatal("expected at least 1 increase point")
	}
	approxEqual(t, result[0].Value, 10.0, 0.001) // 110 - 100 = 10
}

func TestApplyAggregation(t *testing.T) {
	series := []metrics.MetricSeries{
		{Name: "requests", Labels: map[string]string{"service": "api"}, Points: []metrics.MetricPoint{
			{Value: 10, Timestamp: 1000},
			{Value: 20, Timestamp: 2000},
		}},
		{Name: "requests", Labels: map[string]string{"service": "web"}, Points: []metrics.MetricPoint{
			{Value: 30, Timestamp: 1000},
			{Value: 40, Timestamp: 2000},
		}},
	}

	// Sum without groupBy: all merged into one series.
	sumResult := applyAggregation(series, "sum", "")
	if len(sumResult) != 1 {
		t.Fatalf("expected 1 series for sum without groupBy, got %d", len(sumResult))
	}
	// At t=1000: 10 + 30 = 40; at t=2000: 20 + 40 = 60
	ts1000 := findPointAt(sumResult[0].Points, 1000)
	ts2000 := findPointAt(sumResult[0].Points, 2000)
	approxEqual(t, ts1000, 40.0, 0.001)
	approxEqual(t, ts2000, 60.0, 0.001)

	// Sum with groupBy(service): two series, one per service.
	sumGrouped := applyAggregation(series, "sum", "service")
	if len(sumGrouped) != 2 {
		t.Fatalf("expected 2 series for sum by service, got %d", len(sumGrouped))
	}
}

func TestApplyRatio(t *testing.T) {
	numSeries := []metrics.MetricSeries{
		{Name: "success", Labels: nil, Points: []metrics.MetricPoint{
			{Value: 80, Timestamp: 1000},
			{Value: 90, Timestamp: 2000},
		}},
	}
	denSeries := []metrics.MetricSeries{
		{Name: "total", Labels: nil, Points: []metrics.MetricPoint{
			{Value: 100, Timestamp: 1000},
			{Value: 100, Timestamp: 2000},
		}},
	}

	result := applyRatio(numSeries, denSeries)
	if len(result) != 1 {
		t.Fatalf("expected 1 ratio series, got %d", len(result))
	}
	if len(result[0].Points) != 2 {
		t.Fatalf("expected 2 ratio points, got %d", len(result[0].Points))
	}
	approxEqual(t, result[0].Points[0].Value, 0.8, 0.001)  // 80/100
	approxEqual(t, result[0].Points[1].Value, 0.9, 0.001)  // 90/100
}

func TestApplyRatio_DivisionByZero(t *testing.T) {
	numSeries := []metrics.MetricSeries{
		{Name: "success", Labels: nil, Points: []metrics.MetricPoint{
			{Value: 80, Timestamp: 1000},
			{Value: 90, Timestamp: 2000},
		}},
	}
	denSeries := []metrics.MetricSeries{
		{Name: "total", Labels: nil, Points: []metrics.MetricPoint{
			{Value: 100, Timestamp: 1000},
			{Value: 0, Timestamp: 2000}, // zero denominator
		}},
	}

	result := applyRatio(numSeries, denSeries)
	if len(result) != 1 {
		t.Fatalf("expected 1 ratio series, got %d", len(result))
	}
	// Only 1 point: t=1000 with valid denominator; t=2000 skipped.
	if len(result[0].Points) != 1 {
		t.Fatalf("expected 1 ratio point (division by zero skips), got %d", len(result[0].Points))
	}
	approxEqual(t, result[0].Points[0].Value, 0.8, 0.001)
}

func TestImprovedDownsampling(t *testing.T) {
	points := []metrics.MetricPoint{
		{Value: 10, Timestamp: 0},
		{Value: 20, Timestamp: 1000},
		{Value: 30, Timestamp: 2000},
	}
	// step=2000ms, start=0, end=2000
	result := downsamplePoints(points, 0, 2000, 2000)
	// t=0: window [-1000, 1000] → points at 0 (10) and 1000 (20) → avg=15
	// t=2000: window [1000, 3000] → points at 1000 (20) and 2000 (30) → avg=25
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
	if abs := got - want; abs < -tolerance || abs > tolerance {
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
```

- [ ] **Step 11: Run tests**

```bash
cd D:/code/opensource/github/labubu && go test -v ./internal/api/
```

Expected: All tests pass — existing handler tests, parser tests, and new computation tests

- [ ] **Step 12: Commit**

```bash
git add internal/api/metrics_handler.go internal/api/metrics_handler_test.go
git commit -m "feat: implement rate/increase/aggregation/ratio computation with improved downsampling"
```

---

## Task 4: Extended PanelConfig TypeScript Type (Frontend)

**Files:**
- Modify: `web/src/api/client.ts:139-146`

- [ ] **Step 1: Extend PanelConfig interface**

Replace the current interface (lines 139-146) with:

```typescript
export interface PanelConfig {
  id: string
  title: string
  expressionType: 'single' | 'ratio'
  metric: string                       // Single: main metric; Ratio: denominator
  numeratorMetric?: string             // Ratio: numerator (only when expressionType='ratio')
  labels: Record<string, string>
  func: 'none' | 'rate' | 'increase'
  aggregation: 'none' | 'sum' | 'avg' | 'max' | 'min'
  groupBy?: string                     // label key for grouping
  chartType: 'line' | 'bar' | 'stat'
  step?: number
}
```

- [ ] **Step 2: TypeScript type check**

```bash
cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit
```

Expected: No type errors (PanelConfig used in PanelForm and PanelChart will need updates in Tasks 5-6)

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat: extend PanelConfig TypeScript type with expressionType, func, aggregation, groupBy"
```

---

## Task 5: i18n Keys (Frontend)

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

- [ ] **Step 1: Add panelForm keys to en.ts**

Add a new `panelForm` section after the `dashboard` section (after line 21):

```typescript
  panelForm: {
    expressionType: 'Expression Type',
    single: 'Single Metric',
    ratio: 'Ratio (A / B)',
    numerator: 'Numerator (A)',
    denominator: 'Denominator (B)',
    func: 'Function',
    funcNone: 'None',
    funcRate: 'Rate (per second)',
    funcIncrease: 'Increase (5m delta)',
    aggregation: 'Aggregation',
    aggNone: 'None',
    aggSum: 'Sum',
    aggAvg: 'Average',
    aggMax: 'Maximum',
    aggMin: 'Minimum',
    groupBy: 'Group By',
    groupByNone: '-- none --',
  },
```

- [ ] **Step 2: Add panelForm keys to zh.ts**

Add the same section in the same position:

```typescript
  panelForm: {
    expressionType: '表达式类型',
    single: '单指标',
    ratio: '比率 (A / B)',
    numerator: '分子 (A)',
    denominator: '分母 (B)',
    func: '函数',
    funcNone: '无',
    funcRate: '速率（每秒）',
    funcIncrease: '增量（5分钟差值）',
    aggregation: '聚合',
    aggNone: '无',
    aggSum: '求和',
    aggAvg: '均值',
    aggMax: '最大值',
    aggMin: '最小值',
    groupBy: '分组维度',
    groupByNone: '-- 无 --',
  },
```

- [ ] **Step 3: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat: add panelForm i18n keys for expression type, function, aggregation"
```

---

## Task 6: PanelForm UI (Frontend)

**Files:**
- Modify: `web/src/components/PanelForm.vue`

- [ ] **Step 1: Update template — add expression type, numerator/denominator, function, aggregation, groupBy**

Replace the entire `<template>` section with:

```html
<template>
  <div class="modal-overlay" @click.self="$emit('cancel')">
    <div class="modal-content">
      <h3 class="modal-title">{{ isEdit ? t('dashboard.renameDashboard') : t('dashboard.newDashboard') }} — {{ t('panelForm.expressionType') }}</h3>

      <form @submit.prevent="handleSubmit">
        <div class="form-group">
          <label for="pf-title">Title</label>
          <input id="pf-title" v-model="form.title" type="text" required placeholder="e.g. Request Rate" />
        </div>

        <div class="form-group">
          <label for="pf-exprtype">{{ t('panelForm.expressionType') }}</label>
          <select id="pf-exprtype" v-model="form.expressionType">
            <option value="single">{{ t('panelForm.single') }}</option>
            <option value="ratio">{{ t('panelForm.ratio') }}</option>
          </select>
        </div>

        <!-- Single mode -->
        <div v-if="form.expressionType === 'single'" class="form-group">
          <label for="pf-metric">Metric</label>
          <select id="pf-metric" v-model="form.metric" required>
            <option value="" disabled>Select a metric...</option>
            <option v-for="m in metricNames" :key="m" :value="m">{{ m }}</option>
          </select>
        </div>

        <!-- Ratio mode -->
        <template v-if="form.expressionType === 'ratio'">
          <div class="form-group">
            <label for="pf-numerator">{{ t('panelForm.numerator') }}</label>
            <select id="pf-numerator" v-model="form.numeratorMetric" required>
              <option value="" disabled>Select numerator metric...</option>
              <option v-for="m in metricNames" :key="m" :value="m">{{ m }}</option>
            </select>
          </div>
          <div class="form-group">
            <label for="pf-denominator">{{ t('panelForm.denominator') }}</label>
            <select id="pf-denominator" v-model="form.metric" required>
              <option value="" disabled>Select denominator metric...</option>
              <option v-for="m in metricNames" :key="m" :value="m">{{ m }}</option>
            </select>
          </div>
        </template>

        <div class="form-group">
          <label>Labels (optional)</label>
          <div v-for="(item, idx) in form.labelEntries" :key="idx" class="label-row">
            <select v-model="item.key" @change="onLabelKeyChange(idx)">
              <option value="">-- key --</option>
              <option v-for="ln in sortedLabelNames" :key="ln" :value="ln">{{ ln }}</option>
            </select>
            <select v-model="item.value">
              <option value="">-- value --</option>
              <option v-for="lv in labelValuesCache[item.key] || []" :key="lv" :value="lv">{{ lv }}</option>
            </select>
            <button type="button" class="btn-remove" @click="removeLabel(idx)">x</button>
          </div>
          <button type="button" class="btn-add-label" @click="addLabel">+ Add label</button>
        </div>

        <div class="form-group">
          <label for="pf-func">{{ t('panelForm.func') }}</label>
          <select id="pf-func" v-model="form.func">
            <option value="none">{{ t('panelForm.funcNone') }}</option>
            <option value="rate">{{ t('panelForm.funcRate') }}</option>
            <option value="increase">{{ t('panelForm.funcIncrease') }}</option>
          </select>
        </div>

        <div class="form-group">
          <label for="pf-aggregation">{{ t('panelForm.aggregation') }}</label>
          <select id="pf-aggregation" v-model="form.aggregation">
            <option value="none">{{ t('panelForm.aggNone') }}</option>
            <option value="sum">{{ t('panelForm.aggSum') }}</option>
            <option value="avg">{{ t('panelForm.aggAvg') }}</option>
            <option value="max">{{ t('panelForm.aggMax') }}</option>
            <option value="min">{{ t('panelForm.aggMin') }}</option>
          </select>
        </div>

        <div v-if="form.aggregation !== 'none'" class="form-group">
          <label for="pf-groupby">{{ t('panelForm.groupBy') }}</label>
          <select id="pf-groupby" v-model="form.groupBy">
            <option value="">{{ t('panelForm.groupByNone') }}</option>
            <option v-for="ln in sortedLabelNames" :key="ln" :value="ln">{{ ln }}</option>
          </select>
        </div>

        <div class="form-group">
          <label for="pf-charttype">Chart Type</label>
          <select id="pf-charttype" v-model="form.chartType" required>
            <option value="line">Line</option>
            <option value="bar">Bar</option>
            <option value="stat">Stat Card</option>
          </select>
        </div>

        <div class="form-group" v-if="form.chartType !== 'stat'">
          <label for="pf-step">Step (seconds)</label>
          <input id="pf-step" v-model.number="form.step" type="number" min="1" />
        </div>

        <div class="modal-actions">
          <button type="button" class="btn btn-secondary" @click="$emit('cancel')">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? 'Saving...' : 'Save' }}
          </button>
        </div>
        <p v-if="saveError" class="form-error">{{ saveError }}</p>
      </form>
    </div>
  </div>
</template>
```

- [ ] **Step 2: Update script — add new form fields and use i18n**

Replace the entire `<script setup lang="ts">` section with:

```typescript
<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { getMetricNames, getLabels, getLabelValues, createPanel, updatePanel } from '../api/client'
import type { PanelConfig } from '../api/client'

const { t } = useI18n()

const props = defineProps<{
  panel?: PanelConfig | null
  dashboardId?: string
}>()

const emit = defineEmits<{
  saved: [panel: PanelConfig]
  cancel: []
}>()

const isEdit = computed(() => !!props.panel)

const form = reactive({
  title: props.panel?.title || '',
  expressionType: (props.panel?.expressionType || 'single') as 'single' | 'ratio',
  metric: props.panel?.metric || '',
  numeratorMetric: props.panel?.numeratorMetric || '',
  labelEntries: [] as Array<{ key: string; value: string }>,
  func: (props.panel?.func || 'none') as 'none' | 'rate' | 'increase',
  aggregation: (props.panel?.aggregation || 'none') as 'none' | 'sum' | 'avg' | 'max' | 'min',
  groupBy: props.panel?.groupBy || '',
  chartType: (props.panel?.chartType || 'line') as 'line' | 'bar' | 'stat',
  step: props.panel?.step || 60,
})

// Populate initial label entries from panel.
if (props.panel?.labels) {
  form.labelEntries = Object.entries(props.panel.labels).map(([k, v]) => ({ key: k, value: v }))
}

const metricNames = ref<string[]>([])
const allLabelNames = ref<string[]>([])
const labelValuesCache = reactive<Record<string, string[]>>({})
const saving = ref(false)
const saveError = ref('')

const sortedLabelNames = computed(() => {
  return allLabelNames.value.filter(n => n !== '__name__').sort()
})

function addLabel() {
  form.labelEntries.push({ key: '', value: '' })
}

function removeLabel(idx: number) {
  form.labelEntries.splice(idx, 1)
}

async function onLabelKeyChange(idx: number) {
  const key = form.labelEntries[idx].key
  if (key && !labelValuesCache[key]) {
    try {
      const values = await getLabelValues(key)
      labelValuesCache[key] = values
    } catch {
      labelValuesCache[key] = []
    }
  }
}

async function handleSubmit() {
  saveError.value = ''

  // Validate required fields.
  if (!form.title) { saveError.value = 'Title is required'; return }
  if (!form.metric) { saveError.value = 'Metric is required'; return }
  if (form.expressionType === 'ratio' && !form.numeratorMetric) {
    saveError.value = 'Numerator metric is required for ratio'
    return
  }

  const labels: Record<string, string> = {}
  for (const entry of form.labelEntries) {
    if (entry.key && entry.value) {
      labels[entry.key] = entry.value
    }
  }

  const panel: Omit<PanelConfig, 'id'> = {
    title: form.title,
    expressionType: form.expressionType,
    metric: form.metric,
    numeratorMetric: form.expressionType === 'ratio' ? form.numeratorMetric : undefined,
    labels,
    func: form.func,
    aggregation: form.aggregation,
    groupBy: form.aggregation !== 'none' ? form.groupBy : undefined,
    chartType: form.chartType,
  }
  if (form.chartType !== 'stat') {
    panel.step = form.step
  }

  if (!props.dashboardId) {
    saveError.value = 'No dashboard selected'
    return
  }

  saving.value = true
  try {
    let result: PanelConfig
    if (isEdit.value && props.panel) {
      result = await updatePanel(props.dashboardId, {
        ...panel,
        id: props.panel.id,
      })
    } else {
      result = await createPanel(props.dashboardId, panel)
    }
    emit('saved', result)
  } catch (e: any) {
    saveError.value = e.message || 'Save failed'
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  try {
    const [names, labels] = await Promise.all([getMetricNames(), getLabels()])
    metricNames.value = names.sort()
    allLabelNames.value = labels
  } catch {
    // populating dropdowns is best-effort
  }
})
</script>
```

- [ ] **Step 3: Run type check**

```bash
cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit
```

Expected: No type errors

- [ ] **Step 4: Commit**

```bash
git add web/src/components/PanelForm.vue
git commit -m "feat: add expression type, function, aggregation, groupBy selectors to PanelForm"
```

---

## Task 7: PanelChart PromQL Builder (Frontend)

**Files:**
- Modify: `web/src/components/PanelChart.vue`

- [ ] **Step 1: Replace buildPromQL function**

Replace the existing `buildPromQL` function (lines 69-78) with:

```typescript
function buildPromQL(): string {
  const labelsStr = formatLabels(props.panel.labels || {})

  if (props.panel.expressionType === 'ratio') {
    const numExpr = wrapFunc(props.panel.numeratorMetric!, labelsStr, props.panel.func)
    const denExpr = wrapFunc(props.panel.metric, labelsStr, props.panel.func)
    const ratioExpr = `${numExpr} / ${denExpr}`
    return wrapAggregation(ratioExpr, props.panel.aggregation, props.panel.groupBy)
  }

  const expr = wrapFunc(props.panel.metric, labelsStr, props.panel.func)
  return wrapAggregation(expr, props.panel.aggregation, props.panel.groupBy)
}

function formatLabels(labels: Record<string, string>): string {
  const pairs = Object.entries(labels)
    .map(([k, v]) => `${k}="${v}"`)
    .join(',')
  return pairs
}

function wrapFunc(metric: string, labels: string, func: string): string {
  const base = labels ? `${metric}{${labels}}` : metric
  if (func === 'rate') return `rate(${base}[5m])`
  if (func === 'increase') return `increase(${base}[5m])`
  return base
}

function wrapAggregation(expr: string, agg: string, groupBy?: string): string {
  if (agg === 'none' || !agg) return expr
  const byClause = groupBy ? ` by (${groupBy})` : ''
  return `${agg}(${expr})${byClause}`
}
```

- [ ] **Step 2: Update stat panel display for ratio**

In the template, update the stat display section (lines 14-17) to show the metric label more clearly for ratio panels:

```html
<div v-else-if="panel.chartType === 'stat'" class="stat-value">
  <span class="stat-number">{{ formatValue(statValue) }}</span>
  <span class="stat-metric">{{ statLabel }}</span>
</div>
```

Add `statLabel` computed in the script:

```typescript
const statLabel = computed(() => {
  if (props.panel.expressionType === 'ratio') {
    return `${props.panel.numeratorMetric} / ${props.panel.metric}`
  }
  return props.panel.metric
})
```

- [ ] **Step 3: Run type check + build**

```bash
cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit
```

Expected: No type errors

- [ ] **Step 4: Commit**

```bash
git add web/src/components/PanelChart.vue
git commit -m "feat: replace PanelChart buildPromQL with structured builder supporting func/aggregation/ratio"
```

---

## Task 8: Integration Verification

**Files:** None (verification only)

- [ ] **Step 1: Run all backend tests**

```bash
cd D:/code/opensource/github/labubu && go test -v ./internal/api/ ./internal/...
```

Expected: All tests pass

- [ ] **Step 2: Run frontend type check**

```bash
cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit
```

Expected: No type errors

- [ ] **Step 3: Build frontend**

```bash
cd D:/code/opensource/github/labubu/web && npm run build
```

Expected: Build succeeds

- [ ] **Step 4: Build Go binary**

```bash
cd D:/code/opensource/github/labubu && CGO_ENABLED=0 go build -tags "dev" -o /dev/null ./cmd/labubu
```

Expected: Build succeeds

- [ ] **Step 5: Manual smoke test**

```bash
cd D:/code/opensource/github/labubu && go run -tags dev ./cmd/labubu serve --port 8082 --data-dir data-test
```

In browser:
1. Navigate to Dashboard page
2. Create a new panel
3. Verify PanelForm shows Expression Type, Function, Aggregation, Group By selectors
4. Switch expression type to "Ratio" — verify numerator/denominator fields appear
5. Select a function (rate/increase) — verify the chart renders with computed data
6. Select aggregation (sum) + groupBy — verify grouped series display

- [ ] **Step 6: Final commit (if any adjustments needed)**

```bash
git add -A
git commit -m "feat: complete dashboard PromQL functions, aggregation, and ratio expression support"
```

---

## Self-Review

### Spec Coverage

| Spec Section | Task |
|-------------|------|
| Data Model: PanelConfig (Go + TS) | Task 1 + Task 4 |
| PromQL expression construction | Task 2 (parser) + Task 7 (frontend builder) |
| parsePromQL extension | Task 2 |
| Computation pipeline: rate/increase | Task 3 (Step 2) |
| Computation pipeline: aggregation | Task 3 (Step 3) |
| Computation pipeline: ratio | Task 3 (Step 4) |
| InstantQuery extension | Task 3 (Steps 7-8) |
| RangeQuery rewrite | Task 3 (Steps 5-6) |
| Improved downsampling | Task 3 (Step 1) |
| NaN/null handling | Task 3 (Step 2 — skip when no lookback, Step 4 — skip when denom=0) |
| PanelForm UI | Task 6 |
| PanelChart builder | Task 7 |
| Dashboard handler validation | Task 1 (Step 2) |
| i18n keys | Task 5 |
| Testing: parsePromQL | Task 2 (Step 4) |
| Testing: rate/increase computation | Task 3 (Step 10) |
| Testing: aggregation computation | Task 3 (Step 10) |
| Testing: ratio computation | Task 3 (Step 10) |
| Testing: ratio division by zero | Task 3 (Step 10) |
| Testing: improved downsampling | Task 3 (Step 10) |
| Testing: dashboard panel validation | Task 1 (Step 3) |
| Error handling table | Task 3 (empty result for missing metric, null for division by zero) |

No gaps found.

### Placeholder Scan

No TBD, TODO, "implement later", or vague requirements found. All steps contain actual code.

### Type Consistency

- Go `PanelConfig.ExpressionType` → TS `expressionType: 'single' | 'ratio'` ✅
- Go `PanelConfig.Func` → TS `func: 'none' | 'rate' | 'increase'` ✅
- Go `PanelConfig.Aggregation` → TS `aggregation: 'none' | 'sum' | 'avg' | 'max' | 'min'` ✅
- Go `PanelConfig.GroupBy` → TS `groupBy?: string` ✅
- Go `PanelConfig.NumeratorMetric` → TS `numeratorMetric?: string` ✅
- `parsedQuery.Func` matches `"rate"|"increase"|"none"|""` ✅
- `parsedQuery.Aggregation` matches `"sum"|"avg"|"max"|"min"|""` ✅
- Frontend `wrapFunc` uses `panel.func` matching `"rate"|"increase"|other → no wrapper` ✅
- Frontend `wrapAggregation` uses `panel.aggregation` matching `"none"|other → wrapper` ✅
- `lookbackWindowMS` constant = 300_000 matches spec's "5 minutes" ✅
