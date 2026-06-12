# Dashboard PromQL Functions & Ratio Design

## Goal

Extend the Dashboard panel system to support PromQL functions (rate/increase), aggregations (sum/avg/max/min), and ratio expressions (metric_a / metric_b), enabling meaningful metric visualizations like request rates, success rates, and grouped aggregations.

## Motivation

Current Dashboard panels only support `metric_name{labels}` — raw cumulative values. Counter metrics (like `gen_ai_client_requests_total`) produce monotonically increasing lines that are useless without `rate()`. Ratio metrics (like success rate = successes / total) require two metrics divided point-by-point, which can't be expressed at all. These are fundamental gaps for LLM observability.

## Architecture

Backend extends the existing `parsePromQL` + `query_range` pipeline to support function, aggregation, and ratio computation. Frontend PanelForm adds expression type, function, aggregation, and group-by selectors. PanelChart constructs richer PromQL strings and passes them to the same `/api/v1/query_range` endpoint.

**Tech Stack:** Go backend (metrics_handler.go extended PromQL parser + computation) + Vue 3 frontend (PanelForm + PanelChart modifications) + Chart.js (unchanged)

---

## Data Model

### PanelConfig (extended)

Go struct:
```go
type PanelConfig struct {
    ID              string            `json:"id"`
    Title           string            `json:"title"`
    ExpressionType  string            `json:"expressionType"`  // "single" | "ratio"
    Metric          string            `json:"metric"`          // Single: main metric; Ratio: denominator
    NumeratorMetric string            `json:"numeratorMetric,omitempty"` // Ratio: numerator
    Labels          map[string]string `json:"labels"`
    Func            string            `json:"func"`            // "none" | "rate" | "increase"
    Aggregation     string            `json:"aggregation"`     // "none" | "sum" | "avg" | "max" | "min"
    GroupBy         string            `json:"groupBy,omitempty"` // label key for aggregation grouping
    ChartType       string            `json:"chartType"`
    Step            int               `json:"step,omitempty"`
}
```

TypeScript type (in `web/src/api/client.ts`):
```typescript
interface PanelConfig {
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

### Design decisions

- `expressionType` determines single vs ratio mode
- Ratio: `metric` is denominator, `numeratorMetric` is numerator — denominator is the "base" metric (total_requests), numerator is the subset (success_requests)
- `func` and `aggregation` are independent selectors, not coupled
- `groupBy` is optional label key — when aggregation is set, specifies which label dimension to group by; empty means no grouping (aggregate everything into one value)
- New fields default to empty/none — existing panels without these fields continue to work unchanged

### PromQL expression construction

| Config | Generated PromQL |
|--------|-----------------|
| Single + none + none | `metric{labels}` |
| Single + rate + none | `rate(metric{labels}[5m])` |
| Single + rate + sum | `sum(rate(metric{labels}[5m]))` |
| Single + rate + sum + groupBy(service) | `sum(rate(metric{labels}[5m])) by (service)` |
| Ratio(numerator/metric) + none | `numeratorMetric{labels} / metric{labels}` |
| Ratio + rate | `rate(numeratorMetric{labels}[5m]) / rate(metric{labels}[5m])` |
| Ratio + rate + sum by (service) | `sum(rate(numeratorMetric{labels}[5m])) by (service) / sum(rate(metric{labels}[5m])) by (service)` |

Lookback window for rate/increase is fixed at **5 minutes** — the most common industry standard, no custom window selector.

---

## Backend: Extended PromQL Parser & Computation

### parsePromQL extension

Current parser only handles `metric{labels}`. Extend to support a structured query model:

```go
type parsedQuery struct {
    MetricName      string
    Labels          map[string]string
    Func            string   // "" | "rate" | "increase"
    Window          int64    // lookback in ms (300000 for 5m)
    Aggregation     string   // "" | "sum" | "avg" | "max" | "min"
    GroupBy         string   // label key for aggregation grouping
    IsRatio         bool
    NumMetricName   string   // numerator metric (ratio only)
    NumLabels       map[string]string
}
```

The parser still accepts raw PromQL strings for backward compatibility, but the Dashboard frontend will construct structured expressions. The parser recognizes these patterns in order:

1. `agg(func(metric{labels}[5m])) by (dimension)` — full expression with aggregation, function, and grouping
2. `agg(func(metric{labels}[5m]))` — aggregation + function, no grouping
3. `func(metric{labels}[5m])` — function only
4. `func(metric_a{labels}[5m]) / func(metric_b{labels}[5m])` — ratio expression (binary division)
5. `agg(func(metric_a{labels}[5m])) by (dim) / agg(func(metric_b{labels}[5m])) by (dim)` — ratio with aggregation
6. `metric{labels}` — existing simple pattern (unchanged)

Parsing strategy: regex-based recognition of wrapping patterns (function names, aggregation keywords, `[5m]` window, `by (...)` grouping, `/` division), falling back to existing `{labels}` parser for simple expressions.

### Computation pipeline

```
RangeQuery(parsedQuery, start, end, step)
  → 1. Fetch raw data from tstorage (with lookback if func=rate/increase)
  → 2. Apply function:
       rate:   (point_t - point_t-5m) / 300_seconds
       increase: point_t - point_t-5m
  → 3. Apply aggregation (if set):
       group matching series by groupBy label value
       for each group and each step: compute sum/avg/max/min across series
  → 4. For ratio: apply steps 1-3 to numerator and denominator separately
       then divide point-by-point (NaN → null, frontend shows gap)
  → 5. Improved downsampling: window avg instead of nearest-point, skip gaps instead of fill 0
  → 6. Return Prometheus-format JSON response
