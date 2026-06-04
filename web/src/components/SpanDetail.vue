<template>
  <div class="span-detail" v-if="span">
    <!-- Quick Info Grid -->
    <div class="quick-info">
      <div class="qi-item">
        <div class="qi-label">Kind</div>
        <div :class="['qi-value', kindClass(span.kind)]">{{ span.kind }}</div>
      </div>
      <div class="qi-item">
        <div class="qi-label">Status</div>
        <div :class="['qi-value', statusClass(span.status)]">{{ span.status }}</div>
      </div>
      <div class="qi-item">
        <div class="qi-label">Duration</div>
        <div class="qi-value">{{ formatDuration(span.duration_ms) }}</div>
      </div>
      <div class="qi-item">
        <div class="qi-label">Model</div>
        <div class="qi-value qi-model">{{ span.gen_ai_request_model || '-' }}</div>
      </div>
    </div>

    <!-- Status message (error) -->
    <div v-if="span.status_message" class="status-msg">{{ span.status_message }}</div>

    <!-- Token summary line (compact) -->
    <div v-if="span.total_tokens" class="token-summary">
      <div class="ts-item">
        <span class="ts-label">Input</span>
        <span class="ts-val">{{ formatTokens(span.input_tokens) }}</span>
      </div>
      <div class="ts-item">
        <span class="ts-label">Output</span>
        <span class="ts-val">{{ formatTokens(span.output_tokens) }}</span>
      </div>
      <div class="ts-item">
        <span class="ts-label">Total</span>
        <span class="ts-val ts-highlight">{{ formatTokens(span.total_tokens) }}</span>
      </div>
    </div>

    <!-- Attributes (grouped + search) -->
    <div class="detail-section">
      <div class="section-header">
        <h4>Attributes ({{ totalAttrCount }})</h4>
        <input
          v-model="attrFilter"
          class="attr-search"
          placeholder="Filter attributes..."
        />
      </div>

      <div v-if="groupedAttributes.length === 0" class="attr-empty">
        {{ totalAttrCount === 0 ? 'No attributes' : 'No matching attributes' }}
      </div>

      <div v-for="(group, gi) in groupedAttributes" :key="gi" class="attr-group">
        <div class="attr-group-header" @click="toggleGroupExpand(group.name)">
          <span>{{ isGroupExpanded(group.name) ? '▾' : '▸' }} <b>{{ group.name }}</b></span>
          <span class="attr-group-count">{{ group.items.length }}</span>
        </div>
        <div v-if="isGroupExpanded(group.name)" class="attr-group-body">
          <div v-for="item in group.items" :key="item.key" class="attr-row">
            <span class="attr-key">{{ item.key }}</span>
            <span class="attr-value" :class="{ 'attr-empty-val': !item.value }">{{ item.value || '(empty)' }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Events Timeline -->
    <div v-if="span.events && span.events.length > 0" class="detail-section">
      <h4>Events ({{ span.events.length }})</h4>

      <div class="events-timeline">
        <div class="tl-line"></div>
        <div
          v-for="(evt, i) in span.events"
          :key="i"
          :class="['tl-card', eventBorderClass(evt)]"
        >
          <div :class="['tl-dot', eventDotClass(evt)]"></div>
          <div class="tl-header">
            <b :class="eventNameClass(evt)">{{ evt.name }}</b>
            <span class="tl-time">{{ formatTimeOffset(evt.time_ms, span.start_time_ms) }}</span>
          </div>
          <div v-if="Object.keys(evt.attributes || {}).length > 0" class="tl-attrs">
            <template v-for="(v, k) in evt.attributes" :key="k">
              <div class="tl-attr-row" v-if="isToolIO(k)">
                <div class="tl-code-toggle" @click="toggleCodeBlock(evt, k, i)">
                  {{ codeBlockState(evt, k, i).expanded ? '▾' : '▸' }} {{ k }}
                  <span class="tl-copy-inline" @click.stop="copyText(v)">📋</span>
                </div>
                <pre v-if="codeBlockState(evt, k, i).expanded" class="tl-code"><code v-html="highlightJSON(v)"></code></pre>
              </div>
              <div v-else class="tl-attr-row">
                <span class="tl-attr-key">{{ k }}</span>
                <span class="tl-attr-value">{{ v }}</span>
              </div>
            </template>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, watch } from 'vue'
import type { SpanDetail } from '../api/client'

