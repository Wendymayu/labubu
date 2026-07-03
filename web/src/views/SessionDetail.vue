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
            <span class="summary-label">Cost</span>
            <span class="summary-value token-highlight">{{ formatCost(detail.session.cost, detail.session.cost_currency) }}</span>
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

      <!-- Agent Stats Section -->
      <AgentStatsSection
        :stats="agentStats"
        :loading="agentStatsLoading"
        :error="agentStatsError"
      />

      <!-- Context Window Chart -->
      <div class="ctx-window-section">
        <h3 class="ctx-heading">{{ t('sessionDetail.contextWindow') }}</h3>

        <!-- Stat cards -->
        <div v-if="ctxSeries.length > 0 && !ctxLoading" class="ctx-stats">
          <div v-for="s in ctxSeries" :key="s.component" class="ctx-stat-card">
            <span class="ctx-stat-label" :style="{ color: s.color }">{{ s.label }}</span>
            <span class="ctx-stat-values">
              {{ t('sessionDetail.max') }} <strong>{{ formatTokens(s.maxTokens) }}</strong>
              &nbsp;{{ t('sessionDetail.avg') }} <strong>{{ formatTokens(s.avgTokens) }}</strong>
            </span>
          </div>
        </div>

        <!-- Loading / Error / Empty states -->
        <div v-if="ctxLoading" class="ctx-state">{{ t('common.loading') }}</div>
        <div v-else-if="ctxError" class="ctx-state ctx-error">{{ ctxError }}</div>
        <div v-else-if="ctxSeries.length === 0" class="ctx-state">{{ t('sessionDetail.noContextData') }}</div>

        <!-- Chart canvas -->
        <div v-show="ctxSeries.length > 0 && !ctxLoading && !ctxError" class="ctx-chart-body">
          <canvas ref="ctxCanvasRef"></canvas>
          <div ref="ctxTooltipRef" class="ctx-tooltip"></div>
        </div>
      </div>

      <h3 class="turns-heading">Turns ({{ detail.pagination.total }})</h3>

      <div v-if="turnsLoading" class="turns-loading">{{ t('common.loading') }}</div>
      <div class="turns-list" v-else>
        <div
          v-for="(trace, idx) in detail.traces"
          :key="trace.trace_id_hex"
          class="turn-row"
          @click="goToTrace(trace.trace_id_hex)"
        >
          <span class="turn-number">#{{ (page - 1) * pageSize + idx + 1 }}</span>
          <span class="turn-name">{{ trace.root_name }}</span>
          <span :class="['status-badge', statusClass(trace.status)]">{{ trace.status }}</span>
          <span class="turn-duration">{{ formatDuration(trace.duration_ms) }}</span>
          <span class="turn-tokens">{{ formatTokens(trace.total_tokens) }}</span>
          <span class="turn-service">{{ trace.root_service }}</span>
          <span class="turn-time">{{ formatTime(trace.start_time_ms) }}</span>
        </div>
      </div>

      <div class="pagination" v-if="detail.pagination.total > 0">
        <button
          :disabled="page <= 1 || turnsLoading"
          @click="goToTurnsPage(page - 1)"
          class="btn"
        >
          {{ t('common.prev') }}
        </button>
        <span class="page-info">
          {{ t('common.pageOf', { page: page, total: totalPages, count: detail.pagination.total }) }}
        </span>
        <button
          :disabled="page >= totalPages || turnsLoading"
          @click="goToTurnsPage(page + 1)"
          class="btn"
        >
          {{ t('common.next') }}
        </button>
        <span class="page-size">
          <label>{{ t('common.perPage') }}</label>
          <select
            :value="pageSize"
            @change="changeTurnsPageSize(Number(($event.target as HTMLSelectElement).value))"
          >
            <option v-for="n in pageSizeOptions" :key="n" :value="n">{{ n }}</option>
          </select>
        </span>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useTheme } from '../composables/useTheme'
import { usePageSize } from '../composables/usePageSize'
import { getSession, getAgentStats, type SessionDetail, type AgentStats, type QueryResult } from '../api/client'
import AgentStatsSection from '../components/AgentStatsSection.vue'
import { formatCost } from '../utils/format'
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
const turnsLoading = ref(false)
const error = ref('')

// Turns (session traces) pagination.
const { options: pageSizeOptions, loadPageSize, savePageSize } = usePageSize('sessionDetail')
const page = ref(1)
const pageSize = ref(loadPageSize())

const totalPages = computed(() => Math.max(1, Math.ceil((detail.value?.pagination.total ?? 0) / pageSize.value)))

