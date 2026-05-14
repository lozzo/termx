import { describe, expect, it } from 'vitest'
import { LocalApiChannel } from './rtcApiChannel'
import type { RTCDataChannelLike } from './browserRtcSession'
import { decodeRuntimeAPIRequest, decodeRuntimeRequestBody } from './runtimeProtocol'

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
