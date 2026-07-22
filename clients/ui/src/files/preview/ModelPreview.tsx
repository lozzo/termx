import { useEffect, useRef, useState } from 'react'
import type { FilePreviewResponse, FilePreviewStreamOptions, FilePreviewStreamResult } from '../fileApi'
import { clamp } from '../fileUtils'
import { isModelPreviewFile, modelPreviewMimeType } from '../modelFileTypes'
import { MediaPreviewShell } from './MediaPreviewShell'
import { ModelScene } from './ModelScene'
import { disposeModelObject, loadModelPreviewObject, normalizeModelObject } from './modelPreviewLoaders'
import type { ModelObject3D, ThreeModule } from './modelPreviewTypes'
import { PreviewNotice } from './PreviewNotice'
import { useTranslation } from 'react-i18next'
import '../../i18n'

type ModelPreviewState =
  | { status: 'loading'; receivedSize: number; totalSize: number }
  | { status: 'ready'; object: ModelObject3D; three: ThreeModule; label: string }
  | { status: 'error'; message: string }

export function canPreviewModelFile(name: string, mimeType: string): boolean {
  return isModelPreviewFile(name, mimeType)
}

export function ModelPreview({
  preview,
  streamPreview,
}: {
  preview: FilePreviewResponse
  streamPreview(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
}) {
  const { t } = useTranslation()
  const objectRef = useRef<ModelObject3D | null>(null)
  const [state, setState] = useState<ModelPreviewState>({
    status: 'loading',
    receivedSize: 0,
    totalSize: preview.size,
  })

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    disposeModelObject(objectRef.current)
    objectRef.current = null
    setState({ status: 'loading', receivedSize: 0, totalSize: preview.size })

    void streamPreview(preview.path, modelPreviewMimeType(preview.name, preview.mimeType), {
      signal: controller.signal,
      onProgress: ({ receivedSize, totalSize }) => {
        if (!active) return
        setState({ status: 'loading', receivedSize, totalSize })
      },
    }).then(async (result) => {
      const [buffer, text, three] = await Promise.all([
        blobToArrayBuffer(result.blob),
        blobToText(result.blob),
        import('three'),
      ])
      if (!active) return
      const loaded = await loadModelPreviewObject(three, preview.name, buffer, text)
      if (!active) {
        disposeModelObject(loaded.object)
        return
      }
      const normalizedObject = normalizeModelObject(three, loaded.object)
      disposeModelObject(objectRef.current)
      objectRef.current = normalizedObject
      setState({ status: 'ready', object: normalizedObject, three, label: loaded.label })
    }).catch((err) => {
      if (!active || controller.signal.aborted) return
      setState({ status: 'error', message: err instanceof Error ? err.message : String(err) })
    })

    return () => {
      active = false
      controller.abort()
      disposeModelObject(objectRef.current)
      objectRef.current = null
    }
  }, [preview.mimeType, preview.name, preview.path, preview.size, streamPreview])

  if (state.status === 'error') {
    return <PreviewNotice title={t('files.preview.error')} message={state.message || t('files.preview.modelFailed')} />
  }

  if (state.status === 'loading') {
    const progress = state.totalSize > 0 ? Math.round((state.receivedSize / state.totalSize) * 100) : 0
    return (
      <MediaPreviewShell>
        <div className="flex h-full min-h-[calc(100dvh-7.5rem)] flex-col items-center justify-center gap-3 bg-zinc-950 px-6 text-center text-zinc-200">
          <span className="muxvia-square-spinner h-7 w-7 text-zinc-500" aria-hidden="true" />
          <div className="text-[13px] font-semibold tabular-nums text-zinc-300" data-testid="muxvia-model-loading">
            {progress > 0 ? `${progress}%` : t('files.preview.loadingModel')}
          </div>
          <div className="h-1.5 w-full max-w-xs overflow-hidden bg-white/10">
            <div
              className="h-full bg-sky-400 transition-[width]"
              style={{ width: `${clamp(progress, 0, 100)}%` }}
            />
          </div>
        </div>
      </MediaPreviewShell>
    )
  }

  return <ModelScene object={state.object} name={preview.name} label={state.label} three={state.three} />
}

function blobToArrayBuffer(blob: Blob): Promise<ArrayBuffer> {
  const directReader = (blob as Blob & { arrayBuffer?: () => Promise<ArrayBuffer> }).arrayBuffer
  if (typeof directReader === 'function') return directReader.call(blob)
  if (typeof FileReader === 'undefined') {
    return Promise.reject(new Error('This browser context cannot read 3D model preview data.'))
  }
  return new Promise<ArrayBuffer>((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error ?? new Error('3D model preview data could not be read.'))
    reader.onload = () => {
      if (reader.result instanceof ArrayBuffer) {
        resolve(reader.result)
        return
      }
      reject(new Error('3D model preview data was not binary.'))
    }
    reader.readAsArrayBuffer(blob)
  })
}

function blobToText(blob: Blob): Promise<string> {
  const directReader = (blob as Blob & { text?: () => Promise<string> }).text
  if (typeof directReader === 'function') return directReader.call(blob)
  return blobToArrayBuffer(blob).then((buffer) => new TextDecoder().decode(buffer))
}
