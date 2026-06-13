# AI Cost Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Cost Dashboard page that shows total cost overview cards and a cost-by-model breakdown table with time period filtering.

**Architecture:** New `GetCostSummary` Store method aggregates cost data at the storage layer (memstore iterates internal spans/traces, chDB uses SQL GROUP BY). A `CostHandler` exposes this via `/api/v1/cost-summary`. Frontend `CostDashboard.vue` displays the data in card+table layout. The Metrics nav item becomes a nav-group containing Dashboard and Cost sub-items.

**Tech Stack:** Go 1.19 (backend handler + Store method), Vue 3 Composition API + TypeScript (frontend), vue-i18n (i18n)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/storage/storage.go` | Add `CostQuery`, `CostOverview`, `ModelCostItem`, `CostSummaryResult` types; add `GetCostSummary` to Store interface |
| `internal/storage/memstore.go` | Implement `GetCostSummary` — iterate traces in time range, find model from spans, aggregate |
| `internal/api/cost_handler.go` | New — parse period param, compute time range, call store.GetCostSummary, return JSON |
| `internal/api/cost_handler_test.go` | New — test CostHandler with mock store |
| `internal/api/trace_handler_test.go` | Modify — add `GetCostSummary` method to `handlerMockStore` |
| `internal/api/router.go` | Modify — add `costHandler` parameter and `/api/v1/cost-summary` route |
| `cmd/labubu/main.go` | Modify — create CostHandler, pass to NewRouter |
| `web/src/views/CostDashboard.vue` | New — page with overview cards + model cost table |
| `web/src/api/client.ts` | Modify — add `CostSummary`, `ModelCost`, `CostOverview` interfaces; add `getCostSummary` function |
| `web/src/utils/format.ts` | Modify — add `formatNumber` function for token count display |
| `web/src/router.ts` | Modify — add `/cost` route |
| `web/src/App.vue` | Modify — Metrics flat link → nav-group with Dashboard + Cost |
| `web/src/i18n/locales/en.ts` | Modify — add `nav.dashboard`, `nav.cost`, `costDashboard.*` keys |
| `web/src/i18n/locales/zh.ts` | Modify — add same keys in Chinese |

---

### Task 1: Add cost summary types to storage.go

**Files:**
- Modify: `internal/storage/storage.go`

- [ ] **Step 1: Add CostQuery type after TraceQuery**

Add after line 84 (after `TraceQuery` struct closing brace):

```go
// CostQuery defines filters for cost summary aggregation.
type CostQuery struct {
	StartTimeMS uint64
	EndTimeMS   uint64
}

// CostOverview holds aggregated cost totals.
type CostOverview struct {
	TotalCost        float64 `json:"total_cost"`
	TotalTokens      uint64  `json:"total_tokens"`
	TotalInputTokens uint64  `json:"total_input_tokens"`
	TotalOutputTokens uint64 `json:"total_output_tokens"`
	AvgCostPerTrace  float64 `json:"avg_cost_per_trace"`
	TraceCount       int     `json:"trace_count"`
}

// ModelCostItem holds cost aggregation for a single model.
type ModelCostItem struct {
	Model        string  `json:"model"`
	Cost         float64 `json:"cost"`
	Tokens       uint64  `json:"tokens"`
	InputTokens  uint64  `json:"input_tokens"`
	OutputTokens uint64  `json:"output_tokens"`
	TraceCount   int     `json:"trace_count"`
	AvgCost      float64 `json:"avg_cost"`
}

// CostSummaryResult holds the full cost dashboard response.
type CostSummaryResult struct {
	Period   string        `json:"period"`
	Currency string        `json:"currency"`
	Overview CostOverview  `json:"overview"`
	ByModel  []ModelCostItem `json:"by_model"`
}
```

- [ ] **Step 2: Add GetCostSummary to Store interface**

Add after the `UpdateTraceCost` method (line 279):

```go
	// GetCostSummary returns aggregated cost data for the given time range.
	GetCostSummary(ctx context.Context, q CostQuery) (*CostSummaryResult, error)
