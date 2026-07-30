import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { anyttyI18n } from '../i18n'
import { MobileTerminalKeybar } from './MobileTerminalKeybar'

describe('MobileTerminalKeybar', () => {
  beforeEach(async () => {
    await anyttyI18n.changeLanguage('en')
  })

  afterEach(() => {
    vi.useRealTimers()
    cleanup()
  })

  it('cycles Ctrl and Alt through visible off, once, and locked states', async () => {
    const user = userEvent.setup()
    render(<MobileTerminalKeybar onInput={vi.fn()} />)

    const ctrlOff = screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' })
    const altOff = screen.getByRole('button', { name: 'Alt modifier: off. Next: once' })
    expect(ctrlOff.getAttribute('data-state')).toBe('off')
    expect(altOff.getAttribute('data-state')).toBe('off')

    await user.click(ctrlOff)
    const ctrlOnce = screen.getByRole('button', { name: 'Ctrl modifier: once. Next: locked' })
    expect(ctrlOnce.getAttribute('data-state')).toBe('once')
    expect(ctrlOnce.textContent).toContain('once')

    await user.click(ctrlOnce)
    const ctrlLocked = screen.getByRole('button', { name: 'Ctrl modifier: locked. Next: off' })
    expect(ctrlLocked.getAttribute('data-state')).toBe('locked')
    expect(ctrlLocked.textContent).toContain('locked')

    await user.click(ctrlLocked)
    expect(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' }).getAttribute('data-state')).toBe('off')
  })

  it('consumes a one-shot Ctrl modifier after the first actual key sequence', async () => {
    const user = userEvent.setup()
    const onInput = vi.fn()
    render(<MobileTerminalKeybar onInput={onInput} />)

    await user.click(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' }))
    await user.click(screen.getByRole('button', { name: '\\' }))
    await user.click(screen.getByRole('button', { name: '\\' }))

    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['\x1c', '\\'])
    expect(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' })).toBeTruthy()
  })

  it('keeps locked Ctrl and Alt modifiers across actual key sequences', async () => {
    const user = userEvent.setup()
    const onInput = vi.fn()
    render(<MobileTerminalKeybar onInput={onInput} />)

    await user.click(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' }))
    await user.click(screen.getByRole('button', { name: 'Ctrl modifier: once. Next: locked' }))
    await user.click(screen.getByRole('button', { name: 'Alt modifier: off. Next: once' }))
    await user.click(screen.getByRole('button', { name: 'Alt modifier: once. Next: locked' }))
    await user.click(screen.getByRole('button', { name: '\\' }))
    await user.click(screen.getByRole('button', { name: '\\' }))

    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['\x1b\x1c', '\x1b\x1c'])
    expect(screen.getByRole('button', { name: 'Ctrl modifier: locked. Next: off' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Alt modifier: locked. Next: off' })).toBeTruthy()
  })

  it('uses separate direct controls for keyboard visibility and keyboard lock', async () => {
    const user = userEvent.setup()
    const onFocusKeyboard = vi.fn()
    const onBlurKeyboard = vi.fn()
    const onToggleKeyboardLock = vi.fn()
    const view = render(
      <MobileTerminalKeybar
        onInput={vi.fn()}
        onFocusKeyboard={onFocusKeyboard}
        onBlurKeyboard={onBlurKeyboard}
        onToggleKeyboardLock={onToggleKeyboardLock}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Show system keyboard' }))
    expect(onFocusKeyboard).toHaveBeenCalledOnce()
    expect(onBlurKeyboard).not.toHaveBeenCalled()
    expect(onToggleKeyboardLock).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Lock system keyboard' }))
    expect(onToggleKeyboardLock).toHaveBeenCalledOnce()

    view.rerender(
      <MobileTerminalKeybar
        onInput={vi.fn()}
        onFocusKeyboard={onFocusKeyboard}
        onBlurKeyboard={onBlurKeyboard}
        onToggleKeyboardLock={onToggleKeyboardLock}
        keyboardVisible
        keyboardLocked
      />,
    )

    const keyboardButton = screen.getByRole('button', { name: 'Hide system keyboard' }) as HTMLButtonElement
    const lockButton = screen.getByRole('button', { name: 'Unlock system keyboard' })

    expect(keyboardButton.getAttribute('aria-pressed')).toBe('true')
    expect(keyboardButton.disabled).toBe(true)
    expect(lockButton.getAttribute('aria-pressed')).toBe('true')
    expect(lockButton.className).toContain('bg-amber-300')
  })

  it('does not attach modifier or keyboard actions to long press, double click, or swipe', () => {
    vi.useFakeTimers()
    const onInput = vi.fn()
    const onFocusKeyboard = vi.fn()
    const onToggleKeyboardLock = vi.fn()
    render(
      <MobileTerminalKeybar
        onInput={onInput}
        onFocusKeyboard={onFocusKeyboard}
        onToggleKeyboardLock={onToggleKeyboardLock}
      />,
    )

    const ctrl = screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' })
    const keyboard = screen.getByRole('button', { name: 'Show system keyboard' })
    fireEvent.pointerDown(ctrl)
    fireEvent.pointerDown(keyboard)
    fireEvent.touchMove(screen.getByTestId('anytty-mobile-keybar'), {
      touches: [{ clientX: 20, clientY: 20 }],
    })
    expect(vi.getTimerCount()).toBe(0)
    act(() => vi.advanceTimersByTime(1_000))

    expect(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' })).toBeTruthy()
    expect(onInput).not.toHaveBeenCalled()
    expect(onFocusKeyboard).not.toHaveBeenCalled()
    expect(onToggleKeyboardLock).not.toHaveBeenCalled()

    fireEvent.click(keyboard)
    fireEvent.click(keyboard)
    expect(onFocusKeyboard).toHaveBeenCalledTimes(2)
    expect(onToggleKeyboardLock).not.toHaveBeenCalled()
  })

  it('localizes all three modifier states and keyboard aria labels', async () => {
    const view = render(
      <MobileTerminalKeybar
        onInput={vi.fn()}
        modifierState={{ ctrl: 'off', alt: 'locked' }}
        keyboardVisible
      />,
    )

    expect(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Alt modifier: locked. Next: off' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Hide system keyboard' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Lock system keyboard' })).toBeTruthy()

    await act(async () => {
      await anyttyI18n.changeLanguage('zh-CN')
    })
    view.rerender(
      <MobileTerminalKeybar
        onInput={vi.fn()}
        modifierState={{ ctrl: 'once', alt: 'locked' }}
        keyboardLocked
      />,
    )

    expect(screen.getByRole('button', { name: 'Ctrl 修饰键：单次。下一状态：锁定' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Alt 修饰键：锁定。下一状态：关闭' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '显示系统键盘' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '解锁系统键盘' })).toBeTruthy()
  })

  it('keeps every shortcut target at a stable 44 pixels in both dimensions', async () => {
    const user = userEvent.setup()
    render(<MobileTerminalKeybar onInput={vi.fn()} />)

    const keybar = screen.getByTestId('anytty-mobile-keybar')
    const buttons = Array.from(keybar.querySelectorAll('button'))
    expect(buttons.length).toBeGreaterThan(0)
    for (const button of buttons) {
      expect(button.className).toContain('h-11')
      expect(button.className).toContain('w-11')
      expect(button.className).toContain('min-w-11')
    }
    expect(keybar.querySelectorAll('[data-key-id="keyboard-visibility"]')).toHaveLength(1)
    expect(keybar.querySelectorAll('[data-key-id="keyboard-lock"]')).toHaveLength(1)
    expect(keybar.querySelectorAll('.overflow-x-auto')).toHaveLength(2)

    await user.click(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' }))
    await user.click(screen.getByRole('button', { name: 'Ctrl modifier: once. Next: locked' }))
    const lockedCtrl = screen.getByRole('button', { name: 'Ctrl modifier: locked. Next: off' })
    expect(lockedCtrl.className).toContain('h-11')
    expect(lockedCtrl.className).toContain('w-11')
    expect(lockedCtrl.className).toContain('min-w-11')
  })
})
