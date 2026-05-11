import type { RtcBinaryChannel, RtcSession } from '../core/transport'

export type FileEntryType = 'file' | 'dir' | 'symlink' | 'symlink-dir'

export interface FileEntry {
  name: string
  type: FileEntryType | string
  size: number
  mode?: string | undefined
  modTime?: string | undefined
  linkTarget?: string | undefined
  childCount?: number | undefined
  hardLink?: boolean | undefined
  linkCount?: number | undefined
  inode?: number | undefined
}

export interface DirListResponse {
  path: string
  entries: FileEntry[]
  parent: string
  total: number
}

export type FilePreviewCategory = 'text' | 'image' | 'video' | 'unsupported'

export interface FilePreviewResponse {
  path: string
  name: string
  size: number
  mimeType: string
  category: FilePreviewCategory
  isText: boolean
  content?: string | undefined
  contentBase64?: string | undefined
  previewLimit?: number | undefined
}

export interface DownloadInitResponse {
  transfer_id: string
  name: string
  size: number
  chunk_size: number
  offset?: number | undefined
  length?: number | undefined
}

export type TransferStatus = 'pending' | 'transferring' | 'paused' | 'completed' | 'failed' | 'cancelled' | 'missing'

export interface TransferInfo {
  id: string
  machineId?: string
  name: string
  direction: 'download' | 'upload'
  totalSize: number
  transferredSize: number
  status: TransferStatus
  startedAt: number
  updatedAt?: number
  bytesPerSecond?: number
  error?: string
  filePath?: string
  localUri?: string | undefined
  targetDir?: string | undefined
  savedPath?: string | undefined
  savedUri?: string | undefined
}

export interface FileTransferContext {
  /** Subscribe to state changes (useSyncExternalStore API). */
  subscribe(listener: () => void): () => void
  /** Get current snapshot (useSyncExternalStore API). */
  getSnapshot(): { transfers: TransferInfo[]; hasActiveTransfers: boolean }
  getDownloadResumeOffset?(machineId: string, filePath: string, fileSize: number): Promise<number> | number
  startDownload(machineId: string, transferId: string, fileName: string, fileSize: number, filePath: string, offset?: number): void
  startUpload(machineId: string, files: Array<{ uri: string; name: string; size: number }>, targetDir: string): void
  /** Native mode only: opens system file picker then starts upload. */
  pickAndUpload?(machineId: string, targetDir: string): void
  pauseTransfer?(id: string): void | Promise<void>
  resumeTransfer?(id: string): void | Promise<void>
  resumeAllTransfers?(machineId?: string): void | Promise<void>
  cancelTransfer(id: string): void
  dismissTransfer(id: string): void
  isNative: boolean
}

export interface FilePreviewStreamProgress {
  receivedSize: number
  totalSize: number
}

export interface FilePreviewStreamChunk extends FilePreviewStreamProgress {
  chunk: ArrayBuffer
}

export interface FilePreviewStreamResult {
  blob: Blob
  receivedSize: number
  offset: number
  totalSize: number
}

export interface FilePreviewStreamOptions {
  signal?: AbortSignal | undefined
  offset?: number | undefined
  length?: number | undefined
  onChunk?: ((chunk: FilePreviewStreamChunk) => void) | undefined
  onProgress?: ((progress: FilePreviewStreamProgress) => void) | undefined
}

export interface FileApi {
  listDir(path?: string, offset?: number, limit?: number): Promise<DirListResponse>
  stat(path: string): Promise<FileEntry>
  preview(path: string, maxSize?: number): Promise<FilePreviewResponse>
  mkdir(path: string): Promise<{ path: string }>
  delete(path: string): Promise<{ path: string }>
  rename(path: string, newPath: string): Promise<{ path: string }>
  copy(paths: string[], targetDir: string): Promise<{ task_id: string }>
  move(paths: string[], targetDir: string): Promise<{ task_id: string }>
  batchDelete(paths: string[]): Promise<{ task_id: string }>
  downloadInit(path: string, offset?: number, transferId?: string): Promise<DownloadInitResponse>
  downloadRangeInit(path: string, offset: number, length: number, transferId?: string): Promise<DownloadInitResponse>
}

