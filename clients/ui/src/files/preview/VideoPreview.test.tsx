import { cleanup, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { FilePreviewResponse } from '../fileApi'
import { StreamedVideoPreview } from './VideoPreview'

describe('StreamedVideoPreview', () => {
  const createObjectURL = vi.fn(() => 'blob:anytty-video-preview')
  const revokeObjectURL = vi.fn()

  beforeEach(() => {
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: false })
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('falls back to a local blob for small videos when range service workers are unavailable', async () => {
    const streamPreview = vi.fn().mockResolvedValue({
      blob: new Blob([Uint8Array.of(1, 2, 3)], { type: 'video/mp4' }),
      receivedSize: 3,
      offset: 0,
      totalSize: 3,
    })
    const preview: FilePreviewResponse = {
      path: '/clip.mp4',
      name: 'clip.mp4',
      size: 3,
      mimeType: 'video/mp4',
      category: 'video',
      isText: false,
    }

    const rendered = render(<StreamedVideoPreview preview={preview} streamPreview={streamPreview} />)

    await waitFor(() => expect(rendered.container.querySelector('video')?.getAttribute('src')).toBe('blob:anytty-video-preview'))
    expect(streamPreview).toHaveBeenCalledWith('/clip.mp4', 'video/mp4', {
      signal: expect.any(AbortSignal),
    })

    rendered.unmount()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:anytty-video-preview')
  })
})
