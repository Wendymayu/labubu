# Dark/Light Theme Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a light theme to Labubu alongside the existing dark theme, with a toggle button in the sidebar and CSS custom properties for all colors.

**Architecture:** Define a CSS variable token system in a global stylesheet, switch themes via `data-theme` attribute on `<html>`, manage state with a `useTheme` composable, and migrate all hardcoded colors across 12+ components to CSS variables.

**Tech Stack:** Vue 3, CSS Custom Properties, localStorage, Chart.js

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `web/src/styles/theme.css` | CSS variable definitions for dark (default) and light themes + transition rules |
| Create | `web/src/composables/useTheme.ts` | Theme state management, localStorage persistence, reactive ref |
| Create | `web/src/components/ThemeToggle.vue` | Toggle button UI in sidebar footer |
| Modify | `web/index.html` | Flash prevention script + CSS variable fallback in body |
| Modify | `web/src/main.ts` | Import `theme.css` globally |
| Modify | `web/src/App.vue` | Initialize theme, add ThemeToggle, sidebar footer restructure |
| Modify | `web/src/views/TraceList.vue` | Replace hardcoded colors with CSS variables |
| Modify | `web/src/views/TraceDetail.vue` | Replace hardcoded colors with CSS variables |
| Modify | `web/src/views/SessionList.vue` | Replace hardcoded colors with CSS variables |
| Modify | `web/src/views/SessionDetail.vue` | Replace hardcoded colors with CSS variables |
| Modify | `web/src/views/Dashboard.vue` | Replace hardcoded colors with CSS variables |
| Modify | `web/src/components/WaterfallChart.vue` | Replace hardcoded colors with CSS variables (chart semantic tokens) |
| Modify | `web/src/components/SpanDetail.vue` | Replace hardcoded colors with CSS variables |
| Modify | `web/src/components/TokenPieChart.vue` | Replace hardcoded colors with CSS variables, reactive chart colors |
| Modify | `web/src/components/PanelChart.vue` | Replace hardcoded colors with CSS variables, reactive chart colors |
| Modify | `web/src/components/PanelForm.vue` | Replace hardcoded colors with CSS variables |

---

## Task 1: Theme Infrastructure (CSS + Composable + Flash Prevention)

**Files:**
- Create: `web/src/styles/theme.css`
- Create: `web/src/composables/useTheme.ts`
- Modify: `web/index.html`
- Modify: `web/src/main.ts`

- [ ] **Step 1: Create theme.css**

Create `web/src/styles/theme.css`:

```css
/* ===== Dark theme (default) ===== */
:root {
  --bg-primary: #000000;
  --bg-secondary: #111111;
  --bg-surface: #1e293b;
  --bg-surface-deep: #0f172a;
  --bg-surface-hover: #334155;
  --bg-surface-hover-subtle: #1a1a1a;

  --border-default: #334155;
  --border-subtle: #1e293b;
  --border-strong: #475569;
  --border-group: #222222;

  --text-primary: #e2e8f0;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;

  --accent-blue: #38bdf8;
  --accent-primary: #2563eb;
  --accent-primary-hover: #1d4ed8;
  --accent-light: #7dd3fc;

  --status-ok-bg: #065f46;
  --status-ok-text: #6ee7b7;
  --status-error-bg: #7f1d1d;
  --status-error-text: #fca5a5;
  --status-error-accent: #ef4444;
  --status-warning: #fbbf24;
  --token-highlight: #c4b5fd;
  --token-input: #60a5fa;
  --token-green: #6ee7b7;

  --chart-server: #3b82f6;
  --chart-client: #22c55e;
  --chart-producer: #f59e0b;
  --chart-consumer: #a855f7;
  --chart-internal: #6b7280;
  --chart-llm-start: #8b5cf6;
  --chart-llm-end: #a78bfa;

  --chart-pie-system: #8b5cf6;
  --chart-pie-assistant: #ec4899;
  --chart-pie-user: #3b82f6;
  --chart-pie-tool: #06b6d4;
  --chart-pie-tool-defs: #f59e0b;
  --chart-pie-skill: #10b981;
  --chart-pie-output: #ef4444;
  --chart-pie-border: #1a1d27;

  --scrollbar-thumb: #475569;
  --shadow-tooltip: rgba(0, 0, 0, 0.5);
}

/* ===== Light theme ===== */
[data-theme="light"] {
  --bg-primary: #ffffff;
  --bg-secondary: #f1f5f9;
  --bg-surface: #f8fafc;
  --bg-surface-deep: #f1f5f9;
  --bg-surface-hover: #e2e8f0;
  --bg-surface-hover-subtle: #e2e8f0;

  --border-default: #cbd5e1;
  --border-subtle: #e2e8f0;
  --border-strong: #94a3b8;
  --border-group: #e2e8f0;

  --text-primary: #1e293b;
  --text-secondary: #475569;
  --text-muted: #94a3b8;

  --accent-blue: #0284c7;
  --accent-primary: #2563eb;
  --accent-primary-hover: #1d4ed8;
  --accent-light: #0ea5e9;

  --status-ok-bg: #d1fae5;
  --status-ok-text: #065f46;
  --status-error-bg: #fee2e2;
  --status-error-text: #991b1b;
  --status-error-accent: #dc2626;
  --status-warning: #d97706;
  --token-highlight: #7c3aed;
  --token-input: #2563eb;
  --token-green: #059669;

  --chart-server: #2563eb;
  --chart-client: #16a34a;
  --chart-producer: #d97706;
  --chart-consumer: #9333ea;
  --chart-internal: #6b7280;
  --chart-llm-start: #7c3aed;
  --chart-llm-end: #8b5cf6;

  --chart-pie-system: #7c3aed;
  --chart-pie-assistant: #db2777;
  --chart-pie-user: #2563eb;
  --chart-pie-tool: #0891b2;
  --chart-pie-tool-defs: #d97706;
  --chart-pie-skill: #059669;
  --chart-pie-output: #dc2626;
  --chart-pie-border: #e2e8f0;

  --scrollbar-thumb: #cbd5e1;
  --shadow-tooltip: rgba(0, 0, 0, 0.15);
}

/* ===== Body defaults ===== */
body {
  background-color: var(--bg-primary);
  color: var(--text-primary);
}

/* ===== Theme transition ===== */
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

- [ ] **Step 2: Create useTheme composable**

Create `web/src/composables/useTheme.ts`:

```typescript
import { ref, watch } from 'vue'

