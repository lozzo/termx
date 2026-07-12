import { describe, expect, it } from 'vitest'
import {
  decodeFileTransferAckPayload,
  decodeFileTransferDataPayload,
  decodeFileTransferFinishPayload,
  decodeFileTransferResultPayload,
  decodeTerminalMethodParams,
  decodeTerminalMethodResult,
  encodeFileTransferAckPayload,
  encodeFileTransferDataPayload,
  encodeFileTransferFinishPayload,
  encodeFileTransferResultPayload,
  encodeTerminalMethodParams,
  encodeTerminalMethodResult,
} from './terminalWireProtocol'

describe('terminal file wire protocol', () => {
  it('round trips typed file methods', () => {
    const params = [
      ['file.list', { path: '/srv/files', cursor: 'next', limit: 50 }],
      ['file.preview', { path: '/srv/files/a.txt', max_bytes: 4096 }],
      ['file.rename', { path: '/srv/files/a', new_path: '/srv/files/b', overwrite: true }],
      ['file.copy', { paths: ['/srv/files/a'], target_dir: '/srv/target', overwrite: false }],
      ['file.download.open', { path: '/srv/files/a', offset: 1024, expected_size: 8192, expected_modified_at_unix_nano: 123456 }],
      ['file.upload.open', { path: '/srv/files/a', size: 8192, overwrite: true, resume_transfer_id: 'resume-1' }],
      ['file.transfer.cancel', { transfer_id: 'transfer-1' }],
    ] as const
    for (const [method, value] of params) {
      expect(decodeTerminalMethodParams(method, encodeTerminalMethodParams(method, value))).toEqual(value)
    }
  })

  it('preserves file metadata, binary preview and per-item failures', () => {
    const entry = { path: '/srv/a', name: 'a', type: 'file', size: 12, mode: 0o640, modified_at_unix_nano: 99, link_target: '' }
    expect(decodeTerminalMethodResult('file.stat', encodeTerminalMethodResult('file.stat', entry))).toEqual(entry)
    const preview = { entry, mime_type: 'application/octet-stream', content: new Uint8Array([0, 1, 255]), truncated: true }
    expect(decodeTerminalMethodResult('file.preview', encodeTerminalMethodResult('file.preview', preview))).toEqual(preview)
    const batch = { results: [{ path: '/srv/a', target_path: '/srv/b', success: false, error_code: 'already_exists', error_message: 'exists' }] }
    expect(decodeTerminalMethodResult('file.copy', encodeTerminalMethodResult('file.copy', batch))).toEqual(batch)
  })

  it('round trips transfer data, ack, finish and result payloads', () => {
    const digest = new Uint8Array(32).fill(0x5a)
    expect(decodeFileTransferDataPayload(encodeFileTransferDataPayload({ offset: 4096, data: new Uint8Array([1, 2]) }))).toEqual({ offset: 4096, data: new Uint8Array([1, 2]) })
    expect(decodeFileTransferAckPayload(encodeFileTransferAckPayload({ offset: 4098, windowBytes: 65536 }))).toEqual({ offset: 4098, windowBytes: 65536 })
    expect(decodeFileTransferFinishPayload(encodeFileTransferFinishPayload({ size: 4098, sha256: digest }))).toEqual({ size: 4098, sha256: digest })
    expect(decodeFileTransferResultPayload(encodeFileTransferResultPayload({ path: '/srv/a', size: 4098, sha256: digest }))).toEqual({ path: '/srv/a', size: 4098, sha256: digest })
  })
})
