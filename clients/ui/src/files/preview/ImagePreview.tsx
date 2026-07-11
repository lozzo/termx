import type { FilePreviewResponse } from '../fileApi'
import { PreviewNotice } from './PreviewNotice'
import { useBinaryPreviewUrl } from './useBinaryPreviewUrl'
import { ZoomableMediaCanvas } from './ZoomableMediaCanvas'

export function ImagePreview({ preview }: { preview: FilePreviewResponse }) {
  const src = useBinaryPreviewUrl(preview.contentBase64, preview.mimeType)
  if (src === undefined) return null
  if (!src) return <PreviewNotice title="Preview Error" message="Image preview data is invalid." />
  return (
    <ZoomableMediaCanvas zoomLabel={preview.name}>
      <img
        alt={preview.name}
        className="block max-h-[calc(100dvh-8rem)] max-w-[calc(100vw-1rem)] select-none rounded-md shadow-sm"
        draggable={false}
        src={src}
      />
    </ZoomableMediaCanvas>
  )
}
