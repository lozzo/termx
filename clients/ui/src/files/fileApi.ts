import type { RtcBinaryChannel, RtcSession } from '../core/transport'
import { isModelPreviewFile } from './modelFileTypes'
import { TERMX_FRAME_TYPES, decodeTermxFrame, encodeTermxFrame } from '../terminal/termxProtocol'
import {
  decodeFileTransferDataPayload,
  decodeFileTransferFinishPayload,
  decodeTerminalErrorPayload,
  encodeFileTransferAckPayload,
} from '../terminal/terminalWireProtocol'

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

export type FilePreviewCategory = 'text' | 'image' | 'video' | 'model' | 'unsupported'

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
  channel: number
  path: string
  name: string
  size: number
  chunk_size: number
  window_bytes: number
  offset: number
  modified_at_unix_nano: number
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
  startDownload(machineId: string, fileName: string, fileSize: number, filePath: string, offset?: number): void
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
  listDir(path?: string, cursor?: string, limit?: number): Promise<DirListResponse>
  stat(path: string): Promise<FileEntry>
  preview(path: string, maxSize?: number): Promise<FilePreviewResponse>
  mkdir(path: string): Promise<{ path: string }>
  delete(path: string): Promise<{ path: string }>
  rename(path: string, newPath: string): Promise<{ path: string }>
  copy(paths: string[], targetDir: string): Promise<void>
  move(paths: string[], targetDir: string): Promise<void>
  batchDelete(paths: string[]): Promise<void>
  downloadOpen(path: string, offset?: number, expectedSize?: number, expectedModifiedAtUnixNano?: number): Promise<DownloadInitResponse>
}

export interface FilePreviewSource {
  preview(path: string, maxSize?: number): Promise<FilePreviewResponse>
  stream(path: string, mimeType: string, options?: FilePreviewStreamOptions): Promise<FilePreviewStreamResult>
}

export function createFileApi(session: Pick<RtcSession, 'openApi'>): FileApi {
  const apiChannel = () => {
    return session.openApi()
  }

  async function request<TResponse>(method: string, params?: Record<string, unknown>): Promise<TResponse> {
    try {
      const channel = await apiChannel()
      return await channel.request<TResponse>(method, params)
    } catch (err) {
      throw normalizeFileError(err)
    }
  }

  return {
    listDir: async (path = '/', cursor = '', limit = 500) => normalizeProtocolDirListResponse(
      await request<ProtocolDirListResponse>('file.list', { path: normalizeFilePath(path), cursor, limit }),
    ),
    stat: async (path: string) => normalizeProtocolFileEntry(await request<ProtocolFileEntry>('file.stat', { path })),
    preview: async (path: string, maxSize?: number) => normalizeFilePreviewResponse(
      await request<ProtocolFilePreviewResponse>(
        'file.preview',
        maxSize && maxSize > 0 ? { path, max_bytes: maxSize } : { path },
      ),
      path,
    ),
    mkdir: async (path: string) => operationPath(await request<ProtocolFileOperation>('file.mkdir', { path })),
    delete: async (path: string) => operationPath(await request<ProtocolFileOperation>('file.delete', { path, recursive: true })),
    rename: async (path: string, newPath: string) => operationPath(await request<ProtocolFileOperation>('file.rename', { path, new_path: newPath })),
    copy: async (paths: string[], targetDir: string) => assertBatchSuccess(await request<ProtocolFileBatch>('file.copy', { paths, target_dir: targetDir })),
    move: async (paths: string[], targetDir: string) => assertBatchSuccess(await request<ProtocolFileBatch>('file.move', { paths, target_dir: targetDir })),
    batchDelete: async (paths: string[]) => { for (const path of paths) operationPath(await request<ProtocolFileOperation>('file.delete', { path, recursive: true })) },
    downloadOpen: async (path: string, offset = 0, expectedSize = 0, expectedModifiedAtUnixNano = 0) => {
      const opened = await request<ProtocolFileTransferOpen>('file.download.open', { path, offset, expected_size: expectedSize, expected_modified_at_unix_nano: expectedModifiedAtUnixNano })
      return { ...opened, name: basename(opened.path), chunk_size: opened.chunk_bytes }
    },
  }
}