```

- [ ] **Step 3: Run Go build to verify types compile**

Run: `cd D:/code/opensource/github/labubu && go build ./internal/storage/`
Expected: compile error because memStore and handlerMockStore don't implement `GetCostSummary` yet — that's expected, we'll fix in later tasks. If there are other compile errors in storage.go itself, fix them.

- [ ] **Step 4: Commit**

```bash
git add internal/storage/storage.go
git commit -m "feat: add CostQuery/CostSummaryResult types and GetCostSummary to Store interface"
```

---

### Task 2: Implement memStore.GetCostSummary

**Files:**
- Modify: `internal/storage/memstore.go`

- [ ] **Step 1: Add GetCostSummary method to memStore**

Add at the end of `memstore.go`, before the closing of the file (after the `UpdateTraceCost` method, around line 865):

```go
func (m *memStore) GetCostSummary(ctx context.Context, q CostQuery) (*CostSummaryResult, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect traces in the time range.
	type modelAgg struct {
		cost         float64
		tokens       uint64
		inputTokens  uint64
		outputTokens uint64
		traceCount   int
	}
	aggByModel := make(map[string]*modelAgg)
	var totalCost float64
	var totalTokens uint64
	var totalInputTokens uint64
	var totalOutputTokens uint64
	traceCount := 0
	var currency string

	for traceID, t := range m.traces {
		if q.StartTimeMS > 0 && t.StartTimeMS < q.StartTimeMS {
			continue
		}
		if q.EndTimeMS > 0 && t.StartTimeMS > q.EndTimeMS {
			continue
		}
		if t.Cost == nil || *t.Cost == 0 {
			continue // skip traces with no cost
		}

		traceCount++
		totalCost += *t.Cost
		if t.TotalTokens != nil {
			totalTokens += uint64(*t.TotalTokens)
		}
		if currency == "" && t.CostCurrency != "" {
			currency = t.CostCurrency
		}

		// Find the model name from spans.
		modelName := ""
		for _, s := range m.spans {
			if s.TraceID == traceID && s.GenAIRequestModel != nil && *s.GenAIRequestModel != "" {
				modelName = *s.GenAIRequestModel
				if s.InputTokens != nil {
					totalInputTokens += uint64(*s.InputTokens)
				}
				if s.OutputTokens != nil {
					totalOutputTokens += uint64(*s.OutputTokens)
				}
				break // use the first span's model
			}
		}
		if modelName == "" {
			modelName = "(unknown)"
		}

		entry, exists := aggByModel[modelName]
		if !exists {
			entry = &modelAgg{}
			aggByModel[modelName] = entry
		}
		entry.cost += *t.Cost
		if t.TotalTokens != nil {
			entry.tokens += uint64(*t.TotalTokens)
		}
		entry.traceCount++

		// Also accumulate input/output tokens for the model entry.
		for _, s := range m.spans {
			if s.TraceID == traceID && s.GenAIRequestModel != nil && *s.GenAIRequestModel == modelName {
				if s.InputTokens != nil {
					entry.inputTokens += uint64(*s.InputTokens)
				}
				if s.OutputTokens != nil {
					entry.outputTokens += uint64(*s.OutputTokens)
				}
			}
		}
	}

	// Build ByModel slice, sorted by cost descending.
	byModel := make([]ModelCostItem, 0, len(aggByModel))
	for model, agg := range aggByModel {
		avgCost := 0.0
		if agg.traceCount > 0 {
			avgCost = agg.cost / float64(agg.traceCount)
		}
		byModel = append(byModel, ModelCostItem{
			Model:        model,
			Cost:         agg.cost,
			Tokens:       agg.tokens,
			InputTokens:  agg.inputTokens,
			OutputTokens: agg.outputTokens,
			TraceCount:   agg.traceCount,
			AvgCost:      avgCost,
		})
	}
	sort.Slice(byModel, func(i, j int) bool {
		return byModel[i].Cost > byModel[j].Cost
	})

	avgCostPerTrace := 0.0
	if traceCount > 0 {
		avgCostPerTrace = totalCost / float64(traceCount)
	}

	return &CostSummaryResult{
		Period:   "",
		Currency: currency,
		Overview: CostOverview{
			TotalCost:         totalCost,
			TotalTokens:       totalTokens,
			TotalInputTokens:  totalInputTokens,
			TotalOutputTokens: totalOutputTokens,
			AvgCostPerTrace:   avgCostPerTrace,
			TraceCount:        traceCount,
		},
		ByModel: byModel,
	}, nil
}
```

Make sure `sort` is in the import list of `memstore.go` (it should already be there since `ListTraces` uses `sort.Slice`).

- [ ] **Step 2: Run Go build to verify memStore compiles**

Run: `cd D:/code/opensource/github/labubu && go build ./internal/storage/`
Expected: BUILD SUCCESS (memStore now implements the full Store interface including GetCostSummary)

- [ ] **Step 3: Run existing storage tests**

Run: `cd D:/code/opensource/github/labubu && go test ./internal/storage/ -v`
Expected: PASS (existing tests unchanged)

- [ ] **Step 4: Commit**

```bash
git add internal/storage/memstore.go
git commit -m "feat: implement memStore.GetCostSummary for cost aggregation"
```

---

### Task 3: Create CostHandler

**Files:**
- Create: `internal/api/cost_handler.go`

- [ ] **Step 1: Write CostHandler**

Create `internal/api/cost_handler.go`:

```go
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labubu/labubu/internal/storage"
)