const props = defineProps<{
  span: SpanDetail | null
}>()

// --- grouping rules ---

interface AttrGroup {
  name: string
  prefixes: string[]
  defaultExpanded: boolean
  items: { key: string; value: string }[]
}

const GROUP_RULES: Omit<AttrGroup, 'items'>[] = [
  { name: 'Gen AI', prefixes: ['gen_ai.'], defaultExpanded: true },
  { name: 'HTTP', prefixes: ['http.', 'url.', 'net.'], defaultExpanded: false },
  { name: 'Service', prefixes: ['service.', 'telemetry.'], defaultExpanded: false },
]

// --- attribute search ---

const attrFilter = ref('')
const groupExpanded = reactive<Record<string, boolean>>({
  'Gen AI': true,  // expanded by default per spec
})

const totalAttrCount = computed(() => {
  if (!props.span?.attributes) return 0
  return Object.keys(props.span.attributes).length
})

const groupedAttributes = computed<AttrGroup[]>(() => {
  const attrs = props.span?.attributes || {}
  const filter = attrFilter.value.toLowerCase()

  // Initialize groups
  const groups: AttrGroup[] = GROUP_RULES.map(r => ({
    ...r,
    items: [],
  }))
  const otherGroup: AttrGroup = {
    name: 'Other',
    prefixes: [],
    defaultExpanded: false,
    items: [],
  }

  for (const [key, value] of Object.entries(attrs)) {
    // Filter
    if (filter && !key.toLowerCase().includes(filter) && !String(value).toLowerCase().includes(filter)) {
      continue
    }

    let assigned = false
    for (const g of groups) {
      if (g.prefixes.some(pfx => key.startsWith(pfx))) {
        g.items.push({ key, value: String(value) })
        assigned = true
        break
      }
    }
    if (!assigned) {
      otherGroup.items.push({ key, value: String(value) })
    }
  }

  // When filtering, auto-expand all groups that have matches
  if (filter) {
    for (const g of groups) {
      if (g.items.length > 0) groupExpanded[g.name] = true
    }
    if (otherGroup.items.length > 0) groupExpanded[otherGroup.name] = true
  }

  // Return non-empty groups; if filter is active include all (even empty) to show "no matching"
  if (filter) {
    // When filtering, only show groups that have items
    const nonEmpty = groups.filter(g => g.items.length > 0)
    if (otherGroup.items.length > 0) nonEmpty.push(otherGroup)
    return nonEmpty
  }

  const result = groups.filter(g => g.items.length > 0)
  if (otherGroup.items.length > 0) result.push(otherGroup)
  return result
})

function isGroupExpanded(name: string): boolean {
  if (name in groupExpanded) return groupExpanded[name]
  // Other group defaults to collapsed
  return name === 'Gen AI'
}
function toggleGroupExpand(name: string) {
  groupExpanded[name] = !isGroupExpanded(name)
}

// --- code block expand/collapse state ---

const codeBlocks = reactive<Record<string, Record<string, boolean>>>({})

// Reset code block expand state when span changes
watch(() => props.span, () => {
  Object.keys(codeBlocks).forEach(k => delete codeBlocks[k])
})

function getBlockKey(evt: any, k: string, idx: number): string {
  return `${idx}_${evt.name || ''}_${k}`
}

function codeBlockState(evt: any, k: string, idx: number): { expanded: boolean } {
  const evtKey = getBlockKey(evt, k, idx)
  if (codeBlocks[evtKey]) {
    return { expanded: codeBlocks[evtKey].expanded }
  }
  // Return default (expanded for short content) without mutating during render
  const raw = String(evt.attributes[k] || '')
  const lines = raw.split('\n')
  return { expanded: lines.length <= 3 }
}

function toggleCodeBlock(evt: any, k: string, idx: number) {
  const evtKey = getBlockKey(evt, k, idx)
  if (!codeBlocks[evtKey]) {
    const raw = String(evt.attributes[k] || '')
    const lines = raw.split('\n')
    codeBlocks[evtKey] = { expanded: lines.length <= 3 }
  }
  codeBlocks[evtKey].expanded = !codeBlocks[evtKey].expanded
}

// --- tool I/O detection ---

function isToolIO(attrKey: string): boolean {
  const lower = attrKey.toLowerCase()
  return lower === 'input' || lower === 'output' || lower === 'result' ||
    lower.endsWith('.input') || lower.endsWith('.output') || lower.endsWith('.result')
}

