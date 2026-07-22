import type { FilePreviewResponse } from '../fileApi'
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
