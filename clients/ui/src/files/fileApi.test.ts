import { create } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import { ResourceHandleSchema, ResourceKind } from '../generated/apipb/common_pb'
import {
  FileEntrySchema, FileEntryType, FileListResultSchema, FilePreviewResultSchema, FileStatResultSchema,
  FileTransferHandleSchema, FileTransferOpenResultSchema,
} from '../generated/apipb/file_pb'
import { ResourceStreamFrameType } from '../generated/bindingpb/client_binding_pb'
import { MockProtoResourceStream, MockProtoSession, protoResult } from '../test/mockProtoSession'
import { encodeFileTransferDataPayload } from './fileStreamProtocol'
import { createFileApi, createFilePreviewSource } from './fileApi'
import { create as createMessage, toBinary } from '@bufbuild/protobuf'
import { FileTransferFinishSchema } from '../generated/wirepb/terminal_pb'

describe('createFileApi generated Proto API', () => {
  it('routes list, stat, and preview through typed commands', async () => {
    const file = create(FileEntrySchema, { path: '/README.md', name: 'README.md', type: FileEntryType.FILE, size: 8n })
    const session = new MockProtoSession('machine-local', (command) => {
      switch (command.command.case) {
        case 'fileList': return protoResult('fileList', create(FileListResultSchema, { path: '/', entries: [file] }))
        case 'fileStat': return protoResult('fileStat', create(FileStatResultSchema, { entry: file }))
        case 'filePreview': return protoResult('filePreview', create(FilePreviewResultSchema, { entry: file, mimeType: 'text/markdown', content: new TextEncoder().encode('# Hello\n') }))
        default: throw new Error(`unexpected command ${command.command.case}`)
      }
    })
    const api = createFileApi(session)

    await expect(api.listDir('/')).resolves.toMatchObject({ total: 1, entries: [{ name: 'README.md' }] })
    await expect(api.stat('/README.md')).resolves.toMatchObject({ name: 'README.md', size: 8 })
    await expect(api.preview('/README.md', 1024)).resolves.toMatchObject({ content: '# Hello\n', mimeType: 'text/markdown' })
    expect(session.commands.map((command) => command.command.case)).toEqual(['fileList', 'fileStat', 'filePreview'])
  })

  it('opens a resource stream and verifies file payload framing', async () => {
    const content = new TextEncoder().encode('hello')
    const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', content))
    const resource = create(ResourceHandleSchema, { opaqueToken: Uint8Array.of(1), kind: ResourceKind.FILE_DOWNLOAD })
    const stream = new MockProtoResourceStream([
      { type: ResourceStreamFrameType.FILE_DATA, payload: encodeFileTransferDataPayload({ offset: 0, data: content }) },
      { type: ResourceStreamFrameType.FILE_FINISH, payload: toBinary(FileTransferFinishSchema, createMessage(FileTransferFinishSchema, { size: 5n, sha256: digest })) },
    ])
    const session = new MockProtoSession('machine-local', () => protoResult('fileTransferOpen', create(FileTransferOpenResultSchema, {
      transfer: create(FileTransferHandleSchema, { resource, path: '/clip.mp4', size: 5n, chunkBytes: 64, windowBytes: 256n }),
    })), () => stream)

    const result = await createFilePreviewSource(session).stream('/clip.mp4', 'video/mp4')

    expect(result.receivedSize).toBe(5)
    expect(result.blob.size).toBe(5)
    expect(session.openedResources).toEqual([resource])
  })

  it('rejects a stream with a mismatched digest', async () => {
    const content = new TextEncoder().encode('bad')
    const resource = create(ResourceHandleSchema, { opaqueToken: Uint8Array.of(2), kind: ResourceKind.FILE_DOWNLOAD })
    const stream = new MockProtoResourceStream([
      { type: ResourceStreamFrameType.FILE_DATA, payload: encodeFileTransferDataPayload({ offset: 0, data: content }) },
      { type: ResourceStreamFrameType.FILE_FINISH, payload: toBinary(FileTransferFinishSchema, createMessage(FileTransferFinishSchema, { size: 3n, sha256: new Uint8Array(32) })) },
    ])
    const session = new MockProtoSession('machine-local', () => protoResult('fileTransferOpen', create(FileTransferOpenResultSchema, {
      transfer: create(FileTransferHandleSchema, { resource, path: '/bad.bin', size: 3n, chunkBytes: 64, windowBytes: 256n }),
    })), () => stream)

    await expect(createFilePreviewSource(session).stream('/bad.bin', 'application/octet-stream')).rejects.toThrow(/SHA-256 mismatch/)
  })
})