export interface FilePreviewSource {
  preview(path: string, maxSize?: number): Promise<FilePreviewResponse>
  stream(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
}

export function createFileApi(session: Pick<RtcSession, 'openApi'>): FileApi {
  const apiChannel = () => {
    return session.openApi()
  }

  async function request<TResponse>(
    method: 'GET' | 'POST',
    path: string,
    params?: Record<string, unknown>,
  ): Promise<TResponse> {
    try {
      const channel = await apiChannel()
      return await channel.request<TResponse>(method, { path, params })
    } catch (err) {
      throw normalizeFileError(err)
    }
  }

  return {
    listDir: async (path = '', offset = 0, limit = 500) => normalizeDirListResponse(
      await request<RawDirListResponse>('POST', '/files/list', { path, offset, limit }),
    ),
    stat: (path: string) =>
      request<FileEntry>('POST', '/files/stat', { path }),
    preview: async (path: string, maxSize?: number) => normalizeFilePreviewResponse(
      await request<RawFilePreviewResponse>(
        'POST',
        '/files/preview',
        maxSize && maxSize > 0 ? { path, max_size: maxSize } : { path },
      ),
      path,
    ),
    mkdir: (path: string) =>
      request<{ path: string }>('POST', '/files/mkdir', { path }),
    delete: (path: string) =>
      request<{ path: string }>('POST', '/files/delete', { path }),
    rename: (path: string, newPath: string) =>
      request<{ path: string }>('POST', '/files/rename', { path, new_path: newPath }),
    copy: (paths: string[], targetDir: string) =>
      request<{ task_id: string }>('POST', '/files/copy', { paths, target_dir: targetDir }),
    move: (paths: string[], targetDir: string) =>
      request<{ task_id: string }>('POST', '/files/move', { paths, target_dir: targetDir }),
    batchDelete: (paths: string[]) =>
      request<{ task_id: string }>('POST', '/files/batch-delete', { paths }),
    downloadInit: (path: string, offset = 0, transferId?: string) =>
      request<DownloadInitResponse>('POST', '/files/download/init', {
        path,
        ...(offset > 0 ? { offset } : {}),
        ...(transferId ? { transfer_id: transferId } : {}),
      }),
    downloadRangeInit: (path: string, offset: number, length: number, transferId?: string) =>
      request<DownloadInitResponse>('POST', '/files/download/init', {
        path,
        ...(offset > 0 ? { offset } : {}),
        ...(length > 0 ? { length } : {}),
        ...(transferId ? { transfer_id: transferId } : {}),
      }),
  }
}

export function createFilePreviewSource(session: Pick<RtcSession, 'openApi' | 'openFileTransfer'>): FilePreviewSource {
  const api = createFileApi(session)
  return {
    preview: api.preview,
    async stream(path: string, mimeType: string, options: FilePreviewStreamOptions = {}) {
      const transferId = `preview-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
      const offset = normalizeByteRangeValue(options.offset)
      const init = await downloadRangeInit(api, path, offset, normalizeByteRangeValue(options.length), transferId)
      throwIfAborted(options.signal)
      const channel = await session.openFileTransfer(init.transfer_id)
      try {
        return await readFileTransferStream(channel, init.size, mimeType, {
          ...options,
          offset: init.offset ?? offset,
        })
      } finally {
        channel.close()
      }
    },
  }
}

async function downloadRangeInit(
  api: FileApi,
  path: string,
  offset: number,
  length: number,
  transferId: string,
): Promise<DownloadInitResponse> {
  if (length > 0) {
    return await api.downloadRangeInit(path, offset, length, transferId)
  }
  return await api.downloadInit(path, offset, transferId)
}

const fileFrameData = 0x01
const fileFrameComplete = 0x02
const fileFrameError = 0xff

async function readFileTransferStream(
  channel: RtcBinaryChannel,
  totalSize: number,
  mimeType: string,
  options: FilePreviewStreamOptions,
): Promise<FilePreviewStreamResult> {
  await channel.waitOpen()
  throwIfAborted(options.signal)

  return await new Promise<FilePreviewStreamResult>((resolve, reject) => {
    const chunks: ArrayBuffer[] = []
    let receivedSize = 0
    let settled = false
    let messageSubscription: { close(): void } | null = null
    let closeSubscription: { close(): void } | null = null

    const cleanup = () => {
      messageSubscription?.close()
      closeSubscription?.close()
      options.signal?.removeEventListener('abort', abortListener)
    }
    const finish = (result: FilePreviewStreamResult) => {
      if (settled) return
      settled = true
      cleanup()
      resolve(result)
    }
    const fail = (err: unknown) => {
      if (settled) return
      settled = true
      cleanup()
      reject(err)
    }
    const abortListener = () => {
      channel.close()
      fail(abortError())
    }
    messageSubscription = channel.onMessage((frame) => {
      if (settled) return
      if (frame.length === 0) return
      const frameType = frame[0]
      if (frameType === fileFrameData) {
        if (frame.length < 5) {
          fail(new Error('file preview stream sent an invalid data frame'))
          return
        }
        const payload = frame.slice(5)
        const chunk = new ArrayBuffer(payload.byteLength)
        new Uint8Array(chunk).set(payload)
        chunks.push(chunk)
        receivedSize += payload.byteLength
        try {
          options.onChunk?.({ chunk, receivedSize, totalSize })
          options.onProgress?.({ receivedSize, totalSize })
        } catch (err) {
          fail(err)
        }
        return
      }
      if (frameType === fileFrameComplete) {
        try {
          options.onProgress?.({ receivedSize, totalSize })
        } catch (err) {
          fail(err)
          return
        }
        finish({
          blob: new Blob(chunks, { type: mimeType.trim() || 'application/octet-stream' }),
          receivedSize,
          offset: options.offset ?? 0,
          totalSize,
        })
        return
      }
      if (frameType === fileFrameError) {
        fail(new Error(new TextDecoder().decode(frame.slice(1)) || 'file preview stream failed'))
      }
    })
    closeSubscription = channel.onClose(() => {
      if (!settled) fail(new Error('file preview stream closed before completion'))
    })

    options.signal?.addEventListener('abort', abortListener, { once: true })
    if (options.signal?.aborted) abortListener()
  })
}

interface RawFileEntryResponse {
  name?: string
  type?: string
  size?: number
  mode?: string
  mod_time?: string
  modTime?: string
  modified_at?: string
  modifiedAt?: string
  link_target?: string
  linkTarget?: string
  child_count?: number
  childCount?: number
  hard_link?: boolean
  hardLink?: boolean
  link_count?: number
  linkCount?: number
  inode?: number
}

interface RawDirListResponse {
  path?: string
  entries?: RawFileEntryResponse[]
  parent?: string
  total?: number
}

function normalizeDirListResponse(raw: RawDirListResponse): DirListResponse {
  const entries = Array.isArray(raw.entries) ? raw.entries.map(normalizeFileEntryResponse) : []
  return {
    path: raw.path ?? '',
    entries,
    parent: raw.parent ?? '',
    total: typeof raw.total === 'number' ? raw.total : entries.length,
  }
}

function normalizeFileEntryResponse(raw: RawFileEntryResponse): FileEntry {
  return {
    name: raw.name ?? '',
    type: raw.type ?? 'file',
    size: typeof raw.size === 'number' ? raw.size : 0,
    mode: raw.mode,
    modTime: raw.mod_time ?? raw.modTime ?? raw.modified_at ?? raw.modifiedAt,
    linkTarget: raw.link_target ?? raw.linkTarget,
    childCount: typeof raw.child_count === 'number' ? raw.child_count : typeof raw.childCount === 'number' ? raw.childCount : undefined,
    hardLink: raw.hard_link === true || raw.hardLink === true,
    linkCount: typeof raw.link_count === 'number' ? raw.link_count : typeof raw.linkCount === 'number' ? raw.linkCount : undefined,
    inode: typeof raw.inode === 'number' ? raw.inode : undefined,
  }
}

interface RawFilePreviewResponse {
  path?: string
  name?: string
  size?: number
  mime_type?: string
  mimeType?: string
  category?: string
  is_text?: boolean
  isText?: boolean
  content?: string
  content_base64?: string
  contentBase64?: string
  preview_limit?: number
  previewLimit?: number
}

function normalizeFilePreviewResponse(raw: RawFilePreviewResponse, requestedPath: string): FilePreviewResponse {
  const mimeType = raw.mime_type ?? raw.mimeType ?? 'application/octet-stream'
  const category = normalizePreviewCategory(raw.category, mimeType)
  const name = raw.name ?? basename(raw.path ?? requestedPath)
  const contentBase64 = raw.content_base64 ?? raw.contentBase64 ?? (category === 'image' || category === 'video' ? raw.content : undefined)
  return {
    path: raw.path ?? requestedPath,
    name,
    size: typeof raw.size === 'number' ? raw.size : 0,
    mimeType,
    category,
    isText: raw.is_text ?? raw.isText ?? category === 'text',
    content: category === 'text' ? raw.content : undefined,
    contentBase64,
    previewLimit: raw.preview_limit ?? raw.previewLimit,
  }
}

function normalizePreviewCategory(raw: string | undefined, mimeType: string): FilePreviewCategory {
  const category = raw?.trim().toLowerCase()
  if (category === 'text' || category === 'image' || category === 'video' || category === 'unsupported') return category
  const normalizedMime = mimeType.trim().toLowerCase()
  if (normalizedMime.startsWith('text/')) return 'text'
  if (normalizedMime.startsWith('image/')) return 'image'
  if (normalizedMime.startsWith('video/')) return 'video'
  return 'unsupported'
}

function basename(path: string): string {
  const normalized = path.replace(/\/+$/, '')
  return normalized.slice(normalized.lastIndexOf('/') + 1) || normalized
}

function normalizeFileError(err: unknown): Error {
  if (err instanceof Error) return err
  return new Error(String(err))
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (signal?.aborted) throw abortError()
}

function normalizeByteRangeValue(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0
}

function abortError(): Error {
  const err = new Error('File preview stream was cancelled.')
  err.name = 'AbortError'
  return err
}
