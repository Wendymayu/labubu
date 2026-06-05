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
