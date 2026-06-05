import { createI18n } from 'vue-i18n'
import en from './locales/en'
import zh from './locales/zh'

function detectLocale(): string {
  const saved = localStorage.getItem('locale')
  if (saved === 'en' || saved === 'zh') {
    return saved
  }
  return 'en'
}

export const i18n = createI18n({
  legacy: false,
  locale: detectLocale(),
  fallbackLocale: 'en',
  messages: {
    en,
    zh,
  },
})
