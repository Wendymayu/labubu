<template>
  <div class="app">
    <aside class="sidebar">
      <router-link to="/" class="app-title">Labubu</router-link>
      <nav class="app-nav">
        <router-link to="/traces">{{ t('nav.traces') }}</router-link>
        <router-link to="/sessions">{{ t('nav.sessions') }}</router-link>
        <router-link to="/dashboards">{{ t('nav.metrics') }}</router-link>
        <router-link to="/logs">{{ t('nav.logs') }}</router-link>
        <div class="nav-group">
          <button class="nav-group-title" @click="settingsOpen = !settingsOpen">
            <span class="nav-group-arrow">{{ settingsOpen ? '▼' : '▶' }}</span>
            Settings
          </button>
          <div v-show="settingsOpen" class="nav-group-items">
            <router-link to="/settings/pricing">Model Pricing</router-link>
          </div>
        </div>
      </nav>
      <div class="sidebar-footer">
        <ThemeToggle />
        <LanguageToggle />
      </div>
    </aside>
    <main class="app-main">
      <router-view />
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from './composables/useTheme'
import ThemeToggle from './components/ThemeToggle.vue'
import LanguageToggle from './components/LanguageToggle.vue'

const { t } = useI18n()
useTheme() // initialize theme

const settingsOpen = ref(false)
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
.nav-group { display: flex; flex-direction: column; }
.nav-group-title {
  background: none; border: none; color: var(--text-secondary); font-size: 14px;
  padding: 6px 0; cursor: pointer; text-align: left; display: flex; align-items: center; gap: 4px;
}
.nav-group-title:hover { color: var(--text-primary); }
.nav-group-arrow { font-size: 10px; width: 12px; }
.nav-group-items { display: flex; flex-direction: column; padding-left: 16px; }
.app-main { flex: 1; padding: 24px; }
.sidebar-footer {
  margin-top: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

</style>
