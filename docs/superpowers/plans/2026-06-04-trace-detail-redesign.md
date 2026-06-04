# Trace Detail Page Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the fixed two-column trace detail layout with a full-width waterfall + on-demand slide-in drawer, and redesign the span detail panel with grouped attributes, search/filter, and timeline-style events.

**Architecture:** Three Vue SFCs are modified. `TraceDetail.vue` manages drawer open/close state and hosts the drawer template (header + TokenPieChart + SpanDetail). `SpanDetail.vue` gains quick-info grid, attribute grouping with prefix-based auto-categorization and real-time search, and color-coded event timeline cards with JSON pretty-printing. `WaterfallChart.vue` already has a `selectedSpanId` prop; its `.selected` CSS class is enhanced with an arrow marker.

**Tech Stack:** Vue 3 Composition API (`<script setup>`), Chart.js (TokenPieChart only — no changes), vanilla CSS (no new dependencies)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `web/src/views/TraceDetail.vue` | Page-level layout, drawer state machine (open/close/transition), drawer header with close button, token pie chart placement |
| `web/src/components/SpanDetail.vue` | Quick-info grid, prefix-grouped attribute accordions with search, event timeline cards with JSON highlighting |
| `web/src/components/WaterfallChart.vue` | Accept `selectedSpanId` (already does), render arrow marker on selected row |

---

### Task 1: Redesign SpanDetail.vue

**Files:**
- Modify: `web/src/components/SpanDetail.vue` (full rewrite)

This is the biggest change. The component gets a completely new template, script, and styles.

- [ ] **Step 1: Replace the entire file content**

Write `web/src/components/SpanDetail.vue`:

```vue
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
        <div class="attr-group-header" @click="group.expanded = !group.expanded">
          <span>{{ group.expanded ? '▾' : '▸' }} <b>{{ group.name }}</b></span>
          <span class="attr-group-count">{{ group.items.length }}</span>
        </div>
        <div v-if="group.expanded" class="attr-group-body">
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
              <div class="tl-attr-row" v-if="isToolIO(k, evt.name)">
                <div class="tl-code-toggle" @click="toggleCodeBlock(evt, k)">
                  {{ codeBlockState(evt, k).expanded ? '▾' : '▸' }} {{ k }}
                  <span class="tl-copy-inline" @click.stop="copyText(v)">📋</span>
                </div>
                <pre v-if="codeBlockState(evt, k).expanded" class="tl-code"><code v-html="highlightJSON(v)"></code></pre>
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
import { ref, computed, reactive } from 'vue'
import type { SpanDetail } from '../api/client'

const props = defineProps<{
  span: SpanDetail | null
}>()

// --- grouping rules ---

interface AttrGroup {
  name: string
  prefixes: string[]
  defaultExpanded: boolean
  expanded: boolean
  items: { key: string; value: string }[]
}

const GROUP_RULES: Omit<AttrGroup, 'expanded' | 'items'>[] = [
  { name: 'Gen AI', prefixes: ['gen_ai.'], defaultExpanded: true },
  { name: 'HTTP', prefixes: ['http.', 'url.', 'net.'], defaultExpanded: false },
  { name: 'Service', prefixes: ['service.', 'telemetry.'], defaultExpanded: false },
]

// --- attribute search ---

const attrFilter = ref('')

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
    expanded: r.defaultExpanded,
    items: [],
  }))
  const otherGroup: AttrGroup = {
    name: 'Other',
    prefixes: [],
    defaultExpanded: false,
    expanded: false,
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

// --- code block expand/collapse state ---

const codeBlocks = reactive<Record<string, Record<string, boolean>>>({})

function getBlockKey(evt: any, k: string): string {
  return `${evt.name || ''}_${k}`
}

function codeBlockState(evt: any, k: string): { expanded: boolean } {
  const evtKey = getBlockKey(evt, k)
  if (!codeBlocks[evtKey]) {
    // Default: collapse after 3 lines
    const raw = String(evt.attributes[k] || '')
    const lines = raw.split('\n')
    codeBlocks[evtKey] = { expanded: lines.length <= 3 }
  }
  return { expanded: !!codeBlocks[evtKey]?.expanded }
}

function toggleCodeBlock(evt: any, k: string) {
  const evtKey = getBlockKey(evt, k)
  if (!codeBlocks[evtKey]) {
    codeBlocks[evtKey] = { expanded: false }
  }
  codeBlocks[evtKey].expanded = !codeBlocks[evtKey].expanded
}

// --- tool I/O detection ---

function isToolIO(attrKey: string, _eventName?: string): boolean {
  const lower = attrKey.toLowerCase()
  return lower === 'input' || lower === 'output' || lower === 'result' ||
    lower.endsWith('.input') || lower.endsWith('.output') ||
    lower.includes('tool.call') || lower.includes('tool.result')
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

function eventBorderClass(evt: any): string {
  const name = (evt.name || '').toLowerCase()
  if (name.includes('tool.call') && !name.includes('tool.call.')) return 'tl-card-toolcall'
  if (name.includes('tool.result')) return 'tl-card-toolresult'
  if (name.includes('exception') || name.includes('error')) return 'tl-card-error'
  return 'tl-card-default'
}

function eventDotClass(evt: any): string {
  const name = (evt.name || '').toLowerCase()
  if (name.includes('tool.call') && !name.includes('tool.call.')) return 'tl-dot-toolcall'
  if (name.includes('tool.result')) return 'tl-dot-toolresult'
  if (name.includes('exception') || name.includes('error')) return 'tl-dot-error'
  return 'tl-dot-default'
}

function eventNameClass(evt: any): string {
  const name = (evt.name || '').toLowerCase()
  if (name.includes('tool.call') && !name.includes('tool.call.')) return 'evt-name-toolcall'
  if (name.includes('tool.result')) return 'evt-name-toolresult'
  if (name.includes('exception') || name.includes('error')) return 'evt-name-error'
  return ''
}

function formatTimeOffset(eventTimeMs: number, spanStartMs?: number): string {
  if (eventTimeMs == null) return '-'
  const base = spanStartMs ?? 0
  const offset = eventTimeMs - base
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
```

