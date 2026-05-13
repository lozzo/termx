import { FitAddon } from '@xterm/addon-fit'
import { CanvasAddon } from '@xterm/addon-canvas'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal as XTerm } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { hapticSelection } from '../platform/haptics'
import { addNativeKeyboardListener } from '../platform/nativeKeyboard'
import { applyTerminalModifiers, type TerminalModifierState } from './mobileTerminalInput'
import type { TerminalResizeControl, TerminalScrollbackLoadResult } from './terminalClient'
import { logTerminalDiagnostic, terminalNow } from './terminalDiagnostics'
import { appendTerminalText } from './terminalTextWindow'
import { useTerminalSession } from './useTerminalSession'
import type { RtcSession } from '../core/transport'
import { DEFAULT_TERMINAL_SETTINGS, resolveTerminalTheme, type TerminalSettings } from './terminalSettings'

const historyScrollbackPageRows = 250
const historyApplyBatchDelayMs = 160
const historyApplyScrollIdleMs = 180
const historyApplyMaxDelayMs = 900
const historyApplyWatchdogMs = 4000
const bottomAnchorSettleMs = 900
const xtermWriteStatsIntervalMs = 1000
const xtermWriteSlowMs = 500
const xtermWriteLargeChars = 128 * 1024
const liveOutputPendingSoftLimitChars = 512 * 1024
const liveOutputPendingHardLimitChars = 1024 * 1024
const liveOutputWatchdogMs = 4000
const eventLoopProbeIntervalMs = 1000
const eventLoopLagWarnMs = 250

function isNativeAndroidWebView(): boolean {
  if (typeof navigator === 'undefined') return false
  const userAgent = navigator.userAgent || ''
  const cap = globalThis as typeof globalThis & { Capacitor?: { isNativePlatform?: () => boolean } }
  return /Android/i.test(userAgent) && cap.Capacitor?.isNativePlatform?.() === true
}

function effectiveRendererMode(configuredRenderer: TerminalRenderer): TerminalRenderer {
  if (configuredRenderer === 'auto' && isNativeAndroidWebView()) {
    return 'canvas'
  }
  return configuredRenderer
}

interface PendingHistoryApply {
  revision: number
  loadedRows: number
  prependedRows: number
  restoreViewportY: number | null
  text: string
}

export type TerminalRenderer = 'auto' | 'webgl' | 'canvas' | 'dom'

export interface TerminalProps {
  machineId: string
  terminalId: string
  session: RtcSession
  className?: string
  onReady?: () => void
  onInput?: ((data: string) => void) | undefined
  onCursorMove?: (() => void) | undefined
  onBufferChange?: ((isAlternate: boolean) => void) | undefined
  onResizeControl?: ((control: TerminalResizeControl) => void) | undefined
  modifierState?: TerminalModifierState | undefined
  onModifierStateChange?: ((state: TerminalModifierState) => void) | undefined
  selectionMode?: boolean | undefined
  renderer?: TerminalRenderer | undefined
  settings?: TerminalSettings | undefined
  preventFocus?: boolean | undefined
  suppressConnectingOverlay?: boolean | undefined
}

export interface TerminalHandle {
  sendInput(data: string): void
  sendResize(cols: number, rows: number): void
  requestResizeOwner(): Promise<TerminalResizeControl>
  releaseResizeOwner(): Promise<TerminalResizeControl>
  reattach(session: RtcSession, options?: { forceTerminalChannel?: boolean }): void
  focus(): void
  blur(): void
  fit(): void
  pasteText(text: string): void
  selectAll(): void
  selectVisible(): void
  getSelection(): string
  hasSelection(): boolean
  clearSelection(): void
  getCursorInfo(): { cursorY: number; rows: number; lineHeight: number } | null
  adjustInputPosition(bottomOffset: number): void
  getBufferType(): 'normal' | 'alternate'
  updateOptions(opts: { fontSize?: number; cursorBlink?: boolean; fontFamily?: string; scrollback?: number }): void
}

