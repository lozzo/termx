import { describe, expect, it } from 'vitest'
import source from './browserRtcSession.ts?raw'
import transportSource from './transport.ts?raw'
import { createBrowserRtcSession } from './browserRtcSession'

describe('BrowserRtcSession adapter boundary', () => {
  it('keeps browser WebRTC primitives inside the browser adapter module', () => {
    expect(source).toContain('class BrowserRtcSession')
    expect(source).toMatch(/\bRTCPeerConnection\b/)
    expect(source).toMatch(/\bcreateDataChannel\b/)
    expect(source).toMatch(/\bRTCDataChannelState\b/)
    expect(transportSource).not.toMatch(/\bRTCPeerConnection\b|\bRTCDataChannel\b|\bRTCSessionDescriptionInit\b/)
  })

  it('names the factory as a browser session factory instead of a transport factory', () => {
    expect(createBrowserRtcSession).toBeTypeOf('function')
    expect(source).not.toMatch(/class LocalWebRtcPeerTransport|function createLocalWebRtcPeerTransport/)
  })
})
