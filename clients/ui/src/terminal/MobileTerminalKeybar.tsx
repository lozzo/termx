import { forwardRef, useState, useCallback, useRef, useEffect, useLayoutEffect, type Ref } from 'react'
import {
  applyTerminalModifiers,
  type ModifierState,
  type TerminalModifierState,
} from './mobileTerminalInput'
import { hapticImpact, hapticSelection } from '../platform/haptics'
import { useTranslation } from 'react-i18next'
import '../i18n'

export interface MobileTerminalKeybarProps {
  onInput: (data: string) => void
  onFocusKeyboard?: (() => void) | undefined
  onBlurKeyboard?: (() => void) | undefined
  onLockKeyboard?: (() => void) | undefined
  fnOpen?: boolean | undefined
  onToggleFn?: (() => void) | undefined
  modifierState?: TerminalModifierState | undefined
  onModifierStateChange?: ((state: TerminalModifierState) => void) | undefined
  keyboardVisible?: boolean | undefined
  keyboardLocked?: boolean | undefined
  className?: string | undefined
}

const DOUBLE_TAP_MS = 300
const LONG_PRESS_MS = 400

function useModifierToggle(state: ModifierState, setState: (v: ModifierState) => void) {
  const lastTapRef = useRef(0)
  const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const longPressTriggered = useRef(false)

  const clearLP = useCallback(() => {
    if (longPressTimer.current) {
      clearTimeout(longPressTimer.current)
      longPressTimer.current = null
    }
  }, [])

  const handlePointerDown = useCallback((e: React.PointerEvent) => {
    e.preventDefault()
    longPressTriggered.current = false
    clearLP()
    longPressTimer.current = setTimeout(() => {
      longPressTriggered.current = true
      hapticImpact()
      setState('locked')
    }, LONG_PRESS_MS)
  }, [setState, clearLP])

  const handlePointerUp = useCallback(() => { clearLP() }, [clearLP])

  const handleClick = useCallback(() => {
    if (longPressTriggered.current) { longPressTriggered.current = false; return }
    const now = Date.now()
    if (state === 'off') {
      setState('once')
      lastTapRef.current = now
    } else if (state === 'once') {
      if (now - lastTapRef.current < DOUBLE_TAP_MS) {
        setState('locked')
      } else {
        setState('off')
      }
    } else {
      setState('off')
    }
  }, [state, setState])

  return { handlePointerDown, handlePointerUp, handleClick }
}

function KeyPopup({ label }: { label: string }) {
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const rect = el.getBoundingClientRect()
    const pad = 4
    if (rect.left < pad) {
      el.style.transform = `translateX(calc(-50% + ${pad - rect.left}px))`
    } else if (rect.right > window.innerWidth - pad) {
      el.style.transform = `translateX(calc(-50% - ${rect.right - (window.innerWidth - pad)}px))`
    } else {
      el.style.transform = 'translateX(-50%)'
    }
  })
  return (
    <div
      ref={ref}
      className="pointer-events-none absolute bottom-full left-1/2 z-50 mb-0.5 -translate-x-1/2"
      style={{ overflow: 'visible' }}
    >
      <div className="min-w-[2.5rem] border border-[var(--anytty-border-subtle)] bg-[var(--anytty-surface-raised)] px-4 py-2 text-center font-mono text-base font-bold text-[var(--anytty-text)] whitespace-nowrap">
        {label}
      </div>
      <div
        className="mx-auto h-0 w-0 border-l-[7px] border-r-[7px] border-t-[7px] border-l-transparent border-r-transparent"
        style={{ borderTopColor: 'var(--anytty-surface-raised)' }}
      />
    </div>
  )
}

