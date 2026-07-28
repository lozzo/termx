import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { FilePreviewResponse } from '../fileApi'
import { StreamedImagePreview } from './ImagePreview'

describe('StreamedImagePreview', () => {
  const createObjectURL = vi.fn(() => 'blob:anytty-image-preview')
  const revokeObjectURL = vi.fn()

  beforeEach(() => {
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('streams image bytes and revokes the local preview when closed', async () => {
    const streamPreview = vi.fn().mockResolvedValue({
      blob: new Blob([Uint8Array.of(1, 2, 3)], { type: 'image/png' }),
      receivedSize: 3,
      offset: 0,
      totalSize: 3,
    })
    const preview: FilePreviewResponse = {
      path: '/photo.png',
      name: 'photo.png',
      size: 3,
      mimeType: 'image/png',
      category: 'image',
      isText: false,
    }

    const rendered = render(<StreamedImagePreview preview={preview} streamPreview={streamPreview} />)

    await waitFor(() => expect(screen.getByRole('img', { name: 'photo.png' }).getAttribute('src')).toBe('blob:anytty-image-preview'))
    expect(streamPreview).toHaveBeenCalledWith('/photo.png', 'image/png', {
      signal: expect.any(AbortSignal),
    })

    rendered.unmount()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:anytty-image-preview')
  })
})
