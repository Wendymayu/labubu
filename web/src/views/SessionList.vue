<template>
  <div class="session-list">
    <div class="filters">
      <input
        v-model="filters.q"
        type="text"
        placeholder="Search sessions..."
        class="search-input"
        @keyup.enter="search"
      />
      <select v-model="filters.service" class="filter-select">
        <option value="">All services</option>
        <option v-for="svc in services" :key="svc" :value="svc">{{ svc }}</option>
      </select>
      <button @click="search" class="btn btn-primary">Search</button>
      <button @click="reset" class="btn">Reset</button>
    </div>

    <div v-if="loading" class="loading">Loading...</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <template v-else>
      <table class="trace-table" v-if="sessions.length > 0">
        <thead>
          <tr>
            <th>Session ID</th>
            <th>Turns</th>
            <th>Total Tokens</th>
            <th>Avg Latency</th>
            <th>Max Latency</th>
            <th>Error Rate</th>
            <th>Last Active</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="session in sessions"
            :key="session.session_id"
            @click="goToSession(session.session_id)"
            class="trace-row"
          >
            <td class="cell-session-id">{{ session.session_id }}</td>
            <td>{{ session.trace_count }}</td>
            <td class="cell-tokens">{{ formatTokens(session.total_tokens) }}</td>
            <td>{{ formatDuration(session.avg_duration_ms) }}</td>
            <td>{{ formatDuration(session.max_duration_ms) }}</td>
            <td>
              <span :class="['error-rate', errorRateClass(session.error_rate)]">
                {{ (session.error_rate * 100).toFixed(0) }}%
              </span>
            </td>
            <td class="cell-time">{{ formatRelativeTime(session.last_active_ms) }}</td>
          </tr>
        </tbody>
      </table>

      <div v-else class="empty">No sessions found.</div>

      <div class="pagination" v-if="pagination.total > 0">
        <button
          :disabled="pagination.page <= 1"
          @click="goToPage(pagination.page - 1)"
          class="btn"
        >
          ← Prev
        </button>
        <span class="page-info">
          Page {{ pagination.page }} of {{ totalPages }} ({{ pagination.total }} sessions)
        </span>
        <button
          :disabled="pagination.page >= totalPages"
          @click="goToPage(pagination.page + 1)"
          class="btn"
        >
          Next →
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { listSessions, getServices, type SessionListItem, type Pagination } from '../api/client'

const router = useRouter()

const sessions = ref<SessionListItem[]>([])
const pagination = ref<Pagination>({ page: 1, page_size: 20, total: 0 })
const services = ref<string[]>([])
const loading = ref(true)
const error = ref('')

const filters = ref({
  q: '',
  service: '',
})

const totalPages = computed(() => {
  return Math.max(1, Math.ceil(pagination.value.total / pagination.value.page_size))
})

async function fetchSessions(page = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await listSessions({ ...filters.value, page, page_size: 20 })
    sessions.value = result.sessions
    pagination.value = result.pagination
  } catch (e: any) {
    error.value = e.message || 'Failed to load sessions'
  } finally {
    loading.value = false
  }
}

async function fetchServices() {
  try {
    services.value = await getServices()
  } catch {
    // Non-critical.
  }
}

function search() {
  fetchSessions(1)
}

function reset() {
  filters.value = { q: '', service: '' }
  fetchSessions(1)
}

function goToPage(page: number) {
  fetchSessions(page)
}

function goToSession(sessionId: string) {
  router.push({ name: 'session-detail', params: { sessionId } })
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

function formatRelativeTime(ms: number): string {
  const now = Date.now()
  const diff = now - ms
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)} min ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return new Date(ms).toLocaleDateString()
}

function errorRateClass(rate: number): string {
  if (rate > 0.5) return 'error-high'
  if (rate > 0) return 'error-medium'
  return 'error-none'
}

onMounted(() => {
  fetchSessions()
  fetchServices()
})
</script>

<style scoped>
.session-list { max-width: 1400px; }
.filters { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; }
.search-input { flex: 1; min-width: 200px; padding: 8px 12px; background: #1e293b; border: 1px solid #334155; border-radius: 6px; color: #e2e8f0; font-size: 14px; }
.filter-select { padding: 8px 12px; background: #1e293b; border: 1px solid #334155; border-radius: 6px; color: #e2e8f0; font-size: 14px; }
.btn { padding: 8px 16px; background: #334155; border: 1px solid #475569; border-radius: 6px; color: #e2e8f0; cursor: pointer; font-size: 14px; }
.btn:hover { background: #475569; }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-primary { background: #2563eb; border-color: #2563eb; }
.btn-primary:hover { background: #1d4ed8; }
.loading, .error, .empty { text-align: center; padding: 60px 20px; color: #94a3b8; }
.error { color: #f87171; }
.trace-table { width: 100%; border-collapse: collapse; }
.trace-table th { text-align: left; padding: 10px 12px; font-size: 12px; color: #94a3b8; text-transform: uppercase; border-bottom: 1px solid #334155; }
.trace-table td { padding: 10px 12px; font-size: 14px; border-bottom: 1px solid #1e293b; }
.trace-row { cursor: pointer; }
.trace-row:hover { background: #1e293b; }
.cell-session-id { font-family: 'Courier New', monospace; font-size: 13px; color: #38bdf8; max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cell-tokens { color: #c4b5fd; font-weight: 600; }
.cell-time { color: #94a3b8; font-size: 13px; white-space: nowrap; }
.error-rate { font-weight: 600; font-size: 13px; }
.error-high { color: #fca5a5; }
.error-medium { color: #fbbf24; }
.error-none { color: #6ee7b7; }
.pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 20px; }
.page-info { font-size: 14px; color: #94a3b8; }
</style>
