export type ModifierState = 'off' | 'once' | 'locked'

export interface TerminalModifierState {
  ctrl: ModifierState
  alt: ModifierState
}

export interface TerminalModifierResult extends TerminalModifierState {
  data: string
}

export function applyTerminalModifiers(data: string, state: TerminalModifierState): TerminalModifierResult {
  let nextData = data
  let nextCtrl = state.ctrl
  let nextAlt = state.alt

  if (state.ctrl !== 'off') {
    nextData = ctrlData(nextData)
    if (state.ctrl === 'once') nextCtrl = 'off'
  }

  if (state.alt !== 'off') {
    nextData = `\x1b${nextData}`
    if (state.alt === 'once') nextAlt = 'off'
  }

  return {
    data: nextData,
    ctrl: nextCtrl,
    alt: nextAlt,
  }
}

export function nextModifierState(current: ModifierState): ModifierState {
  if (current === 'off') return 'once'
  if (current === 'once') return 'locked'
  return 'off'
}

function ctrlData(data: string): string {
  if (data.length !== 1) return data
  const code = data.charCodeAt(0)
  if (code >= 64 && code <= 95) return String.fromCharCode(code - 64)
  if (code >= 97 && code <= 122) return String.fromCharCode(code - 96)
  return data
}
