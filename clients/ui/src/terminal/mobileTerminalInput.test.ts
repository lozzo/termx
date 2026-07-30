import { describe, expect, it } from 'vitest'
import { applyTerminalModifiers, nextModifierState } from './mobileTerminalInput'

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

  it('cycles modifiers through off, once, locked, and back to off', () => {
    expect(nextModifierState('off')).toBe('once')
    expect(nextModifierState('once')).toBe('locked')
    expect(nextModifierState('locked')).toBe('off')
  })

  it('applies Ctrl before Alt and consumes both one-shot modifiers together', () => {
    expect(applyTerminalModifiers('\\', { ctrl: 'once', alt: 'once' })).toEqual({
      data: '\x1b\x1c',
      ctrl: 'off',
      alt: 'off',
    })
  })
})
