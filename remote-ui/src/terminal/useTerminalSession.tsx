import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react'
import {
  initialConnectionSnapshot,
  reduceConnectionMessage,
  type ConnectionMessage,
  type ConnectionSnapshot,
} from '../connection/connectionMessageReducer'
import {
  defaultTerminalResizeControl,
  TerminalClient,
  type TerminalClientCallbacks,
  type TerminalProtocolSession,
  type TerminalResizeControl,
  type TerminalScrollbackLoadResult,
  type TerminalSnapshotPayload,
} from './terminalClient'
import { createTerminalProtocolClient } from './terminalProtocolClient'
import { logTerminalDiagnostic, terminalNow } from './terminalDiagnostics'
import { appendTerminalText, terminalTextSoftLimitChars, trimTerminalTextToRecentWindow } from './terminalTextWindow'
import type { Terminal } from '../core/model'
import type {
  RtcConnectionStateSnapshot,
  RtcSession,
  RtcSessionConnectionStateEvents,
  RtcTerminalDataChannelController,
} from '../core/transport'

const recentInputRecoveryWindowMs = 1500
const outputStatsIntervalMs = 1000
const largeOutputChunkBytes = 64 * 1024
const liveOutputPublishDelayMs = 120

interface TerminalChannelRecoveryOptions {
  session?: RtcSession | undefined
  forceTerminalChannel?: boolean | undefined
  supersede?: boolean | undefined
  showClosedReason?: boolean | undefined
}

export interface UseTerminalSessionOptions {
  machineId: string
  terminalId: string
  session: RtcSession
  onOutput?: ((text: string) => void) | undefined
}

export interface UseTerminalSessionResult {
  snapshot: ConnectionSnapshot
  terminalSnapshot: TerminalSnapshotPayload | null
  terminalText: string
  terminalInfo: Terminal | null
  resizeControl: TerminalResizeControl
  sendInput(data: string, size?: { cols: number; rows: number }): boolean
  sendResize(cols: number, rows: number): boolean
  requestResizeOwner(size?: { cols: number; rows: number }): Promise<TerminalResizeControl>
  releaseResizeOwner(): Promise<TerminalResizeControl>
  loadScrollback(limit?: number): Promise<TerminalScrollbackLoadResult>
  markSyncLost(reason?: string): void
  handleAppResume(resumeKind: 'quick' | 'cold' | 'frozen'): void
  reattach(session: RtcSession, options?: { forceTerminalChannel?: boolean }): void
  client: TerminalClient | null
}

