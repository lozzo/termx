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
  beforeEach(async () => {
    scannerMock.start.mockReset()
    scannerMock.stop.mockReset().mockResolvedValue(undefined)
    scannerMock.clear.mockReset()
    scannerMock.pause.mockReset()
    scannerMock.success = null
    document.body.replaceChildren()
    await anyttyI18n.changeLanguage('en')
  })

  afterEach(() => {
    expect(document.getElementById('anytty-camera-qr-scanner')).toBeNull()
  })

  it('keeps the scanner surface mounted until Back stops and clears the camera exact once', async () => {
    const start = deferred<void>()
    const stop = deferred<void>()
    scannerMock.start.mockReturnValue(start.promise)
    scannerMock.stop.mockReturnValue(stop.promise)
    const previousFocus = document.createElement('button')
    document.body.append(previousFocus)
    previousFocus.focus()
    const underlyingPageBack = vi.fn(() => true)
    const unregisterUnderlyingPage = addNativeBackHandler(underlyingPageBack, NATIVE_BACK_PRIORITY.SCANNER)
    const result = scanPairingCode()
    start.resolve()
    await start.promise

    expect(dispatchNativeBack()).toBe(true)
    await Promise.resolve()
    expect(scannerMock.stop).toHaveBeenCalledOnce()
    expect(scannerMock.clear).not.toHaveBeenCalled()
    expect(document.getElementById('anytty-camera-qr-scanner')).not.toBeNull()
    expect(dispatchNativeBack()).toBe(true)
    expect(underlyingPageBack).not.toHaveBeenCalled()

    stop.resolve()
    await expect(result).resolves.toBeNull()
    expect(scannerMock.stop).toHaveBeenCalledOnce()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
    expect(document.activeElement).toBe(previousFocus)
    unregisterUnderlyingPage()
    expect(dispatchNativeBack()).toBe(false)
  })

  it('shares one idempotent cleanup across Cancel and repeated completion signals', async () => {
    scannerMock.start.mockResolvedValue(undefined)
    const controller = new AbortController()
    const result = scanPairingCode({ signal: controller.signal })
    await Promise.resolve()

    document.querySelector<HTMLButtonElement>('#anytty-camera-qr-scanner button')?.click()
    controller.abort()
    scannerMock.success?.('late-result')

    await expect(result).resolves.toBeNull()
    expect(scannerMock.stop).toHaveBeenCalledOnce()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
  })

  it('cleans a successful scan before publishing its value', async () => {
    scannerMock.start.mockResolvedValue(undefined)
    const result = scanPairingCode()
    await Promise.resolve()
    scannerMock.success?.('MXP2-SCANNED')

    await expect(result).resolves.toBe('MXP2-SCANNED')
    expect(scannerMock.pause).toHaveBeenCalledOnce()
    expect(scannerMock.stop).toHaveBeenCalledOnce()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
  })

  it('uses the same exact-once cleanup when its React owner unmounts', async () => {
    scannerMock.start.mockResolvedValue(undefined)
    const controller = new AbortController()
    const result = scanPairingCode({ signal: controller.signal })
    await Promise.resolve()

    controller.abort()
    controller.abort()
    scannerMock.success?.('late-result')

    await expect(result).resolves.toBeNull()
    expect(scannerMock.stop).toHaveBeenCalledOnce()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
  })

  it('clears exact once when camera startup fails', async () => {
    scannerMock.start.mockRejectedValue(new Error('camera unavailable'))

    await expect(scanPairingCode()).rejects.toThrow('camera unavailable')
    expect(scannerMock.stop).not.toHaveBeenCalled()
    expect(scannerMock.clear).toHaveBeenCalledOnce()
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