export const MobileTerminalKeybar = forwardRef<HTMLDivElement, MobileTerminalKeybarProps>(function MobileTerminalKeybar({
  onInput,
  onFocusKeyboard,
  onBlurKeyboard,
  onLockKeyboard,
  fnOpen = false,
  onToggleFn,
  modifierState,
  onModifierStateChange,
  keyboardVisible = false,
  keyboardLocked = false,
  className,
}: MobileTerminalKeybarProps, forwardedRef: Ref<HTMLDivElement>) {
  const { t } = useTranslation()
  const [internalModifierState, setInternalModifierState] = useState<TerminalModifierState>({ ctrl: 'off', alt: 'off' })
  const activeModifierState = modifierState ?? internalModifierState
  const setModifierState = onModifierStateChange ?? setInternalModifierState

  const [pressedKey, setPressedKey] = useState<string | null>(null)
  const pressTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const barRef = useRef<HTMLDivElement>(null)
  const setBarRef = useCallback((node: HTMLDivElement | null) => {
    barRef.current = node
    if (typeof forwardedRef === 'function') {
      forwardedRef(node)
    } else if (forwardedRef) {
      ;(forwardedRef as { current: HTMLDivElement | null }).current = node
    }
  }, [forwardedRef])
  const touchingRef = useRef(false)

  const kbLastTapRef = useRef(0)
  const kbLongPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const kbLongPressTriggered = useRef(false)

  const showPress = useCallback((key: string) => {
    setPressedKey(key)
    hapticSelection()
    if (pressTimer.current) clearTimeout(pressTimer.current)
    pressTimer.current = setTimeout(() => setPressedKey(null), 400)
  }, [])

  // Swipe-across detection
  useEffect(() => {
    const bar = barRef.current
    if (!bar) return
    const onTouchStart = () => { touchingRef.current = true }
    const onTouchEnd = () => { touchingRef.current = false }
    const onTouchMove = (e: TouchEvent) => {
      if (!touchingRef.current || e.touches.length < 1) return
      const touch = e.touches[0]!
      const el = document.elementFromPoint(touch.clientX, touch.clientY) as HTMLElement | null
      const btn = el?.closest('[data-key-id]') as HTMLElement | null
      if (btn) {
        const id = btn.getAttribute('data-key-id')!
        if (id !== pressedKey) {
          setPressedKey(id)
          if (pressTimer.current) clearTimeout(pressTimer.current)
          pressTimer.current = setTimeout(() => setPressedKey(null), 400)
        }
      }
    }
    bar.addEventListener('touchstart', onTouchStart, { passive: true })
    bar.addEventListener('touchmove', onTouchMove, { passive: true })
    bar.addEventListener('touchend', onTouchEnd, { passive: true })
    bar.addEventListener('touchcancel', onTouchEnd, { passive: true })
    return () => {
      bar.removeEventListener('touchstart', onTouchStart)
      bar.removeEventListener('touchmove', onTouchMove)
      bar.removeEventListener('touchend', onTouchEnd)
      bar.removeEventListener('touchcancel', onTouchEnd)
    }
  }, [pressedKey])

  const send = useCallback((data: string) => {
    const result = applyTerminalModifiers(data, activeModifierState)
    setModifierState({ ctrl: result.ctrl, alt: result.alt })
    onInput(result.data)
  }, [activeModifierState, onInput, setModifierState])

  const ctrlToggle = useModifierToggle(activeModifierState.ctrl, (v) => setModifierState({ ...activeModifierState, ctrl: v }))
  const altToggle = useModifierToggle(activeModifierState.alt, (v) => setModifierState({ ...activeModifierState, alt: v }))

  const cls = 'relative flex h-8 min-w-0 flex-[1_1_0] touch-manipulation select-none items-center justify-center overflow-visible border-x border-transparent text-center font-mono text-[10px] font-medium'
  const keyboardButtonClass = keyboardLocked
    ? 'bg-red-600 text-white'
    : keyboardVisible
      ? 'bg-[var(--anytty-accent)] text-[var(--anytty-accent-text)]'
      : 'bg-transparent text-[var(--anytty-muted)] active:bg-[var(--anytty-surface-raised)]'

  const btn = (label: string, data: string, ariaLabel?: string) => {
    const id = label + ':' + data
    return (
      <button
        key={id}
        data-key-id={id}
        type="button"
        aria-label={ariaLabel}
        className={`${cls} bg-[var(--anytty-surface-raised)] text-[var(--anytty-text)] active:opacity-70`}
        onPointerDown={(e) => { e.preventDefault(); showPress(id); }}
        onClick={() => send(data)}
      >
        {label}
        {pressedKey === id && <KeyPopup label={label} />}
      </button>
    )
  }

  const modBtn = (label: string, state: ModifierState, toggle: ReturnType<typeof useModifierToggle>) => {
    const stateClass = state === 'locked'
      ? 'bg-amber-500 text-white'
      : state === 'once'
        ? 'bg-[var(--anytty-accent)] text-[var(--anytty-accent-text)]'
        : 'bg-[var(--anytty-surface-raised)] text-[var(--anytty-text)] active:opacity-70'
    return (
      <button
        key={label}
        data-key-id={label}
        type="button"
        aria-pressed={state !== 'off'}
        className={`${cls} ${stateClass}`}
        onPointerDown={(e) => { toggle.handlePointerDown(e); showPress(label) }}
        onPointerUp={toggle.handlePointerUp}
        onPointerCancel={toggle.handlePointerUp}
        onClick={toggle.handleClick}
      >
        {label}
        {state === 'locked' && (
          <span className="absolute bottom-0.5 left-1/2 h-0.5 w-3 -translate-x-1/2 rounded-full bg-white/80" />
        )}
        {pressedKey === label && <KeyPopup label={label} />}
      </button>
    )
  }

  return (
    <div
      ref={setBarRef}
      className={`min-w-0 shrink-0 overflow-x-hidden border-t border-[var(--anytty-border-subtle)] bg-[var(--anytty-surface)] text-[var(--anytty-text)] md:hidden ${className || ''}`}
      data-testid="anytty-mobile-keybar"
      style={{ paddingBottom: 'calc(0.375rem + env(safe-area-inset-bottom))', overflow: 'visible' }}
    >
      <div className="flex min-w-0 gap-1 px-1.5 pt-1.5" style={{ overflow: 'visible' }}>
        {btn('Esc', '\x1b')}
        {btn('/', '/')}
        {btn('|', '|')}
        {btn('-', '-')}
        {btn('Home', '\x1b[H')}
        {btn('↑', '\x1b[A')}
        {btn('End', '\x1b[F')}
        {btn('PgU', '\x1b[5~')}
        <button
          data-key-id="⌨"
          type="button"
          aria-label={t(keyboardLocked ? 'terminal.tools.unlockKeyboard' : 'terminal.tools.toggleKeyboard')}
          aria-pressed={keyboardLocked || keyboardVisible}
          className={`${cls} ${keyboardButtonClass}`}
          onMouseDown={(e) => e.preventDefault()}
          onPointerDown={(e) => {
            e.preventDefault()
            showPress('⌨')
            kbLongPressTriggered.current = false
            if (kbLongPressTimer.current) clearTimeout(kbLongPressTimer.current)
            kbLongPressTimer.current = setTimeout(() => {
              kbLongPressTriggered.current = true
              hapticImpact()
              onLockKeyboard?.()
            }, LONG_PRESS_MS)
          }}
          onPointerUp={() => {
            if (kbLongPressTimer.current) { clearTimeout(kbLongPressTimer.current); kbLongPressTimer.current = null }
          }}
          onPointerCancel={() => {
            if (kbLongPressTimer.current) { clearTimeout(kbLongPressTimer.current); kbLongPressTimer.current = null }
          }}
          onClick={() => {
            if (kbLongPressTriggered.current) { kbLongPressTriggered.current = false; return }
            const now = Date.now()
            if (now - kbLastTapRef.current < DOUBLE_TAP_MS) {
              onLockKeyboard?.()
              kbLastTapRef.current = 0
            } else {
              if (keyboardLocked) {
                onLockKeyboard?.()
                onFocusKeyboard?.()
              } else if (keyboardVisible) {
                onBlurKeyboard?.()
              } else {
                onFocusKeyboard?.()
              }
              kbLastTapRef.current = now
            }
          }}
        >
          ⌨
          {keyboardLocked && (
            <span className="absolute bottom-0.5 left-1/2 h-0.5 w-3 -translate-x-1/2 rounded-full bg-white/80" />
          )}
          {pressedKey === '⌨' && <KeyPopup label="⌨" />}
        </button>
      </div>
      <div className="flex min-w-0 gap-1 px-1.5 pb-1.5 pt-1" style={{ overflow: 'visible' }}>
        {btn('⇥', '\t', t('terminal.tools.tabKey'))}
        {modBtn('Ctrl', activeModifierState.ctrl, ctrlToggle)}
        {modBtn('Alt', activeModifierState.alt, altToggle)}
        {btn('\\', '\\')}
        {btn('←', '\x1b[D')}
        {btn('↓', '\x1b[B')}
        {btn('→', '\x1b[C')}
        {btn('PgD', '\x1b[6~')}
        <button
          data-key-id="Fn"
          type="button"
          aria-label={t('terminal.tools.toggleFn')}
          aria-pressed={fnOpen}
          className={`${cls} ${fnOpen ? 'bg-[var(--anytty-accent)] text-[var(--anytty-accent-text)]' : 'bg-transparent text-[var(--anytty-muted)] active:bg-[var(--anytty-surface-raised)]'}`}
          onPointerDown={(e) => { e.preventDefault(); showPress('Fn') }}
          onClick={() => onToggleFn?.()}
        >
          Fn
          {pressedKey === 'Fn' && <KeyPopup label="Fn" />}
        </button>
      </div>
    </div>
  )
})