type Theme = 'dark' | 'light'
const STORAGE_KEY = 'labubu-theme'

function loadTheme(): Theme {
  const saved = localStorage.getItem(STORAGE_KEY)
  return saved === 'light' ? 'light' : 'dark'
}

const currentTheme = ref<Theme>(loadTheme())

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

- [ ] **Step 3: Update index.html for flash prevention**

Replace `web/index.html` with:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link rel="icon" type="image/png" href="/favicon.png" />
  <title>Labubu - Trace Platform</title>
  <script>
    (function() {
      var t = localStorage.getItem('labubu-theme');
      document.documentElement.setAttribute('data-theme', t || 'dark');
    })();
  </script>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      background: var(--bg-primary, #000);
      color: var(--text-primary, #e2e8f0);
    }
  </style>
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/main.ts"></script>
</body>
</html>
```

- [ ] **Step 4: Import theme.css in main.ts**

Edit `web/src/main.ts` to add the theme CSS import:

```typescript
import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'
import './styles/theme.css'

const app = createApp(App)
app.use(router)
app.use(i18n)
app.mount('#app')
```

- [ ] **Step 5: Verify build succeeds**

Run: `cd web && npm run dev` (or `npx vue-tsc --noEmit` for type check)
Expected: No errors. App loads with default dark theme (visually unchanged).

- [ ] **Step 6: Commit**

```bash
git add web/src/styles/theme.css web/src/composables/useTheme.ts web/index.html web/src/main.ts
git commit -m "feat: add theme infrastructure (CSS variables + useTheme composable)"
```

---

## Task 2: ThemeToggle Component + App.vue Integration

**Files:**
- Create: `web/src/components/ThemeToggle.vue`
- Modify: `web/src/App.vue`

- [ ] **Step 1: Create ThemeToggle.vue**

Create `web/src/components/ThemeToggle.vue`:

```vue
<template>
  <button
    class="theme-toggle"
    @click="toggleTheme"
    :title="theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
  >
    <span class="theme-icon">{{ theme === 'dark' ? '☀️' : '🌙' }}</span>
    <span class="theme-label">{{ theme === 'dark' ? 'Light Mode' : 'Dark Mode' }}</span>
  </button>
</template>

<script setup lang="ts">
import { useTheme } from '../composables/useTheme'

const { theme, toggleTheme } = useTheme()
</script>

<style scoped>
.theme-toggle {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
}
.theme-toggle:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}
.theme-icon {
  font-size: 14px;
  line-height: 1;
}
.theme-label {
  flex: 1;
  text-align: left;
}
</style>
```

- [ ] **Step 2: Update App.vue**

Replace `web/src/App.vue` entirely:

```vue
<template>
  <div class="app">
    <aside class="sidebar">
      <router-link to="/" class="app-title">Labubu</router-link>
      <nav class="app-nav">
        <router-link to="/traces">{{ t('nav.traces') }}</router-link>
        <router-link to="/sessions">{{ t('nav.sessions') }}</router-link>
        <router-link to="/dashboards">{{ t('nav.metrics') }}</router-link>
      </nav>
      <div class="sidebar-footer">
        <ThemeToggle />
        <div class="lang-switcher">
          <select v-model="locale" @change="onLocaleChange">
            <option value="en">English</option>
            <option value="zh">中文</option>
          </select>
        </div>
      </div>
    </aside>
    <main class="app-main">
      <router-view />
    </main>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useTheme } from './composables/useTheme'
import ThemeToggle from './components/ThemeToggle.vue'

const { t, locale } = useI18n()
useTheme() // initialize theme

function onLocaleChange() {
  localStorage.setItem('locale', locale.value)
}
</script>

