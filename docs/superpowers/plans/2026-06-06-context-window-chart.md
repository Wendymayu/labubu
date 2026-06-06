# Context Window Usage Chart — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a time-series context window token chart with per-component stat cards to the Session Detail page, using the existing `/api/v1/query_range` metrics API.

**Architecture:** Zero backend changes. SessionDetail.vue fetches `query_range` after session data loads, parses the Prometheus matrix result into per-component series, renders a Chart.js line chart (reusing patterns from `PanelChart.vue`) plus computed stat cards (max / avg per component). i18n strings added for UI labels and component display names.

**Tech Stack:** Vue 3 + TypeScript + Chart.js (already in project)

---

### Task 1: Add i18n strings for context window section

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

- [ ] **Step 1: Add English strings**

Add a `sessionDetail` block after `sessionList` in `web/src/i18n/locales/en.ts`:

```typescript
  sessionDetail: {
    contextWindow: 'Context Window',
    noContextData: 'No context window data for this session',
    max: 'max',
    avg: 'avg',
    component: {
      system: 'System',
      user: 'User',
      assistant: 'Assistant',
      tool: 'Tool Results',
      tool_definitions: 'Tool Definitions',
      skill: 'Skill',
    },
  },
```

- [ ] **Step 2: Add Chinese strings**

Add a `sessionDetail` block after `sessionList` in `web/src/i18n/locales/zh.ts`:

```typescript
  sessionDetail: {
    contextWindow: '上下文窗口',
    noContextData: '此会话无上下文窗口数据',
    max: '峰值',
    avg: '均值',
    component: {
      system: '系统提示词',
      user: '用户消息',
      assistant: '助手历史',
      tool: '工具结果',
      tool_definitions: '工具定义',
      skill: '技能',
    },
  },
```

- [ ] **Step 3: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat: add session detail i18n strings for context window chart"
```

---

### Task 2: Add Chart.js registration and context window data fetching to SessionDetail.vue

**Files:**
- Modify: `web/src/views/SessionDetail.vue`

- [ ] **Step 1: Add imports for Chart.js, vue-i18n, and helpers**

In the `<script setup>` block (starting at line 65), replace the existing imports with:

```typescript
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getSession, type SessionDetail, type QueryResult } from '../api/client'
import {
  Chart, LineController, CategoryScale, LinearScale,
  PointElement, LineElement, Tooltip, Legend, Filler
} from 'chart.js'
import { useTheme } from '../composables/useTheme'

Chart.register(
  LineController, CategoryScale, LinearScale,
  PointElement, LineElement, Tooltip, Legend, Filler
)
```

After `const router = useRouter()`, add:

```typescript
const { t } = useI18n()
const { theme } = useTheme()
```

- [ ] **Step 2: Add context window types and constants**

Add after the imports, before `const route = useRoute()`:

```typescript
const CTX_COMPONENTS = ['system', 'user', 'assistant', 'tool', 'tool_definitions', 'skill'] as const
const CTX_COLOR_VARS: Record<string, string> = {
  system: '--chart-pie-system',
  user: '--chart-pie-user',
  assistant: '--chart-pie-assistant',
  tool: '--chart-pie-tool',
  tool_definitions: '--chart-pie-tool-defs',
  skill: '--chart-pie-skill',
}

interface CtxSeries {
  component: string
  label: string
  color: string
  values: Array<{ time: number; tokens: number }>
  maxTokens: number
  avgTokens: number
}
```

- [ ] **Step 3: Add reactive state and helper functions**

Add after `const error = ref('')` (around line 76):

```typescript
const ctxSeries = ref<CtxSeries[]>([])
const ctxLoading = ref(false)
const ctxError = ref('')
function getCSSVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function ctxLabel(component: string): string {
  // Dynamic i18n lookup — falls back to raw component name
  const { t } = useI18n ? useI18n() : { t: (k: string) => k }
  return t(`sessionDetail.component.${component}`) || component
}
```

Wait — the project may not use `vue-i18n`. Let me check. Looking at the existing `<router-link>` text `← Back to sessions`, there's no `{{ $t(...) }}` usage. The i18n is likely imported directly.

Let me adjust. The i18n files export default objects, and they're likely used via a composable or direct import. Let me look at how other components use i18n.

Actually, looking at the existing SessionList.vue for reference...

- [ ] **Step 3 (revised): Add reactive state for context window data**

Add after `const error = ref('')` (around line 76):

```typescript
// Context window chart state
const ctxLoading = ref(false)
const ctxError = ref('')

