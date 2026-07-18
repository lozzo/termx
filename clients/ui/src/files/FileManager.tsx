import { useFileManager, type FileSortState } from './useFileManager'
import type { FileTransferContext } from './fileApi'
import { extension, fileEntryMenuSubtitle, fileEntryMeta, isMarkdownFile, joinPath, normalizeFilePath, parentPath } from './fileUtils'
import { isModelPreviewFile } from './modelFileTypes'
import { FilePreviewSheet } from './preview/FilePreviewSheet'
import type { RtcSession } from '../core/transport'
import type { ProtoClientSession } from '../core/protoClientSession'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { PathBookmark } from './pathBookmarks'
import 'highlight.js/styles/github.css'
import { addNativeBackHandler } from '../platform/nativeBack'
import { hapticImpact, hapticSelection } from '../platform/haptics'
import { ActionSheet, type ActionSheetItem } from '../ui/ActionSheet'
import { AlertCircle, ArrowDownAZ, ArrowDownToLine, ArrowUpAZ, ArrowUpFromLine, Bookmark, BookmarkMinus, BookmarkPlus, Box, Check, ChevronRight, ClipboardCopy, Clock, Code2, Eye, EyeOff, File, FileText, FileType, Folder, FolderBookmark, HardDrive, Image, ListChecks, ListFilter, MoreVertical, PlaySquare, RefreshCw, SquarePen, Trash2, X } from 'lucide-react'

