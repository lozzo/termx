import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronLeft, Folder, KeyRound, Monitor, Plus, RefreshCw, SquarePen, Trash2, X } from 'lucide-react'
import { FileManager } from './FileManager'
import { evaluateConnectionCapability } from './connectionPolicy'
import { LocalPairPanel } from './LocalPairPanel'
import type { MachineSessionStore } from './localAppIdentity'
import { MobileTerminalKeybar } from './MobileTerminalKeybar'
import type { TerminalModifierState } from './mobileTerminalInput'
import { Terminal, type TerminalHandle } from './Terminal'
import { TerminalList } from './TerminalList'
import { createTerminalManagementApi } from './terminalManagementApi'
import type { Machine, Terminal as RemoteTerminal } from './model'
import type { ConnectionCapabilities, LocalAgentApi, LocalCreateTerminalInput, LocalPairingApi, LocalUpdateTerminalInput, RtcConnector, RtcSession, RtcSessionLiveness, TerminalInventoryEvents } from './transport'

export interface LocalRemoteInventoryApi extends Pick<LocalAgentApi, 'getStatus'> {
  listTerminals(options?: { onStatus?: (status: string) => void }): Promise<RemoteTerminal[]>
}

export interface LocalRemoteSessionInput {
  machineId: string
}

export type LocalRemoteSessionConnector = RtcConnector<LocalRemoteSessionInput>

export interface LocalRemoteAppProps {
  api: LocalRemoteInventoryApi
  connector: LocalRemoteSessionConnector
  className?: string | undefined
  inventoryEvents?: TerminalInventoryEvents | undefined
  managementPolicy?: Partial<Pick<ConnectionCapabilities, 'apiAllowed' | 'terminalManagementAllowed' | 'denialReason'>> | undefined
  pair?: {
    api: LocalPairingApi
    sessionStore: MachineSessionStore
    appName: string
  } | undefined
  onBack?: (() => void) | undefined
}

type MobileSheet = 'terminals' | 'pair' | 'manage-terminal' | 'edit-terminal' | 'create-terminal' | null
type AppPage = 'terminal-list' | 'terminal'

