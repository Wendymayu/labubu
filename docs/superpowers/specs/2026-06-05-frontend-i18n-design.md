# Frontend Internationalization (i18n) — Menus and Tables

**Date**: 2026-06-05
**Status**: designed, not yet planned

## Motivation

Labubu's frontend currently hardcodes all UI text in English. The project's design specs are bilingual (Chinese/English), and the target audience includes both English and Chinese speakers. Adding i18n support enables users to switch between languages, improving accessibility and adoption.

This feature focuses on **menus and tables** — the structural UI text in the sidebar navigation, trace list, and session list pages. Detail pages, dashboards, and span inspection panels are out of scope for this iteration.

## Design

### Library: vue-i18n v9

The official Vue 3 i18n plugin. It provides:
- Composition API support (`useI18n()` composable)
- TypeScript-friendly with type-safe message keys
- Reactive locale switching (UI updates automatically when `locale` changes)
- Interpolation for parameterized strings (e.g., "Page 1 of 5")

### Architecture

```
web/src/i18n/
├── index.ts              # createI18n() setup, locale detection
└── locales/
    ├── en.ts             # English translations
    └── zh.ts             # Chinese (Simplified) translations
```

**Plugin registration** in `web/src/main.ts`:

```typescript
import { i18n } from './i18n'
app.use(i18n)
```

**Locale detection**: On first load, read `localStorage.getItem('locale')`. If absent, default to `en`. The sidebar language switcher updates both the i18n instance and localStorage.

**Template usage**:

```vue
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<template>
  <th>{{ t('traceList.name') }}</th>
</template>
```

### Translation Key Structure

Hybrid organization: `common` for shared UI strings, `nav` for sidebar, and per-page namespaces (`traceList`, `sessionList`).

#### `common` (shared across pages)

| Key | English | Chinese |
|-----|---------|---------|
| `search` | Search | 搜索 |
| `reset` | Reset | 重置 |
| `loading` | Loading... | 加载中... |
| `prev` | ← Prev | ← 上一页 |
| `next` | Next → | 下一页 → |
| `pageOf` | Page {page} of {total} ({count} items) | 第 {page} / {total} 页（共 {count} 条） |
| `allServices` | All services | 所有服务 |

#### `nav` (sidebar navigation)

| Key | English | Chinese |
|-----|---------|---------|
| `traces` | Trace | 链路追踪 |
| `sessions` | Sessions | 会话 |
| `metrics` | Metrics | 指标监控 |

#### `traceList` (trace list page)

| Key | English | Chinese |
|-----|---------|---------|
| `searchPlaceholder` | Search traces... | 搜索链路... |
| `allStatus` | All status | 所有状态 |
| `name` | Name | 名称 |
| `service` | Service | 服务 |
| `duration` | Duration | 耗时 |
| `spans` | Spans | 跨度数 |
| `status` | Status | 状态 |
| `tokens` | Tokens | Token数 |
| `time` | Time | 时间 |
| `noTraces` | No traces found. | 未找到链路数据。 |
| `countUnit` | traces | 条链路 |

#### `sessionList` (session list page)

| Key | English | Chinese |
|-----|---------|---------|
| `searchPlaceholder` | Search sessions... | 搜索会话... |
| `sessionId` | Session ID | 会话ID |
| `turns` | Turns | 轮次数 |
| `totalTokens` | Total Tokens | 总Token数 |
| `avgLatency` | Avg Latency | 平均延迟 |
| `maxLatency` | Max Latency | 最大延迟 |
| `errorRate` | Error Rate | 错误率 |
| `lastActive` | Last Active | 最后活跃 |
| `noSessions` | No sessions found. | 未找到会话数据。 |
| `countUnit` | sessions | 个会话 |

### Language Switcher

A native `<select>` dropdown in the sidebar, below the navigation links.

- **Options**: "English" and "中文"
- **Behavior**: On selection, updates `i18n.global.locale`, saves to `localStorage`, UI re-renders immediately
- **Style**: Matches the sidebar dark theme — `#94a3b8` text, styled `<select>` element consistent with filter dropdowns

