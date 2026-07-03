# Time-Range Picker for Traces / Sessions / Logs — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add custom time-range filtering to the Traces, Sessions, and Logs list pages via a shared `TimeRangePicker` component, and refactor the cost page to use the same component.

**Architecture:** A new uncontrolled `TimeRangePicker.vue` emits `{ period, start?, end? }` on mount and on every user change. The cost page sends `period` (custom → range) to `getCostSummary`; the three list pages send the emitted `start`/`end` to `listTraces`/`listSessions`/`listLogs`. The backend already fully supports `start`/`end` for all three list endpoints (verified across SQLite/chDB/memstore) — no Go changes.

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, vue-i18n, existing `get()` API helper in `web/src/api/client.ts`.

**Testing reality:** The frontend has **no unit-test runner** (no vitest/jest in `web/package.json`). Per CLAUDE.md, verification = `cd web && npx vue-tsc --noEmit` (strict types, no `any`) + manual browser checks. No Go changes → no Go tests. Each task commits with a **targeted `git add`** (only the files that task touched) to avoid sweeping the branch's pre-existing WIP into commits.

**Spec:** `docs/superpowers/specs/2026-06-27-time-range-picker-design.md`

---

## File Structure

- **Create** `web/src/components/TimeRangePicker.vue` — shared uncontrolled time-range picker; owns preset/custom state, computes `start`/`end` (epoch ms), emits `change`.
- **Modify** `web/src/api/client.ts` — add `TimeRangeSelection` interface (CLAUDE.md: all new types live here).
- **Modify** `web/src/i18n/locales/en.ts` + `zh.ts` — add `timeRange` section; (in the cost-refactor task) remove the 6 migrated keys from `costDashboard`.
- **Modify** `web/src/views/CostDashboard.vue` — replace inline `.period-bar` with `<TimeRangePicker>`; keep `.btn-preset` style for breakdown toggle.
- **Modify** `web/src/views/TraceList.vue`, `SessionList.vue`, `LogList.vue` — add `<TimeRangePicker showAll>` above the filter bar; wire `start`/`end`; reset via `:key` bump; empty-state hint.

---

## Task 1: Add `timeRange` i18n keys (en + zh)

**Files:**
- Modify: `web/src/i18n/locales/en.ts` (add new top-level section; do NOT remove `costDashboard` keys yet)
- Modify: `web/src/i18n/locales/zh.ts` (same)

- [ ] **Step 1: Add `timeRange` section to `en.ts`**

Insert this new top-level block immediately before the existing `costDashboard:` block (around line 155):

```ts
timeRange: {
  all: 'All',
  today: 'Today',
  '7d': '7 Days',
  '30d': '30 Days',
  custom: 'Custom',
  to: 'to',
  invalidRange: 'Start must be before end',
  emptyHint: 'Only showing data for the selected time range. Switch to "All" to see everything.',
},
```

- [ ] **Step 2: Add `timeRange` section to `zh.ts`**

Insert the matching block immediately before the existing `costDashboard:` block (around line 155):

```ts
timeRange: {
  all: '全部',
  today: '今天',
  '7d': '近 7 天',
  '30d': '近 30 天',
  custom: '自定义',
  to: '至',
  invalidRange: '开始时间需早于结束时间',
  emptyHint: '当前仅显示所选时间范围内的数据，切换为「全部」可查看全部数据。',
},
```

- [ ] **Step 3: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS (no errors). i18n key additions don't affect type checking, but confirms no accidental syntax break.

- [ ] **Step 4: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat(i18n): add timeRange section for shared time-range picker"
```

---

## Task 2: Add `TimeRangeSelection` type to `client.ts`

**Files:**
- Modify: `web/src/api/client.ts` (insert after the `Pagination` interface, ~line 21)

- [ ] **Step 1: Add the interface**

Insert immediately after the `Pagination` interface (after line 21, before `TraceListResponse`):

```ts
// TimeRangeSelection is the payload emitted by the shared TimeRangePicker
// component. `start`/`end` are epoch ms; both are undefined for the "all"
// preset (no time filter). For presets (today/7d/30d) and custom, both are set.
export interface TimeRangeSelection {
  period: string
  start?: number
  end?: number
}
```

- [ ] **Step 2: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat(api): add TimeRangeSelection type for time-range picker"
```

