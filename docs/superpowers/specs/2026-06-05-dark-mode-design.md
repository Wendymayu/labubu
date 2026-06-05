# Dark/Light Theme Design Spec

> Date: 2026-06-05
> Feature: Roadmap #11 — 页面风格支持黑白两种背景
> Status: Draft

## 1. Overview

Add a light theme to Labubu alongside the existing dark theme. Users can toggle between dark and light modes via a button in the sidebar. The preference is persisted in `localStorage`, with dark as the default.

### Goals

- Introduce a CSS custom property (variable) based theming system
- Add a `<ThemeToggle>` component in the sidebar footer
- Migrate all hardcoded color values in existing components to CSS variables
- Ensure chart colors (Waterfall, TokenPieChart) adapt per theme
- Smooth 0.25s transition animation when switching themes

### Non-Goals

- System preference (`prefers-color-scheme`) detection — deferred to future iteration
- Additional themes beyond dark and light
- Per-page or per-panel theme overrides

## 2. Architecture

```
┌─────────────────────────────────────────────┐
│  <html data-theme="dark | light">           │
│                                             │
│  styles/theme.css   ← CSS variable tokens   │
│  ├── :root { ... dark tokens (default) }    │
│  └── [data-theme="light"] { ... override }  │
│                                             │
│  composables/useTheme.ts                    │
│  ├── reactive theme state                   │
│  ├── toggleTheme() / setTheme()             │
│  └── localStorage persistence               │
│                                             │
│  App.vue                                    │
│  ├── calls useTheme() to init data-theme    │
│  └── <ThemeToggle /> in sidebar footer      │
│                                             │
│  All components                             │
│  └── hardcoded hex → var(--token-name)      │
│                                             │
│  Global transition rule                     │
│  └── 0.25s ease on bg, color, border-color  │
└─────────────────────────────────────────────┘
```

### File Changes Summary

| Action | File | Purpose |
|--------|------|---------|
| New | `web/src/styles/theme.css` | CSS variable definitions for both themes |
| New | `web/src/composables/useTheme.ts` | Theme state management + persistence |
| New | `web/src/components/ThemeToggle.vue` | Toggle button UI |
| Edit | `web/src/main.ts` | Import `theme.css` |
| Edit | `web/src/App.vue` | Initialize theme, add ThemeToggle, sidebar footer layout |
| Edit | All Vue components (12+) | Replace hardcoded colors with CSS variables |

## 3. CSS Variable Tokens

New file: `web/src/styles/theme.css`

### Token Naming Convention

Tokens are named semantically (by purpose), not by color value:
- `--bg-*` for backgrounds
- `--text-*` for text colors
- `--border-*` for borders
- `--accent-*` for accent/interactive colors
- `--status-*` for status indicators
- `--chart-*` for chart semantic colors
- `--scrollbar-*` for scrollbar styling

### Dark Theme (Default)

```css
:root {
  /* Backgrounds */
  --bg-primary: #000000;
  --bg-secondary: #111111;
  --bg-surface: #1e293b;
  --bg-surface-hover: #334155;

  /* Borders */
  --border-default: #334155;
  --border-subtle: #1e293b;
  --border-strong: #475569;

  /* Text */
  --text-primary: #e2e8f0;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;

  /* Accents */
  --accent-blue: #38bdf8;
  --accent-primary: #2563eb;
  --accent-primary-hover: #1d4ed8;

  /* Status */
  --status-ok-bg: #065f46;
  --status-ok-text: #6ee7b7;
  --status-error-bg: #7f1d1d;
  --status-error-text: #fca5a5;
  --token-highlight: #c4b5fd;

  /* Chart colors */
  --chart-server: #3b82f6;
  --chart-client: #22c55e;
  --chart-producer: #f59e0b;
  --chart-consumer: #a855f7;
  --chart-internal: #6b7280;
  --chart-llm-start: #8b5cf6;
  --chart-llm-end: #a78bfa;

  /* Scrollbar */
  --scrollbar-thumb: #475569;
}
```

