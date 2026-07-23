import { afterEach, describe, expect, it } from 'vitest'
import { muxviaI18n } from '../i18n'
import type { RtcConnectionPhase } from '../core/transport'
import { connectionPathLabel, connectionPhaseLabel, connectionStatusIsSettled } from './connectionState'

describe('connectionStatusIsSettled', () => {
  it('stops the busy overlay when a connection attempt fails', () => {
    expect(connectionStatusIsSettled('failed')).toBe(true)
    expect(connectionStatusIsSettled('reconnecting')).toBe(false)
  })
})

describe('connection display projection', () => {
  afterEach(async () => {
    await muxviaI18n.changeLanguage('en')
  })

  it('maps every runtime phase to symmetric English and Chinese user concepts', async () => {
    const phases: RtcConnectionPhase[] = [
      'idle', 'probing', 'resolving', 'signaling', 'connecting', 'authorizing',
      'connected', 'verifying', 'reconnecting', 'waiting_network', 'failed',
    ]

    await muxviaI18n.changeLanguage('en')
    const english = phases.map((phase) => connectionPhaseLabel(phase, muxviaI18n.t))
    await muxviaI18n.changeLanguage('zh-CN')
    const chinese = phases.map((phase) => connectionPhaseLabel(phase, muxviaI18n.t))

    expect(english).toHaveLength(phases.length)
    expect(chinese).toHaveLength(phases.length)
    expect(english.join(' ')).toMatch(/Direct.*SSH.*P2P.*Relay/s)
    expect(english.join(' ')).toContain('ICE')
    expect(chinese.join(' ')).toMatch(/Direct.*SSH.*P2P.*Relay/s)
    expect(chinese.join(' ')).toContain('ICE')
    expect([...english, ...chinese].join(' ')).not.toMatch(/JNI|native runtime|Go runtime|binding|handle|generation/i)
  })

  it('does not expose internal path-owner names as connection labels', () => {
    expect(connectionPathLabel('hub')).toBe('Muxvia Cloud')
    expect(connectionPathLabel('local')).toBe('Local')
    expect(connectionPathLabel(undefined)).toBe('Connection')
  })
})