<style scoped>
.app { min-height: 100vh; display: flex; }
.sidebar {
  width: 200px;
  flex-shrink: 0;
  background: var(--bg-primary);
  border-right: 1px solid var(--border-default);
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.app-title { font-size: 18px; font-weight: 700; color: var(--accent-blue); text-decoration: none; }
.app-nav { display: flex; flex-direction: column; gap: 8px; }
.app-nav a { color: var(--text-secondary); text-decoration: none; font-size: 14px; padding: 6px 0; }
.app-nav a:hover { color: var(--text-primary); }
.app-nav a.router-link-active { color: var(--accent-blue); }
.app-main { flex: 1; padding: 24px; }
.sidebar-footer {
  margin-top: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.lang-switcher select {
  width: 100%;
  padding: 6px 10px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
}
.lang-switcher select:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}
.lang-switcher select:focus {
  outline: none;
  border-color: var(--accent-blue);
}
</style>
```

- [ ] **Step 3: Visual verification**

Run: `cd web && npm run dev`
Expected:
- App loads with dark theme (visually unchanged from before)
- Sidebar bottom shows "☀️ Light Mode" button above language switcher
- Clicking it toggles to light mode: white background, dark text
- Clicking again toggles back to dark mode
- Refresh page: theme persists

- [ ] **Step 4: Commit**

```bash
git add web/src/components/ThemeToggle.vue web/src/App.vue
git commit -m "feat: add ThemeToggle component and integrate into App.vue"
```

---

## Task 3: Migrate TraceList.vue

**Files:**
- Modify: `web/src/views/TraceList.vue`

- [ ] **Step 1: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/views/TraceList.vue` (lines 180–204) with:

```css
<style scoped>
.trace-list { max-width: 1400px; }
.filters { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; }
.search-input { flex: 1; min-width: 200px; padding: 8px 12px; background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 6px; color: var(--text-primary); font-size: 14px; }
.filter-select { padding: 8px 12px; background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 6px; color: var(--text-primary); font-size: 14px; }
.btn { padding: 8px 16px; background: var(--bg-surface-hover); border: 1px solid var(--border-strong); border-radius: 6px; color: var(--text-primary); cursor: pointer; font-size: 14px; }
.btn:hover { background: var(--border-strong); }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-primary { background: var(--accent-primary); border-color: var(--accent-primary); }
.btn-primary:hover { background: var(--accent-primary-hover); }
.loading, .error, .empty { text-align: center; padding: 60px 20px; color: var(--text-secondary); }
.error { color: var(--status-error-accent); }
.trace-table { width: 100%; border-collapse: collapse; }
.trace-table th { text-align: left; padding: 10px 12px; font-size: 12px; color: var(--text-secondary); text-transform: uppercase; border-bottom: 1px solid var(--border-default); }
.trace-table td { padding: 10px 12px; font-size: 14px; border-bottom: 1px solid var(--border-subtle); }
.trace-row { cursor: pointer; }
.trace-row:hover { background: var(--bg-surface); }
.cell-name { font-weight: 600; color: var(--accent-blue); max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cell-time { color: var(--text-secondary); font-size: 13px; white-space: nowrap; }
.status-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.status-ok { background: var(--status-ok-bg); color: var(--status-ok-text); }
.status-error { background: var(--status-error-bg); color: var(--status-error-text); }
.pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 20px; }
.page-info { font-size: 14px; color: var(--text-secondary); }
</style>
```

- [ ] **Step 2: Visual verification**

Run: `cd web && npm run dev`
Expected:
- Trace list page looks identical in dark mode
- Toggle to light: white background, light gray inputs, blue primary button, green/red badges with lighter backgrounds
- All text readable in both themes

- [ ] **Step 3: Commit**

```bash
git add web/src/views/TraceList.vue
git commit -m "feat: migrate TraceList.vue to CSS variables"
```

---

## Task 4: Migrate SessionList.vue

**Files:**
- Modify: `web/src/views/SessionList.vue`

- [ ] **Step 1: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/views/SessionList.vue` (lines 175–201) with:

```css
<style scoped>
.session-list { max-width: 1400px; }
.filters { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; }
.search-input { flex: 1; min-width: 200px; padding: 8px 12px; background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 6px; color: var(--text-primary); font-size: 14px; }
.filter-select { padding: 8px 12px; background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 6px; color: var(--text-primary); font-size: 14px; }
.btn { padding: 8px 16px; background: var(--bg-surface-hover); border: 1px solid var(--border-strong); border-radius: 6px; color: var(--text-primary); cursor: pointer; font-size: 14px; }
.btn:hover { background: var(--border-strong); }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-primary { background: var(--accent-primary); border-color: var(--accent-primary); }
.btn-primary:hover { background: var(--accent-primary-hover); }
.loading, .error, .empty { text-align: center; padding: 60px 20px; color: var(--text-secondary); }
.error { color: var(--status-error-accent); }
.trace-table { width: 100%; border-collapse: collapse; }
.trace-table th { text-align: left; padding: 10px 12px; font-size: 12px; color: var(--text-secondary); text-transform: uppercase; border-bottom: 1px solid var(--border-default); }
.trace-table td { padding: 10px 12px; font-size: 14px; border-bottom: 1px solid var(--border-subtle); }
.trace-row { cursor: pointer; }
.trace-row:hover { background: var(--bg-surface); }
.cell-session-id { font-family: 'Courier New', monospace; font-size: 13px; color: var(--accent-blue); max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cell-tokens { color: var(--token-highlight); font-weight: 600; }
.cell-time { color: var(--text-secondary); font-size: 13px; white-space: nowrap; }
.error-rate { font-weight: 600; font-size: 13px; }
.error-high { color: var(--status-error-text); }
.error-medium { color: var(--status-warning); }
.error-none { color: var(--status-ok-text); }
.pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 20px; }
.page-info { font-size: 14px; color: var(--text-secondary); }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/SessionList.vue
git commit -m "feat: migrate SessionList.vue to CSS variables"
```

---

## Task 5: Migrate TraceDetail.vue

**Files:**
- Modify: `web/src/views/TraceDetail.vue`

- [ ] **Step 1: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/views/TraceDetail.vue` (lines 233–391) with:

```css
<style scoped>
.trace-detail { max-width: 1600px; }
.back-link { margin-bottom: 16px; }
.back-link a { color: var(--text-secondary); text-decoration: none; font-size: 14px; }
.back-link a:hover { color: var(--text-primary); }
.loading, .error { text-align: center; padding: 60px; color: var(--text-secondary); }
.error { color: var(--status-error-accent); }
.trace-summary { margin-bottom: 24px; }
.trace-summary h2 { font-size: 20px; margin-bottom: 12px; }
.summary-grid { display: flex; gap: 24px; flex-wrap: wrap; }
.summary-item { display: flex; flex-direction: column; }
.summary-label { font-size: 11px; color: var(--text-secondary); text-transform: uppercase; }
.summary-value { font-size: 14px; }
.mono { font-family: 'Courier New', monospace; font-size: 12px; word-break: break-all; }
.token-highlight { color: var(--token-highlight); font-weight: 600; }

.download-group {
  display: flex;
  gap: 0;
  align-self: center;
}
.btn-download {
  padding: 6px 12px;
  border: 1px solid var(--border-group);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 12px;
}
.btn-download:first-child {
  border-radius: 6px 0 0 6px;
}
.btn-download:last-child {
  border-radius: 0 6px 6px 0;
}
.btn-download:hover {
  border-color: var(--accent-blue);
  color: var(--accent-blue);
}

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
  color: var(--text-muted);
  font-size: 12px;
  padding: 24px 0;
}

/* === Drawer === */
.detail-drawer {
  width: 480px;
  flex-shrink: 0;
  border-left: 1px solid var(--border-strong);
  background: var(--bg-primary);
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
  border-bottom: 1px solid var(--border-strong);
  flex-shrink: 0;
}
.drawer-title {
  min-width: 0;
}
.drawer-span-name {
  display: block;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.drawer-span-id {
  display: block;
  font-size: 10px;
  color: var(--text-muted);
  font-family: 'Courier New', monospace;
  margin-top: 2px;
}
.drawer-close {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 18px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
  line-height: 1;
}
.drawer-close:hover {
  color: var(--text-primary);
  background: var(--bg-surface-hover-subtle);
}

.drawer-body {
  padding: 16px;
  overflow-y: auto;
  flex: 1;
}

.drawer-body::-webkit-scrollbar { width: 4px; }
.drawer-body::-webkit-scrollbar-track { background: transparent; }
.drawer-body::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 2px; }

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
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/TraceDetail.vue
git commit -m "feat: migrate TraceDetail.vue to CSS variables"
```

---

## Task 6: Migrate SessionDetail.vue

**Files:**
- Modify: `web/src/views/SessionDetail.vue`

- [ ] **Step 1: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/views/SessionDetail.vue` (lines 131–171) with:

```css
<style scoped>
.session-detail { max-width: 1200px; }
.back-link { margin-bottom: 16px; }
.back-link a { color: var(--text-secondary); text-decoration: none; font-size: 14px; }
.back-link a:hover { color: var(--text-primary); }
.loading, .error { text-align: center; padding: 60px; color: var(--text-secondary); }
.error { color: var(--status-error-accent); }
.session-summary { margin-bottom: 24px; }
.session-summary h2 { font-size: 20px; margin-bottom: 12px; word-break: break-all; }
.summary-grid { display: flex; gap: 24px; flex-wrap: wrap; }
.summary-item { display: flex; flex-direction: column; }
.summary-label { font-size: 11px; color: var(--text-secondary); text-transform: uppercase; }
.summary-value { font-size: 14px; }
.token-highlight { color: var(--token-highlight); font-weight: 600; }
.error-high { color: var(--status-error-text); }
.error-medium { color: var(--status-warning); }
.error-ok { color: var(--status-ok-text); }

.turns-heading { font-size: 16px; margin-bottom: 12px; color: var(--text-primary); }

.turns-list { display: flex; flex-direction: column; gap: 2px; }
.turn-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-subtle);
  cursor: pointer;
  font-size: 14px;
}
.turn-row:hover { background: var(--bg-surface); }
.turn-number { color: var(--text-muted); font-size: 12px; font-weight: 600; min-width: 32px; }
.turn-name { flex: 1; font-weight: 600; color: var(--accent-blue); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.status-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.status-ok { background: var(--status-ok-bg); color: var(--status-ok-text); }
.status-error { background: var(--status-error-bg); color: var(--status-error-text); }
.turn-duration { color: var(--text-secondary); min-width: 70px; text-align: right; }
.turn-tokens { color: var(--token-highlight); font-weight: 600; min-width: 60px; text-align: right; }
.turn-service { color: var(--text-secondary); font-size: 13px; min-width: 100px; }
.turn-time { color: var(--text-muted); font-size: 13px; min-width: 80px; text-align: right; }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/SessionDetail.vue
git commit -m "feat: migrate SessionDetail.vue to CSS variables"
```

---

## Task 7: Migrate Dashboard.vue

**Files:**
- Modify: `web/src/views/Dashboard.vue`

- [ ] **Step 1: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/views/Dashboard.vue` (lines 147–188) with:

```css
<style scoped>
.dashboard-page { max-width: 1400px; margin: 0 auto; }
.dashboard-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 24px; flex-wrap: wrap; gap: 12px;
}
.time-presets { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.btn-preset {
  padding: 6px 16px; border: 1px solid var(--border-group); background: var(--bg-secondary);
  color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 13px;
}
.btn-preset.active { background: var(--accent-blue); color: var(--bg-primary); border-color: var(--accent-blue); }
.btn-preset:hover:not(.active) { border-color: var(--accent-blue); color: var(--accent-blue); }
.custom-range { display: flex; align-items: center; gap: 8px; }
.custom-range input {
  background: var(--bg-primary); border: 1px solid var(--border-group); border-radius: 6px;
  color: var(--text-primary); padding: 6px 10px; font-size: 13px;
}
.custom-range span { color: var(--text-secondary); font-size: 13px; }
.btn {
  padding: 8px 20px; border-radius: 6px; border: none;
  font-size: 14px; cursor: pointer; font-weight: 500;
}
.btn-primary { background: var(--accent-blue); color: var(--bg-primary); }
.btn-primary:hover { background: var(--accent-light); }
.btn-refresh {
  background: var(--bg-secondary); border: 1px solid var(--border-group); color: var(--text-primary);
}
.btn-refresh:hover { background: var(--bg-surface-hover-subtle); border-color: var(--accent-blue); }
.panel-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 20px;
}
@media (max-width: 960px) {
  .panel-grid { grid-template-columns: 1fr; }
}
.page-state {
  text-align: center; padding: 80px 20px; color: var(--text-secondary); font-size: 15px;
}
.page-error { color: var(--status-error-accent); }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/Dashboard.vue
git commit -m "feat: migrate Dashboard.vue to CSS variables"
```

---

## Task 8: Migrate WaterfallChart.vue

**Files:**
- Modify: `web/src/components/WaterfallChart.vue`

- [ ] **Step 1: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/components/WaterfallChart.vue` (lines 142–176) with:

```css
<style scoped>
.waterfall { font-size: 13px; }
.waterfall-header { display: flex; padding: 8px; font-size: 11px; color: var(--text-secondary); text-transform: uppercase; border-bottom: 1px solid var(--border-default); }
.waterfall-row { display: flex; align-items: center; padding: 4px 0; cursor: pointer; border-bottom: 1px solid var(--bg-surface-deep); }
.waterfall-row:hover { background: var(--bg-surface); }
.waterfall-row.selected {
  background: #1e3a5f;
  outline: 1px solid var(--accent-blue);
  outline-offset: -1px;
}
[data-theme="light"] .waterfall-row.selected {
  background: #dbeafe;
}
.col-name { flex: 0 0 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.col-timeline { flex: 1; position: relative; height: 20px; }
.col-duration { flex: 0 0 80px; text-align: right; font-variant-numeric: tabular-nums; color: var(--text-secondary); }
.col-tokens { flex: 0 0 100px; text-align: right; }
.toggle-icon { cursor: pointer; margin-right: 4px; font-size: 10px; color: var(--text-muted); }
.kind-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; }
.dot-server { background: var(--chart-server); }
.dot-client { background: var(--chart-client); }
.dot-producer { background: var(--chart-producer); }
.dot-consumer { background: var(--chart-consumer); }
.dot-internal { background: var(--chart-internal); }
.bar { display: inline-block; height: 14px; border-radius: 3px; min-width: 2px; vertical-align: middle; }
.bar-server { background: var(--chart-server); }
.bar-client { background: var(--chart-client); }
.bar-producer { background: var(--chart-producer); }
.bar-consumer { background: var(--chart-consumer); }
.bar-internal { background: var(--chart-internal); }
.bar-llm { background: linear-gradient(90deg, var(--chart-llm-start), var(--chart-llm-end)); }
.token-badge { font-size: 11px; color: var(--token-highlight); }
.selected-marker {
  color: var(--accent-blue);
  margin-left: 6px;
  font-size: 10px;
}
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/WaterfallChart.vue
git commit -m "feat: migrate WaterfallChart.vue to CSS variables"
```

---

## Task 9: Migrate SpanDetail.vue

**Files:**
- Modify: `web/src/components/SpanDetail.vue`

- [ ] **Step 1: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/components/SpanDetail.vue` (lines 342–624) with:

```css
<style scoped>
.span-detail {
  background: var(--bg-primary);
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
  background: var(--bg-surface-deep);
  border-radius: 6px;
  padding: 10px 8px;
  text-align: center;
}
.qi-label {
  font-size: 10px;
  color: var(--text-muted);
  text-transform: uppercase;
  margin-bottom: 4px;
}
.qi-value {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.qi-model {
  font-size: 11px;
  color: var(--accent-blue);
  word-break: break-all;
}
.kind-server { color: var(--chart-server); }
.kind-client { color: var(--chart-client); }
.kind-producer { color: var(--chart-producer); }
.kind-consumer { color: var(--chart-consumer); }
.kind-internal { color: var(--text-secondary); }
.status-ok { color: var(--status-ok-text); }
.status-error { color: var(--status-error-text); }

.status-msg {
  background: var(--status-error-bg);
  color: var(--status-error-text);
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
  border-bottom: 1px solid var(--border-group);
}
.ts-item {
  text-align: center;
  flex: 1;
}
.ts-label {
  display: block;
  font-size: 10px;
  color: var(--text-muted);
  text-transform: uppercase;
}
.ts-val {
  font-size: 16px;
  font-weight: 700;
  color: var(--text-primary);
}
.ts-highlight {
  color: var(--token-highlight);
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
  color: var(--text-secondary);
  text-transform: uppercase;
  margin: 0;
}
.attr-search {
  background: var(--bg-surface-deep);
  border: 1px solid var(--border-group);
  border-radius: 4px;
  padding: 4px 10px;
  font-size: 11px;
  color: var(--text-primary);
  width: 170px;
}
.attr-search::placeholder {
  color: var(--text-muted);
}
.attr-search:focus {
  outline: none;
  border-color: var(--accent-blue);
}
.attr-empty {
  text-align: center;
  color: var(--text-muted);
  font-size: 12px;
  padding: 12px 0;
}

.attr-group {
  border: 1px solid var(--border-group);
  border-radius: 4px;
  overflow: hidden;
  margin-bottom: 4px;
}
.attr-group-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 10px;
  background: var(--bg-secondary);
  cursor: pointer;
  font-size: 12px;
  color: var(--text-primary);
  user-select: none;
}
.attr-group-header:hover {
  background: var(--bg-surface-hover-subtle);
}
.attr-group-count {
  font-size: 10px;
  color: var(--text-muted);
}
.attr-group-body {
  padding: 2px 0;
}
.attr-row {
  display: flex;
  padding: 3px 10px;
  border-bottom: 1px solid var(--bg-surface-deep);
  font-size: 11px;
}
.attr-row:last-child {
  border-bottom: none;
}
.attr-key {
  color: var(--text-muted);
  width: 170px;
  flex-shrink: 0;
  word-break: break-all;
}
.attr-value {
  color: var(--text-primary);
  word-break: break-all;
  flex: 1;
}
.attr-empty-val {
  color: var(--text-muted);
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
  background: var(--border-group);
}

.tl-card {
  position: relative;
  background: var(--bg-surface-deep);
  border-radius: 4px;
  padding: 8px 10px;
  margin-bottom: 8px;
  border-left: 3px solid var(--chart-internal);
}
.tl-card-toolcall { border-left-color: var(--chart-client); }
.tl-card-toolresult { border-left-color: var(--chart-producer); }
.tl-card-error { border-left-color: var(--status-error-accent); }
.tl-card-default { border-left-color: var(--chart-internal); }

.tl-dot {
  position: absolute;
  left: -15px;
  top: 10px;
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
.tl-dot-toolcall { background: var(--chart-client); }
.tl-dot-toolresult { background: var(--chart-producer); }
.tl-dot-error { background: var(--status-error-accent); }
.tl-dot-default { background: var(--chart-internal); }

.tl-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
}
.tl-time {
  font-size: 10px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.evt-name-toolcall { color: var(--chart-client); }
.evt-name-toolresult { color: var(--chart-producer); }
.evt-name-error { color: var(--status-error-text); }

.tl-attrs {
  margin-top: 6px;
}
.tl-attr-row {
  margin-bottom: 4px;
}
.tl-attr-key {
  display: block;
  font-size: 10px;
  color: var(--text-muted);
  margin-bottom: 2px;
}
.tl-attr-value {
  font-size: 11px;
  color: var(--text-primary);
  word-break: break-all;
}

.tl-code-toggle {
  font-size: 10px;
  color: var(--text-secondary);
  cursor: pointer;
  margin-bottom: 2px;
}
.tl-code-toggle:hover {
  color: var(--text-primary);
}
.tl-copy-inline {
  margin-left: 8px;
  cursor: pointer;
  font-size: 10px;
}
.tl-code {
  background: var(--bg-primary);
  border-radius: 3px;
  padding: 6px 8px;
  font-size: 10px;
  overflow-x: auto;
  max-height: 250px;
  overflow-y: auto;
  margin: 0;
  font-family: 'Courier New', monospace;
  color: var(--text-primary);
  line-height: 1.5;
}
.tl-code code {
  font-family: inherit;
}
.tl-code :deep(.j-key) { color: var(--text-secondary); }
.tl-code :deep(.j-str) { color: var(--token-green); }
.tl-code :deep(.j-num) { color: var(--status-warning); }
.tl-code :deep(.j-bool) { color: var(--chart-pie-assistant); }

/* --- Scrollbar --- */
.tl-code::-webkit-scrollbar { width: 3px; height: 3px; }
.tl-code::-webkit-scrollbar-track { background: transparent; }
.tl-code::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 2px; }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/SpanDetail.vue
git commit -m "feat: migrate SpanDetail.vue to CSS variables"
```

---

## Task 10: Migrate TokenPieChart.vue

**Files:**
- Modify: `web/src/components/TokenPieChart.vue`

This component has hardcoded colors in both the `<script>` section (COLORS map + Chart.js config) and the `<style>` section. The script colors need to read from CSS variables at runtime.

- [ ] **Step 1: Update script COLORS to read CSS variables**

Replace the `COLORS` constant (lines 67–75) and the `createChart` function (lines 125–178) with theme-aware versions. Replace lines 67–75:

```typescript
// --- Colors (read from CSS variables) ---
function getCSSVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function getColors(): Record<string, string> {
  return {
    system:           getCSSVar('--chart-pie-system'),
    assistant:        getCSSVar('--chart-pie-assistant'),
    user:             getCSSVar('--chart-pie-user'),
    tool:             getCSSVar('--chart-pie-tool'),
    tool_definitions: getCSSVar('--chart-pie-tool-defs'),
    skill:            getCSSVar('--chart-pie-skill'),
    output:           getCSSVar('--chart-pie-output'),
  }
}
```

Then replace the `createChart` function (lines 125–178) with:

```typescript
function createChart() {
  if (!canvasRef.value || segments.value.length === 0) return

  if (chart) {
    chart.destroy()
    chart = null
  }

  const colors = getColors()
  const borderColor = getCSSVar('--chart-pie-border')
  const tooltipBg = getCSSVar('--bg-secondary')
  const tooltipTitle = getCSSVar('--text-primary')
  const tooltipBody = getCSSVar('--text-secondary')
  const tooltipBorder = getCSSVar('--border-group')

  const labels = segments.value.map(s => s.label)
  const data = segments.value.map(s => s.value)
  const bgColors = segments.value.map(s => colors[s.key] || getCSSVar('--chart-internal'))

  chart = new Chart(canvasRef.value, {
    type: 'doughnut',
    data: {
      labels,
      datasets: [{
        data,
        backgroundColor: bgColors,
        borderColor: borderColor,
        borderWidth: 2,
        hoverOffset: 6,
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: true,
      cutout: '55%',
      plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: tooltipBg,
          titleColor: tooltipTitle,
          bodyColor: tooltipBody,
          borderColor: tooltipBorder,
          borderWidth: 1,
          padding: 12,
          cornerRadius: 6,
          callbacks: {
            label: (ctx: any) => {
              const total = ctx.dataset.data.reduce((a: number, b: number) => a + b, 0)
              const pct = ((ctx.parsed / total) * 100).toFixed(1)
              return ` ${ctx.label}: ${ctx.parsed.toLocaleString()} (${pct}%)`
            }
          }
        }
      },
      animation: {
        animateRotate: true,
        duration: 600,
      }
    }
  })
}
```

Also update the `segments` computed to use the dynamic colors:

Replace line 101 (`out.push({ key, label, value, color: COLORS[key], pct: '' })`) with:

```typescript
      const colors = getColors()
      out.push({ key, label, value, color: colors[key] || getCSSVar('--chart-internal'), pct: '' })
```

And update the output segment (line 109):

```typescript
    const colors = getColors()
    out.push({ key: 'output', label: 'Output Tokens', value: outputVal, color: colors['output'], pct: '' })
```

Add a watcher to re-render chart when theme changes. Add the import at the top of the `<script setup>` block (near the existing imports on line 49):

```typescript
import { useTheme } from '../composables/useTheme'
```

Add these lines after the existing `watch(segments, ...)` block (around line 182):

```typescript
const { theme } = useTheme()
watch(theme, () => {
  requestAnimationFrame(createChart)
})
```

- [ ] **Step 2: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/components/TokenPieChart.vue` (lines 196–277) with:

```css
<style scoped>
.token-chart {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 12px;
}
.chart-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 12px;
}
.chart-container {
  position: relative;
  width: 100%;
  max-width: 240px;
  margin: 0 auto;
}

