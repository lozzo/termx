import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import en from './locales/en.json'
import zhCN from './locales/zh-CN.json'

export type MuxviaLanguage = 'en' | 'zh-CN'

export const muxviaLanguages: { id: MuxviaLanguage; label: string }[] = [
  { id: 'en', label: 'English' },
  { id: 'zh-CN', label: '简体中文' },
]

export function normalizeMuxviaLanguage(value?: string | null): MuxviaLanguage {
  return value?.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}

function initialLanguage(): MuxviaLanguage {
  if (typeof window === 'undefined') return 'en'
  try {
    return normalizeMuxviaLanguage(window.localStorage?.getItem('muxvia-language') ?? window.navigator.language)
  } catch {
    return normalizeMuxviaLanguage(window.navigator.language)
  }
}

if (!i18n.isInitialized) {
  void i18n.use(initReactI18next).init({
    resources: { en: { translation: en }, 'zh-CN': { translation: zhCN } },
    lng: initialLanguage(),
    fallbackLng: 'en',
    interpolation: { escapeValue: false },
  })
}

if (typeof document !== 'undefined') {
  document.documentElement.lang = normalizeMuxviaLanguage(i18n.resolvedLanguage)
  i18n.on('languageChanged', (language) => {
    const normalized = normalizeMuxviaLanguage(language)
    try {
      window.localStorage?.setItem('muxvia-language', normalized)
    } catch {
      // 某些 WebView/测试环境禁用 localStorage；语言仍在当前进程内生效。
    }
    document.documentElement.lang = normalized
  })
}

export function muxviaIntlLocale(): string {
  return normalizeMuxviaLanguage(i18n.resolvedLanguage)
}

export { i18n as muxviaI18n }
