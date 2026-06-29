<template>
  <div class="log-list">
    <h2 class="page-title">{{ t('nav.logs') }}</h2>

    <!-- Toolbar -->
    <div class="log-toolbar">
      <input
        v-model="searchQuery"
        type="text"
        class="search-input"
        :placeholder="t('logList.searchPlaceholder')"
        @keyup.enter="search"
      />
      <button @click="search" class="btn">{{ t('common.search') }}</button>
      <button @click="reset" class="btn">{{ t('common.reset') }}</button>
    </div>

    <TimeRangePicker :key="resetKey" @change="onTimeChange" />

    <!-- Table -->
    <div class="log-table-wrap">
      <table class="log-table" v-if="logs.length > 0">
        <thead>
          <tr>
            <th class="col-time">{{ t('logList.timestamp') }}</th>
            <th class="col-sev has-filter">
              <div class="th-head">
                <span>{{ t('logList.severity') }}</span>
                <button class="filter-btn" :class="{ active: !!severityFilter }" :title="t('logList.filter')" @click.stop="toggleFilter('severity')">▼</button>
              </div>
              <div v-if="openFilter === 'severity'" class="filter-popover" @click.stop>
                <ul class="filter-list">
                  <li :class="{ active: severityFilter === '' }" @click="selectSeverity('')">{{ t('logList.allSeverity') }}</li>
                  <li v-for="s in SEVERITY_OPTIONS" :key="s" :class="{ active: severityFilter === s }" @click="selectSeverity(s)">{{ s }}</li>
                </ul>
              </div>
            </th>
            <th class="col-trace has-filter">
              <div class="th-head">
                <span>{{ t('logList.trace') }}</span>
                <button class="filter-btn" :class="{ active: !!traceIdFilter }" :title="t('logList.filter')" @click.stop="toggleFilter('trace')">▼</button>
              </div>
              <div v-if="openFilter === 'trace'" class="filter-popover right" @click.stop>
                <input class="header-filter" v-model="traceIdFilter" :placeholder="t('logList.filterTraceId')" @keyup.enter="applyTraceFilter" />
              </div>
            </th>
            <th class="col-body">{{ t('logList.body') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(log, idx) in logs"
            :key="idx"
            class="log-row"
          >
            <td class="col-time">{{ formatTime(log.timestamp) }}</td>
            <td class="col-sev">
              <span :class="['severity-badge', log.severity.toLowerCase()]">{{ log.severity }}</span>
            </td>
            <td class="col-trace">
              <router-link
                v-if="log.trace_id_hex"
                :to="`/traces/${log.trace_id_hex}`"
                class="trace-link"
                :title="log.trace_id_hex"
              >{{ log.trace_id_hex.substring(0, 5) }}...</router-link>
              <span v-else>-</span>
            </td>
            <td class="col-body">
              <pre class="log-body-cell">{{ formatBody(log.body) || '-' }}</pre>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else-if="!loading" class="empty-state">
        {{ t('logList.noLogs') }}
        <div v-if="timeRange.period !== 'all'" class="empty-hint">{{ t('timeRange.emptyHint') }}</div>
      </div>
      <div v-else class="loading-state">{{ t('common.loading') }}</div>
    </div>

    <!-- Pagination -->
    <div class="pagination" v-if="total > 0">
      <button :disabled="page <= 1" @click="goPage(page - 1)">{{ t('common.prev') }}</button>
      <span class="page-info">{{ t('common.pageOf', { page, total: totalPages, count: total }) }}</span>
      <button :disabled="page >= totalPages" @click="goPage(page + 1)">{{ t('common.next') }}</button>
      <span class="page-size">
        <label>{{ t('common.perPage') }}</label>
        <select
          :value="pageSize"
          @change="changePageSize(Number(($event.target as HTMLSelectElement).value))"
        >
          <option v-for="n in pageSizeOptions" :key="n" :value="n">{{ n }}</option>
        </select>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { listLogs, type LogRecord, type TimeRangeSelection } from '../api/client'
import { usePageSize } from '../composables/usePageSize'
import TimeRangePicker from '../components/TimeRangePicker.vue'

const { t } = useI18n()

const logs = ref<LogRecord[]>([])
const loading = ref(false)
const page = ref(1)
const { options: pageSizeOptions, loadPageSize, savePageSize } = usePageSize('logs')
const pageSize = ref(loadPageSize())
const total = ref(0)

const searchQuery = ref('')
const severityFilter = ref('')
const traceIdFilter = ref('')
const SEVERITY_OPTIONS = ['ERROR', 'WARN', 'INFO', 'DEBUG'] as const
const openFilter = ref<'severity' | 'trace' | ''>('')

const timeRange = ref<TimeRangeSelection>({ period: 'today' })
const resetKey = ref(0)

function onTimeChange(sel: TimeRangeSelection) {
  timeRange.value = sel
  page.value = 1
  fetchLogs()
}

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))

function search() {
  page.value = 1
  fetchLogs()
}

function reset() {
  searchQuery.value = ''
  severityFilter.value = ''
  traceIdFilter.value = ''
  page.value = 1
  resetKey.value++ // remount picker → re-emits default 'today' → fetchLogs
}

function toggleFilter(col: 'severity' | 'trace') {
  if (openFilter.value === col) {
    openFilter.value = ''
  } else {
    openFilter.value = col
  }
}

function closeFilter() {
  openFilter.value = ''
}

function selectSeverity(s: string) {
  severityFilter.value = s
  openFilter.value = ''
  search()
}

function applyTraceFilter() {
  search()
  openFilter.value = ''
}