export function useTerminalSession(options: UseTerminalSessionOptions): UseTerminalSessionResult {
  const [snapshot, dispatch] = useReducer(reduceConnectionMessage, undefined, initialConnectionSnapshot)
  const [terminalSnapshot, setTerminalSnapshot] = useState<TerminalSnapshotPayload | null>(null)
  const [terminalText, setTerminalText] = useState('')
  const [terminalInfo, setTerminalInfo] = useState<Terminal | null>(null)
  const [resizeControl, setResizeControl] = useState<TerminalResizeControl>(defaultTerminalResizeControl)
  const clientRef = useRef<TerminalClient | null>(null)
  const sessionRef = useRef(options.session)
  const connectionIdRef = useRef('terminal-connection')
  const protocolSessionRef = useRef<TerminalProtocolSession | null>(null)
  const scrollbackPrefixTextRef = useRef('')
  const loadedScrollbackRowsRef = useRef(0)
  const historyRevisionRef = useRef(0)
  const loadingScrollbackRef = useRef(false)
  const hasMoreScrollbackRef = useRef(true)
  const recoveringInputRef = useRef(false)
  const terminalRecoveryPromiseRef = useRef<Promise<boolean> | null>(null)
  const terminalRecoverySeqRef = useRef(0)
  const sessionConnectionPhaseRef = useRef<RtcConnectionStateSnapshot['phase'] | null>(null)
  const pendingRecoveryInputRef = useRef<{ data: string; size?: { cols: number; rows: number } } | null>(null)
  const pendingRecoveryInputTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const textDecoderRef = useRef(new TextDecoder())
  const onOutputRef = useRef<((text: string) => void) | undefined>(options.onOutput)
  const terminalTextRef = useRef('')
  const terminalTextPublishTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const outputStatsRef = useRef({
    chunks: 0,
    bytes: 0,
    chars: 0,
    trimmedChars: 0,
    lastLogAt: terminalNow(),
    lastBytes: 0,
    lastChunks: 0,
  })

  const logSession = useCallback((event: string, input: {
    level?: 'debug' | 'info' | 'warn' | 'error'
    details?: Record<string, unknown> | undefined
  } = {}) => {
    logTerminalDiagnostic(`session.${event}`, {
      level: input.level,
      machineId: options.machineId,
      terminalId: options.terminalId,
      connectionId: connectionIdRef.current,
      details: input.details,
    })
  }, [options.machineId, options.terminalId])

  const maybeLogOutputStats = useCallback((currentTextChars: number) => {
    const now = terminalNow()
    const stats = outputStatsRef.current
    if (now - stats.lastLogAt < outputStatsIntervalMs) return
    const elapsedSeconds = Math.max(0.001, (now - stats.lastLogAt) / 1000)
    const intervalBytes = stats.bytes - stats.lastBytes
    const intervalChunks = stats.chunks - stats.lastChunks
    logSession('output_stats', {
      details: {
        chunks: stats.chunks,
        bytes: stats.bytes,
        chars: stats.chars,
        currentTextChars,
        trimmedChars: stats.trimmedChars,
        intervalChunks,
        intervalBytes,
        bytesPerSecond: Math.round(intervalBytes / elapsedSeconds),
      },
    })
    stats.lastLogAt = now
    stats.lastBytes = stats.bytes
    stats.lastChunks = stats.chunks
  }, [logSession])

  const clearPendingRecoveryInput = useCallback((pending?: { data: string; size?: { cols: number; rows: number } } | null) => {
    if (pending && pendingRecoveryInputRef.current !== pending) return
    if (pendingRecoveryInputTimerRef.current !== null) {
      clearTimeout(pendingRecoveryInputTimerRef.current)
      pendingRecoveryInputTimerRef.current = null
    }
    pendingRecoveryInputRef.current = null
  }, [])

  const clearTerminalTextPublishTimer = useCallback(() => {
    if (terminalTextPublishTimerRef.current === null) return
    clearTimeout(terminalTextPublishTimerRef.current)
    terminalTextPublishTimerRef.current = null
  }, [])

  const publishTerminalTextNow = useCallback(() => {
    clearTerminalTextPublishTimer()
    setTerminalText(terminalTextRef.current)
  }, [clearTerminalTextPublishTimer])

  const scheduleTerminalTextPublish = useCallback(() => {
    if (terminalTextPublishTimerRef.current !== null) return
    terminalTextPublishTimerRef.current = setTimeout(() => {
      terminalTextPublishTimerRef.current = null
      setTerminalText(terminalTextRef.current)
    }, liveOutputPublishDelayMs)
  }, [])

  useEffect(() => {
    onOutputRef.current = options.onOutput
  }, [options.onOutput])

  const rememberRecoveryInput = useCallback((pending: { data: string; size?: { cols: number; rows: number } }) => {
    if (pendingRecoveryInputTimerRef.current !== null) {
      clearTimeout(pendingRecoveryInputTimerRef.current)
      pendingRecoveryInputTimerRef.current = null
    }
    pendingRecoveryInputRef.current = pending
    pendingRecoveryInputTimerRef.current = setTimeout(() => {
      if (!recoveringInputRef.current && pendingRecoveryInputRef.current === pending) {
        pendingRecoveryInputRef.current = null
      }
      pendingRecoveryInputTimerRef.current = null
    }, recentInputRecoveryWindowMs)
  }, [])

  const recoverTerminalChannel = useCallback((
    reason: string,
    recoveryOptions: TerminalChannelRecoveryOptions = {},
  ): Promise<boolean> => {
    const existing = terminalRecoveryPromiseRef.current
    if (existing && recoveryOptions.supersede !== true) return existing

    const seq = terminalRecoverySeqRef.current + 1
    terminalRecoverySeqRef.current = seq
    const targetSession = recoveryOptions.session ?? sessionRef.current
    const previousSession = sessionRef.current
    const client = clientRef.current
    if (!client) return Promise.resolve(false)

    const startedAt = terminalNow()
    logSession('terminal_recovery_start', {
      level: 'warn',
      details: {
        reason,
        forceTerminalChannel: recoveryOptions.forceTerminalChannel === true,
        supersede: recoveryOptions.supersede === true,
      },
    })

    const recoveryPromise = (async (): Promise<boolean> => {
      try {
        dispatch({
          type: 'terminal.channelClosed',
          machineId: options.machineId,
          terminalId: options.terminalId,
          ...(recoveryOptions.showClosedReason === false ? {} : { reason }),
        })
        protocolSessionRef.current?.closeTerminalChannel(options.terminalId)
        closeRtcTerminalDataChannel(previousSession, options.terminalId)
        if (recoveryOptions.forceTerminalChannel) {
          closeRtcTerminalDataChannel(targetSession, options.terminalId)
        }
        setResizeControl(defaultTerminalResizeControl)
        sessionRef.current = targetSession
        const info = await targetSession.getConnectionInfo()
        if (terminalRecoverySeqRef.current !== seq) return false
        connectionIdRef.current = info.connectionId
        const protocolSession = createProtocolSession(targetSession, options.machineId, options.terminalId, info)
        protocolSessionRef.current = protocolSession
        await client.reattach(protocolSession)
        if (terminalRecoverySeqRef.current !== seq) return false
        dispatch({ type: 'connection.verified', connectionId: info.connectionId })
        logSession('terminal_recovery_done', {
          level: 'info',
          details: {
            reason,
            elapsedMs: Math.round(terminalNow() - startedAt),
            connectionId: info.connectionId,
          },
        })
        return true
      } catch (err) {
        if (terminalRecoverySeqRef.current === seq) {
          const message = err instanceof Error ? err.message : String(err)
          logSession('terminal_recovery_failed', {
            level: 'error',
            details: {
              reason,
              elapsedMs: Math.round(terminalNow() - startedAt),
              error: message,
            },
          })
          dispatch({
            type: 'connection.failed',
            reason: message,
            recoverable: true,
            surface: 'banner',
          })
        }
        return false
      } finally {
        if (terminalRecoverySeqRef.current === seq) {
          terminalRecoveryPromiseRef.current = null
        }
      }
    })()
    terminalRecoveryPromiseRef.current = recoveryPromise
    return recoveryPromise
  }, [logSession, options.machineId, options.terminalId])

  const recoverInputChannel = useCallback(async (
    pending: { data: string; size?: { cols: number; rows: number } },
    reason: string,
  ) => {
    if (recoveringInputRef.current) return
    recoveringInputRef.current = true
    rememberRecoveryInput(pending)
    const session = sessionRef.current
    const client = clientRef.current
    if (!client) {
      recoveringInputRef.current = false
      return
    }
    try {
      const recovered = await recoverTerminalChannel(reason, {
        session,
        forceTerminalChannel: true,
      })
      if (!recovered || pendingRecoveryInputRef.current !== pending) {
        recoveringInputRef.current = false
        return
      }
      clearPendingRecoveryInput(pending)
      const retried = client.sendInput(pending.data, pending.size)
      recoveringInputRef.current = false
      if (!retried) {
        dispatch({ type: 'connection.failed', reason: 'terminal input retry failed', recoverable: true, surface: 'banner' })
      } else {
        const info = await session.getConnectionInfo()
        dispatch({ type: 'connection.verified', connectionId: info.connectionId })
      }
    } catch (err) {
      clearPendingRecoveryInput(pending)
      recoveringInputRef.current = false
      dispatch({
        type: 'connection.failed',
        reason: err instanceof Error ? err.message : String(err),
        recoverable: true,
        surface: 'banner',
      })
    }
  }, [clearPendingRecoveryInput, recoverTerminalChannel, rememberRecoveryInput])

  const callbacks = useMemo<TerminalClientCallbacks>(() => ({
    onOutput: (data) => {
      hasMoreScrollbackRef.current = true
      const decoded = textDecoderRef.current.decode(data)
      const stats = outputStatsRef.current
      stats.chunks += 1
      stats.bytes += data.byteLength
      stats.chars += decoded.length
      if (data.byteLength >= largeOutputChunkBytes) {
        logSession('large_output_chunk', {
          level: 'warn',
          details: {
            bytes: data.byteLength,
            decodedChars: decoded.length,
            chunks: stats.chunks,
            totalBytes: stats.bytes,
          },
        })
      }
      onOutputRef.current?.(decoded)
      const current = terminalTextRef.current
      const next = appendTerminalText(current, decoded)
      const expectedLength = current.length + decoded.length
      if (next.length < expectedLength) {
        stats.trimmedChars += expectedLength - next.length
      }
      terminalTextRef.current = next
      maybeLogOutputStats(next.length)
      scheduleTerminalTextPublish()
    },
    onSnapshot: (nextSnapshot) => {
      const canPreserveHistory = !nextSnapshot.alternateScreen && scrollbackPrefixTextRef.current !== ''
      const nextText = nextSnapshot.replay ?? nextSnapshot.text
      const screenText = nextSnapshot.replay ?? nextSnapshot.screenReplay ?? nextSnapshot.screenText
      setTerminalSnapshot(nextSnapshot)
      logSession('snapshot_received', {
        details: {
          textChars: nextSnapshot.text.length,
          replayChars: nextSnapshot.replay?.length ?? 0,
          screenTextChars: nextSnapshot.screenText?.length ?? 0,
          screenReplayChars: nextSnapshot.screenReplay?.length ?? 0,
          rows: nextSnapshot.rows,
          cols: nextSnapshot.cols,
          scrollbackRows: nextSnapshot.scrollbackRows?.length ?? 0,
          alternateScreen: nextSnapshot.alternateScreen === true,
        },
      })
      if (!canPreserveHistory) {
        loadedScrollbackRowsRef.current = nextSnapshot.scrollbackRows?.length ?? 0
        scrollbackPrefixTextRef.current = ''
      }
      hasMoreScrollbackRef.current = !nextSnapshot.alternateScreen
      terminalTextRef.current = canPreserveHistory && screenText !== undefined
        ? joinHistoryAndSnapshotText(scrollbackPrefixTextRef.current, screenText, nextSnapshot.rows)
        : nextText
      publishTerminalTextNow()
    },
    onTerminalInfo: setTerminalInfo,
    onResizeControl: setResizeControl,
    onLifecycle: (message) => dispatch(message),
    onError: (message) => dispatch({ type: 'connection.failed', reason: message, recoverable: true, surface: 'banner' }),
    onClose: (reason) => {
      const pending = pendingRecoveryInputRef.current
      if (pending && (recoveringInputRef.current || shouldRecoverRecentInput(reason))) {
        void recoverInputChannel(pending, reason ?? 'terminal channel closed')
      } else if (shouldRecoverTerminalChannel(reason)) {
        void recoverTerminalChannel(reason ?? 'terminal channel closed', {
          forceTerminalChannel: true,
        })
      }
    },
    onOpen: () => {},
    onInputDropped: () => {
      if (!recoveringInputRef.current) {
        dispatch({ type: 'connection.failed', reason: 'terminal input dropped', recoverable: true, surface: 'toast' })
      }
    },
    onInputSendFailed: (reason) => {
      const pending = pendingRecoveryInputRef.current
      if (pending) {
        void recoverInputChannel(pending, reason)
      } else if (shouldRecoverTerminalChannel(reason)) {
        void recoverTerminalChannel(reason, { forceTerminalChannel: true })
      }
    },
  }), [logSession, maybeLogOutputStats, publishTerminalTextNow, recoverInputChannel, recoverTerminalChannel, scheduleTerminalTextPublish])

  useEffect(() => {
    sessionRef.current = options.session
    const client = new TerminalClient(callbacks)
    clientRef.current = client

    let cancelled = false
    logSession('mount', { level: 'info' })
    clearTerminalTextPublishTimer()
    terminalTextRef.current = ''
    setTerminalSnapshot(null)
    setTerminalText('')
    setTerminalInfo(null)
    setResizeControl(defaultTerminalResizeControl)
    scrollbackPrefixTextRef.current = ''
    loadedScrollbackRowsRef.current = 0
    historyRevisionRef.current = 0
    loadingScrollbackRef.current = false
    hasMoreScrollbackRef.current = true
    recoveringInputRef.current = false
    terminalRecoveryPromiseRef.current = null
    terminalRecoverySeqRef.current += 1
    sessionConnectionPhaseRef.current = null
    outputStatsRef.current = {
      chunks: 0,
      bytes: 0,
      chars: 0,
      trimmedChars: 0,
      lastLogAt: terminalNow(),
      lastBytes: 0,
      lastChunks: 0,
    }
    clearPendingRecoveryInput()
    dispatch({ type: 'user.connectMachine', machineId: options.machineId })

    options.session.getConnectionInfo().then((info) => {
      if (cancelled) return
      connectionIdRef.current = info.connectionId
      logSession('connection_info', {
        level: 'info',
        details: {
          path: info.path,
          relayInUse: info.relayInUse === true,
          type: info.type,
          rtt: info.rtt,
        },
      })
      dispatch({ type: 'connection.connected', path: info.path, connectionId: info.connectionId })
      dispatch({ type: 'user.openTerminal', machineId: options.machineId, terminalId: options.terminalId })
      return createProtocolSession(options.session, options.machineId, options.terminalId, info)
    }).then((protocolSession) => {
      if (cancelled || !protocolSession) return
      protocolSessionRef.current = protocolSession
      client.connect(options.terminalId, protocolSession)
    }).catch((err: unknown) => {
      if (cancelled) return
      const reason = err instanceof Error ? err.message : String(err)
      logSession('connect_failed', {
        level: 'error',
        details: { reason },
      })
      dispatch({ type: 'connection.failed', reason, recoverable: true, surface: 'banner' })
    })

    return () => {
      cancelled = true
      logSession('unmount', {
        level: 'info',
        details: {
          chunks: outputStatsRef.current.chunks,
          bytes: outputStatsRef.current.bytes,
          chars: outputStatsRef.current.chars,
          trimmedChars: outputStatsRef.current.trimmedChars,
        },
      })
      client.disconnect()
      protocolSessionRef.current = null
      clientRef.current = null
      clearPendingRecoveryInput()
      clearTerminalTextPublishTimer()
      dispatch({ type: 'user.release' })
    }
  }, [callbacks, clearPendingRecoveryInput, clearTerminalTextPublishTimer, logSession, options.machineId, options.terminalId, options.session])

  useEffect(() => {
    sessionConnectionPhaseRef.current = null
    const candidate = options.session as RtcSession & Partial<RtcSessionConnectionStateEvents>
    if (typeof candidate.subscribeConnectionState !== 'function') return
    const subscription = candidate.subscribeConnectionState((state) => {
      const previousPhase = sessionConnectionPhaseRef.current
      sessionConnectionPhaseRef.current = state.phase
      if (state.phase === 'connected' && previousPhase !== null && previousPhase !== 'connected') {
        void recoverTerminalChannel('terminal session connection restored', {
          session: options.session,
          forceTerminalChannel: true,
          supersede: true,
          showClosedReason: false,
        })
      }
      if (state.phase === 'reconnecting' || state.phase === 'waiting_network') {
        dispatch({ type: 'connection.disconnected', reason: state.statusText })
      } else if (state.phase === 'failed') {
        dispatch({
          type: 'connection.failed',
          reason: state.failReason ?? state.statusText ?? 'terminal session connection failed',
          recoverable: true,
          surface: 'banner',
        })
      }
    })
    return () => subscription.close()
  }, [options.session, recoverTerminalChannel])

  const sendInput = useCallback((data: string, size?: { cols: number; rows: number }) => {
    const message = { data, ...(size ? { size } : {}) }
    rememberRecoveryInput(message)
    const sent = clientRef.current?.sendInput(data, size) ?? false
    if (sent) return true
    void recoverInputChannel(message, 'terminal input send failed')
    return false
  }, [recoverInputChannel, rememberRecoveryInput])

  const sendResize = useCallback((cols: number, rows: number) => {
    return clientRef.current?.sendResize(cols, rows) ?? false
  }, [logSession])

  const requestResizeOwner = useCallback((size?: { cols: number; rows: number }) => {
    return clientRef.current?.requestResizeOwner(size) ?? Promise.reject(new Error('terminal client is not connected'))
  }, [])

  const releaseResizeOwner = useCallback(() => {
    return clientRef.current?.releaseResizeOwner() ?? Promise.reject(new Error('terminal client is not connected'))
  }, [])

  const markSyncLost = useCallback((reason?: string) => {
    clientRef.current?.markSyncLost(reason)
  }, [])

  const loadScrollback = useCallback(async (limit = 100): Promise<TerminalScrollbackLoadResult> => {
    if (loadingScrollbackRef.current || !hasMoreScrollbackRef.current) {
      return {
        loadedRows: 0,
        totalRows: loadedScrollbackRowsRef.current,
        hasMore: hasMoreScrollbackRef.current,
      }
    }
    const client = clientRef.current
    if (!client) {
      return {
        loadedRows: 0,
        totalRows: loadedScrollbackRowsRef.current,
        hasMore: false,
      }
    }
    loadingScrollbackRef.current = true
    const startedAt = terminalNow()
    logSession('scrollback_load_start', {
      details: {
        offset: loadedScrollbackRowsRef.current,
        limit,
      },
    })
    try {
      const page = await client.loadScrollback(loadedScrollbackRowsRef.current, limit)
      const loadedRows = page.rows
      if (loadedRows === 0) {
        hasMoreScrollbackRef.current = false
        logSession('scrollback_load_empty', {
          details: {
            elapsedMs: Math.round(terminalNow() - startedAt),
            offset: loadedScrollbackRowsRef.current,
            limit,
          },
        })
        return {
          loadedRows: 0,
          totalRows: loadedScrollbackRowsRef.current,
          hasMore: false,
        }
      }
      loadedScrollbackRowsRef.current += loadedRows
      hasMoreScrollbackRef.current = page.hasMore
      const revision = historyRevisionRef.current + 1
      historyRevisionRef.current = revision
      const prefix = page.replay
      scrollbackPrefixTextRef.current = prependTerminalText(prefix, scrollbackPrefixTextRef.current)
      setTerminalSnapshot((current) => current ? {
        ...current,
        history: {
          revision,
          prependedRows: loadedRows,
          loadedRows: loadedScrollbackRowsRef.current,
          hasMore: page.hasMore,
        },
      } : current)
      terminalTextRef.current = prependTerminalText(prefix, terminalTextRef.current)
      publishTerminalTextNow()
      logSession('scrollback_load_success', {
        details: {
          elapsedMs: Math.round(terminalNow() - startedAt),
          loadedRows,
          totalRows: loadedScrollbackRowsRef.current,
          hasMore: page.hasMore,
          prefixChars: prefix.length,
        },
      })
      return {
        loadedRows,
        totalRows: loadedScrollbackRowsRef.current,
        hasMore: page.hasMore,
      }
    } finally {
      loadingScrollbackRef.current = false
    }
  }, [publishTerminalTextNow])

  const handleAppResume = useCallback((resumeKind: 'quick' | 'cold' | 'frozen') => {
    dispatch({ type: 'app.resume', resumeKind })
  }, [])

  const reattach = useCallback((session: RtcSession, reattachOptions: { forceTerminalChannel?: boolean } = {}) => {
    void recoverTerminalChannel('terminal reattach requested', {
      session,
      forceTerminalChannel: reattachOptions.forceTerminalChannel,
      supersede: true,
      showClosedReason: false,
    })
  }, [recoverTerminalChannel])

  return {
    snapshot,
    terminalSnapshot,
    terminalText,
    terminalInfo,
    resizeControl,
    sendInput,
    sendResize,
    requestResizeOwner,
    releaseResizeOwner,
    loadScrollback,
    markSyncLost,
    handleAppResume,
    reattach,
    client: clientRef.current,
  }
}