interface CtxSeries {
  component: string
  label: string
  color: string
  values: [number, number][]  // [time_ms, token_count]
  maxTokens: number
  avgTokens: number
}

const ctxSeries = ref<CtxSeries[]>([])
```

Add the CSS variable helper — since `PanelChart.vue` has the identical `getCSSVar` function, we copy the same pattern:

```typescript
function getCSSVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}
```

- [ ] **Step 4: Add ctxLabel helper for component display names**

Add after `getCSSVar`:

```typescript
function ctxLabel(component: string): string {
  return t(`sessionDetail.component.${component}`)
}
```

- [ ] **Step 5: Add fetchContextWindow function**

Add after `ctxLabel`:

```typescript
async function fetchContextWindow() {
  if (!detail.value) return

  const sessionId = detail.value.session.session_id
  const startMs = detail.value.session.first_active_ms
  const endMs = detail.value.session.last_active_ms
  const durationSec = (endMs - startMs) / 1000
  const step = Math.max(60, Math.ceil(durationSec / 60))

  const query = `gen_ai_context_tokens{jiuwenclaw.session.id="${sessionId}"}`
  const startSec = Math.floor(startMs / 1000) - 60
  const endSec = Math.floor(endMs / 1000) + 60

  ctxLoading.value = true
  ctxError.value = ''

  try {
    const url = `/api/v1/query_range?query=${encodeURIComponent(query)}&start=${startSec}&end=${endSec}&step=${step}`
    const res = await fetch(url)
    const data: QueryResult = await res.json()

    if (data.status === 'error') {
      ctxError.value = data.error || 'Query error'
      return
    }

    const results = data.data?.result || []
    ctxSeries.value = results.map(series => {
      const comp = series.metric?.component || 'unknown'
      const values = (series.values || []).map(([ts, val]: [number, string]) => [ts * 1000, parseFloat(val)] as [number, number])
      const tokenVals = values.map(v => v[1])
      const maxTokens = tokenVals.length > 0 ? Math.max(...tokenVals) : 0
      const avgTokens = tokenVals.length > 0 ? tokenVals.reduce((a, b) => a + b, 0) / tokenVals.length : 0
      return {
        component: comp,
        label: ctxLabel(comp),
        color: getCSSVar(CTX_COLOR_VARS[comp] || '--text-muted'),
        values,
        maxTokens,
        avgTokens,
      }
    })
    // Sort by component name for stable display order
    ctxSeries.value.sort((a, b) => a.component.localeCompare(b.component))
  } catch (e: any) {
    ctxError.value = e.message || 'Failed to load context window data'
    ctxSeries.value = []
  } finally {
    ctxLoading.value = false
  }
}
```

- [ ] **Step 6: Call fetchContextWindow after session data loads**

In the existing `fetchSession` function, add a call to `fetchContextWindow()` after `detail.value` is set:

```typescript
// Inside fetchSession(), after:
//   detail.value = await getSession(sessionId)
// Add:
    fetchContextWindow()
