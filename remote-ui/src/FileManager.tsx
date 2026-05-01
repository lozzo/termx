import { useFileManager } from './useFileManager'
import type { PeerTransport } from './transport'

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

  return (
    <section
      className={className}
      data-machine-id={machineId}
      data-terminal-id={terminalId}
      data-testid="termx-file-manager"
    >
      <header className="mb-2 flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-zinc-800">{manager.currentPath || '/'}</span>
        <button
          className="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm text-zinc-950 hover:border-slate-500 focus:outline-none focus:ring-2 focus:ring-slate-300"
          type="button"
          onClick={() => { void manager.refresh() }}
        >
          Refresh
        </button>
      </header>

      {manager.error ? (
        <div className="mb-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {manager.error.message}
        </div>
      ) : null}

      {manager.loading ? (
        <p className="text-sm text-zinc-600">Loading files</p>
      ) : (
        <ul aria-label="Files" className="grid list-none gap-1 p-0">
          {manager.entries.map((entry) => {
            const entryPath = joinPath(manager.currentPath, entry.name)
            const isDirectory = entry.type === 'dir' || entry.type === 'symlink-dir'
            return (
              <li key={entryPath}>
                <button
                  className="grid w-full cursor-pointer grid-cols-[minmax(0,1fr)_auto_auto] gap-3 rounded-md border border-slate-200 bg-white px-3 py-2 text-left text-sm text-zinc-950 hover:border-slate-400 focus:outline-none focus:ring-2 focus:ring-slate-300"
                  type="button"
                  aria-label={`${isDirectory ? 'Open' : 'Select'} ${entry.name}`}
                  onClick={() => {
                    if (isDirectory) void manager.navigate(entryPath)
                  }}
                >
                  <span className="min-w-0 truncate">{entry.name}</span>
                  <span className="text-xs text-zinc-500">{entry.type}</span>
                  <span className="text-xs tabular-nums text-zinc-500">{entry.size}</span>
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

function joinPath(base: string, name: string): string {
  if (!base || base === '/') return `/${name}`
  return `${base.replace(/\/+$/, '')}/${name}`
}
