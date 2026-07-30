// @vitest-environment jsdom

import { App as CapApp } from '@capacitor/app'
import { Capacitor } from '@capacitor/core'
import { addNativeBackHandler } from '@anytty/ui'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { handleAndroidBackButton } from './androidBack'

const appPluginMock = vi.hoisted(() => ({
  addListener: vi.fn(),
  exitApp: vi.fn(),
}))

vi.mock('@capacitor/app', () => ({ App: appPluginMock }))

describe('Android native back bridge', () => {
  beforeEach(() => {
    appPluginMock.exitApp.mockReset().mockResolvedValue(undefined)
    vi.spyOn(Capacitor, 'isNativePlatform').mockReturnValue(true)
    vi.spyOn(Capacitor, 'getPlatform').mockReturnValue('android')
    vi.spyOn(Capacitor, 'isPluginAvailable').mockReturnValue(true)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('does not exit when one registered surface consumes Back', () => {
    const handler = vi.fn(() => true)
    const unregister = addNativeBackHandler(handler, 1)

    handleAndroidBackButton()

    expect(handler).toHaveBeenCalledOnce()
    expect(CapApp.exitApp).not.toHaveBeenCalled()
    unregister()
  })

  it('exits from the Android root with the standard Capacitor App plugin', () => {
    handleAndroidBackButton()

    expect(CapApp.exitApp).toHaveBeenCalledOnce()
  })

  it('does not exit on Web and fails closed when the App plugin is unavailable', () => {
    vi.mocked(Capacitor.isNativePlatform).mockReturnValue(false)
    handleAndroidBackButton()
    expect(CapApp.exitApp).not.toHaveBeenCalled()

    vi.mocked(Capacitor.isNativePlatform).mockReturnValue(true)
    vi.mocked(Capacitor.isPluginAvailable).mockReturnValue(false)
    expect(() => handleAndroidBackButton()).not.toThrow()
    expect(CapApp.exitApp).not.toHaveBeenCalled()
  })

  it('contains synchronous and asynchronous exit plugin failures', async () => {
    appPluginMock.exitApp.mockRejectedValueOnce(new Error('plugin rejected'))
    expect(() => handleAndroidBackButton()).not.toThrow()
    await Promise.resolve()

    appPluginMock.exitApp.mockImplementationOnce(() => { throw new Error('plugin missing') })
    expect(() => handleAndroidBackButton()).not.toThrow()
  })
})
