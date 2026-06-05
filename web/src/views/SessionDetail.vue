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