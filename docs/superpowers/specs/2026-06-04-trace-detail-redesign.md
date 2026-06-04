# Trace Detail Page Redesign

**Date**: 2026-06-04
**Status**: designed, not yet planned

## Motivation

The current trace detail page has a fixed two-column layout (waterfall left, 400px detail panel right) that makes span inspection difficult:

- Attributes panel is too narrow (400px), long key names and values get truncated
- Events are bare lists without visual hierarchy; LLM tool call input/output is hard to read
- No search/filter on attributes — finding a specific key among 45+ entries requires manual scanning
- The right panel always occupies space even when the user just wants to browse the waterfall

## Design

### 1. Layout: Full-Width Waterfall + Slide-in Drawer

Replace the fixed two-column layout with an on-demand drawer pattern, matching Jaeger / Grafana UX conventions.

**Default state** (no span selected):
- Waterfall chart occupies the full page width.
- A subtle hint text ("Click any span to view details") is shown.
- The page no longer auto-selects the root span on load.

**Selected state** (user clicks a span):
- A detail drawer slides in from the right. Default width: **480px** (wider than the current 400px).
- The waterfall chart shrinks to fill the remaining space (flex layout).
- Drawer has a close button (✕). Also closes via: clicking empty area in waterfall, pressing Esc.
- Clicking a different span replaces drawer content immediately (no re-animation).
- CSS transition: 300ms ease, `transform: translateX()` or `width` transition.

**TraceDetail.vue changes:**
- New reactive state: `drawerOpen: boolean`, `selectedSpan: SpanDetail | null`.
- Waterfall spans the full width when `drawerOpen === false`.
- Selected span row in waterfall gets a visual indicator (highlight + arrow marker).
- `selectedSpanId` prop passed to WaterfallChart.

### 2. Panel Internal Layout (top to bottom)

Each section is conditionally rendered based on data availability.

| # | Section | Content | When visible |
|---|---------|---------|--------------|
| 1 | **Header** | Span name + span ID + ✕ close button | Always |
| 2 | **Quick Info** | 4-column grid: Kind, Status, Duration, Model | Always |
| 3 | **Token PieChart** | Donut chart + input/output/total counters | Only when `total_tokens > 0` |
| 4 | **Attributes** | Grouped by prefix, with search/filter bar | Always (or "No attributes") |
| 5 | **Events** | Timeline cards with color-coded borders | Only when `events.length > 0` |

### 3. Attributes: Smart Grouping + Search

**Grouping rules** — attributes are automatically grouped by key prefix:

| Group | Prefix match | Default state |
|-------|-------------|---------------|
| Gen AI | `gen_ai.*` | Expanded |
| HTTP | `http.*`, `url.*`, `net.*` | Collapsed |
| Service | `service.*`, `telemetry.*` | Collapsed |
| Other | everything else | Collapsed |

Each group is a collapsible accordion section showing the group name and count. The Gen AI group is expanded by default since it contains the most frequently inspected attributes for LLM traces.

**Search/filter:**
- A text input at the top of the Attributes section.
- Typing filters attributes across all groups in real time (client-side, case-insensitive substring match on key and value).
- Groups with zero matching attributes are hidden.
- Matching text within values is highlighted.

**Value display:**
- Full display (no truncation), natural line-wrapping for long values.
- Key column fixed at ~180px width, value column fills remaining space.

### 4. Events: Timeline Cards

Each event rendered as a card connected by a vertical timeline line.

**Card structure:**
- Left border color indicates event type: `tool.call` = green, `exception` = red, `tool.result` = amber, default = gray.
- A colored dot on the timeline line at the card's vertical position.
- Header row: event name (bold, colored) + time offset from span start (`+120ms`).
- Body: key event attributes displayed as label-value pairs.

**Tool call input/output:**
- For events named `tool.call` or containing `tool.call.*` attributes, render input/output in a monospace code block.
- JSON auto-detection: if the value parses as valid JSON, pretty-print with basic syntax highlighting (keys in one color, string values in another, numbers in a third).
- Each input/output block has a collapse toggle (expanded by default for the first 3 lines).
- Copy button (📋) on each code block for copying the raw value.

**No data state:**
- Events section is hidden entirely when `events` array is empty or undefined.

### 5. TokenPieChart

The existing `TokenPieChart.vue` component is retained as-is. It moves from its current position (above SpanDetail in the right panel) to section #3 inside the drawer, only visible for LLM spans with `total_tokens > 0`.

No visual changes to the chart itself. The component is repositioned via template changes in `TraceDetail.vue`.

### 6. Color Theme

All new UI elements follow the existing black theme:
- Panel/drawer background: `#000`
- Section backgrounds: `#0f172a`, `#111`
- Borders: `#333` (card borders), `#444` (structural borders like drawer edge)
- Text: `#e2e8f0` (primary), `#94a3b8` (secondary), `#64748b` (tertiary)
- Accent colors for event types: `#22c55e` (green/tool.call), `#f59e0b` (amber/tool.result), `#ef4444` (red/exception)

## Files to Change

| File | Changes |
|------|---------|
| `web/src/views/TraceDetail.vue` | Replace fixed 2-column layout with drawer pattern. Move TokenPieChart into drawer. Add drawer state management. Remove auto-select on mount. |
| `web/src/components/SpanDetail.vue` | Major redesign: add Quick Info grid, attribute grouping with search, events timeline cards with JSON highlighting. |
| `web/src/components/WaterfallChart.vue` | Add visual indicator (arrow/label) for selected span when drawer is open. Accept and handle `selectedSpanId` prop. |

## What Stays the Same

- `WaterfallChart.vue` span rendering, bar styles, color coding, tree construction
- `TokenPieChart.vue` — no changes, only repositioned
- `TraceDetail.vue` trace summary header (Trace ID, Service, Duration, Spans, Tokens)
- API client types and fetch functions (`getTrace`, `SpanDetail` type)
- Go backend (no changes needed)
- Dashboard page (no changes)

## Edge Cases

- **Span with no attributes**: Attributes section shows "No attributes" message.
- **Span with no events**: Events section hidden entirely.
- **Non-LLM span (no tokens)**: TokenPieChart section hidden.
- **Very long span name**: Header title truncates with ellipsis after 2 lines.
- **Attribute value is empty string**: Display as `(empty)` in gray.
- **Event with no time offset**: Show `-` instead of offset.
- **Waterfall has 1 span**: Drawer still works; close button returns to full-width view.
- **Window resize**: Drawer width remains fixed 480px on large screens; on screens narrower than 900px, drawer overlays as a full-width panel.
