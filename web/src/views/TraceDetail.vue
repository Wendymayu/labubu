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
          <button class="btn-download" @click="downloadTrace" title="Download trace as JSON">Download</button>
        </div>
      </div>

      <div class="detail-layout" :class="{ 'drawer-open': drawerOpen }">
        <div class="waterfall-panel">
          <WaterfallChart
            :spans="trace.spans"
            :trace-start-ms="trace.start_time_ms"
            :trace-duration-ms="trace.duration_ms"
            :selected-span-id="selectedSpan?.span_id"
            @select-span="openDrawer"
          />
          <div v-if="!drawerOpen" class="hint-click">Click any span to view details</div>
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
import { getTrace, type TraceDetailResponse, type SpanDetail as SpanDetailType } from '../api/client'
import WaterfallChart from '../components/WaterfallChart.vue'
import SpanDetail from '../components/SpanDetail.vue'
import TokenPieChart from '../components/TokenPieChart.vue'
import type { PieSlice } from '../components/TokenPieChart.vue'

const route = useRoute()
const traceIdHex = route.params.id as string

const trace = ref<TraceDetailResponse['trace'] | null>(null)
const loading = ref(true)
const error = ref('')
const selectedSpan = ref<SpanDetailType | null>(null)
const drawerOpen = ref(false)

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

function downloadTrace() {
  if (!trace.value) return
  const json = JSON.stringify(trace.value, null, 2)
  const blob = new Blob([json], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `trace-${traceIdHex}.json`
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
.back-link a { color: #94a3b8; text-decoration: none; font-size: 14px; }
.back-link a:hover { color: #e2e8f0; }
.loading, .error { text-align: center; padding: 60px; color: #94a3b8; }
.error { color: #f87171; }
.trace-summary { margin-bottom: 24px; }
.trace-summary h2 { font-size: 20px; margin-bottom: 12px; }
.summary-grid { display: flex; gap: 24px; flex-wrap: wrap; }
.summary-item { display: flex; flex-direction: column; }
.summary-label { font-size: 11px; color: #94a3b8; text-transform: uppercase; }
.summary-value { font-size: 14px; }
.mono { font-family: 'Courier New', monospace; font-size: 12px; word-break: break-all; }
.token-highlight { color: #c4b5fd; font-weight: 600; }

.btn-download {
  padding: 6px 16px;
  border: 1px solid #333;
  background: #111;
  color: #94a3b8;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
  align-self: center;
}
.btn-download:hover {
  border-color: #38bdf8;
  color: #38bdf8;
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
  color: #64748b;
  font-size: 12px;
  padding: 24px 0;
}

/* === Drawer === */
.detail-drawer {
  width: 480px;
  flex-shrink: 0;
  border-left: 1px solid #444;
  background: #000;
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
  border-bottom: 1px solid #444;
  flex-shrink: 0;
}
.drawer-title {
  min-width: 0;
}
.drawer-span-name {
  display: block;
  font-size: 14px;
  font-weight: 600;
  color: #e2e8f0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.drawer-span-id {
  display: block;
  font-size: 10px;
  color: #64748b;
  font-family: 'Courier New', monospace;
  margin-top: 2px;
}
.drawer-close {
  background: none;
  border: none;
  color: #94a3b8;
  font-size: 18px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
  line-height: 1;
}
.drawer-close:hover {
  color: #e2e8f0;
  background: #222;
}

.drawer-body {
  padding: 16px;
  overflow-y: auto;
  flex: 1;
}

/* Scrollbar for drawer body */
.drawer-body::-webkit-scrollbar { width: 4px; }
.drawer-body::-webkit-scrollbar-track { background: transparent; }
.drawer-body::-webkit-scrollbar-thumb { background: #475569; border-radius: 2px; }

/* Responsive: on narrow screens overlay instead of split */
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
</style>
