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

export interface FileApi {
  listDir(path?: string, offset?: number, limit?: number): Promise<DirListResponse>
  stat(path: string): Promise<FileEntry>
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
    mkdir: (path: string) =>
      request<{ path: string }>('POST', '/files/mkdir', { path }),
    delete: (path: string) =>
      request<{ path: string }>('POST', '/files/delete', { path }),
    rename: (path: string, newPath: string) =>
      request<{ path: string }>('POST', '/files/rename', { path, new_path: newPath }),
  }
}

function normalizeFileError(err: unknown): Error {
  if (err instanceof Error) return err
  return new Error(String(err))
}
