<template>
  <div class="session-list">
    <div class="filters">
      <TimeRangePicker ref="picker" :key="resetKey" @change="onTimeChange" />
      <select v-model="filters.service" class="filter-select" @change="onServiceChange">
        <option value="">{{ t('common.allServices') }}</option>
        <option v-for="svc in services" :key="svc" :value="svc">{{ svc }}</option>
      </select>
      <button @click="reset" class="btn">{{ t('common.reset') }}</button>
    </div>

    <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <template v-else>
      <table class="trace-table" v-if="sessions.length > 0">
        <thead>
          <tr>
            <th class="has-filter">
              <div class="th-head">
                <span>{{ t('sessionList.sessionId') }}</span>
                <button class="filter-btn" :class="{ active: !!filters.q }" :title="t('sessionList.filter')" @click.stop="toggleFilter()">▼</button>
              </div>
              <div v-if="filterOpen" class="filter-popover" @click.stop>
                <input class="header-filter" v-model="filters.q" :placeholder="t('sessionList.filterSessionId')" @keyup.enter="applySessionFilter" />
              </div>
            </th>
            <th>{{ t('sessionList.turns') }}</th>
            <th>{{ t('sessionList.totalTokens') }}</th>
            <th>Cost</th>
            <th>{{ t('sessionList.avgLatency') }}</th>
            <th>{{ t('sessionList.maxLatency') }}</th>
            <th>{{ t('sessionList.errorRate') }}</th>
            <th>{{ t('sessionList.lastActive') }}</th>
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
            <td class="cell-cost">{{ formatCost(session.cost, session.cost_currency) }}</td>
            <td>{{ formatDuration(session.avg_duration_ms) }}</td>
            <td>{{ formatDuration(session.max_duration_ms) }}</td>
            <td>
              <span :class="['error-rate', errorRateClass(session.error_rate)]">
                {{ (session.error_rate * 100).toFixed(0) }}%
              </span>
            </td>
            <td class="cell-time">{{ formatTime(session.last_active_ms) }}</td>
          </tr>
        </tbody>
      </table>

      <div v-else class="empty">
        {{ t('sessionList.noSessions') }}
        <div v-if="timeRange.period !== 'all'" class="empty-hint">{{ t('timeRange.emptyHint') }}</div>
      </div>

      <div class="pagination" v-if="pagination.total > 0">
        <button
          :disabled="pagination.page <= 1"
          @click="goToPage(pagination.page - 1)"
          class="btn"
        >
          {{ t('common.prev') }}
        </button>
        <span class="page-info">
          {{ t('common.pageOf', { page: pagination.page, total: totalPages, count: pagination.total }) }}
        </span>
        <button
          :disabled="pagination.page >= totalPages"
          @click="goToPage(pagination.page + 1)"
          class="btn"
        >
          {{ t('common.next') }}
        </button>
        <span class="page-size">
          <label>{{ t('common.perPage') }}</label>
          <select
            :value="pagination.page_size"
            @change="changePageSize(Number(($event.target as HTMLSelectElement).value))"
          >
            <option v-for="n in pageSizeOptions" :key="n" :value="n">{{ n }}</option>
          </select>
        </span>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { listSessions, getServices, type SessionListItem, type Pagination, type TimeRangeSelection } from '../api/client'
import { usePageSize } from '../composables/usePageSize'
import TimeRangePicker from '../components/TimeRangePicker.vue'
import { formatCost } from '../utils/format'

const router = useRouter()
const { t } = useI18n()

const sessions = ref<SessionListItem[]>([])
const { options: pageSizeOptions, loadPageSize, savePageSize } = usePageSize('sessions')
const pagination = ref<Pagination>({ page: 1, page_size: loadPageSize(), total: 0 })
const services = ref<string[]>([])
const loading = ref(true)
const error = ref('')

const filters = ref({
  q: '',
  service: '',
})

const filterOpen = ref(false)

function toggleFilter() {
  filterOpen.value = !filterOpen.value
}

function closeFilter() {
  filterOpen.value = false
}

function applySessionFilter() {
  search()
  filterOpen.value = false
}

function onServiceChange() {
  search()
}

const timeRange = ref<TimeRangeSelection>({ period: 'today' })
const picker = ref<InstanceType<typeof TimeRangePicker> | null>(null)
const resetKey = ref(0)

function onTimeChange(sel: TimeRangeSelection) {
  timeRange.value = sel
  fetchSessions(1)
}

