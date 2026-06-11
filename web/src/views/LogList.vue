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
        @input="onSearchDebounced"
      />
      <select v-model="severityFilter" class="filter-select" @change="fetchLogs">
        <option value="">{{ t('logList.allSeverity') }}</option>
        <option value="ERROR">ERROR</option>
        <option value="WARN">WARN</option>
        <option value="INFO">INFO</option>
        <option value="DEBUG">DEBUG</option>
      </select>
      <select v-model="eventFilter" class="filter-select" @change="fetchLogs">
        <option value="">{{ t('logList.allEvents') }}</option>
        <option v-for="ev in eventNames" :key="ev" :value="ev">{{ ev }}</option>
      </select>
    </div>

    <!-- Table -->
    <div class="log-table-wrap">
      <table class="log-table" v-if="logs.length > 0">
        <thead>
          <tr>
            <th class="col-time">{{ t('logList.timestamp') }}</th>
            <th class="col-sev">{{ t('logList.severity') }}</th>
            <th class="col-event">{{ t('logList.event') }}</th>
            <th class="col-body">{{ t('logList.body') }}</th>
            <th class="col-trace">{{ t('logList.trace') }}</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="(log, idx) in logs" :key="idx">
            <tr
              :class="['log-row', { expanded: expandedIdx === idx }]"
              @click="toggleExpand(idx)"
            >
              <td class="col-time">{{ formatTime(log.timestamp) }}</td>
              <td class="col-sev">
                <span :class="['severity-badge', log.severity.toLowerCase()]">{{ log.severity }}</span>
              </td>
              <td class="col-event">{{ log.event_name || '-' }}</td>
              <td class="col-body">{{ truncateBody(log.body) }}</td>
              <td class="col-trace">
                <router-link
                  v-if="log.trace_id_hex"
                  :to="`/traces/${log.trace_id_hex}`"
                  class="trace-link"
                  @click.stop
                >{{ log.trace_id_hex.substring(0, 8) }}...</router-link>
                <span v-else>-</span>
              </td>
            </tr>
            <tr v-if="expandedIdx === idx" class="log-expand-row">
              <td colspan="5">
                <pre class="log-body-full">{{ formatBody(log.body) }}</pre>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
      <div v-else-if="!loading" class="empty-state">{{ t('logList.noLogs') }}</div>
      <div v-else class="loading-state">{{ t('common.loading') }}</div>
    </div>

    <!-- Pagination -->
    <div class="pagination" v-if="total > pageSize">
      <button :disabled="page <= 1" @click="goPage(page - 1)">{{ t('common.prev') }}</button>
      <span class="page-info">{{ t('common.pageOf', { page, total: totalPages, count: total }) }}</span>
      <button :disabled="page >= totalPages" @click="goPage(page + 1)">{{ t('common.next') }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { listLogs, getLogEventNames, type LogRecord } from '../api/client'

const { t } = useI18n()

const logs = ref<LogRecord[]>([])
const loading = ref(false)
const page = ref(1)
const pageSize = 20
const total = ref(0)
const expandedIdx = ref<number | null>(null)

const searchQuery = ref('')
const severityFilter = ref('')
const eventFilter = ref('')
const eventNames = ref<string[]>([])

let debounceTimer: ReturnType<typeof setTimeout> | null = null

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

function onSearchDebounced() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    page.value = 1
    fetchLogs()
  }, 500)
}

async function fetchLogs() {
  loading.value = true
  try {
    const result = await listLogs({
      page: page.value,
      page_size: pageSize,
      severity: severityFilter.value || undefined,
      event_name: eventFilter.value || undefined,
      q: searchQuery.value || undefined,
    })
    logs.value = result.logs
    total.value = result.pagination.total
  } catch {
    logs.value = []
  } finally {
    loading.value = false
  }
}

async function fetchEventNames() {
  try {
    eventNames.value = await getLogEventNames()
  } catch {
    eventNames.value = []
  }
}

function goPage(p: number) {
  page.value = p
  fetchLogs()
}

function toggleExpand(idx: number) {
  expandedIdx.value = expandedIdx.value === idx ? null : idx
}

function formatTime(ts: number): string {
  return new Date(ts).toLocaleTimeString()
}

function truncateBody(body: string): string {
  if (!body) return '-'
  return body.length > 80 ? body.substring(0, 80) + '...' : body
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
  fetchEventNames()
  fetchLogs()
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
.log-row { cursor: pointer; }
.log-row:hover { background: var(--bg-surface); }
.log-row.expanded { background: var(--bg-surface-hover-subtle); }
.log-table td {
  padding: 8px 12px;
  border-bottom: 1px solid var(--bg-surface-deep);
  color: var(--text-primary);
}
.col-time { width: 100px; white-space: nowrap; font-variant-numeric: tabular-nums; color: var(--text-secondary); }
.col-sev { width: 80px; }
.col-event { width: 140px; font-family: 'Courier New', monospace; font-size: 12px; }
.col-body { max-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-secondary); }
.col-trace { width: 120px; }

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

.log-expand-row td { padding: 0; }
.log-body-full {
  margin: 0;
  padding: 12px 16px;
  background: var(--bg-surface-deep);
  color: var(--text-primary);
  font-size: 12px;
  font-family: 'Courier New', monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 360px;
  overflow-y: auto;
}

.empty-state, .loading-state { text-align: center; padding: 40px; color: var(--text-secondary); }

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
</style>
