import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { anyttyI18n } from '../i18n'
import { MobileTerminalKeybar } from './MobileTerminalKeybar'

const haptics = vi.hoisted(() => ({
  impact: vi.fn(),
  selection: vi.fn(),
}))

vi.mock('../platform/haptics', () => ({
  hapticImpact: haptics.impact,
  hapticSelection: haptics.selection,
}))

const acceptInput = () => true

describe('MobileTerminalKeybar', () => {
  beforeEach(async () => {
    haptics.impact.mockReset()
    haptics.selection.mockReset()
    await anyttyI18n.changeLanguage('en')
  })

  afterEach(() => {
    vi.useRealTimers()
    cleanup()
  })

  it('cycles Ctrl and Alt through visible off, once, and locked states', async () => {
    const user = userEvent.setup()
    render(<MobileTerminalKeybar onInput={acceptInput} />)

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

  it('consumes a one-shot modifier only after an input target accepts the send', async () => {
    const user = userEvent.setup()
    const onInput = vi.fn()
      .mockReturnValueOnce(false)
      .mockReturnValueOnce(true)
      .mockReturnValueOnce(true)
    render(<MobileTerminalKeybar onInput={onInput} />)

    await user.click(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' }))
    await user.click(screen.getByRole('button', { name: '\\' }))
    expect(screen.getByRole('button', { name: 'Ctrl modifier: once. Next: locked' })).toBeTruthy()
    expect(haptics.selection).toHaveBeenCalledTimes(1)

    await user.click(screen.getByRole('button', { name: '\\' }))
    expect(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' })).toBeTruthy()

    await user.click(screen.getByRole('button', { name: '\\' }))
    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['\x1c', '\x1c', '\\'])
    expect(haptics.selection).toHaveBeenCalledTimes(3)
  })

  it('keeps locked Ctrl and Alt modifiers across accepted key sequences', async () => {
    const user = userEvent.setup()
    const onInput = vi.fn(acceptInput)
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

  it('sends keybar navigation with combined xterm modifiers and consumes accepted once states', async () => {
    const user = userEvent.setup()
    const onInput = vi.fn(acceptInput)
    render(<MobileTerminalKeybar onInput={onInput} />)

    await user.click(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' }))
    await user.click(screen.getByRole('button', { name: 'Alt modifier: off. Next: once' }))
    await user.click(screen.getByRole('button', { name: '↑' }))

    expect(onInput).toHaveBeenCalledWith('\x1b[1;7A')
    expect(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Alt modifier: off. Next: once' })).toBeTruthy()
  })

  it('keeps show/hide disabled while focus lock prevents the system keyboard from opening', async () => {
    const user = userEvent.setup()
    const onFocusKeyboard = vi.fn()
    const onBlurKeyboard = vi.fn()
    const onToggleKeyboardFocusLock = vi.fn()
    const view = render(
      <MobileTerminalKeybar
        onInput={acceptInput}
        onFocusKeyboard={onFocusKeyboard}
        onBlurKeyboard={onBlurKeyboard}
        onToggleKeyboardFocusLock={onToggleKeyboardFocusLock}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Show system keyboard' }))
    expect(onFocusKeyboard).toHaveBeenCalledOnce()
    expect(onBlurKeyboard).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Prevent the system keyboard from opening' }))
    expect(onToggleKeyboardFocusLock).toHaveBeenCalledOnce()

    view.rerender(
      <MobileTerminalKeybar
        onInput={acceptInput}
        onFocusKeyboard={onFocusKeyboard}
        onBlurKeyboard={onBlurKeyboard}
        onToggleKeyboardFocusLock={onToggleKeyboardFocusLock}
        keyboardFocusLocked
      />,
    )

    const keyboardButton = screen.getByRole('button', { name: 'Show system keyboard' }) as HTMLButtonElement
    const focusLockButton = screen.getByRole('button', { name: 'Allow the system keyboard to open' })
    expect(keyboardButton.disabled).toBe(true)
    expect(focusLockButton.getAttribute('aria-pressed')).toBe('true')
    expect(focusLockButton.textContent).toContain('Allow the system keyboard to open')
    expect(focusLockButton.className).toContain('bg-amber-300')
  })

  it('continues sending terminal shortcuts while keyboard focus is locked', async () => {
    const user = userEvent.setup()
    const onInput = vi.fn(acceptInput)
    render(<MobileTerminalKeybar onInput={onInput} keyboardFocusLocked />)

    await user.click(screen.getByRole('button', { name: '\\' }))

    expect(onInput).toHaveBeenCalledWith('\\')
    expect(haptics.selection).toHaveBeenCalledOnce()
  })

  it('does not attach modifier or keyboard actions to long press, double click, or swipe', () => {
    vi.useFakeTimers()
    const onInput = vi.fn(acceptInput)
    const onFocusKeyboard = vi.fn()
    const onToggleKeyboardFocusLock = vi.fn()
    render(
      <MobileTerminalKeybar
        onInput={onInput}
        onFocusKeyboard={onFocusKeyboard}
        onToggleKeyboardFocusLock={onToggleKeyboardFocusLock}
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
    expect(onToggleKeyboardFocusLock).not.toHaveBeenCalled()
    expect(haptics.selection).not.toHaveBeenCalled()

    fireEvent.click(keyboard)
    fireEvent.click(keyboard)
    expect(onFocusKeyboard).toHaveBeenCalledTimes(2)
    expect(onToggleKeyboardFocusLock).not.toHaveBeenCalled()
  })

  it('emits haptic feedback once for an accepted tap and never for pointer cancellation or failure', () => {
    const onInput = vi.fn()
      .mockReturnValueOnce(false)
      .mockReturnValueOnce(true)
    render(<MobileTerminalKeybar onInput={onInput} />)
    const slash = screen.getByRole('button', { name: '/' })

    fireEvent.pointerDown(slash)
    fireEvent.pointerCancel(slash)
    expect(haptics.selection).not.toHaveBeenCalled()

    fireEvent.click(slash)
    expect(haptics.selection).not.toHaveBeenCalled()

    fireEvent.pointerDown(slash)
    fireEvent.click(slash)
    expect(haptics.selection).toHaveBeenCalledOnce()
    expect(onInput).toHaveBeenCalledTimes(2)
  })

  it('localizes modifier states and keyboard focus-lock aria labels', async () => {
    const view = render(
      <MobileTerminalKeybar
        onInput={acceptInput}
        modifierState={{ ctrl: 'off', alt: 'locked' }}
        keyboardVisible
      />,
    )

    expect(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Alt modifier: locked. Next: off' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Hide system keyboard' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Prevent the system keyboard from opening' })).toBeTruthy()

    await act(async () => {
      await anyttyI18n.changeLanguage('zh-CN')
    })
    view.rerender(
      <MobileTerminalKeybar
        onInput={acceptInput}
        modifierState={{ ctrl: 'once', alt: 'locked' }}
        keyboardFocusLocked
      />,
    )

    expect(screen.getByRole('button', { name: 'Ctrl 修饰键：单次。下一状态：锁定' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Alt 修饰键：锁定。下一状态：关闭' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '显示系统键盘' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '允许系统键盘弹出' }).textContent).toContain('允许系统键盘弹出')
  })

  it('keeps every shortcut target at a stable 44 pixels in both dimensions', async () => {
    const user = userEvent.setup()
    render(<MobileTerminalKeybar onInput={acceptInput} />)

    const keybar = screen.getByTestId('anytty-mobile-keybar')
    const buttons = Array.from(keybar.querySelectorAll('button'))
    expect(buttons.length).toBeGreaterThan(0)
    for (const button of buttons) {
      expect(button.className).toContain('h-11')
      if (button.getAttribute('data-key-id') !== 'keyboard-focus-lock') {
        expect(button.className).toContain('w-11')
        expect(button.className).toContain('min-w-11')
      }
    }
    const keyboardVisibility = keybar.querySelector('[data-key-id="keyboard-visibility"]')
    const keyboardFocusLock = keybar.querySelector('[data-key-id="keyboard-focus-lock"]')
    expect(keyboardVisibility?.className).toContain('w-11')
    expect(keyboardVisibility?.className).toContain('min-w-11')
    expect(keyboardFocusLock?.className).toContain('w-24')
    expect(keyboardFocusLock?.className).toContain('min-w-24')
    expect(keybar.querySelectorAll('.overflow-x-auto')).toHaveLength(2)

    await user.click(screen.getByRole('button', { name: 'Ctrl modifier: off. Next: once' }))
    await user.click(screen.getByRole('button', { name: 'Ctrl modifier: once. Next: locked' }))
    const lockedCtrl = screen.getByRole('button', { name: 'Ctrl modifier: locked. Next: off' })
    expect(lockedCtrl.className).toContain('h-11')
    expect(lockedCtrl.className).toContain('w-11')
    expect(lockedCtrl.className).toContain('min-w-11')
  })
})
