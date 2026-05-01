import type { Terminal } from './model'

export interface OpenTerminalIntent {
  machineId: string
  terminalId: string
}

export interface TerminalListProps {
  machineId: string
  terminals: Terminal[]
  onOpenTerminal: (intent: OpenTerminalIntent) => void
  activeTerminalId?: string
  className?: string
}

export function TerminalList({
  machineId,
  terminals,
  onOpenTerminal,
  activeTerminalId,
  className,
}: TerminalListProps) {
  return (
    <section
      className={className}
      data-machine-id={machineId}
      data-testid="termx-terminal-list"
    >
      {terminals.length === 0 ? (
        <p className="text-sm text-zinc-600">No terminals</p>
      ) : (
        <ul aria-label="Terminals" className="grid list-none gap-2 p-0">
          {terminals.map((terminal) => (
            <li
              key={terminal.terminalId}
              data-terminal-id={terminal.terminalId}
              data-active={activeTerminalId === terminal.terminalId ? 'true' : 'false'}
            >
              <button
                className="grid w-full cursor-pointer gap-1 rounded-md border border-slate-300 bg-white px-3 py-2 text-left text-sm text-zinc-950 hover:border-slate-500 focus:outline-none focus:ring-2 focus:ring-slate-300 data-[active=true]:border-slate-900 data-[active=true]:bg-slate-900 data-[active=true]:text-white"
                data-active={activeTerminalId === terminal.terminalId ? 'true' : 'false'}
                type="button"
                aria-label={`Open ${terminal.title}`}
                onClick={() => onOpenTerminal({
                  machineId,
                  terminalId: terminal.terminalId,
                })}
              >
                <span className="font-medium">{terminal.title}</span>
                {terminal.command ? <span className="truncate text-xs opacity-75">{terminal.command}</span> : null}
                {terminal.cols && terminal.rows ? (
                  <span className="text-xs opacity-75">{terminal.cols}x{terminal.rows}</span>
                ) : null}
                <span className="text-xs opacity-75">{terminal.state}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