// CostHandler holds HTTP handlers for cost summary endpoints.
type CostHandler struct {
	store storage.Store
}

// NewCostHandler creates a new CostHandler.
func NewCostHandler(store storage.Store) *CostHandler {
	return &CostHandler{store: store}
}

// ServeHTTP dispatches cost summary requests.
func (h *CostHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	h.summary(w, r)
}

func (h *CostHandler) summary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period := q.Get("period")
	if period == "" {
		period = "7d"
	}

	now := time.Now()
	var startMS, endMS uint64
	endMS = uint64(now.UnixMilli())

	switch period {
	case "today":
		startMS = uint64(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli())
	case "7d":
		startMS = uint64(now.Add(-7 * 24 * time.Hour).UnixMilli())
	case "30d":
		startMS = uint64(now.Add(-30 * 24 * time.Hour).UnixMilli())
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid period, use: today, 7d, 30d"})
		return
	}

	result, err := h.store.GetCostSummary(r.Context(), storage.CostQuery{
		StartTimeMS: startMS,
		EndTimeMS:   endMS,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Set the period label in the result.
	result.Period = period

	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 2: Run Go build to verify handler compiles**

Run: `cd D:/code/opensource/github/labubu && go build ./internal/api/`
Expected: compile error because `handlerMockStore` in trace_handler_test.go doesn't implement `GetCostSummary` yet — that's expected. The handler itself should compile fine.

- [ ] **Step 3: Commit**

```bash
git add internal/api/cost_handler.go
git commit -m "feat: add CostHandler with period-based cost summary endpoint"
```

---

### Task 4: Update mock store and write CostHandler tests

**Files:**
- Modify: `internal/api/trace_handler_test.go` — add `GetCostSummary` to `handlerMockStore`
- Create: `internal/api/cost_handler_test.go`

- [ ] **Step 1: Add GetCostSummary to handlerMockStore**

In `internal/api/trace_handler_test.go`, add a new field and method to `handlerMockStore` struct (after line 22, add `costSummary` field):

After the `handlerMockStore` struct definition (after line 22), add the field:

```go
	costSummary *storage.CostSummaryResult
	costSummaryErr error
```

After the `Close()` method (after line 90), add:

```go
func (m *handlerMockStore) GetCostSummary(ctx context.Context, q storage.CostQuery) (*storage.CostSummaryResult, error) {
	return m.costSummary, m.costSummaryErr
}
```

- [ ] **Step 2: Write CostHandler tests**

Create `internal/api/cost_handler_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

func TestCostSummaryDefaultPeriod(t *testing.T) {
	cost := 5.0
	tokens := uint64(10000)
	store := &handlerMockStore{
		costSummary: &storage.CostSummaryResult{
			Period:   "7d",
			Currency: "USD",
			Overview: storage.CostOverview{
				TotalCost:       cost,
				TotalTokens:     tokens,
				TraceCount:      10,
				AvgCostPerTrace: 0.5,
			},
			ByModel: []storage.ModelCostItem{
				{Model: "claude-sonnet-4-5", Cost: 3.0, Tokens: 6000, TraceCount: 6, AvgCost: 0.5},
				{Model: "gpt-4o", Cost: 2.0, Tokens: 4000, TraceCount: 4, AvgCost: 0.5},
			},
		},
	}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.CostSummaryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result.Period != "7d" {
		t.Errorf("expected period '7d', got '%s'", result.Period)
	}
	if result.Overview.TotalCost != 5.0 {
		t.Errorf("expected total_cost 5.0, got %f", result.Overview.TotalCost)
	}
	if len(result.ByModel) != 2 {
		t.Errorf("expected 2 models, got %d", len(result.ByModel))
	}
}

func TestCostSummaryTodayPeriod(t *testing.T) {
	store := &handlerMockStore{
		costSummary: &storage.CostSummaryResult{
			Period:   "today",
			Currency: "USD",
			Overview: storage.CostOverview{TotalCost: 1.0, TraceCount: 2},
		},
	}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary?period=today", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result storage.CostSummaryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.Period != "today" {
		t.Errorf("expected period 'today', got '%s'", result.Period)
	}
}

func TestCostSummaryInvalidPeriod(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary?period=invalid", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid period, got %d", rec.Code)
	}
}

func TestCostSummaryMethodNotAllowed(t *testing.T) {
	store := &handlerMockStore{}
	handler := NewCostHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cost-summary", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rec.Code)
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `cd D:/code/opensource/github/labubu && go test ./internal/api/ -v -run TestCostSummary`
Expected: ALL PASS

- [ ] **Step 4: Run all API tests to verify mock update didn't break anything**

Run: `cd D:/code/opensource/github/labubu && go test ./internal/api/ -v`
Expected: ALL PASS (existing tests still work with updated mock)

- [ ] **Step 5: Commit**

```bash
git add internal/api/trace_handler_test.go internal/api/cost_handler_test.go
git commit -m "feat: add CostHandler tests and update mock store with GetCostSummary"
```

---

### Task 5: Wire CostHandler into router and main.go

**Files:**
- Modify: `internal/api/router.go`
- Modify: `cmd/labubu/main.go`

- [ ] **Step 1: Add costHandler parameter to NewRouter**

In `internal/api/router.go`, modify the `NewRouter` function signature (line 13) to add `costHandler *CostHandler`:

```go
func NewRouter(traceHandler *TraceHandler, metricsHandler *MetricsHandler, dashboardHandler *DashboardHandler, sessionHandler *SessionHandler, logHandler *LogHandler, pricingHandler *PricingHandler, llmConfigHandler *LLMConfigHandler, alertHandler http.Handler, costHandler *CostHandler) http.Handler {
```

Add route registration after the alerting routes block (after line 88), before the health check:

```go
	// API routes — cost summary.
	if costHandler != nil {
		mux.HandleFunc("/api/v1/cost-summary/", costHandler.ServeHTTP)
		mux.HandleFunc("/api/v1/cost-summary", costHandler.ServeHTTP)
	}
```

- [ ] **Step 2: Create CostHandler in main.go**

In `cmd/labubu/main.go`, after line 197 (`llmConfigHandler := api.NewLLMConfigHandler(store)`), add:

```go
		costHandler := api.NewCostHandler(store)
```

Update the `NewRouter` call on line 202 to include `costHandler`:

```go
		router := api.NewRouter(traceHandler, metricsHandler, dashboardHandler, sessionHandler, logHandler, pricingHandler, llmConfigHandler, alertHandler, costHandler)
```

- [ ] **Step 3: Run Go build to verify everything compiles**

Run: `cd D:/code/opensource/github/labubu && go build ./cmd/labubu/ ./internal/api/ ./internal/storage/`
Expected: BUILD SUCCESS

- [ ] **Step 4: Run all tests**

Run: `cd D:/code/opensource/github/labubu && go test ./internal/api/ ./internal/storage/ -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/router.go cmd/labubu/main.go
git commit -m "feat: wire CostHandler into router and main.go"
```

---

### Task 6: Add formatNumber utility and frontend types/API

**Files:**
- Modify: `web/src/utils/format.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add formatNumber to format.ts**

In `web/src/utils/format.ts`, add after the `formatCost` function (after line 9):

```typescript
export function formatNumber(n: number | null | undefined): string {
  if (n == null) return '-'
  if (n >= 1_000_000) {
    return `${(n / 1_000_000).toFixed(1)}M`
  }
  if (n >= 1_000) {
    return `${(n / 1_000).toFixed(1)}K`
  }
  return n.toString()
}
```

- [ ] **Step 2: Add cost summary types and API function to client.ts**

In `web/src/api/client.ts`, add the following interfaces after the `ModelPricingListResponse` interface (around line 264, after the pricing section):

```typescript
// --- Cost Dashboard types and API ---

export interface CostOverview {
  total_cost: number
  total_tokens: number
  total_input_tokens: number
  total_output_tokens: number
  avg_cost_per_trace: number
  trace_count: number
}

export interface ModelCost {
  model: string
  cost: number
  tokens: number
  input_tokens: number
  output_tokens: number
  trace_count: number
  avg_cost: number
}

export interface CostSummary {
  period: string
  currency: string
  overview: CostOverview
  by_model: ModelCost[]
}

export async function getCostSummary(period: string): Promise<CostSummary> {
  return get<CostSummary>(`${BASE_URL}/cost-summary?period=${period}`)
}
```

- [ ] **Step 3: Run TypeScript type check**

Run: `cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit`
Expected: PASS (new types are just definitions, no usage yet)

- [ ] **Step 4: Commit**

```bash
git add web/src/utils/format.ts web/src/api/client.ts
git commit -m "feat: add formatNumber utility and CostSummary types/API client"
```

---

### Task 7: Add i18n keys

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

- [ ] **Step 1: Add keys to en.ts**

In `web/src/i18n/locales/en.ts`, modify the `nav` section (around line 22-31) to add `dashboard` and `cost`:

```typescript
  nav: {
    traces: 'Trace',
    sessions: 'Sessions',
    metrics: 'Metrics',
    dashboard: 'Dashboard',
    cost: 'Cost',
    logs: 'Logs',
    alerts: 'Alerts',
    settings: 'Settings',
    modelPricing: 'Model Pricing',
    llmConfigs: 'LLM Configs',
  },
```

Add a new `costDashboard` section at the end of the export object (before the closing `}`):

```typescript
  costDashboard: {
    title: 'Cost Dashboard',
    today: 'Today',
    '7d': '7 Days',
    '30d': '30 Days',
    totalCost: 'Total Cost',
    totalTokens: 'Total Tokens',
    avgCostPerTrace: 'Avg Cost/Trace',
    traceCount: 'Trace Count',
    costByModel: 'Cost by Model',
    model: 'Model',
    cost: 'Cost',
    tokens: 'Tokens',
    avgCost: 'Avg Cost',
    traces: 'Traces',
    noPricing: 'No cost data yet. Configure model pricing in Settings → Model Pricing to start tracking costs.',
    noData: 'No traces in this period.',
  },
```

- [ ] **Step 2: Add keys to zh.ts**

In `web/src/i18n/locales/zh.ts`, modify the `nav` section (around line 22-31) to add `dashboard` and `cost`:

```typescript
  nav: {
    traces: '链路追踪',
    sessions: '会话',
    metrics: '指标监控',
    dashboard: '仪表盘',
    cost: '成本',
    logs: '日志',
    alerts: '告警',
    settings: '设置',
    modelPricing: '模型定价',
    llmConfigs: 'LLM 配置',
  },
```

Add a new `costDashboard` section at the end of the export object (before the closing `}`):

```typescript
  costDashboard: {
    title: '成本看板',
    today: '今天',
    '7d': '近 7 天',
    '30d': '近 30 天',
    totalCost: '总花费',
    totalTokens: '总 Token 数',
    avgCostPerTrace: '平均每次调用成本',
    traceCount: '调用次数',
    costByModel: '模型成本分布',
    model: '模型',
    cost: '花费',
    tokens: 'Token 数',
    avgCost: '平均成本',
    traces: '调用数',
    noPricing: '暂无成本数据。请在设置 → 模型定价中配置模型单价以开始追踪成本。',
    noData: '该时间段内无链路数据。',
  },
```

- [ ] **Step 3: Run TypeScript type check**

Run: `cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat: add i18n keys for Cost Dashboard nav and page"
```

---

### Task 8: Create CostDashboard.vue

**Files:**
- Create: `web/src/views/CostDashboard.vue`

- [ ] **Step 1: Write CostDashboard.vue**

Create `web/src/views/CostDashboard.vue`:

```vue
<template>
  <div class="cost-dashboard">
    <h2>{{ t('costDashboard.title') }}</h2>

    <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
    <div v-else-if="loadError" class="error">{{ loadError }}</div>
    <div v-else-if="noPricing" class="no-pricing">
      <p>{{ t('costDashboard.noPricing') }}</p>
      <router-link to="/settings/pricing" class="btn btn-primary">{{ t('nav.modelPricing') }}</router-link>
    </div>
    <div v-else>
      <!-- Period selector -->
      <div class="period-bar">
        <button
          v-for="p in periods"
          :key="p.key"
          :class="['btn', 'btn-preset', { active: activePeriod === p.key }]"
          @click="setPeriod(p.key)"
        >{{ t(`costDashboard.${p.key}`) }}</button>
      </div>

      <!-- Overview cards -->
      <div class="overview-cards">
        <div class="card">
          <div class="card-label">{{ t('costDashboard.totalCost') }}</div>
          <div class="card-value">{{ formatCost(summary.overview.total_cost, summary.currency) }}</div>
        </div>
        <div class="card">
          <div class="card-label">{{ t('costDashboard.totalTokens') }}</div>
          <div class="card-value">{{ formatNumber(summary.overview.total_tokens) }}</div>
          <div class="card-sub">{{ formatNumber(summary.overview.total_input_tokens) }} in / {{ formatNumber(summary.overview.total_output_tokens) }} out</div>
        </div>
        <div class="card">
          <div class="card-label">{{ t('costDashboard.avgCostPerTrace') }}</div>
          <div class="card-value">{{ formatCost(summary.overview.avg_cost_per_trace, summary.currency) }}</div>
        </div>
        <div class="card">
          <div class="card-label">{{ t('costDashboard.traceCount') }}</div>
          <div class="card-value">{{ summary.overview.trace_count }}</div>
        </div>
      </div>

      <!-- Cost by model table -->
      <h3>{{ t('costDashboard.costByModel') }}</h3>
      <table v-if="summary.by_model.length > 0" class="cost-table">
        <thead>
          <tr>
            <th>{{ t('costDashboard.model') }}</th>
            <th>{{ t('costDashboard.cost') }}</th>
            <th>{{ t('costDashboard.tokens') }}</th>
            <th>{{ t('costDashboard.traces') }}</th>
            <th>{{ t('costDashboard.avgCost') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in summary.by_model" :key="m.model">
            <td>{{ m.model }}</td>
            <td>{{ formatCost(m.cost, summary.currency) }}</td>
            <td>{{ formatNumber(m.tokens) }}</td>
            <td>{{ m.trace_count }}</td>
            <td>{{ formatCost(m.avg_cost, summary.currency) }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">{{ t('costDashboard.noData') }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCostSummary, getModelPricing, type CostSummary } from '../api/client'
import { formatCost, formatNumber } from '../utils/format'

const { t } = useI18n()

const periods = [
  { key: 'today' },
  { key: '7d' },
  { key: '30d' },
]

const activePeriod = ref('7d')
const summary = ref<CostSummary>({
  period: '7d',
  currency: 'USD',
  overview: {
    total_cost: 0,
    total_tokens: 0,
    total_input_tokens: 0,
    total_output_tokens: 0,
    avg_cost_per_trace: 0,
    trace_count: 0,
  },
  by_model: [],
})
const loading = ref(true)
const loadError = ref('')
const noPricing = ref(false)

async function fetchData() {
  loading.value = true
  loadError.value = ''
  try {
    const result = await getCostSummary(activePeriod.value)
    summary.value = result
  } catch (e: any) {
    loadError.value = e.message || 'Failed to load cost data'
  } finally {
    loading.value = false
  }
}

async function checkPricing() {
  try {
    const pricingResult = await getModelPricing()
    noPricing.value = pricingResult.models.length === 0
  } catch {
    // If pricing API fails, still show dashboard with whatever data we have
    noPricing.value = false
  }
}

function setPeriod(key: string) {
  activePeriod.value = key
  fetchData()
}

onMounted(() => {
  checkPricing()
  fetchData()
})
</script>

<style scoped>
.cost-dashboard {
  max-width: 1200px;
  margin: 0 auto;
  padding: 24px;
}

.cost-dashboard h2 {
  margin-bottom: 16px;
}

.period-bar {
  display: flex;
  gap: 8px;
  margin-bottom: 20px;
}

.btn-preset {
  padding: 6px 16px;
  border: 1px solid var(--border-default);
  background: var(--bg-primary);
  color: var(--text-secondary);
  cursor: pointer;
  border-radius: 4px;
  font-size: 13px;
}

.btn-preset:hover {
  color: var(--text-primary);
}

.btn-preset.active {
  background: var(--accent-blue);
  color: #fff;
  border-color: var(--accent-blue);
}

.overview-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 24px;
}

.card {
  background: var(--bg-secondary);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  padding: 16px;
}

.card-label {
  font-size: 12px;
  color: var(--text-secondary);
  text-transform: uppercase;
  margin-bottom: 8px;
}

.card-value {
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
}

.card-sub {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 4px;
}

.cost-dashboard h3 {
  margin-bottom: 12px;
}

.cost-table {
  width: 100%;
  border-collapse: collapse;
}

.cost-table th, .cost-table td {
  padding: 10px 16px;
  text-align: left;
  border-bottom: 1px solid var(--border-default);
}

.cost-table th {
  font-size: 11px;
  color: var(--text-secondary);
  text-transform: uppercase;
}

.cost-table td {
  font-size: 14px;
}

.loading, .error, .empty, .no-pricing {
  text-align: center;
  padding: 40px;
  color: var(--text-secondary);
}

.no-pricing .btn {
  margin-top: 12px;
}

@media (max-width: 960px) {
  .overview-cards {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
```

- [ ] **Step 2: Run TypeScript type check**

Run: `cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add web/src/views/CostDashboard.vue
git commit -m "feat: add CostDashboard.vue page with cards and model table"
```

---

### Task 9: Add route and update App.vue navigation

**Files:**
- Modify: `web/src/router.ts`
- Modify: `web/src/App.vue`

- [ ] **Step 1: Add route to router.ts**

In `web/src/router.ts`, add the import at the top (after existing imports):

```typescript
import CostDashboard from './views/CostDashboard.vue'
```

Add the route in the routes array (after the `/dashboards` route):

```typescript
    { path: '/cost', name: 'cost-dashboard', component: CostDashboard },
```

- [ ] **Step 2: Update App.vue navigation — Metrics becomes nav-group**

In `web/src/App.vue`, modify the template. Replace the flat Metrics link (line 8):

```html
        <router-link to="/dashboards">{{ t('nav.metrics') }}</router-link>
```

With a nav-group:

```html
        <div class="nav-group">
          <button class="nav-group-title" @click="metricsOpen = !metricsOpen">
            <span class="nav-group-arrow">{{ metricsOpen ? '▼' : '▶' }}</span>
            {{ t('nav.metrics') }}
          </button>
          <div v-show="metricsOpen" class="nav-group-items">
            <router-link to="/dashboards">{{ t('nav.dashboard') }}</router-link>
            <router-link to="/cost">{{ t('nav.cost') }}</router-link>
          </div>
        </div>
```

Add the `metricsOpen` ref in the `<script setup>` section (alongside `alertsOpen` and `settingsOpen`):

```typescript
const metricsOpen = ref(true)
```

- [ ] **Step 3: Run TypeScript type check**

Run: `cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add web/src/router.ts web/src/App.vue
git commit -m "feat: add /cost route and convert Metrics nav to nav-group"
```

---

### Task 10: Final integration test

**Files:** None — verification only

- [ ] **Step 1: Run all Go tests**

Run: `cd D:/code/opensource/github/labubu && go test ./internal/... ./cmd/... -v`
Expected: ALL PASS

- [ ] **Step 2: Run TypeScript type check**

Run: `cd D:/code/opensource/github/labubu/web && npx vue-tsc --noEmit`
Expected: PASS

- [ ] **Step 3: Build the project**

Run: `cd D:/code/opensource/github/labubu && make build-nocgo`
Expected: BUILD SUCCESS

- [ ] **Step 4: Start the server and verify in browser**

Run: `cd D:/code/opensource/github/labubu && make run`

Visit http://localhost:8080 and verify:
1. Sidebar shows Metrics ▼ with Dashboard and Cost sub-items (expanded by default)
2. Clicking Cost navigates to `/cost`
3. Cost Dashboard page renders with period selector, overview cards, and model table
4. If no pricing configured, shows the "no pricing" message with link to PricingManager
5. If pricing configured with traces, shows cost data
6. Period buttons (Today/7D/30D) switch the data

- [ ] **Step 5: Commit (if any fixes were needed during verification)**

Only if changes were made during manual testing:

```bash
git add -A
git commit -m "fix: adjustments from integration testing"
```