.token-legend {
  margin-top: 14px;
}
.legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 12px;
}
.legend-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}
.legend-label {
  flex: 1;
  color: var(--text-secondary);
}
.legend-value {
  font-weight: 600;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}
.legend-pct {
  color: var(--text-muted);
  font-size: 11px;
  width: 40px;
  text-align: right;
}

.token-summary {
  display: flex;
  gap: 10px;
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid var(--border-default);
}
.summary-item {
  flex: 1;
  text-align: center;
}
.s-label {
  font-size: 10px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.s-value {
  font-size: 16px;
  font-weight: 700;
  margin-top: 2px;
  font-variant-numeric: tabular-nums;
}
.s-value.purple { color: var(--token-highlight); }
.s-value.green { color: var(--token-green); }
.s-value.blue { color: var(--token-input); }
</style>
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/TokenPieChart.vue
git commit -m "feat: migrate TokenPieChart.vue to CSS variables with reactive chart colors"
```

---

## Task 11: Migrate PanelChart.vue

**Files:**
- Modify: `web/src/components/PanelChart.vue`

This component has hardcoded colors in Chart.js configuration and tooltip styles. Chart colors need to read CSS variables.

- [ ] **Step 1: Update renderChart to read CSS variables**

Replace the `COLORS` constant (lines 120–123) and the `renderChart` function (lines 201–267). First, add a helper:

```typescript
function getCSSVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}
```

Replace the COLORS array and the relevant parts of `renderChart`. Replace lines 120–123:

```typescript
const COLORS = [
  '#38bdf8', '#f472b6', '#a78bfa', '#fb923c', '#4ade80',
  '#facc15', '#fb7185', '#2dd4bf', '#e2e8f0', '#94a3b8',
]
```

This can remain as-is since these are multi-series line colors, not semantic chart colors. They work on both light and dark backgrounds. But update the Chart.js config inside `renderChart` to read CSS variables for axis/grid/legend colors.

In `renderChart`, replace the `options` object (starting around line 238) with:

```typescript
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      plugins: {
        legend: {
          display: datasets.length > 1,
          position: 'bottom',
          labels: { color: getCSSVar('--text-secondary'), font: { size: 10 }, boxWidth: 12, padding: 8 },
        },
        tooltip: {
          enabled: false,
          mode: 'index',
          intersect: false,
          position: 'nearest',
          external: externalTooltipHandler,
        },
      },
      scales: {
        x: {
          ticks: { color: getCSSVar('--text-secondary'), maxTicksLimit: 8, font: { size: 10 } },
          grid: { color: getCSSVar('--border-subtle') },
        },
        y: {
          ticks: { color: getCSSVar('--text-secondary'), font: { size: 10 } },
          grid: { color: getCSSVar('--border-subtle') },
        },
      },
    },