export const Terminal = forwardRef<TerminalHandle, TerminalProps>(function Terminal(
  {
    machineId,
    terminalId,
    session,
    className,
    onReady,
    onInput,
    onCursorMove,
    onBufferChange,
    onResizeControl,
    modifierState,
    onModifierStateChange,
    selectionMode = false,
    renderer,
    settings = DEFAULT_TERMINAL_SETTINGS,
    preventFocus = false,
    suppressConnectingOverlay = false,
  },
  ref,
) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const xtermRef = useRef<XTerm | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const lastWrittenTextRef = useRef('')
  const latestTerminalTextRef = useRef('')
  const lastSentResizeRef = useRef<{ cols: number; rows: number } | null>(null)
  const isOpenRef = useRef(false)
  const pendingLiveOutputRef = useRef('')
  const liveOutputInFlightRef = useRef(false)
  const liveOutputGenerationRef = useRef(0)
  const liveOutputDroppedCharsRef = useRef(0)
  const liveOutputSyncLostRef = useRef(false)
  const liveOutputWatchdogRef = useRef<number | null>(null)
  const markLiveOutputSyncLostRef = useRef<(reason?: string) => void>(() => {})
  const surfaceReadyRef = useRef(false)
  const canSendResizeRef = useRef(false)
  const historyLoadingRef = useRef(false)
  const historyApplyingRef = useRef(false)
  const historyRestoreViewportOnLoadRef = useRef(false)
  const historyRevisionAppliedRef = useRef(0)
  const historyLoadedRowsAppliedRef = useRef(0)
  const lastSnapshotTextRef = useRef('')
  const recoveryRevisionAppliedRef = useRef(0)
  const pendingHistoryViewportRef = useRef<number | null>(null)
  const pendingHistoryApplyRef = useRef<PendingHistoryApply | null>(null)
  const historyApplyQueuedAtRef = useRef(0)
  const historyApplyTimerRef = useRef<number | null>(null)
  const historyLoadArmedByUserRef = useRef(false)
  const lastHistoryScrollActivityAtRef = useRef(0)
  const loadScrollbackRef = useRef<(limit?: number) => Promise<TerminalScrollbackLoadResult>>(async () => ({
    loadedRows: 0,
    totalRows: 0,
    hasMore: false,
  }))
  const maybePrefetchScrollbackRef = useRef<() => void>(() => {})
  const scheduleHistoryApplyRef = useRef<() => void>(() => {})
  const hasTerminalSnapshotRef = useRef(false)
  const snapshotAlternateScreenRef = useRef(false)
  const fitFrameRef = useRef<number | null>(null)
  const preventFocusRef = useRef(preventFocus)
  const bottomAnchorUntilRef = useRef(0)
  const bottomAnchorFrameRef = useRef<number | null>(null)
  const xtermWriteStatsRef = useRef({
    writes: 0,
    chars: 0,
    pendingCallbacks: 0,
    lastLogAt: terminalNow(),
    lastChars: 0,
    lastWrites: 0,
  })

  useEffect(() => {
    preventFocusRef.current = preventFocus
  }, [preventFocus])
  const terminalDisposedRef = useRef(true)
  const terminalGenerationRef = useRef(0)
  const settingsRef = useRef(settings)
  const modifierStateRef = useRef<TerminalModifierState | undefined>(undefined)
  const onModifierStateChangeRef = useRef<((state: TerminalModifierState) => void) | undefined>(undefined)
  const onInputRef = useRef<((data: string) => void) | undefined>(undefined)
  const onCursorMoveRef = useRef<(() => void) | undefined>(undefined)
  const onBufferChangeRef = useRef<((isAlternate: boolean) => void) | undefined>(undefined)
  const onResizeControlRef = useRef<((control: TerminalResizeControl) => void) | undefined>(undefined)
  const selectionModeRef = useRef(false)
  const selectionResetHandlersRef = useRef(new Set<() => void>())
  const [surfaceReady, setSurfaceReady] = useState(false)

  const logTerminal = useCallback((event: string, input: {
    level?: 'debug' | 'info' | 'warn' | 'error'
    details?: Record<string, unknown> | undefined
  } = {}) => {
    logTerminalDiagnostic(`xterm.${event}`, {
      level: input.level,
      machineId,
      terminalId,
      details: input.details,
    })
  }, [machineId, terminalId])

  const writeToXterm = useCallback((
    term: XTerm,
    text: string,
    reason: string,
    onDone?: () => void,
  ) => {
    const stats = xtermWriteStatsRef.current
    const startedAt = terminalNow()
    stats.writes += 1
    stats.chars += text.length
    stats.pendingCallbacks += 1
    const writeId = stats.writes
    const now = terminalNow()
    if (text.length >= xtermWriteLargeChars || now - stats.lastLogAt >= xtermWriteStatsIntervalMs) {
      const elapsedSeconds = Math.max(0.001, (now - stats.lastLogAt) / 1000)
      const intervalChars = stats.chars - stats.lastChars
      const intervalWrites = stats.writes - stats.lastWrites
      logTerminal('write_start', {
        level: text.length >= xtermWriteLargeChars ? 'warn' : 'debug',
        details: {
          writeId,
          reason,
          chars: text.length,
          writes: stats.writes,
          totalChars: stats.chars,
          intervalWrites,
          intervalChars,
          charsPerSecond: Math.round(intervalChars / elapsedSeconds),
          pendingCallbacks: stats.pendingCallbacks,
        },
      })
      stats.lastLogAt = now
      stats.lastChars = stats.chars
      stats.lastWrites = stats.writes
    }
    let done = false
    const finish = () => {
      if (done) return
      done = true
      stats.pendingCallbacks = Math.max(0, stats.pendingCallbacks - 1)
      const elapsedMs = Math.round(terminalNow() - startedAt)
      if (elapsedMs >= xtermWriteSlowMs || text.length >= xtermWriteLargeChars) {
        logTerminal('write_done', {
          level: elapsedMs >= xtermWriteSlowMs ? 'warn' : 'debug',
          details: {
            writeId,
            reason,
            chars: text.length,
            elapsedMs,
            pendingCallbacks: stats.pendingCallbacks,
            bufferLength: term.buffer.active.length,
            viewportY: term.buffer.active.viewportY,
            rows: term.rows,
            cols: term.cols,
          },
        })
      }
      onDone?.()
    }
    try {
      term.write(text, finish)
    } catch (error) {
      stats.pendingCallbacks = Math.max(0, stats.pendingCallbacks - 1)
      logTerminal('write_error', {
        level: 'error',
        details: {
          writeId,
          reason,
          chars: text.length,
          message: error instanceof Error ? error.message : String(error),
        },
      })
      throw error
    }
  }, [logTerminal])

  const clearLiveOutputWatchdog = useCallback(() => {
    if (liveOutputWatchdogRef.current === null) return
    window.clearTimeout(liveOutputWatchdogRef.current)
    liveOutputWatchdogRef.current = null
  }, [])

  const markSurfaceReady = useCallback(() => {
    if (surfaceReadyRef.current) return
    surfaceReadyRef.current = true
    setSurfaceReady(true)
  }, [])

  const flushPendingTerminalOutput = useCallback(() => {
    if (terminalDisposedRef.current) return
    if (liveOutputInFlightRef.current) return
    const term = xtermRef.current
    const pending = pendingLiveOutputRef.current
    if (!term || !pending) return
    pendingLiveOutputRef.current = ''
    liveOutputInFlightRef.current = true
    liveOutputSyncLostRef.current = false
    const writeGeneration = liveOutputGenerationRef.current + 1
    liveOutputGenerationRef.current = writeGeneration
    clearLiveOutputWatchdog()
    liveOutputWatchdogRef.current = window.setTimeout(() => {
      liveOutputWatchdogRef.current = null
      if (!liveOutputInFlightRef.current || terminalDisposedRef.current || liveOutputGenerationRef.current !== writeGeneration) return
      const droppedChars = pendingLiveOutputRef.current.length + liveOutputDroppedCharsRef.current
      pendingLiveOutputRef.current = ''
      liveOutputDroppedCharsRef.current = 0
      liveOutputSyncLostRef.current = true
      liveOutputInFlightRef.current = false
      liveOutputGenerationRef.current += 1
      logTerminal('live_output_write_stalled', {
        level: 'warn',
        details: {
          inFlightChars: pending.length,
          droppedChars,
          watchdogMs: liveOutputWatchdogMs,
        },
      })
      markLiveOutputSyncLostRef.current('xterm renderer stalled while applying live output')
    }, liveOutputWatchdogMs)
    writeToXterm(term, pending, 'stream_output', () => {
      if (liveOutputGenerationRef.current !== writeGeneration) return
      clearLiveOutputWatchdog()
      const syncLost = liveOutputSyncLostRef.current
      liveOutputSyncLostRef.current = false
      liveOutputInFlightRef.current = false
      markSurfaceReady()
      if (!syncLost) {
        flushPendingTerminalOutput()
      }
    })
    lastWrittenTextRef.current = appendTerminalText(lastWrittenTextRef.current, pending)
  }, [clearLiveOutputWatchdog, logTerminal, markSurfaceReady, writeToXterm])

  const handleTerminalOutput = useCallback((text: string) => {
    if (terminalDisposedRef.current) return
    if (liveOutputSyncLostRef.current) return
    const nextLength = pendingLiveOutputRef.current.length + text.length
    if (liveOutputInFlightRef.current && nextLength > liveOutputPendingSoftLimitChars) {
      const droppedChars = nextLength + liveOutputDroppedCharsRef.current
      pendingLiveOutputRef.current = ''
      liveOutputDroppedCharsRef.current = 0
      liveOutputSyncLostRef.current = true
      liveOutputGenerationRef.current += 1
      const hardLimitExceeded = nextLength > liveOutputPendingHardLimitChars
      logTerminal(hardLimitExceeded ? 'live_output_pending_hard_limit' : 'live_output_pending_soft_limit', {
        level: 'warn',
        details: {
          incomingChars: text.length,
          droppedChars,
          softLimitChars: liveOutputPendingSoftLimitChars,
          hardLimitChars: liveOutputPendingHardLimitChars,
        },
      })
      markLiveOutputSyncLostRef.current(hardLimitExceeded
        ? 'xterm live output buffer exceeded hard limit'
        : 'xterm live output buffer exceeded soft limit')
      return
    }
    pendingLiveOutputRef.current += text
    flushPendingTerminalOutput()
  }, [flushPendingTerminalOutput, logTerminal])

  const terminalSession = useTerminalSession({
    machineId,
    terminalId,
    session,
    onOutput: handleTerminalOutput,
  })
  markLiveOutputSyncLostRef.current = terminalSession.markSyncLost

  latestTerminalTextRef.current = terminalSession.terminalText
  settingsRef.current = settings
  hasTerminalSnapshotRef.current = terminalSession.terminalSnapshot !== null
  snapshotAlternateScreenRef.current = terminalSession.terminalSnapshot?.alternateScreen === true

  useEffect(() => {
    loadScrollbackRef.current = terminalSession.loadScrollback
  }, [terminalSession.loadScrollback])

  useEffect(() => {
    surfaceReadyRef.current = false
    setSurfaceReady(false)
  }, [machineId, session, terminalId])

  const isOpen = terminalSession.snapshot.terminalChannels[terminalId]?.state === 'open'
  const channelState = terminalSession.snapshot.terminalChannels[terminalId]?.state
  const showConnectingOverlay = !suppressConnectingOverlay && (channelState !== 'open' || !surfaceReady)

  const isScrolledToBottom = useCallback((term: XTerm) => {
    const buffer = term.buffer.active
    return buffer.viewportY >= Math.max(0, buffer.length - term.rows - 1)
  }, [])

  const fitAndMaybeSendResize = useCallback(() => {
    if (terminalDisposedRef.current) return
    const term = xtermRef.current
    const fitAddon = fitAddonRef.current
    const container = containerRef.current
    if (!term || !fitAddon || !container) return
    const shouldKeepBottom = Date.now() <= bottomAnchorUntilRef.current || isScrolledToBottom(term)

    let dimensions: { cols: number; rows: number } | undefined
    try {
      fitAddon.fit()
      dimensions = fitAddon.proposeDimensions()
      if (!dimensions) return
      if (terminalDisposedRef.current || xtermRef.current !== term) return
      term.resize(dimensions.cols, dimensions.rows)
      if (shouldKeepBottom) term.scrollToBottom()
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
  }, [isScrolledToBottom, terminalSession.sendResize])

  const scrollToBottomIfCurrent = useCallback((term: XTerm) => {
    if (terminalDisposedRef.current || xtermRef.current !== term) return
    try {
      term.scrollToBottom()
    } catch (error) {
      if (!terminalDisposedRef.current) throw error
    }
  }, [])

  const cancelBottomAnchor = useCallback(() => {
    bottomAnchorUntilRef.current = 0
    if (bottomAnchorFrameRef.current !== null && typeof window !== 'undefined') {
      window.cancelAnimationFrame(bottomAnchorFrameRef.current)
      bottomAnchorFrameRef.current = null
    }
  }, [])

  const keepBottomAnchored = useCallback(() => {
    if (terminalDisposedRef.current) return
    const term = xtermRef.current
    if (!term) return
    bottomAnchorUntilRef.current = Date.now() + bottomAnchorSettleMs
    scrollToBottomIfCurrent(term)
    if (typeof window === 'undefined' || typeof window.requestAnimationFrame !== 'function') return
    if (bottomAnchorFrameRef.current !== null) {
      window.cancelAnimationFrame(bottomAnchorFrameRef.current)
      bottomAnchorFrameRef.current = null
    }
    const generation = terminalGenerationRef.current
    const tick = () => {
      if (
        terminalDisposedRef.current ||
        terminalGenerationRef.current !== generation ||
        Date.now() > bottomAnchorUntilRef.current
      ) {
        bottomAnchorFrameRef.current = null
        return
      }
      const current = xtermRef.current
      if (current) scrollToBottomIfCurrent(current)
      bottomAnchorFrameRef.current = window.requestAnimationFrame(tick)
    }
    bottomAnchorFrameRef.current = window.requestAnimationFrame(tick)
  }, [scrollToBottomIfCurrent])

  const currentTerminalSize = useCallback(() => {
    if (terminalDisposedRef.current) return undefined
    const term = xtermRef.current
    const fitAddon = fitAddonRef.current
    if (!term || !fitAddon) return undefined
    try {
      fitAddon.fit()
      const dimensions = fitAddon.proposeDimensions()
      if (!dimensions) return undefined
      if (terminalDisposedRef.current || xtermRef.current !== term) return undefined
      term.resize(dimensions.cols, dimensions.rows)
      return dimensions
    } catch {
      return undefined
    }
  }, [])

  const sendInputAtCurrentSize = useCallback((data: string) => {
    terminalSession.sendInput(data, currentTerminalSize())
  }, [currentTerminalSize, terminalSession.sendInput])

  const sendUserInput = useCallback((data: string) => {
    const delegateInput = onInputRef.current
    if (delegateInput) {
      delegateInput(data)
      return
    }
    sendInputAtCurrentSize(data)
  }, [sendInputAtCurrentSize])

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
    sendInput: sendInputAtCurrentSize,
    sendResize: terminalSession.sendResize,
    requestResizeOwner: () => terminalSession.requestResizeOwner(currentTerminalSize()),
    releaseResizeOwner: terminalSession.releaseResizeOwner,
    reattach: terminalSession.reattach,
    focus: () => {
      if (terminalDisposedRef.current || preventFocusRef.current) return
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
      sendInputAtCurrentSize(isMultiline ? `\x1b[200~${text}\x1b[201~` : text)
    },
    selectAll: () => {
      if (terminalDisposedRef.current) return
      xtermRef.current?.selectAll()
    },
    selectVisible: () => {
      if (terminalDisposedRef.current) return
      const term = xtermRef.current
      if (!term) return
      const startRow = term.buffer.active.viewportY
      term.select(0, startRow, term.cols * term.rows)
    },
    getSelection: () => terminalDisposedRef.current ? '' : xtermRef.current?.getSelection() ?? '',
    hasSelection: () => terminalDisposedRef.current ? false : xtermRef.current?.hasSelection() ?? false,
    clearSelection: () => {
      if (terminalDisposedRef.current) return
      xtermRef.current?.clearSelection()
      for (const resetSelection of selectionResetHandlersRef.current) resetSelection()
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
      const compositionOverlay = containerRef.current?.querySelector('.comp-overlay') as HTMLElement | null
      if (bottomOffset > 0) {
        element.classList.add('input-adjusted')
        element.style.setProperty('--kb-input-bottom', `${bottomOffset}px`)
        if (compositionOverlay) compositionOverlay.style.bottom = `${bottomOffset}px`
      } else {
        element.classList.remove('input-adjusted')
        element.style.removeProperty('--kb-input-bottom')
        if (compositionOverlay) compositionOverlay.style.bottom = ''
      }
    },
    getBufferType: () => terminalDisposedRef.current ? 'normal' : xtermRef.current?.buffer.active.type ?? 'normal',
    updateOptions: (opts) => {
      if (terminalDisposedRef.current || !xtermRef.current) return
      if (opts.fontSize !== undefined) xtermRef.current.options.fontSize = opts.fontSize
      if (opts.cursorBlink !== undefined) xtermRef.current.options.cursorBlink = opts.cursorBlink
      if (opts.fontFamily !== undefined) xtermRef.current.options.fontFamily = opts.fontFamily
      if (opts.scrollback !== undefined) xtermRef.current.options.scrollback = opts.scrollback
      settingsRef.current = {
        ...settingsRef.current,
        ...(opts.fontSize !== undefined ? { fontSize: opts.fontSize } : {}),
        ...(opts.cursorBlink !== undefined ? { cursorBlink: opts.cursorBlink } : {}),
        ...(opts.fontFamily !== undefined ? { fontFamily: opts.fontFamily } : {}),
        ...(opts.scrollback !== undefined ? { scrollback: opts.scrollback } : {}),
      }
      fitAndMaybeSendResize()
    },
  }), [currentTerminalSize, fitAndMaybeSendResize, scheduleFit, sendInputAtCurrentSize, terminalSession.reattach, terminalSession.releaseResizeOwner, terminalSession.requestResizeOwner, terminalSession.sendResize])

  useEffect(() => {
    modifierStateRef.current = modifierState
  }, [modifierState])

  useEffect(() => {
    const term = xtermRef.current
    if (!term) return
    term.options.fontSize = settings.fontSize
    term.options.fontFamily = settings.fontFamily
    term.options.cursorBlink = settings.cursorBlink
    term.options.scrollback = settings.scrollback
    term.options.theme = resolveTerminalTheme(settings.themeId)
    fitAndMaybeSendResize()
  }, [fitAndMaybeSendResize, settings])

  useEffect(() => {
    onModifierStateChangeRef.current = onModifierStateChange
  }, [onModifierStateChange])

  useEffect(() => {
    onInputRef.current = onInput
  }, [onInput])

  useEffect(() => {
    onCursorMoveRef.current = onCursorMove
  }, [onCursorMove])

  useEffect(() => {
    onBufferChangeRef.current = onBufferChange
  }, [onBufferChange])

  useEffect(() => {
    onResizeControlRef.current = onResizeControl
  }, [onResizeControl])

  useEffect(() => {
    selectionModeRef.current = selectionMode
    if (!selectionMode) {
      for (const resetSelection of selectionResetHandlersRef.current) resetSelection()
    }
  }, [selectionMode])

  useEffect(() => {
    canSendResizeRef.current = terminalSession.resizeControl.canResize
    onResizeControlRef.current?.(terminalSession.resizeControl)
    if (terminalSession.resizeControl.canResize) {
      fitAndMaybeSendResize()
    }
  }, [fitAndMaybeSendResize, terminalSession.resizeControl])

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const generation = terminalGenerationRef.current + 1
    terminalGenerationRef.current = generation
    terminalDisposedRef.current = false
    if (historyApplyTimerRef.current !== null) {
      window.clearTimeout(historyApplyTimerRef.current)
      historyApplyTimerRef.current = null
    }
    pendingHistoryApplyRef.current = null
    historyApplyQueuedAtRef.current = 0
    historyLoadArmedByUserRef.current = false
    lastHistoryScrollActivityAtRef.current = 0
    historyApplyingRef.current = false
    historyRevisionAppliedRef.current = 0
    historyLoadedRowsAppliedRef.current = 0
    lastSnapshotTextRef.current = ''
    recoveryRevisionAppliedRef.current = 0

    const term = new XTerm({
      allowProposedApi: false,
      cursorBlink: settingsRef.current.cursorBlink,
      convertEol: false,
      fontFamily: settingsRef.current.fontFamily,
      fontSize: settingsRef.current.fontSize,
      scrollback: settingsRef.current.scrollback,
      theme: resolveTerminalTheme(settingsRef.current.themeId),
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(container)
    if (term.element) term.element.style.overflow = 'hidden'
    const coreWithViewport = term as XTerm & { _core?: { viewport?: { scrollBarWidth?: number } } }
    if (coreWithViewport._core?.viewport) {
      coreWithViewport._core.viewport.scrollBarWidth = 0
    }
    const isMobile = typeof window !== 'undefined' && window.innerWidth < 768
    const textarea = container.querySelector('.xterm-helper-textarea') as HTMLTextAreaElement | null
    const compositionOverlay = document.createElement('div')
    compositionOverlay.className = 'comp-overlay'
    term.element?.append(compositionOverlay)
    const handleCompositionStart = () => {
      if (!isMobile) return
      compositionOverlay.style.display = 'block'
      compositionOverlay.textContent = ''
    }
    const handleCompositionUpdate = (event: CompositionEvent) => {
      if (!isMobile) return
      compositionOverlay.textContent = event.data
    }
    const handleCompositionEnd = () => {
      compositionOverlay.style.display = 'none'
      compositionOverlay.textContent = ''
    }
    textarea?.addEventListener('compositionstart', handleCompositionStart)
    textarea?.addEventListener('compositionupdate', handleCompositionUpdate)
    textarea?.addEventListener('compositionend', handleCompositionEnd)
    const onFocus = () => { if (preventFocusRef.current && textarea) textarea.blur() }
    textarea?.addEventListener('focus', onFocus)
    let activeRenderer: TerminalRenderer | 'canvas-fallback' = 'dom'
    const loadCanvasRenderer = (reason: 'requested' | 'fallback') => {
      try {
        term.loadAddon(new CanvasAddon())
        activeRenderer = reason === 'requested' ? 'canvas' : 'canvas-fallback'
      } catch {
        // Fall back to xterm's DOM renderer when canvas is unavailable.
        activeRenderer = 'dom'
      }
    }
    const isTestDom = typeof navigator !== 'undefined' && navigator.userAgent.includes('jsdom')
    if (!isTestDom) {
      const configuredRenderer = renderer ?? settingsRef.current.renderer
      const rendererMode = effectiveRendererMode(configuredRenderer)
      if (rendererMode === 'canvas') {
        loadCanvasRenderer('requested')
      } else if (rendererMode !== 'dom') {
        // auto or webgl: try WebGL first, fall back to canvas
        try {
          const webglAddon = new WebglAddon(true)
          webglAddon.onContextLoss(() => {
            webglAddon.dispose()
            if (rendererMode !== 'webgl') loadCanvasRenderer('fallback')
            term.refresh(0, term.rows - 1)
            logTerminal('renderer_context_loss', {
              level: 'warn',
              details: {
                configuredRenderer,
                effectiveRenderer: rendererMode,
                fallbackRenderer: activeRenderer,
              },
            })
          })
          term.loadAddon(webglAddon)
          activeRenderer = 'webgl'
        } catch {
          if (rendererMode !== 'webgl') {
            loadCanvasRenderer('fallback')
          } else {
            activeRenderer = 'dom'
          }
        }
      }
      logTerminal('renderer_selected', {
        level: 'info',
        details: {
          configuredRenderer,
          effectiveRenderer: rendererMode,
          activeRenderer,
          nativeAndroidWebView: isNativeAndroidWebView(),
        },
      })
    }
    xtermRef.current = term
    fitAddonRef.current = fitAddon

    // Write existing session text so the terminal isn't blank after renderer switch
    const initialText = latestTerminalTextRef.current
    if (initialText) {
      try {
        writeToXterm(term, initialText, 'initial_text', () => {
          markSurfaceReady()
          keepBottomAnchored()
        })
      } catch {}
      lastWrittenTextRef.current = initialText
    }

    const dataDisposable = term.onData((data) => {
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
      const currentModifiers = modifierStateRef.current
      if (currentModifiers && (currentModifiers.ctrl !== 'off' || currentModifiers.alt !== 'off')) {
        const result = applyTerminalModifiers(data, currentModifiers)
        onModifierStateChangeRef.current?.({ ctrl: result.ctrl, alt: result.alt })
        sendUserInput(result.data)
        return
      }
      sendUserInput(data)
    })
    const binaryDisposable = term.onBinary((data) => {
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
      sendUserInput(data)
    })
    term.attachCustomKeyEventHandler((event) => {
      if (event.type !== 'keydown') return true
      const currentModifiers = modifierStateRef.current
      if (!currentModifiers || (currentModifiers.ctrl === 'off' && currentModifiers.alt === 'off')) return true
      if (['Control', 'Alt', 'Shift', 'Meta'].includes(event.key)) return true
      if (event.key.length !== 1) return true

      const result = applyTerminalModifiers(event.key, currentModifiers)
      onModifierStateChangeRef.current?.({ ctrl: result.ctrl, alt: result.alt })
      sendUserInput(result.data)
      return false
    })
    const cursorDisposable = term.onCursorMove(() => {
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
      onCursorMoveRef.current?.()
    })
    const coreTerminal = term as XTerm & {
      _core?: {
        coreMouseService?: { areMouseEventsActive?: boolean }
        coreService?: { decPrivateModes?: { applicationCursorKeys?: boolean } }
      }
    }
    const screenElement = container.querySelector('.xterm-screen') as HTMLElement | null
    const getLineHeight = () => Math.ceil((term.element?.clientHeight ?? 0) / term.rows) || 20
    let lineHeightPx = getLineHeight()
    let touchAccum = 0
    let touchLastY = Number.NaN
    let touchStartX = Number.NaN
    let touchStartY = Number.NaN
    let touchStartTime = 0
    let touchMoved = false
    let velocityY = 0
    let lastTouchTime = 0
    let momentumFrame = 0
    let smoothActive = false
    let totalPxOffset = 0
    let baseViewportY = 0
    let renderedViewportY = 0
    let scrollbarHideTimer = 0
    let scrollbarDragging = false
    let scrollbarDragStartY = 0
    let scrollbarDragStartRatio = 0
    let suppressHistoryLoad = false
    let selectionAnchorCol = 0
    let selectionAnchorRow = 0
    let selectionEndCol = 0
    let selectionEndRow = 0
    let selectionTouchActive = false
    let selectionModeWasActive = false
    let selectionAutoScrollId: number | null = null
    let selectionLastClientX = 0
    let selectionLastClientY = 0
    let selectionPhase: 'idle' | 'anchor-set' = 'idle'
    let selectionExtending = false

    const noteHistoryScrollActivity = () => {
      lastHistoryScrollActivityAtRef.current = Date.now()
    }

    const noteUserScrollActivity = () => {
      cancelBottomAnchor()
      noteHistoryScrollActivity()
    }

    const armHistoryScrollbackLoad = () => {
      historyLoadArmedByUserRef.current = true
      noteUserScrollActivity()
    }

    const clearMomentum = () => {
      if (!momentumFrame) return
      window.cancelAnimationFrame(momentumFrame)
      momentumFrame = 0
    }

    const scrollbarTrack = document.createElement('div')
    scrollbarTrack.className = 'term-scrollbar-track'
    const scrollbarThumb = document.createElement('div')
    scrollbarThumb.className = 'term-scrollbar-thumb'
    scrollbarTrack.append(scrollbarThumb)
    container.append(scrollbarTrack)

    const magnifier = document.createElement('div')
    magnifier.className = 'sel-magnifier'
    const magnifierCanvas = document.createElement('canvas')
    const magnifierWidth = 160
    const magnifierHeight = 60
    const pixelRatio = typeof window === 'undefined' ? 1 : window.devicePixelRatio || 1
    magnifierCanvas.width = magnifierWidth * pixelRatio
    magnifierCanvas.height = magnifierHeight * pixelRatio
    magnifierCanvas.style.width = `${magnifierWidth}px`
    magnifierCanvas.style.height = `${magnifierHeight}px`
    magnifierCanvas.style.display = 'block'
    magnifier.append(magnifierCanvas)
    container.append(magnifier)

    const selectionAnchorMarker = document.createElement('div')
    selectionAnchorMarker.className = 'sel-anchor-marker'
    container.append(selectionAnchorMarker)

    const hideSelectionAnchor = () => {
      selectionAnchorMarker.style.display = 'none'
      selectionAnchorMarker.style.opacity = '0'
      selectionAnchorMarker.style.left = '-9999px'
      selectionAnchorMarker.style.top = '-9999px'
      selectionAnchorMarker.style.height = '0px'
    }
    hideSelectionAnchor()

    const showScrollbar = () => {
      scrollbarTrack.classList.add('visible')
      window.clearTimeout(scrollbarHideTimer)
    }

    const hideScrollbarDelayed = () => {
      window.clearTimeout(scrollbarHideTimer)
      if (scrollbarDragging) return
      scrollbarHideTimer = window.setTimeout(() => {
        scrollbarTrack.classList.remove('visible')
      }, 1200)
    }

    const updateScrollbar = () => {
      const buffer = term.buffer.active
      const totalLines = buffer.length
      const viewportRows = term.rows
      if (totalLines <= viewportRows) {
        scrollbarTrack.classList.remove('visible')
        return
      }
      const maxScroll = totalLines - viewportRows
      const ratio = buffer.viewportY / maxScroll
      const thumbRatio = Math.max(0.06, viewportRows / totalLines)
      const trackHeight = scrollbarTrack.clientHeight
      const thumbHeight = Math.max(24, trackHeight * thumbRatio)
      const thumbTop = ratio * (trackHeight - thumbHeight)
      scrollbarThumb.style.height = `${thumbHeight}px`
      scrollbarThumb.style.transform = `translateY(${thumbTop}px)`
    }

    const canLoadScrollbackPage = () => {
      return !historyLoadingRef.current &&
        !historyApplyingRef.current &&
        !suppressHistoryLoad &&
        term.buffer.active.type === 'normal' &&
        hasTerminalSnapshotRef.current &&
        !snapshotAlternateScreenRef.current
    }

    const loadScrollbackPage = async (restoreViewport: boolean): Promise<TerminalScrollbackLoadResult> => {
      const emptyResult = { loadedRows: 0, totalRows: 0, hasMore: false }
      if (!canLoadScrollbackPage()) return emptyResult
      historyLoadingRef.current = true
      historyRestoreViewportOnLoadRef.current = restoreViewport
      pendingHistoryViewportRef.current = restoreViewport ? term.buffer.active.viewportY : null
      try {
        const result = await loadScrollbackRef.current(historyScrollbackPageRows)
        if (result.loadedRows <= 0) {
          pendingHistoryViewportRef.current = null
        }
        return result
      } catch {
        pendingHistoryViewportRef.current = null
        return emptyResult
      } finally {
        historyLoadingRef.current = false
        historyRestoreViewportOnLoadRef.current = false
      }
    }

    const maybePrefetchScrollback = () => {
      if (!historyLoadArmedByUserRef.current) return
      if (term.buffer.active.viewportY > settingsRef.current.scrollbackPrefetchThresholdRows) return
      void loadScrollbackPage(true)
    }
    maybePrefetchScrollbackRef.current = maybePrefetchScrollback

    const shouldDelayHistoryApply = (pending: PendingHistoryApply) => {
      if (pending.restoreViewportY === null) return false
      const elapsed = Date.now() - historyApplyQueuedAtRef.current
      if (elapsed >= historyApplyMaxDelayMs) return false
      if (momentumFrame || smoothActive || scrollbarDragging) return true
      return Date.now() - lastHistoryScrollActivityAtRef.current < historyApplyScrollIdleMs
    }

    const applyPendingHistory = () => {
      historyApplyTimerRef.current = null
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
      const pending = pendingHistoryApplyRef.current
      if (!pending) return
      if (shouldDelayHistoryApply(pending)) {
        historyApplyTimerRef.current = window.setTimeout(applyPendingHistory, historyApplyScrollIdleMs)
        return
      }
      pendingHistoryApplyRef.current = null
      historyApplyQueuedAtRef.current = 0
      try {
        historyApplyingRef.current = true
        let historyApplyReleased = false
        let historyApplyWatchdog: number | null = window.setTimeout(() => {
          historyApplyWatchdog = null
          if (historyApplyReleased) return
          historyApplyReleased = true
          historyApplyingRef.current = false
        }, historyApplyWatchdogMs)
        const finishHistoryApply = () => {
          if (historyApplyReleased) return
          historyApplyReleased = true
          if (historyApplyWatchdog !== null) {
            window.clearTimeout(historyApplyWatchdog)
            historyApplyWatchdog = null
          }
          historyLoadedRowsAppliedRef.current = pending.loadedRows
          historyApplyingRef.current = false
          if (pending.restoreViewportY !== null) {
            maybePrefetchScrollbackRef.current()
          }
        }
        const previousScrollback = term.options.scrollback
        const rowsAddedSinceLastApply = Math.max(0, pending.loadedRows - historyLoadedRowsAppliedRef.current)
        const currentViewportY = term.buffer.active.viewportY
        const shouldKeepBottom = pending.restoreViewportY === null &&
          (Date.now() <= bottomAnchorUntilRef.current || isScrolledToBottom(term))
        if (typeof previousScrollback === 'number' && rowsAddedSinceLastApply > 0) {
          term.options.scrollback = Math.max(previousScrollback, previousScrollback + rowsAddedSinceLastApply)
        }
        const latestText = latestTerminalTextRef.current
        const textToApply = latestText.startsWith(pending.text) ? latestText : pending.text
        term.reset()
        writeToXterm(term, textToApply, 'history_apply', () => {
          markSurfaceReady()
          if (pending.restoreViewportY !== null) {
            term.scrollToLine(pending.restoreViewportY + pending.prependedRows)
          } else if (shouldKeepBottom) {
            keepBottomAnchored()
          } else {
            term.scrollToLine(currentViewportY + pending.prependedRows)
          }
          finishHistoryApply()
        })
        lastWrittenTextRef.current = textToApply
      } catch (error) {
        historyApplyingRef.current = false
        if (!terminalDisposedRef.current) throw error
      }
    }

    const scheduleHistoryApply = () => {
      if (historyApplyQueuedAtRef.current === 0) {
        historyApplyQueuedAtRef.current = Date.now()
      }
      if (historyApplyTimerRef.current !== null) return
      historyApplyTimerRef.current = window.setTimeout(applyPendingHistory, historyApplyBatchDelayMs)
    }
    scheduleHistoryApplyRef.current = scheduleHistoryApply

    const isMouseModeActive = () => {
      try {
        return Boolean(coreTerminal._core?.coreMouseService?.areMouseEventsActive)
      } catch {
        return false
      }
    }
    const isAlternateBuffer = () => term.buffer.active.type === 'alternate'
    const isApplicationCursor = () => {
      try {
        return Boolean(coreTerminal._core?.coreService?.decPrivateModes?.applicationCursorKeys)
      } catch {
        return false
      }
    }

    const syncTransform = () => {
      if (!screenElement || !smoothActive) return
      const renderedPx = (renderedViewportY - baseViewportY) * lineHeightPx
      const subLinePx = totalPxOffset - renderedPx
      screenElement.style.transform = `translateY(${-subLinePx}px)`
    }

    const smoothBegin = () => {
      if (smoothActive) return
      smoothActive = true
      lineHeightPx = getLineHeight()
      baseViewportY = term.buffer.active.viewportY
      renderedViewportY = baseViewportY
      totalPxOffset = 0
      if (screenElement) screenElement.style.willChange = 'transform'
    }

    const smoothEnd = () => {
      if (!smoothActive) return
      if (screenElement) {
        screenElement.style.transform = ''
        screenElement.style.willChange = ''
      }
      smoothActive = false
    }

    const resetTransientViewportOffset = () => {
      clearMomentum()
      if (screenElement) {
        screenElement.style.transform = ''
        screenElement.style.willChange = ''
      }
      smoothActive = false
      touchAccum = 0
      velocityY = 0
      touchLastY = Number.NaN
    }

    const scrollPixels = (px: number): boolean => {
      totalPxOffset += px
      const desiredViewportY = baseViewportY + Math.trunc(totalPxOffset / lineHeightPx)
      const currentViewportY = term.buffer.active.viewportY
      let clamped = false
      if (desiredViewportY !== currentViewportY) {
        if (px < 0) {
          armHistoryScrollbackLoad()
        } else {
          noteUserScrollActivity()
        }
        term.scrollLines(desiredViewportY - currentViewportY)
        const actualViewportY = term.buffer.active.viewportY
        if (actualViewportY !== desiredViewportY) {
          totalPxOffset = (actualViewportY - baseViewportY) * lineHeightPx
          clamped = true
          if (px < 0) maybePrefetchScrollback()
        }
      }
      syncTransform()
      return clamped
    }

    const sendScrollInput = (down: boolean) => {
      if (isMouseModeActive()) {
        if (!screenElement) return
        const rect = screenElement.getBoundingClientRect()
        screenElement.dispatchEvent(new WheelEvent('wheel', {
          bubbles: true,
          cancelable: true,
          clientX: rect.left + rect.width / 2,
          clientY: rect.top + rect.height / 2,
          deltaMode: WheelEvent.DOM_DELTA_LINE,
          deltaY: down ? 1 : -1,
        }))
        return
      }
      const applicationCursor = isApplicationCursor()
      sendInputAtCurrentSize(down
        ? (applicationCursor ? '\x1bOB' : '\x1b[B')
        : (applicationCursor ? '\x1bOA' : '\x1b[A'))
    }

    const initTouchScroll = (event: TouchEvent) => {
      clearMomentum()
      smoothEnd()
      let sumX = 0
      let sumY = 0
      for (let index = 0; index < event.touches.length; index += 1) {
        const touch = event.touches[index]!
        sumX += touch.clientX
        sumY += touch.clientY
      }
      const x = sumX / event.touches.length
      const y = sumY / event.touches.length
      touchLastY = y
      touchAccum = 0
      touchStartX = x
      touchStartY = y
      touchStartTime = Date.now()
      touchMoved = false
      velocityY = 0
      lastTouchTime = Date.now()
    }

    const stopSelectionAutoScroll = () => {
      if (selectionAutoScrollId === null) return
      window.clearInterval(selectionAutoScrollId)
      selectionAutoScrollId = null
    }

    const hideMagnifier = () => {
      magnifier.style.display = 'none'
    }

    const resetSelectionTouchState = () => {
      selectionPhase = 'idle'
      selectionExtending = false
      selectionTouchActive = false
      stopSelectionAutoScroll()
      hideSelectionAnchor()
      hideMagnifier()
    }
    selectionResetHandlersRef.current.add(resetSelectionTouchState)

    const touchToBufferPosition = (clientX: number, clientY: number) => {
      const targetElement = screenElement || container
      const rect = targetElement.getBoundingClientRect()
      const cellWidth = rect.width / term.cols
      const col = Math.max(0, Math.min(term.cols - 1, Math.floor((clientX - rect.left) / cellWidth)))
      const row = Math.floor((clientY - rect.top) / lineHeightPx)
      const bufferRow = Math.max(0, Math.min(term.buffer.active.length - 1, row + term.buffer.active.viewportY))
      return { col, row: bufferRow }
    }

    const applySelection = () => {
      let row1 = selectionAnchorRow
      let col1 = selectionAnchorCol
      let row2 = selectionEndRow
      let col2 = selectionEndCol
      if (row1 > row2 || (row1 === row2 && col1 > col2)) {
        ;[row1, col1, row2, col2] = [row2, col2, row1, col1]
      }
      const length = (row2 - row1) * term.cols + (col2 - col1) + 1
      term.select(col1, row1, length)
    }

    const showSelectionAnchor = (col: number, row: number) => {
      if (!selectionModeRef.current) {
        hideSelectionAnchor()
        return
      }
      const targetElement = screenElement || container
      const rect = targetElement.getBoundingClientRect()
      const containerRect = container.getBoundingClientRect()
      const cellWidth = rect.width / term.cols
      const viewportRow = row - term.buffer.active.viewportY
      const x = rect.left - containerRect.left + col * cellWidth
      const y = rect.top - containerRect.top + viewportRow * lineHeightPx
      if (!Number.isFinite(x) || !Number.isFinite(y)) {
        hideSelectionAnchor()
        return
      }
      selectionAnchorMarker.style.left = `${x}px`
      selectionAnchorMarker.style.top = `${y}px`
      selectionAnchorMarker.style.height = `${lineHeightPx}px`
      selectionAnchorMarker.style.display = 'block'
      selectionAnchorMarker.style.opacity = '1'
    }

    const updateMagnifier = (col: number, bufferRow: number, clientX: number, clientY: number) => {
      const canvases = container.querySelectorAll('.xterm-screen canvas') as NodeListOf<HTMLCanvasElement>
      const context = magnifierCanvas.getContext('2d')
      if (!context || canvases.length === 0) return

      const referenceCanvas = canvases[0]!
      const viewRow = bufferRow - term.buffer.active.viewportY
      const centerX = ((col + 0.5) / term.cols) * referenceCanvas.width
      const centerY = ((viewRow + 0.5) / term.rows) * referenceCanvas.height
      const rect = (screenElement || container).getBoundingClientRect()
      const scaleX = referenceCanvas.width / Math.max(1, rect.width)
      const scaleY = referenceCanvas.height / Math.max(1, rect.height)
      const sampleWidth = (magnifierWidth * scaleX) / 2
      const sampleHeight = (magnifierHeight * scaleY) / 2

      context.clearRect(0, 0, magnifierCanvas.width, magnifierCanvas.height)
      context.fillStyle = '#000'
      context.fillRect(0, 0, magnifierCanvas.width, magnifierCanvas.height)
      for (const canvas of canvases) {
        try {
          context.drawImage(
            canvas,
            centerX - sampleWidth / 2,
            centerY - sampleHeight / 2,
            sampleWidth,
            sampleHeight,
            0,
            0,
            magnifierCanvas.width,
            magnifierCanvas.height,
          )
        } catch {
          // Canvas can be temporarily unavailable while xterm swaps render layers.
        }
      }

      context.strokeStyle = 'rgba(255,255,255,0.5)'
      context.lineWidth = 1
      const midX = magnifierCanvas.width / 2
      const midY = magnifierCanvas.height / 2
      context.beginPath()
      context.moveTo(midX, 0)
      context.lineTo(midX, magnifierCanvas.height)
      context.moveTo(0, midY)
      context.lineTo(magnifierCanvas.width, midY)
      context.stroke()

      const containerRect = container.getBoundingClientRect()
      let x = clientX - containerRect.left - magnifierWidth / 2
      let y = clientY - containerRect.top - 70 - magnifierHeight
      if (x < 4) x = 4
      if (x + magnifierWidth > containerRect.width - 4) x = containerRect.width - magnifierWidth - 4
      if (y < 4) y = clientY - containerRect.top + 30
      magnifier.style.left = `${x}px`
      magnifier.style.top = `${y}px`
      magnifier.style.display = 'block'
    }

    const checkSelectionAutoScroll = (clientY: number) => {
      const targetElement = screenElement || container
      const rect = targetElement.getBoundingClientRect()
      const zone = 40
      const atTop = clientY < rect.top + zone
      const atBottom = clientY > rect.bottom - zone
      if (!atTop && !atBottom) {
        stopSelectionAutoScroll()
        return
      }
      if (selectionAutoScrollId !== null) return
      selectionAutoScrollId = window.setInterval(() => {
        term.scrollLines(atBottom ? 1 : -1)
        if (selectionExtending) {
          const position = touchToBufferPosition(selectionLastClientX, selectionLastClientY)
          selectionEndCol = position.col
          selectionEndRow = position.row
          applySelection()
        }
      }, 80)
    }

    const handleTouchStart = (event: TouchEvent) => {
      if (event.touches.length < 1) return
      cancelBottomAnchor()
      if (selectionModeRef.current && !selectionModeWasActive) {
        resetSelectionTouchState()
        selectionModeWasActive = true
      }
      if (!selectionModeRef.current && selectionModeWasActive) {
        resetSelectionTouchState()
        selectionModeWasActive = false
      }
      if (selectionModeRef.current && event.touches.length === 1) {
        event.preventDefault()
        event.stopPropagation()
        selectionTouchActive = true
        selectionExtending = false
        const touch = event.touches[0]
        if (!touch) return
        const position = touchToBufferPosition(touch.clientX, touch.clientY)
        if (selectionPhase === 'idle') {
          term.clearSelection()
          selectionAnchorCol = position.col
          selectionAnchorRow = position.row
          selectionPhase = 'anchor-set'
          showSelectionAnchor(position.col, position.row)
          updateMagnifier(position.col, position.row, touch.clientX, touch.clientY)
        } else {
          selectionEndCol = position.col
          selectionEndRow = position.row
          selectionExtending = true
          applySelection()
          hideSelectionAnchor()
          updateMagnifier(position.col, position.row, touch.clientX, touch.clientY)
        }
        return
      }
      if (selectionModeRef.current && event.touches.length >= 2) {
        selectionTouchActive = false
        selectionExtending = false
        stopSelectionAutoScroll()
        hideMagnifier()
        initTouchScroll(event)
        event.preventDefault()
        event.stopPropagation()
        return
      }
      event.preventDefault()
      event.stopPropagation()
      initTouchScroll(event)
    }

    const handleTouchMove = (event: TouchEvent) => {
      if (event.touches.length < 1) return
      if (selectionModeRef.current) {
        event.preventDefault()
        event.stopPropagation()
        if (event.touches.length >= 2 && selectionTouchActive) {
          selectionTouchActive = false
          selectionExtending = false
          stopSelectionAutoScroll()
          hideMagnifier()
          let sumY = 0
          for (let index = 0; index < event.touches.length; index += 1) {
            sumY += event.touches[index]!.clientY
          }
          touchLastY = sumY / event.touches.length
          touchAccum = 0
          velocityY = 0
          lastTouchTime = Date.now()
          touchMoved = false
          touchStartY = touchLastY
          touchStartTime = Date.now()
        }
        if (selectionTouchActive && event.touches.length === 1) {
          const touch = event.touches[0]
          if (!touch) return
          selectionLastClientX = touch.clientX
          selectionLastClientY = touch.clientY
          const position = touchToBufferPosition(touch.clientX, touch.clientY)
          if (selectionExtending) {
            selectionEndCol = position.col
            selectionEndRow = position.row
            applySelection()
            updateMagnifier(position.col, position.row, touch.clientX, touch.clientY)
            checkSelectionAutoScroll(touch.clientY)
            return
          }
          selectionAnchorCol = position.col
          selectionAnchorRow = position.row
          showSelectionAnchor(position.col, position.row)
          updateMagnifier(position.col, position.row, touch.clientX, touch.clientY)
          return
        }
        if (event.touches.length >= 2) {
          let sumY = 0
          for (let index = 0; index < event.touches.length; index += 1) {
            sumY += event.touches[index]!.clientY
          }
          const y = sumY / event.touches.length
          if (Number.isNaN(touchLastY)) {
            touchLastY = y
            lastTouchTime = Date.now()
            return
          }
          if (Math.abs(y - touchStartY) > 10) touchMoved = true
          const now = Date.now()
          const dt = now - lastTouchTime
          const dy = touchLastY - y
          if (dt > 0) {
            const instantVelocity = (dy / dt) * 1000
            velocityY = velocityY * 0.3 + instantVelocity * 0.7
          }
          lastTouchTime = now
          touchLastY = y
          smoothBegin()
          scrollPixels(dy)
          return
        }
        return
      }
      event.preventDefault()
      event.stopPropagation()
      let sumY = 0
      for (let index = 0; index < event.touches.length; index += 1) {
        sumY += event.touches[index]!.clientY
      }
      const y = sumY / event.touches.length
      if (Number.isNaN(touchLastY)) {
        touchLastY = y
        lastTouchTime = Date.now()
        return
      }
      if (Math.abs(y - touchStartY) > 10) touchMoved = true

      const now = Date.now()
      const dt = now - lastTouchTime
      const dy = touchLastY - y
      if (dt > 0) {
        const instantVelocity = (dy / dt) * 1000
        velocityY = velocityY * 0.3 + instantVelocity * 0.7
      }
      lastTouchTime = now
      touchLastY = y

      if (isMouseModeActive() || isAlternateBuffer()) {
        if (smoothActive) smoothEnd()
        touchAccum += dy
        const lines = Math.trunc(touchAccum / lineHeightPx)
        if (lines !== 0) {
          touchAccum -= lines * lineHeightPx
          for (let index = 0; index < Math.abs(lines); index += 1) {
            sendScrollInput(lines > 0)
          }
        }
        return
      }

      smoothBegin()
      scrollPixels(dy)
    }

    const handleTouchEnd = () => {
      if (selectionModeRef.current) {
        if (selectionTouchActive) {
          selectionTouchActive = false
          stopSelectionAutoScroll()
          hideMagnifier()
          if (selectionExtending) {
            selectionAnchorCol = selectionEndCol
            selectionAnchorRow = selectionEndRow
            selectionExtending = false
          }
          return
        }
        smoothEnd()
        touchLastY = Number.NaN
        return
      }
      if (!touchMoved && Date.now() - touchStartTime < 500) {
        if (isMouseModeActive() && screenElement) {
          const mouseOptions: MouseEventInit = {
            bubbles: true,
            button: 0,
            cancelable: true,
            clientX: touchStartX,
            clientY: touchStartY,
          }
          screenElement.dispatchEvent(new MouseEvent('mousedown', { ...mouseOptions, buttons: 1 }))
          screenElement.dispatchEvent(new MouseEvent('mouseup', { ...mouseOptions, buttons: 0 }))
        }
        try {
          if (!preventFocusRef.current) xtermRef.current?.focus()
        } catch {
          // Ignore stale focus work while unmounting.
        }
        velocityY = 0
        smoothEnd()
      } else if (Math.abs(velocityY) > 80) {
        if (Date.now() - lastTouchTime > 80) {
          velocityY = 0
          smoothEnd()
        } else {
          let lastFrameTime = performance.now()
          const deceleration = 0.97
          const minimumVelocity = 80
          const step = (now: number) => {
            if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) {
              momentumFrame = 0
              smoothEnd()
              return
            }
            const frameDt = (now - lastFrameTime) / 1000
            lastFrameTime = now
            velocityY *= Math.pow(deceleration, frameDt * 60)
            if (Math.abs(velocityY) < minimumVelocity) {
              momentumFrame = 0
              smoothEnd()
              return
            }
            const pixelDelta = velocityY * frameDt
            if (isMouseModeActive() || isAlternateBuffer()) {
              if (smoothActive) smoothEnd()
              touchAccum += pixelDelta
              const lines = Math.trunc(touchAccum / lineHeightPx)
              if (lines !== 0) {
                touchAccum -= lines * lineHeightPx
                for (let index = 0; index < Math.abs(lines); index += 1) {
                  sendScrollInput(lines > 0)
                }
              }
            } else if (scrollPixels(pixelDelta)) {
              momentumFrame = 0
              smoothEnd()
              return
            }
            momentumFrame = window.requestAnimationFrame(step)
          }
          touchAccum = 0
          momentumFrame = window.requestAnimationFrame(step)
        }
      } else {
        smoothEnd()
      }
      touchLastY = Number.NaN
      if (historyApplyTimerRef.current === null && pendingHistoryApplyRef.current) {
        scheduleHistoryApply()
      }
    }

    const handleRenderDisposable = typeof term.onRender === 'function'
      ? term.onRender(() => {
        const viewportY = term.buffer.active.viewportY
        if (viewportY !== renderedViewportY) {
          renderedViewportY = viewportY
          if (smoothActive) syncTransform()
          updateScrollbar()
          if (historyLoadingRef.current) {
            if (historyRestoreViewportOnLoadRef.current) {
              pendingHistoryViewportRef.current = term.buffer.active.viewportY
            }
          } else {
            if (historyLoadArmedByUserRef.current) {
              maybePrefetchScrollback()
            }
          }
          const pendingHistoryApply = pendingHistoryApplyRef.current
          if (pendingHistoryApply !== null && pendingHistoryApply.restoreViewportY !== null) {
            pendingHistoryApplyRef.current = {
              ...pendingHistoryApply,
              restoreViewportY: term.buffer.active.viewportY,
            }
          }
          noteHistoryScrollActivity()
          showScrollbar()
          hideScrollbarDelayed()
        }
      })
      : { dispose() {} }
    const bufferDisposable = term.buffer.onBufferChange?.(() => {
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
      clearMomentum()
      smoothEnd()
      updateScrollbar()
      onBufferChangeRef.current?.(term.buffer.active.type === 'alternate')
    }) ?? { dispose() {} }

    const scrollTouchTarget = screenElement || container
    scrollTouchTarget.addEventListener('touchstart', handleTouchStart, { passive: false })
    scrollTouchTarget.addEventListener('touchmove', handleTouchMove, { passive: false })
    scrollTouchTarget.addEventListener('touchend', handleTouchEnd, { passive: true })
    scrollTouchTarget.addEventListener('touchcancel', handleTouchEnd, { passive: true })

    const handleWheel = (event: WheelEvent) => {
      if (event.deltaY < 0 && term.buffer.active.viewportY <= settingsRef.current.scrollbackPrefetchThresholdRows) {
        armHistoryScrollbackLoad()
        maybePrefetchScrollback()
      } else if (event.deltaY !== 0) {
        noteUserScrollActivity()
      }
    }
    scrollTouchTarget.addEventListener('wheel', handleWheel, { passive: true })

    const handleScrollbarTouchStart = (event: TouchEvent) => {
      event.preventDefault()
      event.stopPropagation()
      hapticSelection()
      scrollbarDragging = true
      scrollbarTrack.classList.add('active')
      showScrollbar()
      const touch = event.touches[0]
      if (!touch) return
      scrollbarDragStartY = touch.clientY
      const buffer = term.buffer.active
      const maxScroll = buffer.length - term.rows
      scrollbarDragStartRatio = maxScroll > 0 ? buffer.viewportY / maxScroll : 0
    }
    const handleScrollbarTouchMove = (event: TouchEvent) => {
      if (!scrollbarDragging) return
      event.preventDefault()
      event.stopPropagation()
      const touch = event.touches[0]
      if (!touch) return
      const scrollableTrack = scrollbarTrack.clientHeight - scrollbarThumb.clientHeight
      if (scrollableTrack <= 0) return
      const deltaRatio = (touch.clientY - scrollbarDragStartY) / scrollableTrack
      const nextRatio = Math.max(0, Math.min(1, scrollbarDragStartRatio + deltaRatio))
      const buffer = term.buffer.active
      const maxScroll = buffer.length - term.rows
      const targetY = Math.round(nextRatio * maxScroll)
      const diff = targetY - buffer.viewportY
      if (diff !== 0) {
        if (diff < 0) {
          armHistoryScrollbackLoad()
        } else {
          noteUserScrollActivity()
        }
        term.scrollLines(diff)
      }
      updateScrollbar()
    }
    const handleScrollbarTouchEnd = (event: TouchEvent) => {
      if (!scrollbarDragging) return
      event.preventDefault()
      scrollbarDragging = false
      scrollbarTrack.classList.remove('active')
      hideScrollbarDelayed()
      if (historyApplyTimerRef.current === null && pendingHistoryApplyRef.current) {
        scheduleHistoryApply()
      }
    }
    const handleScrollbarTrackTouchStart = (event: TouchEvent) => {
      if (event.target !== scrollbarTrack) return
      event.preventDefault()
      event.stopPropagation()
      hapticSelection()
      const touch = event.touches[0]
      if (!touch) return
      const rect = scrollbarTrack.getBoundingClientRect()
      const ratio = Math.max(0, Math.min(1, (touch.clientY - rect.top) / rect.height))
      const buffer = term.buffer.active
      const maxScroll = buffer.length - term.rows
      const targetY = Math.round(ratio * maxScroll)
      const diff = targetY - buffer.viewportY
      if (diff !== 0) {
        if (diff < 0) {
          armHistoryScrollbackLoad()
        } else {
          noteUserScrollActivity()
        }
        term.scrollLines(diff)
      }
      updateScrollbar()
      scrollbarDragging = true
      scrollbarTrack.classList.add('active')
      showScrollbar()
      scrollbarDragStartY = touch.clientY
      scrollbarDragStartRatio = maxScroll > 0 ? buffer.viewportY / maxScroll : 0
    }

    scrollbarThumb.addEventListener('touchstart', handleScrollbarTouchStart, { passive: false })
    scrollbarTrack.addEventListener('touchstart', handleScrollbarTrackTouchStart, { passive: false })
    document.addEventListener('touchmove', handleScrollbarTouchMove, { passive: false })
    document.addEventListener('touchend', handleScrollbarTouchEnd, { passive: false })
    document.addEventListener('touchcancel', handleScrollbarTouchEnd, { passive: false })

    fitAndMaybeSendResize()
    scheduleFit()
    let resizeObserver: ResizeObserver | null = null
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => {
        if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
        resetTransientViewportOffset()
        fitAndMaybeSendResize()
        lineHeightPx = getLineHeight()
      })
      resizeObserver.observe(container)
    }

    const clearViewportOffsetForKeyboard = () => {
      if (terminalDisposedRef.current || terminalGenerationRef.current !== generation) return
      resetTransientViewportOffset()
      smoothEnd()
    }

    const removeNativeKeyboardListener = addNativeKeyboardListener(clearViewportOffsetForKeyboard)
    document.addEventListener('termx:resume', clearViewportOffsetForKeyboard)
    let expectedProbeAt = terminalNow() + eventLoopProbeIntervalMs
    const eventLoopProbe = window.setInterval(() => {
      const now = terminalNow()
      const lagMs = now - expectedProbeAt
      expectedProbeAt = now + eventLoopProbeIntervalMs
      if (lagMs >= eventLoopLagWarnMs) {
        logTerminal('event_loop_lag', {
          level: 'warn',
          details: {
            lagMs: Math.round(lagMs),
            pendingWriteCallbacks: xtermWriteStatsRef.current.pendingCallbacks,
            terminalTextChars: latestTerminalTextRef.current.length,
            lastWrittenChars: lastWrittenTextRef.current.length,
            historyApplying: historyApplyingRef.current,
            historyLoading: historyLoadingRef.current,
          },
        })
      }
    }, eventLoopProbeIntervalMs)

    if (window.visualViewport) {
      window.visualViewport.addEventListener('resize', clearViewportOffsetForKeyboard)
      window.visualViewport.addEventListener('scroll', clearViewportOffsetForKeyboard)
    }

    return () => {
      terminalDisposedRef.current = true
      terminalGenerationRef.current += 1
      if (window.visualViewport) {
        window.visualViewport.removeEventListener('resize', clearViewportOffsetForKeyboard)
        window.visualViewport.removeEventListener('scroll', clearViewportOffsetForKeyboard)
      }
      removeNativeKeyboardListener()
      document.removeEventListener('termx:resume', clearViewportOffsetForKeyboard)
      window.clearInterval(eventLoopProbe)
      if (fitFrameRef.current !== null) {
        window.cancelAnimationFrame(fitFrameRef.current)
        fitFrameRef.current = null
      }
      cancelBottomAnchor()
      clearLiveOutputWatchdog()
      liveOutputInFlightRef.current = false
      liveOutputGenerationRef.current += 1
      pendingLiveOutputRef.current = ''
      liveOutputDroppedCharsRef.current = 0
      liveOutputSyncLostRef.current = false
      if (historyApplyTimerRef.current !== null) {
        window.clearTimeout(historyApplyTimerRef.current)
        historyApplyTimerRef.current = null
      }
      pendingHistoryApplyRef.current = null
      historyLoadArmedByUserRef.current = false
      maybePrefetchScrollbackRef.current = () => {}
      scheduleHistoryApplyRef.current = () => {}
      resizeObserver?.disconnect()
      dataDisposable.dispose()
      binaryDisposable.dispose()
      cursorDisposable.dispose()
      handleRenderDisposable.dispose()
      bufferDisposable.dispose()
      textarea?.removeEventListener('compositionstart', handleCompositionStart)
      textarea?.removeEventListener('compositionupdate', handleCompositionUpdate)
      textarea?.removeEventListener('compositionend', handleCompositionEnd)
      textarea?.removeEventListener('focus', onFocus)
      compositionOverlay.remove()
      clearMomentum()
      smoothEnd()
      selectionResetHandlersRef.current.delete(resetSelectionTouchState)
      resetSelectionTouchState()
      window.clearTimeout(scrollbarHideTimer)
      scrollTouchTarget.removeEventListener('touchstart', handleTouchStart)
      scrollTouchTarget.removeEventListener('touchmove', handleTouchMove)
      scrollTouchTarget.removeEventListener('touchend', handleTouchEnd)
      scrollTouchTarget.removeEventListener('touchcancel', handleTouchEnd)
      scrollTouchTarget.removeEventListener('wheel', handleWheel)
      scrollbarThumb.removeEventListener('touchstart', handleScrollbarTouchStart)
      scrollbarTrack.removeEventListener('touchstart', handleScrollbarTrackTouchStart)
      document.removeEventListener('touchmove', handleScrollbarTouchMove)
      document.removeEventListener('touchend', handleScrollbarTouchEnd)
      document.removeEventListener('touchcancel', handleScrollbarTouchEnd)
      scrollbarTrack.remove()
      magnifier.remove()
      selectionAnchorMarker.remove()
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
  }, [cancelBottomAnchor, clearLiveOutputWatchdog, fitAndMaybeSendResize, isScrolledToBottom, keepBottomAnchored, logTerminal, markSurfaceReady, renderer, scheduleFit, sendInputAtCurrentSize, sendUserInput, settings.renderer, writeToXterm])

  useEffect(() => {
    isOpenRef.current = isOpen
    if (!isOpen) {
      clearLiveOutputWatchdog()
      liveOutputInFlightRef.current = false
      liveOutputGenerationRef.current += 1
      pendingLiveOutputRef.current = ''
      liveOutputDroppedCharsRef.current = 0
      liveOutputSyncLostRef.current = false
      lastWrittenTextRef.current = ''
      lastSnapshotTextRef.current = ''
      recoveryRevisionAppliedRef.current = 0
      surfaceReadyRef.current = false
      setSurfaceReady(false)
      return
    }
    fitAndMaybeSendResize()
    scheduleFit()
    if (!terminalDisposedRef.current) {
      try {
        if (!preventFocusRef.current) xtermRef.current?.focus()
      } catch {
        // Ignore stale focus calls after xterm has been disposed.
      }
    }
    onReady?.()
  }, [clearLiveOutputWatchdog, fitAndMaybeSendResize, isOpen, onReady, scheduleFit])

  useEffect(() => {
    if (terminalDisposedRef.current) return
    const term = xtermRef.current
    if (!term) return

    const snapshot = terminalSession.terminalSnapshot
    const snapshotText = snapshot ? (snapshot.replay ?? snapshot.text) : ''
    const recoveryRevision = snapshot?.recovery?.revision ?? 0
    const shouldReplaySnapshot = Boolean(
      snapshot &&
      snapshotText &&
      (snapshotText !== lastSnapshotTextRef.current || recoveryRevision > recoveryRevisionAppliedRef.current) &&
      (lastWrittenTextRef.current === '' || recoveryRevision > recoveryRevisionAppliedRef.current),
    )

    if (shouldReplaySnapshot) {
      try {
        clearLiveOutputWatchdog()
        liveOutputInFlightRef.current = false
        liveOutputGenerationRef.current += 1
        pendingLiveOutputRef.current = ''
        liveOutputDroppedCharsRef.current = 0
        liveOutputSyncLostRef.current = false
        term.reset()
        writeToXterm(term, terminalSession.terminalText, recoveryRevision > recoveryRevisionAppliedRef.current ? 'snapshot_recovery' : 'snapshot_full_text', () => {
          markSurfaceReady()
          keepBottomAnchored()
        })
        lastWrittenTextRef.current = terminalSession.terminalText
        if (recoveryRevision > recoveryRevisionAppliedRef.current) {
          recoveryRevisionAppliedRef.current = recoveryRevision
        }
      } catch (error) {
        if (!terminalDisposedRef.current) throw error
      }
    }
    lastSnapshotTextRef.current = snapshotText

    const history = terminalSession.terminalSnapshot?.history
    if (history && history.revision > historyRevisionAppliedRef.current) {
      historyRevisionAppliedRef.current = history.revision
      const restoreViewportY = pendingHistoryViewportRef.current
      pendingHistoryViewportRef.current = null
      const previousPending = pendingHistoryApplyRef.current
      const previousPendingRows = previousPending?.loadedRows ?? historyLoadedRowsAppliedRef.current
      const pendingPrependedRows = Math.max(0, history.loadedRows - previousPendingRows)
      pendingHistoryApplyRef.current = {
        revision: history.revision,
        loadedRows: history.loadedRows,
        prependedRows: (previousPending?.prependedRows ?? 0) + pendingPrependedRows,
        restoreViewportY: restoreViewportY ?? previousPending?.restoreViewportY ?? null,
        text: terminalSession.terminalText,
      }
      scheduleHistoryApplyRef.current()
      return
    }

    const pendingHistoryApply = pendingHistoryApplyRef.current
    if (pendingHistoryApply) {
      pendingHistoryApplyRef.current = {
        ...pendingHistoryApply,
        text: terminalSession.terminalText,
      }
      return
    }
  }, [clearLiveOutputWatchdog, keepBottomAnchored, markSurfaceReady, terminalSession.terminalSnapshot, terminalSession.terminalText, writeToXterm])

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
        className="absolute inset-0 min-h-0 overflow-hidden xterm-wrapper outline-none"
        style={{
          opacity: showConnectingOverlay ? 0 : 1,
          overscrollBehavior: 'contain',
          touchAction: 'none',
        }}
        role="application"
        tabIndex={0}
      />
      {showConnectingOverlay ? (
        <div className="pointer-events-none absolute inset-0 z-50 flex items-center justify-center bg-[var(--termx-bg)]/80 backdrop-blur-sm text-sm font-medium text-[var(--termx-muted)]">
          <div className="flex items-center gap-2">
            <div className="h-4 w-4 animate-spin rounded-full border-2 border-[var(--termx-muted)] border-t-[var(--termx-text)]" />
            Connecting terminal...
          </div>
        </div>
      ) : null}
    </section>
  )
})
