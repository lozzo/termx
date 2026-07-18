import { describe, expect, expectTypeOf, it } from 'vitest'
import browserSource from '../webrtc/browserRtcSession.ts?raw'
import terminalSource from './Terminal.tsx?raw'
import hookSource from './useTerminalSession.tsx?raw'
import type { TerminalProps } from './Terminal'
import type { RtcSession } from '../core/transport'
import type { ProtoClientSession } from '../core/protoClientSession'

describe('terminal runtime boundary', () => {
  it('keeps UI terminal consumers on RtcSession instead of browser or protocol transport types', () => {
    expectTypeOf<TerminalProps['session']>().toEqualTypeOf<RtcSession | ProtoClientSession>()
    expect(terminalSource).not.toMatch(/TerminalProtocolSession|RTCPeerConnection|RTCDataChannel/)
    expect(hookSource).toMatch(/createProtoTerminalProtocolSession/)
    expect(hookSource).not.toMatch(/RTCPeerConnection|RTCDataChannel/)
  })

  it('keeps the browser adapter from implementing terminal protocol client responsibilities', () => {
    expect(browserSource).not.toMatch(/TerminalProtocolSession|createTerminalProtocolClient|subscribeTerminal|closeTerminalChannel/)
  })
})
