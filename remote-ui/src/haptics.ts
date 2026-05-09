let lastHapticAt = 0

export function haptic(pattern: number | number[] = 10): void {
  if (typeof navigator === 'undefined') return
  const now = Date.now()
  if (now - lastHapticAt < 35) return
  lastHapticAt = now
  try {
    navigator.vibrate?.(pattern)
  } catch {}
}
