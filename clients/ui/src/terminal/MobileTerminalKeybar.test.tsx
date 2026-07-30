import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MobileTerminalKeybar } from './MobileTerminalKeybar'

describe('MobileTerminalKeybar', () => {
  afterEach(() => {
    cleanup()
  })

  it('highlights the keyboard button while the system keyboard is visible', () => {
    render(<MobileTerminalKeybar onInput={vi.fn()} keyboardVisible />)

    const keyboardButton = screen.getByRole('button', { name: /toggle system keyboard/i })

    expect(keyboardButton.getAttribute('aria-pressed')).toBe('true')
    expect(keyboardButton.className).toContain('bg-[var(--anytty-accent)]')
    expect(keyboardButton.className).toContain('text-[var(--anytty-accent-text)]')
  })

  it('keeps the locked keyboard button style above the visible state', () => {
    render(<MobileTerminalKeybar onInput={vi.fn()} keyboardVisible keyboardLocked />)

    const keyboardButton = screen.getByRole('button', { name: /unlock system keyboard/i })

    expect(keyboardButton.getAttribute('aria-pressed')).toBe('true')
    expect(keyboardButton.className).toContain('bg-red-600')
    expect(keyboardButton.className).not.toContain('bg-[var(--anytty-accent)]')
  })

  it('keeps every shortcut target at least 44 pixels in both dimensions', () => {
    render(<MobileTerminalKeybar onInput={vi.fn()} />)

    const keybar = screen.getByTestId('anytty-mobile-keybar')
    const buttons = Array.from(keybar.querySelectorAll('button'))
    expect(buttons.length).toBeGreaterThan(0)
    for (const button of buttons) {
      expect(button.className).toContain('h-11')
      expect(button.className).toContain('min-w-11')
    }
    expect(keybar.querySelectorAll('.overflow-x-auto')).toHaveLength(2)
  })
})