```

Also add a theme watcher to re-render chart on theme change. Add this import at the top of the `<script setup>` block (near existing imports on line 25):

```typescript
import { useTheme } from '../composables/useTheme'
```

Add these lines after the existing `watch` call (around line 300):

```typescript
const { theme } = useTheme()
watch(theme, () => {
  if (chart) {
    chart.destroy()
    chart = null
  }
  fetchData()
})
```

- [ ] **Step 2: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/components/PanelChart.vue` (lines 310–405) with:

```css
<style scoped>
.panel-chart {
  background: var(--bg-primary);
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  overflow: hidden;
}
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-strong);
}
.panel-title { font-size: 14px; font-weight: 600; color: var(--text-primary); margin: 0; }
.panel-actions { display: flex; gap: 4px; }
.btn-icon {
  background: none; border: none; color: var(--text-muted); cursor: pointer;
  font-size: 14px; padding: 4px; border-radius: 4px; line-height: 1;
}
.btn-icon:hover { color: var(--text-primary); background: var(--bg-surface-hover-subtle); }
.panel-body { padding: 16px; height: 280px; position: relative; }
.panel-body canvas { width: 100% !important; height: 100% !important; }
.panel-state {
  display: flex; align-items: center; justify-content: center;
  height: 100%; color: var(--text-secondary); font-size: 14px;
}
.panel-error { color: var(--status-error-accent); }
.stat-value {
  display: flex; flex-direction: column; align-items: center;
  justify-content: center; height: 100%;
}
.stat-number { font-size: 48px; font-weight: 700; color: var(--accent-blue); line-height: 1.2; }
.stat-metric { font-size: 12px; color: var(--text-secondary); margin-top: 8px; }

.chart-tooltip {
  position: fixed;
  pointer-events: auto;
  opacity: 0;
  z-index: 9999;
  background: var(--bg-secondary);
  border: 1px solid var(--border-group);
  border-radius: 6px;
  padding: 8px 10px;
  min-width: 160px;
  max-width: 280px;
  max-height: 220px;
  overflow-y: auto;
  font-size: 12px;
  box-shadow: 0 4px 16px var(--shadow-tooltip);
  transition: opacity 0.15s;
}
.chart-tooltip::-webkit-scrollbar { width: 4px; }
.chart-tooltip::-webkit-scrollbar-track { background: transparent; }
.chart-tooltip::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 2px; }

.tt-time {
  color: var(--text-secondary);
  font-size: 11px;
  margin-bottom: 6px;
  padding-bottom: 4px;
  border-bottom: 1px solid var(--border-group);
}
.tt-body {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.tt-item {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}
.tt-color {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-top: 3px;
  flex-shrink: 0;
}
.tt-labels {
  flex: 1;
  min-width: 0;
}
.tt-label {
  color: var(--text-primary);
  line-height: 1.5;
  word-break: break-all;
}
.tt-value {
  color: var(--accent-blue);
  font-weight: 600;
  flex-shrink: 0;
  margin-left: auto;
}
</style>
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/PanelChart.vue
git commit -m "feat: migrate PanelChart.vue to CSS variables with reactive chart"
```

