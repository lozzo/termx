import { FitAddon } from '@xterm/addon-fit'
import { Terminal as XTerm } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef } from 'react'
import { applyTerminalModifiers, type TerminalModifierState } from './mobileTerminalInput'
import { useTerminalSession } from './useTerminalSession'
import type { RtcSession } from './transport'

export interface TerminalProps {
  machineId: string
  terminalId: string
  session: RtcSession
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
  reattach(session: RtcSession): void
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
    session,
    className,
    onReady,
    onCursorMove,
    onBufferChange,
    modifierState,
    onModifierStateChange,
  },
  ref,
) {
  const terminalSession = useTerminalSession({ machineId, terminalId, session })
  const containerRef = useRef<HTMLDivElement | null>(null)
  const xtermRef = useRef<XTerm | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const lastWrittenTextRef = useRef('')
  const lastSentResizeRef = useRef<{ cols: number; rows: number } | null>(null)
  const isOpenRef = useRef(false)
  const canSendResizeRef = useRef(false)
  const fitFrameRef = useRef<number | null>(null)
  const fitDelayRef = useRef<number | null>(null)
  const terminalDisposedRef = useRef(true)
  const terminalGenerationRef = useRef(0)
  const modifierStateRef = useRef<TerminalModifierState | undefined>(undefined)
  const onModifierStateChangeRef = useRef<((state: TerminalModifierState) => void) | undefined>(undefined)
  const onCursorMoveRef = useRef<(() => void) | undefined>(undefined)
  const onBufferChangeRef = useRef<((isAlternate: boolean) => void) | undefined>(undefined)

  const isOpen = terminalSession.snapshot.terminalChannels[terminalId]?.state === 'open'
  const channelState = terminalSession.snapshot.terminalChannels[terminalId]?.state
  const showConnectingOverlay = channelState !== 'open' && terminalSession.terminalText.length === 0

  const fitAndMaybeSendResize = useCallback(() => {
    if (terminalDisposedRef.current) return
    const term = xtermRef.current
    const fitAddon = fitAddonRef.current
    const container = containerRef.current
    if (!term || !fitAddon || !container) return

    let dimensions: { cols: number; rows: number } | undefined
    try {
      fitAddon.fit()
      dimensions = fitAddon.proposeDimensions()
      if (!dimensions) return
      if (terminalDisposedRef.current || xtermRef.current !== term) return
      term.resize(dimensions.cols, dimensions.rows)
    } catch {
      // xterm can leave delayed viewport/fit work behind while React is unmounting.
      // Treat those races as stale lifecycle work instead of crashing the UI.
      return
    }

    if (!canSendResizeRef.current) return
    if (!isOpenRef.current) return
    const last = lastSentResizeRef.current
    if (last?.cols === dimensions.cols && last.rows === dimensions.rows) return
    lastSentResizeRef.current = dimensions
    terminalSession.sendResize(dimensions.cols, dimensions.rows)
  }, [terminalSession.sendResize])

  const scheduleFit = useCallback(() => {
    if (terminalDisposedRef.current) return
    const generation = terminalGenerationRef.current
    if (typeof window === 'undefined' || typeof window.requestAnimationFrame !== 'function') {
      fitAndMaybeSendResize()
      return
    }
    if (fitFrameRef.current !== null) {
      window.cancelAnimationFrame(fitFrameRef.current)
      fitFrameRef.current = null
    }
    fitFrameRef.current = window.requestAnimationFrame(() => {
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) {
        fitFrameRef.current = null
        return
      }
      fitFrameRef.current = window.requestAnimationFrame(() => {
        fitFrameRef.current = null
        if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
        fitAndMaybeSendResize()
      })
    })
  }, [fitAndMaybeSendResize])

  useImperativeHandle(ref, () => ({
    sendInput: terminalSession.sendInput,
    sendResize: terminalSession.sendResize,
    reattach: terminalSession.reattach,
    focus: () => {
      if (terminalDisposedRef.current) return
      try {
        xtermRef.current?.focus()
      } catch {
        // Ignore stale focus calls after xterm has been disposed.
      }
    },
    blur: () => {
      if (terminalDisposedRef.current) return
      try {
        xtermRef.current?.blur()
      } catch {
        // Ignore stale blur calls after xterm has been disposed.
      }
      containerRef.current?.querySelector('textarea')?.blur()
    },
    fit: () => {
      fitAndMaybeSendResize()
      scheduleFit()
    },
    pasteText: (text: string) => {
      const isMultiline = text.includes('\n') || text.includes('\r')
      terminalSession.sendInput(isMultiline ? `\x1b[200~${text}\x1b[201~` : text)
    },
    getCursorInfo: () => {
      if (terminalDisposedRef.current) return null
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
      if (terminalDisposedRef.current) return
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
    getBufferType: () => terminalDisposedRef.current ? 'normal' : xtermRef.current?.buffer.active.type ?? 'normal',
  }), [fitAndMaybeSendResize, scheduleFit, terminalSession.reattach, terminalSession.sendInput, terminalSession.sendResize])

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
    canSendResizeRef.current = terminalSession.resizeControl.canResize
    if (terminalSession.resizeControl.canResize) {
      fitAndMaybeSendResize()
    }
  }, [fitAndMaybeSendResize, terminalSession.resizeControl.canResize])

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const generation = terminalGenerationRef.current + 1
    terminalGenerationRef.current = generation
    terminalDisposedRef.current = false

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
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
      const currentModifiers = modifierStateRef.current
      if (currentModifiers && (currentModifiers.ctrl !== 'off' || currentModifiers.alt !== 'off')) {
        const result = applyTerminalModifiers(data, currentModifiers)
        onModifierStateChangeRef.current?.({ ctrl: result.ctrl, alt: result.alt })
        terminalSession.sendInput(result.data)
        return
      }
      terminalSession.sendInput(data)
    })
    const cursorDisposable = term.onCursorMove(() => {
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
      onCursorMoveRef.current?.()
    })
    const bufferDisposable = term.buffer.onBufferChange?.(() => {
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
      onBufferChangeRef.current?.(term.buffer.active.type === 'alternate')
    }) ?? { dispose() {} }

    fitAndMaybeSendResize()
    scheduleFit()

    let resizeObserver: ResizeObserver | null = null
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => {
        if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
        scheduleFit()
      })
      resizeObserver.observe(container)
    }

    const handleVisualViewportResize = () => {
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
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
      if (fitDelayRef.current !== null) window.clearTimeout(fitDelayRef.current)
      fitDelayRef.current = window.setTimeout(() => {
        fitDelayRef.current = null
        if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
        scheduleFit()
      }, 100)
    }

    if (window.visualViewport) {
      window.visualViewport.addEventListener('resize', handleVisualViewportResize)
      window.visualViewport.addEventListener('scroll', handleVisualViewportResize)
    }

    return () => {
      terminalDisposedRef.current = true
      terminalGenerationRef.current += 1
      if (window.visualViewport) {
        window.visualViewport.removeEventListener('resize', handleVisualViewportResize)
        window.visualViewport.removeEventListener('scroll', handleVisualViewportResize)
      }
      if (fitFrameRef.current !== null) {
        window.cancelAnimationFrame(fitFrameRef.current)
        fitFrameRef.current = null
      }
      if (fitDelayRef.current !== null) {
        window.clearTimeout(fitDelayRef.current)
        fitDelayRef.current = null
      }
      resizeObserver?.disconnect()
      dataDisposable.dispose()
      cursorDisposable.dispose()
      bufferDisposable.dispose()
      if (xtermRef.current === term) xtermRef.current = null
      if (fitAddonRef.current === fitAddon) fitAddonRef.current = null
      try {
        term.dispose()
      } catch {
        // Ignore dispose races from xterm internals during React teardown.
      }
      lastWrittenTextRef.current = ''
      lastSentResizeRef.current = null
      isOpenRef.current = false
    }
  }, [fitAndMaybeSendResize, scheduleFit, terminalSession.sendInput])

  useEffect(() => {
    isOpenRef.current = isOpen
    if (!isOpen) return
    fitAndMaybeSendResize()
    scheduleFit()
    if (!terminalDisposedRef.current) {
      try {
        xtermRef.current?.focus()
      } catch {
        // Ignore stale focus calls after xterm has been disposed.
      }
    }
    onReady?.()
  }, [fitAndMaybeSendResize, isOpen, onReady, scheduleFit])

  useEffect(() => {
    if (terminalDisposedRef.current) return
    const term = xtermRef.current
    if (!term) return

    const nextText = terminalSession.terminalText
    const previousText = lastWrittenTextRef.current
    if (nextText === previousText) return

    try {
      if (nextText.startsWith(previousText)) {
        term.write(nextText.slice(previousText.length))
      } else {
        term.reset()
        term.write(nextText)
      }
    } catch (error) {
      if (!terminalDisposedRef.current) throw error
    }
    lastWrittenTextRef.current = nextText
  }, [terminalSession.terminalText])

  return (
    <section
      className={`relative flex h-full min-h-0 w-full flex-col overflow-hidden ${className || ''}`}
      data-machine-id={machineId}
      data-terminal-id={terminalId}
      data-phase={terminalSession.snapshot.phase}
      data-testid="termx-terminal"
    >
      <div
        ref={containerRef}
        aria-label="Terminal output"
        className="absolute inset-0 min-h-0 overflow-hidden px-1 py-1 md:p-3 xterm-wrapper outline-none"
        style={{ overscrollBehavior: 'none', touchAction: 'pan-y pan-x' }}
        role="application"
        tabIndex={0}
      />
      {showConnectingOverlay ? (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-black text-sm font-medium text-zinc-500">
          <div className="flex items-center gap-2">
            <div className="h-4 w-4 animate-spin rounded-full border-2 border-zinc-700 border-t-zinc-400" />
            Connecting terminal...
          </div>
        </div>
      ) : null}
    </section>
  )
})
