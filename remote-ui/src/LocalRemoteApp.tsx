import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronLeft, Folder, KeyRound, Monitor, Plus, RefreshCw, SquarePen, Trash2, X } from 'lucide-react'
import { FileManager } from './FileManager'
import { LocalPairPanel } from './LocalPairPanel'
import type { LocalAppCrypto, LocalAppIdentityStore } from './localAppIdentity'
import { MobileTerminalKeybar } from './MobileTerminalKeybar'
import type { TerminalModifierState } from './mobileTerminalInput'
import { Terminal, type TerminalHandle } from './Terminal'
import { TerminalList } from './TerminalList'
import type { Machine, Terminal as RemoteTerminal } from './model'
import type { LocalAgentApi, LocalCreateTerminalInput, LocalUpdateTerminalInput, PeerTransport, TerminalInventoryEvents } from './transport'
import type { TerminalTransport } from './terminalClient'

export interface LocalRemoteTransportInput {
  machineId: string
  terminalId: string
}

export type LocalRemoteTransportFactory = (input: LocalRemoteTransportInput) => PeerTransport & TerminalTransport

export interface LocalRemoteAppProps {
  api: Pick<LocalAgentApi, 'getStatus' | 'listTerminals'> & Partial<Pick<LocalAgentApi, 'createTerminal' | 'updateTerminal' | 'deleteTerminal'>>
  createTransport: LocalRemoteTransportFactory
  className?: string | undefined
  inventoryEvents?: TerminalInventoryEvents | undefined
  pair?: {
    api: Pick<LocalAgentApi, 'pair'>
    storage: LocalAppIdentityStore
    crypto: LocalAppCrypto
    appName: string
  } | undefined
}

type MobileSheet = 'terminals' | 'pair' | 'manage-terminal' | 'edit-terminal' | 'create-terminal' | null
type AppPage = 'terminal-list' | 'terminal'

