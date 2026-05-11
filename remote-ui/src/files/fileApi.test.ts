import { describe, expect, it } from 'vitest'
import { createFileApi, createFilePreviewSource, type FileEntry } from './fileApi'
import { createMockFileSession } from '../test/mockFileSession'

describe('createFileApi', () => {
  it('routes file list and stat calls through the injected api channel', async () => {
    const session = createMockFileSession({
      '/files/list': {
        path: '/',
        parent: '',
        total: 1,
        entries: [entry({ name: 'src', type: 'dir' })],
      },
      '/files/stat': entry({ name: 'README.md', type: 'file', size: 128 }),
    })
    const api = createFileApi(session)

    await expect(api.listDir('/')).resolves.toEqual(
      expect.objectContaining({
        path: '/',
        entries: [expect.objectContaining({ name: 'src', type: 'dir' })],
      }),
    )
    await expect(api.stat('/README.md')).resolves.toEqual(
      expect.objectContaining({ name: 'README.md', size: 128 }),
    )
    expect(session.requests).toEqual([
      { method: 'POST', path: '/files/list', params: { path: '/', offset: 0, limit: 500 } },
      { method: 'POST', path: '/files/stat', params: { path: '/README.md' } },
    ])
  })

  it('normalizes preview responses from snake case file api fields', async () => {
    const session = createMockFileSession({
      '/files/preview': {
        path: '/README.md',
        name: 'README.md',
        size: 18,
        mime_type: 'text/markdown',
        category: 'text',
        is_text: true,
        content: '# Hello\n',
      },
    })
    const api = createFileApi(session)

    await expect(api.preview('/README.md', 1024)).resolves.toEqual({
      path: '/README.md',
      name: 'README.md',
      size: 18,
      mimeType: 'text/markdown',
      category: 'text',
      isText: true,
      content: '# Hello\n',
      contentBase64: undefined,
      previewLimit: undefined,
    })
    expect(session.requests).toContainEqual({
      method: 'POST',
      path: '/files/preview',
      params: { path: '/README.md', max_size: 1024 },
    })
  })

  it('accepts base64 binary preview content from either content field', async () => {
    const session = createMockFileSession({
      '/files/preview': {
        path: '/shot.png',
        name: 'shot.png',
        size: 68,
        mime_type: 'IMAGE/PNG',
        category: 'IMAGE',
        is_text: false,
        content: 'iVBORw0KGgo=',
      },
    })
    const api = createFileApi(session)

    await expect(api.preview('/shot.png')).resolves.toMatchObject({
      path: '/shot.png',
      name: 'shot.png',
      mimeType: 'IMAGE/PNG',
      category: 'image',
      isText: false,
      content: undefined,
      contentBase64: 'iVBORw0KGgo=',
    })
  })

  it('streams preview files through the download channel without embedding binary JSON', async () => {
    const session = createMockFileSession({
      '/files/download/init': {
        transfer_id: 'preview-video-1',
        name: 'clip.mp4',
        size: 5,
        chunk_size: 65536,
      },
    }, {}, {
      transfers: {
        'preview-video-1': [
          fileDataFrame(0, new TextEncoder().encode('hello')),
          fileCompleteFrame(1),
        ],
      },
    })
    const source = createFilePreviewSource(session)
    const progress: number[] = []
    const chunks: number[] = []

    const result = await source.stream('/clip.mp4', 'video/mp4', {
      onChunk: ({ chunk, receivedSize }) => {
        chunks.push(chunk.byteLength, receivedSize)
      },
      onProgress: ({ receivedSize }) => progress.push(receivedSize),
    })

    expect(session.requests).toContainEqual({
      method: 'POST',
      path: '/files/download/init',
      params: expect.objectContaining({ path: '/clip.mp4', transfer_id: expect.stringMatching(/^preview-/) }),
    })
    expect(session.openedTransfers).toEqual(['preview-video-1'])
    expect(result.receivedSize).toBe(5)
    expect(result.blob.size).toBe(5)
    expect(result.blob.type).toBe('video/mp4')
    expect(chunks).toEqual([5, 5])
    expect(progress).toContain(5)
  })

  it('streams preview byte ranges through bounded download transfers', async () => {
    const session = createMockFileSession({
      '/files/download/init': ({ offset, length }: { offset?: number; length?: number } = {}) => ({
        transfer_id: 'preview-range-1',
        name: 'clip.mp4',
        size: 10,
        chunk_size: 65536,
        offset,
        length,
      }),
    }, {}, {
      transfers: {
        'preview-range-1': [
          fileDataFrame(0, new TextEncoder().encode('456')),
          fileCompleteFrame(1),
        ],
      },
    })
    const source = createFilePreviewSource(session)
    const chunks: string[] = []

    const result = await source.stream('/clip.mp4', 'video/mp4', {
      offset: 4,
      length: 3,
      onChunk: ({ chunk }) => chunks.push(new TextDecoder().decode(chunk)),
    })

    expect(session.requests).toContainEqual({
      method: 'POST',
      path: '/files/download/init',
      params: expect.objectContaining({
        path: '/clip.mp4',
        offset: 4,
        length: 3,
        transfer_id: expect.stringMatching(/^preview-/),
      }),
    })
    expect(result.offset).toBe(4)
    expect(result.totalSize).toBe(10)
    expect(result.receivedSize).toBe(3)
    expect(chunks).toEqual(['456'])
  })

  it('normalizes channel failures into user-visible file api errors', async () => {
    const session = createMockFileSession({}, {
      '/files/list': { status: 403, body: { error: 'file manager denied' } },
    })
    const api = createFileApi(session)

    await expect(api.listDir('/private')).rejects.toThrow(/file manager denied/)
  })

  it('does not expose relay credentials or browser/native implementation details', async () => {
    const session = createMockFileSession({
      '/files/list': { path: '/', parent: '', total: 0, entries: [] },
    })
    const api = createFileApi(session)
    await api.listDir('/')

    expect(JSON.stringify(session.requests)).not.toMatch(/turn|credential|RTCPeerConnection|fetch|nativePlugin/i)
  })
})

function entry(overrides: Partial<FileEntry>): FileEntry {
  return {
    name: overrides.name ?? 'file.txt',
    type: overrides.type ?? 'file',
    size: overrides.size ?? 0,
    mode: overrides.mode ?? '-rw-r--r--',
    modTime: overrides.modTime ?? '2026-05-01T10:00:00Z',
    linkTarget: overrides.linkTarget,
    childCount: overrides.childCount,
    hardLink: overrides.hardLink,
    linkCount: overrides.linkCount,
    inode: overrides.inode,
  }
}

function fileDataFrame(chunk: number, payload: Uint8Array): Uint8Array {
  const frame = new Uint8Array(5 + payload.byteLength)
  frame[0] = 0x01
  const view = new DataView(frame.buffer)
  view.setUint32(1, chunk)
  frame.set(payload, 5)
  return frame
}

function fileCompleteFrame(chunk: number): Uint8Array {
  const frame = new Uint8Array(5)
  frame[0] = 0x02
  const view = new DataView(frame.buffer)
  view.setUint32(1, chunk)
  return frame
}
