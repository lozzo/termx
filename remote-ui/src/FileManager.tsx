import { useFileManager } from './useFileManager'
import type { RtcSession } from './transport'
import { Folder, File, RefreshCw, ChevronRight, AlertCircle, HardDrive } from 'lucide-react'

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
        {manager.error ? (
          <div className="m-2 flex items-start gap-3 rounded-xl border border-red-200/60 bg-red-50 p-4 text-[14px] text-red-800 shadow-sm" role="alert">
            <AlertCircle className="h-6 w-6 shrink-0 text-red-500" />
            <div>
               <h3 className="font-bold text-red-900">Directory Error</h3>
               <p className="mt-1">{manager.error.message}</p>
            </div>
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
            {manager.entries.map((entry) => {
              const entryPath = joinPath(manager.currentPath, entry.name)
              const isDirectory = entry.type === 'dir' || entry.type === 'symlink-dir'
              const Icon = isDirectory ? Folder : File
              const itemKey = uniqueFileListKey(entryKeyCounts, entryPath)

              return (
                <li key={itemKey}>
                  <button
                    className={`group relative flex min-h-[3.5rem] w-full items-center gap-4 rounded-xl px-4 py-2 text-left transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 active:bg-zinc-100 ${
                      isDirectory
                        ? 'cursor-pointer'
                        : 'cursor-default'
                    }`}
                    type="button"
                    aria-label={`${isDirectory ? 'Open' : 'Select'} ${entry.name}`}
                    onClick={() => {
                      if (isDirectory) void manager.navigate(entryPath)
                    }}
                  >
                    <div className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg transition-colors ${isDirectory ? 'bg-blue-50 group-active:bg-blue-100' : 'bg-zinc-50'}`}>
                      <Icon className={`h-5 w-5 ${isDirectory ? 'fill-blue-100 text-blue-500' : 'text-zinc-400'}`} />
                    </div>
                    <div className="flex min-w-0 flex-1 flex-col justify-center">
                      <span className={`truncate text-[15px] ${isDirectory ? 'font-semibold text-zinc-900' : 'font-medium text-zinc-700'}`}>
                        {entry.name}
                      </span>
                      {!isDirectory && entry.size > 0 ? (
                        <span className="truncate text-[12px] font-medium text-zinc-500">
                          {formatBytes(entry.size)}
                        </span>
                      ) : null}
                    </div>
                    {isDirectory ? (
                      <ChevronRight className="h-5 w-5 shrink-0 text-zinc-300 group-active:text-zinc-400" />
                    ) : null}
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </div>
  )
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

function formatBytes(bytes: number, decimals = 1) {
  if (!+bytes) return '0 B'
  const k = 1024
  const dm = decimals < 0 ? 0 : decimals
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`
}
