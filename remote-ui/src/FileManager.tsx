import { useFileManager, type FileSortState } from './useFileManager'
import type { FilePreviewResponse, FileTransferContext } from './fileApi'
import type { RtcSession } from './transport'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import cpp from 'highlight.js/lib/languages/cpp'
import css from 'highlight.js/lib/languages/css'
import go from 'highlight.js/lib/languages/go'
import java from 'highlight.js/lib/languages/java'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import python from 'highlight.js/lib/languages/python'
import rust from 'highlight.js/lib/languages/rust'
import sql from 'highlight.js/lib/languages/sql'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'
import 'highlight.js/styles/github.css'
import { addNativeBackHandler } from './nativeBack'
import { ActionSheet, type ActionSheetItem } from './ActionSheet'
import { AlertCircle, ArrowDownAZ, ArrowDownToLine, ArrowUpAZ, ArrowUpFromLine, Check, ChevronRight, Clock, Code2, Eye, EyeOff, File, FileText, FileType, Folder, HardDrive, ListFilter, Image, ListChecks, MoreVertical, PlaySquare, RefreshCw, SquarePen, Trash2, WrapText, X } from 'lucide-react'

hljs.registerLanguage('bash', bash)
hljs.registerLanguage('cpp', cpp)
hljs.registerLanguage('css', css)
hljs.registerLanguage('go', go)
hljs.registerLanguage('java', java)
hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('json', json)
hljs.registerLanguage('python', python)
hljs.registerLanguage('rust', rust)
hljs.registerLanguage('sql', sql)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('yaml', yaml)

export interface FileManagerProps {
  machineId: string
  terminalId?: string | undefined
  session: Pick<RtcSession, 'openApi' | 'openFileTransfer' | 'getConnectionInfo'>
  initialPath?: string | undefined
  className?: string | undefined
  active?: boolean | undefined
  fileTransfer?: FileTransferContext | undefined
  onOpenTransferCenter?: (() => void) | undefined
}

