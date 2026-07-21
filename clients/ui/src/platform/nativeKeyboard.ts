export const MUXVIA_NATIVE_KEYBOARD_EVENT = 'muxvia:native-keyboard'

export interface MuxviaNativeKeyboardEventDetail {
  visible: boolean
  keyboardHeight?: number | undefined
}

export type MuxviaNativeKeyboardHandler = (detail: MuxviaNativeKeyboardEventDetail) => void

export function dispatchNativeKeyboardEvent(detail: MuxviaNativeKeyboardEventDetail): void {
  if (typeof window === 'undefined' || typeof window.dispatchEvent !== 'function') return
  window.dispatchEvent(new CustomEvent<MuxviaNativeKeyboardEventDetail>(MUXVIA_NATIVE_KEYBOARD_EVENT, {
    detail: normalizeNativeKeyboardDetail(detail),
  }))
}

export function addNativeKeyboardListener(handler: MuxviaNativeKeyboardHandler): () => void {
  if (typeof window === 'undefined' || typeof window.addEventListener !== 'function') return () => {}
  const listener = (event: Event) => {
    const detail = nativeKeyboardDetailFromEvent(event)
    if (!detail) return
    handler(detail)
  }
  window.addEventListener(MUXVIA_NATIVE_KEYBOARD_EVENT, listener)
  return () => window.removeEventListener(MUXVIA_NATIVE_KEYBOARD_EVENT, listener)
}

function nativeKeyboardDetailFromEvent(event: Event): MuxviaNativeKeyboardEventDetail | null {
  const detail = (event as Event & { detail?: unknown }).detail
  if (!detail || typeof detail !== 'object') return null
  const record = detail as Record<string, unknown>
  if (typeof record.visible !== 'boolean') return null
  return normalizeNativeKeyboardDetail({
    visible: record.visible,
    keyboardHeight: typeof record.keyboardHeight === 'number' ? record.keyboardHeight : undefined,
  })
}

function normalizeNativeKeyboardDetail(detail: MuxviaNativeKeyboardEventDetail): MuxviaNativeKeyboardEventDetail {
  const keyboardHeight = typeof detail.keyboardHeight === 'number' && Number.isFinite(detail.keyboardHeight)
    ? Math.max(0, detail.keyboardHeight)
    : undefined
  return {
    visible: detail.visible,
    ...(keyboardHeight !== undefined ? { keyboardHeight } : {}),
  }
}