export type TerminalSessionMessage = ConnectionMessage

function prependTerminalText(prefix: string, current: string): string {
  if (!prefix) return current
  return trimTerminalTextPreservingPrefix(prefix, current, '\r\n')
}

function joinTerminalText(prefix: string, current: string): string {
  if (!prefix) return current
  if (!current) return prefix
  return `${prefix}\r\n${current}`
}

function joinHistoryAndSnapshotText(prefix: string, current: string, rows: number): string {
  if (!prefix) return current
  if (!current) return prefix
  const spacerRows = Math.max(0, rows - 1)
  const spacer = spacerRows > 0 ? `\r\n${'\n'.repeat(spacerRows)}` : '\r\n'
  return trimTerminalTextPreservingPrefix(prefix, current, spacer)
}

function trimTerminalTextPreservingPrefix(prefix: string, current: string, separator: string): string {
  if (!prefix) return trimTerminalTextToRecentWindow(current)
  if (!current) return trimTerminalTextToRecentWindow(prefix)
  const joined = joinTerminalTextWithSeparator(prefix, current, separator)
  if (joined.length <= terminalTextSoftLimitChars) return joined
  const currentLimit = terminalTextSoftLimitChars - prefix.length - separator.length
  if (currentLimit <= 0) {
    return trimTerminalTextToRecentWindow(prefix)
  }
  const trimmedCurrent = trimTerminalTextToRecentWindow(current, currentLimit)
  if (!trimmedCurrent) return trimTerminalTextToRecentWindow(prefix)
  return joinTerminalTextWithSeparator(prefix, trimmedCurrent, separator)
}

