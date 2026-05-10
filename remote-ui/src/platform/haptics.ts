let lastHapticAt = 0
let impactHandler: HapticImpactHandler | null = null

export type HapticPattern = number | number[]
export type HapticImpactHandler = (pattern: HapticPattern) => void | Promise<void>

export function setHapticImpactHandler(handler: HapticImpactHandler | null): () => void {
  impactHandler = handler
  return () => {
    if (impactHandler === handler) impactHandler = null
  }
}

export function haptic(pattern: HapticPattern = 10): void {
  const now = Date.now()
  if (now - lastHapticAt < 35) return
  lastHapticAt = now

  if (impactHandler) {
    try {
      void Promise.resolve(impactHandler(pattern)).catch(() => vibrate(pattern))
      return
    } catch {
      vibrate(pattern)
      return
    }
  }

  vibrate(pattern)
}

function vibrate(pattern: HapticPattern): void {
  if (typeof navigator === 'undefined') return
  try {
    navigator.vibrate?.(pattern)
  } catch {}
}
