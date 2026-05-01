import type { Terminal } from './model'
import { Terminal as TerminalIcon, CircleDot } from 'lucide-react'

export interface OpenTerminalIntent {
  machineId: string
  terminalId: string
}

export interface TerminalListProps {
  machineId: string
  terminals: Terminal[]
  onOpenTerminal: (intent: OpenTerminalIntent) => void
  activeTerminalId?: string | undefined
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
    <div
      className={className}
      data-machine-id={machineId}
      data-testid="termx-terminal-list"
    >
      {terminals.length === 0 ? (
        <div className="flex h-16 items-center justify-center rounded-md border border-dashed border-zinc-300 text-sm text-zinc-500">
          No terminals
        </div>
      ) : (
        <ul aria-label="Terminals" className="flex flex-col gap-1.5">
          {terminals.map((terminal) => {
            const isActive = activeTerminalId === terminal.terminalId
            return (
              <li key={terminal.terminalId} data-terminal-id={terminal.terminalId}>
                <button
                  className={`group flex min-h-12 w-full items-center gap-3 rounded-md px-3 py-2 text-left transition-colors focus:outline-none focus:ring-2 focus:ring-zinc-400 ${
                    isActive
                      ? 'bg-zinc-900 text-white shadow-sm'
                      : 'bg-white text-zinc-700 hover:bg-zinc-50 border border-zinc-200 shadow-sm'
                  }`}
                  type="button"
                  aria-label={`Open ${terminal.title}`}
                  aria-current={isActive ? 'true' : 'false'}
                  onClick={() => onOpenTerminal({ machineId, terminalId: terminal.terminalId })}
                >
                  <TerminalIcon className={`h-4 w-4 shrink-0 ${isActive ? 'text-zinc-300' : 'text-zinc-400 group-hover:text-zinc-600'}`} />
                  <div className="flex min-w-0 flex-1 flex-col justify-center">
                    <span className={`truncate text-sm font-medium leading-none ${isActive ? 'text-zinc-100' : 'text-zinc-900'}`}>
                      {terminal.title}
                    </span>
                    {terminal.command && (
                      <span className={`mt-1.5 truncate text-[11px] leading-none ${isActive ? 'text-zinc-400' : 'text-zinc-500'}`}>
                        {terminal.command}
                      </span>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                     {terminal.cols && terminal.rows ? (
                        <span className={`text-[10px] tabular-nums hidden sm:inline-block ${isActive ? 'text-zinc-400' : 'text-zinc-400'}`}>
                          {terminal.cols}x{terminal.rows}
                        </span>
                     ) : null}
                     <CircleDot className={`h-2 w-2 ${terminal.state === 'running' ? 'text-emerald-500 fill-emerald-500' : 'text-zinc-400'}`} />
                  </div>
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
