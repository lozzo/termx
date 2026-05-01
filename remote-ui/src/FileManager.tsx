import { useFileManager } from './useFileManager'
import type { PeerTransport } from './transport'
import { Folder, File, RefreshCw, ChevronRight, AlertCircle, HardDrive } from 'lucide-react'

export interface FileManagerProps {
  machineId: string
  terminalId: string
  transport: Pick<PeerTransport, 'openApi' | 'openFileTransfer' | 'getConnectionInfo'>
  initialPath?: string | undefined
  className?: string | undefined
}

export function FileManager({
  machineId,
  terminalId,
  transport,
  initialPath,
  className,
}: FileManagerProps) {
  const manager = useFileManager({ machineId, terminalId, transport, initialPath })

  const pathSegments = manager.currentPath ? manager.currentPath.split('/').filter(Boolean) : []

  return (
    <div
      className={`relative flex min-h-0 flex-col bg-white ${className || ''}`}
      data-machine-id={machineId}
      data-terminal-id={terminalId}
      data-testid="termx-file-manager"
    >
      <header className="flex h-12 shrink-0 items-center gap-2 border-b border-zinc-200 bg-zinc-50/50 px-3 md:px-4">
        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto text-sm font-medium text-zinc-600">
           <HardDrive className="h-4 w-4 shrink-0 text-zinc-400" />
           <ChevronRight className="h-3.5 w-3.5 shrink-0 text-zinc-300" />
           {pathSegments.length === 0 ? (
             <span className="text-zinc-900 shrink-0">/</span>
           ) : (
             <>
               <button
                 onClick={() => void manager.navigate('/')}
                 className="shrink-0 hover:text-zinc-900 focus:outline-none focus:underline"
               >
                 root
               </button>
               {pathSegments.map((segment, index) => {
                 const isLast = index === pathSegments.length - 1
                 const path = '/' + pathSegments.slice(0, index + 1).join('/')
                 return (
                   <div key={path} className="flex items-center gap-1 shrink-0">
                     <ChevronRight className="h-3.5 w-3.5 shrink-0 text-zinc-300" />
                     {isLast ? (
                       <span className="text-zinc-900">{segment}</span>
                     ) : (
                       <button
                         onClick={() => void manager.navigate(path)}
                         className="hover:text-zinc-900 focus:outline-none focus:underline"
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
          className="flex h-8 shrink-0 items-center justify-center rounded-md border border-zinc-200 bg-white px-3 text-xs font-medium text-zinc-700 shadow-sm transition-colors hover:bg-zinc-50 hover:text-zinc-900 focus:outline-none focus:ring-2 focus:ring-zinc-400 active:bg-zinc-100 disabled:opacity-50"
          type="button"
          onClick={() => { void manager.refresh() }}
          disabled={manager.loading}
          aria-label="Refresh files"
        >
          <RefreshCw className={`mr-1.5 h-3.5 w-3.5 ${manager.loading ? 'animate-spin' : ''}`} />
          Refresh
        </button>
      </header>

      <div className="absolute top-12 bottom-0 left-0 right-0 overflow-y-auto p-2 md:p-3">
        {manager.error ? (
          <div className="mb-3 flex items-start gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700 shadow-sm" role="alert">
            <AlertCircle className="h-5 w-5 shrink-0 text-red-500" />
            <div>
               <h3 className="font-semibold text-red-900">Directory Error</h3>
               <p className="mt-1">{manager.error.message}</p>
            </div>
          </div>
        ) : null}

        {manager.loading && manager.entries.length === 0 ? (
          <div className="flex h-32 items-center justify-center text-sm text-zinc-500">
            <RefreshCw className="mr-2 h-4 w-4 animate-spin text-zinc-400" />
            Loading directory contents...
          </div>
        ) : (
          <ul aria-label="Files" className="flex flex-col gap-0.5">
            {manager.entries.length === 0 && !manager.loading && !manager.error ? (
              <li className="flex h-20 items-center justify-center text-sm text-zinc-500 italic">
                Directory is empty
              </li>
            ) : null}
            {manager.entries.map((entry) => {
              const entryPath = joinPath(manager.currentPath, entry.name)
              const isDirectory = entry.type === 'dir' || entry.type === 'symlink-dir'
              const Icon = isDirectory ? Folder : File

              return (
                <li key={entryPath}>
                  <button
                    className={`group flex min-h-10 w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm transition-colors focus:outline-none focus:ring-2 focus:ring-zinc-400 ${
                      isDirectory
                        ? 'hover:bg-zinc-100/80 cursor-pointer text-zinc-900'
                        : 'hover:bg-zinc-50 cursor-default text-zinc-700'
                    }`}
                    type="button"
                    aria-label={`${isDirectory ? 'Open' : 'Select'} ${entry.name}`}
                    onClick={() => {
                      if (isDirectory) void manager.navigate(entryPath)
                    }}
                  >
                    <Icon className={`h-4 w-4 shrink-0 ${isDirectory ? 'fill-blue-100 text-blue-500 group-hover:text-blue-600' : 'text-zinc-400'}`} />
                    <span className={`min-w-0 flex-1 truncate ${isDirectory ? 'font-medium' : ''}`}>
                      {entry.name}
                    </span>
                    <span className="shrink-0 text-[11px] font-medium text-zinc-400 uppercase tracking-wider w-16 text-right hidden sm:block">
                      {entry.type === 'symlink-dir' ? 'symlink' : entry.type}
                    </span>
                    <span className="shrink-0 text-xs tabular-nums text-zinc-500 w-16 text-right">
                      {formatBytes(entry.size)}
                    </span>
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
