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
        </div>
      </div>

      <div class="detail-layout">
        <div class="waterfall-panel">
          <WaterfallChart
            :spans="trace.spans"
            :trace-start-ms="trace.start_time_ms"
            :trace-duration-ms="trace.duration_ms"
            :selected-span-id="selectedSpan?.span_id"
            @select-span="selectSpan"
          />
        </div>
        <div class="detail-panel">
          <SpanDetail :span="selectedSpan" />
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { getTrace, type TraceDetailResponse, type SpanDetail as SpanDetailType } from '../api/client'
import WaterfallChart from '../components/WaterfallChart.vue'
import SpanDetail from '../components/SpanDetail.vue'

const route = useRoute()
const traceIdHex = route.params.id as string

const trace = ref<TraceDetailResponse['trace'] | null>(null)
const loading = ref(true)
const error = ref('')
const selectedSpan = ref<SpanDetailType | null>(null)

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
    if (result.trace.spans.length > 0) {
      const root = result.trace.spans.find(s => s.parent_span_id === '') || result.trace.spans[0]
      selectedSpan.value = root
    }
  } catch (e: any) {
    error.value = e.message || 'Failed to load trace'
  } finally {
    loading.value = false
  }
}

function selectSpan(span: SpanDetailType) {
  selectedSpan.value = span
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

onMounted(fetchTrace)
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
.detail-layout { display: flex; gap: 16px; }
.waterfall-panel { flex: 1; min-width: 0; overflow-x: auto; }
.detail-panel { flex: 0 0 400px; max-height: calc(100vh - 280px); overflow-y: auto; position: sticky; top: 0; }
</style>