```

The revised `fetchSession`:

```typescript
async function fetchSession() {
  loading.value = true
  error.value = ''
  try {
    detail.value = await getSession(sessionId)
    fetchContextWindow()  // <-- new: fetch context window data in background
  } catch (e: any) {
    error.value = e.message || 'Failed to load session'
  } finally {
    loading.value = false
  }
}
```

- [ ] **Step 7: Commit**

```bash
git add web/src/views/SessionDetail.vue
git commit -m "feat: add context window data fetching to session detail"
```

---

### Task 3: Add Chart.js rendering for context window line chart

**Files:**
- Modify: `web/src/views/SessionDetail.vue`

- [ ] **Step 1: Add chart refs and render function**

Add after the ctxSeries declarations (in the `<script setup>` block):

```typescript
// Chart.js refs
const ctxCanvasRef = ref<HTMLCanvasElement | null>(null)
const ctxTooltipRef = ref<HTMLDivElement | null>(null)
let ctxChart: Chart | null = null
let tooltipHovering = false

const CTX_COLORS = [
  '#a78bfa', '#38bdf8', '#f472b6', '#22d3ee', '#fbbf24', '#4ade80',
]
```

- [ ] **Step 2: Add external tooltip handler (same pattern as PanelChart)**

```typescript
function ctxExternalTooltip(context: any) {
  const { chart, tooltip } = context
  const el = ctxTooltipRef.value
  if (!el) return

  if (tooltip.opacity === 0) {
    if (!tooltipHovering) el.style.opacity = '0'
    return
  }
  if (tooltipHovering) return

  const dataPoints = tooltip.dataPoints || []
  if (dataPoints.length === 0) {
    el.style.opacity = '0'
    return
  }

  let html = `<div class="tt-time">${dataPoints[0].label}</div><div class="tt-body">`
  for (const dp of dataPoints) {
    const ds = chart.data.datasets[dp.datasetIndex]
    const val = parseFloat(dp.formattedValue)
    html += `<div class="tt-item">`
    html += `<span class="tt-color" style="background:${ds.borderColor}"></span>`
    html += `<span class="tt-label">${ds.label}</span>`
    html += `<span class="tt-value">${val >= 1000 ? (val / 1000).toFixed(1) + 'K' : val.toFixed(0)}</span>`
    html += `</div>`
  }
  html += '</div>'

  el.innerHTML = html
  el.style.opacity = '1'

  const rect = chart.canvas.getBoundingClientRect()
  let left = rect.left + window.scrollX + tooltip.caretX - el.offsetWidth / 2
  let top = rect.top + window.scrollY + tooltip.caretY - el.offsetHeight - 12

  if (left < 0) left = 4
  const maxLeft = window.innerWidth - el.offsetWidth - 4
  if (left > maxLeft) left = maxLeft
  if (top < 0) top = rect.top + window.scrollY + tooltip.caretY + 16

  el.style.left = left + 'px'
  el.style.top = top + 'px'
}
```

- [ ] **Step 3: Add renderCtxChart function**

```typescript
function renderCtxChart() {
  if (!ctxCanvasRef.value || ctxSeries.value.length === 0) return

  if (ctxChart) {
    ctxChart.destroy()
    ctxChart = null
  }

  // Use first series' timestamps as shared labels
  const firstValues = ctxSeries.value[0]?.values || []
  const labels = firstValues.map(v => new Date(v[0]).toLocaleTimeString())

  const datasets = ctxSeries.value.map((s, idx) => ({
    label: s.label,
    data: s.values.map(v => v[1]),
    borderColor: s.color || CTX_COLORS[idx % CTX_COLORS.length],
    backgroundColor: s.color ? s.color + '22' : CTX_COLORS[idx % CTX_COLORS.length] + '22',
    borderWidth: 2,
    fill: false,
    tension: 0.3,
    pointRadius: 0,
  }))

  ctxChart = new Chart(ctxCanvasRef.value, {
    type: 'line',
    data: { labels, datasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      plugins: {
        legend: {
          display: ctxSeries.value.length > 1,
          position: 'bottom',
          labels: {
            color: getCSSVar('--text-secondary'),
            font: { size: 10 },
            boxWidth: 12,
            padding: 8,
          },
        },
        tooltip: {
          enabled: false,
          mode: 'index',
          intersect: false,
          position: 'nearest',
          external: ctxExternalTooltip,
        },
      },
      scales: {
        x: {
          ticks: { color: getCSSVar('--text-secondary'), maxTicksLimit: 8, font: { size: 10 } },
          grid: { color: getCSSVar('--border-subtle') },
        },
        y: {
          ticks: {
            color: getCSSVar('--text-secondary'),
            font: { size: 10 },
            callback: (val: any) => val >= 1000 ? (val / 1000).toFixed(0) + 'K' : val,
          },
          grid: { color: getCSSVar('--border-subtle') },
        },
      },
    },
  })
}
```

- [ ] **Step 4: Add tooltip listeners setup**

```typescript
function setupCtxTooltipListeners() {
  const el = ctxTooltipRef.value
  if (!el || !ctxCanvasRef.value) return

  el.addEventListener('mouseenter', () => { tooltipHovering = true })
  el.addEventListener('mouseleave', () => { tooltipHovering = false; el.style.opacity = '0' })
  ctxCanvasRef.value.addEventListener('mouseleave', () => {
    if (!tooltipHovering) el.style.opacity = '0'
  })
}
```

- [ ] **Step 5: Add watch to re-render chart when data or theme changes**

```typescript
watch(theme, () => {
  if (ctxChart) {
    ctxChart.destroy()
    ctxChart = null
  }
  // Re-read CSS variable colors on theme change
  ctxSeries.value = ctxSeries.value.map(s => ({
    ...s,
    color: getCSSVar(CTX_COLOR_VARS[s.component] || '--text-muted'),
  }))
  renderCtxChart()
})
```

- [ ] **Step 6: Add chart rendering to onMounted**

Modify the existing `onMounted` to also render the chart after data arrives. Since `fetchContextWindow` is async and will set `ctxSeries.value`, we watch for changes:

```typescript
// Watch for ctxSeries changes to render chart
watch(ctxSeries, () => {
  nextTick(() => {
    renderCtxChart()
    setupCtxTooltipListeners()
  })
})
```

Add `nextTick` and `watch` to the import from 'vue' (already done in Step 1 imports, but ensure `nextTick` is there).

- [ ] **Step 7: Add cleanup in onUnmounted**

```typescript
onUnmounted(() => {
  if (ctxChart) {
    ctxChart.destroy()
    ctxChart = null
  }
})
```

- [ ] **Step 8: Commit**

```bash
git add web/src/views/SessionDetail.vue
git commit -m "feat: add context window line chart rendering to session detail"
```

---

### Task 4: Add template section (stat cards + chart + states)

**Files:**
- Modify: `web/src/views/SessionDetail.vue`

- [ ] **Step 1: Add context window section template**

Insert between the summary grid (`</div>` closing `session-summary`) and the turns heading (`<h3 class="turns-heading">`), i.e., after line 41 and before line 43:

```html
      <!-- Context Window Chart -->
      <div class="ctx-window-section">
        <h3 class="ctx-heading">Context Window</h3>

        <!-- Stat cards -->
        <div v-if="ctxSeries.length > 0 && !ctxLoading" class="ctx-stats">
          <div v-for="s in ctxSeries" :key="s.component" class="ctx-stat-card">
            <span class="ctx-stat-label" :style="{ color: s.color }">{{ s.label }}</span>
            <span class="ctx-stat-values">
              max <strong>{{ formatTokens(s.maxTokens) }}</strong>
              &nbsp;avg <strong>{{ formatTokens(s.avgTokens) }}</strong>
            </span>
          </div>
        </div>

        <!-- Loading / Error / Empty states -->
        <div v-if="ctxLoading" class="ctx-state">Loading...</div>
        <div v-else-if="ctxError" class="ctx-state ctx-error">{{ ctxError }}</div>
        <div v-else-if="ctxSeries.length === 0" class="ctx-state">No context window data for this session</div>

        <!-- Chart canvas -->
        <div v-show="ctxSeries.length > 0 && !ctxLoading && !ctxError" class="ctx-chart-body">
          <canvas ref="ctxCanvasRef"></canvas>
          <div ref="ctxTooltipRef" class="ctx-tooltip chart-tooltip"></div>
        </div>
      </div>
