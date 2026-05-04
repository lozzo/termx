import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileApi, type DirListResponse, type FileApi, type FileEntry } from './fileApi'
import type { ConnectionInfo, RtcSession } from './transport'

export interface FileManagerVisibleError {
  message: string
  surface: 'banner' | 'toast' | 'modal'
  recoverable: boolean
}

export interface UseFileManagerOptions {
  machineId: string
  terminalId: string
  session: Pick<RtcSession, 'openApi' | 'openFileTransfer' | 'getConnectionInfo'>
  initialPath?: string | undefined
}

export interface UseFileManagerResult {
  machineId: string
  terminalId: string
  currentPath: string
  entries: FileEntry[]
  total: number
  loading: boolean
  error: FileManagerVisibleError | null
  fileApi: FileApi
  navigate(path: string): Promise<void>
  refresh(): Promise<void>
}

export function useFileManager(options: UseFileManagerOptions): UseFileManagerResult {
  const [currentPath, setCurrentPath] = useState(options.initialPath ?? '')
  const [entries, setEntries] = useState<FileEntry[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<FileManagerVisibleError | null>(null)
  const currentPathRef = useRef(options.initialPath ?? '')
  const requestSeqRef = useRef(0)

  const fileApi = useMemo(() => createFileApi(options.session), [options.session])

  const loadPath = useCallback(async (path: string) => {
    const seq = ++requestSeqRef.current
    setLoading(true)
    setError(null)
    try {
      const info = await options.session.getConnectionInfo()
      if (seq !== requestSeqRef.current) return
      assertSessionTarget(info, options.machineId, options.terminalId)
      const response: DirListResponse = await fileApi.listDir(path)
      if (seq !== requestSeqRef.current) return
      setCurrentPath(response.path)
      currentPathRef.current = response.path
      setEntries(response.entries)
      setTotal(response.total)
    } catch (err) {
      if (seq !== requestSeqRef.current) return
      setError({
        message: err instanceof Error ? err.message : String(err),
        surface: 'banner',
        recoverable: true,
      })
    } finally {
      if (seq === requestSeqRef.current) setLoading(false)
    }
  }, [fileApi, options.machineId, options.terminalId, options.session])

  useEffect(() => {
    void loadPath(currentPathRef.current)
    return () => {
      requestSeqRef.current += 1
    }
  }, [loadPath])

  const navigate = useCallback(async (path: string) => {
    await loadPath(path)
  }, [loadPath])

  const refresh = useCallback(async () => {
    await loadPath(currentPathRef.current)
  }, [loadPath])

  return {
    machineId: options.machineId,
    terminalId: options.terminalId,
    currentPath,
    entries,
    total,
    loading,
    error,
    fileApi,
    navigate,
    refresh,
  }
}

function assertSessionTarget(info: ConnectionInfo, machineId: string, terminalId?: string): void {
  if (info.machineId !== machineId) {
    throw new Error(`file session machine mismatch: connected to ${info.machineId}, expected ${machineId}`)
  }
  if (info.terminalId !== undefined && info.terminalId !== terminalId) {
    throw new Error(`file session terminal mismatch: connected to ${info.terminalId}, expected ${terminalId}`)
  }
}
