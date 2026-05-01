import { forwardRef, useEffect, useImperativeHandle } from 'react'
import { useTerminalSession } from './useTerminalSession'
import type { TerminalTransport } from './terminalClient'

export interface TerminalProps {
  machineId: string
  terminalId: string
  transport: TerminalTransport
  className?: string
  onReady?: () => void
}

export interface TerminalHandle {
  sendInput(data: string): void
  sendResize(cols: number, rows: number): void
  reattach(transport: TerminalTransport): void
}

export const Terminal = forwardRef<TerminalHandle, TerminalProps>(function Terminal(
  { machineId, terminalId, transport, className, onReady },
  ref,
) {
  const session = useTerminalSession({ machineId, terminalId, transport })

  useImperativeHandle(ref, () => ({
    sendInput: session.sendInput,
    sendResize: session.sendResize,
    reattach: session.reattach,
  }), [session.sendInput, session.sendResize, session.reattach])

  const isOpen = session.snapshot.terminalChannels[terminalId]?.state === 'open'

  useEffect(() => {
    if (isOpen) onReady?.()
  }, [isOpen, onReady])

  return (
    <section
      className={className}
      data-machine-id={machineId}
      data-terminal-id={terminalId}
      data-phase={session.snapshot.phase}
      data-testid="termx-terminal"
    >
      <pre aria-label="Terminal output">
        {session.terminalText}
      </pre>
    </section>
  )
})
