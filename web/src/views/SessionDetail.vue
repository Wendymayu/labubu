<template>
  <div class="session-detail">
    <div class="back-link">
      <router-link to="/sessions">← Back to sessions</router-link>
    </div>

    <div v-if="loading" class="loading">Loading...</div>
    <div v-else-if="error" class="error">{{ error }}</div>

    <template v-else-if="detail">
      <div class="session-summary">
        <h2>Session: {{ detail.session.session_id }}</h2>
        <div class="summary-grid">
          <div class="summary-item">
            <span class="summary-label">Turns</span>
            <span class="summary-value">{{ detail.session.trace_count }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Total Tokens</span>
            <span class="summary-value token-highlight">{{ formatTokens(detail.session.total_tokens) }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Error Rate</span>
            <span :class="['summary-value', errorRateClass(detail.session.error_rate)]">
              {{ (detail.session.error_rate * 100).toFixed(0) }}%
            </span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Avg Latency</span>
            <span class="summary-value">{{ formatDuration(detail.session.avg_duration_ms) }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Max Latency</span>
            <span class="summary-value">{{ formatDuration(detail.session.max_duration_ms) }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Duration</span>
            <span class="summary-value">{{ formatDuration(detail.session.last_active_ms - detail.session.first_active_ms) }}</span>
          </div>
        </div>
      </div>

      <h3 class="turns-heading">Turns ({{ detail.traces.length }})</h3>

      <div class="turns-list">
        <div
          v-for="(trace, idx) in detail.traces"
          :key="trace.trace_id_hex"
          class="turn-row"
          @click="goToTrace(trace.trace_id_hex)"
        >
          <span class="turn-number">#{{ idx + 1 }}</span>
          <span class="turn-name">{{ trace.root_name }}</span>
          <span :class="['status-badge', statusClass(trace.status)]">{{ trace.status }}</span>
          <span class="turn-duration">{{ formatDuration(trace.duration_ms) }}</span>
          <span class="turn-tokens">{{ formatTokens(trace.total_tokens) }}</span>
          <span class="turn-service">{{ trace.root_service }}</span>
          <span class="turn-time">{{ formatTime(trace.start_time_ms) }}</span>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useTheme } from '../composables/useTheme'
import { getSession, type SessionDetail, type QueryResult } from '../api/client'
import {
  Chart, LineController, CategoryScale, LinearScale,
  PointElement, LineElement, Tooltip, Legend, Filler
} from 'chart.js'

Chart.register(
  LineController, CategoryScale, LinearScale,
  PointElement, LineElement, Tooltip, Legend, Filler
)

const CTX_COLOR_VARS: Record<string, string> = {
  system: '--chart-pie-system',
  user: '--chart-pie-user',
  assistant: '--chart-pie-assistant',
  tool: '--chart-pie-tool',
  tool_definitions: '--chart-pie-tool-defs',
  skill: '--chart-pie-skill',
}

const CTX_COMPONENTS = ['system', 'user', 'assistant', 'tool', 'tool_definitions', 'skill'] as const

interface CtxSeries {
  component: string
  label: string
  color: string
  values: [number, number][]  // [time_ms, token_count]
  maxTokens: number
  avgTokens: number
}

const route = useRoute()
const router = useRouter()
const sessionId = route.params.sessionId as string
const { t } = useI18n()
const { theme } = useTheme()

const detail = ref<SessionDetail | null>(null)
const loading = ref(true)
const error = ref('')

// Context window chart state
const ctxLoading = ref(false)
const ctxError = ref('')

const ctxSeries = ref<CtxSeries[]>([])

// Chart.js refs
const ctxCanvasRef = ref<HTMLCanvasElement | null>(null)
const ctxTooltipRef = ref<HTMLDivElement | null>(null)
let ctxChart: Chart | null = null
let tooltipHovering = false

const CTX_COLORS = [
  '#a78bfa', '#38bdf8', '#f472b6', '#22d3ee', '#fbbf24', '#4ade80',
]

async function fetchSession() {
  loading.value = true
  error.value = ''
  try {
    detail.value = await getSession(sessionId)
    fetchContextWindow()  // fetch context window data in background
  } catch (e: any) {
    error.value = e.message || 'Failed to load session'
  } finally {
    loading.value = false
  }
}

function goToTrace(traceIdHex: string) {
  router.push({ name: 'trace-detail', params: { id: traceIdHex } })
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
  const mins = Math.floor(ms / 60000)
  const secs = ((ms % 60000) / 1000).toFixed(0)
  return `${mins}m ${secs}s`
}

function formatTokens(tokens?: number): string {
  if (tokens == null) return '-'
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return String(tokens)
}

function formatTime(ms: number): string {
  return new Date(ms).toLocaleTimeString()
}

function getCSSVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function ctxLabel(component: string): string {
  return t(`sessionDetail.component.${component}`)
}

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

function setupCtxTooltipListeners() {
  const el = ctxTooltipRef.value
  if (!el || !ctxCanvasRef.value) return

  el.addEventListener('mouseenter', () => { tooltipHovering = true })
  el.addEventListener('mouseleave', () => { tooltipHovering = false; el.style.opacity = '0' })
  ctxCanvasRef.value.addEventListener('mouseleave', () => {
    if (!tooltipHovering) el.style.opacity = '0'
  })
}

// Watch for ctxSeries changes to render chart
watch(ctxSeries, () => {
  nextTick(() => {
    renderCtxChart()
    setupCtxTooltipListeners()
  })
})

// Re-render on theme change to pick up new CSS variable colors
watch(theme, () => {
  if (ctxChart) {
    ctxChart.destroy()
    ctxChart = null
  }
  ctxSeries.value = ctxSeries.value.map(s => ({
    ...s,
    color: getCSSVar(CTX_COLOR_VARS[s.component] || '--text-muted'),
  }))
  nextTick(() => {
    renderCtxChart()
  })
})

function statusClass(status: string): string {
  switch (status) {
    case 'OK': return 'status-ok'
    case 'ERROR': return 'status-error'
    default: return ''
  }
}

function errorRateClass(rate: number): string {
  if (rate > 0.5) return 'error-high'
  if (rate > 0) return 'error-medium'
  return 'error-ok'
}

onMounted(() => {
  fetchSession()
})

onUnmounted(() => {
  if (ctxChart) {
    ctxChart.destroy()
    ctxChart = null
  }
})
</script>

<style scoped>
.session-detail { max-width: 1200px; }
.back-link { margin-bottom: 16px; }
.back-link a { color: #94a3b8; text-decoration: none; font-size: 14px; }
.back-link a:hover { color: #e2e8f0; }
.loading, .error { text-align: center; padding: 60px; color: #94a3b8; }
.error { color: #f87171; }
.session-summary { margin-bottom: 24px; }
.session-summary h2 { font-size: 20px; margin-bottom: 12px; word-break: break-all; }
.summary-grid { display: flex; gap: 24px; flex-wrap: wrap; }
.summary-item { display: flex; flex-direction: column; }
.summary-label { font-size: 11px; color: #94a3b8; text-transform: uppercase; }
.summary-value { font-size: 14px; }
.token-highlight { color: #c4b5fd; font-weight: 600; }
.error-high { color: #fca5a5; }
.error-medium { color: #fbbf24; }
.error-ok { color: #6ee7b7; }

.turns-heading { font-size: 16px; margin-bottom: 12px; color: #e2e8f0; }

.turns-list { display: flex; flex-direction: column; gap: 2px; }
.turn-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 12px;
  border-bottom: 1px solid #1e293b;
  cursor: pointer;
  font-size: 14px;
}
.turn-row:hover { background: #1e293b; }
.turn-number { color: #64748b; font-size: 12px; font-weight: 600; min-width: 32px; }
.turn-name { flex: 1; font-weight: 600; color: #38bdf8; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.status-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.status-ok { background: #065f46; color: #6ee7b7; }
.status-error { background: #7f1d1d; color: #fca5a5; }
.turn-duration { color: #94a3b8; min-width: 70px; text-align: right; }
.turn-tokens { color: #c4b5fd; font-weight: 600; min-width: 60px; text-align: right; }
.turn-service { color: #94a3b8; font-size: 13px; min-width: 100px; }
.turn-time { color: #64748b; font-size: 13px; min-width: 80px; text-align: right; }
</style>