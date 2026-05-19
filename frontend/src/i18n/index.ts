import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN'
import enUS from './locales/en-US'

const savedLocale = localStorage.getItem('argus-locale') || 'zh-CN'

const i18n = createI18n({
  legacy: false,
  locale: savedLocale,
  fallbackLocale: 'en-US',
  messages: {
    'zh-CN': zhCN,
    'en-US': enUS,
  },
})

export default i18n
export const availableLocales = [
  { code: 'zh-CN', label: '中文' },
  { code: 'en-US', label: 'English' },
]