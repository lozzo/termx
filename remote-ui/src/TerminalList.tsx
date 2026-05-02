import type { Terminal } from './model'
import type { ReactNode } from 'react'
import { Activity, CircleDot, Lock, Terminal as TerminalIcon, Unlock } from 'lucide-react'

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
                  className={`group flex min-h-16 w-full items-start gap-3 rounded-md px-3 py-2.5 text-left transition-colors focus:outline-none focus:ring-2 focus:ring-zinc-400 ${
                    isActive
                      ? 'bg-zinc-900 text-white shadow-sm'
                      : 'bg-white text-zinc-700 hover:bg-zinc-50 border border-zinc-200 shadow-sm'
                  }`}
                  type="button"
                  aria-label={`Open ${terminal.title}`}
                  aria-current={isActive ? 'true' : 'false'}
                  onClick={() => onOpenTerminal({ machineId, terminalId: terminal.terminalId })}
                >
                  <TerminalIcon className={`mt-0.5 h-4 w-4 shrink-0 ${isActive ? 'text-zinc-300' : 'text-zinc-400 group-hover:text-zinc-600'}`} />
                  <div className="flex min-w-0 flex-1 flex-col justify-center">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className={`truncate text-sm font-medium leading-none ${isActive ? 'text-zinc-100' : 'text-zinc-900'}`}>
                        {terminal.title}
                      </span>
                      {terminal.environment ? (
                        <span className={`shrink-0 rounded border px-1.5 py-0.5 text-[10px] font-medium leading-none ${isActive ? 'border-zinc-700 text-zinc-300' : 'border-zinc-200 text-zinc-500'}`}>
                          {terminal.environment}
                        </span>
                      ) : null}
                    </div>
                    {terminal.command ? (
                      <span className={`mt-1.5 truncate text-[11px] leading-none ${isActive ? 'text-zinc-400' : 'text-zinc-500'}`}>
                        {terminal.command}
                      </span>
                    ) : null}
                    {terminal.cwd ? (
                      <span className={`mt-1.5 truncate text-[11px] leading-none ${isActive ? 'text-zinc-500' : 'text-zinc-500'}`}>
                        {terminal.cwd}
                      </span>
                    ) : null}
                    <div className="mt-2 flex flex-wrap items-center gap-1.5">
                      <MetadataPill active={isActive}>
                        <CircleDot className={`h-2 w-2 ${terminal.state === 'running' ? 'fill-emerald-500 text-emerald-500' : 'text-zinc-400'}`} />
                        {formatTerminalState(terminal.state)}
                      </MetadataPill>
                      {terminal.cols && terminal.rows ? (
                        <MetadataPill active={isActive}>{terminal.cols}x{terminal.rows}</MetadataPill>
                      ) : null}
                      <MetadataPill active={isActive}>
                        {terminal.sizeLocked || terminal.sizeLockMode === 'lock' ? (
                          <Lock className="h-3 w-3" />
                        ) : (
                          <Unlock className="h-3 w-3" />
                        )}
                        {terminal.sizeLocked || terminal.sizeLockMode === 'lock' ? 'Locked' : 'Resizable'}
                      </MetadataPill>
                      {terminal.lastActiveAt ? (
                        <MetadataPill active={isActive}>
                          <Activity className="h-3 w-3" />
                          Created {formatLifecycleTime(terminal.lastActiveAt)}
                        </MetadataPill>
                      ) : null}
                    </div>
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

function MetadataPill({
  active,
  children,
}: {
  active: boolean
  children: ReactNode
}) {
  return (
    <span className={`inline-flex min-h-5 items-center gap-1 rounded border px-1.5 text-[10px] font-medium leading-none ${active ? 'border-zinc-700 bg-zinc-800 text-zinc-300' : 'border-zinc-200 bg-zinc-50 text-zinc-500'}`}>
      {children}
    </span>
  )
}

function formatTerminalState(state: Terminal['state']): string {
  if (state === 'running') return 'Running'
  if (state === 'exited') return 'Exited'
  return 'Unknown'
}

function formatLifecycleTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