function highlightJSON(raw: string): string {
  try {
    const parsed = JSON.parse(raw)
    const pretty = JSON.stringify(parsed, null, 2)
    // Basic syntax highlighting
    return pretty
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"([^"]+)":/g, '<span class="j-key">"$1"</span>:')
      .replace(/: "([^"]*)"/g, ': <span class="j-str">"$1"</span>')
      .replace(/: (\d+\.?\d*)/g, ': <span class="j-num">$1</span>')
      .replace(/: (true|false|null)/g, ': <span class="j-bool">$1</span>')
  } catch {
    return raw
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
  }
}

function copyText(text: string) {
  navigator.clipboard?.writeText(text).catch(() => {})
}

// --- event helpers ---

type EventType = 'toolcall' | 'toolresult' | 'error' | 'default'

function getEventType(name: string): EventType {
  const lower = (name || '').toLowerCase()
  if (lower.includes('tool.call') && !lower.includes('tool.call.')) return 'toolcall'
  if (lower.includes('tool.result')) return 'toolresult'
  if (lower.includes('exception') || lower.includes('error')) return 'error'
  return 'default'
}

function eventBorderClass(evt: any): string {
  const t = getEventType(evt.name)
  return t === 'default' ? 'tl-card-default' : `tl-card-${t}`
}
function eventDotClass(evt: any): string {
  const t = getEventType(evt.name)
  return t === 'default' ? 'tl-dot-default' : `tl-dot-${t}`
}
function eventNameClass(evt: any): string {
  const t = getEventType(evt.name)
  if (t === 'default') return ''
  return `evt-name-${t}`
}

function formatTimeOffset(eventTimeMs: number, spanStartMs?: number): string {
  if (eventTimeMs == null) return '-'
  if (spanStartMs == null || spanStartMs === 0) return `${eventTimeMs}ms`
  const offset = eventTimeMs - spanStartMs
  if (offset < 0) return `+0ms`
  if (offset < 1000) return `+${offset}ms`
  return `+${(offset / 1000).toFixed(1)}s`
}

// --- general helpers ---

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms.toFixed(1)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

function formatTokens(tokens: number | undefined | null): string {
  if (tokens == null) return '-'
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return String(tokens)
}