```vue
<div class="lang-switcher">
  <select v-model="locale">
    <option value="en">English</option>
    <option value="zh">中文</option>
  </select>
</div>
```

Positioned at the bottom of the sidebar via `margin-top: auto` to push it below the nav links.

### Pagination Text

The current pagination pattern `Page X of Y (Z traces)` is replaced with vue-i18n interpolation:

```vue
{{ t('common.pageOf', {
  page: pagination.page,
  total: totalPages,
  count: pagination.total
}) }}
```

The `countUnit` key provides the localized unit word (e.g., "traces" vs "条链路"). However, since the `pageOf` key already embeds "items" as a generic term, `countUnit` is available for future use if the pagination pattern needs to show the specific unit.

### Error Messages

Error messages displayed in the loading/error states (e.g., `e.message || 'Failed to load traces'`) are **not** internationalized in this iteration. These are developer-facing diagnostic strings. The empty states ("No traces found.", "No sessions found.") **are** internationalized as they are user-facing UI text.

## Files to Change

### New files

| File | Purpose |
|------|---------|
| `web/src/i18n/index.ts` | vue-i18n plugin setup, locale detection from localStorage |
| `web/src/i18n/locales/en.ts` | English translation dictionary |
| `web/src/i18n/locales/zh.ts` | Chinese (Simplified) translation dictionary |

### Modified files

| File | Changes |
|------|---------|
| `web/package.json` | Add `vue-i18n@^9` dependency |
| `web/src/main.ts` | Import and register i18n plugin |
| `web/src/App.vue` | Replace hardcoded nav text with `t()` calls, add language switcher `<select>` |
| `web/src/views/TraceList.vue` | Replace table headers, buttons, placeholders, pagination, empty states with `t()` calls |
| `web/src/views/SessionList.vue` | Replace table headers, buttons, pagination, empty states with `t()` calls |

### Unchanged

- `TraceDetail.vue`, `SessionDetail.vue`, `Dashboard.vue` — out of scope
- `SpanDetail.vue`, `PanelChart.vue`, `PanelForm.vue`, `WaterfallChart.vue`, `TokenPieChart.vue` — out of scope
- All Go backend files — no changes
- `web/src/router.ts` — no changes
- `web/src/api/client.ts` — no changes

## Key Design Decisions

1. **vue-i18n over custom solution** — standard library with Composition API support, type safety, and reactive locale switching
2. **TypeScript locale files** — `.ts` files (not `.json`) for IDE type checking on translation keys at build time
3. **localStorage persistence** — simple, no cookie dependency, survives browser restarts
4. **Hybrid key organization** — `common` avoids duplication, per-page namespaces keep things organized, scales to future pages
5. **Menus + tables scope only** — YAGNI: the user explicitly requested this scope; detail pages can be added in a future iteration without restructuring
6. **Native `<select>` for switcher** — simplest possible implementation, no custom component needed, matches existing dropdown styling

## What Stays the Same

- All existing page layouts, styles, and interactions
- API client and backend — no i18n on the server side
- Router configuration — routes remain in English (`/traces`, `/sessions`, `/dashboards`)
- Error messages from API calls — developer-facing diagnostic strings
- Component internals (SpanDetail, PanelChart, etc.) — out of scope
- Chart.js configurations and visualizations — unchanged

## Edge Cases

- **Missing translation key**: vue-i18n falls back to the key name itself (e.g., `traceList.name` displays as literal text). The `en` locale serves as the ultimate fallback.
- **Browser with no localStorage**: Defaults to `en`. No error handling needed — localStorage is universally available in modern browsers.
- **Locale file import failure**: Static imports in `i18n/index.ts` — if the file exists at build time, it will be bundled. No runtime loading failures.
- **Adding a new language later**: Create a new `locales/ja.ts` file following the same key structure, add an `<option>` to the switcher, and register it in `createI18n()`. No other files need changes.
- **Long Chinese text in table headers**: Chinese text is typically shorter than English for these labels (e.g., "名称" vs "Name"). No overflow concerns.