export function createFilePreviewSource(session: Pick<RtcSession, 'openApi' | 'openFileChannel'>): FilePreviewSource {
  const api = createFileApi(session)
  return {
    preview: api.preview,
    async stream(path: string, mimeType: string, options: FilePreviewStreamOptions = {}) {
      const init = await api.downloadOpen(path)
      throwIfAborted(options.signal)
      const channel = await session.openFileChannel(init.channel, init.transfer_id)
      try {
        return await readProtocolFileTransferStream(channel, init, mimeType, options)
      } finally {
        channel.close()
      }
    },
  }
}


async function readProtocolFileTransferStream(
  channel: RtcBinaryChannel,
  init: DownloadInitResponse,
  mimeType: string,
  options: FilePreviewStreamOptions,
): Promise<FilePreviewStreamResult> {
  await channel.waitOpen()
  throwIfAborted(options.signal)

  return await new Promise<FilePreviewStreamResult>((resolve, reject) => {
    const chunks: Uint8Array[] = []
    let receivedSize = init.offset
    let bytesSinceAck = 0
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
    messageSubscription = channel.onMessage((rawFrame) => {
      if (settled) return
      const frame = decodeTermxFrame(rawFrame)
      if (frame.channel !== init.channel) { fail(new Error('file stream channel mismatch')); return }
      if (frame.type === TERMX_FRAME_TYPES.fileData) {
        const data = decodeFileTransferDataPayload(frame.payload)
        if (data.offset !== receivedSize || data.data.byteLength === 0 || data.data.byteLength > init.chunk_size) { fail(new Error('file stream offset or chunk is invalid')); return }
        chunks.push(data.data)
        receivedSize += data.data.byteLength
        bytesSinceAck += data.data.byteLength
        const chunk = data.data.buffer.slice(data.data.byteOffset, data.data.byteOffset + data.data.byteLength) as ArrayBuffer
        try {
          options.onChunk?.({ chunk, receivedSize, totalSize: init.size })
          options.onProgress?.({ receivedSize, totalSize: init.size })
        } catch (err) {
          fail(err)
        }
        if (bytesSinceAck >= init.window_bytes) {
          channel.send(encodeTermxFrame(init.channel, TERMX_FRAME_TYPES.fileAck, encodeFileTransferAckPayload({ offset: receivedSize, windowBytes: bytesSinceAck })))
          bytesSinceAck = 0
        }
        return
      }
      if (frame.type === TERMX_FRAME_TYPES.fileFinish) {
        const declared = decodeFileTransferFinishPayload(frame.payload)
        if (declared.size !== init.size || receivedSize !== init.size) { fail(new Error('file stream completed with the wrong size')); return }
        void verifyFileDigest(chunks, declared.sha256).then(() => {
          options.onProgress?.({ receivedSize, totalSize: init.size })
          const blobParts = chunks.map((chunk) => chunk.buffer.slice(chunk.byteOffset, chunk.byteOffset + chunk.byteLength) as ArrayBuffer)
          finish({ blob: new Blob(blobParts, { type: mimeType.trim() || 'application/octet-stream' }), receivedSize, offset: init.offset, totalSize: init.size })
        }).catch(fail)
        return
      }
      if (frame.type === TERMX_FRAME_TYPES.error) {
        fail(new Error(decodeTerminalErrorPayload(frame.payload).message))
      }
    })
    closeSubscription = channel.onClose(() => {
      if (!settled) fail(new Error('file preview stream closed before completion'))
    })

    options.signal?.addEventListener('abort', abortListener, { once: true })
    if (options.signal?.aborted) abortListener()
  })
}

async function verifyFileDigest(chunks: Uint8Array[], expected: Uint8Array): Promise<void> {
  if (expected.byteLength !== 32) throw new Error('file stream returned an invalid SHA-256')
  const size = chunks.reduce((total, chunk) => total + chunk.byteLength, 0)
  const content = new Uint8Array(size)
  let offset = 0
  for (const chunk of chunks) { content.set(chunk, offset); offset += chunk.byteLength }
  const actual = new Uint8Array(await crypto.subtle.digest('SHA-256', content))
  if (!actual.every((value, index) => value === expected[index])) throw new Error('file stream SHA-256 mismatch')
}

