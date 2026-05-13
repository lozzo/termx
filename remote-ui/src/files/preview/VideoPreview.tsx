import { useEffect, useRef, useState } from 'react'
import { Maximize2, RefreshCw } from 'lucide-react'
import { createFilePreviewRangeUrl, type FilePreviewRangeUrl } from '../filePreviewRangeUrl'
import type { FilePreviewResponse, FilePreviewStreamOptions, FilePreviewStreamResult } from '../fileApi'
import { hapticSelection } from '../../platform/haptics'
import { MediaPreviewShell } from './MediaPreviewShell'
import { PreviewNotice } from './PreviewNotice'
import { useBinaryPreviewUrl } from './useBinaryPreviewUrl'

export function BinaryVideoPreview({ preview }: { preview: FilePreviewResponse }) {
  const src = useBinaryPreviewUrl(preview.contentBase64, preview.mimeType)
  if (src === undefined) return null
  if (!src) return <PreviewNotice title="Preview Error" message="Video preview data is invalid." />
  return <VideoPreviewPlayer preview={preview} src={src} />
}

export function StreamedVideoPreview({
  preview,
  streamPreview,
}: {
  preview: FilePreviewResponse
  streamPreview(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
}) {
  const [rangeUrl, setRangeUrl] = useState<string | null | undefined>(undefined)
  const rangeSourceRef = useRef<FilePreviewRangeUrl | null>(null)

  useEffect(() => {
    let cancelled = false
    rangeSourceRef.current?.revoke()
    rangeSourceRef.current = null
    setRangeUrl(undefined)
    void createFilePreviewRangeUrl({
      path: preview.path,
      mimeType: preview.mimeType,
      size: preview.size,
      streamPreview,
    }).then((result) => {
      if (cancelled) {
        result?.revoke()
        return
      }
      rangeSourceRef.current = result
      setRangeUrl(result?.url ?? null)
    })
    return () => {
      cancelled = true
      rangeSourceRef.current?.revoke()
      rangeSourceRef.current = null
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

  return <PreviewNotice title="Preview Unavailable" message="This browser context cannot provide seekable video preview without full-file buffering." />
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
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-zinc-200 transition-colors active:scale-95 active:bg-white/10"
            aria-label={`Fullscreen ${preview.name}`}
            title="Fullscreen"
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
          <div className="flex flex-col items-center justify-center gap-3 text-zinc-300" data-testid="termx-video-stream-status">
            <RefreshCw className="h-7 w-7 animate-spin text-zinc-500" />
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
