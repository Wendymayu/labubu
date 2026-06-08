<template>
  <div class="app">
    <aside class="sidebar">
      <router-link to="/" class="app-title">Labubu</router-link>
      <nav class="app-nav">
        <router-link to="/traces">{{ t('nav.traces') }}</router-link>
        <router-link to="/sessions">{{ t('nav.sessions') }}</router-link>
        <router-link to="/dashboards">{{ t('nav.metrics') }}</router-link>
        <router-link to="/settings/llm-configs">LLM Configs</router-link>
      </nav>
      <div class="lang-switcher">
        <select v-model="locale" @change="onLocaleChange">
          <option value="en">English</option>
          <option value="zh">中文</option>
        </select>
      </div>
    </aside>
    <main class="app-main">
      <router-view />
    </main>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t, locale } = useI18n()

function onLocaleChange() {
  localStorage.setItem('locale', locale.value)
}
</script>

<style scoped>
.app { min-height: 100vh; display: flex; }
.sidebar {
  width: 200px;
  flex-shrink: 0;
  background: #000;
  border-right: 1px solid #334155;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.app-title { font-size: 18px; font-weight: 700; color: #38bdf8; text-decoration: none; }
.app-nav { display: flex; flex-direction: column; gap: 8px; }
.app-nav a { color: #94a3b8; text-decoration: none; font-size: 14px; padding: 6px 0; }
.app-nav a:hover { color: #e2e8f0; }
.app-nav a.router-link-active { color: #38bdf8; }
.app-main { flex: 1; padding: 24px; }
.lang-switcher {
  margin-top: auto;
}
.lang-switcher select {
  width: 100%;
  padding: 6px 10px;
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 6px;
  color: #94a3b8;
  font-size: 13px;
  cursor: pointer;
}
.lang-switcher select:hover {
  border-color: #475569;
  color: #e2e8f0;
}
.lang-switcher select:focus {
  outline: none;
  border-color: #38bdf8;
}
</style>
