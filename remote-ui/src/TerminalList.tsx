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
        <p>No terminals</p>
      ) : (
        <ul aria-label="Terminals">
          {terminals.map((terminal) => (
            <li
              key={terminal.terminalId}
              data-terminal-id={terminal.terminalId}
              data-active={activeTerminalId === terminal.terminalId ? 'true' : 'false'}
            >
              <button
                type="button"
                aria-label={`Open ${terminal.title}`}
                onClick={() => onOpenTerminal({
                  machineId,
                  terminalId: terminal.terminalId,
                })}
              >
                <span>{terminal.title}</span>
                {terminal.command ? <span>{terminal.command}</span> : null}
                {terminal.cols && terminal.rows ? <span>{terminal.cols}x{terminal.rows}</span> : null}
                <span>{terminal.state}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
