# Trace LLM Context Bar Chart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a stacked bar chart to the trace detail page that shows how Input / Cache Read / Cache Creation / Output tokens change across the LLM calls in a trace, viewable via a new "Context" insight toggle.

**Architecture:** Frontend-only. A new `ContextBarChart.vue` (Chart.js stacked vertical bar) receives an array of `ContextPoint` (one per LLM span, sorted by `start_time_ms`) and emits `select(spanId)` to open the span drawer. `TraceDetail.vue` adds a 4th insight toggle, a `contextPoints` computed, and renders the component in the existing insight overlay.

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, Chart.js (already a dep), vue-i18n, CSS custom properties for theming.

## Global Constraints

- **TypeScript strict, no `any`** (CLAUDE.md). New types go in `web/src/api/client.ts`.
- **i18n:** all user-facing strings via `t('...')`; add keys to BOTH `web/src/i18n/locales/en.ts` and `zh.ts`.
- **No backend change.** Spans already carry `input_tokens`, `output_tokens`, `total_tokens`, `cache_creation_tokens`, `cache_read_tokens`, `gen_ai_request_model`.
- **LLM span = `(total_tokens ?? 0) > 0`** (existing convention in `WaterfallChart.vue`).
- **No JS test runner** in `web/`; verification = `npx vue-tsc --noEmit` + `npm run build` + manual.
- **Colors** reuse existing theme CSS vars (`--chart-pie-*`, `--chart-pie-border`, `--bg-secondary`, `--text-primary`, `--text-secondary`, `--border-group`) — same source as `TokenPieChart.vue`, so light/dark themes stay consistent.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `web/src/api/client.ts` | Export `ContextPoint` interface (single source of truth for the chart's data shape). |
| `web/src/components/ContextBarChart.vue` | New. Self-contained stacked bar chart. Props: `points: ContextPoint[]`; emits `select(spanId)`. Owns Chart.js lifecycle, theming, tooltip, empty state. |
| `web/src/views/TraceDetail.vue` | Add Context toggle button, `contextPoints` computed, `openDrawerBySpanId` handler, render `<ContextBarChart>` in the insight overlay. |
| `web/src/i18n/locales/en.ts` | Add `traceDetail.*` namespace. |
| `web/src/i18n/locales/zh.ts` | Mirror `traceDetail.*` with Chinese values. |

---

## Task 1: Add `ContextPoint` type

**Files:**
- Modify: `web/src/api/client.ts` (add export near the `SpanDetail` interface, ~line 59)

**Interfaces:**
- Produces: `export interface ContextPoint { index: number; spanId: string; spanName: string; model: string; input: number; cacheRead: number; cacheCreation: number; output: number }` — imported by Task 3 (`ContextBarChart.vue`) and Task 4 (`TraceDetail.vue`).

- [ ] **Step 1: Add the interface**

In `web/src/api/client.ts`, immediately after the `SpanDetail` interface's closing `}` (around line 59), add:

```ts
/**
 * One LLM call in a trace, for the context-change bar chart.
 * Derived from spans where total_tokens > 0, sorted by start_time_ms.
 * input + cacheRead + cacheCreation + output == total_tokens.
 */
export interface ContextPoint {
  index: number          // 1-based position after sorting by start_time_ms
  spanId: string
  spanName: string
  model: string          // gen_ai_request_model ?? ''
  input: number          // input_tokens ?? 0
  cacheRead: number      // cache_read_tokens ?? 0
  cacheCreation: number  // cache_creation_tokens ?? 0
  output: number         // output_tokens ?? 0
}
```

- [ ] **Step 2: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS (no errors). The new type is exported but unused-yet — that's fine.

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat(web): add ContextPoint type for trace context bar chart"
```

---

## Task 2: Add `traceDetail` i18n keys

**Files:**
- Modify: `web/src/i18n/locales/en.ts` (insert a new `traceDetail` block after the `traceList` block, which ends at line 98 `},` — right before `sessionDetail:` at line 99)
- Modify: `web/src/i18n/locales/zh.ts` (mirror, same structural position: after `traceList` block, before `sessionDetail`)

**Interfaces:**
- Produces: keys `traceDetail.context`, `traceDetail.contextTitle`, `traceDetail.contextEmpty`, `traceDetail.ctxInput`, `traceDetail.ctxCacheRead`, `traceDetail.ctxCacheCreation`, `traceDetail.ctxOutput`, `traceDetail.ctxTotal` — consumed by Tasks 3 and 4.

- [ ] **Step 1: Add English keys**

In `web/src/i18n/locales/en.ts`, between the end of `traceList` (line 98 `},`) and `sessionDetail: {` (line 99), insert:

```ts
  traceDetail: {
    context: 'Context',
    contextTitle: 'Context Change Across LLM Calls',
    contextEmpty: 'At least 2 LLM calls are needed to show context change.',
    ctxInput: 'Input',
    ctxCacheRead: 'Cache Read',
    ctxCacheCreation: 'Cache Creation',
    ctxOutput: 'Output',
    ctxTotal: 'Total',
  },
```

- [ ] **Step 2: Add Chinese keys (mirror)**

Open `web/src/i18n/locales/zh.ts`, find the same spot (end of `traceList` block, before `sessionDetail:`). Insert:

```ts
  traceDetail: {
    context: '上下文',
    contextTitle: 'LLM 调用上下文变化',
    contextEmpty: '至少需要 2 次 LLM 调用才能展示上下文变化。',
    ctxInput: '输入',
    ctxCacheRead: '缓存读取',
    ctxCacheCreation: '缓存写入',
    ctxOutput: '输出',
    ctxTotal: '合计',
  },
```

- [ ] **Step 3: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS. (vue-i18n key typing may not check unknown keys; this still confirms no TS breakage.)

- [ ] **Step 4: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat(web): add traceDetail i18n keys for context bar chart"
```

---

## Task 3: Create `ContextBarChart.vue`

**Files:**
- Create: `web/src/components/ContextBarChart.vue`

**Interfaces:**
- Consumes: `ContextPoint` from `../api/client`; i18n keys `traceDetail.contextTitle`, `traceDetail.contextEmpty`, `traceDetail.ctxInput`, `traceDetail.ctxCacheRead`, `traceDetail.ctxCacheCreation`, `traceDetail.ctxOutput`, `traceDetail.ctxTotal`.
- Produces: a Vue component default-exported via `<script setup>`, accepting `points: ContextPoint[]` prop, emitting `select` with a `spanId: string`. Imported by Task 4 as `import ContextBarChart from '../components/ContextBarChart.vue'`.

**Chart spec recap:** vertical stacked bar; 4 datasets each with `stack: 'tokens'`; bottom→top order Input → Cache Read → Cache Creation → Output; x-axis labels = 1-based indices; tooltip shows span name + model + 4 buckets + total; clicking a bar emits `select(spanId)`; empty state when `points.length < 2`.

- [ ] **Step 1: Write the component file**

Create `web/src/components/ContextBarChart.vue` with this exact content:

```vue
<template>
  <div class="context-chart">
    <h3 class="chart-title">{{ t('traceDetail.contextTitle') }}</h3>

    <div v-if="points.length < 2" class="empty-state">
      {{ t('traceDetail.contextEmpty') }}
    </div>

    <div v-else class="chart-container">
      <canvas ref="canvasRef"></canvas>
    </div>

    <!-- Legend (4 segments) -->
    <div v-if="points.length >= 2" class="segment-legend">
      <div v-for="seg in legend" :key="seg.key" class="legend-item">
        <span class="legend-dot" :style="{ background: seg.color }"></span>
        <span class="legend-label">{{ seg.label }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart, BarController, BarElement, CategoryScale, LinearScale, Tooltip,
} from 'chart.js'
import type { ContextPoint } from '../api/client'
import { useTheme } from '../composables/useTheme'

Chart.register(BarController, BarElement, CategoryScale, LinearScale, Tooltip)

const props = defineProps<{ points: ContextPoint[] }>()
const emit = defineEmits<{ (e: 'select', spanId: string): void }>()

const { t } = useI18n()
const { theme } = useTheme()

const canvasRef = ref<HTMLCanvasElement | null>(null)
let chart: Chart<'bar'> | null = null

// --- Theme-aware colors (reuse existing --chart-pie-* vars) ---
function getCSSVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

interface SegmentMeta { key: string; label: string; varName: string }

const SEGMENTS: SegmentMeta[] = [
  { key: 'input',         label: 'ctxInput',         varName: '--chart-pie-user' },
  { key: 'cacheRead',     label: 'ctxCacheRead',     varName: '--chart-pie-tool' },
  { key: 'cacheCreation', label: 'ctxCacheCreation', varName: '--chart-pie-assistant' },
  { key: 'output',        label: 'ctxOutput',        varName: '--chart-pie-output' },
]

const legend = computed(() =>
  SEGMENTS.map(s => ({ key: s.key, label: t(`traceDetail.${s.label}`), color: getCSSVar(s.varName) }))
)

function bucketValue(p: ContextPoint, key: string): number {
  if (key === 'input') return p.input
  if (key === 'cacheRead') return p.cacheRead
  if (key === 'cacheCreation') return p.cacheCreation
  return p.output
}

function createChart() {
  if (!canvasRef.value || props.points.length < 2) return

  if (chart) {
    chart.destroy()
    chart = null
  }

  const labels = props.points.map(p => String(p.index))
  const borderColor = getCSSVar('--chart-pie-border')
  const tooltipBg = getCSSVar('--bg-secondary')
  const tooltipTitleColor = getCSSVar('--text-primary')
  const tooltipBodyColor = getCSSVar('--text-secondary')
  const tooltipBorderColor = getCSSVar('--border-group')

  const datasets = SEGMENTS.map(s => ({
    label: t(`traceDetail.${s.label}`),
    data: props.points.map(p => bucketValue(p, s.key)),
    backgroundColor: getCSSVar(s.varName),
    borderColor,
    borderWidth: 1,
    stack: 'tokens',
  }))

  // Capture for click handler closure.
  const pointsSnapshot = props.points

  chart = new Chart(canvasRef.value, {
    type: 'bar',
    data: { labels, datasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'index', intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: tooltipBg,
          titleColor: tooltipTitleColor,
          bodyColor: tooltipBodyColor,
          borderColor: tooltipBorderColor,
          borderWidth: 1,
          padding: 12,
          cornerRadius: 6,
          callbacks: {
            title: (items: any[]) => {
              const idx = items[0]?.dataIndex ?? 0
              const p = pointsSnapshot[idx]
              if (!p) return ''
              return p.spanName + (p.model ? ` (${p.model})` : '')
            },
            label: (ctx: any) => {
              const p = pointsSnapshot[ctx.dataIndex]
              if (!p) return ''
              const key = SEGMENTS[ctx.datasetIndex].key
              const val = bucketValue(p, key).toLocaleString()
              return ` ${ctx.dataset.label}: ${val}`
            },
            footer: (items: any[]) => {
              const idx = items[0]?.dataIndex ?? 0
              const p = pointsSnapshot[idx]
              if (!p) return ''
              const total = (p.input + p.cacheRead + p.cacheCreation + p.output).toLocaleString()
              return `${t('traceDetail.ctxTotal')}: ${total}`
            },
          },
        },
      },
      scales: {
        x: { stacked: true, grid: { display: false }, ticks: { color: getCSSVar('--text-secondary') } },
        y: { stacked: true, beginAtZero: true, grid: { color: getCSSVar('--border-group') }, ticks: { color: getCSSVar('--text-secondary') } },
      },
      onClick: (_evt: any, elements: any[]) => {
        if (elements.length > 0) {
          const el = elements[0]
          const p = pointsSnapshot[el.index]
          if (p) emit('select', p.spanId)
        }
      },
      animation: { duration: 400 },
    },
  })
}

watch(() => props.points, () => { requestAnimationFrame(createChart) }, { deep: true })
watch(theme, () => { requestAnimationFrame(createChart) })

onMounted(() => { requestAnimationFrame(createChart) })
onBeforeUnmount(() => {
  if (chart) { chart.destroy(); chart = null }
})
</script>

<style scoped>
.context-chart {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  padding: 16px;
}
.chart-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 12px;
}
.chart-container {
  position: relative;
  width: 100%;
  height: 320px;
}
.empty-state {
  color: var(--text-secondary);
  font-size: 13px;
  padding: 24px 0;
  text-align: center;
}
.segment-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid var(--border-default);
}
.legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}
.legend-dot {
  width: 10px;
  height: 10px;
  border-radius: 2px;
  flex-shrink: 0;
}
</style>
```

- [ ] **Step 2: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS. Watch for: `ContextPoint` import resolves; `Chart<'bar'>` typing accepted; `defineEmits` signature correct.

If `vue-tsc` complains about the `any[]` in tooltip callbacks — those are inside Chart.js callback params where Chart.js typings use `TooltipItem<'bar'>[]`. If strict `any` is flagged, replace `(items: any[])` with `import type { TooltipItem } from 'chart.js'` and use `TooltipItem<'bar'>[]`. Prefer keeping `any` only if the project's existing `TokenPieChart.vue` uses `any` in its callbacks (it does — see `ctx: any` at TokenPieChart.vue:172). Keep consistent with the existing file.

- [ ] **Step 3: Build**

Run: `cd web && npm run build`
Expected: PASS (vite build succeeds, no template/script compile errors).

- [ ] **Step 4: Commit**

```bash
git add web/src/components/ContextBarChart.vue
git commit -m "feat(web): add ContextBarChart stacked bar component"
```

---

## Task 4: Wire ContextBarChart into TraceDetail

**Files:**
- Modify: `web/src/views/TraceDetail.vue`
  - import line (~line 208): add `ContextBarChart` import + `ContextPoint` type import
  - `activeInsight` ref type (~line 232): add `'context'`
  - `toggleInsight` signature (~line 234): add `'context'`
  - add `contextPoints` computed (after `selectedSpanOutputTokens`, ~line 292)
  - add `openDrawerBySpanId` function (after `openDrawer`, ~line 425)
  - template: add 4th toggle button (~line 50, after the Agent Behavior button)
  - template: insight overlay title ternary (~line 60-64): add `context` branch
  - template: insight overlay body (~line 88, after the `<AgentBehaviorTab>` block): add `<ContextBarChart>`

**Interfaces:**
- Consumes: `ContextBarChart` component (Task 3), `ContextPoint` type (Task 1), i18n keys (Task 2), `trace.spans` (existing), `openDrawer` pattern (existing at line 422).

- [ ] **Step 1: Update imports**

In `web/src/views/TraceDetail.vue` line 208, extend the `../api/client` import to include `type ContextPoint`. The current line is:

```ts
import { getTrace, getLogsByTrace, getLogCounts, listLogs, getDiagnosisResult, diagnoseTrace, type TraceDetailResponse, type SpanDetail as SpanDetailType, type LogRecord, type DiagnosisResult } from '../api/client'
```

Change to:

```ts
import { getTrace, getLogsByTrace, getLogCounts, listLogs, getDiagnosisResult, diagnoseTrace, type TraceDetailResponse, type SpanDetail as SpanDetailType, type LogRecord, type DiagnosisResult, type ContextPoint } from '../api/client'
```

Then add the component import (after line 210 `import AgentBehaviorTab from '../components/AgentBehaviorTab.vue'`):

```ts
import ContextBarChart from '../components/ContextBarChart.vue'
```

- [ ] **Step 2: Extend `activeInsight` and `toggleInsight`**

Line 232:

```ts
const activeInsight = ref<'logs' | 'diagnosis' | 'agent' | null>(null)
```

Change to:

```ts
const activeInsight = ref<'logs' | 'diagnosis' | 'agent' | 'context' | null>(null)
```

Lines 234-240 — the current function:

```ts
function toggleInsight(insight: 'logs' | 'diagnosis' | 'agent') {
  if (activeInsight.value === insight) {
    activeInsight.value = null
  } else {
    activeInsight.value = insight
  }
}
```

Change the parameter type to:

```ts
function toggleInsight(insight: 'logs' | 'diagnosis' | 'agent' | 'context') {
  if (activeInsight.value === insight) {
    activeInsight.value = null
  } else {
    activeInsight.value = insight
  }
}
```

- [ ] **Step 3: Add `contextPoints` computed**

After `selectedSpanOutputTokens` (ends at line 292), add:

```ts
/** LLM calls in this trace, sorted by start time — drives the context bar chart. */
const contextPoints = computed<ContextPoint[]>(() => {
  const spans = trace.value?.spans
  if (!spans) return []
  return spans
    .filter(s => (s.total_tokens ?? 0) > 0)
    .slice()
    .sort((a, b) => a.start_time_ms - b.start_time_ms)
    .map((s, i) => ({
      index: i + 1,
      spanId: s.span_id,
      spanName: s.name,
      model: s.gen_ai_request_model ?? '',
      input: s.input_tokens ?? 0,
      cacheRead: s.cache_read_tokens ?? 0,
      cacheCreation: s.cache_creation_tokens ?? 0,
      output: s.output_tokens ?? 0,
    }))
})
```

- [ ] **Step 4: Add `openDrawerBySpanId` handler**

After `openDrawer` (ends at line 425), add:

```ts
function openDrawerBySpanId(spanId: string) {
  const span = trace.value?.spans.find(s => s.span_id === spanId)
  if (span) openDrawer(span)
}
```

- [ ] **Step 5: Add the 4th toggle button (template)**

In the `.summary-actions` div, after the Agent Behavior button (lines 50-52), add a fourth button before the closing `</div>` (line 53):

```html
            <button :class="['btn-insight', { active: activeInsight === 'context' }]" @click="toggleInsight('context')">
              {{ t('traceDetail.context') }}
            </button>
```

- [ ] **Step 6: Extend the overlay title ternary (template)**

Lines 60-64 currently:

```html
          <span class="insight-overlay-title">{{
            activeInsight === 'logs' ? t('logList.logCount', { count: totalLogCount })
            : activeInsight === 'diagnosis' ? t('diagnosis.tab')
            : t('agentStats.agentBehavior')
          }}</span>
```

Change to add a `context` branch:

```html
          <span class="insight-overlay-title">{{
            activeInsight === 'logs' ? t('logList.logCount', { count: totalLogCount })
            : activeInsight === 'diagnosis' ? t('diagnosis.tab')
            : activeInsight === 'agent' ? t('agentStats.agentBehavior')
            : t('traceDetail.contextTitle')
          }}</span>
```

- [ ] **Step 7: Render `<ContextBarChart>` in the overlay body (template)**

After the `<AgentBehaviorTab>` block (lines 85-88):

```html
          <AgentBehaviorTab
            v-if="activeInsight === 'agent'"
            :spans="trace.spans"
          />
```

Add immediately after it:

```html
          <ContextBarChart
            v-if="activeInsight === 'context'"
            :points="contextPoints"
            @select="openDrawerBySpanId"
          />
```

- [ ] **Step 8: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS. Confirms: `ContextPoint` import used, `contextPoints` returns correct shape, `activeInsight` union accepts `'context'`, `@select` handler matches the emitted `spanId: string`.

- [ ] **Step 9: Build**

Run: `cd web && npm run build`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add web/src/views/TraceDetail.vue
git commit -m "feat(web): add Context insight toggle with bar chart to trace detail"
```

---

## Task 5: Manual verification

**Files:** none (verification only).

- [ ] **Step 1: Start the dev server**

Run: `cd d:/opensource/github/labubu && make run`
Expected: server starts on http://localhost:8080.

- [ ] **Step 2: Open a trace with ≥2 LLM calls**

In a browser, go to http://localhost:8080, open the trace list, pick a trace that has at least 2 LLM spans (e.g. an agent loop trace). Verify the **Context** button appears in the insight action group alongside Logs / Diagnosis / Agent Behavior.

- [ ] **Step 3: Verify the chart**

Click **Context**. Verify:
- The insight overlay opens with title "Context Change Across LLM Calls" (en) / "LLM 调用上下文变化" (zh).
- A stacked bar chart renders: one bar per LLM call, x-axis shows 1, 2, 3, ….
- Each bar has 4 stacked segments in bottom→top order: Input, Cache Read, Cache Creation, Output (colors per legend).
- The 4-segment legend appears below the chart.
- Hovering a bar shows a tooltip with: span name (+ model in parens if present), the 4 bucket values labeled, and "Total: <sum>".

- [ ] **Step 4: Verify click-to-open-drawer**

Click a bar. Verify: the insight overlay stays/closes per existing behavior, and the span drawer opens showing the matching span (same span that bar represents). Cross-check the drawer's span name matches the bar's tooltip name.

- [ ] **Step 5: Verify empty state**

Open a trace with only 1 LLM call (or 0). Click **Context**. Verify the empty-state message shows: "At least 2 LLM calls are needed to show context change." (en) / "至少需要 2 次 LLM 调用才能展示上下文变化。" (zh). No chart canvas.

- [ ] **Step 6: Verify theme switch**

Toggle the theme (light↔dark) while the chart is visible. Verify the bar colors, axis ticks, tooltip background, and gridlines all update to match the new theme (colors come from CSS variables, re-read via the `theme` watcher).

- [ ] **Step 7: Verify language switch**

Toggle the language (en↔zh) while the chart is visible. Verify the button label, overlay title, legend labels, and tooltip labels all switch language.

If any step fails, fix in the relevant component and re-run type check + build before re-testing. No commit needed for a passing manual verification (the code commits happened in Tasks 1–4).
