import { useCallback, useState } from 'react'
import {
  applyTerminalModifiers,
  nextModifierState,
  type ModifierState,
  type TerminalModifierState,
} from './mobileTerminalInput'

export interface MobileTerminalKeybarProps {
  onInput: (data: string) => void
  onFocusKeyboard?: (() => void) | undefined
  onBlurKeyboard?: (() => void) | undefined
  modifierState?: TerminalModifierState | undefined
  onModifierStateChange?: ((state: TerminalModifierState) => void) | undefined
  className?: string | undefined
}

export function MobileTerminalKeybar({
  onInput,
  onFocusKeyboard,
  onBlurKeyboard,
  modifierState,
  onModifierStateChange,
  className,
}: MobileTerminalKeybarProps) {
  const [internalModifierState, setInternalModifierState] = useState<TerminalModifierState>({ ctrl: 'off', alt: 'off' })
  const [keyboardLocked, setKeyboardLocked] = useState(false)
  const activeModifierState = modifierState ?? internalModifierState
  const setModifierState = onModifierStateChange ?? setInternalModifierState

  const send = useCallback((data: string) => {
    // Light haptic feedback
    if (typeof navigator !== 'undefined' && navigator.vibrate) {
      navigator.vibrate(10)
    }
    const result = applyTerminalModifiers(data, activeModifierState)
    setModifierState({ ctrl: result.ctrl, alt: result.alt })
    onInput(result.data)
  }, [activeModifierState, onInput, setModifierState])

  const toggleKeyboard = useCallback(() => {
    setKeyboardLocked((current) => {
      const next = !current
      if (next) onBlurKeyboard?.()
      else onFocusKeyboard?.()
      return next
    })
  }, [onBlurKeyboard, onFocusKeyboard])

  return (
    <div
      className={`shrink-0 border-t border-zinc-800 bg-zinc-950 px-1.5 py-1 text-zinc-100 md:hidden ${className || ''}`}
      data-testid="termx-mobile-keybar"
    >
      <div className="grid grid-cols-9 gap-1">
        {keyButton('Esc', '\x1b', send)}
        {keyButton('/', '/', send)}
        {keyButton('|', '|', send)}
        {keyButton('-', '-', send)}
        {keyButton('Home', '\x1b[H', send)}
        {keyButton('↑', '\x1b[A', send)}
        {keyButton('End', '\x1b[F', send)}
        {keyButton('PgU', '\x1b[5~', send)}
        <button
          type="button"
          aria-label={keyboardLocked ? 'Unlock system keyboard' : 'Lock system keyboard'}
          className={`min-h-8 rounded px-1 text-center font-mono text-[11px] select-none touch-manipulation ${keyboardLocked ? 'bg-red-600 text-white' : 'bg-zinc-800 text-zinc-200 active:bg-zinc-700'}`}
          onPointerDown={(e) => {
            e.preventDefault()
            e.currentTarget.dataset.pointerHandled = '1'
            toggleKeyboard()
          }}
          onClick={(e) => {
            if (e.currentTarget.dataset.pointerHandled === '1') {
              delete e.currentTarget.dataset.pointerHandled
              return
            }
            toggleKeyboard()
          }}
        >
          ⌨
        </button>
      </div>
      <div className="mt-1 grid grid-cols-9 gap-1">
        {keyButton('⇥', '\t', send, 'Tab key')}
        {modifierButton('Ctrl', activeModifierState.ctrl, () => setModifierState({
          ...activeModifierState,
          ctrl: nextModifierState(activeModifierState.ctrl),
        }))}
        {modifierButton('Alt', activeModifierState.alt, () => setModifierState({
          ...activeModifierState,
          alt: nextModifierState(activeModifierState.alt),
        }))}
        {keyButton('\\', '\\', send)}
        {keyButton('←', '\x1b[D', send)}
        {keyButton('↓', '\x1b[B', send)}
        {keyButton('→', '\x1b[C', send)}
        {keyButton('PgD', '\x1b[6~', send)}
        {keyButton('Fn', '\x1bOP', send)}
      </div>
    </div>
  )
}

function keyButton(label: string, data: string, send: (data: string) => void, ariaLabel?: string) {
  return (
    <button
      key={`${label}:${data}`}
      type="button"
      aria-label={ariaLabel}
      className="min-h-8 rounded bg-zinc-800 px-1 text-center font-mono text-[11px] text-zinc-100 active:bg-zinc-700 select-none touch-manipulation"
      onPointerDown={(e) => {
        e.preventDefault()
        e.currentTarget.dataset.pointerHandled = '1'
        send(data)
      }}
      onClick={(e) => {
        if (e.currentTarget.dataset.pointerHandled === '1') {
          delete e.currentTarget.dataset.pointerHandled
          return
        }
        send(data)
      }}
    >
      {label}
    </button>
  )
}

function modifierButton(label: string, state: ModifierState, onClick: () => void) {
  const activeClass = state === 'locked'
    ? 'bg-amber-600 text-white'
    : state === 'once'
      ? 'bg-blue-600 text-white'
      : 'bg-zinc-800 text-zinc-100 active:bg-zinc-700'

  return (
    <button
      key={label}
      type="button"
      aria-pressed={state !== 'off'}
      className={`relative min-h-8 rounded px-1 text-center font-mono text-[11px] select-none touch-manipulation ${activeClass}`}
      onPointerDown={(e) => {
        e.preventDefault()
        e.currentTarget.dataset.pointerHandled = '1'
        onClick()
      }}
      onClick={(e) => {
        if (e.currentTarget.dataset.pointerHandled === '1') {
          delete e.currentTarget.dataset.pointerHandled
          return
        }
        onClick()
      }}
    >
      {label}
      {state === 'locked' ? <span className="absolute bottom-0.5 left-1/2 h-0.5 w-3 -translate-x-1/2 rounded-full bg-white" /> : null}
    </button>
  )
}
