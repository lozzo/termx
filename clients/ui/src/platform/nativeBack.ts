export const MUXVIA_NATIVE_BACK_EVENT = 'termx:native-back'

export type TermxNativeBackHandler = () => boolean

interface NativeBackHandlerEntry {
  id: number
  priority: number
  handler: TermxNativeBackHandler
}

let nextNativeBackHandlerId = 1
const nativeBackHandlers: NativeBackHandlerEntry[] = []

export function addNativeBackHandler(handler: TermxNativeBackHandler, priority = 0): () => void {
  const entry = {
    id: nextNativeBackHandlerId++,
    priority,
    handler,
  }
  nativeBackHandlers.push(entry)
  return () => {
    const index = nativeBackHandlers.indexOf(entry)
    if (index >= 0) nativeBackHandlers.splice(index, 1)
  }
}

export function dispatchNativeBack(): boolean {
  const handlers = nativeBackHandlers
    .slice()
    .sort((left, right) => right.priority - left.priority || right.id - left.id)

  for (const entry of handlers) {
    if (!nativeBackHandlers.includes(entry)) continue
    try {
      if (entry.handler()) return true
    } catch (err) {
      globalThis.setTimeout(() => {
        throw err
      }, 0)
      return true
    }
  }

  if (typeof window === 'undefined' || typeof window.dispatchEvent !== 'function') return false
  const event = new Event(MUXVIA_NATIVE_BACK_EVENT, { cancelable: true })
  window.dispatchEvent(event)
  return event.defaultPrevented
}
