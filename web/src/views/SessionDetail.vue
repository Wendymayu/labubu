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
  values: [number, number][]  // [time_ms, token_count]
  maxTokens: number
  avgTokens: number
}

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { theme } = useTheme()
const sessionId = route.params.sessionId as string

const detail = ref<SessionDetail | null>(null)
const loading = ref(true)
const error = ref('')

// Context window chart state
const ctxLoading = ref(false)
const ctxError = ref('')

const ctxSeries = ref<CtxSeries[]>([])

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
.turn-number { color: var(--text-muted); font-size: 12px; font-weight: 600; min-width: 32px; }
.turn-name { flex: 1; font-weight: 600; color: var(--accent-blue); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.status-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.status-ok { background: var(--status-ok-bg); color: var(--status-ok-text); }
.status-error { background: var(--status-error-bg); color: var(--status-error-text); }
.turn-duration { color: var(--text-secondary); min-width: 70px; text-align: right; }
.turn-tokens { color: var(--token-highlight); font-weight: 600; min-width: 60px; text-align: right; }
.turn-service { color: var(--text-secondary); font-size: 13px; min-width: 100px; }
.turn-time { color: var(--text-muted); font-size: 13px; min-width: 80px; text-align: right; }
</style>