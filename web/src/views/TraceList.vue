<template>
  <div class="trace-list">
    <div class="filters">
      <TimeRangePicker :key="resetKey" @change="onTimeChange" />
      <input
        v-model="filters.q"
        type="text"
        :placeholder="t('traceList.searchPlaceholder')"
        class="search-input"
        @keyup.enter="search"
      />
      <button @click="search" class="btn">{{ t('common.search') }}</button>
      <button @click="reset" class="btn">{{ t('common.reset') }}</button>
      <button v-if="selectedIds.size > 0" @click="downloadSelected" :disabled="exportLoading" class="btn">
        {{ exportLoading ? t('common.loading') : t('traceList.downloadSelected', { count: selectedIds.size }) }}
      </button>
      <button @click="triggerImport" :disabled="importLoading" class="btn">
        {{ importLoading ? t('common.loading') : t('traceList.importBtn') }}
      </button>
      <input ref="fileInput" type="file" accept=".json" style="display:none" @change="handleImportFile" />

      <span v-if="importResult" class="import-result">{{ t('traceList.importResult', { imported: importResult.imported, skipped: importResult.skipped }) }}</span>
      <span v-if="importError" class="import-error">{{ t('traceList.importFailed') }}: {{ importError }}</span>
    </div>

    <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <template v-else>
      <table class="trace-table" v-if="traces.length > 0">
        <thead>
          <tr>
            <th class="col-checkbox">
              <input type="checkbox" :checked="selectedIds.size === traces.length && traces.length > 0" @change="toggleSelectAll" />
            </th>
            <th>{{ t('traceList.name') }}</th>
            <th class="has-filter">
              <div class="th-head">
                <span>{{ t('traceList.service') }}</span>
                <button class="filter-btn" :class="{ active: !!filters.service }" :title="t('traceList.filter')" @click.stop="toggleFilter('service')">▼</button>
              </div>
              <div v-if="openFilter === 'service'" class="filter-popover" @click.stop>
                <ul class="filter-list">
                  <li :class="{ active: filters.service === '' }" @click="selectService('')">{{ t('common.allServices') }}</li>
                  <li v-for="svc in services" :key="svc" :class="{ active: filters.service === svc }" @click="selectService(svc)">{{ svc }}</li>
                </ul>
              </div>
            </th>
            <th>{{ t('traceList.duration') }}</th>
            <th>{{ t('traceList.spans') }}</th>
            <th class="has-filter">
              <div class="th-head">
                <span>{{ t('traceList.status') }}</span>
                <button class="filter-btn" :class="{ active: !!filters.status }" :title="t('traceList.filter')" @click.stop="toggleFilter('status')">▼</button>
              </div>
              <div v-if="openFilter === 'status'" class="filter-popover" @click.stop>
                <ul class="filter-list">
                  <li :class="{ active: filters.status === '' }" @click="selectStatus('')">{{ t('traceList.allStatus') }}</li>
                  <li :class="{ active: filters.status === 'OK' }" @click="selectStatus('OK')">OK</li>
                  <li :class="{ active: filters.status === 'ERROR' }" @click="selectStatus('ERROR')">ERROR</li>
                </ul>
              </div>
            </th>
            <th>{{ t('traceList.tokens') }}</th>
            <th>Cost</th>
            <th>{{ t('traceList.time') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="trace in traces"
            :key="trace.trace_id_hex"
            :class="['trace-row', { 'row-selected': isSelected(trace.trace_id_hex) }]"
          >
            <td class="col-checkbox" @click.stop>
              <input type="checkbox" :checked="isSelected(trace.trace_id_hex)" @change="toggleSelect(trace.trace_id_hex)" />
            </td>
            <td class="cell-name" @click="goToTrace(trace.trace_id_hex)">{{ trace.root_name }}</td>
            <td @click="goToTrace(trace.trace_id_hex)">{{ trace.root_service }}</td>
            <td @click="goToTrace(trace.trace_id_hex)">{{ formatDuration(trace.duration_ms) }}</td>
            <td @click="goToTrace(trace.trace_id_hex)">{{ trace.span_count }}</td>
            <td @click="goToTrace(trace.trace_id_hex)">
              <span :class="['status-badge', statusClass(trace.status)]">{{ trace.status }}</span>
            </td>
            <td @click="goToTrace(trace.trace_id_hex)">{{ formatTokens(trace.total_tokens) }}</td>
            <td class="cell-cost" @click="goToTrace(trace.trace_id_hex)">{{ formatCost(trace.cost, trace.cost_currency) }}</td>
            <td class="cell-time" @click="goToTrace(trace.trace_id_hex)">{{ formatTime(trace.start_time_ms) }}</td>
          </tr>
        </tbody>
      </table>

      <div v-else class="empty">
        {{ t('traceList.noTraces') }}
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
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { listTraces, getServices, exportTraces, importTraces, type TraceListItem, type Pagination, type ImportResult, type TimeRangeSelection } from '../api/client'
import { formatCost } from '../utils/format'
import { usePageSize } from '../composables/usePageSize'
import TimeRangePicker from '../components/TimeRangePicker.vue'

const router = useRouter()
const { t } = useI18n()

const traces = ref<TraceListItem[]>([])
const { options: pageSizeOptions, loadPageSize, savePageSize } = usePageSize('traces')
const pagination = ref<Pagination>({ page: 1, page_size: loadPageSize(), total: 0 })
const services = ref<string[]>([])
const loading = ref(true)
const error = ref('')

// Batch selection state
const selectedIds = ref<Set<string>>(new Set())
const exportLoading = ref(false)
const importLoading = ref(false)
const importResult = ref<ImportResult | null>(null)
const importError = ref('')
const fileInput = ref<HTMLInputElement | null>(null)

const timeRange = ref<TimeRangeSelection>({ period: 'today' })
const resetKey = ref(0)

function onTimeChange(sel: TimeRangeSelection) {
  timeRange.value = sel
  fetchTraces(1)
}

function toggleSelect(traceId: string) {
  const next = new Set(selectedIds.value)
  if (next.has(traceId)) {
    next.delete(traceId)
  } else {
    next.add(traceId)
  }
  selectedIds.value = next
}

function toggleSelectAll() {
  if (selectedIds.value.size === traces.value.length) {
    selectedIds.value = new Set()
  } else {
    selectedIds.value = new Set(traces.value.map(t => t.trace_id_hex))
  }
}

function clearSelection() {
  selectedIds.value = new Set()
}

function isSelected(traceId: string): boolean {
  return selectedIds.value.has(traceId)
}

function downloadBlob(content: string, filename: string) {
  const blob = new Blob([content], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

async function downloadSelected() {
  const ids = Array.from(selectedIds.value)
  if (ids.length === 0) return

  exportLoading.value = true
  try {
    const data = await exportTraces(ids, 'otlp')
    downloadBlob(JSON.stringify(data, null, 2), 'labubu-traces-export.json')
  } catch (e: any) {
    alert(`${t('traceList.exportFailed')}: ${e.message}`)
  } finally {
    exportLoading.value = false
  }
}

function triggerImport() {
  fileInput.value?.click()
}

async function handleImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  importLoading.value = true
  importResult.value = null
  importError.value = ''
  try {
    const text = await file.text()
    const result = await importTraces(text)
    importResult.value = result
    fetchTraces()
  } catch (e: any) {
    importError.value = e.message
  } finally {
    importLoading.value = false
    input.value = ''
  }
}

const filters = ref({
  q: '',
  service: '',
  status: '',
})

const openFilter = ref<'service' | 'status' | ''>('')

function toggleFilter(col: 'service' | 'status') {
  openFilter.value = openFilter.value === col ? '' : col
}

function closeFilter() {
  openFilter.value = ''
}

function selectService(s: string) {
  filters.value.service = s
  openFilter.value = ''
  fetchTraces(1)
}

function selectStatus(s: string) {
  filters.value.status = s
  openFilter.value = ''
  fetchTraces(1)
}

const totalPages = computed(() => {
  return Math.max(1, Math.ceil(pagination.value.total / pagination.value.page_size))
})

async function fetchTraces(page = 1) {
  clearSelection()
  loading.value = true
  error.value = ''
  try {
    const result = await listTraces({
      ...filters.value,
      page,
      page_size: pagination.value.page_size,
      start: timeRange.value.start,
      end: timeRange.value.end,
    })
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
  openFilter.value = ''
  // Bumping :key remounts the picker → it re-emits the default 'today' range
  // → onTimeChange → fetchTraces(1). Clears any custom datetime too.
  resetKey.value++
}

function goToPage(page: number) {
  fetchTraces(page)
}

function changePageSize(n: number) {
  pagination.value.page_size = n
  savePageSize(n)
  fetchTraces(1)
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
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return String(tokens)
}

function formatTime(ms: number): string {
  return new Date(ms).toLocaleString()
}

function statusClass(status: string): string {
  switch (status) {
    case 'OK': return 'status-ok'
    case 'ERROR': return 'status-error'
    default: return ''
  }
}

// Clear selection when page changes
watch(() => pagination.value.page, () => {
  clearSelection()
})

onMounted(() => {
  document.addEventListener('click', closeFilter)
  // fetchTraces is triggered by the picker's mount emit (default 'today').
  fetchServices()
})

onUnmounted(() => {
  document.removeEventListener('click', closeFilter)
})
</script>

<style scoped>
.trace-list { max-width: 1400px; }
.filters { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; align-items: center; }
.filters :deep(.period-bar) { margin-bottom: 0; }
.search-input { flex: 1; min-width: 200px; padding: 8px 12px; background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 6px; color: var(--text-primary); font-size: 14px; }
.has-filter { position: relative; }
.th-head { display: flex; align-items: center; gap: 4px; }
.filter-btn { background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 10px; padding: 0; line-height: 1; }
.filter-btn:hover { color: var(--text-primary); }
.filter-btn.active { color: var(--accent-blue); }
.filter-popover { position: absolute; top: 100%; left: 0; z-index: 30; min-width: 220px; margin-top: 4px; padding: 6px; background: var(--bg-primary); border: 1px solid var(--border-default); border-radius: 6px; box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25); text-transform: none; }
.filter-list { list-style: none; margin: 6px 0 0; padding: 0; max-height: 220px; overflow-y: auto; }
.filter-list li { padding: 4px 8px; cursor: pointer; border-radius: 4px; font-size: 12px; color: var(--text-primary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.filter-list li:hover { background: var(--bg-surface-hover); }
.filter-list li.active { color: var(--accent-blue); font-weight: 600; }
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
.cell-name { font-weight: 600; color: var(--accent-blue); max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cell-time { color: var(--text-secondary); font-size: 13px; white-space: nowrap; }
.status-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.status-ok { background: var(--status-ok-bg); color: var(--status-ok-text); }
.status-error { background: var(--status-error-bg); color: var(--status-error-text); }
.pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 20px; }
.page-info { font-size: 14px; color: var(--text-secondary); }
.page-size { display: flex; align-items: center; gap: 6px; font-size: 14px; color: var(--text-secondary); }
.page-size select { padding: 2px 6px; background: var(--bg-primary); color: var(--text-primary); border: 1px solid var(--border-default); border-radius: 4px; font-size: 13px; }

/* Batch selection */
.col-checkbox {
  width: 36px;
  text-align: center;
}
.col-checkbox input[type="checkbox"] {
  width: 16px;
  height: 16px;
  cursor: pointer;
  accent-color: var(--accent-primary);
}
.row-selected {
  background: var(--bg-surface) !important;
}
.import-result { color: var(--status-ok-text); font-size: 13px; margin-left: 8px; }
.import-error { color: var(--status-error-accent); font-size: 13px; margin-left: 8px; }
</style>
