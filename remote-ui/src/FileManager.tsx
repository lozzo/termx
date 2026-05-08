import { useFileManager } from './useFileManager'
import type { FilePreviewResponse } from './fileApi'
import type { RtcSession } from './transport'
import { useState, type ReactNode } from 'react'
import { AlertCircle, Check, ChevronRight, Code2, Eye, EyeOff, File, FileText, Folder, HardDrive, Image, MoreVertical, PlaySquare, RefreshCw, SquarePen, Trash2, X } from 'lucide-react'

export interface FileManagerProps {
  machineId: string
  terminalId: string
  session: Pick<RtcSession, 'openApi' | 'openFileTransfer' | 'getConnectionInfo'>
  initialPath?: string | undefined
  className?: string | undefined
}

export function FileManager({
  machineId,
  terminalId,
  session,
  initialPath,
  className,
}: FileManagerProps) {
  const manager = useFileManager({ machineId, terminalId, session, initialPath })
  const [newDirOpen, setNewDirOpen] = useState(false)
  const [entryMenuPath, setEntryMenuPath] = useState<string | null>(null)
  const [renamePath, setRenamePath] = useState<string | null>(null)
  const [renameName, setRenameName] = useState('')
  const [deletePath, setDeletePath] = useState<string | null>(null)

  const pathSegments = manager.currentPath ? manager.currentPath.split('/').filter(Boolean) : []
  const entryKeyCounts = new Map<string, number>()

  return (
    <div
      className={`relative flex min-h-0 flex-col bg-white ${className || ''}`}
      data-machine-id={machineId}
      data-terminal-id={terminalId}
      data-testid="termx-file-manager"
    >
      <header className="flex h-14 shrink-0 items-center gap-3 border-b border-zinc-200/60 bg-zinc-50/80 px-4 backdrop-blur-md">
        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto text-[15px] font-medium text-zinc-600 no-scrollbar">
           <HardDrive className="h-5 w-5 shrink-0 text-zinc-400" />
           <ChevronRight className="h-4 w-4 shrink-0 text-zinc-300" />
           {pathSegments.length === 0 ? (
             <span className="text-zinc-900 shrink-0 font-semibold">/</span>
           ) : (
             <>
               <button
                 onClick={() => void manager.navigate('/')}
                 className="shrink-0 rounded-md px-2 py-1 text-zinc-500 transition-colors active:bg-zinc-200"
               >
                 root
               </button>
               {pathSegments.map((segment, index) => {
                 const isLast = index === pathSegments.length - 1
                 const path = '/' + pathSegments.slice(0, index + 1).join('/')
                 return (
                   <div key={`${path}:${index}`} className="flex items-center shrink-0">
                     <ChevronRight className="h-4 w-4 shrink-0 text-zinc-300" />
                     {isLast ? (
                       <span className="px-2 py-1 font-semibold text-zinc-900">{segment}</span>
                     ) : (
                       <button
                         onClick={() => void manager.navigate(path)}
                         className="rounded-md px-2 py-1 text-zinc-500 transition-colors active:bg-zinc-200"
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
          className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-full transition-colors active:scale-95 ${manager.showHidden ? 'bg-blue-50 text-blue-600' : 'bg-zinc-100 text-zinc-600 active:bg-zinc-200'}`}
          type="button"
          onClick={manager.toggleShowHidden}
          aria-label={manager.showHidden ? 'Hide hidden files' : 'Show hidden files'}
        >
          {manager.showHidden ? <Eye className="h-5 w-5" /> : <EyeOff className="h-5 w-5" />}
        </button>
        <button
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-zinc-100 text-zinc-600 transition-colors active:scale-95 active:bg-zinc-200"
          type="button"
          onClick={() => setNewDirOpen((current) => !current)}
          aria-label="New directory"
        >
          <Folder className="h-5 w-5" />
        </button>
        <button
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-zinc-100 text-zinc-600 transition-colors active:scale-95 active:bg-zinc-200 disabled:opacity-50"
          type="button"
          onClick={() => { void manager.refresh() }}
          disabled={manager.loading}
          aria-label="Refresh files"
        >
          <RefreshCw className={`h-5 w-5 ${manager.loading ? 'animate-spin' : ''}`} />
        </button>
      </header>

      <div className="absolute top-14 bottom-0 left-0 right-0 overflow-y-auto bg-white p-2">
        {newDirOpen ? (
          <div className="m-2 flex items-center gap-2 rounded-xl border border-zinc-200 bg-zinc-50 p-2">
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

        {manager.loading && manager.entries.length === 0 ? (
          <div className="flex h-40 flex-col items-center justify-center gap-3 text-[14px] font-medium text-zinc-500">
            <RefreshCw className="h-6 w-6 animate-spin text-zinc-400" />
            Loading directory...
          </div>
        ) : (
          <ul aria-label="Files" className="flex flex-col gap-1 pb-safe">
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
              const isMenuOpen = entryMenuPath === entryPath
              const isRenaming = renamePath === entryPath
              const openEntry = () => {
                if (isDirectory) void manager.navigate(entryPath)
                else void manager.openPreview(entryPath)
              }

              return (
                <li key={itemKey}>
                  <div
                    className="group relative flex min-h-[3.5rem] w-full items-center gap-4 rounded-xl px-4 py-2 text-left transition-colors focus-within:ring-2 focus-within:ring-blue-500 active:bg-zinc-100"
                  >
                    <div className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg transition-colors ${isDirectory ? 'bg-blue-50 group-active:bg-blue-100' : 'bg-zinc-50'}`}>
                      <Icon className={`h-5 w-5 ${isDirectory ? 'fill-blue-100 text-blue-500' : 'text-zinc-400'}`} />
                    </div>
                    <button
                      type="button"
                      aria-label={`${isDirectory ? 'Open' : 'Preview'} ${entry.name}`}
                      className="flex min-w-0 flex-1 flex-col justify-center text-left"
                      onClick={openEntry}
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
                        <span className={`truncate text-[15px] ${isDirectory ? 'font-semibold text-zinc-900' : 'font-medium text-zinc-700'}`}>
                          {entry.name}
                        </span>
                      )}
                      {!isDirectory && entry.size > 0 && !isRenaming ? (
                        <span className="truncate text-[12px] font-medium text-zinc-500">
                          {formatBytes(entry.size)}
                        </span>
                      ) : null}
                    </button>
                    <button
                      type="button"
                      aria-label={`More actions for ${entry.name}`}
                      className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-zinc-400 active:bg-zinc-200"
                      onClick={(event) => {
                        event.stopPropagation()
                        setEntryMenuPath((current) => current === entryPath ? null : entryPath)
                      }}
                    >
                      <MoreVertical className="h-5 w-5" />
                    </button>
                    {isDirectory ? <ChevronRight className="h-5 w-5 shrink-0 text-zinc-300 group-active:text-zinc-400" /> : null}
                  </div>
                  {isMenuOpen ? (
                    <div className={`mx-2 mb-1 grid gap-2 rounded-xl bg-zinc-50 p-2 ${isDirectory ? 'grid-cols-2' : 'grid-cols-3'}`}>
                      {!isDirectory ? (
                        <button
                          type="button"
                          className="flex h-10 items-center justify-center gap-2 rounded-lg bg-white text-[13px] font-semibold text-zinc-700 shadow-sm"
                          onClick={() => {
                            setEntryMenuPath(null)
                            void manager.openPreview(entryPath)
                          }}
                        >
                          <Eye className="h-4 w-4" />
                          Preview
                        </button>
                      ) : null}
                      <button
                        type="button"
                        className="flex h-10 items-center justify-center gap-2 rounded-lg bg-white text-[13px] font-semibold text-zinc-700 shadow-sm"
                        onClick={() => {
                          setRenamePath(entryPath)
                          setRenameName(entry.name)
                          setEntryMenuPath(null)
                        }}
                      >
                        <SquarePen className="h-4 w-4" />
                        Rename
                      </button>
                      <button
                        type="button"
                        className="flex h-10 items-center justify-center gap-2 rounded-lg bg-red-50 text-[13px] font-semibold text-red-700 shadow-sm"
                        onClick={() => {
                          setDeletePath(entryPath)
                          setEntryMenuPath(null)
                        }}
                      >
                        <Trash2 className="h-4 w-4" />
                        Delete
                      </button>
                    </div>
                  ) : null}
                </li>
              )
            })}
          </ul>
        )}
      </div>
      {deletePath ? (
        <div className="absolute inset-0 z-50 flex items-end bg-black/40 p-3 backdrop-blur-sm md:items-center md:justify-center" data-testid="termx-file-delete-confirm" onClick={() => setDeletePath(null)}>
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
    </div>
  )
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

  return (
    <div
      className="absolute inset-0 z-40 flex items-end bg-black/45 p-2 backdrop-blur-sm md:items-center md:justify-center md:p-4"
      data-testid="termx-file-preview"
      onClick={onClose}
    >
      <section
        className="flex max-h-[92%] w-full min-h-0 flex-col overflow-hidden rounded-2xl bg-white shadow-2xl md:max-h-[86%] md:max-w-4xl"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="flex shrink-0 items-center gap-3 border-b border-zinc-200 px-4 py-3">
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-[16px] font-bold text-zinc-950">{title}</h2>
            <p className="mt-0.5 truncate text-[12px] font-medium text-zinc-500">{subtitle}</p>
          </div>
          <button
            type="button"
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-zinc-100 text-zinc-600 active:bg-zinc-200"
            aria-label="Close preview"
            onClick={onClose}
          >
            <X className="h-5 w-5" />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-auto bg-zinc-50">
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
      </section>
    </div>
  )
}

function PreviewContent({ preview }: { preview: FilePreviewResponse }) {
  if (preview.category === 'image' && preview.contentBase64) {
    return (
      <div className="flex min-h-full items-center justify-center p-3">
        <img
          alt={preview.name}
          className="max-h-full max-w-full rounded-lg object-contain shadow-sm"
          src={`data:${preview.mimeType};base64,${preview.contentBase64}`}
        />
      </div>
    )
  }
  if (preview.category === 'video' && preview.contentBase64) {
    return (
      <div className="flex min-h-full items-center justify-center bg-black p-2">
        <video
          className="max-h-full max-w-full"
          controls
          preload="metadata"
          src={`data:${preview.mimeType};base64,${preview.contentBase64}`}
        />
      </div>
    )
  }
  if (preview.category === 'text' && preview.content !== undefined) {
    if (isMarkdownFile(preview.name, preview.mimeType)) {
      return <MarkdownPreview text={preview.content} />
    }
    return <TextPreview text={preview.content} />
  }
  const limit = preview.previewLimit && preview.previewLimit > 0 ? formatBytes(preview.previewLimit) : ''
  const message = preview.category === 'unsupported'
    ? 'This file type is not available for inline preview.'
    : `This file is too large to preview${limit ? ` within the ${limit} limit` : ''}.`
  return <PreviewNotice title="No Preview" message={message} />
}

function TextPreview({ text }: { text: string }) {
  return (
    <pre className="min-h-full whitespace-pre-wrap break-words bg-white p-4 font-mono text-[13px] leading-6 text-zinc-900">
      {text}
    </pre>
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