export interface FileManagerProps {
  machineId: string
  terminalId?: string | undefined
  session: Pick<RtcSession, 'openApi' | 'openFileChannel' | 'getConnectionInfo'> | ProtoClientSession
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
  const [bookmarksOpen, setBookmarksOpen] = useState(false)
  const [editingBookmarkId, setEditingBookmarkId] = useState<string | null>(null)
  const [bookmarkAlias, setBookmarkAlias] = useState('')
  const [transferError, setTransferError] = useState<string | null>(null)
  const pathBarRef = useRef<HTMLDivElement>(null)
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
            fileTransfer.startDownload(
              machineId,
              menuEntry.name,
              menuEntry.size,
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
      label: 'Copy Path',
      icon: <ClipboardCopy className="h-5 w-5" />,
      onClick: () => { hapticImpact(); void manager.copyFilePaths([entryMenuPath]) },
    })

    actions.push({
      label: 'Copy',
      icon: <File className="h-5 w-5" />,
      onClick: () => { hapticImpact(); manager.copy([entryMenuPath]) },
    })

    actions.push({
      label: 'Cut',
      icon: <Folder className="h-5 w-5" />,
      onClick: () => { hapticImpact(); manager.cut([entryMenuPath]) },
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
    if (bookmarksOpen) {
      setBookmarksOpen(false)
      setEditingBookmarkId(null)
      setBookmarkAlias('')
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
    bookmarksOpen,
  ])

  const editingBookmark = useMemo(() => (
    editingBookmarkId ? manager.pathBookmarks.find((bookmark) => bookmark.id === editingBookmarkId) ?? null : null
  ), [editingBookmarkId, manager.pathBookmarks])

  const openBookmarkEditor = (bookmark: PathBookmark) => {
    setEditingBookmarkId(bookmark.id)
    setBookmarkAlias(bookmark.label)
  }

  const closeBookmarkEditor = () => {
    setEditingBookmarkId(null)
    setBookmarkAlias('')
  }

  const saveBookmarkAlias = () => {
    if (!editingBookmark) return
    const label = bookmarkAlias.trim()
    hapticImpact()
    void manager.updatePathBookmark(editingBookmark.id, { label }).then(closeBookmarkEditor)
  }

  useEffect(() => {
    const pathBar = pathBarRef.current
    if (!pathBar) return undefined
    const frame = window.requestAnimationFrame(() => {
      pathBar.scrollLeft = pathBar.scrollWidth
    })
    return () => window.cancelAnimationFrame(frame)
  }, [manager.currentPath])

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
            className="text-[15px] font-medium text-zinc-500 hover:text-zinc-700 active:text-zinc-800"
            onClick={() => { hapticSelection(); manager.setSelectionMode(false) }}
          >
            Cancel
          </button>
          <div className="text-[16px] font-semibold text-zinc-900">
            {manager.selectedPaths.size} selected
          </div>
          <button
            className="text-[15px] font-medium text-blue-600 hover:text-blue-700 active:text-blue-800"
            onClick={() => {
              hapticSelection()
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
              ref={pathBarRef}
              data-testid="termx-file-pathbar"
              className="flex h-11 min-w-0 flex-1 items-center gap-1 overflow-x-auto border border-[var(--termx-app-line)] bg-zinc-50 px-2 text-[14px] font-medium text-zinc-600 no-scrollbar"
            >
              <HardDrive className="h-4 w-4 shrink-0 text-zinc-400" />
              {pathSegments.length === 0 ? (
                <span className="shrink-0 font-semibold text-zinc-900">/</span>
              ) : (
                <>
                  <button
                    onClick={() => { hapticSelection(); void manager.navigate('/') }}
                    className="shrink-0 px-1.5 py-1 text-zinc-500 transition-colors hover:bg-zinc-100 active:bg-zinc-200"
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
                            onClick={() => { hapticSelection(); void manager.navigate(path) }}
                            className="px-1.5 py-1 text-zinc-500 transition-colors hover:bg-zinc-100 active:bg-zinc-200"
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
            <button
              type="button"
              aria-label="Path bookmarks"
              title="Path bookmarks"
              className="ml-2 flex h-11 w-11 shrink-0 items-center justify-center text-zinc-500 transition-colors hover:bg-zinc-50 active:bg-zinc-100"
              onClick={() => {
                hapticSelection()
                setBookmarksOpen(true)
                void manager.refreshPathBookmarks()
              }}
            >
              <FolderBookmark className="h-4 w-4" />
            </button>
            <button
              type="button"
              aria-label="Copy current directory path"
              title="Copy current directory path"
              className="ml-1 flex h-11 w-11 shrink-0 items-center justify-center text-zinc-500 transition-colors hover:bg-zinc-50 active:bg-zinc-100"
              onClick={() => { hapticImpact(); void manager.copyFilePaths([manager.currentPath || '/']) }}
            >
              <ClipboardCopy className="h-4 w-4" />
            </button>
          </div>
        </header>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto bg-white">
        {newDirOpen ? (
          <div className="mx-3 my-2 flex items-center gap-2 border border-[var(--termx-app-line)] bg-zinc-50 p-2">
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
              className="termx-app-primary-button disabled:bg-zinc-200 disabled:text-zinc-400"
              disabled={!manager.newDirName.trim() || manager.creatingDirectory}
              onClick={() => { hapticImpact(); void manager.createDirectory().then(() => setNewDirOpen(false)) }}
            >
              <Check className="h-4 w-4" />
            </button>
            <button
              type="button"
              aria-label="Cancel new directory"
              className="termx-app-secondary-button bg-zinc-200 text-zinc-600"
              onClick={() => {
                manager.setNewDirName('')
                setNewDirOpen(false)
              }}
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        ) : null}

        {manager.pathBookmarkError ? (
          <div className="m-2 border border-amber-200 bg-amber-50 px-4 py-2 text-[13px] font-medium text-amber-800" role="alert">
            {manager.pathBookmarkError}
          </div>
        ) : null}

        {manager.error ? (
          <div className="m-2 flex items-start gap-3 border border-red-200 bg-red-50 p-4 text-[14px] text-red-800" role="alert">
            <AlertCircle className="h-6 w-6 shrink-0 text-red-500" />
            <div>
               <h3 className="font-bold text-red-900">Directory Error</h3>
               <p className="mt-1">{manager.error.message}</p>
            </div>
          </div>
        ) : null}

        {manager.actionMessage ? (
          <div className="m-2 border border-emerald-200 bg-emerald-50 px-4 py-2 text-[13px] font-medium text-emerald-800" role="status">
            {manager.actionMessage}
          </div>
        ) : null}

        {transferError ? (
          <div className="m-2 border border-red-200 bg-red-50 px-4 py-2 text-[13px] font-medium text-red-800" role="alert">
            {transferError}
          </div>
        ) : null}

        {manager.loading && manager.entries.length === 0 && !manager.error ? (
          <div className="flex h-40 flex-col items-center justify-center gap-3 text-[14px] font-medium text-zinc-500">
            <span className="termx-square-spinner h-6 w-6 text-zinc-500" aria-hidden="true" />
            Loading directory...
          </div>
        ) : (
          <ul aria-label="Files" className="divide-y divide-zinc-100 pb-[120px]">
            {manager.entries.length === 0 && !manager.loading && !manager.error ? (
              <li className="flex h-32 flex-col items-center justify-center gap-3 border-y border-dashed border-zinc-300 bg-zinc-50/50 text-[14px] font-medium text-zinc-500">
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
                  hapticSelection()
                  manager.toggleSelect(entryPath)
                } else {
                  hapticSelection()
                  if (isDirectory) void manager.navigate(entryPath)
                  else void manager.openPreview(entryPath)
                }
              }

              return (
                <li key={itemKey}>
                  <div
                    className={`group relative flex min-h-[3.25rem] w-full items-center gap-2.5 px-3 py-1.5 text-left transition-colors focus-within:ring-2 focus-within:ring-blue-500 hover:bg-zinc-50 active:bg-zinc-50 ${isSelected ? 'bg-blue-50/70' : ''}`}
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
                    <div className={`flex h-9 w-9 shrink-0 items-center justify-center border border-[var(--termx-app-line)] transition-colors ${isDirectory ? 'bg-blue-50 group-hover:bg-blue-50/80 group-active:bg-blue-100' : 'bg-zinc-50'}`}>
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
                          className="min-h-11 border border-blue-200 bg-white px-2 text-[15px] font-medium text-zinc-900 outline-none"
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
                      {!isRenaming ? (
                        <span className="truncate text-[12px] font-medium text-zinc-500">
                          {fileEntryMeta(entry)}
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
                            className="flex h-11 w-11 items-center justify-center text-zinc-400 hover:bg-zinc-50 active:bg-zinc-100"
                            onClick={(event) => {
                              event.stopPropagation()
                              hapticSelection()
                              setEntryMenuPath((current) => current === entryPath ? null : entryPath)
                            }}
                          >
                            <MoreVertical className="h-5 w-5" />
                          </button>
                          {isDirectory ? <ChevronRight className="h-5 w-5 text-zinc-300 group-active:text-zinc-400" /> : <div className="h-5 w-5" />}
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
        <div className="absolute inset-0 z-50 flex items-end bg-black/40 p-3 pb-[calc(env(safe-area-inset-bottom)+0.75rem)] backdrop-blur-sm md:items-center md:justify-center" data-testid="termx-file-delete-confirm" onClick={() => { hapticSelection(); setDeletePath(null) }}>
          <section className="termx-app-panel w-full p-4 md:max-w-sm" onClick={(event) => event.stopPropagation()}>
            <h2 className="text-[17px] font-bold text-zinc-950">Delete entry?</h2>
            <p className="mt-2 break-all text-sm text-zinc-500">{deletePath}</p>
            <div className="mt-4 grid grid-cols-2 gap-3">
              <button type="button" className="termx-app-secondary-button h-11 text-sm font-semibold" onClick={() => { hapticSelection(); setDeletePath(null) }}>
                Cancel
              </button>
              <button
                type="button"
                className="h-11 border border-red-600 bg-red-600 text-sm font-semibold text-white"
                onClick={() => {
                  hapticImpact()
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
          streamPreview={manager.streamPreview}
          onClose={manager.closePreview}
        />
      ) : null}

      {/* Default Bottom Toolbar */}
      {!manager.selectionMode && (!manager.clipboard || manager.clipboard.paths.length === 0) ? (
        <div
          className="absolute bottom-0 left-0 right-0 z-40 bg-white/95 backdrop-blur-xl border-t border-zinc-200 pb-[env(safe-area-inset-bottom)]"
          data-testid="termx-file-toolbar"
        >
          <div className="flex h-[60px] items-center justify-around px-2">
            <button
              className="flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 text-zinc-600 hover:bg-zinc-50 hover:text-blue-600 active:bg-zinc-100"
              type="button"
              onClick={() => { hapticImpact(); manager.setSelectionMode(true) }}
              aria-label="Select files"
            >
              <ListChecks className="h-5 w-5" />
              <span className="text-[11px] font-medium">Select</span>
            </button>
            <button
              className={`flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 ${manager.showHidden ? 'bg-blue-50 text-blue-600' : 'text-zinc-600 hover:bg-zinc-50 hover:text-blue-600 active:bg-zinc-100'}`}
              type="button"
              onClick={() => { hapticSelection(); manager.toggleShowHidden() }}
              aria-label={manager.showHidden ? 'Hide hidden files' : 'Show hidden files'}
            >
              {manager.showHidden ? <Eye className="h-5 w-5" /> : <EyeOff className="h-5 w-5" />}
              <span className="text-[11px] font-medium">Hidden</span>
            </button>
            <button
              className="flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 text-zinc-600 hover:bg-zinc-50 hover:text-blue-600 active:bg-zinc-100"
              type="button"
              onClick={() => { hapticSelection(); setSortMenuOpen(true) }}
              aria-label={`Sort files: ${sortLabel}`}
              title={`Sort files: ${sortLabel}`}
            >
              <ListFilter className="h-5 w-5" />
              <span className="text-[11px] font-medium">Sort</span>
            </button>
            <button
              className="flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 text-zinc-600 hover:bg-zinc-50 hover:text-blue-600 active:bg-zinc-100"
              type="button"
              onClick={() => { hapticSelection(); setNewDirOpen((current) => !current) }}
              aria-label="New directory"
            >
              <Folder className="h-5 w-5" />
              <span className="text-[11px] font-medium">New</span>
            </button>
            {fileTransfer ? (
              <>
                <button
                  className="flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 text-zinc-600 hover:bg-zinc-50 hover:text-blue-600 active:bg-zinc-100"
                  type="button"
                  aria-label="Upload files"
                  onClick={() => {
                    hapticImpact()
                    if (fileTransfer.isNative) {
                      fileTransfer.pickAndUpload?.(machineId, manager.currentPath || '/')
                      onOpenTransferCenter?.()
                    } else {
                      webUploadRef.current?.click()
                    }
                  }}
                >
                  <ArrowUpFromLine className="h-5 w-5" />
                  <span className="text-[11px] font-medium">Upload</span>
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
              className="flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 text-zinc-600 hover:bg-zinc-50 hover:text-blue-600 active:bg-zinc-100 disabled:opacity-50"
              type="button"
              onClick={() => { hapticImpact(); void manager.refresh() }}
              disabled={manager.loading}
              aria-label="Refresh files"
            >
              <RefreshCw className={`h-5 w-5 ${manager.loading ? 'animate-spin' : ''}`} />
              <span className="text-[11px] font-medium">Refresh</span>
            </button>
          </div>
        </div>
      ) : null}

      {/* Selection Mode Bottom Bar */}
      {manager.selectionMode && manager.selectedPaths.size > 0 ? (
        <div className="absolute bottom-0 left-0 right-0 z-40 bg-white/95 backdrop-blur-xl border-t border-zinc-200 pb-[env(safe-area-inset-bottom)]">
          <div className="flex h-[60px] items-stretch justify-around px-2">
            <button
              onClick={() => {
                hapticImpact()
                manager.copy(Array.from(manager.selectedPaths))
                manager.setSelectionMode(false)
              }}
              className="flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 text-zinc-600 hover:bg-zinc-50 hover:text-blue-600 active:bg-zinc-100"
            >
              <File className="h-5 w-5" />
              <span className="text-[11px] font-medium">Copy</span>
            </button>
            <button
              onClick={() => {
                hapticImpact()
                void manager.copyFilePaths(Array.from(manager.selectedPaths)).then(() => manager.setSelectionMode(false))
              }}
              className="flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 text-zinc-600 hover:bg-zinc-50 hover:text-blue-600 active:bg-zinc-100"
            >
              <ClipboardCopy className="h-5 w-5" />
              <span className="text-[11px] font-medium">Path</span>
            </button>
            <button
              onClick={() => {
                hapticImpact()
                manager.cut(Array.from(manager.selectedPaths))
                manager.setSelectionMode(false)
              }}
              className="flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 text-zinc-600 hover:bg-zinc-50 hover:text-blue-600 active:bg-zinc-100"
            >
              <Folder className="h-5 w-5" />
              <span className="text-[11px] font-medium">Cut</span>
            </button>
            <button
              onClick={() => { hapticImpact(); void manager.batchDelete(Array.from(manager.selectedPaths)) }}
              className="flex min-h-11 min-w-11 flex-col items-center justify-center gap-1 px-2 text-red-500 hover:bg-red-50/80 hover:text-red-700 active:bg-red-50"
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
                onClick={() => { hapticSelection(); manager.setClipboard(null) }}
                className="min-h-11 border border-zinc-700 px-3 py-1.5 text-sm font-semibold text-zinc-300 hover:bg-zinc-800"
              >
                Cancel
              </button>
              <button
                onClick={() => { hapticImpact(); void manager.paste() }}
                className="min-h-11 bg-blue-600 px-4 py-1.5 text-sm font-semibold text-white hover:bg-blue-700"
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
        subtitle={menuEntry ? fileEntryMenuSubtitle(menuEntry) : ''}
        actions={menuActions}
      />
      <ActionSheet
        isOpen={sortMenuOpen}
        onClose={() => setSortMenuOpen(false)}
        title="Sort files"
        subtitle={`${sortLabel} · Folders first`}
        actions={sortActions}
      />
      <ActionSheet
        isOpen={bookmarksOpen && !editingBookmark}
        onClose={() => {
          setBookmarksOpen(false)
          closeBookmarkEditor()
        }}
        title="Path bookmarks"
        subtitle={manager.pathBookmarksLoading ? 'Loading...' : `${manager.pathBookmarks.length} saved`}
        actions={bookmarkActions({
          bookmarks: manager.pathBookmarks,
          onAddCurrent: () => { hapticImpact(); void manager.addCurrentPathBookmark() },
          onEdit: openBookmarkEditor,
          onOpen: (bookmark) => { hapticSelection(); void manager.navigate(bookmark.path) },
        })}
      />
      {bookmarksOpen && editingBookmark ? (
        <div className="fixed inset-0 z-[110] flex items-end justify-center bg-black/40 backdrop-blur-[2px] md:items-center" onClick={closeBookmarkEditor}>
          <section
            className="w-full max-w-xl animate-slide-up border-t border-[var(--termx-app-line)] bg-white pb-[calc(env(safe-area-inset-bottom)+1rem)] md:border"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="mx-auto mt-3 h-1 w-12 bg-[var(--termx-app-line-strong)] md:hidden" />
            <div className="px-5 pb-2 pt-4">
              <h3 className="text-[17px] font-bold text-zinc-900">Edit bookmark</h3>
              <p className="mt-1 break-all text-[13px] font-medium text-zinc-500">{editingBookmark.path}</p>
            </div>
            <div className="px-5 py-3">
              <label className="flex flex-col gap-2 text-[13px] font-semibold text-zinc-600">
                Alias
                <input
                  aria-label="Bookmark alias"
                  className="h-12 border border-[var(--termx-app-line)] bg-zinc-50 px-3 text-[16px] font-semibold text-zinc-900 outline-none focus:border-[var(--termx-app-accent)] focus:ring-2 focus:ring-blue-500/20"
                  value={bookmarkAlias}
                  onChange={(event) => setBookmarkAlias(event.currentTarget.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') saveBookmarkAlias()
                    if (event.key === 'Escape') closeBookmarkEditor()
                  }}
                  autoFocus
                />
              </label>
              <div className="mt-4 grid grid-cols-2 gap-3">
                <button
                  type="button"
                  className="termx-app-secondary-button h-11 text-sm font-semibold"
                  onClick={() => { hapticSelection(); closeBookmarkEditor() }}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="termx-app-primary-button h-11 text-sm font-semibold"
                  onClick={saveBookmarkAlias}
                >
                  Save
                </button>
              </div>
              <button
                type="button"
                className="mt-3 h-11 w-full border border-red-200 bg-red-50 text-sm font-semibold text-red-600 active:bg-red-100"
                onClick={() => {
                  hapticImpact()
                  const id = editingBookmark.id
                  closeBookmarkEditor()
                  void manager.removePathBookmark(id)
                }}
              >
                Remove Bookmark
              </button>
            </div>
          </section>
        </div>
      ) : null}
    </div>
  )
}

function bookmarkActions({
  bookmarks,
  onAddCurrent,
  onEdit,
  onOpen,
}: {
  bookmarks: PathBookmark[]
  onAddCurrent: () => void
  onEdit: (bookmark: PathBookmark) => void
  onOpen: (bookmark: PathBookmark) => void
}): ActionSheetItem[] {
  return [
    {
      label: 'Save current directory',
      icon: <BookmarkPlus className="h-5 w-5" />,
      onClick: onAddCurrent,
      closeOnClick: false,
    },
    ...bookmarks.map((bookmark) => ({
      label: bookmark.label,
      ariaLabel: `Open bookmark ${bookmark.label}`,
      subtitle: bookmark.path,
      icon: <Bookmark className="h-5 w-5" />,
      onClick: () => onOpen(bookmark),
      secondaryAction: {
        label: `Edit bookmark ${bookmark.label}`,
        icon: <SquarePen className="h-5 w-5" />,
        onClick: () => onEdit(bookmark),
        closeOnClick: false,
      },
    })),
  ]
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

function iconForFile(name: string) {
  const ext = extension(name)
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'bmp', 'ico', 'avif'].includes(ext)) return Image
  if (['mp4', 'webm', 'mov', 'm4v', 'ogv', 'ogg'].includes(ext)) return PlaySquare
  if (isModelPreviewFile(name, '')) return Box
  if (isMarkdownFile(name, '')) return FileText
  if (['js', 'ts', 'jsx', 'tsx', 'go', 'py', 'rs', 'java', 'c', 'cpp', 'h', 'hpp', 'css', 'html', 'json', 'yaml', 'yml', 'toml', 'xml', 'sh', 'sql'].includes(ext)) return Code2
  return File
}

function uniqueFileListKey(counts: Map<string, number>, baseKey: string): string {
  const count = counts.get(baseKey) ?? 0
  counts.set(baseKey, count + 1)
  return count === 0 ? baseKey : `${baseKey}:${count}`
}
