import { describe, expect, it } from 'vitest'
import { resolveTerminalTheme, terminalThemeCssVariables } from './terminalSettings'

describe('terminal theme UI variables', () => {
  it('keeps terminal chrome colors coordinated with the selected theme', () => {
    expect(terminalThemeCssVariables('tokyo-night')).toMatchObject({
      '--muxvia-bg': '#030712',
      '--muxvia-surface': '#1f2937',
      '--muxvia-surface-raised': '#111827',
      '--muxvia-border': '#374151',
      '--muxvia-border-subtle': 'rgba(255, 255, 255, 0.06)',
      '--muxvia-terminal-bg': '#1a1b26',
      '--muxvia-terminal-fg': '#a9b1d6',
    })

    expect(terminalThemeCssVariables('github-light')).toMatchObject({
      '--muxvia-bg': '#f6f8fa',
      '--muxvia-surface': '#ffffff',
      '--muxvia-surface-raised': '#f6f8fa',
      '--muxvia-border': '#d0d7de',
      '--muxvia-border-subtle': 'rgba(0, 0, 0, 0.08)',
      '--muxvia-terminal-bg': '#ffffff',
      '--muxvia-terminal-fg': '#24292f',
    })
  })

  it('preserves the matching xterm terminal palette for a selected theme', () => {
    expect(resolveTerminalTheme('tokyo-night')).toMatchObject({
      background: '#1a1b26',
      foreground: '#a9b1d6',
      cursor: '#c0caf5',
    })
  })
})