```

### InstantQuery extension

For `stat` panels, InstantQuery also needs to support rate/increase:
- Fetch raw data with 5m lookback
- Apply function and aggregation
- Return single latest computed value

### Improved downsampling

Current `downsamplePoints` picks the closest single point per step interval and fills 0 when no data exists. Change to:

```go
func downsamplePoints(points []metrics.MetricPoint, start, end, step int64) [][]interface{} {
    // For each step window [t-step/2, t+step/2]:
    //   - Collect all points within the window
    //   - Return their average value
    //   - If no points in window, skip (emit nothing) instead of 0
}
```

This eliminates misleading zero spikes and produces smoother, more accurate charts.

### NaN / null handling

- Rate/increase at the very start of data: if no point exists 5m before, emit null (skip the point)
- Ratio division by zero: emit null, Chart.js will show a gap in the line
- This matches Prometheus behavior where `rate()` returns no data for the first window

---

## Frontend: PanelForm

### New form layout

```
┌─────────────────────────────────────┐
│ Title          [________________]    │
│                                     │
│ Expression Type [Single ▼ / Ratio ▼]│
│                                     │
│ ── Single mode ──                   │
│ Metric         [________________ ▼] │
│                                     │
│ ── Ratio mode ──                    │
│ Numerator      [________________ ▼] │ ← only when expressionType='ratio'
│ Denominator    [________________ ▼] │ ← only when expressionType='ratio'
│                                     │
│ Labels (shared)                     │
│   [key ▼] = [value ▼]   [×]        │
│   [+ Add label]                     │
│                                     │
│ Function       [None ▼ / rate / inc]│
│ Aggregation    [None ▼ / sum / avg…]│
│ Group By       [label key ▼]       │ ← only when aggregation != 'none'
│                                     │
│ Chart Type     [line ▼ / bar / stat]│
│ Step (sec)     [60]                 │ ← only when chartType != 'stat'
│                                     │
│         [Cancel]  [Save]            │
└─────────────────────────────────────┘
```

### Interaction logic

- `ExpressionType` toggle: switches between single metric field vs numerator/denominator pair
- `Aggregation` set to 'none': `GroupBy` field hidden
- `GroupBy` dropdown: populated from `GET /api/v1/labels` (same source as label key dropdown)
- Label selector: existing behavior unchanged — key dropdown + value dropdown per row
- All new fields default to empty/none on panel creation

### Label selector note

No changes needed for the "group by = selected label" concept. The current label selector already supports filtering by key=value. The `groupBy` field is a separate, dedicated dropdown for aggregation grouping — it's a distinct concept from label filtering. Users can:
- Filter by `service=api` AND group by `model` — see each model's rate within the api service
- Filter by nothing AND group by `service` — see each service's rate separately
- Filter by `service=api` AND no group by — see the single rate for api service

---

## Frontend: PanelChart

### PromQL construction

Replace the existing `buildPromQL` function with a structured builder:

```typescript
function buildPromQL(panel: PanelConfig): string {
  const labelsStr = formatLabels(panel.labels)

  if (panel.expressionType === 'ratio') {
    const numExpr = wrapFunc(panel.numeratorMetric!, labelsStr, panel.func)
    const denExpr = wrapFunc(panel.metric, labelsStr, panel.func)
    const ratioExpr = `${numExpr} / ${denExpr}`
    return wrapAggregation(ratioExpr, panel.aggregation, panel.groupBy)
  }

  const expr = wrapFunc(panel.metric, labelsStr, panel.func)
  return wrapAggregation(expr, panel.aggregation, panel.groupBy)
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

### Stat panel handling

For stat panels with rate/increase, use `/api/v1/query` (instant query) instead of `/api/v1/query_range`. The backend InstantQuery must be extended to apply the same function/aggregation computation.

### Ratio stat display

When expressionType is 'ratio' and chartType is 'stat', display the raw ratio number. For example, success rate of 87% shows as `0.87`. Users understand the meaning from their metric naming conventions — no heuristic percentage detection needed.

---

## Backend: Dashboard Handler

### PanelConfig validation

The `DashboardHandler` panel validation currently requires `title`, `metric`, and `chartType`. Extend to:

- `expressionType`: required, must be "single" or "ratio"
- When `expressionType` is "ratio": `numeratorMetric` is also required
- `func`: optional, defaults to "none" if empty
- `aggregation`: optional, defaults to "none" if empty
- `groupBy`: optional, only meaningful when aggregation is not "none"
- `chartType`: required, must be "line", "bar", or "stat" (unchanged)

Backward compatibility: panels created before this feature may lack `expressionType`, `func`, `aggregation` fields. Treat missing fields as defaults: `expressionType="single"`, `func="none"`, `aggregation="none"`.

---

## Error Handling

| Error | Handling |
|-------|----------|
| Invalid function name in PromQL | Backend returns `{"status":"error","error":"unsupported function: xyz"}` |
| Aggregation without matching series | Return empty result set (no crash) |
| Ratio with denominator all zeros | Return null values, frontend shows gap |
| Rate/increase with < 5m of data | Return null for initial window, data starts after first 5m |
| Metric name not found | Return empty result set (existing behavior) |
| Numerator metric not found (ratio) | Return empty result set |

---

## i18n

New keys needed in both `en.ts` and `zh.ts`:

```typescript
// en.ts - under dashboard section or a new panelForm section
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
}

// zh.ts
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
}
```

---

## Testing

### Backend tests

- `TestParsePromQL_Simple` — existing `metric{labels}` still works
- `TestParsePromQL_Function` — `rate(metric[5m])` and `increase(metric[5m])`
- `TestParsePromQL_Aggregation` — `sum(rate(metric[5m]))`
- `TestParsePromQL_AggregationWithGroupBy` — `sum(rate(metric[5m])) by (service)`
- `TestParsePromQL_Ratio` — `rate(a[5m]) / rate(b[5m])`
- `TestParsePromQL_RatioWithAggregation` — `sum(rate(a[5m])) by (service) / sum(rate(b[5m])) by (service)`
- `TestRateComputation` — inject counter data, verify rate values are (delta / 300)
- `TestIncreaseComputation` — inject counter data, verify increase values are (delta)
- `TestAggregationComputation` — inject multi-series data, verify sum/avg/max/min
- `TestRatioComputation` — inject numerator/denominator data, verify division
- `TestRatioDivisionByZero` — denominator has zeros, verify null output
- `TestImprovedDownsampling` — verify window average and gap skipping
- `TestDashboardHandler_PanelValidation` — ratio panel requires numeratorMetric

### Frontend tests

- `TestBuildPromQL_SingleNoFunc` — `metric{labels}`
- `TestBuildPromQL_SingleRate` — `rate(metric{labels}[5m])`
- `TestBuildPromQL_SingleRateSumGroupBy` — `sum(rate(metric{labels}[5m])) by (service)`
- `TestBuildPromQL_RatioRate` — `rate(a{labels}[5m]) / rate(b{labels}[5m])`
- PanelForm: verify expression type toggle shows/hides numerator/denominator fields
- PanelForm: verify aggregation != 'none' shows groupBy field
