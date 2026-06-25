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
          <div class="summary-item">
            <span class="summary-label">Cost</span>
            <span class="summary-value token-highlight">
              {{ formatCost(trace.cost, trace.cost_currency) }}
              <span v-if="trace.unpriced_spans" class="unpriced-hint">({{ trace.unpriced_spans }} unpriced)</span>
            </span>
          </div>
          <div class="download-group">
            <button class="btn-download" @click="downloadTraceOTLP">{{ t('traceList.download') }}</button>
          </div>
        </div>
        <div class="summary-actions">
          <button :class="['btn-insight', { active: activeInsight === 'logs' }]" @click="toggleInsight('logs')">
            📋 {{ t('logList.logCount', { count: totalLogCount }) }}
          </button>
          <button :class="['btn-insight', { active: activeInsight === 'diagnosis' }]" @click="toggleInsight('diagnosis')">
            🔍 {{ t('diagnosis.tab') }}
          </button>
          <button :class="['btn-insight', { active: activeInsight === 'agent' }]" @click="toggleInsight('agent')">
            🤖 {{ t('agentStats.agentBehavior') }}
          </button>
        </div>
      </div>

      <div v-if="activeInsight" class="insight-backdrop" @click="activeInsight = null"></div>
      <div v-if="activeInsight" class="insight-overlay">
        <div class="insight-overlay-header">
          <span class="insight-overlay-title">{{
            activeInsight === 'logs' ? t('logList.logCount', { count: totalLogCount })
            : activeInsight === 'diagnosis' ? t('diagnosis.tab')
            : t('agentStats.agentBehavior')
          }}</span>
          <button class="insight-overlay-close" @click="activeInsight = null" title="Close">✕</button>
        </div>
        <div class="insight-overlay-body">
          <DiagnosisTab
            v-if="activeInsight === 'diagnosis'"
            :result="diagnosisResult"
            :loading="diagnosisLoading"
            :noModel="diagnosisNoModel"
            :error="diagnosisError"
            @diagnose="startDiagnosis"
            @navigate-span="onDiagnosisNavigateSpan"
          />
          <AgentBehaviorTab
            v-if="activeInsight === 'agent'"
            :spans="trace.spans"
          />
          <div v-if="activeInsight === 'logs'" class="log-overlay">
            <div v-if="logSpanFilter" class="log-filter-tag">
              {{ t('logList.filteredBySpan', { name: filteredSpanName }) }}
              <button class="filter-clear" @click="clearLogFilter">✕</button>
            </div>
            <div v-if="logsLoading" class="loading-state">{{ t('common.loading') }}</div>
            <div v-else-if="pageLogs.length === 0" class="empty-state">{{ t('logList.noLogs') }}</div>
            <div v-else class="log-list-inline">
              <div
                v-for="(log, idx) in pageLogs"
                :key="idx"
                class="log-item"
              >
                <span class="log-item-time">{{ formatLogTime(log.timestamp) }}</span>
                <span :class="['severity-badge', log.severity.toLowerCase()]">{{ log.severity }}</span>
                <span class="log-item-event">{{ log.event_name || '-' }}</span>
                <span v-if="log.body" class="log-item-body">{{ formatLogBody(log.body) }}</span>
              </div>
            </div>
            <div v-if="!logsLoading && pageLogs.length > 0" class="log-pagination">
              <button class="page-btn" :disabled="logPage <= 1" @click="prevLogPage">◀ {{ t('logList.prev') }}</button>
              <span class="page-info">{{ t('logList.pageOf', { page: logPage, total: Math.max(1, Math.ceil(logTotal / logPageSize)) }) }}</span>
              <button class="page-btn" :disabled="logPage * logPageSize >= logTotal" @click="nextLogPage">{{ t('logList.next') }} ▶</button>
              <span class="page-total">{{ t('logList.logCount', { count: logTotal }) }}</span>
            </div>
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
        </div>

        <div v-if="drawerOpen" class="drawer-backdrop" @click="closeDrawer"></div>
        <div v-if="drawerOpen" class="detail-drawer">
          <div class="drawer-header">
            <div class="drawer-title">
              <span class="drawer-span-name">{{ selectedSpan?.name }}</span>
              <span class="drawer-span-id">{{ selectedSpan?.span_id }}</span>
            </div>
            <div class="drawer-view-toggle">
              <input
                v-model="contentSearch"
                class="content-search"
                type="text"
                placeholder="Search content..."
              />
              <button
                :class="['view-toggle-btn', { active: viewMode === 'structured' }]"
                @click="viewMode = 'structured'"
              >Structured</button>
              <button
                :class="['view-toggle-btn', { active: viewMode === 'json' }]"
                @click="viewMode = 'json'"
              >JSON</button>
            </div>
            <button class="drawer-close" @click="closeDrawer" title="Close (Esc)">✕</button>
          </div>

          <div class="drawer-body">
            <template v-if="viewMode === 'structured'">
              <TokenPieChart
                v-if="selectedSpanTokenSlices.length > 0"
                :items="selectedSpanTokenSlices"
                :input-tokens="selectedSpanInputTokens"
                :output-tokens="selectedSpanOutputTokens"
              />
              <SpanDetail :span="selectedSpan" :search="contentSearch" />
            </template>

            <div v-else class="json-preview">
              <div class="json-toolbar">
                <button class="json-copy-btn" @click="copySpanJSON">
                  {{ copyLabel }}
                </button>
                <input
                  v-model="jsonSearch"
                  class="json-search"
                  type="text"
                  placeholder="Search..."
                />
              </div>
              <pre
                class="json-content"
                v-html="highlightedSpanJSON"
              ></pre>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getTrace, getLogsByTrace, getLogCounts, listLogs, getDiagnosisResult, diagnoseTrace, type TraceDetailResponse, type SpanDetail as SpanDetailType, type LogRecord, type DiagnosisResult } from '../api/client'
