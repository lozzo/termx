import { FitAddon } from '@xterm/addon-fit'
import { Terminal as XTerm } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef } from 'react'
import { applyTerminalModifiers, type TerminalModifierState } from './mobileTerminalInput'
import { useTerminalSession } from './useTerminalSession'
import type { TerminalTransport } from './terminalClient'

export interface TerminalProps {
  machineId: string
  terminalId: string
  transport: TerminalTransport
  className?: string
  onReady?: () => void
  onCursorMove?: (() => void) | undefined
  onBufferChange?: ((isAlternate: boolean) => void) | undefined
  modifierState?: TerminalModifierState | undefined
  onModifierStateChange?: ((state: TerminalModifierState) => void) | undefined
}

export interface TerminalHandle {
  sendInput(data: string): void
  sendResize(cols: number, rows: number): void
  reattach(transport: TerminalTransport): void
  focus(): void
  blur(): void
  fit(): void
  pasteText(text: string): void
  getCursorInfo(): { cursorY: number; rows: number; lineHeight: number } | null
  adjustInputPosition(bottomOffset: number): void
  getBufferType(): 'normal' | 'alternate'
}

export const Terminal = forwardRef<TerminalHandle, TerminalProps>(function Terminal(
  {
    machineId,
    terminalId,
    transport,
    className,
    onReady,
    onCursorMove,
    onBufferChange,
    modifierState,
    onModifierStateChange,
  },
  ref,
) {
  const session = useTerminalSession({ machineId, terminalId, transport })
  const containerRef = useRef<HTMLDivElement | null>(null)
  const xtermRef = useRef<XTerm | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const lastWrittenTextRef = useRef('')
  const lastSentResizeRef = useRef<{ cols: number; rows: number } | null>(null)
  const isOpenRef = useRef(false)
  const canSendResizeRef = useRef(false)
  const modifierStateRef = useRef<TerminalModifierState | undefined>(undefined)
  const onModifierStateChangeRef = useRef<((state: TerminalModifierState) => void) | undefined>(undefined)
  const onCursorMoveRef = useRef<(() => void) | undefined>(undefined)
  const onBufferChangeRef = useRef<((isAlternate: boolean) => void) | undefined>(undefined)

  const isOpen = session.snapshot.terminalChannels[terminalId]?.state === 'open'

  const fitAndMaybeSendResize = useCallback(() => {
    const term = xtermRef.current
    const fitAddon = fitAddonRef.current
    const container = containerRef.current
    if (!term || !fitAddon || !container) return

    fitAddon.fit()
    const dimensions = fitAddon.proposeDimensions()
    if (!dimensions) return
    term.resize(dimensions.cols, dimensions.rows)

    if (!canSendResizeRef.current) return
    if (!isOpenRef.current) return
    const last = lastSentResizeRef.current
    if (last?.cols === dimensions.cols && last.rows === dimensions.rows) return
    lastSentResizeRef.current = dimensions
    session.sendResize(dimensions.cols, dimensions.rows)
  }, [session.sendResize])

  useImperativeHandle(ref, () => ({
    sendInput: session.sendInput,
    sendResize: session.sendResize,
    reattach: session.reattach,
    focus: () => {
      xtermRef.current?.focus()
    },
    blur: () => {
      xtermRef.current?.blur()
      containerRef.current?.querySelector('textarea')?.blur()
    },
    fit: fitAndMaybeSendResize,
    pasteText: (text: string) => {
      const isMultiline = text.includes('\n') || text.includes('\r')
      session.sendInput(isMultiline ? `\x1b[200~${text}\x1b[201~` : text)
    },
    getCursorInfo: () => {
      const term = xtermRef.current
      if (!term) return null
      const lineHeight = Math.ceil((term.element?.clientHeight ?? 0) / term.rows) || 20
      return {
        cursorY: term.buffer.active.cursorY,
        rows: term.rows,
        lineHeight,
      }
    },
    adjustInputPosition: (bottomOffset: number) => {
      const element = xtermRef.current?.element
      if (!element) return
      if (bottomOffset > 0) {
        element.classList.add('termx-keyboard-adjusted')
        element.style.setProperty('--termx-keyboard-bottom', `${bottomOffset}px`)
      } else {
        element.classList.remove('termx-keyboard-adjusted')
        element.style.removeProperty('--termx-keyboard-bottom')
      }
    },
    getBufferType: () => xtermRef.current?.buffer.active.type ?? 'normal',
  }), [fitAndMaybeSendResize, session.reattach, session.sendInput, session.sendResize])

  useEffect(() => {
    modifierStateRef.current = modifierState
  }, [modifierState])

  useEffect(() => {
    onModifierStateChangeRef.current = onModifierStateChange
  }, [onModifierStateChange])

  useEffect(() => {
    onCursorMoveRef.current = onCursorMove
  }, [onCursorMove])

  useEffect(() => {
    onBufferChangeRef.current = onBufferChange
  }, [onBufferChange])

  useEffect(() => {
    canSendResizeRef.current = session.resizeControl.canResize
    if (session.resizeControl.canResize) {
      fitAndMaybeSendResize()
    }
  }, [fitAndMaybeSendResize, session.resizeControl.canResize])

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const term = new XTerm({
      allowProposedApi: false,
      cursorBlink: true,
      convertEol: false,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
      fontSize: 14,
      scrollback: 10000,
      theme: {
        background: '#0c0c0c',
        foreground: '#f4f4f5',
        cursor: '#d4d4d8',
        selectionBackground: '#3f3f46',
        black: '#18181b',
        red: '#ef4444',
        green: '#22c55e',
        yellow: '#eab308',
        blue: '#3b82f6',
        magenta: '#d946ef',
        cyan: '#06b6d4',
        white: '#f4f4f5',
        brightBlack: '#71717a',
        brightRed: '#f87171',
        brightGreen: '#4ade80',
        brightYellow: '#fde047',
        brightBlue: '#60a5fa',
        brightMagenta: '#e879f9',
        brightCyan: '#22d3ee',
        brightWhite: '#fafafa',
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(container)
    xtermRef.current = term
    fitAddonRef.current = fitAddon

    const dataDisposable = term.onData((data) => {
      const currentModifiers = modifierStateRef.current
      if (currentModifiers && (currentModifiers.ctrl !== 'off' || currentModifiers.alt !== 'off')) {
        const result = applyTerminalModifiers(data, currentModifiers)
        onModifierStateChangeRef.current?.({ ctrl: result.ctrl, alt: result.alt })
        session.sendInput(result.data)
        return
      }
      session.sendInput(data)
    })
    const cursorDisposable = term.onCursorMove(() => {
      onCursorMoveRef.current?.()
    })
    const bufferDisposable = term.buffer.onBufferChange?.(() => {
      onBufferChangeRef.current?.(term.buffer.active.type === 'alternate')
    }) ?? { dispose() {} }

    fitAndMaybeSendResize()

    let resizeObserver: ResizeObserver | null = null
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => {
        fitAndMaybeSendResize()
      })
      resizeObserver.observe(container)
    }

    const handleVisualViewportResize = () => {
      if (window.visualViewport && containerRef.current) {
        // When keyboard appears, visualViewport.height shrinks.
        // We can limit the container's height to the visual viewport's height relative to its position.
        const rect = containerRef.current.getBoundingClientRect()
        // The bottom of the visual viewport relative to the layout viewport
        const visualViewportBottom = window.visualViewport.offsetTop + window.visualViewport.height
        const visibleHeight = visualViewportBottom - rect.top

        if (visibleHeight < rect.height && visibleHeight > 0) {
          // Keyboard is likely showing, or viewport shrunk
          containerRef.current.style.maxHeight = `${visibleHeight}px`
        } else {
          // Reset
          containerRef.current.style.maxHeight = ''
        }
      }

      // Small delay to let browser finish animating keyboard
      setTimeout(fitAndMaybeSendResize, 100)
    }

    if (window.visualViewport) {
      window.visualViewport.addEventListener('resize', handleVisualViewportResize)
      window.visualViewport.addEventListener('scroll', handleVisualViewportResize)
    }

    return () => {
      if (window.visualViewport) {
        window.visualViewport.removeEventListener('resize', handleVisualViewportResize)
        window.visualViewport.removeEventListener('scroll', handleVisualViewportResize)
      }
      resizeObserver?.disconnect()
      dataDisposable.dispose()
      cursorDisposable.dispose()
      bufferDisposable.dispose()
      term.dispose()
      if (xtermRef.current === term) xtermRef.current = null
      if (fitAddonRef.current === fitAddon) fitAddonRef.current = null
      lastWrittenTextRef.current = ''
      lastSentResizeRef.current = null
      isOpenRef.current = false
    }
  }, [fitAndMaybeSendResize, session.sendInput])

  useEffect(() => {
    isOpenRef.current = isOpen
    if (!isOpen) return
    fitAndMaybeSendResize()
    xtermRef.current?.focus()
    onReady?.()
  }, [fitAndMaybeSendResize, isOpen, onReady])

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
      className={`relative flex flex-col ${className || ''}`}
      data-machine-id={machineId}
      data-terminal-id={terminalId}
      data-phase={session.snapshot.phase}
      data-testid="termx-terminal"
    >
      <div
        ref={containerRef}
        aria-label="Terminal output"
        className="absolute inset-0 p-2 md:p-3 xterm-wrapper outline-none"
        style={{ overscrollBehavior: 'none', touchAction: 'pan-y pan-x' }}
        role="application"
        tabIndex={0}
      />
    </section>
  )
})
