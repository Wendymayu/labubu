<template>
  <div class="span-detail" v-if="span">
    <h3 class="detail-title">Span Detail</h3>

    <table class="detail-table">
      <tr><td class="label">Name</td><td>{{ span.name }}</td></tr>
      <tr><td class="label">Kind</td><td>{{ span.kind }}</td></tr>
      <tr><td class="label">Status</td><td><span :class="['status-badge', statusClass(span.status)]">{{ span.status }}</span></td></tr>
      <tr v-if="span.status_message"><td class="label">Status Message</td><td class="error-text">{{ span.status_message }}</td></tr>
      <tr><td class="label">Duration</td><td>{{ formatDuration(span.duration_ms) }}</td></tr>
      <tr v-if="span.gen_ai_request_model"><td class="label">Model</td><td>{{ span.gen_ai_request_model }}</td></tr>
    </table>

    <!-- Token breakdown for LLM spans -->
    <div v-if="span.total_tokens" class="token-section">
      <h4>Token Usage</h4>
      <div class="token-grid">
        <div class="token-item">
          <div class="token-value">{{ span.input_tokens ?? '-' }}</div>
          <div class="token-label">Input</div>
        </div>
        <div class="token-item">
          <div class="token-value">{{ span.output_tokens ?? '-' }}</div>
          <div class="token-label">Output</div>
        </div>
        <div class="token-item">
          <div class="token-value">{{ span.total_tokens }}</div>
          <div class="token-label">Total</div>
        </div>
      </div>
    </div>

    <!-- Attributes -->
    <div v-if="Object.keys(span.attributes || {}).length > 0" class="detail-section">
      <h4>Attributes</h4>
      <table class="kv-table">
        <tr v-for="(v, k) in span.attributes" :key="k">
          <td class="kv-key">{{ k }}</td>
          <td class="kv-value">{{ v }}</td>
        </tr>
      </table>
    </div>

    <!-- Events -->
    <div v-if="span.events && span.events.length > 0" class="detail-section">
      <h4>Events ({{ span.events.length }})</h4>
      <div v-for="(evt, i) in span.events" :key="i" class="event-item">
        <div class="event-name">{{ evt.name }}</div>
        <div class="event-time">at {{ formatDurationFromStart(evt.time_ms) }}</div>
        <table class="kv-table" v-if="Object.keys(evt.attributes || {}).length > 0">
          <tr v-for="(v, k) in evt.attributes" :key="k">
            <td class="kv-key">{{ k }}</td>
            <td class="kv-value">{{ v }}</td>
          </tr>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { SpanDetail } from '../api/client'

defineProps<{
  span: SpanDetail | null
}>()

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms.toFixed(1)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

function formatDurationFromStart(ms: number): string {
  if (ms < 1000) return `+${ms}ms`
  return `+${(ms / 1000).toFixed(2)}s`
}

function statusClass(status: string): string {
  switch (status) {
    case 'OK': return 'status-ok'
    case 'ERROR': return 'status-error'
    default: return ''
  }
}
</script>

<style scoped>
.span-detail { background: #1e293b; border: 1px solid #334155; border-radius: 8px; padding: 16px; }
.detail-title { font-size: 16px; margin-bottom: 12px; color: #e2e8f0; }
.detail-table { width: 100%; border-collapse: collapse; }
.detail-table td { padding: 4px 8px; font-size: 13px; }
.label { color: #94a3b8; width: 120px; white-space: nowrap; }
.status-badge { display: inline-block; padding: 1px 6px; border-radius: 3px; font-size: 12px; font-weight: 600; }
.status-ok { background: #065f46; color: #6ee7b7; }
.status-error { background: #7f1d1d; color: #fca5a5; }
.error-text { color: #fca5a5; }
.detail-section { margin-top: 16px; }
.detail-section h4 { font-size: 13px; color: #94a3b8; margin-bottom: 8px; text-transform: uppercase; }
.kv-table { width: 100%; border-collapse: collapse; }
.kv-table td { padding: 3px 6px; font-size: 12px; border-bottom: 1px solid #0f172a; }
.kv-key { color: #94a3b8; width: 180px; word-break: break-all; }
.kv-value { color: #e2e8f0; word-break: break-all; }
.token-section { margin-top: 16px; }
.token-section h4 { font-size: 13px; color: #94a3b8; margin-bottom: 8px; text-transform: uppercase; }
.token-grid { display: flex; gap: 16px; }
.token-item { text-align: center; }
.token-value { font-size: 20px; font-weight: 700; color: #c4b5fd; }
.token-label { font-size: 11px; color: #94a3b8; }
.event-item { margin-top: 8px; padding: 8px; background: #0f172a; border-radius: 4px; }
.event-name { font-weight: 600; font-size: 13px; }
.event-time { font-size: 11px; color: #94a3b8; margin-bottom: 4px; }
</style>