export function FileManager({
  machineId,
  terminalId,
  session,
  initialPath,
  className,
  active = true,
  fileTransfer,
  onOpenTransferCenter,
}: FileManagerProps) {
  const manager = useFileManager({ machineId, terminalId, session, initialPath })
  const [newDirOpen, setNewDirOpen] = useState(false)
  const [entryMenuPath, setEntryMenuPath] = useState<string | null>(null)
  const [renamePath, setRenamePath] = useState<string | null>(null)
  const [renameName, setRenameName] = useState('')
  const [deletePath, setDeletePath] = useState<string | null>(null)
  const [sortMenuOpen, setSortMenuOpen] = useState(false)
  const [transferError, setTransferError] = useState<string | null>(null)
  const webUploadRef = useRef<HTMLInputElement>(null)

  const pathSegments = manager.currentPath ? manager.currentPath.split('/').filter(Boolean) : []
  const entryKeyCounts = new Map<string, number>()
  const sortLabel = fileSortLabel(manager.sortState)

  const menuEntry = useMemo(() => {
    if (!entryMenuPath) return null
    return manager.entries.find(e => joinPath(manager.currentPath, e.name) === entryMenuPath)
  }, [entryMenuPath, manager.entries, manager.currentPath])

  const menuActions: ActionSheetItem[] = useMemo(() => {
    if (!menuEntry || !entryMenuPath) return []
    const isDirectory = menuEntry.type === 'dir' || menuEntry.type === 'symlink-dir'
    const actions: ActionSheetItem[] = []

    if (!isDirectory) {
      actions.push({
        label: 'Preview',
        icon: <Eye className="h-5 w-5" />,
        onClick: () => void manager.openPreview(entryMenuPath),
      })
    }

    if (!isDirectory && fileTransfer) {
      actions.push({
        label: 'Download',
        icon: <ArrowDownToLine className="h-5 w-5" />,
        onClick: async () => {
          setTransferError(null)
          try {
            const resumeOffset = Math.max(
              0,
              Math.min(
                menuEntry.size,
                await Promise.resolve(fileTransfer.getDownloadResumeOffset?.(machineId, entryMenuPath, menuEntry.size) ?? 0),
              ),
            )
            const init = await manager.fileApi.downloadInit(entryMenuPath, resumeOffset)
            fileTransfer.startDownload(
              machineId,
              init.transfer_id,
              init.name || menuEntry.name,
              init.size,
              entryMenuPath,
              resumeOffset,
            )
            onOpenTransferCenter?.()
          } catch (err) {
            setTransferError(err instanceof Error ? err.message : String(err))
          }
        },
      })
    }

    actions.push({
      label: 'Copy',
      icon: <File className="h-5 w-5" />,
      onClick: () => manager.copy([entryMenuPath]),
    })

    actions.push({
      label: 'Cut',
      icon: <Folder className="h-5 w-5" />,
      onClick: () => manager.cut([entryMenuPath]),
    })

    actions.push({
      label: 'Rename',
      icon: <SquarePen className="h-5 w-5" />,
      onClick: () => {
        setRenamePath(entryMenuPath)
        setRenameName(menuEntry.name)
      },
    })

    actions.push({
      label: 'Delete',
      icon: <Trash2 className="h-5 w-5" />,
      onClick: () => setDeletePath(entryMenuPath),
      danger: true,
    })

    return actions
  }, [menuEntry, entryMenuPath, manager, fileTransfer, machineId])

  const sortActions: ActionSheetItem[] = useMemo(() => fileSortOptions.map((option) => {
    const active = option.field === manager.sortState.field && option.direction === manager.sortState.direction
    return {
      label: `${active ? 'Selected: ' : ''}${option.label}`,
      icon: active ? <Check className="h-5 w-5" /> : option.icon,
      onClick: () => manager.setSort({ field: option.field, direction: option.direction }),
    }
  }), [manager])

  useEffect(() => {
    if (!active) return undefined
    return addNativeBackHandler(() => {
    if (deletePath) {
      setDeletePath(null)
      return true
    }
    if (manager.previewPath) {
      manager.closePreview()
      return true
    }
    if (entryMenuPath) {
      setEntryMenuPath(null)
      return true
    }
    if (sortMenuOpen) {
      setSortMenuOpen(false)
      return true
    }
    if (renamePath) {
      setRenamePath(null)
      setRenameName('')
      return true
    }
    if (newDirOpen) {
      manager.setNewDirName('')
      setNewDirOpen(false)
      return true
    }
    if (manager.selectionMode) {
      manager.setSelectionMode(false)
      return true
    }
    if (manager.clipboard) {
      manager.setClipboard(null)
      return true
    }
    const currentPath = normalizeFilePath(manager.currentPath)
    if (currentPath !== '/') {
      void manager.navigate(parentPath(currentPath))
      return true
    }
    return false
    }, 30)
  }, [
    active,
    deletePath,
    entryMenuPath,
    manager,
    manager.clipboard,
    manager.currentPath,
    manager.previewPath,
    manager.selectionMode,
    newDirOpen,
    renamePath,
    sortMenuOpen,
  ])

  return (
    <div
      className={`relative flex min-h-0 flex-col bg-white ${className || ''}`}
      data-machine-id={machineId}
      data-terminal-id={terminalId}
      data-testid="termx-file-manager"
    >
      {manager.selectionMode ? (
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-zinc-200/70 bg-white px-4">
          <button
            className="text-[15px] font-medium text-zinc-500 active:text-zinc-800"
            onClick={() => manager.setSelectionMode(false)}
          >
            Cancel
          </button>
          <div className="text-[16px] font-semibold text-zinc-900">
            {manager.selectedPaths.size} selected
          </div>
          <button
            className="text-[15px] font-medium text-blue-600 active:text-blue-800"
            onClick={() => {
              if (manager.selectedPaths.size === manager.visibleEntries.length) manager.deselectAll()
              else manager.selectAll()
            }}
          >
            {manager.selectedPaths.size === manager.visibleEntries.length ? 'Deselect All' : 'Select All'}
          </button>
        </header>
      ) : (
        <header className="shrink-0 border-b border-zinc-200/70 bg-white">
          <div className="flex h-11 min-w-0 items-center px-3">
            <div
              data-testid="termx-file-pathbar"
              className="flex h-9 min-w-0 flex-1 items-center gap-1 overflow-x-auto rounded-lg bg-zinc-50 px-2 text-[14px] font-medium text-zinc-600 no-scrollbar"
            >
              <HardDrive className="h-4 w-4 shrink-0 text-zinc-400" />
              {pathSegments.length === 0 ? (
                <span className="shrink-0 font-semibold text-zinc-900">/</span>
              ) : (
                <>
                  <button
                    onClick={() => void manager.navigate('/')}
                    className="shrink-0 rounded-md px-1.5 py-1 text-zinc-500 transition-colors active:bg-zinc-200"
                  >
                    /
                  </button>
                  {pathSegments.map((segment, index) => {
                    const isLast = index === pathSegments.length - 1
                    const path = '/' + pathSegments.slice(0, index + 1).join('/')
                    return (
                      <div key={`${path}:${index}`} className="flex shrink-0 items-center">
                        <ChevronRight className="h-3.5 w-3.5 shrink-0 text-zinc-300" />
                        {isLast ? (
                          <span className="px-1.5 py-1 font-semibold text-zinc-900">{segment}</span>
                        ) : (
                          <button
                            onClick={() => void manager.navigate(path)}
                            className="rounded-md px-1.5 py-1 text-zinc-500 transition-colors active:bg-zinc-200"
                          >
                            {segment}
                          </button>
                        )}
                      </div>
                    )
                  })}
                </>
              )}
            </div>
          </div>
          <div
            data-testid="termx-file-toolbar"
            className="flex h-11 items-center justify-end gap-1 overflow-x-auto border-t border-zinc-100 px-3 no-scrollbar"
          >
            <button
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-zinc-600 transition-colors active:scale-95 active:bg-zinc-100"
              type="button"
              onClick={() => manager.setSelectionMode(true)}
              aria-label="Select files"
            >
              <ListChecks className="h-5 w-5" />
            </button>
            <button
              className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg transition-colors active:scale-95 ${manager.showHidden ? 'bg-blue-50 text-blue-600' : 'text-zinc-600 active:bg-zinc-100'}`}
              type="button"
              onClick={manager.toggleShowHidden}
              aria-label={manager.showHidden ? 'Hide hidden files' : 'Show hidden files'}
            >
              {manager.showHidden ? <Eye className="h-5 w-5" /> : <EyeOff className="h-5 w-5" />}
            </button>
            <button
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-zinc-600 transition-colors active:scale-95 active:bg-zinc-100"
              type="button"
              onClick={() => setSortMenuOpen(true)}
              aria-label={`Sort files: ${sortLabel}`}
              title={`Sort files: ${sortLabel}`}
            >
              <ListFilter className="h-5 w-5" />
            </button>
            <button
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-zinc-600 transition-colors active:scale-95 active:bg-zinc-100"
              type="button"
              onClick={() => setNewDirOpen((current) => !current)}
              aria-label="New directory"
            >
              <Folder className="h-5 w-5" />
            </button>
            {fileTransfer ? (
              <>
                <button
                  className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-zinc-600 transition-colors active:scale-95 active:bg-zinc-100"
                  type="button"
                  aria-label="Upload files"
                  onClick={() => {
                    if (fileTransfer.isNative) {
                      fileTransfer.pickAndUpload?.(machineId, manager.currentPath || '/')
                      onOpenTransferCenter?.()
                    } else {
                      webUploadRef.current?.click()
                    }
                  }}
                >
                  <ArrowUpFromLine className="h-5 w-5" />
                </button>
                {!fileTransfer.isNative ? (
                  <input
                    ref={webUploadRef}
                    type="file"
                    multiple
                    className="hidden"
                    onChange={(e) => {
                      const files = e.target.files
                      if (!files) return
                      const picked = Array.from(files).map((f) => ({
                        uri: URL.createObjectURL(f),
                        name: f.name,
                        size: f.size,
                      }))
                      fileTransfer.startUpload(machineId, picked, manager.currentPath || '/')
                      onOpenTransferCenter?.()
                      e.target.value = ''
                    }}
                  />
                ) : null}
              </>
            ) : null}
            <button
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-zinc-600 transition-colors active:scale-95 active:bg-zinc-100 disabled:opacity-50"
              type="button"
              onClick={() => { void manager.refresh() }}
              disabled={manager.loading}
              aria-label="Refresh files"
            >
              <RefreshCw className={`h-5 w-5 ${manager.loading ? 'animate-spin' : ''}`} />
            </button>
          </div>
        </header>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto bg-white">
        {newDirOpen ? (
          <div className="mx-3 my-2 flex items-center gap-2 rounded-lg border border-zinc-200 bg-zinc-50 p-2">
            <Folder className="h-5 w-5 shrink-0 text-blue-500" />
            <input
              aria-label="Directory name"
              className="min-h-10 flex-1 bg-transparent px-2 text-[15px] font-medium text-zinc-900 outline-none placeholder:text-zinc-400"
              placeholder="Directory name"
              value={manager.newDirName}
              onChange={(event) => manager.setNewDirName(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') void manager.createDirectory().then(() => setNewDirOpen(false))
                if (event.key === 'Escape') {
                  manager.setNewDirName('')
                  setNewDirOpen(false)
                }
              }}
              autoFocus
            />
            <button
              type="button"
              aria-label="Create directory"
              className="flex h-9 w-9 items-center justify-center rounded-lg bg-blue-600 text-white disabled:bg-zinc-200 disabled:text-zinc-400"
              disabled={!manager.newDirName.trim() || manager.creatingDirectory}
              onClick={() => { void manager.createDirectory().then(() => setNewDirOpen(false)) }}
            >
              <Check className="h-4 w-4" />
            </button>
            <button
              type="button"
              aria-label="Cancel new directory"
              className="flex h-9 w-9 items-center justify-center rounded-lg bg-zinc-200 text-zinc-600"
              onClick={() => {
                manager.setNewDirName('')
                setNewDirOpen(false)
              }}
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        ) : null}

        {manager.error ? (
          <div className="m-2 flex items-start gap-3 rounded-xl border border-red-200/60 bg-red-50 p-4 text-[14px] text-red-800 shadow-sm" role="alert">
            <AlertCircle className="h-6 w-6 shrink-0 text-red-500" />
            <div>
               <h3 className="font-bold text-red-900">Directory Error</h3>
               <p className="mt-1">{manager.error.message}</p>
            </div>
          </div>
        ) : null}

        {manager.actionMessage ? (
          <div className="m-2 rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-2 text-[13px] font-medium text-emerald-800" role="status">
            {manager.actionMessage}
          </div>
        ) : null}

        {transferError ? (
          <div className="m-2 rounded-xl border border-red-200 bg-red-50 px-4 py-2 text-[13px] font-medium text-red-800" role="alert">
            {transferError}
          </div>
        ) : null}

        {manager.loading && manager.entries.length === 0 ? (
          <div className="flex h-40 flex-col items-center justify-center gap-3 text-[14px] font-medium text-zinc-500">
            <RefreshCw className="h-6 w-6 animate-spin text-zinc-400" />
            Loading directory...
          </div>
        ) : (
          <ul aria-label="Files" className="divide-y divide-zinc-100 pb-[120px]">
            {manager.entries.length === 0 && !manager.loading && !manager.error ? (
              <li className="flex h-32 flex-col items-center justify-center gap-3 rounded-xl border-2 border-dashed border-zinc-200 bg-zinc-50/50 text-[14px] font-medium text-zinc-500">
                <Folder className="h-8 w-8 text-zinc-300" />
                Directory is empty
              </li>
            ) : null}
            {manager.visibleEntries.map((entry) => {
              const entryPath = joinPath(manager.currentPath, entry.name)
              const isDirectory = entry.type === 'dir' || entry.type === 'symlink-dir'
              const Icon = isDirectory ? Folder : iconForFile(entry.name)
              const itemKey = uniqueFileListKey(entryKeyCounts, entryPath)
              const isRenaming = renamePath === entryPath
              const isSelected = manager.selectedPaths.has(entryPath)

              const handleItemClick = () => {
                if (manager.selectionMode) {
                  manager.toggleSelect(entryPath)
                } else {
                  if (isDirectory) void manager.navigate(entryPath)
                  else void manager.openPreview(entryPath)
                }
              }

              return (
                <li key={itemKey}>
                  <div
                    className={`group relative flex min-h-[3.25rem] w-full items-center gap-2.5 px-3 py-1.5 text-left transition-colors focus-within:ring-2 focus-within:ring-blue-500 active:bg-zinc-50 ${isSelected ? 'bg-blue-50/70' : ''}`}
                  >
                    {manager.selectionMode ? (
                      <div className="shrink-0 pr-1">
                        <div
                          className={`flex h-6 w-6 items-center justify-center rounded-full border-2 transition-colors ${isSelected ? 'border-blue-500 bg-blue-500' : 'border-zinc-300 bg-transparent'}`}
                        >
                          {isSelected ? <Check className="h-4 w-4 text-white" strokeWidth={3} /> : null}
                        </div>
                      </div>
                    ) : null}
                    <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-md transition-colors ${isDirectory ? 'bg-blue-50 group-active:bg-blue-100' : 'bg-zinc-50'}`}>
                      <Icon className={`h-5 w-5 ${isDirectory ? 'fill-blue-50 text-blue-500' : 'text-zinc-400'}`} />
                    </div>
                    <button
                      type="button"
                      aria-label={`${manager.selectionMode ? (isSelected ? 'Deselect' : 'Select') : isDirectory ? 'Open' : 'Preview'} ${entry.name}`}
                      className="flex min-w-0 flex-1 flex-col justify-center overflow-hidden text-left"
                      onClick={handleItemClick}
                    >
                      {isRenaming ? (
                        <input
                          aria-label="Rename entry"
                          className="min-h-9 rounded-lg border border-blue-200 bg-white px-2 text-[15px] font-medium text-zinc-900 outline-none"
                          value={renameName}
                          onClick={(event) => event.stopPropagation()}
                          onChange={(event) => setRenameName(event.currentTarget.value)}
                          onKeyDown={(event) => {
                            if (event.key === 'Enter') {
                              event.preventDefault()
                              void manager.renameEntry(entryPath, renameName).then(() => {
                                setRenamePath(null)
                                setRenameName('')
                              })
                            }
                            if (event.key === 'Escape') {
                              setRenamePath(null)
                              setRenameName('')
                            }
                          }}
                          autoFocus
                        />
                      ) : (
                        <span className={`truncate text-[15px] leading-5 ${isDirectory ? 'font-medium text-zinc-950' : 'font-medium text-zinc-800'}`}>
                          {entry.name}
                        </span>
                      )}
                      {!isDirectory && entry.size > 0 && !isRenaming ? (
                        <span className="truncate text-[12px] font-medium text-zinc-500">
                          {formatBytes(entry.size)}
                        </span>
                      ) : null}
                    </button>
                    {!manager.selectionMode ? (
                      <>
                        <div
                          data-testid="termx-file-row-actions"
                          className="ml-auto flex w-16 shrink-0 items-center justify-end gap-1"
                        >
                          <button
                            type="button"
                            aria-label={`More actions for ${entry.name}`}
                            className="flex h-8 w-8 items-center justify-center rounded-lg text-zinc-400 active:bg-zinc-100"
                            onClick={(event) => {
                              event.stopPropagation()
                              setEntryMenuPath((current) => current === entryPath ? null : entryPath)
                            }}
                          >
                            <MoreVertical className="h-5 w-5" />
                          </button>
                          {isDirectory ? <ChevronRight className="h-5 w-5 text-zinc-300 group-active:text-zinc-400" /> : null}
                        </div>
                      </>
                    ) : null}
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </div>
      {deletePath ? (
        <div className="absolute inset-0 z-50 flex items-end bg-black/40 p-3 pb-[calc(env(safe-area-inset-bottom)+0.75rem)] backdrop-blur-sm md:items-center md:justify-center" data-testid="termx-file-delete-confirm" onClick={() => setDeletePath(null)}>
          <section className="w-full rounded-2xl bg-white p-4 shadow-2xl md:max-w-sm" onClick={(event) => event.stopPropagation()}>
            <h2 className="text-[17px] font-bold text-zinc-950">Delete entry?</h2>
            <p className="mt-2 break-all text-sm text-zinc-500">{deletePath}</p>
            <div className="mt-4 grid grid-cols-2 gap-3">
              <button type="button" className="h-11 rounded-xl bg-zinc-100 text-sm font-semibold text-zinc-700" onClick={() => setDeletePath(null)}>
                Cancel
              </button>
              <button
                type="button"
                className="h-11 rounded-xl bg-red-600 text-sm font-semibold text-white"
                onClick={() => {
                  const target = deletePath
                  setDeletePath(null)
                  void manager.deleteEntry(target)
                }}
              >
                Delete
              </button>
            </div>
          </section>
        </div>
      ) : null}
      {manager.previewPath ? (
        <FilePreviewSheet
          preview={manager.preview}
          path={manager.previewPath}
          loading={manager.previewLoading}
          error={manager.previewError?.message ?? null}
          onClose={manager.closePreview}
        />
      ) : null}

      {/* Selection Mode Bottom Bar */}
      {manager.selectionMode && manager.selectedPaths.size > 0 ? (
        <div className="absolute bottom-0 left-0 right-0 z-40 bg-white/95 backdrop-blur-xl border-t border-zinc-200 pb-[env(safe-area-inset-bottom)]">
          <div className="flex h-[60px] items-stretch justify-around px-2">
            <button
              onClick={() => {
                manager.copy(Array.from(manager.selectedPaths))
                manager.setSelectionMode(false)
              }}
              className="flex flex-col items-center justify-center gap-1 px-3 text-zinc-600 hover:text-blue-600 active:bg-zinc-100 rounded-lg"
            >
              <File className="h-5 w-5" />
              <span className="text-[11px] font-medium">Copy</span>
            </button>
            <button
              onClick={() => {
                manager.cut(Array.from(manager.selectedPaths))
                manager.setSelectionMode(false)
              }}
              className="flex flex-col items-center justify-center gap-1 px-3 text-zinc-600 hover:text-blue-600 active:bg-zinc-100 rounded-lg"
            >
              <Folder className="h-5 w-5" />
              <span className="text-[11px] font-medium">Cut</span>
            </button>
            <button
              onClick={() => void manager.batchDelete(Array.from(manager.selectedPaths))}
              className="flex flex-col items-center justify-center gap-1 px-3 text-red-500 hover:text-red-700 active:bg-red-50 rounded-lg"
            >
              <Trash2 className="h-5 w-5" />
              <span className="text-[11px] font-medium">Delete</span>
            </button>
          </div>
        </div>
      ) : null}

      {/* Clipboard Paste Bar */}
      {!manager.selectionMode && manager.clipboard && manager.clipboard.paths.length > 0 ? (
        <div className="absolute bottom-0 left-0 right-0 z-40 bg-zinc-900/95 backdrop-blur-xl pb-[env(safe-area-inset-bottom)]">
          <div className="flex h-14 items-center justify-between px-4">
            <span className="text-sm font-medium text-zinc-300">
              {manager.clipboard.paths.length} {manager.clipboard.paths.length === 1 ? 'item' : 'items'} to {manager.clipboard.mode}
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={() => manager.setClipboard(null)}
                className="rounded-lg px-3 py-1.5 text-sm font-semibold text-zinc-400 hover:bg-zinc-800"
              >
                Cancel
              </button>
              <button
                onClick={() => void manager.paste()}
                className="rounded-lg bg-blue-600 px-4 py-1.5 text-sm font-semibold text-white hover:bg-blue-700"
              >
                Paste
              </button>
            </div>
          </div>
        </div>
      ) : null}

      <ActionSheet
        isOpen={!!entryMenuPath}
        onClose={() => setEntryMenuPath(null)}
        title={menuEntry?.name}
        subtitle={menuEntry ? (menuEntry.type === 'dir' ? 'Folder' : `${formatBytes(menuEntry.size)} · File`) : ''}
        actions={menuActions}
      />
      <ActionSheet
        isOpen={sortMenuOpen}
        onClose={() => setSortMenuOpen(false)}
        title="Sort files"
        subtitle={`${sortLabel} · Folders first`}
        actions={sortActions}
      />
    </div>
  )
}

interface FileSortOption extends FileSortState {
  label: string
  icon: ReactNode
}

const fileSortOptions: FileSortOption[] = [
  { field: 'name', direction: 'asc', label: 'Name A to Z', icon: <ArrowUpAZ className="h-5 w-5" /> },
  { field: 'name', direction: 'desc', label: 'Name Z to A', icon: <ArrowDownAZ className="h-5 w-5" /> },
  { field: 'modified', direction: 'desc', label: 'Newest first', icon: <Clock className="h-5 w-5" /> },
  { field: 'modified', direction: 'asc', label: 'Oldest first', icon: <Clock className="h-5 w-5" /> },
  { field: 'size', direction: 'desc', label: 'Largest first', icon: <File className="h-5 w-5" /> },
  { field: 'size', direction: 'asc', label: 'Smallest first', icon: <File className="h-5 w-5" /> },
  { field: 'type', direction: 'asc', label: 'Type A to Z', icon: <FileType className="h-5 w-5" /> },
  { field: 'type', direction: 'desc', label: 'Type Z to A', icon: <FileType className="h-5 w-5" /> },
]

function fileSortLabel(sort: FileSortState): string {
  const option = fileSortOptions.find((candidate) => (
    candidate.field === sort.field && candidate.direction === sort.direction
  ))
  return option?.label ?? 'Name A to Z'
}

interface FilePreviewSheetProps {
  path: string
  preview: FilePreviewResponse | null
  loading: boolean
  error: string | null
  onClose(): void
}

function FilePreviewSheet({ path, preview, loading, error, onClose }: FilePreviewSheetProps) {
  const title = preview?.name ?? basename(path)
  const subtitle = preview ? `${formatBytes(preview.size)} · ${preview.mimeType}` : path

  useEffect(() => {
    if (typeof document === 'undefined') return undefined
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = previousOverflow
    }
  }, [])

  const dialog = (
    <div
      className="fixed inset-0 z-[80] flex h-[100dvh] flex-col bg-white"
      data-testid="termx-file-preview"
      role="dialog"
      aria-modal="true"
      aria-labelledby="termx-file-preview-title"
    >
      <header className="flex shrink-0 items-center gap-3 border-b border-zinc-200/70 bg-white px-4 pb-2 pt-[calc(env(safe-area-inset-top)+0.5rem)] md:h-14 md:pb-0 md:pt-0">
        <div className="min-w-0 flex-1">
          <h2 id="termx-file-preview-title" className="truncate text-[17px] font-bold tracking-tight text-zinc-950">{title}</h2>
          <p className="mt-0.5 truncate text-[12px] font-medium text-zinc-500">{subtitle}</p>
        </div>
        <button
          type="button"
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-zinc-500 transition-colors active:scale-95 active:bg-zinc-100"
          aria-label="Close preview"
          onClick={onClose}
        >
          <X className="h-5 w-5" />
        </button>
      </header>
      <div className="min-h-0 flex-1 overflow-auto bg-zinc-50 pb-[env(safe-area-inset-bottom)]">
        {loading ? (
          <div className="flex h-56 flex-col items-center justify-center gap-3 text-[14px] font-medium text-zinc-500">
            <RefreshCw className="h-6 w-6 animate-spin text-zinc-400" />
            Loading preview...
          </div>
        ) : error ? (
          <PreviewNotice title="Preview Error" message={error} />
        ) : preview ? (
          <PreviewContent preview={preview} />
        ) : null}
      </div>
    </div>
  )

  if (typeof document === 'undefined') return dialog
  return createPortal(dialog, document.body)
}