---

## Task 12: Migrate PanelForm.vue

**Files:**
- Modify: `web/src/components/PanelForm.vue`

- [ ] **Step 1: Replace `<style scoped>` block**

Replace the entire `<style scoped>` block in `web/src/components/PanelForm.vue` (lines 172–212) with:

```css
<style scoped>
.modal-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.7);
  display: flex; align-items: center; justify-content: center;
  z-index: 1000;
}
.modal-content {
  background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 12px;
  padding: 24px; width: 480px; max-height: 90vh; overflow-y: auto;
}
.modal-title { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0 0 20px 0; }
.form-group { margin-bottom: 16px; }
.form-group label { display: block; font-size: 13px; color: var(--text-secondary); margin-bottom: 6px; }
.form-group input, .form-group select {
  width: 100%; padding: 8px 12px; background: var(--bg-surface-deep); border: 1px solid var(--border-default);
  border-radius: 6px; color: var(--text-primary); font-size: 14px; box-sizing: border-box;
}
.form-group input:focus, .form-group select:focus { border-color: var(--accent-blue); outline: none; }
.label-row { display: flex; gap: 8px; margin-bottom: 8px; }
.label-row select { flex: 1; }
.btn-remove {
  background: none; border: none; color: var(--status-error-accent); cursor: pointer;
  font-size: 16px; padding: 0 8px;
}
.btn-add-label {
  background: none; border: 1px dashed var(--border-default); color: var(--text-secondary);
  padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; width: 100%;
}
.btn-add-label:hover { border-color: var(--accent-blue); color: var(--accent-blue); }
.modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 20px; }
.btn {
  padding: 8px 20px; border-radius: 6px; border: none;
  font-size: 14px; cursor: pointer; font-weight: 500;
}
.btn-primary { background: var(--accent-blue); color: var(--bg-primary); }
.btn-primary:hover { background: var(--accent-light); }
.btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-secondary { background: var(--bg-surface-hover); color: var(--text-primary); }
.btn-secondary:hover { background: var(--border-strong); }
.form-error { color: var(--status-error-accent); font-size: 13px; margin-top: 8px; }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/PanelForm.vue
git commit -m "feat: migrate PanelForm.vue to CSS variables"
```

