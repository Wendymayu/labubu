<template>
  <div class="waterfall">
    <div class="waterfall-header">
      <span class="col-name">Name</span>
      <span class="col-timeline">Timeline</span>
      <span class="col-duration">Duration</span>
      <span class="col-tokens">Tokens</span>
    </div>

    <div
      v-for="span in displaySpans"
      :key="span.span_id"
      :class="['waterfall-row', { selected: selectedSpanId === span.span_id }]"
      @click="$emit('select-span', span)"
    >
      <span class="col-name" :style="{ paddingLeft: (span._depth * 20 + 8) + 'px' }">
        <span
          v-if="span._hasChildren"
          class="toggle-icon"
          @click.stop="toggleExpand(span.span_id)"
        >{{ span._expanded ? '▼' : '▶' }}</span>
        <span :class="['kind-dot', kindDotClass(span.kind)]"></span>
        {{ span.name }}
        <span v-if="selectedSpanId === span.span_id" class="selected-marker">◀</span>
      </span>

      <span class="col-timeline">
        <span
          :class="['bar', kindBarClass(span.kind, span.total_tokens)]"
          :style="barStyle(span)"
          :title="`${span.name}: ${span.duration_ms}ms`"
        ></span>
      </span>

      <span class="col-duration">{{ formatDuration(span.duration_ms) }}</span>
      <span class="col-tokens">
        <span v-if="span.total_tokens" class="token-badge">🎯 {{ formatTokens(span.total_tokens) }}</span>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { SpanDetail } from '../api/client'

const props = defineProps<{
  spans: SpanDetail[]
  traceStartMs: number
  traceDurationMs: number
  selectedSpanId?: string
}>()

defineEmits<{
  'select-span': [span: SpanDetail]
}>()

interface DisplaySpan extends SpanDetail {
  _depth: number
  _hasChildren: boolean
  _expanded: boolean
}

const displaySpans = computed(() => {
  const childrenMap = new Map<string, SpanDetail[]>()

  for (const span of props.spans) {
    const parentKey = span.parent_span_id || '__root__'
    if (!childrenMap.has(parentKey)) {
      childrenMap.set(parentKey, [])
    }
    childrenMap.get(parentKey)!.push(span)
  }

  const result: DisplaySpan[] = []

  function walk(parentId: string, depth: number) {
    const children = childrenMap.get(parentId) || []
    for (const span of children) {
      const hasChildren = childrenMap.has(span.span_id) && (childrenMap.get(span.span_id)?.length ?? 0) > 0
      result.push({
        ...span,
        _depth: depth,
        _hasChildren: hasChildren,
        _expanded: true,
      })
      if (hasChildren) {
        walk(span.span_id, depth + 1)
      }
    }
  }

  walk('__root__', 0)
  return result
})

function toggleExpand(_spanId: string) {
  // Phase 1: all spans expanded by default. Expand/collapse in future iteration.
}

function barStyle(span: DisplaySpan) {
  const offset = ((span.start_time_ms - props.traceStartMs) / props.traceDurationMs) * 100
  const width = (span.duration_ms / props.traceDurationMs) * 100
  return {
    marginLeft: `${offset}%`,
    width: `${Math.max(width, 0.05)}%`,
  }
}

function kindDotClass(kind: string): string {
  switch (kind) {
    case 'SERVER': return 'dot-server'
    case 'CLIENT': return 'dot-client'
    case 'PRODUCER': return 'dot-producer'
    case 'CONSUMER': return 'dot-consumer'
    default: return 'dot-internal'
  }
}

function kindBarClass(kind: string, hasTokens?: number): string {
  if (hasTokens != null && hasTokens > 0) return 'bar-llm'
  switch (kind) {
    case 'SERVER': return 'bar-server'
    case 'CLIENT': return 'bar-client'
    case 'PRODUCER': return 'bar-producer'
    case 'CONSUMER': return 'bar-consumer'
    default: return 'bar-internal'
  }
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms.toFixed(1)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

function formatTokens(tokens: number): string {
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(0)}K`
  return String(tokens)
}
</script>

<style scoped>
.waterfall { font-size: 13px; }
.waterfall-header { display: flex; padding: 8px; font-size: 11px; color: var(--text-secondary); text-transform: uppercase; border-bottom: 1px solid var(--border-default); }
.waterfall-row { display: flex; align-items: center; padding: 4px 0; cursor: pointer; border-bottom: 1px solid var(--bg-surface-deep); }
.waterfall-row:hover { background: var(--bg-surface); }
.waterfall-row.selected {
  background: #1e3a5f;
  outline: 1px solid var(--accent-blue);
  outline-offset: -1px;
}
[data-theme="light"] .waterfall-row.selected {
  background: #dbeafe;
}
.col-name { flex: 0 0 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.col-timeline { flex: 1; position: relative; height: 20px; }
.col-duration { flex: 0 0 80px; text-align: right; font-variant-numeric: tabular-nums; color: var(--text-secondary); }
.col-tokens { flex: 0 0 100px; text-align: right; }
.toggle-icon { cursor: pointer; margin-right: 4px; font-size: 10px; color: var(--text-muted); }
.kind-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; }
.dot-server { background: var(--chart-server); }
.dot-client { background: var(--chart-client); }
.dot-producer { background: var(--chart-producer); }
.dot-consumer { background: var(--chart-consumer); }
.dot-internal { background: var(--chart-internal); }
.bar { display: inline-block; height: 14px; border-radius: 3px; min-width: 2px; vertical-align: middle; }
.bar-server { background: var(--chart-server); }
.bar-client { background: var(--chart-client); }
.bar-producer { background: var(--chart-producer); }
.bar-consumer { background: var(--chart-consumer); }
.bar-internal { background: var(--chart-internal); }
.bar-llm { background: linear-gradient(90deg, var(--chart-llm-start), var(--chart-llm-end)); }
.token-badge { font-size: 11px; color: var(--token-highlight); }
.selected-marker {
  color: var(--accent-blue);
  margin-left: 6px;
  font-size: 10px;
}
</style>
