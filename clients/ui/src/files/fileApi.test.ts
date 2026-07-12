import { describe, expect, it } from 'vitest'
import { createFileApi, createFilePreviewSource } from './fileApi'
import { createMockFileSession } from '../test/mockFileSession'
import { TERMX_FRAME_TYPES, encodeTermxFrame } from '../terminal/termxProtocol'
import { encodeFileTransferDataPayload, encodeFileTransferFinishPayload } from '../terminal/terminalWireProtocol'

describe('createFileApi', () => {
  it('routes list and stat through typed file methods', async () => {
    const session = createMockFileSession({
      'file.list': { path: '/', entries: [entry('/src', 'dir')], next_cursor: '' },
      'file.stat': entry('/README.md', 'file', 128),
    })
    const api = createFileApi(session)

    await expect(api.listDir('/')).resolves.toMatchObject({ path: '/', total: 1, entries: [{ name: 'src', type: 'dir' }] })
    await expect(api.stat('/README.md')).resolves.toMatchObject({ name: 'README.md', size: 128 })
    expect(session.requests).toEqual([
      { method: 'file.list', path: 'file.list', params: { path: '/', cursor: '', limit: 500 } },
      { method: 'file.stat', path: 'file.stat', params: { path: '/README.md' } },
    ])
  })

  it('decodes text preview bytes without JSON base64', async () => {
    const session = createMockFileSession({
      'file.preview': {
        entry: entry('/README.md', 'file', 8),
        mime_type: 'text/markdown',
        content: new TextEncoder().encode('# Hello\n'),
        truncated: false,
      },
    })

    await expect(createFileApi(session).preview('/README.md', 1024)).resolves.toMatchObject({
      path: '/README.md', name: 'README.md', mimeType: 'text/markdown', category: 'text', isText: true, content: '# Hello\n',
    })
    expect(session.requests).toContainEqual({
      method: 'file.preview', path: 'file.preview', params: { path: '/README.md', max_bytes: 1024 },
    })
  })

  it('projects binary preview bytes for image rendering', async () => {
    const bytes = Uint8Array.from([0x89, 0x50, 0x4e, 0x47])
    const session = createMockFileSession({
      'file.preview': { entry: entry('/shot.png', 'file', bytes.byteLength), mime_type: 'image/png', content: bytes, truncated: false },
    })

    await expect(createFileApi(session).preview('/shot.png')).resolves.toMatchObject({
      path: '/shot.png', category: 'image', isText: false, contentBase64: 'iVBORw==',
    })
  })

  it('streams download frames on the daemon-assigned channel and verifies SHA-256', async () => {
    const channel = 41
    const content = new TextEncoder().encode('hello')
    const session = createMockFileSession({
      'file.download.open': transferOpen('/clip.mp4', content.byteLength, channel),
    }, {}, {
      transfers: {
        'preview-video-1': [
          encodeTermxFrame(channel, TERMX_FRAME_TYPES.fileData, encodeFileTransferDataPayload({ offset: 0, data: content })),
          encodeTermxFrame(channel, TERMX_FRAME_TYPES.fileFinish, encodeFileTransferFinishPayload({ size: content.byteLength, sha256: hex('2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824') })),
        ],
      },
    })

    const result = await createFilePreviewSource(session).stream('/clip.mp4', 'video/mp4')

    expect(session.requests).toContainEqual({
      method: 'file.download.open', path: 'file.download.open',
      params: { path: '/clip.mp4', offset: 0, expected_size: 0, expected_modified_at_unix_nano: 0 },
    })
    expect(session.openedTransfers).toEqual(['preview-video-1'])
    expect(result.receivedSize).toBe(content.byteLength)
    expect(result.blob.size).toBe(content.byteLength)
    expect(result.blob.type).toBe('video/mp4')
  })

  it('rejects a download with a mismatched digest', async () => {
    const channel = 42
    const content = new TextEncoder().encode('bad')
    const session = createMockFileSession({
      'file.download.open': transferOpen('/bad.bin', content.byteLength, channel),
    }, {}, { transfers: { 'preview-video-1': [
      encodeTermxFrame(channel, TERMX_FRAME_TYPES.fileData, encodeFileTransferDataPayload({ offset: 0, data: content })),
      encodeTermxFrame(channel, TERMX_FRAME_TYPES.fileFinish, encodeFileTransferFinishPayload({ size: content.byteLength, sha256: new Uint8Array(32) })),
    ] } })

    await expect(createFilePreviewSource(session).stream('/bad.bin', 'application/octet-stream')).rejects.toThrow(/SHA-256 mismatch/)
  })

  it('surfaces per-item mutation failure', async () => {
    const session = createMockFileSession({
      'file.copy': { results: [{ path: '/a', target_path: '/target/a', success: false, error_code: 'exists', error_message: 'target exists' }] },
    })
    await expect(createFileApi(session).copy(['/a'], '/target')).rejects.toThrow(/target exists/)
  })

  it('normalizes protocol authorization failures', async () => {
    const session = createMockFileSession({}, { 'file.list': { status: 403, body: { error: 'file manager denied' } } })
    await expect(createFileApi(session).listDir('/private')).rejects.toThrow(/file manager denied/)
  })
})

function entry(path: string, type: string, size = 0) {
  return { path, name: path.slice(path.lastIndexOf('/') + 1), type, size, mode: 0o644, modified_at_unix_nano: 0, link_target: '' }
}

function transferOpen(path: string, size: number, channel: number) {
  return { transfer_id: 'preview-video-1', channel, path, offset: 0, size, modified_at_unix_nano: 1, window_bytes: 262144, chunk_bytes: 65536 }
}

function hex(value: string): Uint8Array {
  return Uint8Array.from(value.match(/../g) ?? [], (byte) => Number.parseInt(byte, 16))
}
