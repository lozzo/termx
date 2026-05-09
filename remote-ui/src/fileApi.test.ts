import { describe, expect, it } from 'vitest'
import { createFileApi, type FileEntry } from './fileApi'
import { createMockFileSession } from './test/mockFileSession'

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
  }
}