function PreviewContent({ preview }: { preview: FilePreviewResponse }) {
  if (preview.category === 'image' && preview.contentBase64) {
    return <BinaryImagePreview preview={preview} />
  }
  if (preview.category === 'video' && preview.contentBase64) {
    return <BinaryVideoPreview preview={preview} />
  }
  if (preview.category === 'text' && preview.content !== undefined) {
    if (isMarkdownFile(preview.name, preview.mimeType)) {
      return <MarkdownPreview text={preview.content} />
    }
    return <TextPreview text={preview.content} name={preview.name} mimeType={preview.mimeType} />
  }
  const limit = preview.previewLimit && preview.previewLimit > 0 ? formatBytes(preview.previewLimit) : ''
  const message = preview.category === 'unsupported'
    ? 'This file type is not available for inline preview.'
    : `This file is too large to preview${limit ? ` within the ${limit} limit` : ''}.`
  return <PreviewNotice title="No Preview" message={message} />
}

function BinaryImagePreview({ preview }: { preview: FilePreviewResponse }) {
  const src = useBinaryPreviewUrl(preview.contentBase64, preview.mimeType)
  if (src === undefined) return null
  if (!src) return <PreviewNotice title="Preview Error" message="Image preview data is invalid." />
  return (
    <div className="flex min-h-full items-center justify-center p-3">
      <img
        alt={preview.name}
        className="max-h-full max-w-full rounded-lg object-contain shadow-sm"
        src={src}
      />
    </div>
  )
}

