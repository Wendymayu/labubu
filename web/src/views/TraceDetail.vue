<template>
  <div class="trace-detail">
    <div class="back-link">
      <router-link to="/traces">← Back to traces</router-link>
    </div>

    <div v-if="loading" class="loading">Loading...</div>
    <div v-else-if="error" class="error">{{ error }}</div>

    <template v-else-if="trace">
      <div class="trace-summary">
        <h2>{{ rootSpanName }}</h2>
        <div class="summary-grid">
          <div class="summary-item">
            <span class="summary-label">Trace ID</span>
            <span class="summary-value mono">{{ traceIdHex }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Service</span>
            <span class="summary-value">{{ trace.resource_attributes?.['service.name'] || '-' }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Duration</span>
            <span class="summary-value">{{ formatDuration(trace.duration_ms) }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Spans</span>
            <span class="summary-value">{{ trace.spans.length }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">Total Tokens</span>
            <span class="summary-value token-highlight">{{ formatTokens(computeTotalTokens()) }}</span>
          </div>
          <div class="download-group">
            <button class="btn-download" @click="downloadTraceJSON" title="Download as internal JSON">JSON</button>
            <button class="btn-download" @click="downloadTraceOTLP" title="Download as OTLP JSON (importable to Jaeger/Grafana)">OTLP</button>
          </div>
        </div>
      </div>

      <div class="detail-layout" :class="{ 'drawer-open': drawerOpen }">
        <div class="waterfall-panel">
          <WaterfallChart
            :spans="trace.spans"
            :trace-start-ms="trace.start_time_ms"
            :trace-duration-ms="trace.duration_ms"
            :selected-span-id="selectedSpan?.span_id"
            :log-counts="logCounts"
            @select-span="openDrawer"
            @filter-logs="filterLogsBySpan"
          />

        <!-- Tab bar and Log panel -->
        <div class="detail-panel" v-if="!drawerOpen">
          <div class="panel-tabs">
            <button :class="['tab-btn', { active: activeTab === 'spans' }]" @click="switchTab('spans')">
              Spans
            </button>
            <button :class="['tab-btn', { active: activeTab === 'logs' }]" @click="switchTab('logs')">
              {{ t('logList.logCount', { count: totalLogCount }) }}
            </button>
          </div>

          <div v-if="activeTab === 'logs'" class="log-panel">
            <div v-if="logSpanFilter" class="log-filter-tag">
              {{ t('logList.filteredBySpan', { name: logSpanFilter }) }}
              <button class="filter-clear" @click="clearLogFilter">✕</button>
            </div>
            <div v-if="logsLoading" class="loading-state">{{ t('common.loading') }}</div>
            <div v-else-if="filteredLogs.length === 0" class="empty-state">{{ t('logList.noLogs') }}</div>
            <div v-else class="log-list-inline">
              <div
                v-for="(log, idx) in filteredLogs"
                :key="idx"
                :class="['log-item', { expanded: logExpandedIdx === idx }]"
                @click="logExpandedIdx = logExpandedIdx === idx ? null : idx"
              >
                <div class="log-item-header">
                  <span class="log-item-time">{{ formatLogTime(log.timestamp) }}</span>
                  <span :class="['severity-badge', log.severity.toLowerCase()]">{{ log.severity }}</span>
                  <span class="log-item-event">{{ log.event_name || '-' }}</span>
                  <span class="log-item-expand">{{ logExpandedIdx === idx ? '▼' : '▶' }}</span>
                </div>
                <pre v-if="logExpandedIdx === idx" class="log-item-body">{{ formatLogBody(log.body) }}</pre>
              </div>
            </div>
          </div>

          <div v-if="activeTab === 'spans'" class="hint-click">Click any span to view details</div>
        </div>
        </div>

        <div v-if="drawerOpen" class="detail-drawer">
          <div class="drawer-header">
            <div class="drawer-title">
              <span class="drawer-span-name">{{ selectedSpan?.name }}</span>
              <span class="drawer-span-id">{{ selectedSpan?.span_id }}</span>
            </div>
            <button class="drawer-close" @click="closeDrawer" title="Close (Esc)">✕</button>
          </div>

          <div class="drawer-body">
            <TokenPieChart
              v-if="selectedSpanTokenSlices.length > 0"
              :items="selectedSpanTokenSlices"
              :input-tokens="selectedSpanInputTokens"
              :output-tokens="selectedSpanOutputTokens"
            />
            <SpanDetail :span="selectedSpan" />
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getTrace, getLogsByTrace, type TraceDetailResponse, type SpanDetail as SpanDetailType, type LogRecord } from '../api/client'
import WaterfallChart from '../components/WaterfallChart.vue'
import SpanDetail from '../components/SpanDetail.vue'
import TokenPieChart from '../components/TokenPieChart.vue'
import type { PieSlice } from '../components/TokenPieChart.vue'

const route = useRoute()
const { t } = useI18n()
const traceIdHex = route.params.id as string

const trace = ref<TraceDetailResponse['trace'] | null>(null)
const loading = ref(true)
const error = ref('')
const selectedSpan = ref<SpanDetailType | null>(null)
const drawerOpen = ref(false)
const traceLogs = ref<LogRecord[]>([])
const logsLoading = ref(false)
const activeTab = ref<'spans' | 'logs'>('spans')
const logSpanFilter = ref('')
const logExpandedIdx = ref<number | null>(null)

/** Context-window token breakdown, matching gen_ai.context.*_tokens convention. */
const CTX_PATTERNS: { key: string; label: string }[] = [
  { key: 'gen_ai.context.system_tokens',           label: 'System' },
  { key: 'gen_ai.context.assistant_tokens',        label: 'Assistant History' },
  { key: 'gen_ai.context.user_tokens',             label: 'User' },
  { key: 'gen_ai.context.tool_tokens',             label: 'Tool Results' },
  { key: 'gen_ai.context.tool_definitions_tokens', label: 'Tool Definitions' },
  { key: 'gen_ai.context.skill_tokens',            label: 'Skill' },
]

const selectedSpanTokenSlices = computed<PieSlice[]>(() => {
  const span = selectedSpan.value
  if (!span) return []
  const attrs = span.attributes || {}
  const slices: PieSlice[] = []

  for (const { key, label } of CTX_PATTERNS) {
    const raw = attrs[key]
    if (!raw) continue
    const n = parseInt(raw, 10)
    if (isNaN(n) || n <= 0) continue
    slices.push({ name: label, tokens: n })
  }

  return slices
})

const selectedSpanInputTokens = computed(() => {
  const span = selectedSpan.value
  if (!span) return 0
  return span.input_tokens ?? 0
})

const selectedSpanOutputTokens = computed(() => {
  const span = selectedSpan.value
  if (!span) return 0
  return span.output_tokens ?? 0
})

const logCounts = computed(() => {
  const counts: Record<string, number> = {}
  for (const l of traceLogs.value) {
    if (l.span_id_hex) {
      counts[l.span_id_hex] = (counts[l.span_id_hex] || 0) + 1
    }
  }
  return counts
})

const totalLogCount = computed(() => traceLogs.value.length)

const filteredLogs = computed(() => {
  if (!logSpanFilter.value) return traceLogs.value
  return traceLogs.value.filter(l => l.span_id_hex === logSpanFilter.value)
})

const rootSpanName = computed(() => {
  if (!trace.value) return 'Trace Detail'
  const root = trace.value.spans.find(s => s.parent_span_id === '')
  return root?.name || 'Trace Detail'
})

async function fetchTrace() {
  loading.value = true
  error.value = ''
  try {
    const result = await getTrace(traceIdHex)
    trace.value = result.trace
    fetchTraceLogs()
  } catch (e: any) {
    error.value = e.message || 'Failed to load trace'
  } finally {
    loading.value = false
  }
}

function openDrawer(span: SpanDetailType) {
  selectedSpan.value = span
  drawerOpen.value = true
}

function closeDrawer() {
  drawerOpen.value = false
}

async function fetchTraceLogs() {
  logsLoading.value = true
  try {
    const result = await getLogsByTrace(traceIdHex)
    traceLogs.value = result.logs || []
  } catch {
    traceLogs.value = []
  } finally {
    logsLoading.value = false
  }
}

function filterLogsBySpan(spanId: string) {
  logSpanFilter.value = spanId
  activeTab.value = 'logs'
}

function clearLogFilter() {
  logSpanFilter.value = ''
}

function switchTab(tab: 'spans' | 'logs') {
  activeTab.value = tab
}

function formatLogTime(ts: number): string {
  return new Date(ts).toLocaleTimeString()
}

function formatLogBody(body: string): string {
  if (!body) return ''
  try {
    const parsed = JSON.parse(body)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return body
  }
}

function downloadTraceJSON() {
  if (!trace.value) return
  downloadBlob(JSON.stringify(trace.value, null, 2), `trace-${traceIdHex}.json`)
}

async function downloadTraceOTLP() {
  try {
    const res = await fetch(`/api/v1/traces/${traceIdHex}?format=otlp`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const json = await res.json()
    downloadBlob(JSON.stringify(json, null, 2), `trace-${traceIdHex}-otlp.json`)
  } catch (e: any) {
    alert(`OTLP download failed: ${e.message}`)
  }
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

function computeTotalTokens(): number | null {
  if (!trace.value) return null
  let total = 0
  let hasTokens = false
  for (const span of trace.value.spans) {
    if (span.total_tokens != null) {
      total += span.total_tokens
      hasTokens = true
    }
  }
  return hasTokens ? total : null
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
  const mins = Math.floor(ms / 60000)
  const secs = ((ms % 60000) / 1000).toFixed(0)
  return `${mins}m ${secs}s`
}

function formatTokens(tokens: number | null): string {
  if (tokens == null) return '-'
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(0)}K`
  return String(tokens)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && drawerOpen.value) {
    closeDrawer()
  }
}

onMounted(() => {
  fetchTrace()
  window.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
})
</script>

<style scoped>
.trace-detail { max-width: 1600px; }
.back-link { margin-bottom: 16px; }
.back-link a { color: var(--text-secondary); text-decoration: none; font-size: 14px; }
.back-link a:hover { color: var(--text-primary); }
.loading, .error { text-align: center; padding: 60px; color: var(--text-secondary); }
.error { color: var(--status-error-accent); }
.trace-summary { margin-bottom: 24px; }
.trace-summary h2 { font-size: 20px; margin-bottom: 12px; }
.summary-grid { display: flex; gap: 24px; flex-wrap: wrap; }
.summary-item { display: flex; flex-direction: column; }
.summary-label { font-size: 11px; color: var(--text-secondary); text-transform: uppercase; }
.summary-value { font-size: 14px; }
.mono { font-family: 'Courier New', monospace; font-size: 12px; word-break: break-all; }
.token-highlight { color: var(--token-highlight); font-weight: 600; }

.download-group {
  display: flex;
  gap: 0;
  align-self: center;
}
.btn-download {
  padding: 6px 12px;
  border: 1px solid var(--border-group);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 12px;
}
.btn-download:first-child {
  border-radius: 6px 0 0 6px;
}
.btn-download:last-child {
  border-radius: 0 6px 6px 0;
}
.btn-download:hover {
  border-color: var(--accent-blue);
  color: var(--accent-blue);
}

/* === New drawer layout === */
.detail-layout {
  display: flex;
  gap: 0;
}
.detail-layout.drawer-open .waterfall-panel {
  flex: 1;
  min-width: 0;
}
.detail-layout:not(.drawer-open) .waterfall-panel {
  flex: 1;
  width: 100%;
}

.waterfall-panel {
  position: relative;
  overflow-x: auto;
  transition: flex 0.3s ease;
}

.hint-click {
  text-align: center;
  color: var(--text-muted);
  font-size: 12px;
  padding: 24px 0;
}

/* === Drawer === */
.detail-drawer {
  width: 480px;
  flex-shrink: 0;
  border-left: 1px solid var(--border-strong);
  background: var(--bg-primary);
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 240px);
  overflow: hidden;
  animation: slideIn 0.3s ease;
}

@keyframes slideIn {
  from { opacity: 0; transform: translateX(20px); }
  to { opacity: 1; transform: translateX(0); }
}

.drawer-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-strong);
  flex-shrink: 0;
}
.drawer-title {
  min-width: 0;
}
.drawer-span-name {
  display: block;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.drawer-span-id {
  display: block;
  font-size: 10px;
  color: var(--text-muted);
  font-family: 'Courier New', monospace;
  margin-top: 2px;
}
.drawer-close {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 18px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
  line-height: 1;
}
.drawer-close:hover {
  color: var(--text-primary);
  background: var(--bg-surface-hover-subtle);
}

.drawer-body {
  padding: 16px;
  overflow-y: auto;
  flex: 1;
}

.drawer-body::-webkit-scrollbar { width: 4px; }
.drawer-body::-webkit-scrollbar-track { background: transparent; }
.drawer-body::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 2px; }

@media (max-width: 900px) {
  .detail-drawer {
    position: fixed;
    right: 0;
    top: 0;
    bottom: 0;
    width: 90vw;
    max-width: 480px;
    z-index: 100;
    max-height: 100vh;
  }
  .detail-layout.drawer-open::after {
    content: '';
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.5);
    z-index: 99;
  }
}

/* === Tab bar and Log panel === */
.detail-panel {
  margin-top: 16px;
  border: 1px solid var(--border-default);
  border-radius: 8px;
  overflow: hidden;
}
.panel-tabs {
  display: flex;
  border-bottom: 1px solid var(--border-default);
  background: var(--bg-surface);
}
.tab-btn {
  padding: 10px 20px;
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
  border-bottom: 2px solid transparent;
}
.tab-btn.active {
  color: var(--accent-blue);
  border-bottom-color: var(--accent-blue);
}
.tab-btn:hover { color: var(--text-primary); }

.log-panel {
  max-height: 320px;
  overflow-y: auto;
}
.log-filter-tag {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: var(--bg-surface-hover-subtle);
  border-bottom: 1px solid var(--border-default);
  font-size: 12px;
  color: var(--text-secondary);
}
.filter-clear {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 14px;
  padding: 0 4px;
}
.filter-clear:hover { color: var(--status-error-accent); }

.log-list-inline { }
.log-item {
  border-bottom: 1px solid var(--bg-surface-deep);
  cursor: pointer;
}
.log-item:hover { background: var(--bg-surface); }
.log-item.expanded { background: var(--bg-surface-hover-subtle); }
.log-item-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  font-size: 12px;
}
.log-item-time { color: var(--text-secondary); font-variant-numeric: tabular-nums; white-space: nowrap; }
.log-item-event { color: var(--text-secondary); font-family: 'Courier New', monospace; font-size: 11px; }
.log-item-expand { color: var(--text-muted); font-size: 10px; margin-left: auto; }

.severity-badge {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
}
.severity-badge.error { background: var(--status-error-bg); color: var(--status-error-accent); }
.severity-badge.warn { background: rgba(251, 191, 36, 0.15); color: var(--status-warning); }
.severity-badge.info { background: rgba(56, 189, 248, 0.12); color: var(--accent-blue); }
.severity-badge.debug { background: var(--bg-surface-hover); color: var(--text-muted); }

.log-item-body {
  margin: 0;
  padding: 8px 16px 12px;
  font-size: 12px;
  font-family: 'Courier New', monospace;
  color: var(--text-primary);
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 240px;
  overflow-y: auto;
  background: var(--bg-surface-deep);
}

.loading-state, .empty-state { text-align: center; padding: 24px; color: var(--text-secondary); font-size: 13px; }
</style>