const totalPages = computed(() => {
  return Math.max(1, Math.ceil(pagination.value.total / pagination.value.page_size))
})

async function fetchSessions(page = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await listSessions({
      ...filters.value,
      page,
      page_size: pagination.value.page_size,
      start: timeRange.value.start,
      end: timeRange.value.end,
    })
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
  // Recompute relative presets (today/7d/30d) against now so the window
  // moves forward instead of being frozen at selection time.
  const fresh = picker.value?.refresh()
  if (fresh) timeRange.value = fresh
  fetchSessions(1)
}

function reset() {
  filters.value = { q: '', service: '' }
  filterOpen.value = false
  resetKey.value++ // remount picker → re-emits default 'today' → fetchSessions(1)
}

function goToPage(page: number) {
  fetchSessions(page)
}

function changePageSize(n: number) {
  pagination.value.page_size = n
  savePageSize(n)
  fetchSessions(1)
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
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return String(tokens)
}

function formatTime(ms: number): string {
  const d = new Date(ms)
  return d.toLocaleString()
}

function errorRateClass(rate: number): string {
  if (rate > 0.5) return 'error-high'
  if (rate > 0) return 'error-medium'
  return 'error-none'
}

onMounted(() => {
  document.addEventListener('click', closeFilter)
  // fetchSessions is triggered by the picker's mount emit (default 'today').
  fetchServices()
})

onUnmounted(() => {
  document.removeEventListener('click', closeFilter)
})
</script>

<style scoped>
.session-list { max-width: 1400px; }
.filters { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; align-items: center; }
.filters :deep(.period-bar) { margin-bottom: 0; }
.filter-select { padding: 8px 12px; background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 6px; color: var(--text-primary); font-size: 14px; }
.has-filter { position: relative; }
.th-head { display: flex; align-items: center; gap: 4px; }
.filter-btn { background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 10px; padding: 0; line-height: 1; }
.filter-btn:hover { color: var(--text-primary); }
.filter-btn.active { color: var(--accent-blue); }
.filter-popover { position: absolute; top: 100%; left: 0; z-index: 30; min-width: 220px; margin-top: 4px; padding: 6px; background: var(--bg-primary); border: 1px solid var(--border-default); border-radius: 6px; box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25); text-transform: none; }
.header-filter { width: 100%; box-sizing: border-box; padding: 4px 6px; background: var(--bg-primary); border: 1px solid var(--border-default); border-radius: 4px; color: var(--text-primary); font-size: 12px; }
.header-filter:focus { border-color: var(--accent-blue); outline: none; }
.btn { padding: 8px 16px; background: var(--bg-surface-hover); border: 1px solid var(--border-strong); border-radius: 6px; color: var(--text-primary); cursor: pointer; font-size: 14px; }
.btn:hover { background: var(--border-strong); }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-primary { background: var(--accent-primary); border-color: var(--accent-primary); }
.btn-primary:hover { background: var(--accent-primary-hover); }
.loading, .error, .empty { text-align: center; padding: 60px 20px; color: var(--text-secondary); }
.empty-hint { margin-top: 8px; font-size: 13px; color: var(--text-secondary); }
.error { color: var(--status-error-accent); }
.trace-table { width: 100%; border-collapse: collapse; }
.trace-table th { text-align: left; padding: 10px 12px; font-size: 12px; color: var(--text-secondary); text-transform: uppercase; border-bottom: 1px solid var(--border-default); }
.trace-table td { padding: 10px 12px; font-size: 14px; border-bottom: 1px solid var(--border-subtle); }
.trace-row { cursor: pointer; }
.trace-row:hover { background: var(--bg-surface); }
.cell-session-id { font-weight: 600; color: var(--accent-blue); max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cell-tokens { color: var(--token-highlight); font-weight: 600; }
.cell-time { color: var(--text-secondary); font-size: 13px; white-space: nowrap; }
.error-rate { font-weight: 600; font-size: 13px; }
.error-high { color: var(--status-error-text); }
.error-medium { color: var(--status-warning); }
.error-none { color: var(--status-ok-text); }
.pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 20px; }
.page-info { font-size: 14px; color: var(--text-secondary); }
.page-size { display: flex; align-items: center; gap: 6px; font-size: 14px; color: var(--text-secondary); }
.page-size select { padding: 2px 6px; background: var(--bg-primary); color: var(--text-primary); border: 1px solid var(--border-default); border-radius: 4px; font-size: 13px; }
</style>
