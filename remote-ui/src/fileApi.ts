import type { RtcSession } from './transport'

export type FileEntryType = 'file' | 'dir' | 'symlink' | 'symlink-dir'

export interface FileEntry {
  name: string
  type: FileEntryType | string
  size: number
  mode?: string | undefined
  modTime?: string | undefined
  linkTarget?: string | undefined
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
  pauseTransfer?(id: string): void
  resumeTransfer?(id: string): void
  resumeAllTransfers?(machineId?: string): void
  cancelTransfer(id: string): void
  dismissTransfer(id: string): void
  isNative: boolean
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
    listDir: (path = '', offset = 0, limit = 500) =>
      request<DirListResponse>('POST', '/files/list', { path, offset, limit }),
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
  return {
    path: raw.path ?? requestedPath,
    name,
    size: typeof raw.size === 'number' ? raw.size : 0,
    mimeType,
    category,
    isText: raw.is_text ?? raw.isText ?? category === 'text',
    content: raw.content,
    contentBase64: raw.content_base64 ?? raw.contentBase64,
    previewLimit: raw.preview_limit ?? raw.previewLimit,
  }
}

function normalizePreviewCategory(raw: string | undefined, mimeType: string): FilePreviewCategory {
  if (raw === 'text' || raw === 'image' || raw === 'video' || raw === 'unsupported') return raw
  if (mimeType.startsWith('text/')) return 'text'
  if (mimeType.startsWith('image/')) return 'image'
  if (mimeType.startsWith('video/')) return 'video'
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
