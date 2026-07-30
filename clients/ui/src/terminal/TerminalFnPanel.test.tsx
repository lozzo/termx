import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { TerminalFnPanel } from './TerminalFnPanel'

describe('TerminalFnPanel', () => {
  afterEach(cleanup)

  it('keeps tabs and shortcut buttons at least 44 pixels tall', () => {
    render(<TerminalFnPanel onSend={vi.fn()} />)

    const buttons = screen.getAllByRole('button')
    expect(buttons.length).toBeGreaterThan(1)
    for (const button of buttons) expect(button.className).toContain('min-h-11')
  })
})