async function fetchLogs() {
  loading.value = true
  try {
    const result = await listLogs({
      page: page.value,
      page_size: pageSize.value,
      severity: severityFilter.value || undefined,
      q: searchQuery.value || undefined,
      trace_id: traceIdFilter.value || undefined,
      start: timeRange.value.start,
      end: timeRange.value.end,
    })
    logs.value = result.logs
    total.value = result.pagination.total
  } catch {
    logs.value = []
  } finally {
    loading.value = false
  }
}

function goPage(p: number) {
  page.value = p
  fetchLogs()
}

function changePageSize(n: number) {
  pageSize.value = n
  savePageSize(n)
  page.value = 1
  fetchLogs()
}

function formatTime(ts: number): string {
  const d = new Date(ts)
  const pad = (n: number, l = 2) => String(n).padStart(l, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

function formatBody(body: string): string {
  if (!body) return ''
  try {
    const parsed = JSON.parse(body)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return body
  }
}

onMounted(() => {
  document.addEventListener('click', closeFilter)
  // fetchLogs is triggered by the picker's mount emit (default 'today').
})

onUnmounted(() => {
  document.removeEventListener('click', closeFilter)
})
</script>

<style scoped>
.log-list { max-width: 1600px; }
.page-title { font-size: 20px; margin-bottom: 16px; color: var(--text-primary); }

.log-toolbar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}
.search-input {
  flex: 1;
  padding: 8px 12px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 13px;
}
.search-input:focus { border-color: var(--accent-blue); outline: none; }
.search-input::placeholder { color: var(--text-muted); }
.filter-select {
  padding: 8px 12px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
}
.btn {
  padding: 8px 16px;
  background: var(--bg-surface-hover);
  border: 1px solid var(--border-strong);
  border-radius: 6px;
  color: var(--text-primary);
  cursor: pointer;
  font-size: 13px;
}
.btn:hover { background: var(--border-strong); }

.log-table-wrap {
  border: 1px solid var(--border-default);
  border-radius: 8px;
  overflow: hidden;
}
.log-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.log-table th {
  text-align: left;
  padding: 10px 12px;
  font-size: 11px;
  color: var(--text-secondary);
  text-transform: uppercase;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border-default);
}
.has-filter { position: relative; }
.th-head { display: flex; align-items: center; gap: 4px; }
.filter-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 10px;
  padding: 0;
  line-height: 1;
}
.filter-btn:hover { color: var(--text-primary); }
.filter-btn.active { color: var(--accent-blue); }
.filter-popover {
  position: absolute;
  top: 100%;
  left: 0;
  z-index: 30;
  min-width: 220px;
  margin-top: 4px;
  padding: 6px;
  background: var(--bg-primary);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25);
  text-transform: none;
}
.filter-popover.right { left: auto; right: 0; }
.filter-list { list-style: none; margin: 6px 0 0; padding: 0; max-height: 220px; overflow-y: auto; }
.filter-list li {
  padding: 4px 8px;
  cursor: pointer;
  border-radius: 4px;
  font-size: 12px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.filter-list li:hover { background: var(--bg-surface-hover); }
.filter-list li.active { color: var(--accent-blue); font-weight: 600; }
.header-filter {
  width: 100%;
  box-sizing: border-box;
  padding: 4px 6px;
  background: var(--bg-primary);
  border: 1px solid var(--border-default);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 12px;
}
.header-filter:focus { border-color: var(--accent-blue); outline: none; }
.log-row:hover { background: var(--bg-surface); }
.log-table td {
  padding: 8px 12px;
  border-bottom: 1px solid var(--bg-surface-deep);
  color: var(--text-primary);
  vertical-align: top;
}
.col-time { width: 180px; white-space: nowrap; font-variant-numeric: tabular-nums; color: var(--text-secondary); }
.col-sev { width: 80px; }
.col-body { white-space: pre-wrap; word-break: break-all; color: var(--text-primary); }
.col-trace { width: 90px; }

.severity-badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
}
.severity-badge.error { background: var(--status-error-bg); color: var(--status-error-accent); }
.severity-badge.warn { background: rgba(251, 191, 36, 0.15); color: var(--status-warning); }
.severity-badge.info { background: rgba(56, 189, 248, 0.12); color: var(--accent-blue); }
.severity-badge.debug { background: var(--bg-surface-hover); color: var(--text-secondary); }

.trace-link { color: var(--accent-blue); text-decoration: none; font-family: 'Courier New', monospace; font-size: 12px; }
.trace-link:hover { text-decoration: underline; }

.log-body-cell {
  margin: 0;
  padding: 0;
  background: transparent;
  color: var(--text-primary);
  font-size: 12px;
  font-family: 'Courier New', monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 240px;
  overflow-y: auto;
}

[data-theme="light"] .log-body-cell {
  color: #0f172a;
}

.empty-state, .loading-state { text-align: center; padding: 40px; color: var(--text-secondary); }
.empty-hint { margin-top: 8px; font-size: 13px; color: var(--text-secondary); }

.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  margin-top: 16px;
}
.pagination button {
  padding: 6px 16px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
}
.pagination button:hover:not(:disabled) { border-color: var(--accent-blue); color: var(--accent-blue); }
.pagination button:disabled { opacity: 0.4; cursor: not-allowed; }
.page-info { font-size: 13px; color: var(--text-secondary); }
.page-size { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--text-secondary); }
.page-size select { padding: 2px 6px; background: var(--bg-primary); color: var(--text-primary); border: 1px solid var(--border-default); border-radius: 4px; font-size: 13px; }
</style>