export function LocalRemoteApp({ api, createTransport, className, inventoryEvents, pair }: LocalRemoteAppProps) {
  const [machine, setMachine] = useState<Machine | null>(null)
  const [terminals, setTerminals] = useState<RemoteTerminal[]>([])
  const [activeTerminalId, setActiveTerminalId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pairStatus, setPairStatus] = useState<string | null>(null)
  const [refreshingTerminals, setRefreshingTerminals] = useState(false)
  const [verifiedDevice, setVerifiedDevice] = useState(() => pair ? Boolean(pair.storage.loadCertificate()) : true)
  const [connectedTransport, setConnectedTransport] = useState<(PeerTransport & TerminalTransport) | null>(null)
  const [fileTransport, setFileTransport] = useState<(PeerTransport & TerminalTransport) | null>(null)
  const [fileTerminalId, setFileTerminalId] = useState<string | null>(null)
  const [fileTransportReady, setFileTransportReady] = useState(false)
  const [fileInitialPath, setFileInitialPath] = useState('/')
  const [transportRetryToken, setTransportRetryToken] = useState(0)

  const [page, setPage] = useState<AppPage>('terminal-list')
  const [filesOpen, setFilesOpen] = useState(false)
  const [mobileSheet, setMobileSheet] = useState<MobileSheet>(null)
  const [managedTerminalId, setManagedTerminalId] = useState<string | null>(null)
  const [terminalForm, setTerminalForm] = useState<{
    name: string
    command: string
    cwd: string
    environment: string
    sizeLockMode: 'off' | 'warn' | 'lock'
  }>({
    name: '',
    command: '/bin/zsh -l',
    cwd: '',
    environment: '',
    sizeLockMode: 'off',
  })
  const [modifierState, setModifierState] = useState<TerminalModifierState>({ ctrl: 'off', alt: 'off' })
  const terminalRef = useRef<TerminalHandle | null>(null)
  const fileTransportConnectSeqRef = useRef(0)
  const listPullStartRef = useRef<number | null>(null)
  const listPullDistanceRef = useRef(0)
  const listContainerRef = useRef<HTMLDivElement | null>(null)
  const [listPullDistance, setListPullDistance] = useState(0)
  const activeTerminal = terminals.find((terminal) => terminal.terminalId === activeTerminalId)
  const managedTerminal = terminals.find((terminal) => terminal.terminalId === managedTerminalId)
  const activeTerminalTitle = activeTerminal?.title || activeTerminal?.command || activeTerminalId || 'Terminal'
  const requireVerification = Boolean(pair && !verifiedDevice)
  const canManageTerminals = Boolean(api.createTerminal && api.updateTerminal && api.deleteTerminal)

  const refreshTerminals = useCallback(async () => {
    setRefreshingTerminals(true)
    try {
      const status = await api.getStatus()
      const terminalList = await api.listTerminals()
      setMachine(status.machine)
      setTerminals(terminalList)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setRefreshingTerminals(false)
    }
  }, [api])

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
    if (!inventoryEvents || !machine) return
    const subscription = inventoryEvents.subscribe(machine.machineId, () => {
      void refreshTerminals()
    })
    return () => {
      subscription.close()
    }
  }, [inventoryEvents, machine, refreshTerminals, transportRetryToken])

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
    if (requireVerification) {
      setMobileSheet('pair')
      return
    }
    if (machine && intent.machineId !== machine.machineId) {
      setError(`terminal machine mismatch: ${intent.machineId} != ${machine.machineId}`)
      return
    }
    setActiveTerminalId(intent.terminalId)
    setPage('terminal')
    setMobileSheet(null)
  }, [machine, requireVerification])

  const handlePaired = useCallback((machineId: string) => {
    setError(null)
    setVerifiedDevice(true)
    setPairStatus(`Paired with ${machineId}`)
    setTransportRetryToken((current) => current + 1)
    setMobileSheet(null)
  }, [])

  useEffect(() => {
    if (!pair) return
    if (pair.storage.loadCertificate()) {
      setVerifiedDevice(true)
    }
  }, [pair, transportRetryToken])

  const showTerminalListPage = useCallback(() => {
    setPage('terminal-list')
    setMobileSheet(null)
    terminalRef.current?.adjustInputPosition(0)
  }, [])

  const openFiles = useCallback(() => {
    if (requireVerification) {
      setMobileSheet('pair')
      return
    }
    const fileTerminal = activeTerminal ?? terminals[0]
    if (!machine || !fileTerminal) {
      setError('No terminal is available for local file access')
      return
    }
    setFileInitialPath(fileTerminal.cwd || '/')
    if (!fileTransport || fileTerminalId !== fileTerminal.terminalId) {
      void fileTransport?.disconnect()
      try {
        const connectSeq = fileTransportConnectSeqRef.current + 1
        fileTransportConnectSeqRef.current = connectSeq
        const transport = createTransport({
          machineId: machine.machineId,
          terminalId: fileTerminal.terminalId,
        })
        setFileTransport(transport)
        setFileTerminalId(fileTerminal.terminalId)
        setFileTransportReady(false)
        void transport.connect({
          machineId: machine.machineId,
          terminalId: fileTerminal.terminalId,
          mode: 'local',
        }).then(() => {
          if (fileTransportConnectSeqRef.current !== connectSeq) return
          setFileTransportReady(true)
        }).catch((err: unknown) => {
          if (fileTransportConnectSeqRef.current !== connectSeq) return
          setError(err instanceof Error ? err.message : String(err))
          setFilesOpen(false)
          setFileTransport(null)
          setFileTerminalId(null)
          setFileTransportReady(false)
          if (pair) setMobileSheet('pair')
        })
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
        setFileTransportReady(false)
        if (pair) setMobileSheet('pair')
        return
      }
    }
    setFilesOpen(true)
    setMobileSheet(null)
  }, [activeTerminal, createTransport, fileTerminalId, fileTransport, machine, pair, requireVerification, terminals])

  const openManageTerminal = useCallback((intent: { machineId: string; terminalId: string }) => {
    if (requireVerification) {
      setMobileSheet('pair')
      return
    }
    if (!canManageTerminals) return
    if (machine && intent.machineId !== machine.machineId) {
      setError(`terminal machine mismatch: ${intent.machineId} != ${machine.machineId}`)
      return
    }
    setManagedTerminalId(intent.terminalId)
    setMobileSheet('manage-terminal')
  }, [canManageTerminals, machine, requireVerification])

  const openCreateTerminal = useCallback(() => {
    if (requireVerification) {
      setMobileSheet('pair')
      return
    }
    if (!canManageTerminals) return
    setManagedTerminalId(null)
    setTerminalForm({
      name: '',
      command: '/bin/zsh -l',
      cwd: '',
      environment: '',
      sizeLockMode: 'off',
    })
    setMobileSheet('create-terminal')
  }, [canManageTerminals, requireVerification])

  const openEditTerminal = useCallback(() => {
    if (!managedTerminal) return
    setTerminalForm({
      name: managedTerminal.title,
      command: managedTerminal.command ?? '/bin/zsh -l',
      cwd: managedTerminal.cwd ?? '',
      environment: managedTerminal.environment ?? '',
      sizeLockMode: managedTerminal.sizeLockMode ?? 'off',
    })
    setMobileSheet('edit-terminal')
  }, [managedTerminal])

  const submitCreateTerminal = useCallback(async () => {
    if (!api.createTerminal) return
    const input: LocalCreateTerminalInput = {
      name: terminalForm.name.trim() || undefined,
      command: terminalForm.command.trim().split(/\s+/).filter(Boolean),
      cwd: terminalForm.cwd.trim() || undefined,
      environment: terminalForm.environment.trim() || undefined,
      sizeLockMode: terminalForm.sizeLockMode,
    }
    const created = await api.createTerminal(input)
    await refreshTerminals()
    setPairStatus(`Created ${created.title}`)
    setMobileSheet(null)
  }, [api, refreshTerminals, terminalForm])

  const submitUpdateTerminal = useCallback(async () => {
    if (!api.updateTerminal || !managedTerminalId) return
    const input: LocalUpdateTerminalInput = {
      terminalId: managedTerminalId,
      name: terminalForm.name.trim() || undefined,
      cwd: terminalForm.cwd.trim() || undefined,
      environment: terminalForm.environment.trim() || undefined,
      sizeLockMode: terminalForm.sizeLockMode,
    }
    const updated = await api.updateTerminal(input)
    await refreshTerminals()
    setPairStatus(`Updated ${updated.title}`)
    setMobileSheet(null)
  }, [api, managedTerminalId, refreshTerminals, terminalForm])

  const deleteManagedTerminal = useCallback(async () => {
    if (!api.deleteTerminal || !managedTerminalId) return
    const deletedTerminalId = managedTerminalId
    const deletedTitle = managedTerminal?.title ?? managedTerminalId
    await api.deleteTerminal(deletedTerminalId)
    if (activeTerminalId === deletedTerminalId) {
      setActiveTerminalId(null)
      setPage('terminal-list')
    }
    if (fileTerminalId === deletedTerminalId) {
      setFilesOpen(false)
      setFileTerminalId(null)
      setFileTransport(null)
      setFileTransportReady(false)
    }
    await refreshTerminals()
    setPairStatus(`Deleted ${deletedTitle}`)
    setMobileSheet(null)
  }, [activeTerminalId, api, fileTerminalId, managedTerminal, managedTerminalId, refreshTerminals])

  const openTerminalPanel = useCallback(() => {
    setFilesOpen(false)
    window.setTimeout(() => {
      terminalRef.current?.fit()
      terminalRef.current?.focus()
    }, 0)
  }, [])

  const resetKeyboardOffset = useCallback(() => {
    terminalRef.current?.adjustInputPosition(0)
  }, [])

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
          <div className="flex shrink-0 items-center gap-1">
            <button
              type="button"
              aria-label="Refresh terminals"
              className="flex h-9 w-9 items-center justify-center rounded-md text-zinc-600 active:bg-zinc-100"
              onClick={() => { void refreshTerminals() }}
            >
              <RefreshCw className={`h-5 w-5 ${refreshingTerminals ? 'animate-spin' : ''}`} />
            </button>
            <button
              type="button"
              aria-label="Open files"
              className="flex h-9 w-9 items-center justify-center rounded-md text-zinc-600 active:bg-zinc-100"
              onClick={openFiles}
            >
              <Folder className="h-5 w-5" />
            </button>
            {canManageTerminals ? (
              <button
                type="button"
                aria-label="Create terminal"
                className="flex h-9 w-9 items-center justify-center rounded-md text-zinc-600 active:bg-zinc-100"
                onClick={openCreateTerminal}
              >
                <Plus className="h-5 w-5" />
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
        {requireVerification ? (
          <section className="mx-3 mt-3 rounded-2xl border border-zinc-200 bg-white p-5 shadow-sm ring-1 ring-zinc-200/60" data-testid="termx-verification-gate">
            <div className="mb-4 flex items-center gap-3">
              <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-zinc-100 text-zinc-600">
                <KeyRound className="h-5 w-5" />
              </div>
              <div>
                <h2 className="text-[17px] font-bold tracking-tight text-zinc-900">Verify This Device</h2>
                <p className="text-[13px] font-medium text-zinc-500">Pair first before opening or managing local terminals.</p>
              </div>
            </div>
            <button
              type="button"
              className="flex min-h-12 w-full items-center justify-center gap-2 rounded-xl bg-zinc-900 px-4 text-[15px] font-semibold text-white shadow-md transition-all active:scale-[0.98] active:bg-zinc-800"
              onClick={() => setMobileSheet('pair')}
            >
              <KeyRound className="h-4 w-4" />
              Verify device
            </button>
          </section>
        ) : null}
        <div
          ref={listContainerRef}
          className="min-h-0 flex-1 overflow-y-auto p-3"
          data-testid="termx-terminal-list-scroll"
          onTouchStart={(event) => {
            if ((listContainerRef.current?.scrollTop ?? 0) > 0) return
            listPullStartRef.current = event.touches[0]?.clientY ?? null
          }}
          onTouchMove={(event) => {
            if (listPullStartRef.current === null) return
            const currentY = event.touches[0]?.clientY ?? listPullStartRef.current
            const nextDistance = Math.max(0, Math.min(96, currentY - listPullStartRef.current))
            listPullDistanceRef.current = nextDistance
            setListPullDistance(nextDistance)
          }}
          onTouchEnd={() => {
            const shouldRefresh = listPullDistanceRef.current >= 64
            listPullStartRef.current = null
            listPullDistanceRef.current = 0
            setListPullDistance(0)
            if (shouldRefresh) {
              void refreshTerminals()
            }
          }}
        >
          <div
            className="flex items-center justify-center overflow-hidden text-xs font-medium text-zinc-500 transition-all"
            style={{ height: `${Math.min(listPullDistance, 48)}px` }}
          >
            {refreshingTerminals ? 'Refreshing terminals...' : listPullDistance >= 64 ? 'Release to refresh terminals' : 'Pull down to refresh'}
          </div>
          <h2 className="mb-2 px-1 text-xs font-semibold uppercase tracking-wider text-zinc-500">Terminals</h2>
          <TerminalList
            machineId={machine.machineId}
            terminals={terminals}
            onOpenTerminal={openTerminal}
            onManageTerminal={openManageTerminal}
            activeTerminalId={activeTerminalId ?? undefined}
          />
        </div>

        {mobileSheet === 'manage-terminal' && managedTerminal ? (
          <MobileSheetPanel title={managedTerminal.title || 'Terminal'} testId="termx-terminal-actions-sheet" onClose={() => setMobileSheet(null)}>
            <div className="flex flex-col gap-3">
              <button
                type="button"
                className="flex min-h-12 w-full items-center justify-between rounded-xl border border-zinc-200 bg-white px-4 text-left text-[15px] font-medium text-zinc-900 shadow-sm"
                onClick={openEditTerminal}
              >
                <span>Edit terminal</span>
                <SquarePen className="h-4 w-4 text-zinc-500" />
              </button>
              <button
                type="button"
                className="flex min-h-12 w-full items-center justify-between rounded-xl border border-red-200 bg-red-50 px-4 text-left text-[15px] font-medium text-red-700 shadow-sm"
                onClick={() => { void deleteManagedTerminal() }}
              >
                <span>Delete terminal</span>
                <Trash2 className="h-4 w-4 text-red-500" />
              </button>
            </div>
          </MobileSheetPanel>
        ) : null}

        {(mobileSheet === 'create-terminal' || mobileSheet === 'edit-terminal') ? (
          <MobileSheetPanel
            title={mobileSheet === 'create-terminal' ? 'New terminal' : 'Edit terminal'}
            testId="termx-terminal-editor-sheet"
            onClose={() => setMobileSheet(null)}
          >
            <div className="flex flex-col gap-4">
              <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                Name
                <input
                  className="min-h-12 rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                  value={terminalForm.name}
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setTerminalForm((current) => ({ ...current, name: value }))
                  }}
                />
              </label>
              {mobileSheet === 'create-terminal' ? (
                <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                  Command
                  <input
                    className="min-h-12 rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                    value={terminalForm.command}
                    onChange={(event) => {
                      const value = event.currentTarget.value
                      setTerminalForm((current) => ({ ...current, command: value }))
                    }}
                  />
                </label>
              ) : null}
              <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                Working directory
                <input
                  className="min-h-12 rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                  value={terminalForm.cwd}
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setTerminalForm((current) => ({ ...current, cwd: value }))
                  }}
                />
              </label>
              <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                Environment
                <input
                  className="min-h-12 rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                  value={terminalForm.environment}
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setTerminalForm((current) => ({ ...current, environment: value }))
                  }}
                />
              </label>
              <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                Size lock
                <select
                  className="min-h-12 rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                  value={terminalForm.sizeLockMode}
                  onChange={(event) => {
                    const value = event.currentTarget.value as 'off' | 'warn' | 'lock'
                    setTerminalForm((current) => ({ ...current, sizeLockMode: value }))
                  }}
                >
                  <option value="off">Resizable</option>
                  <option value="warn">Warn only</option>
                  <option value="lock">Locked</option>
                </select>
              </label>
              <button
                type="button"
                className="mt-2 flex min-h-12 w-full items-center justify-center gap-2 rounded-xl bg-zinc-900 px-4 text-[15px] font-semibold text-white shadow-md transition-all active:scale-[0.98] active:bg-zinc-800"
                onClick={() => {
                  if (mobileSheet === 'create-terminal') {
                    void submitCreateTerminal()
                    return
                  }
                  void submitUpdateTerminal()
                }}
              >
                {mobileSheet === 'create-terminal' ? 'Create terminal' : 'Save changes'}
              </button>
            </div>
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
        terminalRef.current?.adjustInputPosition(bottomOffset > 80 ? bottomOffset : 0)
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
  }, [activeTerminalId, connectedTransport])

  useEffect(() => () => {
    void fileTransport?.disconnect()
  }, [fileTransport])

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
    <div className={`relative flex h-[100dvh] w-full flex-col overflow-hidden bg-zinc-50 font-sans text-zinc-900 md:flex-row ${className || ''}`} data-machine-id={machine.machineId}>
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
            onManageTerminal={openManageTerminal}
            activeTerminalId={activeTerminalId ?? undefined}
          />
        </div>
      </aside>
      ) : null}

      {page === 'terminal-list' ? renderTerminalListPage() : (
      <main className="relative flex min-w-0 flex-1 flex-col overflow-hidden bg-black">
        <header className="relative z-10 flex h-14 shrink-0 items-center justify-between gap-2 border-b border-zinc-800/80 bg-zinc-900/80 px-2 backdrop-blur-xl md:hidden">
          <div className="flex min-w-0 flex-1 items-center gap-1.5">
            <button
              type="button"
              aria-label="Back to terminal list"
              className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-zinc-400 transition-colors active:bg-zinc-800"
              onClick={showTerminalListPage}
            >
              <ChevronLeft className="h-6 w-6" />
            </button>
            <button
              type="button"
              aria-label="Switch terminal"
              className="flex min-w-0 flex-1 flex-col items-start justify-center rounded-xl px-2 py-1 text-left transition-colors active:bg-zinc-800"
              onClick={() => setMobileSheet('terminals')}
            >
              <span className="max-w-full truncate text-[11px] font-bold uppercase tracking-wider text-zinc-500">{machine.machineId}</span>
              <span className="max-w-full truncate text-[15px] font-semibold leading-tight text-zinc-100" data-testid="termx-terminal-title">{activeTerminalTitle}</span>
            </button>
          </div>

          <div className="flex shrink-0 items-center gap-1">
            <button
              type="button"
              aria-label="Open files"
              onClick={openFiles}
              className={`flex h-10 w-10 items-center justify-center rounded-full transition-colors active:scale-95 ${filesOpen ? 'bg-zinc-100 text-zinc-900' : 'text-zinc-400 active:bg-zinc-800'}`}
            >
              <Folder className="h-5 w-5" />
            </button>
          </div>
        </header>

        {error ? (
          <div className="m-3 shrink-0 rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3 text-[14px] font-medium text-red-200 shadow-sm" role="alert">
            {error}
          </div>
        ) : null}
        {pairStatus ? (
          <div className="mx-3 mt-3 shrink-0 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-[14px] font-medium text-emerald-200 shadow-sm" role="status">
            {pairStatus}
          </div>
        ) : null}

        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-black md:bg-zinc-950">
          <div
            className="relative min-h-0 flex-1 bg-black md:m-3 md:rounded-2xl md:border md:border-zinc-800 md:shadow-2xl md:overflow-hidden"
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

        </div>

        <MobileTerminalKeybar
          onInput={(data) => terminalRef.current?.sendInput(data)}
          onFocusKeyboard={() => terminalRef.current?.focus()}
          onBlurKeyboard={() => terminalRef.current?.blur()}
          modifierState={modifierState}
          onModifierStateChange={setModifierState}
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

      {fileTerminalId && fileTransport ? (
        <div
          className={`absolute inset-0 z-30 flex flex-col bg-zinc-50 shadow-[0_-20px_40px_rgba(0,0,0,0.15)] transition-transform duration-300 md:m-6 md:rounded-2xl md:border md:border-zinc-200/60 ${filesOpen ? 'translate-y-0' : 'translate-y-full'}`}
          data-testid="termx-machine-files-overlay"
          style={{ visibility: filesOpen ? 'visible' : 'hidden' }}
        >
          <div className="flex h-16 shrink-0 items-center justify-between border-b border-zinc-200/50 bg-white/80 px-4 backdrop-blur-xl md:h-14">
            <div className="flex items-center gap-2">
              <Folder className="h-5 w-5 text-zinc-500" />
              <span className="text-[17px] font-bold tracking-tight text-zinc-900">Files</span>
            </div>
            <button
              type="button"
              aria-label="Close files"
              className="flex h-9 w-9 items-center justify-center rounded-full bg-zinc-100 text-zinc-500 transition-colors active:scale-95 active:bg-zinc-200"
              onClick={openTerminalPanel}
            >
              <X className="h-5 w-5" />
            </button>
          </div>
          {fileTransportReady ? (
            <FileManager
              key={fileTerminalId}
              machineId={machine.machineId}
              terminalId={fileTerminalId}
              transport={fileTransport}
              initialPath={fileInitialPath}
              className="flex h-full min-h-0 flex-col relative"
            />
          ) : (
            <div className="flex h-full items-center justify-center text-sm text-zinc-500">
              Local file access is not ready
            </div>
          )}
        </div>
      ) : null}
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
    <div className="absolute inset-0 z-40 flex items-end bg-black/40 backdrop-blur-sm transition-opacity md:items-center md:justify-center" data-testid={testId} onClick={onClose}>
      <section
        className="relative max-h-[85vh] w-full overflow-hidden rounded-t-[2rem] bg-zinc-50/95 backdrop-blur-xl shadow-[0_-8px_30px_rgba(0,0,0,0.12)] md:max-w-md md:rounded-2xl"
        onClick={(e) => e.stopPropagation()}
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        <div className="absolute left-1/2 top-3 h-1.5 w-12 -translate-x-1/2 rounded-full bg-zinc-300/80" />
        <header className="flex h-16 items-center justify-between border-b border-zinc-200/50 px-5 pt-3">
          <h2 className="text-[17px] font-bold tracking-tight text-zinc-900">{title}</h2>
          <button
            type="button"
            aria-label={`Close ${title}`}
            className="flex h-8 w-8 items-center justify-center rounded-full bg-zinc-200/50 text-zinc-500 transition-colors active:scale-95 active:bg-zinc-300"
            onClick={onClose}
          >
            <X className="h-5 w-5" />
          </button>
        </header>
        <div className="max-h-[calc(85vh-4rem)] overflow-y-auto p-4">
          {children}
        </div>
      </section>
    </div>
  )
}