function BinaryVideoPreview({ preview }: { preview: FilePreviewResponse }) {
  const src = useBinaryPreviewUrl(preview.contentBase64, preview.mimeType)
  if (src === undefined) return null
  if (!src) return <PreviewNotice title="Preview Error" message="Video preview data is invalid." />
  return (
    <div className="flex min-h-full items-center justify-center bg-black p-2">
      <video
        className="max-h-full max-w-full"
        controls
        preload="metadata"
        src={src}
      />
    </div>
  )
}

function useBinaryPreviewUrl(contentBase64: string | undefined, mimeType: string): string | null | undefined {
  const [url, setUrl] = useState<string | null | undefined>(undefined)

  useEffect(() => {
    if (!contentBase64) {
      setUrl(null)
      return undefined
    }
    const src = binaryPreviewUrl(contentBase64, mimeType)
    setUrl(src)
    return () => {
      if (src?.startsWith('blob:')) URL.revokeObjectURL(src)
    }
  }, [contentBase64, mimeType])

  return url
}

function binaryPreviewUrl(contentBase64: string, mimeType: string): string | null {
  const trimmed = contentBase64.trim()
  if (trimmed.startsWith('data:')) return trimmed
  try {
    const binary = atob(trimmed)
    const bytes = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i)
    const blob = new Blob([bytes], { type: mimeType || 'application/octet-stream' })
    return URL.createObjectURL(blob)
  } catch {
    return null
  }
}

