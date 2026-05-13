import { useEffect, useState } from 'react'
import { binaryPreviewUrl } from './binaryPreview'

export function useBinaryPreviewUrl(contentBase64: string | undefined, mimeType: string): string | null | undefined {
  const [url, setUrl] = useState<string | null | undefined>(undefined)

  useEffect(() => {
    if (!contentBase64) {
      setUrl(null)
      return undefined
    }
    const src = binaryPreviewUrl(contentBase64, mimeType)
    setUrl(src)
    return () => {
      if (src?.startsWith('blob:')) URL.revokeObjectURL(src)
    }
  }, [contentBase64, mimeType])

  return url
}
