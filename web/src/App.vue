<template>
  <div class="app">
    <aside class="sidebar">
      <router-link to="/" class="app-title">Labubu</router-link>
      <nav class="app-nav">
        <router-link to="/traces">{{ t('nav.traces') }}</router-link>
        <router-link to="/sessions">{{ t('nav.sessions') }}</router-link>
        <router-link to="/dashboards">{{ t('nav.metrics') }}</router-link>
        <router-link to="/logs">{{ t('nav.logs') }}</router-link>
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
