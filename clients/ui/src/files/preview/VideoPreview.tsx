import { useEffect, useRef, useState } from 'react'
import { Maximize2 } from 'lucide-react'
import { createFilePreviewRangeUrl, type FilePreviewRangeUrl } from '../filePreviewRangeUrl'
import type { FilePreviewResponse, FilePreviewStreamOptions, FilePreviewStreamResult } from '../fileApi'
import { hapticSelection } from '../../platform/haptics'
import { MediaPreviewShell } from './MediaPreviewShell'
import { PreviewNotice } from './PreviewNotice'
import { useBinaryPreviewUrl } from './useBinaryPreviewUrl'
import { useTranslation } from 'react-i18next'
import '../../i18n'

const maxFallbackVideoBytes = 32 * 1024 * 1024

export function BinaryVideoPreview({ preview }: { preview: FilePreviewResponse }) {
  const { t } = useTranslation()
  const src = useBinaryPreviewUrl(preview.contentBase64, preview.mimeType)
  if (src === undefined) return null
  if (!src) return <PreviewNotice title={t('files.preview.error')} message={t('files.preview.invalidVideo')} />
  return <VideoPreviewPlayer preview={preview} src={src} />
}

export function StreamedVideoPreview({
  preview,
  streamPreview,
}: {
  preview: FilePreviewResponse
  streamPreview(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
}) {
  const { t } = useTranslation()
  const [rangeUrl, setRangeUrl] = useState<string | null | undefined>(undefined)
  const rangeSourceRef = useRef<FilePreviewRangeUrl | null>(null)

  useEffect(() => {
    let cancelled = false
    let objectUrl: string | null = null
    const controller = new AbortController()
    rangeSourceRef.current?.revoke()
    rangeSourceRef.current = null
    setRangeUrl(undefined)
    void createFilePreviewRangeUrl({
      path: preview.path,
      mimeType: preview.mimeType,
      size: preview.size,
      streamPreview,
    }).then(async (result) => {
      if (cancelled) {
        result?.revoke()
        return
      }
      if (result) {
        rangeSourceRef.current = result
        setRangeUrl(result.url)
        return
      }
      if (preview.size > maxFallbackVideoBytes) {
        setRangeUrl(null)
        return
      }
      const streamed = await streamPreview(preview.path, preview.mimeType, { signal: controller.signal })
      if (cancelled) return
      objectUrl = URL.createObjectURL(streamed.blob)
      setRangeUrl(objectUrl)
    }).catch(() => {
      if (!cancelled && !controller.signal.aborted) setRangeUrl(null)
    })
    return () => {
      cancelled = true
      controller.abort()
      rangeSourceRef.current?.revoke()
      rangeSourceRef.current = null
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [preview.mimeType, preview.path, preview.size, streamPreview])

  if (rangeUrl === undefined) {
    return (
      <VideoPreviewPlayer
        preview={preview}
      />
    )
  }

  if (rangeUrl) {
    return (
      <VideoPreviewPlayer
        preview={preview}
        src={rangeUrl}
        onLoadedMetadata={(duration) => {
          rangeSourceRef.current?.configure({ duration })
        }}
      />
    )
  }

  return <PreviewNotice title={t('files.preview.unavailable')} message={t('files.preview.videoUnavailable')} />
}

interface FullscreenVideoElement extends HTMLVideoElement {
  webkitEnterFullscreen?: () => void
}

function VideoPreviewPlayer({
  preview,
  src,
  onLoadedMetadata,
}: {
  preview: FilePreviewResponse
  src?: string | undefined
  onLoadedMetadata?: ((duration: number) => void) | undefined
}) {
  const { t } = useTranslation()
  const videoRef = useRef<HTMLVideoElement>(null)
  const [fullscreenSupported, setFullscreenSupported] = useState(false)

  useEffect(() => {
    const video = videoRef.current
    setFullscreenSupported(!!video && (canRequestFullscreen(video) || canEnterFullscreen(video)))
  }, [src])

  const requestFullscreen = () => {
    const video = videoRef.current
    if (!video) return
    hapticSelection()
    if (canRequestFullscreen(video)) {
      void video.requestFullscreen().catch(() => {})
      return
    }
    if (canEnterFullscreen(video)) {
      ;(video as FullscreenVideoElement).webkitEnterFullscreen?.()
    }
  }

  return (
    <MediaPreviewShell
      toolbar={fullscreenSupported ? (
        <div className="sticky top-0 z-20 flex min-h-11 items-center justify-end gap-2 border-b border-white/10 bg-black/85 px-4 py-2 text-zinc-200 backdrop-blur">
          <button
            type="button"
            className="flex h-8 w-8 shrink-0 items-center justify-center border border-white/10 text-zinc-200 transition-colors hover:bg-white/5 active:bg-white/10"
            aria-label={t('files.preview.fullscreenNamed', { name: preview.name })}
            title={t('files.preview.fullscreen')}
            onClick={requestFullscreen}
          >
            <Maximize2 className="h-4 w-4" />
          </button>
        </div>
      ) : undefined}
    >
      <div className="relative flex h-full min-h-[calc(100dvh-7.5rem)] items-center justify-center p-2">
        {src ? (
          <video
            ref={videoRef}
            className="h-full max-h-[calc(100dvh-8rem)] w-full object-contain"
            controls
            onLoadedMetadata={(event) => {
              const duration = event.currentTarget.duration
              if (Number.isFinite(duration) && duration > 0) onLoadedMetadata?.(duration)
            }}
            playsInline
            preload="metadata"
            src={src}
          />
        ) : (
          <div className="flex flex-col items-center justify-center gap-3 text-zinc-300" data-testid="anytty-video-stream-status">
            <span className="anytty-square-spinner h-7 w-7 text-zinc-500" aria-hidden="true" />
          </div>
        )}
      </div>
    </MediaPreviewShell>
  )
}

function canRequestFullscreen(element: HTMLElement): element is HTMLElement & { requestFullscreen: () => Promise<void> } {
  return typeof element.requestFullscreen === 'function'
}

function canEnterFullscreen(element: HTMLVideoElement): element is FullscreenVideoElement & { webkitEnterFullscreen: () => void } {
  return typeof (element as FullscreenVideoElement).webkitEnterFullscreen === 'function'
}