function joinTerminalTextWithSeparator(prefix: string, current: string, separator: string): string {
  if (!prefix) return current
  if (!current) return prefix
  return `${prefix}${separator}${current}`
}

function createProtocolSession(
  session: RtcSession,
  machineId: string,
  terminalId: string,
  connectionInfo: Awaited<ReturnType<RtcSession['getConnectionInfo']>>,
): TerminalProtocolSession {
  let protocol: TerminalProtocolSession | null = null
  let protocolPromise: Promise<TerminalProtocolSession> | null = null
  const pendingSubscribers = new Map<string, Set<Parameters<TerminalProtocolSession['subscribeTerminal']>[1]>>()
  let closed = false
  let pendingChannel: Awaited<ReturnType<RtcSession['openTerminal']>> | null = null
  const resetProtocol = (id: string) => {
    protocol?.closeTerminalChannel(id)
    pendingChannel?.close()
    protocol = null
    protocolPromise = null
    pendingChannel = null
    closeRtcTerminalDataChannel(session, id)
  }
  const ensureProtocol = async (id: string): Promise<TerminalProtocolSession> => {
    if (protocol) return protocol
    if (closed) throw new Error(`terminal protocol session ${terminalId} is closed`)
    if (!protocolPromise) {
      protocolPromise = session.openTerminal(id).then((channel) => {
        if (closed) {
          channel.close()
          return createClosedProtocolSession(connectionInfo, `terminal protocol session ${terminalId} is closed`)
        }
        pendingChannel = channel
        protocol = createTerminalProtocolClient({
          channel,
          machineId,
          terminalId,
          connectionInfo,
          resizePolicy: 'follower',
          surfaceId: `app:${machineId}:terminal:${terminalId}`,
          autoRequestResizeOwner: true,
        })
        for (const [pendingTerminalId, handlers] of pendingSubscribers) {
          for (const handler of handlers) {
            protocol.subscribeTerminal(pendingTerminalId, handler)
          }
        }
        return protocol
      }).catch((err: unknown) => {
        if (!closed) throw err
        return createClosedProtocolSession(connectionInfo, err instanceof Error ? err.message : String(err))
      })
    }
    return protocolPromise
  }
  return {
    async openTerminal(id) {
      try {
        const current = await ensureProtocol(id)
        return await current.openTerminal(id)
      } catch (err) {
        if (!closed) resetProtocol(id)
        throw err
      }
    },
    getConnectionInfo: () => Promise.resolve(connectionInfo),
    subscribeTerminal(id, handler) {
      if (protocol) return protocol.subscribeTerminal(id, handler)
      let handlers = pendingSubscribers.get(id)
      if (!handlers) {
        handlers = new Set()
        pendingSubscribers.set(id, handlers)
      }
      handlers.add(handler)
      void ensureProtocol(id)
      return () => {
        handlers?.delete(handler)
      }
    },
    async loadScrollback(id, offset, limit) {
      const current = await ensureProtocol(id)
      return current.loadScrollback(id, offset, limit)
    },
    closeTerminalChannel(id) {
      closed = true
      resetProtocol(id)
    },
    markSyncLost(id, reason) {
      if (closed) return
      if (protocol?.markSyncLost) {
        protocol.markSyncLost(id, reason)
        return
      }
      void ensureProtocol(id).then((current) => {
        current.markSyncLost?.(id, reason)
      }).catch(() => {})
    },
    async requestResizeOwner(id, size) {
      return withRecoverableProtocolRetry(id, async (current) => {
        if (!current.requestResizeOwner) throw new Error('terminal resize ownership is not available')
        return current.requestResizeOwner(id, size)
      })
    },
    async releaseResizeOwner(id) {
      return withRecoverableProtocolRetry(id, async (current) => {
        if (!current.releaseResizeOwner) throw new Error('terminal resize ownership is not available')
        return current.releaseResizeOwner(id)
      })
    },
  }

  async function withRecoverableProtocolRetry<T>(
    id: string,
    run: (current: TerminalProtocolSession) => Promise<T>,
  ): Promise<T> {
    const current = await ensureProtocol(id)
    try {
      return await run(current)
    } catch (err) {
      if (closed || !isRecoverableTerminalProtocolError(err)) throw err
      resetProtocol(id)
      const next = await ensureProtocol(id)
      return run(next)
    }
  }
}

