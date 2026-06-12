# AI Cost Dashboard Design

Date: 2026-06-12
Roadmap item: #17 — AI 成本追踪

## Overview

Add a Cost Dashboard page to Labubu that gives developers a clear view of their AI spending. The page shows total cost overview cards and a cost-by-model breakdown table, with fixed time period filtering (Today / 7 Days / 30 Days).

The existing model pricing configuration (PricingManager) and automatic cost calculation on traces/sessions already work. This design fills the missing piece: a dedicated dashboard for viewing aggregated cost data.

**Target user**: Developers tracking their own AI usage spend.

## Approach

**Chosen: Pure frontend + new backend API** (Approach A)

A new `/api/v1/cost-summary` endpoint aggregates cost data on the server side. The frontend displays it via a new `CostDashboard.vue` page with card-style layout. This is the simplest approach that follows the project's existing pattern (new handler + new view page).

Alternatives considered:
- **Approach B (reuse Dashboard panels)**: Would require changing the panel data source system from PromQL-only to support cost data. Too invasive for the benefit.
- **Approach C (frontend-only aggregation)**: Pull all traces/sessions to the browser and compute there. Performance destroys at scale; violates server-aggregate / client-display separation.

## Navigation Structure

Current:
```
Metrics → /dashboards (flat link)
```

New:
```
Metrics ▼ (nav-group, same pattern as Alerts/Settings)
  ├── Dashboard → /dashboards (unchanged)
  └── Cost → /cost (new)
```

The Metrics nav-group defaults to expanded (`metricsOpen = true`).

### Changes

- `App.vue`: Replace `<router-link to="/dashboards">` with a `nav-group` containing two sub-links. Add `metricsOpen` reactive state.
- `router.ts`: Add `{ path: '/cost', name: 'cost-dashboard', component: CostDashboard }`.
- `i18n`: Add `nav.dashboard` and `nav.cost` keys; keep `nav.metrics` as the group title.

## Backend API

### Endpoint

`GET /api/v1/cost-summary`

### Query Parameters

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `period` | string | `7d` | `today` / `7d` / `30d` |
| `currency` | string | — | Optional, filter by currency |

### Response

```json
{
  "period": "7d",
  "currency": "USD",
  "overview": {
    "total_cost": 12.34,
    "total_tokens": 156789,
    "total_input_tokens": 89000,
    "total_output_tokens": 67889,
    "avg_cost_per_trace": 0.05,
    "trace_count": 246
  },
  "by_model": [
    {
      "model": "claude-sonnet-4-5",
      "cost": 8.50,
      "tokens": 100000,
      "input_tokens": 60000,
      "output_tokens": 40000,
      "trace_count": 150,
      "avg_cost": 0.057
    }
  ]
}
```

### Implementation

- `CostHandler` in `internal/api/cost_handler.go` queries traces within the time period from Store.
- Server-side aggregation: sum cost/tokens, group by `model_name`, compute averages.
- Time filtering uses Store's existing `TraceQuery` time range fields.
- Route registration in `router.go` at `/api/v1/cost-summary`.

## Frontend Page

### Layout