### Light Theme

```css
[data-theme="light"] {
  --bg-primary: #ffffff;
  --bg-secondary: #f1f5f9;
  --bg-surface: #f8fafc;
  --bg-surface-hover: #e2e8f0;

  --border-default: #cbd5e1;
  --border-subtle: #e2e8f0;
  --border-strong: #94a3b8;

  --text-primary: #1e293b;
  --text-secondary: #475569;
  --text-muted: #94a3b8;

  --accent-blue: #0284c7;
  --accent-primary: #2563eb;
  --accent-primary-hover: #1d4ed8;

  --status-ok-bg: #d1fae5;
  --status-ok-text: #065f46;
  --status-error-bg: #fee2e2;
  --status-error-text: #991b1b;
  --token-highlight: #7c3aed;

  --chart-server: #2563eb;
  --chart-client: #16a34a;
  --chart-producer: #d97706;
  --chart-consumer: #9333ea;
  --chart-internal: #6b7280;
  --chart-llm-start: #7c3aed;
  --chart-llm-end: #8b5cf6;

  --scrollbar-thumb: #cbd5e1;
}
```

### Body Background

```css
body {
  background-color: var(--bg-primary);
  color: var(--text-primary);
}
```

### Index.html Flash Prevention

Add an inline script to `web/index.html` to set `data-theme` before render, preventing a flash of wrong theme:

```html
<script>
  document.documentElement.setAttribute('data-theme',
    localStorage.getItem('labubu-theme') || 'dark'
  );
</script>
```

### ThemeToggle Tooltip

- Dark mode active: tooltip shows "Switch to light mode"
- Light mode active: tooltip shows "Switch to dark mode"

### Transition Rule

```css
html {
  transition: background-color 0.25s ease, color 0.25s ease;
}

html *,
html *::before,
html *::after {
  transition: background-color 0.25s ease,
              border-color 0.25s ease,
              color 0.25s ease,
              fill 0.25s ease,
              stroke 0.25s ease;
}
```

## 4. Theme Composable

New file: `web/src/composables/useTheme.ts`

```typescript
import { ref, watch } from 'vue'

type Theme = 'dark' | 'light'
const STORAGE_KEY = 'labubu-theme'

const currentTheme = ref<Theme>(loadTheme())

function loadTheme(): Theme {
  const saved = localStorage.getItem(STORAGE_KEY)
  return saved === 'light' ? 'light' : 'dark'
}

function applyTheme(theme: Theme) {
  document.documentElement.setAttribute('data-theme', theme)
  localStorage.setItem(STORAGE_KEY, theme)
}

function toggleTheme() {
  currentTheme.value = currentTheme.value === 'dark' ? 'light' : 'dark'
}

function setTheme(theme: Theme) {
  currentTheme.value = theme
}

watch(currentTheme, applyTheme, { immediate: true })
applyTheme(currentTheme.value)

export function useTheme() {
  return { theme: currentTheme, toggleTheme, setTheme }
}
```

**Behavior:**
- On load: reads `localStorage('labubu-theme')`, defaults to `'dark'`
- On toggle: flips value, applies `data-theme` attribute, persists to `localStorage`
- Reactive: any component importing `useTheme()` gets reactive `theme` ref

## 5. ThemeToggle Component

New file: `web/src/components/ThemeToggle.vue`

A button in the sidebar footer, visually consistent with the existing language switcher.

```vue
<template>
  <button class="theme-toggle" @click="toggleTheme" :title="tooltip">
    <span class="theme-icon">{{ theme === 'dark' ? '☀️' : '🌙' }}</span>
    <span class="theme-label">{{ theme === 'dark' ? 'Light Mode' : 'Dark Mode' }}</span>
  </button>
</template>
```

**Styling:** Matches the language switcher's look — same background, border, and hover behavior. Icon + label layout for clarity.

