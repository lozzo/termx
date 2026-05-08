import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react'
import {
  initialConnectionSnapshot,
  reduceConnectionMessage,
  type ConnectionMessage,
  type ConnectionSnapshot,
} from './connectionMessageReducer'
import {
  defaultTerminalResizeControl,
  TerminalClient,
  type TerminalClientCallbacks,
  type TerminalProtocolSession,
  type TerminalResizeControl,
  type TerminalSnapshotPayload,
} from './terminalClient'
import { createTerminalProtocolClient } from './terminalProtocolClient'
import type { Terminal } from './model'
import type { RtcSession, RtcTerminalDataChannelController } from './transport'

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
  sendInput(data: string, size?: { cols: number; rows: number }): void
  sendResize(cols: number, rows: number): void
  handleAppResume(resumeKind: 'quick' | 'cold' | 'frozen'): void
  reattach(session: RtcSession): void
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

  const callbacks = useMemo<TerminalClientCallbacks>(() => ({
    onOutput: (data) => {
      setTerminalText((current) => current + new TextDecoder().decode(data))
    },
    onSnapshot: (nextSnapshot) => {
      setTerminalSnapshot(nextSnapshot)
      setTerminalText(nextSnapshot.replay ?? nextSnapshot.text)
    },
    onTerminalInfo: setTerminalInfo,
    onResizeControl: setResizeControl,
    onLifecycle: (message) => dispatch(message),
    onError: (message) => dispatch({ type: 'connection.failed', reason: message, recoverable: true, surface: 'banner' }),
    onClose: () => {},
    onOpen: () => {},
    onInputDropped: () => dispatch({ type: 'connection.failed', reason: 'terminal input dropped', recoverable: true, surface: 'toast' }),
  }), [options.machineId, options.terminalId])

  useEffect(() => {
    sessionRef.current = options.session
    const client = new TerminalClient(callbacks)
    clientRef.current = client

    let cancelled = false
    setTerminalSnapshot(null)
    setTerminalText('')
    setTerminalInfo(null)
    setResizeControl(defaultTerminalResizeControl)
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
      dispatch({ type: 'user.release' })
    }
  }, [callbacks, options.machineId, options.terminalId, options.session])

  const sendInput = useCallback((data: string, size?: { cols: number; rows: number }) => {
    clientRef.current?.sendInput(data, size)
  }, [])

  const sendResize = useCallback((cols: number, rows: number) => {
    clientRef.current?.sendResize(cols, rows)
  }, [])

  const handleAppResume = useCallback((resumeKind: 'quick' | 'cold' | 'frozen') => {
    dispatch({ type: 'app.resume', resumeKind })
  }, [])

  const reattach = useCallback((session: RtcSession) => {
    const previousSession = sessionRef.current
    protocolSessionRef.current?.closeTerminalChannel(options.terminalId)
    closeRtcTerminalDataChannel(previousSession, options.terminalId)
    setResizeControl(defaultTerminalResizeControl)
    sessionRef.current = session
    session.getConnectionInfo().then((info) => {
      connectionIdRef.current = info.connectionId
      const protocolSession = createProtocolSession(session, options.machineId, options.terminalId, info)
      protocolSessionRef.current = protocolSession
      clientRef.current?.reattach(protocolSession)
      dispatch({ type: 'connection.verified', connectionId: info.connectionId })
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
    closeTerminalChannel() {},
  }
}
