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
          <button class="nav-group-title" @click="alertsOpen = !alertsOpen">
            {{ t('nav.alerts') }}
            <span class="nav-group-toggle" :class="{ open: alertsOpen }"><i></i><i></i><i></i></span>
          </button>
          <div v-show="alertsOpen" class="nav-group-items">
            <router-link to="/alerts/rules">{{ t('alerts.rules') }}</router-link>
            <router-link to="/alerts/history">{{ t('alerts.history') }}</router-link>
          </div>
        </div>
        <div class="nav-group">
          <button class="nav-group-title" @click="settingsOpen = !settingsOpen">
            {{ t('nav.settings') }}
            <span class="nav-group-toggle" :class="{ open: settingsOpen }"><i></i><i></i><i></i></span>
          </button>
          <div v-show="settingsOpen" class="nav-group-items">
            <router-link to="/settings/pricing">{{ t('nav.modelPricing') }}</router-link>
            <router-link to="/settings/llm-configs">{{ t('nav.llmConfigs') }}</router-link>
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

const alertsOpen = ref(false)
const settingsOpen = ref(false)
</script>

<style scoped>
.app { min-height: 100vh; display: flex; align-items: flex-start; }
.sidebar {
  position: sticky;
  top: 0;
  width: 200px;
  height: 100vh;
  flex-shrink: 0;
  background: var(--bg-primary);
  border-right: 1px solid var(--border-default);
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 20px;
  overflow-y: auto;
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
.nav-group-arrow { font-size: 10px; margin-left: auto; }
.nav-group-toggle {
  display: inline-flex;
  flex-direction: column;
  justify-content: center;
  gap: 2px;
  margin-left: 6px;
  width: 14px;
  height: 14px;
  transition: transform 0.2s ease;
}
.nav-group-toggle i {
  display: block;
  width: 100%;
  height: 1.5px;
  background: var(--text-muted);
  border-radius: 1px;
  transition: background 0.2s, transform 0.25s ease;
}
.nav-group-toggle:hover i { background: var(--text-primary); }
.nav-group-toggle.open { transform: rotate(90deg); }
.nav-group-items { display: flex; flex-direction: column; padding-left: 16px; }
.app-main { flex: 1; padding: 24px; }
.sidebar-footer {
  margin-top: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

</style>
