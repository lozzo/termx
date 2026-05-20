import { describe, expect, it } from 'vitest'
import architecture from '../../docs/webrtc-rewrite-architecture.md?raw'
import log from '../../docs/webrtc-rewrite-log.md?raw'

describe('WebRTC rewrite docs', () => {
  it('marks the historical architecture document as archived and points to current rules', () => {
    expect(architecture).toContain('# Archived: remote-ui WebRTC Rewrite Architecture')
    expect(architecture).toContain('It is no longer the active planning document for current work.')
    expect(architecture).toContain('repository-root `AGENTS.md`')
    expect(architecture).toContain('repository-root `workflow.md`')
  })

  it('keeps only archive handoff content in the public docs', () => {
    expect(architecture).toContain('runtime transport remains WebRTC DataChannel')
    expect(architecture).toContain('relay is not a fourth client path')
    expect(architecture).toContain('browser-side integration against the unified remote product flow')
    expect(architecture).not.toMatch(/## Non-Negotiable Model|## Why The Old Boundary Is Wrong|### `RtcSession`/)
    expect(architecture).not.toMatch(/anonymous_p2p|managed_p2p|paid_relay/)
  })

  it('keeps a slice-oriented handoff log', () => {
    expect(log).toContain('Slice 1')
    expect(log).toContain('Goal')
    expect(log).toContain('Failing tests first')
    expect(log).toContain('Review')
    expect(log).toContain('Remaining risk')
  })
})
