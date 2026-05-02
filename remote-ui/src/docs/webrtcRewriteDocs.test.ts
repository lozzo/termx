import { describe, expect, it } from 'vitest'
import architecture from '../../docs/webrtc-rewrite-architecture.md?raw'
import log from '../../docs/webrtc-rewrite-log.md?raw'

describe('WebRTC rewrite docs', () => {
  it('documents the non-negotiable runtime boundaries', () => {
    expect(architecture).toContain('Runtime is always WebRTC DataChannel')
    expect(architecture).toContain('HTTP is not transport')
    expect(architecture).toContain('RtcSession')
    expect(architecture).toContain('BrowserRtcSession')
    expect(architecture).toContain('TerminalProtocolClient')
    expect(architecture).toContain('local')
    expect(architecture).toContain('public_p2p')
    expect(architecture).toContain('managed')
    expect(architecture).toContain('relay is not a client transport type')
    expect(architecture).toContain('native adapter')
  })

  it('rejects old transport taxonomy and browser leakage in public docs', () => {
    const pathMatches = architecture.match(/`(local|public_p2p|managed)`/g) ?? []
    expect(new Set(pathMatches)).toEqual(new Set(['`local`', '`public_p2p`', '`managed`']))
    const modelSection = sectionBetween(architecture, '## Non-Negotiable Model', '## Why The Old Boundary Is Wrong')
    expect(modelSection).not.toMatch(/anonymous_p2p|managed_p2p|paid_relay|fourth client transport/i)

    const rtcSessionSection = sectionBetween(architecture, '### `RtcSession`', '### Browser Adapter')
    expect(rtcSessionSection).not.toMatch(/\bRTCPeerConnection\b|\bRTCDataChannel\b|\bBlob\b|nativePlugin/)
    expect(architecture).toContain('Terminal management after a runtime session exists must move through `RtcSession.openApi()`')
  })

  it('keeps a slice-oriented handoff log', () => {
    expect(log).toContain('Slice 1')
    expect(log).toContain('Goal')
    expect(log).toContain('Failing tests first')
    expect(log).toContain('Review')
    expect(log).toContain('Remaining risk')
  })
})

function sectionBetween(source: string, start: string, end: string): string {
  const startIndex = source.indexOf(start)
  const endIndex = source.indexOf(end)
  if (startIndex < 0 || endIndex < 0 || endIndex <= startIndex) {
    throw new Error(`missing section ${start} -> ${end}`)
  }
  return source.slice(startIndex, endIndex)
}