- [ ] **Step 2: Add watcher to reset code block state when span changes**

After the `codeBlocks` reactive declaration and the helper functions, add a `watch` import and watcher. Update the import line:

```typescript
import { ref, computed, reactive, watch } from 'vue'
```

Then add this watcher after the `toggleCodeBlock` function (around line 248 in the new file):

```typescript
// Reset code block expand state when span changes
watch(() => props.span, () => {
  Object.keys(codeBlocks).forEach(k => delete codeBlocks[k])
})
```

- [ ] **Step 3: Auto-expand collapsed groups when filter has matching items**

Update the `groupedAttributes` computed — when a filter is active, auto-expand all groups so the user sees matches without manually expanding each group:

In the `groupedAttributes` computed, after the filter loop, add:

```typescript
// When filtering, auto-expand all groups that have matches
if (filter) {
  for (const g of groups) {
    if (g.items.length > 0) g.expanded = true
  }
  if (otherGroup.items.length > 0) otherGroup.expanded = true
}
```

Insert this right before the `// Return non-empty groups` comment (after the for-loop that assigns items to groups).

- [ ] **Step 4: Verify the file syntax**

```bash
cd /d/opensource/github/labubu/web && npx vue-tsc --noEmit src/components/SpanDetail.vue 2>&1 | head -30
```

- [ ] **Step 5: Commit**

```bash
git add web/src/components/SpanDetail.vue
git commit -m "feat: redesign SpanDetail with quick-info grid, grouped attributes, and event timeline"
```

---

### Task 2: Redesign TraceDetail.vue layout

**Files:**
- Modify: `web/src/views/TraceDetail.vue`

- [ ] **Step 1: Replace the template**

Replace the `<template>` block in `web/src/views/TraceDetail.vue`:

```html
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
```

- [ ] **Step 2: Update the `<script setup>` block**

Replace the script block — keep all existing imports and functions, but change the state management section:

```typescript
// === Replace the selectedSpan declaration and selectSpan function: ===

const selectedSpan = ref<SpanDetailType | null>(null)
const drawerOpen = ref(false)

function openDrawer(span: SpanDetailType) {
  selectedSpan.value = span
  drawerOpen.value = true
}

function closeDrawer() {
  drawerOpen.value = false
}

// === Replace the selectSpan function: remove it (replaced by openDrawer) ===

// === Update onMounted — remove auto-select, keep fetch only: ===

onMounted(fetchTrace)

// === Add keyboard handler for Esc: ===
import { onMounted, onUnmounted } from 'vue'

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
```