```

Note: the `chart-tooltip` class is already defined in `PanelChart.vue` (scoped). Since we're inlining, we need our own tooltip styles. We'll add them in the style section.

- [ ] **Step 2: Commit**

```bash
git add web/src/views/SessionDetail.vue
git commit -m "feat: add context window chart template and stat cards to session detail"
```

---

### Task 5: Add styles for context window section

**Files:**
- Modify: `web/src/views/SessionDetail.vue`

- [ ] **Step 1: Add scoped styles**

Add to the existing `<style scoped>` block, before the closing `</style>` tag:

```css
/* Context window chart section */
.ctx-window-section {
  margin-bottom: 24px;
}
.ctx-heading {
  font-size: 16px;
  margin-bottom: 12px;
  color: var(--text-primary);
}
.ctx-stats {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.ctx-stat-card {
  display: flex;
  flex-direction: column;
  padding: 8px 12px;
  background: var(--bg-surface);
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  min-width: 140px;
}
.ctx-stat-label {
  font-size: 11px;
  font-weight: 600;
  margin-bottom: 4px;
}
.ctx-stat-values {
  font-size: 12px;
  color: var(--text-secondary);
}
.ctx-stat-values strong {
  color: var(--text-primary);
  font-weight: 600;
}
.ctx-state {
  text-align: center;
  padding: 40px 0;
  color: var(--text-secondary);
  font-size: 14px;
}
.ctx-error {
  color: var(--status-error-accent);
}
.ctx-chart-body {
  height: 300px;
  position: relative;
}
.ctx-chart-body canvas {
  width: 100% !important;
  height: 100% !important;
}

/* Tooltip (mirrors PanelChart's .chart-tooltip to avoid scoped-style shadowing) */
.ctx-tooltip {
  position: fixed;
  pointer-events: auto;
  opacity: 0;
  z-index: 9999;
  background: var(--bg-secondary);
  border: 1px solid var(--border-group);
  border-radius: 6px;
  padding: 8px 10px;
  min-width: 160px;
  max-width: 280px;
  max-height: 220px;
  overflow-y: auto;
  font-size: 12px;
  box-shadow: 0 4px 16px var(--shadow-tooltip);
  transition: opacity 0.15s;
}
.ctx-tooltip::-webkit-scrollbar { width: 4px; }
.ctx-tooltip::-webkit-scrollbar-track { background: transparent; }
.ctx-tooltip::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 2px; }
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/SessionDetail.vue
git commit -m "style: add context window chart and stat card styles"
```

---

### Task 6: Run app and verify

**Files:**
- None (verification only)

- [ ] **Step 1: Build and run**

```bash
make run
```

Expected: App starts without errors.

- [ ] **Step 2: Navigate to a session that has context window metrics data**

Open `http://localhost:8080/sessions/<sessionId>` where the session has `gen_ai_context_tokens` metrics with matching `jiuwenclaw.session.id`.

- [ ] **Step 3: Verify chart renders correctly**

Check:
- [ ] Stat cards show component labels with max/avg values
- [ ] Line chart renders with one line per component, matching colors from `TokenPieChart`
- [ ] Hover tooltip shows time + per-component token values
- [ ] Legend at bottom shows component names
- [ ] Y-axis uses K suffix for large values
- [ ] X-axis shows time labels

- [ ] **Step 4: Verify edge cases**

- [ ] Session with no metrics: shows "No context window data" placeholder
- [ ] Session with some components missing: only present components appear in chart and stats
- [ ] Theme toggle (if available): chart colors update

- [ ] **Step 5: Commit if any fixes made**

```bash
git add web/src/views/SessionDetail.vue
git commit -m "fix: context window chart edge cases"
```
