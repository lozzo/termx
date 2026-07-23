import { afterEach, describe, expect, it } from 'vitest'
import en from './locales/en.json'
import zhCN from './locales/zh-CN.json'
import { muxviaI18n, normalizeMuxviaLanguage } from './i18n'

function localeKeys(value: unknown, prefix = ''): string[] {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return [prefix]
  return Object.entries(value).flatMap(([key, child]) => localeKeys(child, prefix ? `${prefix}.${key}` : key))
}

describe('Muxvia UI i18n', () => {
  afterEach(async () => {
    await muxviaI18n.changeLanguage('en')
  })

  it('normalizes system locales to the two supported languages', () => {
    expect(normalizeMuxviaLanguage('zh-CN')).toBe('zh-CN')
    expect(normalizeMuxviaLanguage('zh-TW')).toBe('zh-CN')
    expect(normalizeMuxviaLanguage('en-US')).toBe('en')
    expect(normalizeMuxviaLanguage('ru-RU')).toBe('en')
  })

  it('switches the primary device and pairing vocabulary without changing domain state', async () => {
    await muxviaI18n.changeLanguage('zh-CN')
    expect(muxviaI18n.t('machines.title')).toBe('设备')
    expect(muxviaI18n.t('pairing.add')).toBe('添加设备')

    await muxviaI18n.changeLanguage('en')
    expect(muxviaI18n.t('machines.title')).toBe('Devices')
    expect(muxviaI18n.t('pairing.add')).toBe('Add device')
  })

  it('keeps English and Simplified Chinese locale keys symmetric', () => {
    expect(localeKeys(zhCN).sort()).toEqual(localeKeys(en).sort())
  })
})
