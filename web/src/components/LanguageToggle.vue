<template>
  <button
    class="lang-toggle"
    @click="toggleLocale"
    :title="locale === 'zh' ? 'Switch to English' : '切换到中文'"
  >
    <span class="lang-icon">🌐</span>
    <span class="lang-label">{{ locale === 'zh' ? '中文' : 'English' }}</span>
  </button>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { locale } = useI18n()

function toggleLocale() {
  locale.value = locale.value === 'zh' ? 'en' : 'zh'
  localStorage.setItem('locale', locale.value)
}
</script>

<style scoped>
.lang-toggle {
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
.lang-toggle:hover {
  border-color: var(--border-strong);
  color: var(--text-primary);
}
.lang-icon {
  font-size: 14px;
  line-height: 1;
}
.lang-label {
  flex: 1;
  text-align: left;
}
</style>