export function LocalRemoteApp({ api, connector, className, inventoryEvents, managementPolicy, pair, onBack }: LocalRemoteAppProps) {
  const [machine, setMachine] = useState<Machine | null>(null)
  const [terminals, setTerminals] = useState<RemoteTerminal[]>([])
  const [loadingTerminals, setLoadingTerminals] = useState(true)
  const [connectionStatus, setConnectionStatus] = useState<string | null>(null)
  const [activeTerminalId, setActiveTerminalId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pairStatus, setPairStatus] = useState<string | null>(null)
  const [refreshingTerminals, setRefreshingTerminals] = useState(false)
  const [verifiedDevice, setVerifiedDevice] = useState(() => pair && machine ? Boolean(pair.sessionStore.getSessionToken(machine.machineId)) : !pair)
  const [connectedSession, setConnectedSession] = useState<RtcSession | null>(null)
  const [connectedTerminalId, setConnectedTerminalId] = useState<string | null>(null)
  const [connectingTerminalId, setConnectingTerminalId] = useState<string | null>(null)
  const [fileTerminalId, setFileTerminalId] = useState<string | null>(null)
  const [fileInitialPath, setFileInitialPath] = useState('/')
  const [connectionRetryToken, setConnectionRetryToken] = useState(0)

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
  const machineSessionRef = useRef<{
    connector: LocalRemoteSessionConnector
    machineId: string
    retryToken: number
    session: RtcSession
  } | null>(null)
  const machineSessionPromiseRef = useRef<{
    connector: LocalRemoteSessionConnector
    machineId: string
    retryToken: number
    promise: Promise<RtcSession>
  } | null>(null)
  const machineSessionConnectSeqRef = useRef(0)
  const listPullStartRef = useRef<number | null>(null)
  const listPullDistanceRef = useRef(0)
  const listContainerRef = useRef<HTMLDivElement | null>(null)
  const [listPullDistance, setListPullDistance] = useState(0)
  const activeTerminal = terminals.find((terminal) => terminal.terminalId === activeTerminalId)
  const managedTerminal = terminals.find((terminal) => terminal.terminalId === managedTerminalId)
  const activeTerminalTitle = activeTerminal?.title || activeTerminal?.command || activeTerminalId || 'Terminal'
  const requireVerification = Boolean(pair && !verifiedDevice)
  const canManageTerminals = evaluateConnectionCapability({
    path: 'local',
    connectionId: 'local-management-policy',
    machineId: machine?.machineId ?? 'pending',
    relayInUse: false,
  }, {
    terminalAllowed: true,
    apiAllowed: managementPolicy?.apiAllowed ?? true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    terminalManagementAllowed: managementPolicy?.terminalManagementAllowed ?? true,
    relayInUse: false,
    ...(managementPolicy?.denialReason ? { denialReason: managementPolicy.denialReason } : {}),
  }, 'terminal_management').allowed

  const disconnectMachineSession = useCallback(() => {
    machineSessionConnectSeqRef.current += 1
    const current = machineSessionRef.current
    machineSessionPromiseRef.current = null
    machineSessionRef.current = null
    void current?.session.disconnect()
  }, [])

  const releaseMachineSession = useCallback(() => {
    disconnectMachineSession()
    setConnectedSession(null)
    setConnectedTerminalId(null)
    setConnectingTerminalId(null)
  }, [disconnectMachineSession])

  const ensureMachineSession = useCallback(async (machineId: string): Promise<RtcSession> => {
    const reusable = machineSessionRef.current
    if (
      reusable &&
      reusable.connector === connector &&
      reusable.machineId === machineId &&
      reusable.retryToken === connectionRetryToken
    ) {
      if (isRtcSessionAlive(reusable.session)) return reusable.session
      releaseMachineSession()
    }
    const pending = machineSessionPromiseRef.current
    if (
      pending &&
      pending.connector === connector &&
      pending.machineId === machineId &&
      pending.retryToken === connectionRetryToken
    ) {
      return pending.promise
    }
    const entry: {
      connector: LocalRemoteSessionConnector
      machineId: string
      retryToken: number
      promise: Promise<RtcSession>
    } = {
      connector,
      machineId,
      retryToken: connectionRetryToken,
      promise: Promise.resolve(null as unknown as RtcSession),
    }
    entry.promise = connector.connect({ machineId }).then((session) => {
      if (machineSessionPromiseRef.current !== entry) {
        void session.disconnect()
        return session
      }
      machineSessionPromiseRef.current = null
      machineSessionRef.current = {
        connector,
        machineId,
        retryToken: connectionRetryToken,
        session,
      }
      setConnectedSession(session)
      return session
    }).catch((err: unknown) => {
      if (machineSessionPromiseRef.current === entry) {
        machineSessionPromiseRef.current = null
      }
      throw err
    })
    machineSessionPromiseRef.current = entry
    return entry.promise
  }, [connector, connectionRetryToken, releaseMachineSession])

  const withManagementApi = useCallback(async () => {
    if (!machine) throw new Error('machine is required before managing terminals')
    const session = await ensureMachineSession(machine.machineId)
    return {
      session,
      api: createTerminalManagementApi(session, machine.machineId),
    }
  }, [ensureMachineSession, machine])

  const refreshTerminals = useCallback(async () => {
    setRefreshingTerminals(true)
    try {
      const status = await api.getStatus()
      setMachine(status.machine)
      const terminalList = await api.listTerminals({
        onStatus: setConnectionStatus,
      })
      setTerminals(terminalList)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setRefreshingTerminals(false)
      setConnectionStatus(null)
      setLoadingTerminals(false)
    }
  }, [api])

  useEffect(() => {
    let cancelled = false
    async function load() {
      setLoadingTerminals(true)
      try {
        const status = await api.getStatus()
        if (cancelled) return
        setMachine(status.machine)
        const terminalList = await api.listTerminals({
          onStatus: (status) => !cancelled && setConnectionStatus(status),
        })
        if (cancelled) return
        setTerminals(terminalList)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!cancelled) {
          setLoadingTerminals(false)
          setConnectionStatus(null)
        }
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
  }, [inventoryEvents, machine, refreshTerminals, connectionRetryToken])

  useEffect(() => {
    const machineId = machine?.machineId
    const current = machineSessionRef.current
    if (!machineId) {
      if (current) releaseMachineSession()
      return
    }
    if (
      current &&
      (current.connector !== connector ||
        current.machineId !== machineId ||
        current.retryToken !== connectionRetryToken)
    ) {
      releaseMachineSession()
    }
    if (!activeTerminalId) {
      setConnectedTerminalId(null)
      setConnectingTerminalId(null)
      return
    }
    const reusable = machineSessionRef.current
    if (
      reusable &&
      reusable.connector === connector &&
      reusable.machineId === machineId &&
      reusable.retryToken === connectionRetryToken
    ) {
      if (isRtcSessionAlive(reusable.session)) {
        setConnectedSession(reusable.session)
        setConnectedTerminalId(activeTerminalId)
        setConnectingTerminalId(null)
        return
      }
      releaseMachineSession()
    }
    if (page !== 'terminal') {
      setConnectingTerminalId(null)
      return
    }

    let cancelled = false
    const connectSeq = machineSessionConnectSeqRef.current + 1
    machineSessionConnectSeqRef.current = connectSeq
    setConnectedSession(null)
    setConnectedTerminalId(null)
    setConnectingTerminalId(activeTerminalId)
    ensureMachineSession(machineId).then((session) => {
      if (cancelled || machineSessionConnectSeqRef.current !== connectSeq) return
      setConnectedSession(session)
      setConnectedTerminalId(activeTerminalId)
      setConnectingTerminalId(null)
    }).catch((err: unknown) => {
      if (!cancelled && machineSessionConnectSeqRef.current === connectSeq) {
        setConnectedSession(null)
        setConnectedTerminalId(null)
        setConnectingTerminalId(null)
        setError(err instanceof Error ? err.message : String(err))
        if (pair) setMobileSheet('pair')
      }
    })
    return () => {
      cancelled = true
    }
  }, [activeTerminalId, connector, connectionRetryToken, ensureMachineSession, machine?.machineId, page, pair, releaseMachineSession])

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
    setConnectionRetryToken((current) => current + 1)
    setMobileSheet(null)
  }, [])

  useEffect(() => {
    if (!pair) return
    if (machine && pair.sessionStore.getSessionToken(machine.machineId)) {
      setVerifiedDevice(true)
    }
  }, [machine, pair, connectionRetryToken])

  useEffect(() => {
    if (!pairStatus) return
    const timer = setTimeout(() => setPairStatus(null), 3000)
    return () => clearTimeout(timer)
  }, [pairStatus])

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
    if (fileTerminalId !== fileTerminal.terminalId) {
      setFileTerminalId(fileTerminal.terminalId)
    }
    void ensureMachineSession(machine.machineId).catch((err: unknown) => {
      setError(err instanceof Error ? err.message : String(err))
      setFilesOpen(false)
      setFileTerminalId(null)
      if (pair) setMobileSheet('pair')
    })
    setFilesOpen(true)
    setMobileSheet(null)
  }, [activeTerminal, ensureMachineSession, fileTerminalId, machine, pair, requireVerification, terminals])

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
    if (!canManageTerminals) return
    const input: LocalCreateTerminalInput = {
      name: terminalForm.name.trim() || undefined,
      command: terminalForm.command.trim().split(/\s+/).filter(Boolean),
      cwd: terminalForm.cwd.trim() || undefined,
      environment: terminalForm.environment.trim() || undefined,
      sizeLockMode: terminalForm.sizeLockMode,
    }
    const management = await withManagementApi()
    const created = await management.api.createTerminal(input)
    await refreshTerminals()
    setPairStatus(`Created ${created.terminalId || input.name || 'terminal'}`)
    setMobileSheet(null)
  }, [canManageTerminals, refreshTerminals, terminalForm, withManagementApi])

  const submitUpdateTerminal = useCallback(async () => {
    if (!canManageTerminals || !managedTerminalId) return
    const input: LocalUpdateTerminalInput = {
      terminalId: managedTerminalId,
      name: terminalForm.name.trim() || undefined,
      cwd: terminalForm.cwd.trim() || undefined,
      environment: terminalForm.environment.trim() || undefined,
      sizeLockMode: terminalForm.sizeLockMode,
    }
    const management = await withManagementApi()
    await management.api.updateTerminal(input)
    await refreshTerminals()
    setPairStatus(`Updated ${input.name || managedTerminal?.title || managedTerminalId}`)
    setMobileSheet(null)
  }, [canManageTerminals, managedTerminal, managedTerminalId, refreshTerminals, terminalForm, withManagementApi])

  const deleteManagedTerminal = useCallback(async () => {
    if (!canManageTerminals || !managedTerminalId) return
    const deletedTerminalId = managedTerminalId
    const deletedTitle = managedTerminal?.title ?? managedTerminalId
    const management = await withManagementApi()
    await management.api.deleteTerminal(deletedTerminalId)
    if (activeTerminalId === deletedTerminalId) {
      setActiveTerminalId(null)
      setPage('terminal-list')
    }
    if (fileTerminalId === deletedTerminalId) {
      setFilesOpen(false)
      setFileTerminalId(null)
    }
    await refreshTerminals()
    setPairStatus(`Deleted ${deletedTitle}`)
    setMobileSheet(null)
  }, [activeTerminalId, canManageTerminals, fileTerminalId, managedTerminal, managedTerminalId, refreshTerminals, withManagementApi])

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
            {onBack ? (
              <button
                type="button"
                aria-label="Back to machines"
                className="mr-1 flex h-8 w-8 items-center justify-center rounded-md text-zinc-600 active:bg-zinc-100"
                onClick={onBack}
              >
                <ChevronLeft className="h-5 w-5" />
              </button>
            ) : null}
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
          {loadingTerminals ? (
            <div className="flex flex-col items-center justify-center p-8 text-sm text-zinc-500">
              <div className="mb-3 h-6 w-6 animate-spin rounded-full border-2 border-zinc-300 border-t-zinc-600"></div>
              {connectionStatus ? <p className="animate-pulse">{connectionStatus}</p> : <p>Loading terminals...</p>}
            </div>
          ) : (
            <TerminalList
              machineId={machine.machineId}
              terminals={terminals}
              onOpenTerminal={openTerminal}
              onManageTerminal={openManageTerminal}
              activeTerminalId={activeTerminalId ?? undefined}
            />
          )}
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
              sessionStore={pair.sessionStore}
              appName={pair.appName}
              machineId={machine.machineId}
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
  }, [activeTerminalId, connectedSession])

  useEffect(() => () => {
    disconnectMachineSession()
  }, [disconnectMachineSession])

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
          {loadingTerminals ? (
            <div className="flex flex-col items-center justify-center p-8 text-sm text-zinc-500">
              <div className="mb-3 h-6 w-6 animate-spin rounded-full border-2 border-zinc-300 border-t-zinc-600"></div>
              {connectionStatus ? <p className="animate-pulse">{connectionStatus}</p> : <p>Loading terminals...</p>}
            </div>
          ) : (
            <TerminalList
              machineId={machine.machineId}
              terminals={terminals}
              onOpenTerminal={openTerminal}
              onManageTerminal={openManageTerminal}
              activeTerminalId={activeTerminalId ?? undefined}
            />
          )}
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
              <span className="max-w-full truncate text-[11px] font-bold uppercase tracking-wider text-zinc-500">{machine.name}</span>
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

        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-black md:bg-zinc-950">
          <div
            className="relative min-h-0 flex-1 bg-black md:m-3 md:rounded-2xl md:border md:border-zinc-800 md:shadow-2xl md:overflow-hidden"
            data-testid="termx-terminal-panel"
          >
            {activeTerminalId && connectedSession && connectedTerminalId === activeTerminalId ? (
              <Terminal
                ref={terminalRef}
                machineId={machine.machineId}
                terminalId={activeTerminalId}
                session={connectedSession}
                className="absolute inset-0 outline-none"
                modifierState={modifierState}
                onModifierStateChange={setModifierState}
                onCursorMove={resetKeyboardOffset}
              />
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-zinc-500">
                {activeTerminalId && connectingTerminalId === activeTerminalId ? 'Connecting terminal...' : 'No active terminal'}
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
              sessionStore={pair.sessionStore}
              appName={pair.appName}
              machineId={machine.machineId}
              onPaired={handlePaired}
            />
          </MobileSheetPanel>
        ) : null}
      </main>
      )}

      <div
        className={`absolute inset-0 z-30 flex flex-col bg-zinc-50 shadow-[0_-20px_40px_rgba(0,0,0,0.15)] transition-all duration-300 md:m-6 md:rounded-2xl md:border md:border-zinc-200/60 ${filesOpen ? 'translate-y-0 visible' : 'translate-y-full invisible'}`}
        data-testid="termx-machine-files-overlay"
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
        {fileTerminalId && connectedSession ? (
          <FileManager
            key={fileTerminalId}
            machineId={machine.machineId}
            terminalId={fileTerminalId}
            session={connectedSession}
            initialPath={fileInitialPath}
            className="flex h-full min-h-0 flex-col relative"
          />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-zinc-500">
            {fileTerminalId ? 'Connecting...' : 'Local file access is not ready'}
          </div>
        )}
      </div>

      <div className={`pointer-events-none absolute bottom-8 left-1/2 z-50 flex -translate-x-1/2 transform flex-col items-center gap-2 transition-all duration-300 ${pairStatus ? 'translate-y-0 opacity-100' : 'translate-y-4 opacity-0'}`}>
        <div className="flex items-center gap-2 rounded-full bg-zinc-900/95 px-4 py-2.5 text-sm font-medium text-white shadow-[0_8px_30px_rgb(0,0,0,0.12)] backdrop-blur-md ring-1 ring-white/10" role="status" aria-live="polite">
          {pairStatus}
        </div>
      </div>
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

function isRtcSessionAlive(session: RtcSession): boolean {
  const candidate = session as RtcSession & Partial<RtcSessionLiveness>
  if (typeof candidate.isAlive !== 'function') return true
  return candidate.isAlive()
}
