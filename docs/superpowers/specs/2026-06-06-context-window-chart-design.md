# Context Window Usage Chart — Design Spec

**Date:** 2026-06-06
**Status:** approved

## Overview

Add a context window token usage time-series chart to the Session Detail page (`SessionDetail.vue`). The chart shows how token distribution across context window components (system prompt, user messages, assistant history, tool results, tool definitions, skills) evolves over the duration of a session.

No backend changes — reuses the existing Prometheus-compatible metrics query API.

## Data Source

Jiuwenclaw agent already reports context window token breakdowns as a Prometheus metric:

- **Metric name:** `gen_ai_context_tokens`
- **Labels:** component (`system` | `user` | `assistant` | `tool` | `tool_definitions` | `skill`), `jiuwenclaw.session.id`
- **Storage:** tstorage (metrics store), queried via `GET /api/v1/query_range`

## API Integration

Single range query issued after session detail loads:

```
GET /api/v1/query_range
  ?query=gen_ai_context_tokens{jiuwenclaw.session.id="<sessionId>"}
  &start=<first_active_ms / 1000 - 60>
  &end=<last_active_ms / 1000 + 60>
  &step=<auto>
```

- `start`/`end` derived from `SessionDetail.session.first_active_ms` / `last_active_ms`, with ±60s padding for clock skew
- `step` = max(60s, duration / 60), targeting ~60 data points per series
- Returns `resultType: "matrix"` with one series per component label value

## Layout

Chart section inserted between the summary grid and the turns list:

```
┌─ Back link ─────────────────────────────┐
│  Session: abc123...                      │
│  ┌────────┬────────┬────────┬───────┐   │
│  │ Turns  │ Tokens │ Errors │ Latency│ ← existing summary grid
│  └────────┴────────┴────────┴───────┘   │
│                                          │
│  Context Window ───────────────────────  │  ← NEW section
│  System: max 8.2K avg 7.1K │ User...   │  ← stat cards row
│  ┌──────────────────────────────────┐   │
│  │ ▂▃▅▆▇  (line chart)              │   │  ← Chart.js canvas
│  └──────────────────────────────────┘   │
│                                          │
│  Turns (15) ──────────────────────────── │  ← existing turns list
└──────────────────────────────────────────┘
```

### Stat Cards

A row of compact stat cards above the chart, one per component that appears in the data. Each card shows:

- Component name (human-readable, i18n)
- Max value (peak)
- Average value

Omitted components produce no card.

## Chart

- **Library:** Chart.js (already in use by `PanelChart.vue`)
- **Type:** Line chart, multiple series (one per component)
- **X axis:** Time (format: locale time string)
- **Y axis:** Token count (auto-scaled)
- **Legend:** Bottom, colored by component
- **Tooltip:** Custom external tooltip (same style as `PanelChart.vue`), shows time + per-component values on hover
- **Animation:** Disabled (`animation: false`)
- **Theme:** Reactive to CSS variable theme changes

### Component Color Mapping

Reuse existing CSS variables from `TokenPieChart`:

| component label | CSS variable | approximate color |
|---|---|---|
| system | `--chart-pie-system` | purple |
| user | `--chart-pie-user` | blue |
| assistant | `--chart-pie-assistant` | pink |
| tool | `--chart-pie-tool` | cyan |
| tool_definitions | `--chart-pie-tool-defs` | amber |
| skill | `--chart-pie-skill` | green |

Unknown component labels fall back to a default palette (gray).

### Component Name Mapping (i18n)

| label key | English display | Chinese display |
|---|---|---|
| system | System | 系统提示词 |
| user | User | 用户消息 |
| assistant | Assistant | 助手历史 |
| tool | Tool Results | 工具结果 |
| tool_definitions | Tool Definitions | 工具定义 |
| skill | Skill | 技能 |

## Edge Cases

| Case | Behavior |
|---|---|
| No metrics data for session | Show "No context window data" placeholder text, no blank chart |
| Partial components missing | That series absent from chart and stats; no error |
| Session has very long duration (>1h) | Step auto-scales to keep ~60 points per series |
| Session has very short duration (<1min) | Step clamped to minimum 60s |
| Metrics query errors | Show error message inline in chart area |
| Theme switch (dark/light) | Chart re-renders with new CSS variable colors |

## Loading Behavior

1. Session detail loads first — summary grid and turns list render immediately
2. Context window chart data loads asynchronously (independent `fetch`)
3. Chart area shows loading state while query_range is in flight
4. Errors/timeouts from metrics query do not affect the rest of the page

## Out of Scope

- No backend changes
- No new API endpoints
- No changes to `PanelChart.vue`
- No changes to data ingestion pipeline
- Chart is not extracted as a reusable component (until needed by another page)
- Dashboard-style time range picker (fixed to session duration)
