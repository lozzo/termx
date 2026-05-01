import { describe, expect, it } from 'vitest'
import { createFileApi, type FileEntry } from './fileApi'
import { createMockFilePeerTransport } from './test/mockFileTransport'

describe('createFileApi', () => {
  it('routes file list and stat calls through the injected api channel', async () => {
    const transport = createMockFilePeerTransport({
      '/files/list': {
        path: '/',
        parent: '',
        total: 1,
        entries: [entry({ name: 'src', type: 'dir' })],
      },
      '/files/stat': entry({ name: 'README.md', type: 'file', size: 128 }),
    })
    const api = createFileApi(transport)

    await expect(api.listDir('/')).resolves.toEqual(
      expect.objectContaining({
        path: '/',
        entries: [expect.objectContaining({ name: 'src', type: 'dir' })],
      }),
    )
    await expect(api.stat('/README.md')).resolves.toEqual(
      expect.objectContaining({ name: 'README.md', size: 128 }),
    )
    expect(transport.requests).toEqual([
      { method: 'GET', path: '/files/list', params: { path: '/', offset: 0, limit: 500 } },
      { method: 'GET', path: '/files/stat', params: { path: '/README.md' } },
    ])
  })

  it('normalizes channel failures into user-visible file api errors', async () => {
    const transport = createMockFilePeerTransport({}, {
      '/files/list': { status: 403, body: { error: 'file manager denied' } },
    })
    const api = createFileApi(transport)

    await expect(api.listDir('/private')).rejects.toThrow(/file manager denied/)
  })

  it('does not expose relay credentials or browser/native implementation details', async () => {
    const transport = createMockFilePeerTransport({
      '/files/list': { path: '/', parent: '', total: 0, entries: [] },
    })
    const api = createFileApi(transport)
    await api.listDir('/')

    expect(JSON.stringify(transport.requests)).not.toMatch(/turn|credential|RTCPeerConnection|fetch|nativePlugin/i)
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