```
┌─────────────────────────────────────────────────┐
│  Cost Dashboard          [Today | 7D | 30D]     │
├─────────┬──────────┬──────────┬─────────────────┤
│ Total   │  Total   │  Avg     │   Trace         │
│ Cost    │  Tokens  │  Cost/   │   Count         │
│ $12.34  │  156.8K  │  Trace   │   246           │
│         │          │  $0.05   │                 │
├─────────┴──────────┴──────────┴─────────────────┤
│  Cost by Model                                   │
│ ┌─────────────────────────────────────────────┐ │
│ │ Model │ Cost │ Tokens │ Traces │ Avg Cost   │ │
│ │ claude│ $8.50│ 100K   │ 150    │ $0.06      │ │
│ │ gpt-4o│ $3.84│ 56.8K  │ 96     │ $0.04      │ │
│ └─────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

### Components

**Period selector**: Three buttons (Today / 7D / 30D), default 7D. Matches existing Dashboard panel toggle style.

**Overview cards**: 4 big-number cards in a row:
- Total Cost (formatted with `formatCost`)
- Total Tokens (formatted with `formatNumber`, showing input/output breakdown)
- Avg Cost/Trace
- Trace Count

**Model cost table**: Standard HTML table, columns: Model, Cost, Tokens, Traces, Avg Cost. Default sort: Cost descending (most expensive first).

### API Client

Add to `client.ts`:

```typescript
export interface CostSummary {
  period: string
  currency: string
  overview: {
    total_cost: number
    total_tokens: number
    total_input_tokens: number
    total_output_tokens: number
    avg_cost_per_trace: number
    trace_count: number
  }
  by_model: ModelCost[]
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

export async function getCostSummary(period: string): Promise<CostSummary> {
  return get<CostSummary>(`${BASE_URL}/cost-summary?period=${period}`)
}
```

### i18n Keys

All under `costDashboard.*` prefix:

| Key | EN | ZH |
|-----|----|----|
| `costDashboard.title` | Cost Dashboard | 成本看板 |
| `costDashboard.today` | Today | 今天 |
| `costDashboard.7d` | 7 Days | 近 7 天 |
| `costDashboard.30d` | 30 Days | 近 30 天 |
| `costDashboard.totalCost` | Total Cost | 总花费 |
| `costDashboard.totalTokens` | Total Tokens | 总 Token 数 |
| `costDashboard.avgCostPerTrace` | Avg Cost/Trace | 平均每次调用成本 |
| `costDashboard.traceCount` | Trace Count | 调用次数 |
| `costDashboard.costByModel` | Cost by Model | 模型成本分布 |
| `costDashboard.model` | Model | 模型 |
| `costDashboard.cost` | Cost | 花费 |
| `costDashboard.tokens` | Tokens | Token 数 |
| `costDashboard.avgCost` | Avg Cost | 平均成本 |
| `costDashboard.noPricing` | No cost data yet. Configure model pricing in Settings → Model Pricing to start tracking costs. | 暂无成本数据。请在设置 → 模型定价中配置模型单价以开始追踪成本。 |
| `costDashboard.noData` | No traces in this period. | 该时间段内无链路数据。 |

Navigation keys:

| Key | EN | ZH |
|-----|----|----|
| `nav.dashboard` | Dashboard | 仪表盘 |
| `nav.cost` | Cost | 成本 |

## Empty States and Error Handling

**No pricing configured**: If no ModelPricing entries exist, trace `cost` fields are null. Show a guided prompt linking to the PricingManager page.

**Pricing configured but no traces in period**: Show zero-value cards ($0.00 / 0 tokens) and an empty-table message.

**API errors**: Display error inline on the page, consistent with the project's existing pattern (no toast notifications).

## Testing

### Backend: `internal/api/cost_handler_test.go`

Table-driven tests covering:
- All three period values (today / 7d / 30d)
- Zero values when no pricing is configured
- Multi-model aggregation correctness
- Invalid period parameter → 400 response

### Frontend

No additional frontend unit tests (project has no frontend test convention). TypeScript type checking via `vue-tsc --noEmit` covers the new interfaces.

## Change Summary

| File | Change Type |
|------|-------------|
| `internal/api/cost_handler.go` | New |
| `internal/api/cost_handler_test.go` | New |
| `internal/api/router.go` | Add cost-summary route |
| `web/src/views/CostDashboard.vue` | New |
| `web/src/api/client.ts` | Add CostSummary/ModelCost interfaces + getCostSummary function |
| `web/src/router.ts` | Add /cost route |
| `web/src/App.vue` | Metrics: flat link → nav-group |
| `web/src/i18n/locales/en.ts` | Add nav.dashboard, nav.cost, costDashboard.* keys |
| `web/src/i18n/locales/zh.ts` | Same keys in Chinese |