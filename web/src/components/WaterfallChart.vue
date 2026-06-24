<template>
  <div class="waterfall">
    <div class="waterfall-toolbar">
      <div class="toolbar-left">
        <input
          class="search-input"
          type="text"
          placeholder="Search spans..."
          v-model="searchQuery"
        />
        <span v-if="searchQuery" class="search-count">{{ matchCount }}/{{ spans.length }}</span>
      </div>
      <div class="toolbar-filters">
        <button
          :class="['filter-btn', { active: activeFilters.size === 0 }]"
          @click="clearFilters"
        >All</button>
        <button
          v-for="opt in FILTER_OPTIONS"
          :key="opt.key"
          :class="['filter-btn', { active: activeFilters.has(opt.key) }]"
          @click="toggleFilter(opt.key)"
        >{{ opt.label }}</button>
      </div>
      <div class="toolbar-actions">
        <button class="action-btn" @click="toggleExpandCollapse" :title="allExpanded ? 'Collapse All' : 'Expand All'">{{ allExpanded ? '⇥' : '⇤' }}</button>
      </div>
    </div>
    <div class="waterfall-stats">
      <span>共 {{ statsCounts.total }} spans</span>
      <span class="stats-sep">·</span>
      <span :class="['stats-link', { active: activeFilters.has('llm') }]" @click="toggleFilter('llm')">LLM {{ statsCounts.llm }}</span>
      <span class="stats-sep">·</span>
      <span :class="['stats-link', { active: activeFilters.has('error') }]" @click="toggleFilter('error')">Error {{ statsCounts.error }}</span>
      <span class="stats-sep">·</span>
      <span :class="['stats-link', { active: activeFilters.has('tool') }]" @click="toggleFilter('tool')">Tool {{ statsCounts.tool }}</span>
      <span class="stats-sep">·</span>
      <span>最长 span: {{ statsCounts.maxDurationName }} ({{ formatDuration(statsCounts.maxDurationMs) }})</span>
    </div>
    <div class="waterfall-header">
      <span class="col-name">Name</span>
      <span class="col-timeline">Timeline</span>
      <span class="col-duration">Duration</span>
      <span class="col-children"></span>
      <span class="col-tokens">Tokens</span>
    </div>

    <div
      v-for="span in displaySpans"
      :key="span.span_id"
      :class="[
        'waterfall-row',
        {
          selected: selectedSpanId === span.span_id,
          'search-match': span._searchMatch && searchQuery,
          'filter-dimmed': activeFilters.size > 0 && !span._filterMatch,
        }
      ]"
      @click="$emit('select-span', span)"
    >
      <span class="col-name" :style="{ paddingLeft: (span._depth * 20 + 8) + 'px' }">
        <span
          v-if="span._hasChildren"
          class="toggle-icon"
          @click.stop="toggleExpand(span.span_id)"
        >{{ span._expanded ? '▼' : '▶' }}</span>
        <span v-else class="toggle-icon toggle-placeholder"></span>
        <span :class="['kind-dot', kindDotClass(span.kind)]"></span>
        <span :class="{ 'match-text': span._searchMatch && searchQuery }">{{ span.name }}</span>
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
      <span class="col-children">{{ span._hasCollapsedChildren ? `[${span._childCount}]` : '' }}</span>
      <span class="col-tokens">
        <span v-if="span.total_tokens" class="token-badge">🎯 {{ formatTokens(span.total_tokens) }}</span>
        <span v-if="logCounts?.[span.span_id]" class="log-badge" @click.stop="$emit('filter-logs', span.span_id)" :title="`${logCounts[span.span_id]} log(s)`">
          📋 {{ logCounts[span.span_id] }}
        </span>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { SpanDetail } from '../api/client'

const props = defineProps<{
  spans: SpanDetail[]
  traceStartMs: number
  traceDurationMs: number
  selectedSpanId?: string
  logCounts?: Record<string, number>
}>()

defineEmits<{
  'select-span': [span: SpanDetail]
  'filter-logs': [spanId: string]
}>()

// --- Collapse state ---
const collapsedParents = ref<Set<string>>(new Set())
const previousSpans = ref<SpanDetail[] | null>(null)
const DEFAULT_EXPAND_DEPTH = 1

// --- Search & Filter ---
const searchQuery = ref('')
const activeFilters = ref<Set<string>>(new Set())
const FILTER_OPTIONS = [
  { key: 'llm', label: 'LLM' },
  { key: 'error', label: 'Error' },
  { key: 'tool', label: 'Tool' },
] as const

function matchesSearch(span: SpanDetail): boolean {
  if (!searchQuery.value) return true
  const q = searchQuery.value.toLowerCase()
  // Match against span name.
  if (span.name.toLowerCase().includes(q)) return true
  // Match against attribute keys and values.
  if (span.attributes) {
    for (const [key, val] of Object.entries(span.attributes)) {
      if (key.toLowerCase().includes(q)) return true
      if (typeof val === 'string' && val.toLowerCase().includes(q)) return true
    }
  }
  // Match against span ID.
  if (span.span_id.toLowerCase().includes(q)) return true
  return false
}

