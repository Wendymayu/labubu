# Span JSON Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a structured/JSON view toggle in the TraceDetail drawer header, with a read-only JSON preview (copy + search) showing the full raw span data.

**Architecture:** Three-file change — extract `highlightJSON` from SpanDetail.vue into `utils/format.ts` as a shared utility, add `viewMode` toggle state and JSON rendering area to TraceDetail.vue, and update SpanDetail.vue to use the shared utility. No new components, no backend changes.

**Tech Stack:** Vue 3 + TypeScript, existing CSS variable system, no new dependencies.

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `web/src/utils/format.ts` | Add `highlightJSON` function extracted from SpanDetail.vue |
| Modify | `web/src/components/SpanDetail.vue` | Remove local `highlightJSON`, import from utils |
| Modify | `web/src/views/TraceDetail.vue` | Add viewMode ref, toggle buttons in drawer header, JSON preview area with copy/search |

---

### Task 1: Extract `highlightJSON` to shared utility

**Files:**
- Modify: `web/src/utils/format.ts`
- Modify: `web/src/components/SpanDetail.vue`

- [ ] **Step 1: Add `highlightJSON` to `format.ts`**

Append to `web/src/utils/format.ts`:

```typescript
export function highlightJSON(raw: string): string {
  try {
    const parsed = JSON.parse(raw)
    const pretty = JSON.stringify(parsed, null, 2)
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

/** Minimal CSS for JSON syntax highlighting. Include once in app styles. */
export const JSON_HIGHLIGHT_CSS = `
.j-key { color: var(--text-secondary); }
.j-str { color: var(--token-green); }
.j-num { color: var(--status-warning); }
.j-bool { color: var(--chart-pie-assistant); }
`
```

- [ ] **Step 2: Update SpanDetail.vue to use shared utility**

In `web/src/components/SpanDetail.vue`:

Remove the local `highlightJSON` function (lines 250-268) and replace the import section:

Add import:
```typescript
import { highlightJSON } from '../utils/format'
```

Remove the local `highlightJSON` function definition.

The `<style scoped>` section at the bottom has the `.j-key`, `.j-str`, etc. styles. Keep those — they're scoped to SpanDetail. The new JSON view in TraceDetail will use `JSON_HIGHLIGHT_CSS` or define its own.

- [ ] **Step 3: Verify TypeScript compilation**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add web/src/utils/format.ts web/src/components/SpanDetail.vue
git commit -m "refactor: extract highlightJSON to shared format utility"
```

---

### Task 2: Add JSON view toggle and preview to TraceDetail

**Files:**
- Modify: `web/src/views/TraceDetail.vue`

- [ ] **Step 1: Add view mode toggle buttons in drawer header**

In the template, inside `<div class="drawer-header">`, replace the existing header content between `drawer-title` and `drawer-close`:

Current (lines 100-107):
```html
        <div v-if="drawerOpen" class="detail-drawer">
          <div class="drawer-header">
            <div class="drawer-title">
              <span class="drawer-span-name">{{ selectedSpan?.name }}</span>
              <span class="drawer-span-id">{{ selectedSpan?.span_id }}</span>
            </div>
            <button class="drawer-close" @click="closeDrawer" title="Close (Esc)">✕</button>
          </div>
```

Replace with:
```html
        <div v-if="drawerOpen" class="detail-drawer">
          <div class="drawer-header">
            <div class="drawer-title">
              <span class="drawer-span-name">{{ selectedSpan?.name }}</span>
              <span class="drawer-span-id">{{ selectedSpan?.span_id }}</span>
            </div>
            <div class="drawer-view-toggle">
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
```

- [ ] **Step 2: Add JSON view area in drawer body**

In the template, inside `drawer-body`, conditionally render structured view or JSON view:

Current:
```html
          <div class="drawer-body">
            <TokenPieChart ... />
            <SpanDetail :span="selectedSpan" />
          </div>