function kindClass(kind: string): string {
  switch (kind) {
    case 'SERVER': return 'kind-server'
    case 'CLIENT': return 'kind-client'
    case 'PRODUCER': return 'kind-producer'
    case 'CONSUMER': return 'kind-consumer'
    default: return 'kind-internal'
  }
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
.span-detail {
  background: #000;
  padding: 0;
}

/* --- Quick Info Grid --- */
.quick-info {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
  margin-bottom: 12px;
}
.qi-item {
  background: #0f172a;
  border-radius: 6px;
  padding: 10px 8px;
  text-align: center;
}
.qi-label {
  font-size: 10px;
  color: #64748b;
  text-transform: uppercase;
  margin-bottom: 4px;
}
.qi-value {
  font-size: 13px;
  font-weight: 600;
  color: #e2e8f0;
}
.qi-model {
  font-size: 11px;
  color: #38bdf8;
  word-break: break-all;
}
.kind-server { color: #3b82f6; }
.kind-client { color: #22c55e; }
.kind-producer { color: #f59e0b; }
.kind-consumer { color: #a855f7; }
.kind-internal { color: #94a3b8; }
.status-ok { color: #6ee7b7; }
.status-error { color: #fca5a5; }

.status-msg {
  background: #7f1d1d;
  color: #fca5a5;
  padding: 8px 12px;
  border-radius: 4px;
  font-size: 12px;
  margin-bottom: 12px;
}

/* --- Token Summary --- */
.token-summary {
  display: flex;
  gap: 16px;
  margin-bottom: 12px;
  padding: 8px 0;
  border-bottom: 1px solid #222;
}
.ts-item {
  text-align: center;
  flex: 1;
}
.ts-label {
  display: block;
  font-size: 10px;
  color: #64748b;
  text-transform: uppercase;
}
.ts-val {
  font-size: 16px;
  font-weight: 700;
  color: #e2e8f0;
}
.ts-highlight {
  color: #c4b5fd;
}

/* --- Attributes --- */
.detail-section {
  margin-bottom: 16px;
}
.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}
.section-header h4 {
  font-size: 11px;
  color: #94a3b8;
  text-transform: uppercase;
  margin: 0;
}
.attr-search {
  background: #0f172a;
  border: 1px solid #333;
  border-radius: 4px;
  padding: 4px 10px;
  font-size: 11px;
  color: #e2e8f0;
  width: 170px;
}
.attr-search::placeholder {
  color: #64748b;
}
.attr-search:focus {
  outline: none;
  border-color: #38bdf8;
}
.attr-empty {
  text-align: center;
  color: #64748b;
  font-size: 12px;
  padding: 12px 0;
}

.attr-group {
  border: 1px solid #222;
  border-radius: 4px;
  overflow: hidden;
  margin-bottom: 4px;
}
.attr-group-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 10px;
  background: #111;
  cursor: pointer;
  font-size: 12px;
  color: #e2e8f0;
  user-select: none;
}
.attr-group-header:hover {
  background: #1a1a1a;
}
.attr-group-count {
  font-size: 10px;
  color: #64748b;
}
.attr-group-body {
  padding: 2px 0;
}
.attr-row {
  display: flex;
  padding: 3px 10px;
  border-bottom: 1px solid #0f172a;
  font-size: 11px;
}
.attr-row:last-child {
  border-bottom: none;
}
.attr-key {
  color: #64748b;
  width: 170px;
  flex-shrink: 0;
  word-break: break-all;
}
.attr-value {
  color: #e2e8f0;
  word-break: break-all;
  flex: 1;
}
.attr-empty-val {
  color: #64748b;
  font-style: italic;
}

/* --- Events Timeline --- */
.events-timeline {
  position: relative;
  padding-left: 18px;
}
.tl-line {
  position: absolute;
  left: 6px;
  top: 6px;
  bottom: 6px;
  width: 2px;
  background: #222;
}

.tl-card {
  position: relative;
  background: #0f172a;
  border-radius: 4px;
  padding: 8px 10px;
  margin-bottom: 8px;
  border-left: 3px solid #6b7280;
}
.tl-card-toolcall { border-left-color: #22c55e; }
.tl-card-toolresult { border-left-color: #f59e0b; }
.tl-card-error { border-left-color: #ef4444; }
.tl-card-default { border-left-color: #6b7280; }

.tl-dot {
  position: absolute;
  left: -15px;
  top: 10px;
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
.tl-dot-toolcall { background: #22c55e; }
.tl-dot-toolresult { background: #f59e0b; }
.tl-dot-error { background: #ef4444; }
.tl-dot-default { background: #6b7280; }

.tl-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
}
.tl-time {
  font-size: 10px;
  color: #64748b;
  font-variant-numeric: tabular-nums;
}
.evt-name-toolcall { color: #22c55e; }
.evt-name-toolresult { color: #f59e0b; }
.evt-name-error { color: #fca5a5; }

.tl-attrs {
  margin-top: 6px;
}
.tl-attr-row {
  margin-bottom: 4px;
}
.tl-attr-key {
  display: block;
  font-size: 10px;
  color: #64748b;
  margin-bottom: 2px;
}
.tl-attr-value {
  font-size: 11px;
  color: #e2e8f0;
  word-break: break-all;
}

.tl-code-toggle {
  font-size: 10px;
  color: #94a3b8;
  cursor: pointer;
  margin-bottom: 2px;
}
.tl-code-toggle:hover {
  color: #e2e8f0;
}
.tl-copy-inline {
  margin-left: 8px;
  cursor: pointer;
  font-size: 10px;
}
.tl-code {
  background: #000;
  border-radius: 3px;
  padding: 6px 8px;
  font-size: 10px;
  overflow-x: auto;
  max-height: 250px;
  overflow-y: auto;
  margin: 0;
  font-family: 'Courier New', monospace;
  color: #e2e8f0;
  line-height: 1.5;
}
.tl-code code {
  font-family: inherit;
}
.tl-code :deep(.j-key) { color: #94a3b8; }
.tl-code :deep(.j-str) { color: #6ee7b7; }
.tl-code :deep(.j-num) { color: #facc15; }
.tl-code :deep(.j-bool) { color: #f472b6; }

/* --- Scrollbar --- */
.tl-code::-webkit-scrollbar { width: 3px; height: 3px; }
.tl-code::-webkit-scrollbar-track { background: transparent; }
.tl-code::-webkit-scrollbar-thumb { background: #475569; border-radius: 2px; }
</style>