function matchesFilters(span: SpanDetail): boolean {
  if (activeFilters.value.size === 0) return true
  let match = false
  for (const f of activeFilters.value) {
    switch (f) {
      case 'llm':
        if (span.total_tokens && span.total_tokens > 0) match = true
        break
      case 'error':
        if (span.status === 'ERROR') match = true
        break
      case 'tool':
        if (span.kind === 'CLIENT' && span.name.toLowerCase().includes('tool')) match = true
        break
    }
  }
  return match
}

function toggleFilter(key: string) {
  const newFilters = new Set(activeFilters.value)
  if (newFilters.has(key)) {
    newFilters.delete(key)
  } else {
    newFilters.add(key)
  }
  activeFilters.value = newFilters
}

function clearFilters() {
  activeFilters.value = new Set()
}

interface DisplaySpan extends SpanDetail {
  _depth: number
  _hasChildren: boolean
  _expanded: boolean
  _childCount: number
  _hasCollapsedChildren: boolean
  _searchMatch: boolean
  _filterMatch: boolean
}

// A span is treated as a root when its parent is empty OR not present in the
// current span set. This keeps in-progress/incomplete traces visible: when the
// root span hasn't been exported yet, every received child span points at a
// missing parent and would otherwise be unreachable from the tree root,
// leaving the waterfall empty.
const knownSpanIds = computed(() => {
  const ids = new Set<string>()
  for (const span of props.spans) ids.add(span.span_id)
  return ids
})

function parentKeyOf(span: SpanDetail): string {
  const p = span.parent_span_id
  if (!p || !knownSpanIds.value.has(p)) return '__root__'
  return p
}

const displaySpans = computed(() => {
  // --- Build childrenMap and childCountMap (once, reused below) ---
  const childrenMap = new Map<string, SpanDetail[]>()
  const childCountMap = new Map<string, number>()
  for (const span of props.spans) {
    const parentKey = parentKeyOf(span)
    if (!childrenMap.has(parentKey)) childrenMap.set(parentKey, [])
    childrenMap.get(parentKey)!.push(span)
    childCountMap.set(parentKey, (childCountMap.get(parentKey) || 0) + 1)
  }

  // --- First load: init collapsed state ---
  if (previousSpans.value !== props.spans) {
    collapsedParents.value = new Set()
    function markCollapsed(parentId: string, depth: number) {
      const children = childrenMap.get(parentId) || []
      for (const span of children) {
        const hasKids = (childrenMap.get(span.span_id)?.length ?? 0) > 0
        if (hasKids && depth >= DEFAULT_EXPAND_DEPTH) {
          collapsedParents.value.add(span.span_id)
        }
        markCollapsed(span.span_id, depth + 1)
      }
    }
    markCollapsed('__root__', 0)
    previousSpans.value = props.spans
  }

  // --- Walk tree, honor collapsed state ---
  const result: DisplaySpan[] = []
  function walk(parentId: string, depth: number) {
    const children = childrenMap.get(parentId) || []
    for (const span of children) {
      const hasChildren = (childrenMap.get(span.span_id)?.length ?? 0) > 0
      const childCount = childCountMap.get(span.span_id) || 0
      const isCollapsed = collapsedParents.value.has(span.span_id)
      result.push({
        ...span,
        _depth: depth,
        _hasChildren: hasChildren,
        _expanded: !isCollapsed,
        _childCount: childCount,
        _hasCollapsedChildren: hasChildren && isCollapsed,
        _searchMatch: searchQuery.value ? matchesSearch(span) : true,
        _filterMatch: activeFilters.value.size > 0 ? matchesFilters(span) : true,
      })
      if (hasChildren && !isCollapsed) {
        walk(span.span_id, depth + 1)
      }
    }
  }
  walk('__root__', 0)
  return result
})

const matchCount = computed(() => {
  let count = 0
  for (const span of props.spans) {
    if (matchesSearch(span) && matchesFilters(span)) count++
  }
  return count
})

const statsCounts = computed(() => {
  let llm = 0, error = 0, tool = 0
  let maxDurationSpan: SpanDetail | null = null
  for (const span of props.spans) {
    if (span.total_tokens && span.total_tokens > 0) llm++
    if (span.status === 'ERROR') error++
    if (span.kind === 'CLIENT' && span.name.toLowerCase().includes('tool')) tool++
    if (!maxDurationSpan || span.duration_ms > maxDurationSpan.duration_ms) {
      maxDurationSpan = span
    }
  }
  return {
    llm, error, tool, total: props.spans.length,
    maxDurationName: maxDurationSpan?.name || '-',
    maxDurationMs: maxDurationSpan?.duration_ms || 0,
  }
})

function toggleExpand(spanId: string) {
  if (collapsedParents.value.has(spanId)) {
    collapsedParents.value.delete(spanId)
  } else {
    collapsedParents.value.add(spanId)
  }
}

