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
import { rowsToPlainText, rowsToReplay } from './termxProtocol'
import { createTerminalProtocolClient } from './terminalProtocolClient'
import type { Terminal } from '../core/model'
import type { RtcSession, RtcTerminalDataChannelController } from '../core/transport'

const recentInputRecoveryWindowMs = 1500

export interface UseTerminalSessionOptions {
  machineId: string
  terminalId: string
  session: RtcSession
}

export interface UseTerminalSessionResult {
  snapshot: ConnectionSnapshot
  terminalSnapshot: TerminalSnapshotPayload | null
  terminalText: string
  terminalInfo: Terminal | null
  resizeControl: TerminalResizeControl
  sendInput(data: string, size?: { cols: number; rows: number }): boolean
  sendResize(cols: number, rows: number): boolean
  loadScrollback(limit?: number): Promise<TerminalScrollbackLoadResult>
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
  const loadedScrollbackRowsRef = useRef(0)
  const historyRevisionRef = useRef(0)
  const loadingScrollbackRef = useRef(false)
  const hasMoreScrollbackRef = useRef(true)
  const recoveringInputRef = useRef(false)
  const pendingRecoveryInputRef = useRef<{ data: string; size?: { cols: number; rows: number } } | null>(null)
  const pendingRecoveryInputTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const clearPendingRecoveryInput = useCallback((pending?: { data: string; size?: { cols: number; rows: number } } | null) => {
    if (pending && pendingRecoveryInputRef.current !== pending) return
    if (pendingRecoveryInputTimerRef.current !== null) {
      clearTimeout(pendingRecoveryInputTimerRef.current)
      pendingRecoveryInputTimerRef.current = null
    }
    pendingRecoveryInputRef.current = null
  }, [])

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
      dispatch({
        type: 'terminal.channelClosed',
        machineId: options.machineId,
        terminalId: options.terminalId,
        reason,
      })
      protocolSessionRef.current?.closeTerminalChannel(options.terminalId)
      closeRtcTerminalDataChannel(session, options.terminalId)
      const info = await session.getConnectionInfo()
      connectionIdRef.current = info.connectionId
      const protocolSession = createProtocolSession(session, options.machineId, options.terminalId, info)
      protocolSessionRef.current = protocolSession
      await client.reattach(protocolSession)
      if (pendingRecoveryInputRef.current !== pending) return
      clearPendingRecoveryInput(pending)
      const retried = client.sendInput(pending.data, pending.size)
      recoveringInputRef.current = false
      if (!retried) {
        dispatch({ type: 'connection.failed', reason: 'terminal input retry failed', recoverable: true, surface: 'banner' })
      } else {
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
  }, [clearPendingRecoveryInput, options.machineId, options.terminalId, rememberRecoveryInput])

  const callbacks = useMemo<TerminalClientCallbacks>(() => ({
    onOutput: (data) => {
      setTerminalText((current) => current + new TextDecoder().decode(data))
    },
    onSnapshot: (nextSnapshot) => {
      setTerminalSnapshot(nextSnapshot)
      loadedScrollbackRowsRef.current = nextSnapshot.scrollbackRows?.length ?? 0
      hasMoreScrollbackRef.current = true
      setTerminalText(nextSnapshot.replay ?? nextSnapshot.text)
    },
    onTerminalInfo: setTerminalInfo,
    onResizeControl: setResizeControl,
    onLifecycle: (message) => dispatch(message),
    onError: (message) => dispatch({ type: 'connection.failed', reason: message, recoverable: true, surface: 'banner' }),
    onClose: (reason) => {
      const pending = pendingRecoveryInputRef.current
      if (pending && (recoveringInputRef.current || shouldRecoverRecentInput(reason))) {
        void recoverInputChannel(pending, reason ?? 'terminal channel closed')
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
      }
    },
  }), [recoverInputChannel])

  useEffect(() => {
    sessionRef.current = options.session
    const client = new TerminalClient(callbacks)
    clientRef.current = client

    let cancelled = false
    setTerminalSnapshot(null)
    setTerminalText('')
    setTerminalInfo(null)
    setResizeControl(defaultTerminalResizeControl)
    loadedScrollbackRowsRef.current = 0
    historyRevisionRef.current = 0
    loadingScrollbackRef.current = false
    hasMoreScrollbackRef.current = true
    recoveringInputRef.current = false
    clearPendingRecoveryInput()
    dispatch({ type: 'user.connectMachine', machineId: options.machineId })

    options.session.getConnectionInfo().then((info) => {
      if (cancelled) return
      connectionIdRef.current = info.connectionId
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
      dispatch({ type: 'connection.failed', reason, recoverable: true, surface: 'banner' })
    })

    return () => {
      cancelled = true
      client.disconnect()
      protocolSessionRef.current = null
      clientRef.current = null
      clearPendingRecoveryInput()
      dispatch({ type: 'user.release' })
    }
  }, [callbacks, clearPendingRecoveryInput, options.machineId, options.terminalId, options.session])

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
    try {
      const page = await client.loadScrollback(loadedScrollbackRowsRef.current, limit)
      const rows = page.rows
      const loadedRows = rows.length
      if (loadedRows === 0) {
        hasMoreScrollbackRef.current = false
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
      const text = rowsToPlainText(rows)
      const replay = rowsToReplay(rows)
      setTerminalSnapshot((current) => current ? {
        ...current,
        history: {
          revision,
          prependedRows: loadedRows,
          loadedRows: loadedScrollbackRowsRef.current,
          hasMore: page.hasMore,
        },
      } : current)
      setTerminalText((current) => `${replay || text}\r\n${current}`)
      return {
        loadedRows,
        totalRows: loadedScrollbackRowsRef.current,
        hasMore: page.hasMore,
      }
    } finally {
      loadingScrollbackRef.current = false
    }
  }, [])

  const handleAppResume = useCallback((resumeKind: 'quick' | 'cold' | 'frozen') => {
    dispatch({ type: 'app.resume', resumeKind })
  }, [])

  const reattach = useCallback((session: RtcSession, reattachOptions: { forceTerminalChannel?: boolean } = {}) => {
    const previousSession = sessionRef.current
    protocolSessionRef.current?.closeTerminalChannel(options.terminalId)
    closeRtcTerminalDataChannel(previousSession, options.terminalId)
    if (reattachOptions.forceTerminalChannel) {
      closeRtcTerminalDataChannel(session, options.terminalId)
    }
    setResizeControl(defaultTerminalResizeControl)
    sessionRef.current = session
    session.getConnectionInfo().then((info) => {
      connectionIdRef.current = info.connectionId
      const protocolSession = createProtocolSession(session, options.machineId, options.terminalId, info)
      protocolSessionRef.current = protocolSession
      void clientRef.current?.reattach(protocolSession).then(() => {
        dispatch({ type: 'connection.verified', connectionId: info.connectionId })
      })
    }).catch((err: unknown) => {
      dispatch({ type: 'connection.failed', reason: err instanceof Error ? err.message : String(err), recoverable: true, surface: 'banner' })
    })
  }, [options.machineId, options.terminalId])

  return {
    snapshot,
    terminalSnapshot,
    terminalText,
    terminalInfo,
    resizeControl,
    sendInput,
    sendResize,
    loadScrollback,
    handleAppResume,
    reattach,
    client: clientRef.current,
  }
}

export type TerminalSessionMessage = ConnectionMessage

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
  }
}

function closeRtcTerminalDataChannel(session: RtcSession, terminalId: string): void {
  const controller = session as RtcSession & Partial<RtcTerminalDataChannelController>
  if (typeof controller.closeTerminalDataChannel === 'function') {
    controller.closeTerminalDataChannel(terminalId)
  }
}

function shouldRecoverRecentInput(reason?: string): boolean {
  if (!reason) return false
  return /not open|send failed|unavailable|input send failed|not attached|readonly/i.test(reason)
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
        offset: 0,
        limit: 0,
        rows: [],
        rawSnapshot: {},
        snapshot: { text: '', cols: 0, rows: 0 },
        hasMore: false,
      }
    },
    closeTerminalChannel() {},
  }
}