interface ProtocolFileEntry {
  path: string
  name?: string
  type?: string
  size?: number
  mode?: number
  modified_at_unix_nano?: number
  link_target?: string
}

interface ProtocolDirListResponse {
  path: string
  entries: ProtocolFileEntry[]
  next_cursor?: string
}

function normalizeProtocolDirListResponse(raw: ProtocolDirListResponse): DirListResponse {
  const entries = Array.isArray(raw.entries) ? raw.entries.map(normalizeProtocolFileEntry) : []
  return {
    path: raw.path,
    entries,
    parent: parentPath(raw.path),
    total: entries.length,
  }
}

function normalizeProtocolFileEntry(raw: ProtocolFileEntry): FileEntry {
  return {
    name: raw.name ?? '',
    type: raw.type ?? 'file',
    size: typeof raw.size === 'number' ? raw.size : 0,
    mode: typeof raw.mode === 'number' ? raw.mode.toString(8) : undefined,
    modTime: raw.modified_at_unix_nano ? new Date(raw.modified_at_unix_nano / 1_000_000).toISOString() : undefined,
    linkTarget: raw.link_target,
  }
}

interface ProtocolFilePreviewResponse {
  entry: ProtocolFileEntry
  mime_type: string
  content: Uint8Array
  truncated: boolean
}

interface ProtocolFileOperation { path: string; target_path: string; success: boolean; error_code: string; error_message: string }
interface ProtocolFileBatch { results: ProtocolFileOperation[] }
interface ProtocolFileTransferOpen { transfer_id: string; channel: number; path: string; offset: number; size: number; modified_at_unix_nano: number; window_bytes: number; chunk_bytes: number }

function operationPath(result: ProtocolFileOperation): { path: string } {
  if (!result.success) throw new Error(result.error_message || result.error_code || 'file operation failed')
  return { path: result.target_path || result.path }
}

function assertBatchSuccess(result: ProtocolFileBatch): void {
  const failed = result.results.find((item) => !item.success)
  if (failed) throw new Error(failed.error_message || failed.error_code || `file operation failed for ${failed.path}`)
}

function normalizeFilePreviewResponse(raw: ProtocolFilePreviewResponse, requestedPath: string): FilePreviewResponse {
  const mimeType = raw.mime_type || 'application/octet-stream'
  const path = raw.entry?.path || requestedPath
  const name = raw.entry?.name || basename(path)
  const category = normalizePreviewCategory(undefined, mimeType, name)
  const contentBase64 = category === 'image' || category === 'video' ? bytesToBase64(raw.content) : undefined
  return {
    path,
    name,
    size: raw.entry?.size ?? 0,
    mimeType,
    category,
    isText: category === 'text',
    content: category === 'text' ? new TextDecoder().decode(raw.content) : undefined,
    contentBase64,
    previewLimit: raw.truncated ? raw.content.byteLength : undefined,
  }
}

function normalizePreviewCategory(raw: string | undefined, mimeType: string, name: string): FilePreviewCategory {
  const category = raw?.trim().toLowerCase()
  const normalizedMime = mimeType.trim().toLowerCase()
  if (category === 'text' || category === 'image' || category === 'video' || category === 'model') return category
  if (isModelPreviewFile(name, normalizedMime)) return 'model'
  if (category === 'unsupported') return category
  if (normalizedMime.startsWith('text/')) return 'text'
  if (normalizedMime.startsWith('image/')) return 'image'
  if (normalizedMime.startsWith('video/')) return 'video'
  if (normalizedMime.startsWith('model/')) return 'model'
  return 'unsupported'
}

function basename(path: string): string {
  const normalized = path.replace(/\/+$/, '')
  return normalized.slice(normalized.lastIndexOf('/') + 1) || normalized
}

function parentPath(path: string): string {
  const normalized = normalizeFilePath(path)
  if (normalized === '/') return ''
  const parent = normalized.slice(0, normalized.lastIndexOf('/'))
  return parent || '/'
}

function normalizeFilePath(path: string): string {
  const trimmed = path.trim()
  return trimmed.startsWith('/') ? trimmed : '/'
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
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