```

Replace with:
```html
          <div class="drawer-body">
            <template v-if="viewMode === 'structured'">
              <TokenPieChart
                v-if="selectedSpanTokenSlices.length > 0"
                :items="selectedSpanTokenSlices"
                :input-tokens="selectedSpanInputTokens"
                :output-tokens="selectedSpanOutputTokens"
              />
              <SpanDetail :span="selectedSpan" />
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
```

- [ ] **Step 3: Add script logic**

Add these imports at the top of `<script setup>`:

```typescript
import { highlightJSON } from '../utils/format'
```

Add these reactive variables (alongside existing refs, around line 148):

```typescript
const viewMode = ref<'structured' | 'json'>('structured')
const jsonSearch = ref('')
const copyLabel = ref('📋 Copy')
```

Add these computed properties:

```typescript
const spanJSON = computed(() => {
  if (!selectedSpan.value) return ''
  return JSON.stringify(selectedSpan.value, null, 2)
})

const highlightedSpanJSON = computed(() => {
  let raw = spanJSON.value
  if (!raw) return ''
  // Apply search highlight
  if (jsonSearch.value.trim()) {
    const term = jsonSearch.value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const re = new RegExp(`(${term})`, 'gi')
    raw = raw.replace(re, '<mark class="json-search-hit">$1</mark>')
  }
  return highlightJSON(raw)
})
```

Add the copy function:

```typescript
function copySpanJSON() {
  navigator.clipboard?.writeText(spanJSON.value).then(() => {
    copyLabel.value = '✓ Copied!'
    setTimeout(() => { copyLabel.value = '📋 Copy' }, 2000)
  }).catch(() => {
    copyLabel.value = '✗ Failed'
    setTimeout(() => { copyLabel.value = '📋 Copy' }, 2000)
  })
}
```

Add a watcher to reset view mode when a new span is selected:

```typescript
import { watch } from 'vue'

watch(selectedSpan, () => {
  viewMode.value = 'structured'
  jsonSearch.value = ''
})
```

- [ ] **Step 4: Add styles**

Append to the `<style scoped>` section in TraceDetail.vue:

```css
/* --- Drawer view toggle --- */
.drawer-view-toggle {
  display: flex;
  gap: 0;
  margin-left: auto;
  margin-right: 12px;
}
.view-toggle-btn {
  padding: 4px 10px;
  border: 1px solid var(--border-group);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 11px;
  cursor: pointer;
}
.view-toggle-btn:first-child {
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

/* JSON syntax highlighting in preview */
.json-content :deep(.j-key) { color: var(--text-secondary); }
.json-content :deep(.j-str) { color: var(--token-green); }
.json-content :deep(.j-num) { color: var(--status-warning); }
.json-content :deep(.j-bool) { color: var(--chart-pie-assistant); }

/* Search hit highlighting */
.json-content :deep(.json-search-hit) {
  background: var(--status-warning);
  color: #000;
  border-radius: 2px;
}
```

- [ ] **Step 5: Verify TypeScript compilation**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors

- [ ] **Step 6: Build frontend**

Run: `cd web && npm run build`
Expected: builds successfully

- [ ] **Step 7: Commit**

```bash
git add web/src/views/TraceDetail.vue
git commit -m "feat: add structured/JSON view toggle with copy and search in span drawer"
```

---

### Task 3: End-to-end verification

**Files:**
- None — verification task

- [ ] **Step 1: Run TypeScript check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors

- [ ] **Step 2: Build frontend**

Run: `cd web && npm run build`
Expected: builds successfully with bundled assets

- [ ] **Step 3: Run Go build (with embedded frontend)**

Run: `go build ./cmd/labubu/...`
Expected: compiles successfully

- [ ] **Step 4: Commit if any fixes were needed**

```bash
git add -A
git commit -m "chore: fix build issues from span JSON preview"
```
