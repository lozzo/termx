import { useCallback, useEffect, useMemo, useState } from 'react'
import { FileManager } from './FileManager'
import { LocalPairPanel } from './LocalPairPanel'
import type { LocalAppCrypto, LocalAppIdentityStore } from './localAppIdentity'
import { Terminal } from './Terminal'
import { TerminalList } from './TerminalList'
import type { Machine, Terminal as RemoteTerminal } from './model'
import type { LocalAgentApi, PeerTransport } from './transport'
import type { TerminalTransport } from './terminalClient'

export interface LocalRemoteTransportInput {
  machineId: string
  terminalId: string
}

export type LocalRemoteTransportFactory = (input: LocalRemoteTransportInput) => PeerTransport & TerminalTransport

export interface LocalRemoteAppProps {
  api: Pick<LocalAgentApi, 'getStatus' | 'listTerminals'>
  createTransport: LocalRemoteTransportFactory
  className?: string | undefined
  pair?: {
    api: Pick<LocalAgentApi, 'pair'>
    storage: LocalAppIdentityStore
    crypto: LocalAppCrypto
    appName: string
  } | undefined
}

export function LocalRemoteApp({ api, createTransport, className, pair }: LocalRemoteAppProps) {
  const [machine, setMachine] = useState<Machine | null>(null)
  const [terminals, setTerminals] = useState<RemoteTerminal[]>([])
  const [activeTerminalId, setActiveTerminalId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [connectedTransport, setConnectedTransport] = useState<(PeerTransport & TerminalTransport) | null>(null)
  const [transportRetryToken, setTransportRetryToken] = useState(0)

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const status = await api.getStatus()
        const terminalList = await api.listTerminals()
        if (cancelled) return
        setMachine(status.machine)
        setTerminals(terminalList)
        setActiveTerminalId((current) => current ?? terminalList[0]?.terminalId ?? null)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [api])

  useEffect(() => {
    if (!machine || !activeTerminalId) {
      setConnectedTransport(null)
      return
    }
    let cancelled = false
    let transport: PeerTransport & TerminalTransport
    try {
      transport = createTransport({
        machineId: machine.machineId,
        terminalId: activeTerminalId,
      })
    } catch (err) {
      setConnectedTransport(null)
      setError(err instanceof Error ? err.message : String(err))
      return
    }
    setConnectedTransport(null)
    transport.connect({
      machineId: machine.machineId,
      terminalId: activeTerminalId,
      mode: 'local',
    }).then(() => {
      if (!cancelled) setConnectedTransport(transport)
    }).catch((err: unknown) => {
      if (!cancelled) setError(err instanceof Error ? err.message : String(err))
    })
    return () => {
      cancelled = true
      void transport.disconnect()
    }
  }, [activeTerminalId, createTransport, machine, transportRetryToken])

  const openTerminal = useCallback((intent: { machineId: string; terminalId: string }) => {
    if (machine && intent.machineId !== machine.machineId) {
      setError(`terminal machine mismatch: ${intent.machineId} != ${machine.machineId}`)
      return
    }
    setActiveTerminalId(intent.terminalId)
  }, [machine])

  if (error && !machine) {
    return <section className={className}><div role="alert">{error}</div></section>
  }

  if (!machine) {
    return <section className={className}>Loading</section>
  }

  return (
    <main className={className} data-machine-id={machine.machineId}>
      {error ? <div role="alert">{error}</div> : null}

      <TerminalList
        machineId={machine.machineId}
        terminals={terminals}
        onOpenTerminal={openTerminal}
        {...(activeTerminalId ? { activeTerminalId } : {})}
      />

      {activeTerminalId && connectedTransport ? (
        <>
          <Terminal
            machineId={machine.machineId}
            terminalId={activeTerminalId}
            transport={connectedTransport}
          />
          <FileManager
            machineId={machine.machineId}
            terminalId={activeTerminalId}
            transport={connectedTransport}
            initialPath="/"
          />
        </>
      ) : null}

      {pair ? (
        <LocalPairPanel
          api={pair.api}
          storage={pair.storage}
          crypto={pair.crypto}
          appName={pair.appName}
          onPaired={() => {
            setError(null)
            setTransportRetryToken((current) => current + 1)
          }}
        />
      ) : null}
    </main>
  )
}
