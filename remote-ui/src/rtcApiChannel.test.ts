import { describe, expect, it } from 'vitest'
import { LocalApiChannel } from './rtcApiChannel'
import type { RTCDataChannelLike } from './browserRtcSession'

describe('LocalApiChannel', () => {
  it('rejects a request on a closed channel before calling RTCDataChannel.send', async () => {
    const channel = new MockRTCDataChannel('api')
    const api = new LocalApiChannel(channel)

    channel.close()

    await expect(api.request('GET', { path: '/files/list', params: { path: '/' } }))
      .rejects.toThrow(/data channel api is closed/)
    expect(channel.sendCalls).toBe(0)
  })
})

class MockRTCDataChannel extends EventTarget implements RTCDataChannelLike {
  readyState: RTCDataChannelState = 'open'
  binaryType?: BinaryType
  sendCalls = 0

  constructor(readonly label: string) {
    super()
  }

  send(): void {
    this.sendCalls += 1
    throw new Error("Failed to execute 'send' on 'RTCDataChannel': RTCDataChannel.readyState is not 'open'")
  }

  close(): void {
    this.readyState = 'closed'
    this.dispatchEvent(new Event('close'))
  }
}
