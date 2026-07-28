import { useEffect, useState } from 'react'
import type { FilePreviewResponse, FilePreviewStreamOptions, FilePreviewStreamResult } from '../fileApi'
import { formatBytes } from '../fileUtils'
import { MediaPreviewShell } from './MediaPreviewShell'
import { PreviewNotice } from './PreviewNotice'
import { useBinaryPreviewUrl } from './useBinaryPreviewUrl'
import { ZoomableMediaCanvas } from './ZoomableMediaCanvas'
import { useTranslation } from 'react-i18next'
import '../../i18n'

export function ImagePreview({ preview }: { preview: FilePreviewResponse }) {
  const { t } = useTranslation()
  const src = useBinaryPreviewUrl(preview.contentBase64, preview.mimeType)
  if (src === undefined) return null
  if (!src) return <PreviewNotice title={t('files.preview.error')} message={t('files.preview.invalidImage')} />
  return (
    <ZoomableMediaCanvas zoomLabel={preview.name}>
      <img
        alt={preview.name}
        className="block max-h-[calc(100dvh-8rem)] max-w-[calc(100vw-1rem)] select-none"
        draggable={false}
        src={src}
      />
    </ZoomableMediaCanvas>
  )
}

const maxStreamedImageBytes = 32 * 1024 * 1024

export function StreamedImagePreview({
  preview,
  streamPreview,
}: {
  preview: FilePreviewResponse
  streamPreview(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
}) {
  const { t } = useTranslation()
  const [src, setSrc] = useState<string | null | undefined>(undefined)

  useEffect(() => {
    if (preview.size > maxStreamedImageBytes) {
      setSrc(null)
      return
    }
    const controller = new AbortController()
    let active = true
    let objectUrl: string | null = null
    setSrc(undefined)
    void streamPreview(preview.path, preview.mimeType, { signal: controller.signal }).then((streamed) => {
      if (!active) return
      objectUrl = URL.createObjectURL(streamed.blob)
      setSrc(objectUrl)
    }).catch(() => {
      if (active && !controller.signal.aborted) setSrc(null)
    })
    return () => {
      active = false
      controller.abort()
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [preview.mimeType, preview.path, preview.size, streamPreview])

  if (src === undefined) {
    return (
      <MediaPreviewShell>
        <div className="flex h-full min-h-[calc(100dvh-7.5rem)] items-center justify-center">
          <span className="anytty-square-spinner h-7 w-7 text-zinc-500" aria-hidden="true" />
        </div>
      </MediaPreviewShell>
    )
  }
  if (!src) {
    const message = preview.size > maxStreamedImageBytes
      ? t('files.preview.tooLargeLimit', { limit: formatBytes(maxStreamedImageBytes) })
      : t('files.preview.invalidImage')
    return <PreviewNotice title={t('files.preview.unavailable')} message={message} />
  }
  return (
    <ZoomableMediaCanvas zoomLabel={preview.name}>
      <img
        alt={preview.name}
        className="block max-h-[calc(100dvh-8rem)] max-w-[calc(100vw-1rem)] select-none"
        draggable={false}
        src={src}
      />
    </ZoomableMediaCanvas>
  )
}
