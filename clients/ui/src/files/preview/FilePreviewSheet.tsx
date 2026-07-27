import { useEffect } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { hapticSelection } from '../../platform/haptics'
import type { FilePreviewResponse, FilePreviewStreamOptions, FilePreviewStreamResult } from '../fileApi'
import { basename, formatBytes, isMarkdownFile } from '../fileUtils'
import { BinaryVideoPreview, StreamedVideoPreview } from './VideoPreview'
import { ImagePreview } from './ImagePreview'
import { MarkdownPreview, TextPreview } from './TextPreview'
import { ModelPreview, canPreviewModelFile } from './ModelPreview'
import { PreviewNotice } from './PreviewNotice'
import { useTranslation } from 'react-i18next'
import '../../i18n'

interface FilePreviewSheetProps {
  path: string
  preview: FilePreviewResponse | null
  loading: boolean
  error: string | null
  streamPreview(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
  onClose(): void
}

export function FilePreviewSheet({ path, preview, loading, error, streamPreview, onClose }: FilePreviewSheetProps) {
  const { t } = useTranslation()
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
      data-testid="anytty-file-preview"
      role="dialog"
      aria-modal="true"
      aria-labelledby="anytty-file-preview-title"
    >
      <header className="anytty-app-header flex shrink-0 items-center gap-3 border-b px-4 pb-2 pt-[calc(env(safe-area-inset-top)+0.5rem)] md:h-14 md:pb-0 md:pt-0">
        <div className="min-w-0 flex-1">
          <h2 id="anytty-file-preview-title" className="truncate text-[17px] font-bold tracking-tight text-zinc-950">{title}</h2>
          <p className="mt-0.5 truncate text-[12px] font-medium text-zinc-500">{subtitle}</p>
        </div>
        <button
          type="button"
          className="anytty-app-icon-button shrink-0 border-transparent bg-transparent"
          aria-label={t('files.preview.close')}
          onClick={() => { hapticSelection(); onClose() }}
        >
          <X className="h-5 w-5" />
        </button>
      </header>
      <div className={`min-h-0 flex-1 pb-[env(safe-area-inset-bottom)] ${isMediaPreview ? 'overflow-hidden bg-black' : 'overflow-auto bg-zinc-50'}`}>
        {loading ? (
          <div className="flex h-56 flex-col items-center justify-center gap-3 text-[14px] font-medium text-zinc-500">
            <span className="anytty-square-spinner h-6 w-6 text-zinc-500" aria-hidden="true" />
            {t('files.preview.loading')}
          </div>
        ) : error ? (
          <PreviewNotice title={t('files.preview.error')} message={error} />
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
  const { t } = useTranslation()
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
    ? t('files.preview.unsupported')
    : t(limit ? 'files.preview.tooLargeLimit' : 'files.preview.tooLarge', { limit })
  return <PreviewNotice title={t('files.preview.none')} message={message} />
}
