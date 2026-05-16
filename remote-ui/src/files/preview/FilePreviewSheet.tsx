import { useEffect } from 'react'
import { createPortal } from 'react-dom'
import { RefreshCw, X } from 'lucide-react'
import type { FilePreviewResponse, FilePreviewStreamOptions, FilePreviewStreamResult } from '../fileApi'
import { basename, formatBytes, isMarkdownFile } from '../fileUtils'
import { BinaryVideoPreview, StreamedVideoPreview } from './VideoPreview'
import { ImagePreview } from './ImagePreview'
import { MarkdownPreview, TextPreview } from './TextPreview'
import { ModelPreview, canPreviewModelFile } from './ModelPreview'
import { PreviewNotice } from './PreviewNotice'

interface FilePreviewSheetProps {
  path: string
  preview: FilePreviewResponse | null
  loading: boolean
  error: string | null
  streamPreview(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
  onClose(): void
}

export function FilePreviewSheet({ path, preview, loading, error, streamPreview, onClose }: FilePreviewSheetProps) {
  const title = preview?.name ?? basename(path)
  const subtitle = preview ? `${formatBytes(preview.size)} · ${preview.mimeType}` : path
  const isMediaPreview = preview?.category === 'image' || preview?.category === 'video' || preview?.category === 'model'

  useEffect(() => {
    if (typeof document === 'undefined') return undefined
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = previousOverflow
    }
  }, [])

  const dialog = (
    <div
      className="fixed inset-0 z-[80] flex h-[100dvh] flex-col bg-white"
      data-testid="termx-file-preview"
      role="dialog"
      aria-modal="true"
      aria-labelledby="termx-file-preview-title"
    >
      <header className="flex shrink-0 items-center gap-3 border-b border-zinc-200/70 bg-white px-4 pb-2 pt-[calc(env(safe-area-inset-top)+0.5rem)] md:h-14 md:pb-0 md:pt-0">
        <div className="min-w-0 flex-1">
          <h2 id="termx-file-preview-title" className="truncate text-[17px] font-bold tracking-tight text-zinc-950">{title}</h2>
          <p className="mt-0.5 truncate text-[12px] font-medium text-zinc-500">{subtitle}</p>
        </div>
        <button
          type="button"
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-zinc-500 transition-colors active:scale-95 hover:bg-zinc-50 active:bg-zinc-100"
          aria-label="Close preview"
          onClick={onClose}
        >
          <X className="h-5 w-5" />
        </button>
      </header>
      <div className={`min-h-0 flex-1 pb-[env(safe-area-inset-bottom)] ${isMediaPreview ? 'overflow-hidden bg-black' : 'overflow-auto bg-zinc-50'}`}>
        {loading ? (
          <div className="flex h-56 flex-col items-center justify-center gap-3 text-[14px] font-medium text-zinc-500">
            <RefreshCw className="h-6 w-6 animate-spin text-zinc-400" />
            Loading preview...
          </div>
        ) : error ? (
          <PreviewNotice title="Preview Error" message={error} />
        ) : preview ? (
          <PreviewContent preview={preview} streamPreview={streamPreview} />
        ) : null}
      </div>
    </div>
  )

  if (typeof document === 'undefined') return dialog
  return createPortal(dialog, document.body)
}

function PreviewContent({
  preview,
  streamPreview,
}: {
  preview: FilePreviewResponse
  streamPreview(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
}) {
  if (preview.category === 'image' && preview.contentBase64) {
    return <ImagePreview preview={preview} />
  }
  if (preview.category === 'video' && preview.contentBase64) {
    return <BinaryVideoPreview preview={preview} />
  }
  if (preview.category === 'video') {
    return <StreamedVideoPreview preview={preview} streamPreview={streamPreview} />
  }
  if (preview.category === 'model' && canPreviewModelFile(preview.name, preview.mimeType)) {
    return <ModelPreview preview={preview} streamPreview={streamPreview} />
  }
  if (preview.category === 'text' && preview.content !== undefined) {
    if (isMarkdownFile(preview.name, preview.mimeType)) {
      return <MarkdownPreview text={preview.content} />
    }
    return <TextPreview text={preview.content} name={preview.name} mimeType={preview.mimeType} />
  }
  const limit = preview.previewLimit && preview.previewLimit > 0 ? formatBytes(preview.previewLimit) : ''
  const message = preview.category === 'unsupported'
    ? 'This file type is not available for inline preview.'
    : `This file is too large to preview${limit ? ` within the ${limit} limit` : ''}.`
  return <PreviewNotice title="No Preview" message={message} />
}
