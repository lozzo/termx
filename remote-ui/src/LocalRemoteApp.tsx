import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronLeft, Folder, KeyRound, Monitor, X } from 'lucide-react'
import { FileManager } from './FileManager'
import { LocalPairPanel } from './LocalPairPanel'
import type { LocalAppCrypto, LocalAppIdentityStore } from './localAppIdentity'
import { MobileTerminalKeybar } from './MobileTerminalKeybar'
import type { TerminalModifierState } from './mobileTerminalInput'
import { Terminal, type TerminalHandle } from './Terminal'
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

type MobileSheet = 'terminals' | 'pair' | null
type PanelMode = 'terminal' | 'files'
type AppPage = 'terminal-list' | 'terminal'

export function LocalRemoteApp({ api, createTransport, className, pair }: LocalRemoteAppProps) {
  const [machine, setMachine] = useState<Machine | null>(null)
  const [terminals, setTerminals] = useState<RemoteTerminal[]>([])
  const [activeTerminalId, setActiveTerminalId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pairStatus, setPairStatus] = useState<string | null>(null)
  const [connectedTransport, setConnectedTransport] = useState<(PeerTransport & TerminalTransport) | null>(null)
  const [transportRetryToken, setTransportRetryToken] = useState(0)

  const [page, setPage] = useState<AppPage>('terminal-list')
  const [panelMode, setPanelMode] = useState<PanelMode>('terminal')
  const [mobileSheet, setMobileSheet] = useState<MobileSheet>(null)
  const [modifierState, setModifierState] = useState<TerminalModifierState>({ ctrl: 'off', alt: 'off' })
  const terminalRef = useRef<TerminalHandle | null>(null)

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const status = await api.getStatus()
        const terminalList = await api.listTerminals()
        if (cancelled) return
        setMachine(status.machine)
        setTerminals(terminalList)
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
    if (!machine || !activeTerminalId || page !== 'terminal') {
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
      if (pair) setMobileSheet('pair')
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
  }, [activeTerminalId, createTransport, machine, page, transportRetryToken])

  const openTerminal = useCallback((intent: { machineId: string; terminalId: string }) => {
    if (machine && intent.machineId !== machine.machineId) {
      setError(`terminal machine mismatch: ${intent.machineId} != ${machine.machineId}`)
      return
    }
    setActiveTerminalId(intent.terminalId)
    setPage('terminal')
    setPanelMode('terminal')
    setMobileSheet(null)
  }, [machine])

  const handlePaired = useCallback((machineId: string) => {
    setError(null)
    setPairStatus(`Paired with ${machineId}`)
    setTransportRetryToken((current) => current + 1)
    setMobileSheet(null)
    setPanelMode('terminal')
  }, [])

  const showTerminalListPage = useCallback(() => {
    setPage('terminal-list')
    setPanelMode('terminal')
    setMobileSheet(null)
    terminalRef.current?.adjustInputPosition(0)
  }, [])

  const openFiles = useCallback(() => {
    setPanelMode('files')
    setMobileSheet(null)
    window.setTimeout(() => terminalRef.current?.fit(), 0)
  }, [])

  const openTerminalPanel = useCallback(() => {
    setPanelMode('terminal')
    setMobileSheet(null)
    window.setTimeout(() => {
      terminalRef.current?.fit()
      terminalRef.current?.focus()
    }, 0)
  }, [])

  const resetKeyboardOffset = useCallback(() => {
    terminalRef.current?.adjustInputPosition(0)
  }, [])

  const activeTerminal = terminals.find((terminal) => terminal.terminalId === activeTerminalId)
  const activeTerminalTitle = activeTerminal?.title || activeTerminal?.command || activeTerminalId || 'Terminal'

  const renderTerminalListPage = () => {
    if (!machine) return null

    return (
      <main className="relative flex min-h-0 flex-1 flex-col bg-zinc-50" data-testid="termx-terminal-list-page">
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-zinc-200 bg-white px-4">
          <div className="flex min-w-0 items-center gap-2">
            <Monitor className="h-4 w-4 shrink-0 text-zinc-500" />
            <div className="min-w-0">
              <h1 className="truncate text-sm font-semibold text-zinc-900">{machine.name || machine.machineId}</h1>
              <p className="truncate text-[11px] text-zinc-500">{machine.machineId}</p>
            </div>
          </div>
          {pair ? (
            <button
              type="button"
              aria-label="Pair device"
              className="flex h-9 w-9 items-center justify-center rounded-md text-zinc-600 active:bg-zinc-100"
              onClick={() => setMobileSheet('pair')}
            >
              <KeyRound className="h-5 w-5" />
            </button>
          ) : null}
        </header>
        {error ? (
          <div className="m-3 shrink-0 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 shadow-sm" role="alert">
            {error}
          </div>
        ) : null}
        {pairStatus ? (
          <div className="mx-3 mt-3 shrink-0 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-800 shadow-sm" role="status">
            {pairStatus}
          </div>
        ) : null}
        <div className="min-h-0 flex-1 overflow-y-auto p-3">
          <h2 className="mb-2 px-1 text-xs font-semibold uppercase tracking-wider text-zinc-500">Terminals</h2>
          <TerminalList
            machineId={machine.machineId}
            terminals={terminals}
            onOpenTerminal={openTerminal}
            activeTerminalId={activeTerminalId ?? undefined}
          />
        </div>

        {mobileSheet === 'pair' && pair ? (
          <MobileSheetPanel title="Pair device" testId="termx-pair-sheet" onClose={() => setMobileSheet(null)}>
            <LocalPairPanel
              api={pair.api}
              storage={pair.storage}
              crypto={pair.crypto}
              appName={pair.appName}
              onPaired={handlePaired}
            />
          </MobileSheetPanel>
        ) : null}
      </main>
    )
  }

  useEffect(() => {
    if (typeof window === 'undefined' || !window.visualViewport) {
      terminalRef.current?.adjustInputPosition(0)
      return
    }

    const viewport = window.visualViewport
    let frame = 0
    const syncKeyboardOffset = () => {
      if (frame) return
      frame = window.requestAnimationFrame(() => {
        frame = 0
        const currentViewport = window.visualViewport
        const bottomOffset = currentViewport
          ? Math.max(0, Math.round(window.innerHeight - currentViewport.height - currentViewport.offsetTop))
          : 0
        terminalRef.current?.adjustInputPosition(panelMode === 'terminal' && bottomOffset > 80 ? bottomOffset : 0)
      })
    }

    viewport.addEventListener('resize', syncKeyboardOffset)
    viewport.addEventListener('scroll', syncKeyboardOffset)
    syncKeyboardOffset()

    return () => {
      if (frame) window.cancelAnimationFrame(frame)
      viewport.removeEventListener('resize', syncKeyboardOffset)
      viewport.removeEventListener('scroll', syncKeyboardOffset)
      terminalRef.current?.adjustInputPosition(0)
    }
  }, [activeTerminalId, connectedTransport, panelMode])

  if (error && !machine) {
    return (
      <div className={`flex h-[100dvh] items-center justify-center bg-zinc-50 p-4 ${className || ''}`}>
        <div className="w-full max-w-md rounded-lg border border-red-200 bg-white p-4 text-sm text-red-700 shadow-sm" role="alert">
          <h2 className="mb-2 font-semibold text-red-900">Connection Error</h2>
          <p>{error}</p>
        </div>
      </div>
    )
  }

  if (!machine) {
    return (
      <div className={`flex h-[100dvh] items-center justify-center bg-zinc-50 ${className || ''}`}>
        <div className="flex items-center gap-2 text-sm text-zinc-500">
          <div className="h-4 w-4 animate-spin rounded-full border-2 border-zinc-300 border-t-zinc-600"></div>
          Connecting to TermX...
        </div>
      </div>
    )
  }

  return (
    <div className={`flex h-[100dvh] w-full flex-col overflow-hidden bg-zinc-50 font-sans text-zinc-900 md:flex-row ${className || ''}`} data-machine-id={machine.machineId}>
      {page === 'terminal' ? (
      <aside className="hidden w-72 shrink-0 flex-col border-r border-zinc-200 bg-zinc-100 md:flex">
        <div className="flex h-12 shrink-0 items-center border-b border-zinc-200 px-4">
          <Monitor className="mr-2 h-4 w-4 text-zinc-500" />
          <h1 className="text-sm font-semibold text-zinc-800 truncate">{machine.machineId}</h1>
        </div>
        <div className="shrink-0 border-b border-zinc-200 bg-zinc-50 p-3">
          <button
            type="button"
            aria-label="Show terminal list"
            className="flex min-h-10 w-full items-center justify-center gap-2 rounded-md border border-zinc-200 bg-white px-3 text-sm font-medium text-zinc-800 shadow-sm active:bg-zinc-100"
            onClick={showTerminalListPage}
          >
            <ChevronLeft className="h-4 w-4" />
            Terminal list
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-3">
          <h2 className="mb-2 px-1 text-xs font-semibold uppercase tracking-wider text-zinc-500">Terminals</h2>
          <TerminalList
            machineId={machine.machineId}
            terminals={terminals}
            onOpenTerminal={openTerminal}
            activeTerminalId={activeTerminalId ?? undefined}
          />
        </div>
        {pair ? (
          <div className="shrink-0 border-t border-zinc-200 bg-zinc-50 p-3">
            <button
              type="button"
              aria-label="Open pairing"
              className="flex min-h-10 w-full items-center justify-center gap-2 rounded-md border border-zinc-200 bg-white px-3 text-sm font-medium text-zinc-800 shadow-sm active:bg-zinc-100"
              onClick={() => setMobileSheet('pair')}
            >
              <KeyRound className="h-4 w-4" />
              Pair
            </button>
          </div>
        ) : null}
      </aside>
      ) : null}

      {page === 'terminal-list' ? renderTerminalListPage() : (
      <main className="relative flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-11 shrink-0 items-center justify-between gap-2 border-b border-zinc-200 bg-white px-2 md:hidden">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <button
              type="button"
              aria-label="Back to terminal list"
              className="-ml-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-zinc-600 active:bg-zinc-100"
              onClick={showTerminalListPage}
            >
              <ChevronLeft className="h-5 w-5" />
            </button>
            <button
              type="button"
              aria-label="Switch terminal"
              className="flex min-w-0 flex-1 flex-col items-start rounded-md px-1 py-1 text-left active:bg-zinc-100"
              onClick={() => setMobileSheet('terminals')}
            >
              <span className="max-w-full truncate text-[11px] font-medium text-zinc-500">{machine.machineId}</span>
              <span className="max-w-full truncate font-mono text-sm font-semibold leading-tight text-zinc-900" data-testid="termx-terminal-title">{activeTerminalTitle}</span>
            </button>
          </div>

          <div className="flex shrink-0 items-center gap-1">
            <button
              type="button"
              aria-label="Open files"
              onClick={openFiles}
              className={`flex h-9 w-9 items-center justify-center rounded-md active:bg-zinc-100 ${panelMode === 'files' ? 'bg-zinc-100 text-zinc-900' : 'text-zinc-600'}`}
            >
              <Folder className="h-5 w-5" />
            </button>
            {pair ? (
              <button
                type="button"
                aria-label="Pair device"
                className="flex h-9 w-9 items-center justify-center rounded-md text-zinc-600 active:bg-zinc-100"
                onClick={() => setMobileSheet('pair')}
              >
                <KeyRound className="h-5 w-5" />
              </button>
            ) : null}
          </div>
        </header>

        {error ? (
          <div className="m-3 shrink-0 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 shadow-sm" role="alert">
            {error}
          </div>
        ) : null}
        {pairStatus ? (
          <div className="mx-3 mt-3 shrink-0 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-800 shadow-sm" role="status">
            {pairStatus}
          </div>
        ) : null}

        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-white md:bg-zinc-50">
          <div
            className="relative min-h-0 flex-1 bg-[#0c0c0c] md:m-3 md:rounded-lg md:border md:border-zinc-800 md:shadow-xl md:overflow-hidden"
            data-testid="termx-terminal-panel"
          >
            {activeTerminalId && connectedTransport ? (
              <Terminal
                ref={terminalRef}
                machineId={machine.machineId}
                terminalId={activeTerminalId}
                transport={connectedTransport}
                className="absolute inset-0 outline-none"
                modifierState={modifierState}
                onModifierStateChange={setModifierState}
                onCursorMove={resetKeyboardOffset}
              />
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-zinc-500">
                No active terminal
              </div>
            )}
          </div>

          {panelMode === 'files' ? (
            <div className="absolute inset-0 z-20 flex flex-col bg-white shadow-2xl md:inset-y-auto md:relative md:z-auto md:h-[40%] md:min-h-[250px] md:border-t md:border-zinc-200" data-testid="termx-file-overlay">
              <div className="flex h-11 shrink-0 items-center justify-between border-b border-zinc-200 bg-zinc-50 px-3 md:hidden">
                <span className="text-sm font-semibold text-zinc-900">Files</span>
                <button
                  type="button"
                  aria-label="Close files"
                  className="flex h-8 w-8 items-center justify-center rounded-md text-zinc-500 active:bg-zinc-200"
                  onClick={openTerminalPanel}
                >
                  <X className="h-5 w-5" />
                </button>
              </div>
              {activeTerminalId && connectedTransport ? (
                <FileManager
                  machineId={machine.machineId}
                  terminalId={activeTerminalId}
                  transport={connectedTransport}
                  initialPath="/"
                  className="flex h-full min-h-0 flex-col relative"
                />
              ) : (
                <div className="flex h-full items-center justify-center text-sm text-zinc-500">
                  Connect to a terminal to view files
                </div>
              )}
            </div>
          ) : null}
        </div>

        <MobileTerminalKeybar
          onInput={(data) => terminalRef.current?.sendInput(data)}
          onFocusKeyboard={() => terminalRef.current?.focus()}
          onBlurKeyboard={() => terminalRef.current?.blur()}
          modifierState={modifierState}
          onModifierStateChange={setModifierState}
          className={panelMode === 'terminal' ? '' : 'hidden'}
        />

        {mobileSheet === 'terminals' ? (
          <MobileSheetPanel title="Terminals" testId="termx-terminal-switcher-sheet" onClose={() => setMobileSheet(null)}>
            <TerminalList
              machineId={machine.machineId}
              terminals={terminals}
              onOpenTerminal={openTerminal}
              activeTerminalId={activeTerminalId ?? undefined}
            />
          </MobileSheetPanel>
        ) : null}

        {mobileSheet === 'pair' && pair ? (
          <MobileSheetPanel title="Pair device" testId="termx-pair-sheet" onClose={() => setMobileSheet(null)}>
            <LocalPairPanel
              api={pair.api}
              storage={pair.storage}
              crypto={pair.crypto}
              appName={pair.appName}
              onPaired={handlePaired}
            />
          </MobileSheetPanel>
        ) : null}
      </main>
      )}
    </div>
  )
}

function MobileSheetPanel({
  children,
  onClose,
  testId,
  title,
}: {
  children: ReactNode
  onClose: () => void
  testId: string
  title: string
}) {
  return (
    <div className="absolute inset-0 z-40 flex items-end bg-black/30 md:items-center md:justify-center" data-testid={testId}>
      <section className="max-h-[78vh] w-full overflow-hidden rounded-t-lg bg-zinc-50 shadow-2xl md:max-w-md md:rounded-lg">
        <header className="flex h-12 items-center justify-between border-b border-zinc-200 px-4">
          <h2 className="text-sm font-semibold text-zinc-900">{title}</h2>
          <button
            type="button"
            aria-label={`Close ${title}`}
            className="flex h-9 w-9 items-center justify-center rounded-md text-zinc-500 active:bg-zinc-200"
            onClick={onClose}
          >
            <X className="h-5 w-5" />
          </button>
        </header>
        <div className="max-h-[calc(78vh-3rem)] overflow-y-auto p-3">
          {children}
        </div>
      </section>
    </div>
  )
}