---

## Task 13: Final Verification + Build

**Files:** (no new changes, verification only)

- [ ] **Step 1: Type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: No TypeScript errors.

- [ ] **Step 2: Build frontend**

Run: `cd web && npm run build`
Expected: Build succeeds with no errors.

- [ ] **Step 3: Visual verification — dark mode**

Run: `cd web && npm run dev`
Open http://localhost:3001 and verify:
- All pages render correctly in dark mode (unchanged from before)
- TraceList: dark inputs, dark table, green/red badges
- TraceDetail: waterfall with colored bars, slide-in drawer with dark background
- SessionList/SessionDetail: matches TraceList/TraceDetail styling
- Dashboard: dark panels, chart with correct colors, dark toolbar
- SpanDetail: dark attribute groups, code blocks, event timeline
- TokenPieChart: colored donut with dark container
- PanelForm modal: dark form inputs

- [ ] **Step 4: Visual verification — light mode**

Click the "☀️ Light Mode" toggle and verify:
- Background turns white, text turns dark
- All inputs have light gray backgrounds
- Tables have light borders
- Status badges use light backgrounds (pale green/red)
- Waterfall chart bars use darker/more saturated colors
- Dashboard panels have white backgrounds
- TokenPieChart has light border and background
- PanelForm modal has light surface background
- Smooth 0.25s transition animation on toggle

- [ ] **Step 5: Verify persistence**

- Toggle to light mode
- Refresh the page
- Expected: page loads in light mode (no flash of dark)
- Toggle back to dark mode
- Refresh again
- Expected: page loads in dark mode

- [ ] **Step 6: Final commit (if any touch-ups needed)**

```bash
git add -A
git commit -m "fix: theme visual polish touch-ups"
```

Or if no changes needed, skip this step.

- [ ] **Step 7: Push to remote**

```bash
git push origin develop
```
