import { FitAddon } from '@xterm/addon-fit'
import { Terminal as XTerm } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef } from 'react'
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
  const containerRef = useRef<HTMLDivElement | null>(null)
  const xtermRef = useRef<XTerm | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const lastWrittenTextRef = useRef('')
  const lastSentResizeRef = useRef<{ cols: number; rows: number } | null>(null)
  const isOpenRef = useRef(false)

  useImperativeHandle(ref, () => ({
    sendInput: session.sendInput,
    sendResize: session.sendResize,
    reattach: session.reattach,
  }), [session.sendInput, session.sendResize, session.reattach])

  const isOpen = session.snapshot.terminalChannels[terminalId]?.state === 'open'

  const fitAndSendResize = useCallback(() => {
    const term = xtermRef.current
    const fitAddon = fitAddonRef.current
    if (!term || !fitAddon) return

    fitAddon.fit()
    const dimensions = fitAddon.proposeDimensions()
    if (!dimensions) return
    term.resize(dimensions.cols, dimensions.rows)

    if (!isOpenRef.current) return
    const last = lastSentResizeRef.current
    if (last?.cols === dimensions.cols && last.rows === dimensions.rows) return
    lastSentResizeRef.current = dimensions
    session.sendResize(dimensions.cols, dimensions.rows)
  }, [session.sendResize])

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const term = new XTerm({
      allowProposedApi: false,
      cursorBlink: true,
      convertEol: false,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      fontSize: 13,
      scrollback: 5000,
      theme: {
        background: '#101418',
        foreground: '#eef3f8',
        cursor: '#ffffff',
        selectionBackground: '#3c4b59',
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(container)
    xtermRef.current = term
    fitAddonRef.current = fitAddon

    const dataDisposable = term.onData((data) => {
      session.sendInput(data)
    })

    fitAndSendResize()

    let resizeObserver: ResizeObserver | null = null
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => {
        fitAndSendResize()
      })
      resizeObserver.observe(container)
    }

    return () => {
      resizeObserver?.disconnect()
      dataDisposable.dispose()
      term.dispose()
      if (xtermRef.current === term) xtermRef.current = null
      if (fitAddonRef.current === fitAddon) fitAddonRef.current = null
      lastWrittenTextRef.current = ''
      lastSentResizeRef.current = null
      isOpenRef.current = false
    }
  }, [fitAndSendResize, session.sendInput])

  useEffect(() => {
    isOpenRef.current = isOpen
    if (!isOpen) return
    fitAndSendResize()
    xtermRef.current?.focus()
    onReady?.()
  }, [fitAndSendResize, isOpen, onReady])

  useEffect(() => {
    const term = xtermRef.current
    if (!term) return

    const nextText = session.terminalText
    const previousText = lastWrittenTextRef.current
    if (nextText === previousText) return

    if (nextText.startsWith(previousText)) {
      term.write(nextText.slice(previousText.length))
    } else {
      term.reset()
      term.write(nextText)
    }
    lastWrittenTextRef.current = nextText
  }, [session.terminalText])

  return (
    <section
      className={className}
      data-machine-id={machineId}
      data-terminal-id={terminalId}
      data-phase={session.snapshot.phase}
      data-testid="termx-terminal"
    >
      <div
        ref={containerRef}
        aria-label="Terminal output"
        className="h-[52vh] min-h-80 overflow-hidden p-2 font-mono text-[13px] leading-normal"
        role="application"
        tabIndex={0}
      />
    </section>
  )
})