function closeRtcTerminalDataChannel(session: RtcSession, terminalId: string): void {
  const controller = session as RtcSession & Partial<RtcTerminalDataChannelController>
  if (typeof controller.closeTerminalDataChannel === 'function') {
    controller.closeTerminalDataChannel(terminalId)
  }
}

function isRecoverableTerminalProtocolError(err: unknown): boolean {
  if (!(err instanceof Error)) return false
  return /channel .*not open|channel is not open|attachment channel \d+ does not match terminal|terminal attachment channel \d+ is not attached|ensure resize requires attachment/i.test(err.message)
}

function shouldRecoverRecentInput(reason?: string): boolean {
  if (!reason) return false
  return /not open|send failed|unavailable|input send failed|not attached|readonly/i.test(reason)
}

function shouldRecoverTerminalChannel(reason?: string): boolean {
  if (!reason) return false
  return /terminal data channel|terminal channel send failed|terminal protocol .* timed out|native bridge|not open|closed|timed out|send failed|not attached|attachment channel|ensure resize requires attachment/i.test(reason)
}

function createClosedProtocolSession(
  connectionInfo: Awaited<ReturnType<RtcSession['getConnectionInfo']>>,
  reason: string,
): TerminalProtocolSession {
  return {
    async openTerminal() {
      throw new Error(reason)
    },
    getConnectionInfo: () => Promise.resolve(connectionInfo),
    subscribeTerminal() {
      return () => {}
    },
    async loadScrollback() {
      return {
        beforeOffset: 0,
        limit: 0,
        rows: 0,
        replay: '',
        hasMore: false,
      }
    },
    closeTerminalChannel() {},
    markSyncLost() {},
  }
}