---

## Task 3: Create `TimeRangePicker.vue`

**Files:**
- Create: `web/src/components/TimeRangePicker.vue`

- [ ] **Step 1: Write the component**

Create `web/src/components/TimeRangePicker.vue` with this exact content:

```vue
<template>
  <div class="period-bar">
    <button
      v-if="showAll"
      :class="['btn', 'btn-preset', { active: activePeriod === 'all' }]"
      @click="setPeriod('all')"
    >{{ t('timeRange.all') }}</button>
    <button
      v-for="p in periods"
      :key="p"
      :class="['btn', 'btn-preset', { active: activePeriod === p }]"
      @click="setPeriod(p)"
    >{{ t(`timeRange.${p}`) }}</button>
    <div v-if="activePeriod === 'custom'" class="custom-range">
      <input type="datetime-local" v-model="customStart" @change="onCustomChange" />
      <span>{{ t('timeRange.to') }}</span>
      <input type="datetime-local" v-model="customEnd" @change="onCustomChange" />
    </div>
    <span v-if="rangeError" class="range-error">{{ t('timeRange.invalidRange') }}</span>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TimeRangeSelection } from '../api/client'

withDefaults(defineProps<{ showAll?: boolean }>(), {
  showAll: false,
})

const emit = defineEmits<{
  (e: 'change', selection: TimeRangeSelection): void
}>()

const { t } = useI18n()

// Presets always rendered (the "all" button is conditional on showAll).
const periods = ['today', '7d', '30d', 'custom'] as const

const activePeriod = ref<string>('today')
const customStart = ref('')
const customEnd = ref('')
const rangeError = ref(false)

// <input type="datetime-local"> values are local time with no zone suffix.
function toLocalInputValue(ms: number): string {
  const d = new Date(ms)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// Compute start/end (epoch ms) for a non-custom preset. 'all' → no bounds.
function presetRange(period: string): { start?: number; end?: number } {
  const now = Date.now()
  switch (period) {
    case 'today': {
      const d = new Date()
      return { start: new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime(), end: now }
    }
    case '7d':
      return { start: now - 7 * 24 * 3600 * 1000, end: now }
    case '30d':
      return { start: now - 30 * 24 * 3600 * 1000, end: now }
    default: // 'all' or unknown
      return {}
  }
}

function emitSelection(period: string) {
  rangeError.value = false
  if (period === 'custom') {
    if (!customStart.value || !customEnd.value) return // not fully entered yet
    const start = new Date(customStart.value).getTime()
    const end = new Date(customEnd.value).getTime()
    if (start > end) {
      rangeError.value = true
      return // do not emit an invalid range
    }
    emit('change', { period, start, end })
    return
  }
  const { start, end } = presetRange(period)
  emit('change', { period, start, end })
}

function setPeriod(key: string) {
  if (key === 'custom') {
    // Seed the custom range from the current preset so the user can fine-tune.
    const now = Date.now()
    let start: number
    switch (activePeriod.value) {
      case 'today': {
        const d = new Date()
        start = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
        break
      }
      case '7d': start = now - 7 * 24 * 3600 * 1000; break
      case '30d': start = now - 30 * 24 * 3600 * 1000; break
      default: start = now - 24 * 3600 * 1000 // 'all' or already-custom → last 24h
    }
    customStart.value = toLocalInputValue(start)
    customEnd.value = toLocalInputValue(now)
    activePeriod.value = 'custom'
    emitSelection('custom')
    return
  }
  activePeriod.value = key
  emitSelection(key)
}

function onCustomChange() {
  emitSelection('custom')
}

onMounted(() => {
  // Emit the default 'today' selection so parents run their first fetch with
  // the correct time range. Vue mounts children before parents, so this fires
  // before the parent's onMounted.
  emitSelection(activePeriod.value)
})
</script>

<style scoped>
.period-bar {
  display: flex;
  gap: 8px;
  margin-bottom: 20px;
  align-items: center;
  flex-wrap: wrap;
}

.btn-preset {
  padding: 6px 16px;
  border: 1px solid var(--border-default);
  background: var(--bg-primary);
  color: var(--text-secondary);
  cursor: pointer;
  border-radius: 4px;
  font-size: 13px;
}

.btn-preset:hover {
  color: var(--text-primary);
}

.btn-preset.active {
  background: var(--accent-blue);
  color: #fff;
  border-color: var(--accent-blue);
}

.custom-range {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: 4px;
}

.custom-range input {
  background: var(--bg-primary);
  border: 1px solid var(--border-default);
  border-radius: 4px;
  color: var(--text-primary);
  padding: 6px 10px;
  font-size: 13px;
}

.custom-range span {
  color: var(--text-secondary);
  font-size: 13px;
}

html[data-theme="dark"] .custom-range input {
  color-scheme: dark;
}

.range-error {
  color: var(--status-error-accent);
  font-size: 13px;
  margin-left: 8px;
}
</style>
```

