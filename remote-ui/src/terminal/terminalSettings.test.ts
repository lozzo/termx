import { describe, expect, it } from 'vitest'
import { resolveTerminalTheme, terminalThemeCssVariables } from './terminalSettings'

describe('terminal theme UI variables', () => {
  it('keeps terminal chrome colors coordinated with the selected theme', () => {
    expect(terminalThemeCssVariables('tokyo-night')).toMatchObject({
      '--termx-bg': '#030712',
      '--termx-surface': '#1f2937',
      '--termx-surface-raised': '#111827',
      '--termx-border': '#374151',
      '--termx-border-subtle': 'rgba(255, 255, 255, 0.06)',
      '--termx-terminal-bg': '#1a1b26',
      '--termx-terminal-fg': '#a9b1d6',
    })

    expect(terminalThemeCssVariables('github-light')).toMatchObject({
      '--termx-bg': '#f6f8fa',
      '--termx-surface': '#ffffff',
      '--termx-surface-raised': '#f6f8fa',
      '--termx-border': '#d0d7de',
      '--termx-border-subtle': 'rgba(0, 0, 0, 0.08)',
      '--termx-terminal-bg': '#ffffff',
      '--termx-terminal-fg': '#24292f',
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
