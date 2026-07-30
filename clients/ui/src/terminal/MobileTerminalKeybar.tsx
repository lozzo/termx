import { forwardRef, useCallback, useState, type Ref } from 'react'
import { Keyboard, LockKeyhole } from 'lucide-react'
import {
  applyTerminalModifiers,
  nextModifierState,
  type ModifierState,
  type TerminalModifierState,
} from './mobileTerminalInput'
import { hapticImpact, hapticSelection } from '../platform/haptics'
import { useTranslation } from 'react-i18next'
import '../i18n'

export interface MobileTerminalKeybarProps {
  onInput: (data: string) => boolean
  onFocusKeyboard?: (() => void) | undefined
  onBlurKeyboard?: (() => void) | undefined
  onToggleKeyboardFocusLock?: (() => void) | undefined
  fnOpen?: boolean | undefined
  onToggleFn?: (() => void) | undefined
  modifierState?: TerminalModifierState | undefined
  onModifierStateChange?: ((state: TerminalModifierState) => void) | undefined
  keyboardVisible?: boolean | undefined
  keyboardFocusLocked?: boolean | undefined
  className?: string | undefined
}

export const MobileTerminalKeybar = forwardRef<HTMLDivElement, MobileTerminalKeybarProps>(function MobileTerminalKeybar({
  onInput,
  onFocusKeyboard,
  onBlurKeyboard,
  onToggleKeyboardFocusLock,
  fnOpen = false,
  onToggleFn,
  modifierState,
  onModifierStateChange,
  keyboardVisible = false,
  keyboardFocusLocked = false,
  className,
}: MobileTerminalKeybarProps, forwardedRef: Ref<HTMLDivElement>) {
  const { t } = useTranslation()
  const [internalModifierState, setInternalModifierState] = useState<TerminalModifierState>({ ctrl: 'off', alt: 'off' })
  const activeModifierState = modifierState ?? internalModifierState
  const setModifierState = onModifierStateChange ?? setInternalModifierState

  const send = useCallback((data: string): boolean => {
    const result = applyTerminalModifiers(data, activeModifierState)
    const accepted = onInput(result.data)
    if (!accepted) return false
    hapticSelection()
    if (result.ctrl !== activeModifierState.ctrl || result.alt !== activeModifierState.alt) {
      setModifierState({ ctrl: result.ctrl, alt: result.alt })
    }
    return true
  }, [activeModifierState, onInput, setModifierState])

  const buttonBaseClass = 'relative flex h-11 flex-none touch-manipulation select-none items-center justify-center overflow-hidden border-x border-transparent text-center font-mono text-[10px] font-medium'
  const cls = `${buttonBaseClass} w-11 min-w-11`
  const keyboardButtonClass = keyboardVisible
    ? 'bg-[var(--anytty-accent)] text-[var(--anytty-accent-text)]'
    : 'bg-transparent text-[var(--anytty-muted)] active:bg-[var(--anytty-surface-raised)]'
  const keyboardFocusLockButtonClass = keyboardFocusLocked
    ? 'bg-amber-300 text-zinc-950'
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
        onPointerDown={(e) => e.preventDefault()}
        onClick={() => send(data)}
      >
        {label}
      </button>
    )
  }

  const modBtn = (label: string, key: keyof TerminalModifierState, state: ModifierState) => {
    const nextState = nextModifierState(state)
    const stateClass = state === 'locked'
      ? 'bg-amber-300 text-zinc-950'
      : state === 'once'
        ? 'bg-[var(--anytty-accent)] text-[var(--anytty-accent-text)]'
        : 'bg-[var(--anytty-surface-raised)] text-[var(--anytty-text)] active:opacity-70'
    return (
      <button
        key={label}
        data-key-id={label}
        data-state={state}
        type="button"
        aria-label={t('terminal.tools.modifierAria', {
          modifier: label,
          state: t(`terminal.tools.modifierState.${state}`),
          nextState: t(`terminal.tools.modifierState.${nextState}`),
        })}
        className={`${cls} flex-col gap-0.5 ${stateClass}`}
        onPointerDown={(e) => e.preventDefault()}
        onClick={() => {
          if (nextState === 'locked') hapticImpact()
          else hapticSelection()
          setModifierState({ ...activeModifierState, [key]: nextState })
        }}
      >
        <span className="leading-none">{label}</span>
        <span className="text-[9px] font-semibold leading-none">{t(`terminal.tools.modifierState.${state}`)}</span>
      </button>
    )
  }

  return (
    <div
      ref={forwardedRef}
      className={`min-w-0 shrink-0 overflow-x-hidden border-t border-[var(--anytty-border-subtle)] bg-[var(--anytty-surface)] text-[var(--anytty-text)] md:hidden ${className || ''}`}
      data-testid="anytty-mobile-keybar"
      style={{ paddingBottom: 'calc(0.375rem + env(safe-area-inset-bottom))' }}
    >
      <div className="anytty-terminal-key-row flex min-w-0 touch-pan-x gap-1 overflow-x-auto px-1.5 pt-1.5">
        {btn('Esc', '\x1b')}
        {btn('/', '/')}
        {btn('|', '|')}
        {btn('-', '-')}
        {btn('Home', '\x1b[H')}
        {btn('↑', '\x1b[A')}
        {btn('End', '\x1b[F')}
        {btn('PgU', '\x1b[5~')}
        <button
          data-key-id="keyboard-visibility"
          type="button"
          aria-label={t(keyboardVisible ? 'terminal.tools.hideKeyboard' : 'terminal.tools.showKeyboard')}
          aria-pressed={keyboardVisible}
          className={`${cls} ${keyboardButtonClass} disabled:opacity-45`}
          disabled={keyboardFocusLocked}
          onPointerDown={(e) => e.preventDefault()}
          onClick={() => {
            hapticSelection()
            if (keyboardVisible) onBlurKeyboard?.()
            else onFocusKeyboard?.()
          }}
        >
          <Keyboard aria-hidden="true" className="h-4 w-4" />
        </button>
        <button
          data-key-id="keyboard-focus-lock"
          type="button"
          aria-label={t(keyboardFocusLocked ? 'terminal.tools.allowKeyboardPopup' : 'terminal.tools.preventKeyboardPopup')}
          aria-pressed={keyboardFocusLocked}
          title={t(keyboardFocusLocked ? 'terminal.tools.allowKeyboardPopup' : 'terminal.tools.preventKeyboardPopup')}
          className={`${buttonBaseClass} w-24 min-w-24 flex-col gap-0.5 px-1 ${keyboardFocusLockButtonClass}`}
          onPointerDown={(e) => e.preventDefault()}
          onClick={() => {
            hapticImpact()
            onToggleKeyboardFocusLock?.()
          }}
        >
          <LockKeyhole aria-hidden="true" className="h-3.5 w-3.5" />
          <span className="max-w-[5.5rem] text-[8px] font-semibold leading-[9px] whitespace-normal">
            {t(keyboardFocusLocked ? 'terminal.tools.allowKeyboardPopup' : 'terminal.tools.preventKeyboardPopup')}
          </span>
        </button>
      </div>
      <div className="anytty-terminal-key-row flex min-w-0 touch-pan-x gap-1 overflow-x-auto px-1.5 pb-1.5 pt-1">
        {btn('⇥', '\t', t('terminal.tools.tabKey'))}
        {modBtn('Ctrl', 'ctrl', activeModifierState.ctrl)}
        {modBtn('Alt', 'alt', activeModifierState.alt)}
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
          onPointerDown={(e) => e.preventDefault()}
          onClick={() => { hapticSelection(); onToggleFn?.() }}
        >
          Fn
        </button>
      </div>
    </div>
  )
})
