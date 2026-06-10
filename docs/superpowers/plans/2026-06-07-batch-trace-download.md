# Batch Trace Download — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add batch trace download (OTLP JSON array) from TraceList.vue via a new `POST /api/v1/traces/export` backend endpoint that reuses existing `convertToOTLP()`.

**Architecture:** Single new backend handler (`ExportTraces`) accepts a list of hex trace IDs, fetches each via `Store.GetTrace()`, converts to OTLP via existing `convertToOTLP()`, and returns a JSON array. Frontend adds checkbox selection to TraceList.vue, a batch action bar with download button, and an `exportTraces()` function in the API client. Reuses existing `downloadBlob()` pattern from TraceDetail.vue.

**Tech Stack:** Go 1.19+ (net/http), Vue 3 + TypeScript, no new dependencies

---

### Task 1: Add ExportTraces backend handler and route

**Files:**
- Modify: `internal/api/trace_handler.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add ExportTraces method to TraceHandler**

In `internal/api/trace_handler.go`, add after the `GetServices` method (after line 106, before `writeJSON`):

```go
// ExportTraces handles POST /api/v1/traces/export.
// Accepts a list of trace IDs and returns them as an OTLP JSON array.
func (h *TraceHandler) ExportTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST allowed"})
		return
	}

	var req struct {
		TraceIDs []string `json:"trace_ids"`
		Format   string   `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	if len(req.TraceIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trace_ids must not be empty"})
		return
	}
	if len(req.TraceIDs) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max 100 traces per export"})
		return
	}

	results := make([]otlpTraceResponse, 0, len(req.TraceIDs))
	for _, hexID := range req.TraceIDs {
		if len(hexID) != 32 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid trace_id length: %s (must be 32 hex chars)", hexID)})
			return
		}
		traceIDBytes, err := hex.DecodeString(hexID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid hex trace_id %s: %v", hexID, err)})
			return
		}
		var traceID [16]byte
		copy(traceID[:], traceIDBytes)

		detail, err := h.store.GetTrace(r.Context(), traceID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get trace %s: %v", hexID, err)})
			return
		}
		if detail == nil {
			continue // silently skip missing traces
		}

		results = append(results, convertToOTLP(detail))
	}

	writeJSON(w, http.StatusOK, results)
}
```

- [ ] **Step 2: Register the route in router.go**

In `internal/api/router.go`, add a new route registration after the existing `/api/v1/traces/` block (after line 25, before the `/api/v1/services` line):

```go
	mux.HandleFunc("/api/v1/traces/export", traceHandler.ExportTraces)
```

Note: This must be registered **before** the `/api/v1/traces/` pattern, because Go's `http.ServeMux` matches longer paths first. Actually, `/api/v1/traces/export` and `/api/v1/traces/` — the exact path `/api/v1/traces/export` takes precedence over the prefix pattern `/api/v1/traces/`. Adding it before the prefix means Go will match it exactly.

- [ ] **Step 3: Run Go tests to verify compilation and existing tests pass**

Run: `go build ./...`
Expected: Exit code 0

Run: `go test ./internal/... -v`
Expected: All existing tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/api/trace_handler.go internal/api/router.go
git commit -m "feat: add POST /api/v1/traces/export endpoint for batch trace download"
```

---

### Task 2: Add exportTraces function to API client

**Files:**
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add exportTraces function**

In `web/src/api/client.ts`, add after the `getServices` function (after line 112):

```typescript
export interface ExportRequest {
  trace_ids: string[]
  format: string
}

export async function exportTraces(traceIds: string[], format: string): Promise<any> {
  const res = await fetch(`${BASE_URL}/traces/export`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ trace_ids: traceIds, format } as ExportRequest),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || `Export failed: ${res.status}`)
  }
  return res.json()
}
```

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd web && npx vue-tsc --noEmit`
Expected: Exit code 0, no type errors

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts
git commit -m "feat: add exportTraces API client function"
```

---

### Task 3: Add i18n strings for batch download UI

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

- [ ] **Step 1: Add English strings**

In `web/src/i18n/locales/en.ts`, add to the `traceList` block inside the default export object (after `noTraces`):

```typescript
    selected: '{count} selected',
    downloadOtlp: 'Download OTLP',
    clearSelection: 'Clear selection',
    exportFailed: 'Export failed',