- [ ] **Step 2: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS. (`showAll` is used in the template via `v-if="showAll"`; the bare `withDefaults(defineProps…)` form exposes it to the template without needing a `props` const.)

- [ ] **Step 3: Commit**

```bash
git add web/src/components/TimeRangePicker.vue
git commit -m "feat(web): add shared TimeRangePicker component"
```

---

## Task 4: Refactor `CostDashboard.vue` to use the picker

**Files:**
- Modify: `web/src/views/CostDashboard.vue`
- Modify: `web/src/i18n/locales/en.ts` (remove 6 migrated keys from `costDashboard`)
- Modify: `web/src/i18n/locales/zh.ts` (same)

This task migrates the cost template to `timeRange.*` AND removes the now-unused `costDashboard.{today,7d,30d,custom,to,invalidRange}` keys — atomic, so there is no transient broken-key state.

- [ ] **Step 1: Replace the inline period-bar in the template**

In `CostDashboard.vue`, replace the entire `<!-- Period selector -->` block (lines 10-23):

```vue
      <!-- Period selector -->
      <div class="period-bar">
        <button
          v-for="p in periods"
          :key="p.key"
          :class="['btn', 'btn-preset', { active: activePeriod === p.key }]"
          @click="setPeriod(p.key)"
        >{{ t(`costDashboard.${p.key}`) }}</button>
        <div v-if="activePeriod === 'custom'" class="custom-range">
          <input type="datetime-local" v-model="customStart" @change="onCustomChange" />
          <span>{{ t('costDashboard.to') }}</span>
          <input type="datetime-local" v-model="customEnd" @change="onCustomChange" />
        </div>
      </div>
```

with:

```vue
      <!-- Period selector -->
      <TimeRangePicker @change="onTimeChange" />
```

- [ ] **Step 2: Update the `<script setup>` block**

In the same file, make these script changes:

(a) Update imports (line 93) — replace:
```ts
import { getCostSummary, getModelPricing, type CostSummary } from '../api/client'
```
with:
```ts
import { getCostSummary, getModelPricing, type CostSummary, type TimeRangeSelection } from '../api/client'
import TimeRangePicker from '../components/TimeRangePicker.vue'
```

(b) Remove the `periods` array and the inline state/helpers. Delete lines 98-120 (the `periods` array, `activePeriod`, `groupBy` stays, `customStart`, `customEnd`, `toLocalInputValue`, `onCustomChange`, `setGroupBy`). Replace that whole region so the remaining state is:

```ts
const groupBy = ref<'model' | 'service'>('model')
const currentRange = ref<TimeRangeSelection>({ period: 'today' })
```

(c) Replace the old `setGroupBy` (which was deleted in (b)) with:

```ts
function setGroupBy(dim: 'model' | 'service') {
  if (groupBy.value === dim) return
  groupBy.value = dim
  fetchData(currentRange.value)
}
```

(d) Replace the `fetchData` function (lines 172-194) with:

```ts
async function fetchData(sel: TimeRangeSelection) {
  loading.value = true
  loadError.value = ''
  try {
    // Narrow start/end to number (they are number|undefined on the type) so the
    // range matches getCostSummary's { start: number; end: number } param.
    const range = sel.period === 'custom' && sel.start != null && sel.end != null
      ? { start: sel.start, end: sel.end }
      : undefined
    const result = await getCostSummary(sel.period, groupBy.value, range)
    summary.value = result
  } catch (e: any) {
    loadError.value = e.message || 'Failed to load cost data'
  } finally {
    loading.value = false
  }
}
```