import DiagnosisTab from '../components/DiagnosisTab.vue'
import AgentBehaviorTab from '../components/AgentBehaviorTab.vue'
import WaterfallChart from '../components/WaterfallChart.vue'
import SpanDetail from '../components/SpanDetail.vue'
import TokenPieChart from '../components/TokenPieChart.vue'
import type { PieSlice } from '../components/TokenPieChart.vue'
import { formatCost, highlightJSON } from '../utils/format'

const route = useRoute()
const { t } = useI18n()
const traceIdHex = route.params.id as string

const trace = ref<TraceDetailResponse['trace'] | null>(null)
const loading = ref(true)
const error = ref('')
const selectedSpan = ref<SpanDetailType | null>(null)
const drawerOpen = ref(false)
const pageLogs = ref<LogRecord[]>([])
const logsLoading = ref(false)
const logPage = ref(1)
const logPageSize = 50
const logTotal = ref(0)
const logCounts = ref<Record<string, number>>({})
const activeInsight = ref<'logs' | 'diagnosis' | 'agent' | null>(null)

function toggleInsight(insight: 'logs' | 'diagnosis' | 'agent') {
  if (activeInsight.value === insight) {
    activeInsight.value = null
  } else {
    activeInsight.value = insight
  }
}
const logSpanFilter = ref('')
const viewMode = ref<'structured' | 'json'>('structured')
const jsonSearch = ref('')
const contentSearch = ref('')
const copyLabel = ref('📋 Copy')
const diagnosisResult = ref<DiagnosisResult | null>(null)
const diagnosisLoading = ref(false)
const diagnosisNoModel = ref(false)
const diagnosisError = ref('')

/** Context-window token breakdown, with fallback patterns for multi-agent compatibility. */
const CTX_PATTERNS: { patterns: string[]; label: string }[] = [
  { patterns: ['gen_ai.context.system_prompt',       'system_prompt_tokens'],  label: 'System' },
  { patterns: ['gen_ai.context.assistant_messages',  'assistant_messages_tokens'], label: 'Assistant History' },
  { patterns: ['gen_ai.context.user_messages',       'user_messages_tokens'],  label: 'User' },
  { patterns: ['gen_ai.context.tool_results',        'tool_results_tokens'],   label: 'Tool Results' },
  { patterns: ['gen_ai.context.tool_definitions',    'tool_definitions_tokens'], label: 'Tool Definitions' },
  { patterns: ['gen_ai.context.skill',               'skill_tokens'],          label: 'Skill' },
]

