# Span JSON Preview Design Spec

> Status: Draft | Date: 2026-06-09

## Overview

Add a JSON format preview for the currently selected span in TraceDetail's slide-in drawer. A toggle in the drawer header switches between the existing structured view (SpanDetail) and a raw JSON view with copy and search.

## Scope & Decisions

| Dimension | Decision | Rationale |
|-----------|----------|-----------|
| View switching | Toggle buttons in drawer header | Clean, no extra hierarchy |
| JSON content | Full span data, all fields | Devs want the complete raw data |
| JSON interactions | Read-only + copy button + search | Search essential for large JSON; edit meaningless for read-only data |
| Implementation | Inline in TraceDetail.vue, no new component | Single-file change, minimal scope |

## UI Design

### Drawer Header

Add two toggle buttons between the span name and close button:

```
┌──────────────────────────────────────────┐
│ span_name           [Structured] [JSON] ✕ │
│ span_id                                   │
└──────────────────────────────────────────┘
```

- Default: "Structured" active
- Clicking "JSON" switches the drawer body to JSON preview
- Active button gets accent-blue background

### JSON View

```
┌──────────────────────────────────────────┐
│ [📋 Copy]  [🔍 Search...               ] │
├──────────────────────────────────────────┤
│ {                                         │
│   "span_id": "abc123...",                 │
│   "parent_span_id": "def456...",          │
│   "name": "chat completion",              │
│   "kind": "CLIENT",                       │
│   ...                                     │
│ }                                         │
└──────────────────────────────────────────┘
```

- **Copy button**: copies entire JSON to clipboard, could show brief "Copied!" feedback
- **Search box**: filters/scrolls to matching text in the JSON, highlights matches
- **JSON rendering**: `JSON.stringify(span, null, 2)`, monospace font, syntax highlighting reusing `highlightJSON` from SpanDetail
- **Scroll**: both vertical and horizontal overflow for long/nested content

### Syntax Highlighting

Reuse the existing `highlightJSON` function from SpanDetail.vue — extract to a shared utility. Colors:
- Keys: `--text-secondary`
- Strings: `--token-green`
- Numbers: `--status-warning`
- Booleans/null: `--chart-pie-assistant`

## Implementation

### Files Changed

| File | Change |
|------|--------|
| `web/src/utils/format.ts` | Add `highlightJSON` function (extracted from SpanDetail.vue) |
| `web/src/components/SpanDetail.vue` | Import `highlightJSON` from utils, remove local copy |
| `web/src/views/TraceDetail.vue` | Add view toggle state, JSON view area in drawer body, search logic |

### Components

No new components. A single `viewMode` ref (`'structured' | 'json'`) in TraceDetail controls which content renders in the drawer body.

## Testing

- Manual verification: open TraceDetail, click a span, toggle to JSON view, verify all fields present, copy button works, search filters correctly
- No automated tests required for this UI-only change
- TypeScript check: `cd web && npx vue-tsc --noEmit`