// Agent stats state
const agentStats = ref<AgentStats | null>(null)
const agentStatsLoading = ref(false)
const agentStatsError = ref('')

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
  const firstLoad = detail.value == null
  if (firstLoad) {
    loading.value = true
  } else {
    turnsLoading.value = true
  }
  error.value = ''
  try {
    detail.value = await getSession(sessionId, page.value, pageSize.value)
    if (firstLoad) {
      fetchContextWindow()  // fetch context window data in background
    }
  } catch (e: any) {
    error.value = e.message || 'Failed to load session'
  } finally {
    loading.value = false
    turnsLoading.value = false
  }
}

function goToTurnsPage(p: number) {
  page.value = p
  fetchSession()
}

function changeTurnsPageSize(n: number) {
  pageSize.value = n
  savePageSize(n)
  page.value = 1
  fetchSession()
}

async function fetchAgentStats() {
  if (!route.params.sessionId) return
  agentStatsLoading.value = true
  agentStatsError.value = ''
  try {
    agentStats.value = await getAgentStats(route.params.sessionId as string)
  } catch (e: any) {
    if (e.message === 'no_agent_data') {
      agentStats.value = null
    } else {
      agentStatsError.value = e.message || 'Failed to load agent stats'
    }
  } finally {
    agentStatsLoading.value = false
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
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return String(tokens)
}

function formatTime(ms: number): string {
  return new Date(ms).toLocaleString()
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
  fetchAgentStats()
  setupCtxTooltipListeners()
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
.back-link a { color: var(--text-secondary); text-decoration: none; font-size: 14px; }
.back-link a:hover { color: var(--text-primary); }
.loading, .error { text-align: center; padding: 60px; color: var(--text-secondary); }
.error { color: var(--status-error-accent); }
.session-summary { margin-bottom: 24px; }
.session-summary h2 { font-size: 20px; margin-bottom: 12px; word-break: break-all; }
.summary-grid { display: flex; gap: 24px; flex-wrap: wrap; }
.summary-item { display: flex; flex-direction: column; }
.summary-label { font-size: 11px; color: var(--text-secondary); text-transform: uppercase; }
.summary-value { font-size: 14px; }
.token-highlight { color: var(--token-highlight); font-weight: 600; }
.error-high { color: var(--status-error-text); }
.error-medium { color: var(--status-warning); }
.error-ok { color: var(--status-ok-text); }

.turns-heading { font-size: 16px; margin-bottom: 12px; color: var(--text-primary); }

.turns-list { display: flex; flex-direction: column; gap: 2px; }
.turn-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-subtle);
  cursor: pointer;
  font-size: 14px;
}
.turn-row:hover { background: var(--bg-surface); }
.turn-number { color: var(--text-secondary); font-size: 12px; font-weight: 600; min-width: 32px; }
.turn-name { flex: 1; font-weight: 600; color: var(--accent-blue); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.status-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.status-ok { background: var(--status-ok-bg); color: var(--status-ok-text); }
.status-error { background: var(--status-error-bg); color: var(--status-error-text); }
.turn-duration { color: var(--text-secondary); min-width: 70px; text-align: right; }
.turn-tokens { color: var(--token-highlight); font-weight: 600; min-width: 60px; text-align: right; }
.turn-service { color: var(--text-secondary); font-size: 13px; min-width: 100px; }
.turn-time { color: var(--text-secondary); font-size: 13px; min-width: 80px; text-align: right; }

.turns-loading { text-align: center; padding: 24px; color: var(--text-secondary); font-size: 14px; }

.pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 20px; }
.page-info { font-size: 14px; color: var(--text-secondary); }
.page-size { display: flex; align-items: center; gap: 6px; font-size: 14px; color: var(--text-secondary); }
.page-size select { padding: 2px 6px; background: var(--bg-primary); color: var(--text-primary); border: 1px solid var(--border-default); border-radius: 4px; font-size: 13px; }
.btn { padding: 8px 16px; background: var(--bg-surface-hover); border: 1px solid var(--border-strong); border-radius: 6px; color: var(--text-primary); cursor: pointer; font-size: 14px; }
.btn:hover { background: var(--border-strong); }
.btn:disabled { opacity: 0.5; cursor: default; }

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

.ctx-tooltip .tt-time {
  color: var(--text-secondary);
  font-size: 11px;
  margin-bottom: 6px;
  padding-bottom: 4px;
  border-bottom: 1px solid var(--border-group);
}
.ctx-tooltip .tt-body {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ctx-tooltip .tt-item {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}
.ctx-tooltip .tt-color {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-top: 3px;
  flex-shrink: 0;
}
.ctx-tooltip .tt-label {
  color: var(--text-primary);
  line-height: 1.5;
  word-break: break-all;
}
.ctx-tooltip .tt-value {
  color: var(--accent-blue);
  font-weight: 600;
  flex-shrink: 0;
  margin-left: auto;
}
</style>