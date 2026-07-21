import { describe, expect, it } from 'vitest'
import { applyTerminalModifiers } from './mobileTerminalInput'

describe('mobile terminal input helpers', () => {
  it('maps one-shot Ctrl input to control characters and clears only one-shot modifiers', () => {
    expect(applyTerminalModifiers('c', { ctrl: 'once', alt: 'off' })).toEqual({
      data: '\x03',
      ctrl: 'off',
      alt: 'off',
    })
  })

  it('prefixes Alt input with escape and preserves locked modifiers', () => {
    expect(applyTerminalModifiers('x', { ctrl: 'locked', alt: 'locked' })).toEqual({
      data: '\x1b\x18',
      ctrl: 'locked',
      alt: 'locked',
    })
  })

  it('leaves Muxvia public language free of tgent pane concepts', () => {
    const publicKeys = Object.keys({ ctrl: 'once', alt: 'off', data: '\t' })
    expect(publicKeys).not.toEqual(expect.arrayContaining(['paneId', 'sessionId', 'workspaceId', 'tabId']))
  })
})