const selectedSpanTokenSlices = computed<PieSlice[]>(() => {
  const span = selectedSpan.value
  if (!span) return []
  const attrs = span.attributes || {}
  const slices: PieSlice[] = []

  for (const { patterns, label } of CTX_PATTERNS) {
    let raw: string | undefined
    for (const p of patterns) {
      raw = attrs[p]
      if (raw) break
    }
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

const totalLogCount = computed(() => {
  let n = 0
  for (const k in logCounts.value) n += logCounts.value[k]
  return n
})

const filteredSpanName = computed(() => {
  if (!logSpanFilter.value || !trace.value) return logSpanFilter.value
  const span = trace.value.spans.find(s => s.span_id === logSpanFilter.value)
  return span?.name || logSpanFilter.value
})

const rootSpanName = computed(() => {
  if (!trace.value) return 'Trace Detail'
  const root = trace.value.spans.find(s => s.parent_span_id === '')
  // Incomplete traces may have no root span yet; fall back to the earliest
  // span so the header still shows something meaningful.
  return root?.name || trace.value.spans[0]?.name || 'Trace Detail'
})

const spanJSON = computed(() => {
  if (!selectedSpan.value) return ''
  return JSON.stringify(selectedSpan.value, null, 2)
})

const highlightedSpanJSON = computed(() => {
  let raw = spanJSON.value
  if (!raw) return ''
  if (jsonSearch.value.trim()) {
    const term = jsonSearch.value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const re = new RegExp(`(${term})`, 'gi')
    raw = raw.replace(re, '<mark class="json-search-hit">$1</mark>')
  }
  return highlightJSON(raw)
})

function copySpanJSON() {
  navigator.clipboard?.writeText(spanJSON.value).then(() => {
    copyLabel.value = '✓ Copied!'
    setTimeout(() => { copyLabel.value = '📋 Copy' }, 2000)
  }).catch(() => {
    copyLabel.value = '✗ Failed'
    setTimeout(() => { copyLabel.value = '📋 Copy' }, 2000)
  })
}

watch(selectedSpan, () => {
  viewMode.value = 'structured'
  jsonSearch.value = ''
  contentSearch.value = ''
})

async function fetchTrace() {
  loading.value = true
  error.value = ''
  try {
    const result = await getTrace(traceIdHex)
    trace.value = result.trace
    fetchLogCounts()
    fetchLogPage()
  } catch (e: any) {
    error.value = e.message || 'Failed to load trace'
  } finally {
    loading.value = false
  }
}

async function fetchDiagnosis() {
  try {
    diagnosisResult.value = await getDiagnosisResult(traceIdHex)
    diagnosisNoModel.value = false
  } catch (e: any) {
    if (e.message === 'no_diagnosis') {
      diagnosisResult.value = null
    }
  }
}

async function startDiagnosis() {
  diagnosisLoading.value = true
  diagnosisNoModel.value = false
  diagnosisError.value = ''
  const currentLocale = localStorage.getItem('locale') || 'en'
  try {
    diagnosisResult.value = await diagnoseTrace(traceIdHex, false, currentLocale)
  } catch (e: any) {
    if (e.message === 'no_default_model') {
      diagnosisNoModel.value = true
      diagnosisLoading.value = false
    } else if (e.message === 'diagnosis_in_flight') {
      // Already diagnosing — keep loading state and poll for result
      pollDiagnosisResult()
    } else {
      diagnosisError.value = e.message || 'Diagnosis failed'
      diagnosisLoading.value = false
    }
  }
}

async function pollDiagnosisResult() {
  // Keep showing loading state. Poll GET endpoint every 3 seconds until result appears.
  for (let i = 0; i < 20; i++) { // max 60 seconds
    await new Promise(r => setTimeout(r, 3000))
    try {
      diagnosisResult.value = await getDiagnosisResult(traceIdHex)
      diagnosisLoading.value = false
      return
    } catch (e: any) {
      if (e.message === 'no_diagnosis') {
        continue // still in progress
      }
      diagnosisError.value = e.message || 'Diagnosis failed'
      diagnosisLoading.value = false
      return
    }
  }
  // Timeout after polling
  diagnosisError.value = 'Diagnosis timed out'
  diagnosisLoading.value = false
}

function onDiagnosisNavigateSpan(spanIndex: number) {
  drawerOpen.value = false
  nextTick(() => {
    if (trace.value?.spans && trace.value.spans[spanIndex]) {
      selectedSpan.value = trace.value.spans[spanIndex]
      drawerOpen.value = true
    }
  })
}

function openDrawer(span: SpanDetailType) {
  selectedSpan.value = span
  drawerOpen.value = true
}

function closeDrawer() {
  drawerOpen.value = false
}

async function fetchLogCounts() {
  try {
    const result = await getLogCounts(traceIdHex)
    logCounts.value = result.counts || {}
  } catch {
    logCounts.value = {}
  }
}

async function fetchLogPage() {
  logsLoading.value = true
  try {
    const result = await listLogs({
      trace_id: traceIdHex,
      span_id: logSpanFilter.value || undefined,
      page: logPage.value,
      page_size: logPageSize,
    })
    pageLogs.value = result.logs || []
    logTotal.value = result.pagination?.total ?? 0
  } catch {
    pageLogs.value = []
    logTotal.value = 0
  } finally {
    logsLoading.value = false
  }
}

function prevLogPage() {
  if (logPage.value > 1) {
    logPage.value--
    fetchLogPage()
  }
}

function nextLogPage() {
  if (logPage.value * logPageSize < logTotal.value) {
    logPage.value++
    fetchLogPage()
  }
}

function filterLogsBySpan(spanId: string) {
  logSpanFilter.value = spanId
  logPage.value = 1
  activeInsight.value = 'logs'
  fetchLogPage()
}

function clearLogFilter() {
  logSpanFilter.value = ''
  logPage.value = 1
  fetchLogPage()
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
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(0)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(0)}K`
  return String(tokens)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (drawerOpen.value) {
      closeDrawer()
    } else if (activeInsight.value) {
      activeInsight.value = null
    }
  }
}

onMounted(() => {
  fetchTrace()
  fetchDiagnosis()
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

/* === Insight actions bar below summary === */
.summary-actions {
  display: flex;
  gap: 6px;
  margin-top: 10px;
}
.btn-insight {
  padding: 6px 14px;
  border: 1px solid var(--border-group);
  border-radius: 6px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 12px;
}
.btn-insight:hover {
  border-color: var(--accent-blue);
  color: var(--accent-blue);
}
.btn-insight.active {
  background: var(--accent-blue);
  color: #fff;
  border-color: var(--accent-blue);
}

/* === Insight overlay panel === */
.insight-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.25);
  z-index: 50;
}
.insight-overlay {
  position: fixed;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 720px;
  max-width: 90vw;
  max-height: 80vh;
  background: var(--bg-primary);
  border: 1px solid var(--border-default);
  border-radius: 12px;
  z-index: 51;
  display: flex;
  flex-direction: column;
  animation: overlayFadeIn 0.2s ease;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
}
.insight-overlay-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-default);
  flex-shrink: 0;
}
.insight-overlay-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
}
.insight-overlay-close {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 16px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
  line-height: 1;
}
.insight-overlay-close:hover {
  color: var(--text-primary);
  background: var(--bg-surface-hover-subtle);
}
.insight-overlay-body {
  padding: 16px;
  overflow-y: auto;
  flex: 1;
}
.insight-overlay-body::-webkit-scrollbar { width: 4px; }
.insight-overlay-body::-webkit-scrollbar-track { background: transparent; }
.insight-overlay-body::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 2px; }
@keyframes overlayFadeIn {
  from { opacity: 0; transform: translate(-50%, -50%) scale(0.95); }
  to { opacity: 1; transform: translate(-50%, -50%) scale(1); }
}

@media (max-width: 600px) {
  .insight-overlay {
    width: 100vw;
    max-width: 100vw;
    max-height: 100vh;
    border-radius: 0;
  }
}

/* === New drawer layout === */
.detail-layout {
  display: flex;
  gap: 0;
}
.waterfall-panel {
  flex: 1;
  width: 100%;
  position: relative;
  overflow-x: auto;
}

/* === Drawer (overlay) === */
.detail-drawer {
  position: fixed;
  right: 0;
  top: 0;
  bottom: 0;
  width: 900px;
  max-width: 90vw;
  z-index: 100;
  border-left: 1px solid var(--border-strong);
  background: var(--bg-primary);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  animation: slideIn 0.3s ease;
  box-shadow: -4px 0 24px rgba(0,0,0,0.3);
}

/* Backdrop */
.drawer-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.3);
  z-index: 99;
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
  color: var(--text-secondary);
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

@media (max-width: 600px) {
  .detail-drawer {
    width: 100vw;
    max-width: 100vw;
  }
}

/* === Log items (rendered inside the insight overlay) === */
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
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 14px;
  padding: 0 4px;
}
.filter-clear:hover { color: var(--status-error-accent); }

.log-list-inline { }
.log-item {
  border-bottom: 1px solid var(--bg-surface-deep);
  padding: 8px 12px;
  font-size: 12px;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.log-item .severity-badge { margin-right: 10px; }
.log-item-time {
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
  margin-right: 10px;
}
.log-item-event {
  color: var(--text-secondary);
  font-family: 'Courier New', monospace;
  font-size: 11px;
  margin-right: 10px;
}

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
.severity-badge.debug { background: var(--bg-surface-hover); color: var(--text-secondary); }

.log-item-body {
  color: var(--text-primary);
  font-family: 'Courier New', monospace;
  white-space: pre-wrap;
}

.log-pagination {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  border-top: 1px solid var(--border-default);
  font-size: 12px;
  color: var(--text-secondary);
}
.page-btn {
  background: none;
  border: 1px solid var(--border-group);
  border-radius: 4px;
  color: var(--text-secondary);
  padding: 3px 10px;
  font-size: 12px;
  cursor: pointer;
}
.page-btn:hover:not(:disabled) {
  border-color: var(--accent-blue);
  color: var(--accent-blue);
}
.page-btn:disabled {
  opacity: 0.4;
  cursor: default;
}
.page-info { color: var(--text-primary); }
.page-total { margin-left: auto; }

.loading-state, .empty-state { text-align: center; padding: 24px; color: var(--text-secondary); font-size: 13px; }

/* --- Drawer view toggle --- */
.drawer-view-toggle {
  display: flex;
  gap: 0;
  margin-left: auto;
  margin-right: 12px;
  flex-shrink: 0;
}
.view-toggle-btn {
  padding: 4px 10px;
  border: 1px solid var(--border-group);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 11px;
  cursor: pointer;
}
.content-search {
  padding: 4px 10px;
  border: 1px solid var(--border-group);
  border-radius: 4px;
  background: var(--bg-surface-deep);
  color: var(--text-primary);
  font-size: 11px;
  width: 160px;
  margin-right: 4px;
}
.content-search::placeholder { color: var(--text-muted); }
.content-search:focus { outline: none; border-color: var(--accent-blue); }

.view-toggle-btn:nth-child(2) {
  border-radius: 4px 0 0 4px;
}
.view-toggle-btn:last-child {
  border-radius: 0 4px 4px 0;
}
.view-toggle-btn.active {
  background: var(--accent-blue);
  color: #fff;
  border-color: var(--accent-blue);
}
.view-toggle-btn:not(.active):hover {
  border-color: var(--accent-blue);
  color: var(--accent-blue);
}

/* --- JSON preview --- */
.json-preview {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.json-toolbar {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
  flex-shrink: 0;
}
.json-copy-btn {
  padding: 5px 12px;
  border: 1px solid var(--border-group);
  border-radius: 4px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
}
.json-copy-btn:hover {
  border-color: var(--accent-blue);
  color: var(--accent-blue);
}
.json-search {
  flex: 1;
  padding: 5px 10px;
  border: 1px solid var(--border-group);
  border-radius: 4px;
  background: var(--bg-surface-deep);
  color: var(--text-primary);
  font-size: 12px;
}
.json-search::placeholder {
  color: var(--text-muted);
}
.json-search:focus {
  outline: none;
  border-color: var(--accent-blue);
}
.json-content {
  flex: 1;
  margin: 0;
  padding: 12px;
  background: var(--bg-surface-deep);
  border: 1px solid var(--border-group);
  border-radius: 6px;
  font-family: 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-primary);
  white-space: pre;
  overflow: auto;
  max-height: calc(100vh - 360px);
}
.json-content::-webkit-scrollbar { width: 4px; height: 4px; }
.json-content::-webkit-scrollbar-track { background: transparent; }
.json-content::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 2px; }

/* JSON syntax highlighting */
.json-content :deep(.j-key) { color: var(--text-secondary); }
.json-content :deep(.j-str) { color: var(--token-green); }
.json-content :deep(.j-num) { color: var(--status-warning); }
.json-content :deep(.j-bool) { color: var(--chart-pie-assistant); }
.json-content :deep(.json-search-hit) {
  background: var(--status-warning);
  color: #000;
  border-radius: 2px;
}
</style>
