import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { createFilePreviewRangeUrl as createFilePreviewRangeUrlFn } from './filePreviewRangeUrl'

describe('createFilePreviewRangeUrl', () => {
  let listeners: Array<(event: MessageEvent) => void>
  let controllerMessages: unknown[]
  let createFilePreviewRangeUrl: typeof createFilePreviewRangeUrlFn

  beforeEach(async () => {
    vi.resetModules()
    listeners = []
    controllerMessages = []
    Object.defineProperty(window, 'isSecureContext', {
      configurable: true,
      value: true,
    })
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: {
        controller: {
          postMessage: (message: unknown) => controllerMessages.push(message),
        },
        ready: Promise.resolve({}),
        register: vi.fn().mockResolvedValue({}),
        addEventListener: (_type: string, listener: (event: MessageEvent) => void) => {
          listeners.push(listener)
        },
        removeEventListener: vi.fn(),
      },
    })
    createFilePreviewRangeUrl = (await import('./filePreviewRangeUrl')).createFilePreviewRangeUrl
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('creates a range URL and serves service worker range messages through streamPreview', async () => {
    const blob = new Blob(['456'], { type: 'video/mp4' })
    const streamPreview = vi.fn().mockResolvedValue({
      blob,
      receivedSize: 3,
      offset: 4,
      totalSize: 10,
    })

    const result = await createFilePreviewRangeUrl({
      path: '/clip.mp4',
      mimeType: 'video/mp4',
      size: 10,
      streamPreview,
    })

    expect(result?.url).toMatch(/^\/__termx_file_preview__\/preview-/)
    expect(navigator.serviceWorker.register).toHaveBeenCalledWith('/termx-file-preview-sw.js', { scope: '/' })

    await listeners[0]?.(new MessageEvent('message', {
      data: {
        type: 'termx-preview-range-request',
        requestId: 'range-1',
        token: decodeURIComponent(result?.url.split('/__termx_file_preview__/')[1]?.split('?')[0] ?? ''),
        offset: 4,
        length: 3,
      },
      source: navigator.serviceWorker.controller as unknown as MessageEventSource,
    }))

    expect(streamPreview).toHaveBeenCalledWith('/clip.mp4', 'video/mp4', {
      offset: 4,
      length: 3,
    })
    expect(controllerMessages).toContainEqual(expect.objectContaining({
      type: 'termx-preview-response',
      requestId: 'range-1',
      blob,
      offset: 4,
      length: 3,
      totalSize: 10,
      mimeType: 'video/mp4',
    }))

    result?.revoke()
  })

  it('rejects malformed range messages instead of falling back to a full-file stream', async () => {
    const streamPreview = vi.fn()

    const result = await createFilePreviewRangeUrl({
      path: '/clip.mp4',
      mimeType: 'video/mp4',
      size: 10,
      streamPreview,
    })

    await listeners[0]?.(new MessageEvent('message', {
      data: {
        type: 'termx-preview-range-request',
        requestId: 'range-2',
        token: decodeURIComponent(result?.url.split('/__termx_file_preview__/')[1]?.split('?')[0] ?? ''),
        offset: 0,
        length: 0,
      },
      source: navigator.serviceWorker.controller as unknown as MessageEventSource,
    }))

    expect(streamPreview).not.toHaveBeenCalled()
    expect(controllerMessages).toContainEqual(expect.objectContaining({
      type: 'termx-preview-error',
      requestId: 'range-2',
      message: 'file preview range length is required',
    }))

    result?.revoke()
  })
})
