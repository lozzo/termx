import type { FilePreviewStreamOptions, FilePreviewStreamResult } from './fileApi'

export interface FilePreviewRangeUrlRequest {
  path: string
  mimeType: string
  size: number
  streamPreview(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
}

interface ActivePreviewRangeSource {
  path: string
  mimeType: string
  size: number
  streamPreview(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
}

const previewPathPrefix = '/__muxvia_file_preview__/'
const swPath = '/muxvia-file-preview-sw.js'
const activeSources = new Map<string, ActivePreviewRangeSource>()
let registrationPromise: Promise<ServiceWorkerRegistration | null> | null = null
let messageListenerAttached = false

export interface FilePreviewRangeUrl {
  url: string
  configure(metadata: { duration?: number | undefined }): void
  revoke(): void
}

export async function createFilePreviewRangeUrl(request: FilePreviewRangeUrlRequest): Promise<FilePreviewRangeUrl | null> {
  if (!canUseServiceWorker()) return null
  const registration = await ensurePreviewServiceWorker()
  if (!registration) return null
  const token = createPreviewToken()
  activeSources.set(token, {
    path: request.path,
    mimeType: request.mimeType,
    size: request.size,
    streamPreview: request.streamPreview,
  })
  const query = new URLSearchParams({
    size: String(Math.max(0, Math.floor(request.size))),
    mime: request.mimeType.trim() || 'application/octet-stream',
  })
  return {
    url: `${previewPathPrefix}${encodeURIComponent(token)}?${query.toString()}`,
    configure(metadata: { duration?: number | undefined }) {
      postPreviewWorkerControl({
        type: 'muxvia-preview-configure',
        token,
        size: Math.max(0, Math.floor(request.size)),
        mimeType: request.mimeType.trim() || 'application/octet-stream',
        duration: metadata.duration,
      })
    },
    revoke() {
      activeSources.delete(token)
      postPreviewWorkerControl({
        type: 'muxvia-preview-release',
        token,
      })
    },
  }
}

function canUseServiceWorker(): boolean {
  return typeof navigator !== 'undefined' && !!navigator.serviceWorker && window.isSecureContext
}

async function ensurePreviewServiceWorker(): Promise<ServiceWorkerRegistration | null> {
  if (!registrationPromise) {
    registrationPromise = navigator.serviceWorker.register(swPath, { scope: '/' })
      .then(async (registration) => {
        attachPreviewMessageListener()
        await navigator.serviceWorker.ready
        await waitForServiceWorkerController()
        return registration
      })
      .catch(() => null)
  }
  return await registrationPromise
}

function attachPreviewMessageListener(): void {
  if (messageListenerAttached) return
  messageListenerAttached = true
  navigator.serviceWorker.addEventListener('message', (event) => {
    void handlePreviewRangeMessage(event)
  })
}

async function handlePreviewRangeMessage(event: MessageEvent): Promise<void> {
  const data = event.data
  if (!isRangeRequest(data)) return
  const source = activeSources.get(data.token)
  if (!source) {
    postRangeError(event, data.requestId, 'file preview source expired')
    return
  }
  const offset = normalizeRangeValue(data.offset)
  const length = normalizeRangeValue(data.length)
  if (source.size > 0 && length <= 0) {
    postRangeError(event, data.requestId, 'file preview range length is required')
    return
  }
  try {
    const result = await source.streamPreview(source.path, source.mimeType, {
      offset,
      length,
    })
    postRangeResponse(event, data.requestId, result.blob, {
      offset: result.offset,
      length: result.receivedSize,
      totalSize: result.totalSize || source.size,
      mimeType: source.mimeType,
    })
  } catch (err) {
    postRangeError(event, data.requestId, err instanceof Error ? err.message : String(err))
  }
}

function postRangeResponse(
  event: MessageEvent,
  requestId: string,
  blob: Blob,
  metadata: { offset: number; length: number; totalSize: number; mimeType: string },
): void {
  postToPreviewWorker(event, {
    type: 'muxvia-preview-response',
    requestId,
    blob,
    offset: metadata.offset,
    length: metadata.length,
    totalSize: metadata.totalSize,
    mimeType: metadata.mimeType,
  })
}

function postRangeError(event: MessageEvent, requestId: string, message: string): void {
  postToPreviewWorker(event, {
    type: 'muxvia-preview-error',
    requestId,
    message,
  })
}

function postToPreviewWorker(event: MessageEvent, message: unknown): void {
  const source = event.source as ServiceWorker | null
  if (source && typeof source.postMessage === 'function') {
    source.postMessage(message)
    return
  }
  navigator.serviceWorker.controller?.postMessage(message)
}

function postPreviewWorkerControl(message: unknown): void {
  navigator.serviceWorker.controller?.postMessage(message)
}

function isRangeRequest(value: unknown): value is {
  type: 'muxvia-preview-range-request'
  requestId: string
  token: string
  offset: number
  length: number
} {
  if (typeof value !== 'object' || value === null) return false
  const data = value as Record<string, unknown>
  return data.type === 'muxvia-preview-range-request'
    && typeof data.requestId === 'string'
    && typeof data.token === 'string'
    && typeof data.offset === 'number'
    && typeof data.length === 'number'
}

function normalizeRangeValue(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0
}

function createPreviewToken(): string {
  const random = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
  return `preview-${random}`
}

function waitForServiceWorkerController(): Promise<void> {
  if (navigator.serviceWorker.controller) return Promise.resolve()
  return new Promise((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange)
      reject(new Error('file preview service worker controller was not ready'))
    }, 3000)
    const onControllerChange = () => {
      window.clearTimeout(timeout)
      navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange)
      resolve()
    }
    navigator.serviceWorker.addEventListener('controllerchange', onControllerChange)
  })
}