The full `<script setup>` after changes:

```typescript
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
```

- [ ] **Step 3: Replace the `<style scoped>` block**

Replace the entire style block:

```css
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
```

- [ ] **Step 4: Verify the file syntax**

```bash
cd /d/opensource/github/labubu/web && npx vue-tsc --noEmit src/views/TraceDetail.vue 2>&1 | head -30
```

- [ ] **Step 5: Commit**

```bash
git add web/src/views/TraceDetail.vue
git commit -m "feat: replace fixed two-column layout with full-width waterfall + slide-in drawer"
```

---

### Task 3: Enhance WaterfallChart selected-span indicator

**Files:**
- Modify: `web/src/components/WaterfallChart.vue`

The component already has a `selectedSpanId` prop and applies a `.selected` CSS class. We enhance the visual indicator with an arrow marker.

- [ ] **Step 1: Add the arrow marker in the template**

In `web/src/components/WaterfallChart.vue`, find the `.col-name` span inside `.waterfall-row` and add the arrow:

Replace:
```html
<span class="col-name" :style="{ paddingLeft: (span._depth * 20 + 8) + 'px' }">
  <span
    v-if="span._hasChildren"
    class="toggle-icon"
    @click.stop="toggleExpand(span.span_id)"
  >{{ span._expanded ? '▼' : '▶' }}</span>
  <span :class="['kind-dot', kindDotClass(span.kind)]"></span>
  {{ span.name }}
</span>
```

With:
```html
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
```

- [ ] **Step 2: Add the marker CSS**

In the `<style scoped>` block, add:

```css
.selected-marker {
  color: #38bdf8;
  margin-left: 6px;
  font-size: 10px;
}
```

- [ ] **Step 3: Enhance the `.selected` row style for better visibility**

Update the existing `.waterfall-row.selected` rule:

```css
.waterfall-row.selected {
  background: #1e3a5f;
  outline: 1px solid #38bdf8;
  outline-offset: -1px;
}
```

- [ ] **Step 4: Verify and commit**

```bash
cd /d/opensource/github/labubu/web && npx vue-tsc --noEmit src/components/WaterfallChart.vue 2>&1 | head -10
git add web/src/components/WaterfallChart.vue
git commit -m "feat: add selected-span arrow marker to waterfall rows"
```

---

### Task 4: Build and verify

**Files:**
- Modify: `web/dist/` (build output)

- [ ] **Step 1: Build the frontend**

```bash
cd /d/opensource/github/labubu/web && npm run build
```

Expected: build succeeds with no errors.

- [ ] **Step 2: Run type checking on all changed files**

```bash
cd /d/opensource/github/labubu/web && npx vue-tsc --noEmit 2>&1 | head -20
```

Expected: no type errors.

- [ ] **Step 3: Commit the build output**

```bash
git add web/dist/
git commit -m "chore: build trace detail redesign output"
```

---

## Verification Checklist

After implementation, manually verify these behaviors in the browser:

1. **Default state**: Navigate to a trace → waterfall fills full width, "Click any span to view details" hint shown
2. **Open drawer**: Click a span → drawer slides in from right (480px), waterfall shrinks, selected row has blue outline + ◀ marker
3. **Close drawer**: Click ✕ → drawer closes, waterfall returns to full width
4. **Esc key**: With drawer open, press Esc → drawer closes
5. **Switch span**: Click another span in waterfall → drawer content updates immediately (no re-animation flicker)
6. **Attributes groups**: Gen AI group expanded by default, others collapsed, click to toggle
7. **Attribute search**: Type in filter → attributes filter in real time across all groups
8. **Events timeline**: Events show colored left border + dot, tool.call events show formatted JSON blocks
9. **JSON expand/collapse**: Click ▸ to expand tool I/O, ▾ to collapse
10. **Copy button**: Click 📋 on a tool I/O block → value copied to clipboard
11. **Responsive**: Resize window below 900px → drawer overlays as fixed panel with backdrop
12. **TokenPieChart**: Only visible for LLM spans with `total_tokens > 0`