## 6. App.vue Changes

- Import and call `useTheme()` to initialize `data-theme` on `<html>` at app startup
- Restructure sidebar bottom: wrap ThemeToggle + lang-switcher in a `<div class="sidebar-footer">`
- Replace hardcoded sidebar colors with CSS variables

```html
<aside class="sidebar">
  <router-link to="/" class="app-title">Labubu</router-link>
  <nav class="app-nav">...</nav>
  <div class="sidebar-footer">
    <ThemeToggle />
    <div class="lang-switcher">...</div>
  </div>
</aside>
```

## 7. Component Migration

### Migration Rules

1. **Only replace color values** — no layout or logic changes
2. Each `<style scoped>` block: replace hex/rgb with `var(--token-name)`
3. Use semantic tokens: `--bg-primary` for main backgrounds, `--text-primary` for text, etc.

### Components to Migrate

| Component | Key Replacements |
|-----------|-----------------|
| `App.vue` | `#000`→`--bg-primary`, `#334155`→`--border-default`, `#38bdf8`→`--accent-blue`, `#1e293b`→`--bg-surface`, `#94a3b8`→`--text-secondary`, `#e2e8f0`→`--text-primary` |
| `TraceList.vue` | `#1e293b`→`--bg-surface`, `#334155`→`--border-default`, `#e2e8f0`→`--text-primary`, `#94a3b8`→`--text-secondary`, `#2563eb`→`--accent-primary`, `#065f46/#6ee7b7`→`--status-ok-*`, `#7f1d1d/#fca5a5`→`--status-error-*` |
| `TraceDetail.vue` | `#000`→`--bg-primary`, `#111`→`--bg-secondary`, `#444`→`--border-strong`, `#38bdf8`→`--accent-blue`, `#c4b5fd`→`--token-highlight` |
| `WaterfallChart.vue` | `#1e293b`→`--bg-surface`, `#334155`→`--border-default`, dot/bar colors→`--chart-*` tokens |
| `Dashboard.vue` | `#000/#111`→`--bg-primary/--bg-secondary`, `#333`→`--border-default`, `#38bdf8`→`--accent-blue`, `#e2e8f0`→`--text-primary`, `#94a3b8`→`--text-secondary` |
| `SessionList.vue` | Same pattern as TraceList |
| `SessionDetail.vue` | Same pattern as TraceDetail |
| `SpanDetail.vue` | Background, border, and text colors |
| `TokenPieChart.vue` | Legend text colors |
| `PanelChart.vue` | Container background, border |
| `PanelForm.vue` | Input/select/button colors |

## 8. Chart Color Adaptation

Waterfall chart bars and dots use semantic `--chart-*` tokens that differ between themes:

| Span Kind | Dark | Light |
|-----------|------|-------|
| SERVER | `#3b82f6` | `#2563eb` (darker blue for white bg) |
| CLIENT | `#22c55e` | `#16a34a` (darker green) |
| PRODUCER | `#f59e0b` | `#d97706` (darker amber) |
| CONSUMER | `#a855f7` | `#9333ea` (darker purple) |
| INTERNAL | `#6b7280` | `#6b7280` (same gray) |
| LLM gradient | `#8b5cf6→#a78bfa` | `#7c3aed→#8b5cf6` (darker purple gradient) |

TokenPieChart segment colors use the same `--chart-*` tokens for consistency.

## 9. Testing Strategy

- **Manual visual check**: toggle between dark/light and verify each page
- **Pages to check**: TraceList, TraceDetail (with waterfall + drawer), Dashboard (with charts), SessionList, SessionDetail
- **Edge cases**: refresh page after toggle (should persist), first visit (should be dark)
- **Transition**: verify smooth 0.25s animation on toggle, no flash/jump

## 10. Persistence

- **Key**: `localStorage('labubu-theme')`
- **Values**: `'dark'` or `'light'`
- **Default**: `'dark'` (when key is absent)
- No cookie or server-side storage
