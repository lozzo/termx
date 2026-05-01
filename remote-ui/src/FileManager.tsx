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
      <header>
        <span>{manager.currentPath || '/'}</span>
        <button type="button" onClick={() => { void manager.refresh() }}>Refresh</button>
      </header>

      {manager.error ? (
        <div role="alert">{manager.error.message}</div>
      ) : null}

      {manager.loading ? (
        <p>Loading files</p>
      ) : (
        <ul aria-label="Files">
          {manager.entries.map((entry) => {
            const entryPath = joinPath(manager.currentPath, entry.name)
            const isDirectory = entry.type === 'dir' || entry.type === 'symlink-dir'
            return (
              <li key={entryPath}>
                <button
                  type="button"
                  aria-label={`${isDirectory ? 'Open' : 'Select'} ${entry.name}`}
                  onClick={() => {
                    if (isDirectory) void manager.navigate(entryPath)
                  }}
                >
                  <span>{entry.name}</span>
                  <span>{entry.type}</span>
                  <span>{entry.size}</span>
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