function TextPreview({ text, name, mimeType }: { text: string; name: string; mimeType: string }) {
  const language = detectCodeLanguage(name, mimeType)
  const languageId = language?.id
  const isCode = !!languageId
  const [softWrap, setSoftWrap] = useState(() => !isCode)
  const lines = useMemo(() => normalizePreviewText(text).split('\n'), [text])
  const highlightedLines = useMemo(
    () => languageId ? lines.map((line) => highlightCodeLine(line, languageId)) : [],
    [languageId, lines],
  )

  useEffect(() => {
    setSoftWrap(!isCode)
  }, [isCode, name, mimeType])

  const wrapLabel = softWrap ? 'Disable line wrap' : 'Enable line wrap'

  return (
    <div className="flex min-h-full flex-col bg-white">
      <div className="sticky top-0 z-10 flex min-h-11 shrink-0 items-center justify-between gap-2 border-b border-zinc-200/70 bg-white px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          {isCode ? <Code2 className="h-4 w-4 shrink-0 text-zinc-500" /> : <FileText className="h-4 w-4 shrink-0 text-zinc-500" />}
          <span className="truncate text-[12px] font-semibold uppercase tracking-wide text-zinc-500">
            {language?.label ?? 'Plain text'}
          </span>
        </div>
        <button
          type="button"
          aria-label={wrapLabel}
          title={wrapLabel}
          className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg transition-colors active:scale-95 ${softWrap ? 'bg-blue-50 text-blue-600' : 'text-zinc-600 active:bg-zinc-100'}`}
          onClick={() => setSoftWrap((current) => !current)}
        >
          <WrapText className="h-4 w-4" />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto bg-white">
        <div className={`py-2 font-mono text-[12px] leading-5 text-zinc-900 ${softWrap ? 'min-w-0' : 'w-max min-w-full'}`}>
          {lines.map((line, index) => (
            <div
              key={`line-${index}`}
              className={`grid min-h-5 ${softWrap ? 'grid-cols-[3.25rem_minmax(0,1fr)]' : 'grid-cols-[3.25rem_max-content]'}`}
            >
              <span className="select-none border-r border-zinc-100 pr-2 text-right text-[11px] leading-5 text-zinc-400">
                {index + 1}
              </span>
              <code
                data-testid={`termx-file-preview-line-${index + 1}`}
                className={`hljs block bg-transparent px-3 text-[12px] leading-5 ${softWrap ? 'whitespace-pre-wrap break-words' : 'whitespace-pre'}`}
                {...(isCode
                  ? { dangerouslySetInnerHTML: { __html: highlightedLines[index] ?? '' } }
                  : { children: line })}
              />
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function MarkdownPreview({ text }: { text: string }) {
  return (
    <article className="min-h-full bg-white px-4 py-5 text-[15px] leading-7 text-zinc-800">
      {renderMarkdownBlocks(text)}
    </article>
  )
}

function PreviewNotice({ title, message }: { title: string; message: string }) {
  return (
    <div className="flex h-56 flex-col items-center justify-center gap-3 px-6 text-center">
      <AlertCircle className="h-7 w-7 text-zinc-400" />
      <h3 className="text-[16px] font-bold text-zinc-900">{title}</h3>
      <p className="max-w-sm text-[14px] leading-6 text-zinc-500">{message}</p>
    </div>
  )
}

interface CodeLanguage {
  id: string
  label: string
}

const codeLanguageByExtension: Record<string, CodeLanguage> = {
  bash: { id: 'bash', label: 'Shell' },
  c: { id: 'cpp', label: 'C' },
  cc: { id: 'cpp', label: 'C++' },
  cpp: { id: 'cpp', label: 'C++' },
  cs: { id: 'java', label: 'C#' },
  css: { id: 'css', label: 'CSS' },
  go: { id: 'go', label: 'Go' },
  h: { id: 'cpp', label: 'C/C++' },
  hpp: { id: 'cpp', label: 'C++' },
  html: { id: 'xml', label: 'HTML' },
  java: { id: 'java', label: 'Java' },
  js: { id: 'javascript', label: 'JavaScript' },
  json: { id: 'json', label: 'JSON' },
  jsx: { id: 'javascript', label: 'JSX' },
  kt: { id: 'java', label: 'Kotlin' },
  mjs: { id: 'javascript', label: 'JavaScript' },
  py: { id: 'python', label: 'Python' },
  rb: { id: 'python', label: 'Ruby' },
  rs: { id: 'rust', label: 'Rust' },
  scss: { id: 'css', label: 'SCSS' },
  sh: { id: 'bash', label: 'Shell' },
  sql: { id: 'sql', label: 'SQL' },
  ts: { id: 'typescript', label: 'TypeScript' },
  tsx: { id: 'typescript', label: 'TSX' },
  xml: { id: 'xml', label: 'XML' },
  yaml: { id: 'yaml', label: 'YAML' },
  yml: { id: 'yaml', label: 'YAML' },
  zsh: { id: 'bash', label: 'Shell' },
}

function normalizePreviewText(text: string): string {
  return text.replace(/\r\n?/g, '\n')
}

function detectCodeLanguage(name: string, mimeType: string): CodeLanguage | null {
  const ext = extension(name)
  const language = codeLanguageByExtension[ext]
  if (language) return language
  if (/typescript/i.test(mimeType)) return { id: 'typescript', label: 'TypeScript' }
  if (/javascript|ecmascript/i.test(mimeType)) return { id: 'javascript', label: 'JavaScript' }
  if (/json/i.test(mimeType)) return { id: 'json', label: 'JSON' }
  if (/html|xml/i.test(mimeType)) return { id: 'xml', label: mimeType.includes('xml') ? 'XML' : 'HTML' }
  if (/css/i.test(mimeType)) return { id: 'css', label: 'CSS' }
  if (/x-python/i.test(mimeType)) return { id: 'python', label: 'Python' }
  if (/x-sh|shellscript/i.test(mimeType)) return { id: 'bash', label: 'Shell' }
  return null
}

function highlightCodeLine(line: string, language: string): string {
  return hljs.highlight(line || ' ', {
    language,
    ignoreIllegals: true,
  }).value
}

function renderMarkdownBlocks(text: string) {
  const lines = text.replace(/\r\n?/g, '\n').split('\n')
  const blocks: ReactNode[] = []
  let index = 0
  let key = 0

  while (index < lines.length) {
    const line = lines[index] ?? ''
    if (!line.trim()) {
      index += 1
      continue
    }

    const fence = line.match(/^```(\S*)\s*$/)
    if (fence) {
      const code: string[] = []
      index += 1
      while (index < lines.length && !/^```\s*$/.test(lines[index] ?? '')) {
        code.push(lines[index] ?? '')
        index += 1
      }
      if (index < lines.length) index += 1
      blocks.push(
        <pre key={`code-${key++}`} className="my-4 overflow-x-auto rounded-lg bg-zinc-950 p-3 font-mono text-[12px] leading-5 text-zinc-100">
          <code>{code.join('\n')}</code>
        </pre>,
      )
      continue
    }

    const heading = line.match(/^(#{1,3})\s+(.+)$/)
    if (heading) {
      const level = heading[1]?.length ?? 1
      const content = renderInlineMarkdown(heading[2] ?? '', `h-${key}`)
      if (level === 1) {
        blocks.push(<h1 key={`h-${key++}`} className="mt-1 break-words text-[22px] font-bold leading-8 text-zinc-950">{content}</h1>)
      } else if (level === 2) {
        blocks.push(<h2 key={`h-${key++}`} className="mt-5 break-words text-[18px] font-bold leading-7 text-zinc-950">{content}</h2>)
      } else {
        blocks.push(<h3 key={`h-${key++}`} className="mt-4 break-words text-[16px] font-bold leading-7 text-zinc-900">{content}</h3>)
      }
      index += 1
      continue
    }

    if (/^\s*[-*]\s+/.test(line)) {
      const items: ReactNode[] = []
      while (index < lines.length && /^\s*[-*]\s+/.test(lines[index] ?? '')) {
        const itemText = (lines[index] ?? '').replace(/^\s*[-*]\s+/, '')
        items.push(<li key={`li-${key}-${items.length}`} className="break-words">{renderInlineMarkdown(itemText, `li-${key}-${items.length}`)}</li>)
        index += 1
      }
      blocks.push(<ul key={`ul-${key++}`} className="my-3 list-disc space-y-1 pl-5">{items}</ul>)
      continue
    }

    if (/^\s*\d+\.\s+/.test(line)) {
      const items: ReactNode[] = []
      while (index < lines.length && /^\s*\d+\.\s+/.test(lines[index] ?? '')) {
        const itemText = (lines[index] ?? '').replace(/^\s*\d+\.\s+/, '')
        items.push(<li key={`oli-${key}-${items.length}`} className="break-words">{renderInlineMarkdown(itemText, `oli-${key}-${items.length}`)}</li>)
        index += 1
      }
      blocks.push(<ol key={`ol-${key++}`} className="my-3 list-decimal space-y-1 pl-5">{items}</ol>)
      continue
    }

    if (/^>\s?/.test(line)) {
      const quote: string[] = []
      while (index < lines.length && /^>\s?/.test(lines[index] ?? '')) {
        quote.push((lines[index] ?? '').replace(/^>\s?/, ''))
        index += 1
      }
      blocks.push(
        <blockquote key={`quote-${key++}`} className="my-3 border-l-4 border-zinc-300 pl-3 text-zinc-600">
          {renderInlineMarkdown(quote.join(' '), `quote-${key}`)}
        </blockquote>,
      )
      continue
    }

    const paragraph = [line.trim()]
    index += 1
    while (index < lines.length && (lines[index] ?? '').trim() && !isMarkdownBlockStart(lines[index] ?? '')) {
      paragraph.push((lines[index] ?? '').trim())
      index += 1
    }
    blocks.push(
      <p key={`p-${key++}`} className="my-3 break-words">
        {renderInlineMarkdown(paragraph.join(' '), `p-${key}`)}
      </p>,
    )
  }

  return blocks.length > 0 ? blocks : [<p key="empty" className="text-zinc-500">Empty file</p>]
}

function renderInlineMarkdown(text: string, keyPrefix: string) {
  const parts: ReactNode[] = []
  const tokenPattern = /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\)|\*[^*]+\*)/g
  let lastIndex = 0
  let partIndex = 0
  for (const match of text.matchAll(tokenPattern)) {
    const token = match[0]
    const index = match.index ?? 0
    if (index > lastIndex) parts.push(text.slice(lastIndex, index))
    if (token.startsWith('`') && token.endsWith('`')) {
      parts.push(<code key={`${keyPrefix}-code-${partIndex++}`} className="rounded bg-zinc-100 px-1 py-0.5 font-mono text-[0.9em] text-zinc-900">{token.slice(1, -1)}</code>)
    } else if (token.startsWith('**') && token.endsWith('**')) {
      parts.push(<strong key={`${keyPrefix}-strong-${partIndex++}`} className="font-bold text-zinc-950">{token.slice(2, -2)}</strong>)
    } else if (token.startsWith('*') && token.endsWith('*')) {
      parts.push(<em key={`${keyPrefix}-em-${partIndex++}`}>{token.slice(1, -1)}</em>)
    } else {
      const link = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/)
      const href = link?.[2] ?? ''
      if (isSafeLink(href)) {
        parts.push(
          <a key={`${keyPrefix}-link-${partIndex++}`} className="break-all font-semibold text-blue-600 underline" href={href} target="_blank" rel="noreferrer">
            {link?.[1] ?? href}
          </a>,
        )
      } else {
        parts.push(link?.[1] ?? token)
      }
    }
    lastIndex = index + token.length
  }
  if (lastIndex < text.length) parts.push(text.slice(lastIndex))
  return parts
}

function isMarkdownBlockStart(line: string): boolean {
  return /^```/.test(line) ||
    /^#{1,3}\s+/.test(line) ||
    /^\s*[-*]\s+/.test(line) ||
    /^\s*\d+\.\s+/.test(line) ||
    /^>\s?/.test(line)
}

function isSafeLink(href: string): boolean {
  return /^(https?:|mailto:|#|\/)/i.test(href)
}

function iconForFile(name: string) {
  const ext = extension(name)
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'bmp', 'ico', 'avif'].includes(ext)) return Image
  if (['mp4', 'webm', 'mov', 'm4v', 'ogv', 'ogg'].includes(ext)) return PlaySquare
  if (isMarkdownFile(name, '')) return FileText
  if (['js', 'ts', 'jsx', 'tsx', 'go', 'py', 'rs', 'java', 'c', 'cpp', 'h', 'hpp', 'css', 'html', 'json', 'yaml', 'yml', 'toml', 'xml', 'sh', 'sql'].includes(ext)) return Code2
  return File
}

function isMarkdownFile(name: string, mimeType: string): boolean {
  const ext = extension(name)
  return mimeType === 'text/markdown' || ext === 'md' || ext === 'markdown' || ext === 'mdx'
}

function extension(name: string): string {
  const base = basename(name).toLowerCase()
  const dot = base.lastIndexOf('.')
  return dot >= 0 ? base.slice(dot + 1) : base
}

function uniqueFileListKey(counts: Map<string, number>, baseKey: string): string {
  const count = counts.get(baseKey) ?? 0
  counts.set(baseKey, count + 1)
  return count === 0 ? baseKey : `${baseKey}:${count}`
}

function joinPath(base: string, name: string): string {
  if (!base || base === '/') return `/${name}`
  return `${base.replace(/\/+$/, '')}/${name}`
}

function normalizeFilePath(path: string): string {
  const trimmed = path.trim()
  if (!trimmed || trimmed === '/') return '/'
  return trimmed.replace(/\/+$/, '') || '/'
}

function parentPath(path: string): string {
  const normalized = normalizeFilePath(path)
  if (normalized === '/') return '/'
  const index = normalized.lastIndexOf('/')
  if (index <= 0) return '/'
  return normalized.slice(0, index)
}

function basename(path: string): string {
  const normalized = path.replace(/\/+$/, '')
  return normalized.slice(normalized.lastIndexOf('/') + 1) || normalized
}

function formatBytes(bytes: number, decimals = 1) {
  if (!+bytes) return '0 B'
  const k = 1024
  const dm = decimals < 0 ? 0 : decimals
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`
}
