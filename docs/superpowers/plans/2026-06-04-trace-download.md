# Trace JSON Download Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a download button to the trace detail page that saves the current trace as a pretty-printed JSON file.

**Architecture:** Pure frontend change — the `trace` object is already in memory after `fetchTrace()`. A `downloadTrace()` function serializes it to JSON, creates a Blob, and triggers a browser download via a temporary anchor element. No backend changes.

**Tech Stack:** Vue 3, vanilla browser APIs (Blob, URL.createObjectURL)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `web/src/views/TraceDetail.vue` | Add download button in summary area + `downloadTrace()` function |

---

### Task 1: Add JSON Download Button

**Files:**
- Modify: `web/src/views/TraceDetail.vue`

- [ ] **Step 1: Add the download button in the template**

In the `trace-summary` section, add a download button to the `summary-grid`. Insert this after the "Total Tokens" summary item (line 33):

```html
<div class="summary-item">
  <span class="summary-label">Total Tokens</span>
  <span class="summary-value token-highlight">{{ formatTokens(computeTotalTokens()) }}</span>
</div>
<button class="btn-download" @click="downloadTrace" title="Download trace as JSON">Download</button>
```

- [ ] **Step 2: Add the `downloadTrace` function**

In the `<script setup>` block, add this function after `closeDrawer()`:

```typescript
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
```

- [ ] **Step 3: Add CSS for the download button**

In the `<style scoped>` block, add after the `.token-highlight` rule:

```css
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
```

- [ ] **Step 4: Build and verify**

```bash
cd /d/opensource/github/labubu/web && npm run build
```

Expected: build succeeds with no errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/views/TraceDetail.vue web/dist/
git commit -m "feat: add JSON download button to trace detail page"
```

---

## Verification Checklist

1. Open a trace detail page → "Download" button visible in summary area
2. Click "Download" → browser downloads `trace-{traceIdHex}.json`
3. Open downloaded file → valid, pretty-printed JSON with full trace data
4. File contains all spans, attributes, events, links
5. Empty trace (edge case) → button still works, downloads empty structure