(e) Add `onTimeChange` (place it near `fetchData`):

```ts
function onTimeChange(sel: TimeRangeSelection) {
  currentRange.value = sel
  fetchData(sel)
}
```

(f) Replace the `onMounted` block (lines 231-234) — remove the `fetchData()` call (the picker's mount emit now triggers it), keep `checkPricing`:

```ts
onMounted(() => {
  checkPricing()
})
```

- [ ] **Step 3: Remove migrated style rules from `CostDashboard.vue`**

In the `<style scoped>` block, delete the `.period-bar`, `.custom-range`, `.custom-range input`, `.custom-range span`, and `html[data-theme="dark"] .custom-range input` rules (lines ~244-296). **Keep** the `.btn-preset`, `.btn-preset:hover`, `.btn-preset.active` rules — the breakdown toggle (byModel/byService) still uses `btn-preset`.

- [ ] **Step 4: Remove the 6 migrated keys from `costDashboard` in `en.ts`**

In `web/src/i18n/locales/en.ts`, inside the `costDashboard:` block, delete these 6 lines (they now live in `timeRange`):
```
    today: 'Today',
    '7d': '7 Days',
    '30d': '30 Days',
    custom: 'Custom',
    to: 'to',
    invalidRange: 'Start must be before end',
```
Keep all other `costDashboard` keys (`totalCost`, `totalTokens`, … `noData`).

- [ ] **Step 5: Remove the 6 migrated keys from `costDashboard` in `zh.ts`**

In `web/src/i18n/locales/zh.ts`, delete the matching 6 lines:
```
    today: '今天',
    '7d': '近 7 天',
    '30d': '近 30 天',
    custom: '自定义',
    to: '至',
    invalidRange: '开始时间需早于结束时间',
```

- [ ] **Step 6: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS. Confirms no dangling references to removed `periods`/`activePeriod`/`customStart`/`customEnd`/`setPeriod`/`toLocalInputValue`/`onCustomChange`.

- [ ] **Step 7: Manual verify cost page**

Run: `go run -tags dev ./cmd/labubu serve` (from repo root), open http://localhost:8080/cost.
Expected:
- Picker shows Today / 7 Days / 30 Days / Custom (no "All"). Today is active.
- Default load shows today's data (same as before).
- Click 7d / 30d → data refreshes.
- Click Custom → two datetime inputs appear, seeded from the previous preset; changing them refreshes; entering start > end shows the inline "Start must be before end" and does NOT fetch.
- By Model / By Service toggle still styled and works.
- No `costDashboard.today` raw-key text appears anywhere.

- [ ] **Step 8: Commit**

```bash
git add web/src/views/CostDashboard.vue web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "refactor(web): CostDashboard uses shared TimeRangePicker"
```

---

## Task 5: Integrate `TimeRangePicker` into `TraceList.vue`

**Files:**
- Modify: `web/src/views/TraceList.vue`

- [ ] **Step 1: Add the picker to the template**

In `TraceList.vue`, immediately before the `<div class="filters">` line (line 3), insert:

```vue
    <TimeRangePicker showAll :key="resetKey" @change="onTimeChange" />
```

- [ ] **Step 2: Augment the empty state with a time-range hint**

Replace the empty-state line (line 76):

```vue
      <div v-else class="empty">{{ t('traceList.noTraces') }}</div>
```

with:

```vue
      <div v-else class="empty">
        {{ t('traceList.noTraces') }}
        <div v-if="timeRange.period !== 'all'" class="empty-hint">{{ t('timeRange.emptyHint') }}</div>
      </div>
```

- [ ] **Step 3: Update imports**

Replace line 114:

```ts
import { listTraces, getServices, exportTraces, importTraces, type TraceListItem, type Pagination, type ImportResult } from '../api/client'
import { formatCost } from '../utils/format'
import { usePageSize } from '../composables/usePageSize'
```

with:

```ts
import { listTraces, getServices, exportTraces, importTraces, type TraceListItem, type Pagination, type ImportResult, type TimeRangeSelection } from '../api/client'
import { formatCost } from '../utils/format'
import { usePageSize } from '../composables/usePageSize'
import TimeRangePicker from '../components/TimeRangePicker.vue'
```

- [ ] **Step 4: Add time-range state and handlers**

After the `fileInput` ref (line 134), add:

```ts
const timeRange = ref<TimeRangeSelection>({ period: 'today' })
const resetKey = ref(0)

function onTimeChange(sel: TimeRangeSelection) {
  timeRange.value = sel
  fetchTraces(1)
}
```

- [ ] **Step 5: Pass `start`/`end` into `listTraces`**

In `fetchTraces` (line 229), replace:

```ts
    const result = await listTraces({ ...filters.value, page, page_size: pagination.value.page_size })
```

with:

```ts
    const result = await listTraces({
      ...filters.value,
      page,
      page_size: pagination.value.page_size,
      start: timeRange.value.start,
      end: timeRange.value.end,
    })
```

- [ ] **Step 6: Reset time range in `reset()`**

Replace the `reset` function (lines 251-254):

```ts
function reset() {
  filters.value = { q: '', service: '', status: '' }
  fetchTraces(1)
}
```

with:

```ts
function reset() {
  filters.value = { q: '', service: '', status: '' }
  // Bumping :key remounts the picker → it re-emits the default 'today' range
  // → onTimeChange → fetchTraces(1). Clears any custom datetime too.
  resetKey.value++
}
```

- [ ] **Step 7: Remove the now-redundant onMounted `fetchTraces()` call**

Replace the `onMounted` block (lines 302-305):

```ts
onMounted(() => {
  fetchTraces()
  fetchServices()
})
```

with:

```ts
onMounted(() => {
  // fetchTraces is triggered by the picker's mount emit (default 'today').
  fetchServices()
})
```

- [ ] **Step 8: Add the empty-hint style**

In the `<style scoped>` block, add (e.g. after the `.empty` rule region):

```css
.empty-hint { margin-top: 8px; font-size: 13px; color: var(--text-secondary); }
```

- [ ] **Step 9: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS.

- [ ] **Step 10: Manual verify traces page**

Run: `go run -tags dev ./cmd/labubu serve`, open http://localhost:8080/traces.
Expected:
- Picker shows All / Today / 7 Days / 30 Days / Custom; Today active by default; list shows only today's traces.
- All → all traces; 7d / 30d → respective windows; pagination resets to page 1 on each change.
- Custom → seeded inputs; invalid range shows error, no fetch.
- Reset button → text filters clear AND picker returns to Today (remount).
- Empty result under Today/7d/30d/Custom shows the hint; under All it does not.

- [ ] **Step 11: Commit**

```bash
git add web/src/views/TraceList.vue
git commit -m "feat(web): custom time-range filter on Traces page"
```

---

## Task 6: Integrate `TimeRangePicker` into `SessionList.vue`

**Files:**
- Modify: `web/src/views/SessionList.vue`

Mirrors Task 5. `SessionQuery` already has `start`/`end`; `listSessions` already passes them through.

- [ ] **Step 1: Add the picker to the template**

Immediately before `<div class="filters">` (line 3), insert:

```vue
    <TimeRangePicker showAll :key="resetKey" @change="onTimeChange" />
```

- [ ] **Step 2: Augment the empty state**

Replace line 58:

```vue
      <div v-else class="empty">{{ t('sessionList.noSessions') }}</div>
```

with:

```vue
      <div v-else class="empty">
        {{ t('sessionList.noSessions') }}
        <div v-if="timeRange.period !== 'all'" class="empty-hint">{{ t('timeRange.emptyHint') }}</div>
      </div>
```

- [ ] **Step 3: Update imports**

Replace lines 96-97:

```ts
import { listSessions, getServices, type SessionListItem, type Pagination } from '../api/client'
import { usePageSize } from '../composables/usePageSize'
```

with:

```ts
import { listSessions, getServices, type SessionListItem, type Pagination, type TimeRangeSelection } from '../api/client'
import { usePageSize } from '../composables/usePageSize'
import TimeRangePicker from '../components/TimeRangePicker.vue'
```

- [ ] **Step 4: Add time-range state and handlers**

After the `filters` ref (line 113), add:

```ts
const timeRange = ref<TimeRangeSelection>({ period: 'today' })
const resetKey = ref(0)

function onTimeChange(sel: TimeRangeSelection) {
  timeRange.value = sel
  fetchSessions(1)
}
```

- [ ] **Step 5: Pass `start`/`end` into `listSessions`**

In `fetchSessions` (line 123), replace:

```ts
    const result = await listSessions({ ...filters.value, page, page_size: pagination.value.page_size })
```

with:

```ts
    const result = await listSessions({
      ...filters.value,
      page,
      page_size: pagination.value.page_size,
      start: timeRange.value.start,
      end: timeRange.value.end,
    })
```

- [ ] **Step 6: Reset time range in `reset()`**

Replace the `reset` function (lines 145-148):

```ts
function reset() {
  filters.value = { q: '', service: '' }
  fetchSessions(1)
}
```

with:

```ts
function reset() {
  filters.value = { q: '', service: '' }
  resetKey.value++ // remount picker → re-emits default 'today' → fetchSessions(1)
}
```

- [ ] **Step 7: Remove the redundant onMounted `fetchSessions()` call**

Replace the `onMounted` block (lines 190-193):

```ts
onMounted(() => {
  fetchSessions()
  fetchServices()
})
```

with:

```ts
onMounted(() => {
  // fetchSessions is triggered by the picker's mount emit (default 'today').
  fetchServices()
})
```

- [ ] **Step 8: Add the empty-hint style**

In `<style scoped>`, add:

```css
.empty-hint { margin-top: 8px; font-size: 13px; color: var(--text-secondary); }
```

- [ ] **Step 9: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS.

- [ ] **Step 10: Manual verify sessions page**

Run: `go run -tags dev ./cmd/labubu serve`, open http://localhost:8080/sessions.
Expected: same behavior as Traces (Task 5 Step 10), substituting sessions/noSessions.

- [ ] **Step 11: Commit**

```bash
git add web/src/views/SessionList.vue
git commit -m "feat(web): custom time-range filter on Sessions page"
```

---

## Task 7: Integrate `TimeRangePicker` into `LogList.vue`

**Files:**
- Modify: `web/src/views/LogList.vue`

Mirrors Tasks 5-6. `LogQuery` already has `start`/`end`; `listLogs` already passes them through. LogList's empty state is `.empty-state` (not `.empty`).

- [ ] **Step 1: Add the picker to the template**

Immediately before `<!-- Table -->` / `<div class="log-table-wrap">` (line 19), insert:

```vue
    <TimeRangePicker showAll :key="resetKey" @change="onTimeChange" />
```

- [ ] **Step 2: Augment the empty state**

Replace line 91:

```vue
      <div v-else-if="!loading" class="empty-state">{{ t('logList.noLogs') }}</div>
```

with:

```vue
      <div v-else-if="!loading" class="empty-state">
        {{ t('logList.noLogs') }}
        <div v-if="timeRange.period !== 'all'" class="empty-hint">{{ t('timeRange.emptyHint') }}</div>
      </div>
```

- [ ] **Step 3: Update imports**

Replace lines 116-117:

```ts
import { listLogs, getLogEventNames, type LogRecord } from '../api/client'
import { usePageSize } from '../composables/usePageSize'
```

with:

```ts
import { listLogs, getLogEventNames, type LogRecord, type TimeRangeSelection } from '../api/client'
import { usePageSize } from '../composables/usePageSize'
import TimeRangePicker from '../components/TimeRangePicker.vue'
```

- [ ] **Step 4: Add time-range state and handlers**

After the `openFilter` ref (line 136), add:

```ts
const timeRange = ref<TimeRangeSelection>({ period: 'today' })
const resetKey = ref(0)

function onTimeChange(sel: TimeRangeSelection) {
  timeRange.value = sel
  page.value = 1
  fetchLogs()
}
```

- [ ] **Step 5: Pass `start`/`end` into `listLogs`**

In `fetchLogs` (lines 193-200), replace the `listLogs({...})` call:

```ts
    const result = await listLogs({
      page: page.value,
      page_size: pageSize.value,
      severity: severityFilter.value || undefined,
      event_name: eventFilter.value || undefined,
      q: searchQuery.value || undefined,
      trace_id: traceIdFilter.value || undefined,
    })
```

with:

```ts
    const result = await listLogs({
      page: page.value,
      page_size: pageSize.value,
      severity: severityFilter.value || undefined,
      event_name: eventFilter.value || undefined,
      q: searchQuery.value || undefined,
      trace_id: traceIdFilter.value || undefined,
      start: timeRange.value.start,
      end: timeRange.value.end,
    })
```

- [ ] **Step 6: Reset time range in `reset()`**

Replace the `reset` function (lines 151-158):

```ts
function reset() {
  searchQuery.value = ''
  severityFilter.value = ''
  eventFilter.value = ''
  traceIdFilter.value = ''
  page.value = 1
  fetchLogs()
}
```

with:

```ts
function reset() {
  searchQuery.value = ''
  severityFilter.value = ''
  eventFilter.value = ''
  traceIdFilter.value = ''
  page.value = 1
  resetKey.value++ // remount picker → re-emits default 'today' → fetchLogs
}
```

- [ ] **Step 7: Remove the redundant onMounted `fetchLogs()` call**

Replace the `onMounted` block (lines 253-257):

```ts
onMounted(() => {
  document.addEventListener('click', closeFilter)
  fetchEventNames()
  fetchLogs()
})
```

with:

```ts
onMounted(() => {
  document.addEventListener('click', closeFilter)
  fetchEventNames()
  // fetchLogs is triggered by the picker's mount emit (default 'today').
})
```

- [ ] **Step 8: Add the empty-hint style**

In `<style scoped>`, add:

```css
.empty-hint { margin-top: 8px; font-size: 13px; color: var(--text-secondary); }
```

- [ ] **Step 9: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS.

- [ ] **Step 10: Manual verify logs page**

Run: `go run -tags dev ./cmd/labubu serve`, open http://localhost:8080/logs.
Expected: same behavior as Traces (Task 5 Step 10), substituting logs/noLogs. Note the logs table timestamp column shows time-of-day; confirm filtering by Today actually narrows to today's logs.

- [ ] **Step 11: Commit**

```bash
git add web/src/views/LogList.vue
git commit -m "feat(web): custom time-range filter on Logs page"
```

---

## Task 8: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS with zero errors.

- [ ] **Step 2: Build check (no CGO)**

Run: `make build-nocgo` (or, if `make` is unavailable on Windows per project memory, `cd web && npx vue-tsc && npx vite build` then `go build -tags !dev ./cmd/labubu`)
Expected: builds cleanly.

- [ ] **Step 3: End-to-end manual sweep**

Run: `go run -tags dev ./cmd/labubu serve`, open each page and confirm:
- `/cost` — behavior unchanged vs. before this change (presets send `period`, custom sends range, breakdown toggle styled, no raw key strings).
- `/traces`, `/sessions`, `/logs` — default Today; All/7d/30d/Custom all work; pagination resets to page 1 on time change; Reset returns to Today; empty-state hint appears under non-All filters only.
- Switch language (en ↔ zh) — all time-range labels translate correctly.

- [ ] **Step 4: Final commit (only if Step 2/3 surfaced fixups)**

If any fixup was needed, stage just the touched files and commit with `fix(web: time-range picker polish`. Otherwise no commit — all changes were committed per-task.

---

## Self-Review (run after writing, before handoff)

- **Spec coverage:** Component (Task 3) ✓ · `showAll` prop ✓ · mount-emit + onMounted cleanup (Task 4 Step 2f, Task 5/6/7 Step 7) ✓ · cost behavior unchanged (Task 4) ✓ · list pages send start/end (Task 5/6/7 Step 5) ✓ · reset to Today via :key (Step 6) ✓ · empty-state hint (Step 2) ✓ · i18n `timeRange` + migrate 6 keys (Task 1 + Task 4 Step 4-5) ✓ · verification vue-tsc + manual (every task + Task 8) ✓.
- **Type consistency:** `TimeRangeSelection` (defined Task 2) used identically in component (Task 3), cost (Task 4), traces/sessions/logs (Tasks 5-7) — `{ period: string; start?: number; end?: number }`. `onTimeChange(sel: TimeRangeSelection)` signature matches the component's `emit('change', …)` payload. `resetKey` + `:key="resetKey"` named consistently.
- **No placeholders:** every code step shows full code; no TBD/TODO/"add error handling".
