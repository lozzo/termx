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
  type TerminalResizeControl,
  type TerminalSnapshotPayload,
  type TerminalTransport,
} from './terminalClient'
import type { Terminal } from './model'

export interface UseTerminalSessionOptions {
  machineId: string
  terminalId: string
  transport: TerminalTransport
}

export interface UseTerminalSessionResult {
  snapshot: ConnectionSnapshot
  terminalSnapshot: TerminalSnapshotPayload | null
  terminalText: string
  terminalInfo: Terminal | null
  resizeControl: TerminalResizeControl
  sendInput(data: string): void
  sendResize(cols: number, rows: number): void
  handleAppResume(resumeKind: 'quick' | 'cold' | 'frozen'): void
  reattach(transport: TerminalTransport): void
  client: TerminalClient | null
}

export function useTerminalSession(options: UseTerminalSessionOptions): UseTerminalSessionResult {
  const [snapshot, dispatch] = useReducer(reduceConnectionMessage, undefined, initialConnectionSnapshot)
  const [terminalSnapshot, setTerminalSnapshot] = useState<TerminalSnapshotPayload | null>(null)
  const [terminalText, setTerminalText] = useState('')
  const [terminalInfo, setTerminalInfo] = useState<Terminal | null>(null)
  const [resizeControl, setResizeControl] = useState<TerminalResizeControl>(defaultTerminalResizeControl)
  const clientRef = useRef<TerminalClient | null>(null)
  const transportRef = useRef(options.transport)
  const connectionIdRef = useRef('terminal-connection')

  const callbacks = useMemo<TerminalClientCallbacks>(() => ({
    onOutput: (data) => {
      setTerminalText((current) => current + new TextDecoder().decode(data))
    },
    onSnapshot: (nextSnapshot) => {
      setTerminalSnapshot(nextSnapshot)
      setTerminalText(nextSnapshot.text)
    },
    onTerminalInfo: setTerminalInfo,
    onResizeControl: setResizeControl,
    onLifecycle: (message) => dispatch(message),
    onError: (message) => dispatch({ type: 'transport.failed', reason: message, recoverable: true, surface: 'banner' }),
    onClose: () => {},
    onOpen: () => {},
    onInputDropped: () => dispatch({ type: 'transport.failed', reason: 'terminal input dropped', recoverable: true, surface: 'toast' }),
  }), [options.machineId, options.terminalId])

  useEffect(() => {
    transportRef.current = options.transport
    const client = new TerminalClient(callbacks)
    clientRef.current = client

    let cancelled = false
    dispatch({ type: 'user.connectMachine', machineId: options.machineId })

    options.transport.getConnectionInfo().then((info) => {
      if (cancelled) return
      connectionIdRef.current = info.connectionId
      dispatch({ type: 'transport.connected', mode: info.mode, connectionId: info.connectionId })
      dispatch({ type: 'user.openTerminal', machineId: options.machineId, terminalId: options.terminalId })
      client.connect(options.terminalId, options.transport)
    }).catch((err: unknown) => {
      if (cancelled) return
      const reason = err instanceof Error ? err.message : String(err)
      dispatch({ type: 'transport.failed', reason, recoverable: true, surface: 'banner' })
    })

    return () => {
      cancelled = true
      client.disconnect()
      clientRef.current = null
      dispatch({ type: 'user.release' })
    }
  }, [callbacks, options.machineId, options.terminalId, options.transport])

  const sendInput = useCallback((data: string) => {
    clientRef.current?.sendInput(data)
  }, [])

  const sendResize = useCallback((cols: number, rows: number) => {
    clientRef.current?.sendResize(cols, rows)
  }, [])

  const handleAppResume = useCallback((resumeKind: 'quick' | 'cold' | 'frozen') => {
    dispatch({ type: 'app.resume', resumeKind })
  }, [])

  const reattach = useCallback((transport: TerminalTransport) => {
    transportRef.current = transport
    clientRef.current?.reattach(transport)
    dispatch({ type: 'transport.verified', connectionId: connectionIdRef.current })
  }, [])

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
