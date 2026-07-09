export function binaryPreviewUrl(contentBase64: string, mimeType: string): string | null {
  const trimmed = contentBase64.trim()
  if (trimmed.startsWith('data:')) return trimmed
  const normalizedMimeType = mimeType.trim() || 'application/octet-stream'
  const compact = trimmed.replace(/\s+/g, '')
  if (/^[A-Za-z0-9+/=_-]+$/.test(compact)) {
    return `data:${normalizedMimeType};base64,${normalizeBase64ForDataUrl(compact)}`
  }
  try {
    const binary = atob(trimmed)
    const bytes = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i)
    const blob = new Blob([bytes], { type: normalizedMimeType })
    return URL.createObjectURL(blob)
  } catch {
    return null
  }
}

function normalizeBase64ForDataUrl(contentBase64: string): string {
  const normalized = contentBase64.replace(/-/g, '+').replace(/_/g, '/')
  const padding = normalized.length % 4
  return padding === 0 ? normalized : `${normalized}${'='.repeat(4 - padding)}`
}
