import { describe, expect, it } from 'vitest'
import { LocalApiChannel } from './rtcApiChannel'
import type { RTCDataChannelLike } from './browserRtcSession'
import { decodeRuntimeAPIRequest, decodeRuntimeRequestBody, decodeRuntimeResponseBody, encodeRuntimeResponseBody } from './runtimeProtocol'

describe('LocalApiChannel', () => {
  it('rejects a request on a closed channel before calling RTCDataChannel.send', async () => {
    const channel = new MockRTCDataChannel('api')
    const api = new LocalApiChannel(channel)

    channel.close()

    await expect(api.request('GET', { path: '/files/list', params: { path: '/' } }))
      .rejects.toThrow(/data channel api is closed/)
    expect(channel.sendCalls).toBe(0)
  })

  it('preserves file api body fields beyond path, offset, and limit', async () => {
    const channel = new MockRTCDataChannel('api')
    const api = new LocalApiChannel(channel)

    const pending = api.request('POST', { path: '/files/preview', params: { path: '/README.md', max_size: 1024 } })
      .catch(() => undefined)

    const request = decodeRuntimeAPIRequest(channel.sentBytes[0] ?? new Uint8Array())
    expect(request).toEqual(expect.objectContaining({
      method: 'POST',
      path: '/files/preview',
    }))
    expect(decodeRuntimeRequestBody(request.path, request.method, request.body)).toEqual({
      path: '/README.md',
      max_size: 1024,
    })
    api.close()
    await pending
  })

  it('encodes storage api request bodies over the runtime api channel', async () => {
    const channel = new MockRTCDataChannel('api')
    const api = new LocalApiChannel(channel)

    const value = new TextEncoder().encode('hello')
    const pending = api.request('POST', {
      path: '/storage/put',
      params: {
        app_id: 'termx.clipboard',
        scope: 'public',
        owner_id: 'owner-a',
        key: 'history/clip-1',
        value,
        check_version: true,
        expected_version: 2,
      },
    }).catch(() => undefined)

    const request = decodeRuntimeAPIRequest(channel.sentBytes[0] ?? new Uint8Array())
    expect(request).toEqual(expect.objectContaining({
      method: 'POST',
      path: '/storage/put',
    }))
    const decoded = decodeRuntimeRequestBody(request.path, request.method, request.body) as Record<string, unknown>
    expect(decoded).toEqual(expect.objectContaining({
      app_id: 'termx.clipboard',
      scope: 'public',
      owner_id: 'owner-a',
      key: 'history/clip-1',
      check_version: true,
      expected_version: 2,
    }))
    expect(Array.from(decoded.value as Uint8Array)).toEqual(Array.from(value))
    api.close()
    await pending
  })

  it('encodes terminal restart over the runtime api channel', async () => {
    const channel = new MockRTCDataChannel('api')
    const api = new LocalApiChannel(channel)

    const pending = api.request('restart', { terminal_id: 'terminal-1' }).catch(() => undefined)

    const request = decodeRuntimeAPIRequest(channel.sentBytes[0] ?? new Uint8Array())
    expect(request).toEqual(expect.objectContaining({
      method: 'restart',
      path: 'restart',
    }))
    expect(decodeRuntimeRequestBody(request.path, request.method, request.body)).toEqual({
      terminal_id: 'terminal-1',
    })
    api.close()
    await pending
  })

  it('encodes terminal create requests over the runtime api channel', async () => {
    const channel = new MockRTCDataChannel('api')
    const api = new LocalApiChannel(channel)

    const pending = api.request('create', {
      name: 'ops shell',
      command: ['/bin/zsh', '-l'],
    }).catch(() => undefined)

    const request = decodeRuntimeAPIRequest(channel.sentBytes[0] ?? new Uint8Array())
    expect(request).toEqual(expect.objectContaining({
      method: 'create',
      path: 'create',
    }))
    expect(decodeRuntimeRequestBody(request.path, request.method, request.body)).toEqual({
      name: 'ops shell',
      command: ['/bin/zsh', '-l'],
    })
    api.close()
    await pending
  })

  it('decodes storage api response bodies from runtime protobuf messages', () => {
    const body = encodeRuntimeResponseBody('/storage/list', 'POST', {
      entries: [{
        app_id: 'termx.clipboard',
        scope: 'public',
        key: 'history/clip-1',
        value: new TextEncoder().encode('hello'),
        version: 7,
        updated_at: '2026-05-16T08:00:00Z',
      }],
    })

    const decoded = decodeRuntimeResponseBody('/storage/list', 'POST', body) as { entries?: Array<Record<string, unknown>> }

    expect(decoded.entries).toHaveLength(1)
    expect(decoded.entries?.[0]).toEqual(expect.objectContaining({
      app_id: 'termx.clipboard',
      scope: 'public',
      key: 'history/clip-1',
      version: 7,
      updated_at: '2026-05-16T08:00:00Z',
    }))
    expect(Array.from(decoded.entries?.[0]?.value as Uint8Array)).toEqual(Array.from(new TextEncoder().encode('hello')))
  })
})

class MockRTCDataChannel extends EventTarget implements RTCDataChannelLike {
  readyState: RTCDataChannelState = 'open'
  binaryType?: BinaryType
  sendCalls = 0
  sentBytes: Uint8Array[] = []

  constructor(readonly label: string) {
    super()
  }

  send(data?: string | ArrayBuffer | Blob | ArrayBufferView): void {
    this.sendCalls += 1
    if (this.readyState === 'open' && data && ArrayBuffer.isView(data)) {
      this.sentBytes.push(new Uint8Array(data.buffer, data.byteOffset, data.byteLength))
      return
    }
    throw new Error("Failed to execute 'send' on 'RTCDataChannel': RTCDataChannel.readyState is not 'open'")
  }

  close(): void {
    this.readyState = 'closed'
    this.dispatchEvent(new Event('close'))
  }
}
