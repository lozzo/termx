// @vitest-environment jsdom

import { NATIVE_BACK_PRIORITY, addNativeBackHandler, anyttyI18n, dispatchNativeBack } from '@anytty/ui'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { scanPairingCode } from './nativeQrScanner'

const scannerMock = vi.hoisted(() => ({
  start: vi.fn(),
  stop: vi.fn(),
  clear: vi.fn(),
  pause: vi.fn(),
  success: null as ((value: string) => void) | null,
}))

vi.mock('html5-qrcode', () => ({
  Html5Qrcode: class {
    start(_camera: unknown, _config: unknown, success: (value: string) => void) {
      scannerMock.success = success
      return scannerMock.start()
    }
    stop() { return scannerMock.stop() }
    clear() { return scannerMock.clear() }
    pause() { return scannerMock.pause() }
  },
}))

describe('native QR scanner ownership', () => {
  let animationFrames: FrameRequestCallback[]

  beforeEach(async () => {
    scannerMock.start.mockReset()
    scannerMock.stop.mockReset().mockResolvedValue(undefined)
    scannerMock.clear.mockReset()
    scannerMock.pause.mockReset()
    scannerMock.success = null
    document.body.replaceChildren()
    animationFrames = []
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
      animationFrames.push(callback)
      return animationFrames.length
    })
    await anyttyI18n.changeLanguage('en')
  })

  afterEach(() => {
    expect(document.getElementById('anytty-camera-qr-scanner')).toBeNull()
    vi.restoreAllMocks()
  })

  const runAnimationFrames = () => {
    for (const callback of animationFrames.splice(0)) callback(0)
  }

  it('cancels immediately without waiting for a camera start that never settles', async () => {
    const start = deferred<void>()
    scannerMock.start.mockReturnValue(start.promise)
    const previousFocus = document.createElement('button')
    document.body.append(previousFocus)
    previousFocus.focus()
    const underlyingPageBack = vi.fn()
    const unregisterUnderlyingPage = addNativeBackHandler(underlyingPageBack, NATIVE_BACK_PRIORITY.SCANNER)
    const result = scanPairingCode()

    expect(dispatchNativeBack()).toBe(true)
    await expect(result).resolves.toBeNull()
    expect(document.getElementById('anytty-camera-qr-scanner')).toBeNull()
    expect(scannerMock.stop).not.toHaveBeenCalled()
    expect(scannerMock.clear).not.toHaveBeenCalled()
    expect(dispatchNativeBack()).toBe(true)
    expect(underlyingPageBack).toHaveBeenCalledOnce()

    runAnimationFrames()
    expect(document.activeElement).toBe(previousFocus)
    unregisterUnderlyingPage()
    expect(dispatchNativeBack()).toBe(false)
  })

  it('stops and clears exact once when camera start resolves after cancellation', async () => {
    const start = deferred<void>()
    scannerMock.start.mockReturnValue(start.promise)
    const result = scanPairingCode()

    document.querySelector<HTMLButtonElement>('#anytty-camera-qr-scanner button')?.click()
    await expect(result).resolves.toBeNull()
    expect(scannerMock.stop).not.toHaveBeenCalled()
    expect(scannerMock.clear).not.toHaveBeenCalled()

    start.resolve()
    await start.promise
    await Promise.resolve()
    await Promise.resolve()
    expect(scannerMock.stop).toHaveBeenCalledOnce()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
  })

  it('removes prompt ownership immediately while an already-started camera is still stopping', async () => {
    const stop = deferred<void>()
    scannerMock.start.mockResolvedValue(undefined)
    scannerMock.stop.mockReturnValue(stop.promise)
    const result = scanPairingCode()
    await Promise.resolve()

    document.querySelector<HTMLButtonElement>('#anytty-camera-qr-scanner button')?.click()
    await expect(result).resolves.toBeNull()
    expect(document.getElementById('anytty-camera-qr-scanner')).toBeNull()
    expect(dispatchNativeBack()).toBe(false)
    expect(scannerMock.stop).toHaveBeenCalledOnce()
    expect(scannerMock.clear).not.toHaveBeenCalled()

    stop.resolve()
    await stop.promise
    await Promise.resolve()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
  })

  it('clears without stopping when camera start rejects after cancellation', async () => {
    const start = deferred<void>()
    scannerMock.start.mockReturnValue(start.promise)
    const result = scanPairingCode()

    document.querySelector<HTMLButtonElement>('#anytty-camera-qr-scanner button')?.click()
    await expect(result).resolves.toBeNull()
    start.reject(new Error('late camera rejection'))
    await start.promise.catch(() => undefined)
    await Promise.resolve()
    await Promise.resolve()

    expect(scannerMock.stop).not.toHaveBeenCalled()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
  })

  it('shares exact-once cleanup across successful decode and late signals', async () => {
    scannerMock.start.mockResolvedValue(undefined)
    const result = scanPairingCode()
    await Promise.resolve()
    scannerMock.success?.('MXP2-SCANNED')
    scannerMock.success?.('late-result')

    await expect(result).resolves.toBe('MXP2-SCANNED')
    await Promise.resolve()
    expect(scannerMock.pause).toHaveBeenCalledOnce()
    expect(scannerMock.stop).toHaveBeenCalledOnce()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
    expect(animationFrames).toHaveLength(0)
  })

  it('uses the same exact-once cleanup when its React owner aborts on unmount', async () => {
    scannerMock.start.mockResolvedValue(undefined)
    const controller = new AbortController()
    const result = scanPairingCode({ signal: controller.signal })
    await Promise.resolve()

    controller.abort()
    controller.abort()
    scannerMock.success?.('late-result')

    await expect(result).resolves.toBeNull()
    await Promise.resolve()
    expect(scannerMock.stop).toHaveBeenCalledOnce()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
  })

  it('isolates the background, traps Tab, handles Escape, and restores focus after a frame', async () => {
    const background = document.createElement('main')
    const previousFocus = document.createElement('button')
    background.append(previousFocus)
    const preservedBackground = document.createElement('aside')
    preservedBackground.setAttribute('aria-hidden', 'false')
    preservedBackground.setAttribute('inert', 'preserved')
    document.body.append(background, preservedBackground)
    previousFocus.focus()
    scannerMock.start.mockReturnValue(new Promise<void>(() => {}))

    const result = scanPairingCode()
    const scannerRoot = document.getElementById('anytty-camera-qr-scanner')!
    const cancel = scannerRoot.querySelector<HTMLButtonElement>('button')!
    const alternate = document.createElement('button')
    alternate.textContent = 'alternate'
    scannerRoot.append(alternate)

    expect(background.getAttribute('aria-hidden')).toBe('true')
    expect(background.hasAttribute('inert')).toBe(true)
    expect(preservedBackground.getAttribute('aria-hidden')).toBe('true')
    expect(preservedBackground.hasAttribute('inert')).toBe(true)
    expect(document.activeElement).toBe(cancel)

    cancel.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(alternate)
    alternate.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(cancel)

    previousFocus.disabled = true
    cancel.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await expect(result).resolves.toBeNull()
    expect(background.hasAttribute('aria-hidden')).toBe(false)
    expect(background.hasAttribute('inert')).toBe(false)
    expect(preservedBackground.getAttribute('aria-hidden')).toBe('false')
    expect(preservedBackground.getAttribute('inert')).toBe('preserved')
    expect(document.activeElement).not.toBe(previousFocus)

    previousFocus.disabled = false
    runAnimationFrames()
    expect(document.activeElement).toBe(previousFocus)
    expect(dispatchNativeBack()).toBe(false)
  })

  it('clears exact once and restores focus after a startup error', async () => {
    const previousFocus = document.createElement('button')
    document.body.append(previousFocus)
    previousFocus.focus()
    scannerMock.start.mockRejectedValue(new Error('camera unavailable'))
    previousFocus.disabled = true

    await expect(scanPairingCode()).rejects.toThrow('camera unavailable')
    await Promise.resolve()
    expect(scannerMock.stop).not.toHaveBeenCalled()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
    expect(document.activeElement).not.toBe(previousFocus)
    previousFocus.disabled = false
    runAnimationFrames()
    expect(document.activeElement).toBe(previousFocus)
    expect(dispatchNativeBack()).toBe(false)
  })
})

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}