```

- [ ] **Step 2: Add Chinese strings**

In `web/src/i18n/locales/zh.ts`, add to the `traceList` block (after `noTraces`):

```typescript
    selected: '已选 {count} 条',
    downloadOtlp: '下载 OTLP',
    clearSelection: '取消选择',
    exportFailed: '导出失败',
```

- [ ] **Step 3: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat: add batch download i18n strings"
```

---

### Task 4: Add checkbox selection, batch bar, and download to TraceList.vue

**Files:**
- Modify: `web/src/views/TraceList.vue`

- [ ] **Step 1: Add selection state and download logic to script block**

In the `<script setup>` block, replace the import line:

```typescript
import { listTraces, getServices, type TraceListItem, type Pagination } from '../api/client'
```

with:

```typescript
import { listTraces, getServices, exportTraces, type TraceListItem, type Pagination } from '../api/client'
```

After `const error = ref('')` (line 97), add:

```typescript
// Batch selection state
const selectedIds = ref<Set<string>>(new Set())
const exportLoading = ref(false)

function toggleSelect(traceId: string) {
  const next = new Set(selectedIds.value)
  if (next.has(traceId)) {
    next.delete(traceId)
  } else {
    next.add(traceId)
  }
  selectedIds.value = next
}

function toggleSelectAll() {
  if (selectedIds.value.size === traces.value.length) {
    selectedIds.value = new Set()
  } else {
    selectedIds.value = new Set(traces.value.map(t => t.trace_id_hex))
  }
}

function clearSelection() {
  selectedIds.value = new Set()
}

function isSelected(traceId: string): boolean {
  return selectedIds.value.has(traceId)
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

async function downloadSelected() {
  const ids = Array.from(selectedIds.value)
  if (ids.length === 0) return

  exportLoading.value = true
  try {
    const data = await exportTraces(ids, 'otlp')
    downloadBlob(JSON.stringify(data, null, 2), 'labubu-traces-export.json')
  } catch (e: any) {
    alert(`${t('traceList.exportFailed')}: ${e.message}`)
  } finally {
    exportLoading.value = false
  }
}
```

Also, add `watch` to the Vue import:
```typescript
import { ref, computed, onMounted, watch } from 'vue'
```

And add a watcher to clear selection on page change, after the existing functions (before `onMounted`):

```typescript
// Clear selection when page changes
watch(() => pagination.value.page, () => {
  clearSelection()
})
```

- [ ] **Step 2: Add batch action bar and checkbox column to template**

In `<template>`, after the `</div>` closing the filters div (after line 22), insert the batch action bar:

```html
      <!-- Batch action bar -->
      <div v-if="selectedIds.size > 0" class="batch-bar">
        <span class="batch-count">{{ t('traceList.selected', { count: selectedIds.size }) }}</span>
        <button @click="downloadSelected" :disabled="exportLoading" class="btn btn-primary">
          {{ exportLoading ? t('common.loading') : t('traceList.downloadOtlp') }}
        </button>
        <button @click="clearSelection" class="btn">{{ t('traceList.clearSelection') }}</button>
      </div>
```

In the table header (`<thead>`), add a checkbox column before the Name column:

```html
        <thead>
          <tr>
            <th class="col-checkbox">
              <input type="checkbox" :checked="selectedIds.size === traces.length && traces.length > 0" @change="toggleSelectAll" />
            </th>
            <th>{{ t('traceList.name') }}</th>
```