function expandAncestors(spanId: string) {
  // Build parent map from props.spans
  const parentMap = new Map<string, string>()
  for (const span of props.spans) {
    if (span.parent_span_id) {
      parentMap.set(span.span_id, span.parent_span_id)
    }
  }
  // Walk up from spanId to root, expanding all ancestors
  let current: string | undefined = spanId
  while (current && parentMap.has(current)) {
    const parentId: string = parentMap.get(current)!
    collapsedParents.value.delete(parentId)
    current = parentId
  }
  // Trigger reactive update
  collapsedParents.value = new Set(collapsedParents.value)
}

watch(searchQuery, (newVal) => {
  if (!newVal) return
  for (const span of props.spans) {
    if (matchesSearch(span)) {
      expandAncestors(span.span_id)
    }
  }
})

const allExpanded = computed(() => collapsedParents.value.size === 0)

function toggleExpandCollapse() {
  if (allExpanded.value) {
    collapseAll()
  } else {
    expandAll()
  }
}

function expandAll() {
  collapsedParents.value = new Set()
}

function collapseAll() {
  const newSet = new Set<string>()
  // Collect all parent spans at depth >= 1
  const childrenMap = new Map<string, SpanDetail[]>()
  for (const span of props.spans) {
    const pk = parentKeyOf(span)
    if (!childrenMap.has(pk)) childrenMap.set(pk, [])
    childrenMap.get(pk)!.push(span)
  }
  function collect(parentId: string, depth: number) {
    const children = childrenMap.get(parentId) || []
    for (const span of children) {
      const hasKids = (childrenMap.get(span.span_id)?.length ?? 0) > 0
      if (hasKids && depth >= 1) {
        newSet.add(span.span_id)
      }
      collect(span.span_id, depth + 1)
    }
  }
  collect('__root__', 0)
  collapsedParents.value = newSet
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
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(0)}M`
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
.col-children { flex: 0 0 70px; text-align: right; font-size: 11px; color: var(--text-secondary); }
.col-tokens { flex: 0 0 100px; text-align: right; }
.toggle-icon { cursor: pointer; margin-right: 4px; font-size: 10px; color: var(--text-secondary); }
.toggle-placeholder { display: inline-block; width: 14px; }
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
.log-badge {
  margin-left: 6px;
  font-size: 11px;
  cursor: pointer;
  opacity: 0.7;
  padding: 1px 4px;
  border-radius: 3px;
  background: var(--bg-surface-hover);
}
.log-badge:hover { opacity: 1; background: var(--bg-surface-hover-subtle); }
.selected-marker {
  color: var(--accent-blue);
  margin-left: 6px;
  font-size: 10px;
}

/* === Toolbar === */
.waterfall-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border-default);
  flex-wrap: wrap;
}
.toolbar-left {
  display: flex;
  align-items: center;
  gap: 6px;
  order: 2;
}
.search-input {
  padding: 4px 10px;
  background: var(--bg-primary);
  border: 1px solid var(--border-default);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 12px;
  width: 180px;
}
.search-input::placeholder { color: var(--text-muted); }
.search-input:focus {
  outline: none;
  border-color: var(--accent-blue);
}
.search-count {
  font-size: 11px;
  color: var(--text-secondary);
  white-space: nowrap;
}
.toolbar-filters {
  display: flex;
  gap: 4px;
  order: 1;
}
.filter-btn {
  padding: 3px 10px;
  border: 1px solid var(--border-default);
  border-radius: 4px;
  background: var(--bg-primary);
  color: var(--text-secondary);
  font-size: 11px;
  cursor: pointer;
}
.filter-btn:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}
.filter-btn.active {
  background: var(--accent-blue);
  color: #fff;
  border-color: var(--accent-blue);
}
.toolbar-actions {
  display: flex;
  gap: 4px;
  order: 0;
}
.action-btn {
  padding: 3px 8px;
  border: 1px solid var(--border-default);
  border-radius: 4px;
  background: var(--bg-primary);
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
  line-height: 1;
}
.action-btn:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}

/* === Stats bar === */
.waterfall-stats {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  font-size: 11px;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--bg-surface-deep);
}
.stats-sep { color: var(--text-secondary); }
.stats-link {
  cursor: pointer;
  color: var(--accent-blue);
}
.stats-link:hover { text-decoration: underline; }
.stats-link.active {
  font-weight: 600;
  background: rgba(59, 130, 246, 0.1);
  padding: 1px 4px;
  border-radius: 2px;
}

/* === Search & filter highlights === */
.waterfall-row.search-match {
  background: rgba(251, 191, 36, 0.15);
  border-left: 3px solid var(--status-warning);
}
.waterfall-row.search-match:hover {
  background: rgba(251, 191, 36, 0.22);
}
.waterfall-row.filter-dimmed {
  opacity: 0.35;
}
.waterfall-row.filter-dimmed:hover {
  opacity: 0.6;
}
.match-text { font-weight: 700; color: var(--status-error-accent); }
</style>
