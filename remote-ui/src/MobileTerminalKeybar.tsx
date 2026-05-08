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
  fnOpen?: boolean | undefined
  onToggleFn?: (() => void) | undefined
  modifierState?: TerminalModifierState | undefined
  onModifierStateChange?: ((state: TerminalModifierState) => void) | undefined
  className?: string | undefined
}

export function MobileTerminalKeybar({
  onInput,
  onFocusKeyboard,
  onBlurKeyboard,
  fnOpen = false,
  onToggleFn,
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
      className={`absolute inset-x-0 bottom-0 z-30 border-t border-zinc-800/70 bg-zinc-950/90 px-1.5 py-1.5 text-zinc-100 shadow-[0_-4px_18px_rgba(0,0,0,0.28)] backdrop-blur-lg md:hidden ${className || ''}`}
      data-testid="termx-mobile-keybar"
      style={{ paddingBottom: 'calc(0.375rem + env(safe-area-inset-bottom))' }}
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
          className={`flex h-8 flex-col items-center justify-center rounded-md text-center font-mono text-[11px] font-medium transition-transform active:scale-95 select-none touch-manipulation ${keyboardLocked ? 'bg-red-500 text-white shadow-sm shadow-red-500/20' : 'bg-zinc-800 text-zinc-300 hover:bg-zinc-700 active:bg-zinc-600'}`}
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
        <button
          type="button"
          aria-label="Toggle Fn shortcuts"
          aria-pressed={fnOpen}
          className={`flex h-8 flex-col items-center justify-center rounded-md text-center font-mono text-[10px] font-medium shadow-sm transition-transform active:scale-95 select-none touch-manipulation ${fnOpen ? 'bg-blue-500 text-white shadow-blue-500/20' : 'bg-zinc-800 text-zinc-300 hover:bg-zinc-700 active:bg-zinc-600'}`}
          onPointerDown={(e) => {
            e.preventDefault()
            e.currentTarget.dataset.pointerHandled = '1'
            onToggleFn?.()
          }}
          onClick={(e) => {
            if (e.currentTarget.dataset.pointerHandled === '1') {
              delete e.currentTarget.dataset.pointerHandled
              return
            }
            onToggleFn?.()
          }}
        >
          Fn
        </button>
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
      className="flex h-8 flex-col items-center justify-center rounded-md bg-zinc-800 text-center font-mono text-[10px] font-medium text-zinc-300 shadow-sm transition-transform hover:bg-zinc-700 active:scale-95 active:bg-zinc-600 select-none touch-manipulation"
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
    ? 'bg-amber-500 text-white shadow-sm shadow-amber-500/20'
    : state === 'once'
      ? 'bg-blue-500 text-white shadow-sm shadow-blue-500/20'
      : 'bg-zinc-800 text-zinc-300 hover:bg-zinc-700 active:bg-zinc-600'

  return (
    <button
      key={label}
      type="button"
      aria-pressed={state !== 'off'}
      className={`relative flex h-8 flex-col items-center justify-center rounded-md text-center font-mono text-[10px] font-medium shadow-sm transition-transform active:scale-95 select-none touch-manipulation ${activeClass}`}
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
      {state === 'locked' ? <span className="absolute bottom-0.5 left-1/2 h-0.5 w-3 -translate-x-1/2 rounded-full bg-white/80" /> : null}
    </button>
  )
}
