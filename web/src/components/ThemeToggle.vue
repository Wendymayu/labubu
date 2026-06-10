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