In the table body, add a checkbox cell before `<td class="cell-name">`:

```html
          <tr
            v-for="trace in traces"
            :key="trace.trace_id_hex"
            :class="['trace-row', { 'row-selected': isSelected(trace.trace_id_hex) }]"
          >
            <td class="col-checkbox" @click.stop>
              <input type="checkbox" :checked="isSelected(trace.trace_id_hex)" @change="toggleSelect(trace.trace_id_hex)" />
            </td>
            <td class="cell-name" @click="goToTrace(trace.trace_id_hex)">{{ trace.root_name }}</td>
```

For the remaining `<td>` elements, add `@click="goToTrace(trace.trace_id_hex)"` to each (since the row click was on `<tr>`, but now we have a checkbox with `@click.stop`, we need individual cell clicks):

```html
            <td @click="goToTrace(trace.trace_id_hex)">{{ trace.root_service }}</td>
            <td @click="goToTrace(trace.trace_id_hex)">{{ formatDuration(trace.duration_ms) }}</td>
            <td @click="goToTrace(trace.trace_id_hex)">{{ trace.span_count }}</td>
            <td @click="goToTrace(trace.trace_id_hex)">
              <span :class="['status-badge', statusClass(trace.status)]">{{ trace.status }}</span>
            </td>
            <td @click="goToTrace(trace.trace_id_hex)">{{ formatTokens(trace.total_tokens) }}</td>
            <td class="cell-time" @click="goToTrace(trace.trace_id_hex)">{{ formatTime(trace.start_time_ms) }}</td>
```

- [ ] **Step 3: Add batch bar and selection styles**

In the `<style scoped>` block, add before the closing `</style>` tag:

```css
/* Batch selection */
.batch-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  margin-bottom: 12px;
  background: var(--bg-surface);
  border: 1px solid var(--accent-primary);
  border-radius: 6px;
}
.batch-count {
  font-size: 14px;
  color: var(--text-primary);
  font-weight: 600;
}
.col-checkbox {
  width: 36px;
  text-align: center;
}
.col-checkbox input[type="checkbox"] {
  width: 16px;
  height: 16px;
  cursor: pointer;
  accent-color: var(--accent-primary);
}
.row-selected {
  background: var(--bg-surface) !important;
}
```

- [ ] **Step 4: Run TypeScript check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: Exit code 0, no type errors

- [ ] **Step 5: Commit**

```bash
git add web/src/views/TraceList.vue
git commit -m "feat: add batch selection and OTLP download to trace list"
```

---

### Task 5: Build and verify

**Files:**
- None (verification only)

- [ ] **Step 1: Build the project**

```bash
make build
```

Expected: Frontend builds, Go binary compiles successfully.

- [ ] **Step 2: Start the server and verify the export endpoint**

```bash
./bin/labubu serve --data-dir /tmp/labubu-test --metrics-enabled=false
```

Send a test request (from another terminal):

```bash
# Test with empty trace_ids (should fail)
curl -s -X POST http://localhost:8080/api/v1/traces/export \
  -H "Content-Type: application/json" \
  -d '{"trace_ids":[],"format":"otlp"}' | python -m json.tool

# Expected: {"error": "trace_ids must not be empty"}

# Test with >100 IDs (should fail)
# Test with valid IDs for traces that exist (should return JSON array)
```

- [ ] **Step 3: Verify frontend in browser**

Open `http://localhost:8080/traces`:
- Checkboxes visible on each row and in header
- Clicking a checkbox highlights the row
- Header checkbox toggles all on current page
- Batch bar appears with "N selected" count, Download OTLP button, Clear button
- Clicking "Clear" deselects all and hides the bar
- Clicking "Download OTLP" triggers download of `labubu-traces-export.json`
- Downloaded file is valid JSON with OTLP resourceSpans array
- Changing pages clears selection

- [ ] **Step 4: Commit any fixes if needed**
