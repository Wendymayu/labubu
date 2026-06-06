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
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getSession, type SessionDetail } from '../api/client'

const route = useRoute()
const router = useRouter()
const sessionId = route.params.sessionId as string

const detail = ref<SessionDetail | null>(null)
const loading = ref(true)
const error = ref('')

async function fetchSession() {
  loading.value = true
  error.value = ''
  try {
    detail.value = await getSession(sessionId)
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

/* Context window chart section */
.ctx-window-section {
  margin-bottom: 24px;
}
.ctx-heading {
  font-size: 16px;
  margin-bottom: 12px;
  color: #e2e8f0;
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
  background: #1e293b;
  border: 1px solid #1e293b;
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
  color: #94a3b8;
}
.ctx-stat-values strong {
  color: #e2e8f0;
  font-weight: 600;
}
.ctx-state {
  text-align: center;
  padding: 40px 0;
  color: #94a3b8;
  font-size: 14px;
}
.ctx-error {
  color: #f87171;
}
.ctx-chart-body {
  height: 300px;
  position: relative;
}
.ctx-chart-body canvas {
  width: 100% !important;
  height: 100% !important;
}

/* Tooltip (mirrors PanelChart's .chart-tooltip for use outside scoped styles) */
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
</style>