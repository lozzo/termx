export const MUXVIA_NATIVE_KEYBOARD_EVENT = 'termx:native-keyboard'

export interface TermxNativeKeyboardEventDetail {
  visible: boolean
  keyboardHeight?: number | undefined
}

export type TermxNativeKeyboardHandler = (detail: TermxNativeKeyboardEventDetail) => void

export function dispatchNativeKeyboardEvent(detail: TermxNativeKeyboardEventDetail): void {
  if (typeof window === 'undefined' || typeof window.dispatchEvent !== 'function') return
  window.dispatchEvent(new CustomEvent<TermxNativeKeyboardEventDetail>(MUXVIA_NATIVE_KEYBOARD_EVENT, {
    detail: normalizeNativeKeyboardDetail(detail),
  }))
}

export function addNativeKeyboardListener(handler: TermxNativeKeyboardHandler): () => void {
  if (typeof window === 'undefined' || typeof window.addEventListener !== 'function') return () => {}
  const listener = (event: Event) => {
    const detail = nativeKeyboardDetailFromEvent(event)
    if (!detail) return
    handler(detail)
  }
  window.addEventListener(MUXVIA_NATIVE_KEYBOARD_EVENT, listener)
  return () => window.removeEventListener(MUXVIA_NATIVE_KEYBOARD_EVENT, listener)
}

function nativeKeyboardDetailFromEvent(event: Event): TermxNativeKeyboardEventDetail | null {
  const detail = (event as Event & { detail?: unknown }).detail
  if (!detail || typeof detail !== 'object') return null
  const record = detail as Record<string, unknown>
  if (typeof record.visible !== 'boolean') return null
  return normalizeNativeKeyboardDetail({
    visible: record.visible,
    keyboardHeight: typeof record.keyboardHeight === 'number' ? record.keyboardHeight : undefined,
  })
}

function normalizeNativeKeyboardDetail(detail: TermxNativeKeyboardEventDetail): TermxNativeKeyboardEventDetail {
  const keyboardHeight = typeof detail.keyboardHeight === 'number' && Number.isFinite(detail.keyboardHeight)
    ? Math.max(0, detail.keyboardHeight)
    : undefined
  return {
    visible: detail.visible,
    ...(keyboardHeight !== undefined ? { keyboardHeight } : {}),
  }
}
