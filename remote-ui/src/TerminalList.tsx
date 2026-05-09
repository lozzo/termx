import type { Terminal } from './model'
import type { ReactNode } from 'react'
import { CircleDot, Lock, MoreVertical, Terminal as TerminalIcon, Unlock, ChevronRight } from 'lucide-react'
import { haptic } from './haptics'

export interface OpenTerminalIntent {
  machineId: string
  terminalId: string
}

export interface TerminalListProps {
  machineId: string
  terminals: Terminal[]
  onOpenTerminal: (intent: OpenTerminalIntent) => void
  onManageTerminal?: ((intent: OpenTerminalIntent) => void) | undefined
  activeTerminalId?: string | undefined
  className?: string
}

export function TerminalList({
  machineId,
  terminals,
  onOpenTerminal,
  onManageTerminal,
  activeTerminalId,
  className,
}: TerminalListProps) {
  const terminalKeyCounts = new Map<string, number>()

  return (
    <div
      className={className}
      data-machine-id={machineId}
      data-testid="termx-terminal-list"
    >
      {terminals.length === 0 ? (
        <div className="flex h-32 flex-col items-center justify-center gap-3 rounded-2xl border-2 border-dashed border-zinc-200 bg-zinc-50/50 text-sm text-zinc-500">
          <TerminalIcon className="h-8 w-8 text-zinc-300" />
          <p>No active terminals</p>
        </div>
      ) : (
        <ul aria-label="Terminals" className="flex flex-col gap-3">
          {terminals.map((terminal) => {
            const isActive = activeTerminalId === terminal.terminalId
            const itemKey = uniqueTerminalListKey(terminalKeyCounts, machineId, terminal)
            return (
              <li key={itemKey} data-terminal-id={terminal.terminalId}>
                <div
                  className={`group relative flex w-full items-center gap-3 rounded-xl p-3 text-left transition-all duration-200 focus-within:ring-2 focus-within:ring-blue-500 ${
                    isActive
                      ? 'bg-zinc-900 text-white shadow-lg shadow-zinc-900/10'
                      : 'bg-white text-zinc-700 shadow-sm border border-zinc-200/60 hover:border-zinc-300 hover:bg-zinc-50'
                  }`}
                  onContextMenu={(event) => {
                    if (!onManageTerminal) return
                    event.preventDefault()
                    onManageTerminal({ machineId, terminalId: terminal.terminalId })
                  }}
                  onPointerDown={(event) => {
                    if (!onManageTerminal || event.pointerType === 'mouse') return
                    const target = event.currentTarget
                    const timer = window.setTimeout(() => {
                      haptic()
                      onManageTerminal({ machineId, terminalId: terminal.terminalId })
                    }, 450)
                    const clear = () => {
                      window.clearTimeout(timer)
                      target.removeEventListener('pointerup', clear)
                      target.removeEventListener('pointerleave', clear)
                      target.removeEventListener('pointercancel', clear)
                    }
                    target.addEventListener('pointerup', clear, { once: true })
                    target.addEventListener('pointerleave', clear, { once: true })
                    target.addEventListener('pointercancel', clear, { once: true })
                  }}
                >
                  <button
                    className="flex min-w-0 flex-1 items-center gap-3 text-left active:scale-[0.98] focus:outline-none"
                    type="button"
                    aria-label={`Open ${terminal.title}`}
                    aria-current={isActive ? 'true' : 'false'}
                    onClick={() => { haptic(); onOpenTerminal({ machineId, terminalId: terminal.terminalId }) }}
                  >
                    <div className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg transition-colors ${isActive ? 'bg-zinc-800' : 'bg-zinc-100 group-hover:bg-zinc-200'}`}>
                      <TerminalIcon className={`h-5 w-5 ${isActive ? 'text-zinc-200' : 'text-zinc-500'}`} />
                    </div>

                    <div className="flex min-w-0 flex-1 flex-col justify-center gap-1.5">
                      <div className="flex min-w-0 items-center justify-between gap-2">
                        <span className={`truncate text-[14px] font-semibold tracking-tight leading-none ${isActive ? 'text-zinc-100' : 'text-zinc-900'}`}>
                          {terminal.title || terminal.command || 'Terminal'}
                        </span>
                        {terminal.environment ? (
                          <span className={`shrink-0 rounded-md px-1.5 py-0.5 text-[9px] font-bold tracking-wider uppercase leading-none ${isActive ? 'bg-zinc-800 text-zinc-300' : 'bg-zinc-100 text-zinc-500'}`}>
                            {terminal.environment}
                          </span>
                        ) : null}
                      </div>
                      {terminal.command || terminal.cwd ? (
                        <span className={`truncate text-[11px] font-medium leading-none ${isActive ? 'text-zinc-400' : 'text-zinc-500'}`}>
                          {terminal.cwd ? terminal.cwd : terminal.command}
                        </span>
                      ) : null}

                      <div className="mt-0.5 flex flex-wrap items-center gap-1.5">
                        <MetadataPill active={isActive}>
                          <CircleDot className={`h-2.5 w-2.5 ${terminal.state === 'running' ? 'fill-emerald-500 text-emerald-500' : 'text-zinc-400'}`} />
                          {formatTerminalState(terminal.state)}
                        </MetadataPill>
                        {terminal.cols && terminal.rows ? (
                          <MetadataPill active={isActive}>{terminal.cols} × {terminal.rows}</MetadataPill>
                        ) : null}
                        <MetadataPill active={isActive}>
                          {terminal.sizeLocked || terminal.sizeLockMode === 'lock' ? (
                            <Lock className="h-3 w-3" />
                          ) : (
                            <Unlock className="h-3 w-3" />
                          )}
                        </MetadataPill>
                      </div>
                    </div>
                    <ChevronRight className={`h-4 w-4 shrink-0 transition-transform group-active:translate-x-1 ${isActive ? 'text-zinc-500' : 'text-zinc-300 group-hover:text-zinc-400'}`} />
                  </button>

                  {onManageTerminal ? (
                    <button
                      type="button"
                      className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 ${isActive ? 'text-zinc-300 hover:bg-zinc-800' : 'text-zinc-400 hover:bg-zinc-100 hover:text-zinc-700'}`}
                      aria-label={`Manage ${terminal.title}`}
                      onClick={() => { haptic(); onManageTerminal({ machineId, terminalId: terminal.terminalId }) }}
                    >
                      <MoreVertical className="h-4 w-4" />
                    </button>
                  ) : null}
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

function uniqueTerminalListKey(counts: Map<string, number>, fallbackMachineId: string, terminal: Terminal): string {
  const baseKey = `${terminal.machineId || fallbackMachineId}:${terminal.terminalId}`
  const count = counts.get(baseKey) ?? 0
  counts.set(baseKey, count + 1)
  return count === 0 ? baseKey : `${baseKey}:${count}`
}

function MetadataPill({
  active,
  children,
}: {
  active: boolean
  children: ReactNode
}) {
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-md px-1.5 py-0.5 text-[10px] font-semibold leading-none transition-colors ${active ? 'bg-zinc-800/80 text-zinc-300' : 'bg-zinc-100 text-zinc-500'}`}>
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
