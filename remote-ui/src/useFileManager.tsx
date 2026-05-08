import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createFileApi, type DirListResponse, type FileApi, type FileEntry, type FilePreviewResponse } from './fileApi'
import type { ConnectionInfo, RtcSession } from './transport'

export const FILE_PREVIEW_MAX_BYTES = 8 * 1024 * 1024

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
  visibleEntries: FileEntry[]
  total: number
  loading: boolean
  error: FileManagerVisibleError | null
  showHidden: boolean
  newDirName: string
  creatingDirectory: boolean
  actionMessage: string | null
  preview: FilePreviewResponse | null
  previewPath: string | null
  previewLoading: boolean
  previewError: FileManagerVisibleError | null
  fileApi: FileApi
  setNewDirName(name: string): void
  toggleShowHidden(): void
  openPreview(path: string): Promise<void>
  closePreview(): void
  createDirectory(): Promise<void>
  deleteEntry(path: string): Promise<void>
  renameEntry(path: string, newName: string): Promise<void>
  navigate(path: string): Promise<void>
  refresh(): Promise<void>
}

export function useFileManager(options: UseFileManagerOptions): UseFileManagerResult {
  const [currentPath, setCurrentPath] = useState(options.initialPath ?? '')
  const [entries, setEntries] = useState<FileEntry[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<FileManagerVisibleError | null>(null)
  const [showHidden, setShowHidden] = useState(false)
  const [newDirName, setNewDirName] = useState('')
  const [creatingDirectory, setCreatingDirectory] = useState(false)
  const [actionMessage, setActionMessage] = useState<string | null>(null)
  const [preview, setPreview] = useState<FilePreviewResponse | null>(null)
  const [previewPath, setPreviewPath] = useState<string | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState<FileManagerVisibleError | null>(null)
  const currentPathRef = useRef(options.initialPath ?? '')
  const requestSeqRef = useRef(0)
  const previewSeqRef = useRef(0)

  const fileApi = useMemo(() => createFileApi(options.session), [options.session])

  const showActionMessage = useCallback((message: string) => {
    setActionMessage(message)
    window.setTimeout(() => setActionMessage(null), 2500)
  }, [])

  const loadPath = useCallback(async (path: string) => {
    const seq = ++requestSeqRef.current
    setLoading(true)
    setError(null)
    try {
      const info = await options.session.getConnectionInfo()
      if (seq !== requestSeqRef.current) return
      assertSessionTarget(info, options.machineId, options.terminalId)
      const { response, fallbackFrom } = await listDirWithParentFallback(fileApi, path)
      if (seq !== requestSeqRef.current) return
      setCurrentPath(response.path)
      currentPathRef.current = response.path
      setEntries(response.entries)
      setTotal(response.total)
      if (fallbackFrom && fallbackFrom !== response.path) {
        showActionMessage(`Opened ${response.path} instead`)
      }
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
  }, [fileApi, options.machineId, options.terminalId, options.session, showActionMessage])

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

  const visibleEntries = useMemo(
    () => showHidden ? entries : entries.filter((entry) => !entry.name.startsWith('.')),
    [entries, showHidden],
  )

  const toggleShowHidden = useCallback(() => {
    setShowHidden((current) => !current)
  }, [])

  const openPreview = useCallback(async (path: string) => {
    const seq = ++previewSeqRef.current
    setPreviewPath(path)
    setPreview(null)
    setPreviewError(null)
    setPreviewLoading(true)
    try {
      const info = await options.session.getConnectionInfo()
      if (seq !== previewSeqRef.current) return
      assertSessionTarget(info, options.machineId, options.terminalId)
      const response = await fileApi.preview(path, FILE_PREVIEW_MAX_BYTES)
      if (seq !== previewSeqRef.current) return
      setPreview(response)
    } catch (err) {
      if (seq !== previewSeqRef.current) return
      setPreviewError({
        message: err instanceof Error ? err.message : String(err),
        surface: 'modal',
        recoverable: true,
      })
    } finally {
      if (seq === previewSeqRef.current) setPreviewLoading(false)
    }
  }, [fileApi, options.machineId, options.terminalId, options.session])

  const closePreview = useCallback(() => {
    previewSeqRef.current += 1
    setPreview(null)
    setPreviewPath(null)
    setPreviewError(null)
    setPreviewLoading(false)
  }, [])

  const createDirectory = useCallback(async () => {
    const name = newDirName.trim()
    if (!name) return
    setCreatingDirectory(true)
    setError(null)
    try {
      await fileApi.mkdir(joinPath(currentPathRef.current || '/', name))
      setNewDirName('')
      showActionMessage(`Created ${name}`)
      await loadPath(currentPathRef.current)
    } catch (err) {
      setError({
        message: err instanceof Error ? err.message : String(err),
        surface: 'toast',
        recoverable: true,
      })
    } finally {
      setCreatingDirectory(false)
    }
  }, [fileApi, loadPath, newDirName, showActionMessage])

  const deleteEntry = useCallback(async (path: string) => {
    setError(null)
    try {
      await fileApi.delete(path)
      showActionMessage(`Deleted ${basename(path)}`)
      await loadPath(currentPathRef.current)
    } catch (err) {
      setError({
        message: err instanceof Error ? err.message : String(err),
        surface: 'toast',
        recoverable: true,
      })
    }
  }, [fileApi, loadPath, showActionMessage])

  const renameEntry = useCallback(async (path: string, newName: string) => {
    const name = newName.trim()
    if (!name) return
    setError(null)
    try {
      await fileApi.rename(path, joinPath(parentPath(path), name))
      showActionMessage(`Renamed to ${name}`)
      await loadPath(currentPathRef.current)
    } catch (err) {
      setError({
        message: err instanceof Error ? err.message : String(err),
        surface: 'toast',
        recoverable: true,
      })
    }
  }, [fileApi, loadPath, showActionMessage])

  return {
    machineId: options.machineId,
    terminalId: options.terminalId,
    currentPath,
    entries,
    visibleEntries,
    total,
    loading,
    error,
    showHidden,
    newDirName,
    creatingDirectory,
    actionMessage,
    preview,
    previewPath,
    previewLoading,
    previewError,
    fileApi,
    setNewDirName,
    toggleShowHidden,
    openPreview,
    closePreview,
    createDirectory,
    deleteEntry,
    renameEntry,
    navigate,
    refresh,
  }
}

function joinPath(base: string, name: string): string {
  if (!base || base === '/') return `/${name.replace(/^\/+/, '')}`
  return `${base.replace(/\/+$/, '')}/${name.replace(/^\/+/, '')}`
}

function parentPath(path: string): string {
  const normalized = path.replace(/\/+$/, '')
  const index = normalized.lastIndexOf('/')
  if (index <= 0) return '/'
  return normalized.slice(0, index)
}

async function listDirWithParentFallback(
  fileApi: FileApi,
  path: string,
): Promise<{ response: DirListResponse; fallbackFrom?: string | undefined }> {
  const requested = normalizeAbsolutePath(path)
  let current = requested
  let fallbackFrom: string | undefined
  let lastErr: unknown
  for (;;) {
    try {
      return { response: await fileApi.listDir(current), fallbackFrom }
    } catch (err) {
      lastErr = err
      if (current === '/') break
      fallbackFrom = requested
      current = parentPath(current)
    }
  }
  throw lastErr instanceof Error ? lastErr : new Error(String(lastErr))
}

function normalizeAbsolutePath(path: string): string {
  const trimmed = path.trim()
  if (!trimmed || trimmed === '/') return '/'
  return trimmed.replace(/\/+$/, '') || '/'
}

function basename(path: string): string {
  const normalized = path.replace(/\/+$/, '')
  return normalized.slice(normalized.lastIndexOf('/') + 1) || normalized
}

function assertSessionTarget(info: ConnectionInfo, machineId: string, terminalId?: string): void {
  if (info.machineId !== machineId) {
    throw new Error(`file session machine mismatch: connected to ${info.machineId}, expected ${machineId}`)
  }
  if (info.terminalId !== undefined && info.terminalId !== terminalId) {
    throw new Error(`file session terminal mismatch: connected to ${info.terminalId}, expected ${terminalId}`)
  }
}
