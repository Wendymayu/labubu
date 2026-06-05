<template>
  <div class="trace-list">
    <div class="filters">
      <input
        v-model="filters.q"
        type="text"
        :placeholder="t('traceList.searchPlaceholder')"
        class="search-input"
        @keyup.enter="search"
      />
      <select v-model="filters.service" class="filter-select">
        <option value="">{{ t('common.allServices') }}</option>
        <option v-for="svc in services" :key="svc" :value="svc">{{ svc }}</option>
      </select>
      <select v-model="filters.status" class="filter-select">
        <option value="">{{ t('traceList.allStatus') }}</option>
        <option value="OK">OK</option>
        <option value="ERROR">ERROR</option>
      </select>
      <button @click="search" class="btn btn-primary">{{ t('common.search') }}</button>
      <button @click="reset" class="btn">{{ t('common.reset') }}</button>
    </div>

    <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <template v-else>
      <table class="trace-table" v-if="traces.length > 0">
        <thead>
          <tr>
            <th>{{ t('traceList.name') }}</th>
            <th>{{ t('traceList.service') }}</th>
            <th>{{ t('traceList.duration') }}</th>
            <th>{{ t('traceList.spans') }}</th>
            <th>{{ t('traceList.status') }}</th>
            <th>{{ t('traceList.tokens') }}</th>
            <th>{{ t('traceList.time') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="trace in traces"
            :key="trace.trace_id_hex"
            @click="goToTrace(trace.trace_id_hex)"
            class="trace-row"
          >
            <td class="cell-name">{{ trace.root_name }}</td>
            <td>{{ trace.root_service }}</td>
            <td>{{ formatDuration(trace.duration_ms) }}</td>
            <td>{{ trace.span_count }}</td>
            <td>
              <span :class="['status-badge', statusClass(trace.status)]">{{ trace.status }}</span>
            </td>
            <td>{{ formatTokens(trace.total_tokens) }}</td>
            <td class="cell-time">{{ formatTime(trace.start_time_ms) }}</td>
          </tr>
        </tbody>
      </table>

      <div v-else class="empty">{{ t('traceList.noTraces') }}</div>

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
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { listTraces, getServices, type TraceListItem, type Pagination } from '../api/client'

const router = useRouter()
const { t } = useI18n()

const traces = ref<TraceListItem[]>([])
const pagination = ref<Pagination>({ page: 1, page_size: 20, total: 0 })
const services = ref<string[]>([])
const loading = ref(true)
const error = ref('')

const filters = ref({
  q: '',
  service: '',
  status: '',
})

const totalPages = computed(() => {
  return Math.max(1, Math.ceil(pagination.value.total / pagination.value.page_size))
})

async function fetchTraces(page = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await listTraces({ ...filters.value, page, page_size: 20 })
    traces.value = result.traces
    pagination.value = result.pagination
  } catch (e: any) {
    error.value = e.message || 'Failed to load traces'
  } finally {
    loading.value = false
  }
}

async function fetchServices() {
  try {
    services.value = await getServices()
  } catch {
    // Services filter is non-critical.
  }
}

function search() {
  fetchTraces(1)
}

function reset() {
  filters.value = { q: '', service: '', status: '' }
  fetchTraces(1)
}

function goToPage(page: number) {
  fetchTraces(page)
}

function goToTrace(id: string) {
  router.push({ name: 'trace-detail', params: { id } })
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
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

onMounted(() => {
  fetchTraces()
  fetchServices()
})
</script>

<style scoped>
.trace-list { max-width: 1400px; }
.filters { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; }
.search-input { flex: 1; min-width: 200px; padding: 8px 12px; background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 6px; color: var(--text-primary); font-size: 14px; }
.filter-select { padding: 8px 12px; background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 6px; color: var(--text-primary); font-size: 14px; }
.btn { padding: 8px 16px; background: var(--bg-surface-hover); border: 1px solid var(--border-strong); border-radius: 6px; color: var(--text-primary); cursor: pointer; font-size: 14px; }
.btn:hover { background: var(--border-strong); }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-primary { background: var(--accent-primary); border-color: var(--accent-primary); }
.btn-primary:hover { background: var(--accent-primary-hover); }
.loading, .error, .empty { text-align: center; padding: 60px 20px; color: var(--text-secondary); }
.error { color: var(--status-error-accent); }
.trace-table { width: 100%; border-collapse: collapse; }
.trace-table th { text-align: left; padding: 10px 12px; font-size: 12px; color: var(--text-secondary); text-transform: uppercase; border-bottom: 1px solid var(--border-default); }
.trace-table td { padding: 10px 12px; font-size: 14px; border-bottom: 1px solid var(--border-subtle); }
.trace-row { cursor: pointer; }
.trace-row:hover { background: var(--bg-surface); }
.cell-name { font-weight: 600; color: var(--accent-blue); max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cell-time { color: var(--text-secondary); font-size: 13px; white-space: nowrap; }
.status-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.status-ok { background: var(--status-ok-bg); color: var(--status-ok-text); }
.status-error { background: var(--status-error-bg); color: var(--status-error-text); }
.pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 20px; }
.page-info { font-size: 14px; color: var(--text-secondary); }
</style>
