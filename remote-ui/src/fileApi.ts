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

export interface FileApi {
  listDir(path?: string, offset?: number, limit?: number): Promise<DirListResponse>
  stat(path: string): Promise<FileEntry>
  preview(path: string, maxSize?: number): Promise<FilePreviewResponse>
  mkdir(path: string): Promise<{ path: string }>
  delete(path: string): Promise<{ path: string }>
  rename(path: string, newPath: string): Promise<{ path: string }>
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
      request<DirListResponse>('GET', '/files/list', { path, offset, limit }),
    stat: (path: string) =>
      request<FileEntry>('GET', '/files/stat', { path }),
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
