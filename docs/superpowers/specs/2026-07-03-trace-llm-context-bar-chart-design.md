# Trace LLM Context Bar Chart — Design

**Date:** 2026-07-03
**Scope:** Frontend-only. Add a stacked bar chart to the trace detail page showing how the LLM context (input / cache read / cache creation / output tokens) changes across the LLM calls within a single trace.

## Goal & Motivation

A trace often contains multiple LLM calls (a multi-turn agent loop). Today there is no trace-level view of how the context window evolves across those calls — you can only open each span individually and look at its `TokenPieChart`. A single stacked bar chart, one bar per LLM call ordered by time, lets you see at a glance:

- how the input context grows turn by turn,
- where cache read / cache creation kicks in (prompt caching reuse),
- the output token volume per call.

This is viewable by clicking a new **Context** button on the trace detail page.

## Non-Goals (YAGNI)

- Horizontal bar chart variant.
- Switching grouping axis (e.g. "group by model").
- Image / PNG export of the chart.
- Any backend change. All required fields already exist on the span.
- Replacing the existing per-span `TokenPieChart` (that stays in the span drawer).

## Background — What Exists

- `web/src/views/TraceDetail.vue` has an insight button group (`Logs` / `Diagnosis` / `Agent Behavior`) using a single `activeInsight` ref as a mutually-exclusive toggle. The active one renders into an insight overlay region. A 4th toggle fits this pattern exactly.
- `SpanDetail` ([web/src/api/client.ts:38-59](../../../web/src/api/client.ts#L38-L59)) already carries: `input_tokens`, `output_tokens`, `total_tokens`, `cache_creation_tokens`, `cache_read_tokens`, `gen_ai_request_model`. All optional numbers.
- Backend `Span` ([internal/storage/storage.go:14-37](../../../internal/storage/storage.go#L14-L37)) stores these as nullable `*uint32`; `total_tokens = input + cache_creation + cache_read + output` (derived in [internal/storage/tokens.go](../../../internal/storage/tokens.go)). The four buckets are non-overlapping.
- LLM span identification convention (used by `WaterfallChart.vue`): a span is an LLM call when `total_tokens > 0`. No dedicated span kind exists.
- Chart.js is already a dependency. `TokenPieChart.vue` (doughnut) and `PanelChart.vue` (line/bar) both use it. No stacked-bar component exists yet; Chart.js supports stacking natively via per-dataset `stack`.
- There is **no `traceDetail.*` i18n namespace**. Token/context labels currently live under `sessionDetail.*` and `costDashboard.*`.

## Design

### Component placement (TraceDetail.vue)

Add a 4th toggle button `Context` to the insight action group, bound to `activeInsight === 'context'`. When active, the insight overlay renders the new `<ContextBarChart>` component, passing the trace's spans. No new state beyond reusing `activeInsight`; no router change.

### Data preparation (TraceDetail.vue, computed)

A computed `contextPoints` transforms `trace.spans` into chart points:

1. Filter: keep spans where `(total_tokens ?? 0) > 0` (LLM spans, by the existing convention).
2. Sort ascending by `start_time_ms`.
3. Map each to:

```ts
interface ContextPoint {
  index: number          // 1-based, post-sort position
  spanId: string
  spanName: string
  model: string          // gen_ai_request_model ?? ''
  input: number          // input_tokens ?? 0
  cacheRead: number      // cache_read_tokens ?? 0
  cacheCreation: number  // cache_creation_tokens ?? 0
  output: number         // output_tokens ?? 0
}
```

### New component `web/src/components/ContextBarChart.vue`

- **Props:** `points: ContextPoint[]`.
- **Emits:** `select(spanId: string)`.
- **Chart type:** vertical stacked bar chart via Chart.js (`BarController` + `BarElement`, each dataset with `stack: 'tokens'`).
- **Datasets, bottom → top stack order:** `Input` → `Cache Read` → `Cache Creation` → `Output`. The three input-side segments sit adjacent at the bottom (their sum = total input-side tokens); `Output` sits on top. This makes context accumulation visually obvious.
- **x-axis:** category labels are the 1-based indices (`1`, `2`, …, `N`). Span name and model are NOT on the axis — they appear only in the tooltip.
- **y-axis:** token count, linear, starts at 0.
- **Tooltip:** span name, model (if present), the four bucket values, and the call's `total` (sum of the four).
- **Colors:** reuse the theme's CSS custom properties / accent palette (same source as `TokenPieChart`) so light/dark themes stay consistent. Each of the 4 segments gets a distinct accent shade.
- **Click interaction:** clicking a bar calls `emit('select', point.spanId)`. `TraceDetail` handles this by opening that span's drawer — reusing the existing `selectSpan(spanId)` handler so behavior matches clicking a span in the waterfall.
- **Lifecycle:** create the Chart.js instance in `onMounted`, destroy in `onBeforeUnmount`. Watch `points` (deep) and call `chart.data` update + `chart.update()` when it changes.
- **Empty state:** when `points.length < 2`, render a message ("at least 2 LLM calls are needed to show context change") instead of the chart. A single LLM call produces a one-bar chart with no "change" to show.

### Types (`web/src/api/client.ts`)

Export `ContextPoint` (interface above) from `client.ts`, per the project rule that all new types live there. `TraceDetail.vue` imports it; `ContextBarChart.vue` imports it for its prop type.

### i18n

Add a new `traceDetail` namespace to both `web/src/i18n/locales/en.ts` and `zh.ts` (mirrored keys, Chinese values):

| key | en | zh |
|-----|----|----|
| `traceDetail.context` | Context | 上下文 |
| `traceDetail.contextTitle` | Context Change Across LLM Calls | LLM 调用上下文变化 |
| `traceDetail.contextEmpty` | At least 2 LLM calls are needed to show context change. | 至少需要 2 次 LLM 调用才能展示上下文变化。 |
| `traceDetail.ctxInput` | Input | 输入 |
| `traceDetail.ctxCacheRead` | Cache Read | 缓存读取 |
| `traceDetail.ctxCacheCreation` | Cache Creation | 缓存写入 |
| `traceDetail.ctxOutput` | Output | 输出 |
| `traceDetail.ctxTotal` | Total | 合计 |

The button label reuses `traceDetail.context`. Segment labels and tooltip use the `ctx*` keys.

## Data Flow

```
getTrace() → trace.spans
   → contextPoints (computed: filter LLM spans, sort by start_time, map to ContextPoint[])
      → <ContextBarChart :points="contextPoints" @select="selectSpan">
            → Chart.js stacked bar (4 datasets, stack='tokens')
            → click bar → emit select(spanId) → selectSpan(spanId) → open drawer
```

## Error Handling & Edge Cases

- **Trace with 0 or 1 LLM call:** chart not rendered; empty-state message shown.
- **Spans missing token fields:** `?? 0` per field; a bar with all-zero buckets is excluded by the `total_tokens > 0` filter, so no zero bars appear.
- **Provider without prompt caching (e.g. OpenAI):** `cacheRead` / `cacheCreation` are 0; the bar shows only Input + Output. No special-casing needed.
- **Very long traces (many LLM calls):** Chart.js handles many bars; x-axis labels stay single-digit-ish indices. No pagination planned (a single trace rarely has hundreds of LLM calls; if it does, the chart scrolls).
- **Theme switch:** colors come from CSS variables read at mount; on theme change the chart colors update via the existing `useTheme` reactivity (same approach as `TokenPieChart`).

## Testing

The `web/` package has no JavaScript test runner (no vitest/jest) — consistent with the rest of the frontend, verification is type-check + manual:

- **Type check:** `cd web && npx vue-tsc --noEmit` must pass with the new `ContextPoint` type and the component's props.
- **Build:** `cd web && npm run build` must succeed (catches template/script compile errors).
- **Manual:** `make run`, open a trace with ≥2 LLM calls, click **Context**, verify: 4-segment stacked bars in Input → Cache Read → Cache Creation → Output order; tooltip shows span name + model + 4 buckets + total; clicking a bar opens the matching span drawer; empty-state message on a trace with <2 LLM calls; chart colors track light/dark theme switch.
- No Go tests needed (no backend change).

## Files Touched

| File | Change |
|------|--------|
| `web/src/api/client.ts` | Add `ContextPoint` interface |
| `web/src/components/ContextBarChart.vue` | New — stacked bar chart component |
| `web/src/views/TraceDetail.vue` | Add Context toggle button, `contextPoints` computed, render `ContextBarChart` in overlay, wire `@select` |
| `web/src/i18n/locales/en.ts` | Add `traceDetail.*` keys |
| `web/src/i18n/locales/zh.ts` | Add `traceDetail.*` keys (Chinese) |
