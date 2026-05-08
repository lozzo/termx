import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronLeft, Folder, KeyRound, Link2, Link2Off, Monitor, PanelBottomClose, Plus, RefreshCw, Rows2, SquarePen, Trash2, Unlock, X } from 'lucide-react'
import { FileManager } from './FileManager'
import { LocalPairPanel } from './LocalPairPanel'
import type { MachineSessionStore } from './localAppIdentity'
import { MobileTerminalKeybar } from './MobileTerminalKeybar'
import type { TerminalModifierState } from './mobileTerminalInput'
import { PasteConfirmDialog } from './PasteConfirmDialog'
import { Terminal, type TerminalHandle } from './Terminal'
import { TerminalActionToolbar, type TerminalToolbarMode } from './TerminalActionToolbar'
import { TerminalFnPanel } from './TerminalFnPanel'
import { defaultTerminalResizeControl, type TerminalResizeControl } from './terminalClient'
import { TerminalList } from './TerminalList'
import { createTerminalManagementApi } from './terminalManagementApi'
import type { Machine, Terminal as RemoteTerminal } from './model'
import type { LocalAgentApi, LocalCreateTerminalInput, LocalPairingApi, LocalUpdateTerminalInput, RtcConnector, RtcEvent, RtcSession, RtcSessionLiveness, TerminalInventoryEvents } from './transport'

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
  subscribeRuntimeInventoryEvents?: boolean | undefined
  pair?: {
    api: LocalPairingApi
    sessionStore: MachineSessionStore
    appName: string
  } | undefined
  onBack?: (() => void) | undefined
}

type MobileSheet = 'terminals' | 'split-terminal' | 'pair' | 'manage-terminal' | 'edit-terminal' | 'create-terminal' | null
type AppPage = 'terminal-list' | 'terminal'
type TerminalSlot = 0 | 1

export function LocalRemoteApp({ api, connector, className, inventoryEvents, subscribeRuntimeInventoryEvents = false, pair, onBack }: LocalRemoteAppProps) {
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
  const [fileOpenNonce, setFileOpenNonce] = useState(0)
  const [connectionRetryToken, setConnectionRetryToken] = useState(0)
  const [terminalResizeControl, setTerminalResizeControl] = useState<TerminalResizeControl>(defaultTerminalResizeControl)
  const [unlockingResize, setUnlockingResize] = useState(false)

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
  const [terminalToolbarOpen, setTerminalToolbarOpen] = useState(false)
  const [terminalToolbarMode, setTerminalToolbarMode] = useState<TerminalToolbarMode>('default')
  const [terminalFnOpen, setTerminalFnOpen] = useState(false)
  const [hasTerminalSelection, setHasTerminalSelection] = useState(false)
  const [pasteConfirmText, setPasteConfirmText] = useState('')
  const [splitTerminalId, setSplitTerminalId] = useState<string | null>(null)
  const [activeTerminalSlot, setActiveTerminalSlot] = useState<TerminalSlot>(0)
  const [syncSplitInput, setSyncSplitInput] = useState(false)
  const terminalRef = useRef<TerminalHandle | null>(null)
  const splitTerminalRef = useRef<TerminalHandle | null>(null)
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
  const terminalRefreshSeqRef = useRef(0)
  const runtimeInventorySubscriptionRef = useRef<{
    connector: LocalRemoteSessionConnector
    machineId: string
    retryToken: number
    session: RtcSession
    subscription: { close(): void }
  } | null>(null)
  const [listPullDistance, setListPullDistance] = useState(0)
  const activeTerminal = terminals.find((terminal) => terminal.terminalId === activeTerminalId)
  const splitTerminal = terminals.find((terminal) => terminal.terminalId === splitTerminalId)
  const activeToolTerminal = activeTerminalSlot === 1 && splitTerminal ? splitTerminal : activeTerminal
  const managedTerminal = terminals.find((terminal) => terminal.terminalId === managedTerminalId)
  const activeTerminalTitle = activeTerminal?.title || activeTerminal?.command || activeTerminalId || 'Terminal'
  const splitTerminalTitle = splitTerminal?.title || splitTerminal?.command || splitTerminalId || 'Terminal'
  const terminalHeaderTitle = splitTerminalId ? `${activeTerminalTitle} / ${splitTerminalTitle}` : activeTerminalTitle
  const activeTerminalResizeLocked = terminalResizeControl.sizeLocked === true || terminalResizeControl.reason === 'size_locked'
  const requireVerification = Boolean(pair && !verifiedDevice)
  const canManageTerminals = true

  const disconnectMachineSession = useCallback(() => {
    machineSessionConnectSeqRef.current += 1
    const current = machineSessionRef.current
    machineSessionPromiseRef.current = null
    machineSessionRef.current = null
    const runtimeInventorySubscription = runtimeInventorySubscriptionRef.current
    runtimeInventorySubscriptionRef.current = null
    runtimeInventorySubscription?.subscription.close()
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
    const seq = terminalRefreshSeqRef.current + 1
    terminalRefreshSeqRef.current = seq
    setRefreshingTerminals(true)
    try {
      const status = await api.getStatus()
      if (terminalRefreshSeqRef.current !== seq) return
      setMachine(status.machine)
      const terminalList = await api.listTerminals({
        onStatus: (status) => {
          if (terminalRefreshSeqRef.current === seq) setConnectionStatus(status)
        },
      })
      if (terminalRefreshSeqRef.current !== seq) return
      setTerminals(terminalList)
      setError(null)
    } catch (err) {
      if (terminalRefreshSeqRef.current === seq) setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (terminalRefreshSeqRef.current === seq) {
        setRefreshingTerminals(false)
        setConnectionStatus(null)
        setLoadingTerminals(false)
      }
    }
  }, [api])

  useEffect(() => {
    let cancelled = false
    const seq = terminalRefreshSeqRef.current + 1
    terminalRefreshSeqRef.current = seq
    async function load() {
      setLoadingTerminals(true)
      try {
        const status = await api.getStatus()
        if (cancelled || terminalRefreshSeqRef.current !== seq) return
        setMachine(status.machine)
        const terminalList = await api.listTerminals({
          onStatus: (status) => {
            if (!cancelled && terminalRefreshSeqRef.current === seq) setConnectionStatus(status)
          },
        })
        if (cancelled || terminalRefreshSeqRef.current !== seq) return
        setTerminals(terminalList)
      } catch (err) {
        if (!cancelled && terminalRefreshSeqRef.current === seq) setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!cancelled && terminalRefreshSeqRef.current === seq) {
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
    if (!subscribeRuntimeInventoryEvents || requireVerification || !machineId) return
    let cancelled = false
    void ensureMachineSession(machineId).then((session) => {
      if (cancelled) return
      const current = runtimeInventorySubscriptionRef.current
      if (
        current &&
        current.connector === connector &&
        current.machineId === machineId &&
        current.retryToken === connectionRetryToken &&
        current.session === session
      ) {
        return
      }
      current?.subscription.close()
      const subscription = session.subscribeEvents((event) => {
        if (isTerminalInventoryRuntimeEvent(event)) void refreshTerminals()
      })
      runtimeInventorySubscriptionRef.current = {
        connector,
        machineId,
        retryToken: connectionRetryToken,
        session,
        subscription,
      }
    }).catch(() => {
      if (cancelled) return
    })
    return () => {
      cancelled = true
      const current = runtimeInventorySubscriptionRef.current
      if (
        current &&
        current.connector === connector &&
        current.machineId === machineId &&
        current.retryToken === connectionRetryToken
      ) {
        runtimeInventorySubscriptionRef.current = null
        current.subscription.close()
      }
    }
  }, [connectionRetryToken, connector, ensureMachineSession, machine?.machineId, refreshTerminals, requireVerification, subscribeRuntimeInventoryEvents])

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
    if (splitTerminalId === intent.terminalId) {
      setSplitTerminalId(null)
      setActiveTerminalSlot(0)
      setSyncSplitInput(false)
    }
    setPage('terminal')
    setMobileSheet(null)
  }, [machine, requireVerification, splitTerminalId])

  const openSplitTerminalSheet = useCallback(() => {
    if (requireVerification) {
      setMobileSheet('pair')
      return
    }
    if (!activeTerminalId) {
      setError('Open a terminal before starting split view')
      return
    }
    const availableTerminals = terminals.filter((terminal) => terminal.terminalId !== activeTerminalId)
    if (availableTerminals.length === 0) {
      setPairStatus('No other terminal to split')
      return
    }
    setTerminalToolbarOpen(false)
    setTerminalFnOpen(false)
    setMobileSheet('split-terminal')
  }, [activeTerminalId, requireVerification, terminals])

  const selectSplitTerminal = useCallback((intent: { machineId: string; terminalId: string }) => {
    if (machine && intent.machineId !== machine.machineId) {
      setError(`terminal machine mismatch: ${intent.machineId} != ${machine.machineId}`)
      return
    }
    if (intent.terminalId === activeTerminalId) {
      setPairStatus('Choose a different terminal')
      return
    }
    setSplitTerminalId(intent.terminalId)
    setActiveTerminalSlot(1)
    setMobileSheet(null)
    window.setTimeout(() => {
      terminalRef.current?.fit()
      splitTerminalRef.current?.fit()
      splitTerminalRef.current?.focus()
    }, 0)
  }, [activeTerminalId, machine])

  const closeSplitTerminal = useCallback(() => {
    setSplitTerminalId(null)
    setActiveTerminalSlot(0)
    setSyncSplitInput(false)
    window.setTimeout(() => {
      terminalRef.current?.fit()
      terminalRef.current?.focus()
    }, 0)
  }, [])

  const activeTerminalHandle = useCallback(() => {
    if (activeTerminalSlot === 1 && splitTerminalId) return splitTerminalRef.current
    return terminalRef.current
  }, [activeTerminalSlot, splitTerminalId])

  const sendTerminalInput = useCallback((data: string) => {
    if (syncSplitInput && splitTerminalId) {
      terminalRef.current?.sendInput(data)
      splitTerminalRef.current?.sendInput(data)
      return
    }
    activeTerminalHandle()?.sendInput(data)
  }, [activeTerminalHandle, splitTerminalId, syncSplitInput])

  const pasteTerminalText = useCallback((text: string) => {
    if (syncSplitInput && splitTerminalId) {
      terminalRef.current?.pasteText(text)
      splitTerminalRef.current?.pasteText(text)
      return
    }
    activeTerminalHandle()?.pasteText(text)
  }, [activeTerminalHandle, splitTerminalId, syncSplitInput])

  const focusActiveTerminal = useCallback(() => {
    activeTerminalHandle()?.focus()
  }, [activeTerminalHandle])

  useEffect(() => {
    if (!splitTerminalId) return
    const splitStillAvailable = terminals.some((terminal) => terminal.terminalId === splitTerminalId)
    if (splitTerminalId === activeTerminalId || !splitStillAvailable) {
      setSplitTerminalId(null)
      setActiveTerminalSlot(0)
      setSyncSplitInput(false)
    }
  }, [activeTerminalId, splitTerminalId, terminals])

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
    setSplitTerminalId(null)
    setActiveTerminalSlot(0)
    setSyncSplitInput(false)
    terminalRef.current?.adjustInputPosition(0)
    splitTerminalRef.current?.adjustInputPosition(0)
  }, [])

  const openFiles = useCallback(() => {
    if (requireVerification) {
      setMobileSheet('pair')
      return
    }
    const fileTerminal = page === 'terminal' ? activeToolTerminal : (activeToolTerminal ?? terminals[0])
    if (!machine || !fileTerminal) {
      setError('No terminal is available for local file access')
      return
    }
    const fallbackPath = fileTerminal.cwd || '/'
    const resolveTerminalDirectory = page === 'terminal'
    setFileInitialPath(fallbackPath)
    setFileOpenNonce((current) => current + 1)
    if (fileTerminalId !== fileTerminal.terminalId) {
      setFileTerminalId(fileTerminal.terminalId)
    }
    void ensureMachineSession(machine.machineId)
      .then(async (session) => {
        if (!resolveTerminalDirectory) return
        try {
          const directory = await createTerminalManagementApi(session, machine.machineId)
            .getTerminalDirectory(fileTerminal.terminalId)
          const livePath = normalizeTerminalDirectory(directory.path)
          if (livePath) {
            setFileInitialPath(livePath)
            setFileOpenNonce((current) => current + 1)
          }
        } catch {
          setFileInitialPath(fallbackPath)
          setFileOpenNonce((current) => current + 1)
        }
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err))
        setFilesOpen(false)
        setFileTerminalId(null)
        if (pair) setMobileSheet('pair')
      })
    setFilesOpen(true)
    setMobileSheet(null)
  }, [activeToolTerminal, ensureMachineSession, fileTerminalId, machine, page, pair, requireVerification, terminals])

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

  const unlockTerminalResize = useCallback(async () => {
    if (!canManageTerminals || !activeTerminalId) return
    setUnlockingResize(true)
    try {
      const management = await withManagementApi()
      await management.api.updateTerminal({
        terminalId: activeTerminalId,
        sizeLockMode: 'off',
      })
      setTerminalResizeControl(defaultTerminalResizeControl)
      await refreshTerminals()
      terminalRef.current?.reattach(management.session)
      window.setTimeout(() => {
        terminalRef.current?.fit()
        terminalRef.current?.focus()
      }, 0)
      setPairStatus('Resize unlocked')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setUnlockingResize(false)
    }
  }, [activeTerminalId, canManageTerminals, refreshTerminals, withManagementApi])

  const deleteManagedTerminal = useCallback(async () => {
    if (!canManageTerminals || !managedTerminalId) return
    const deletedTerminalId = managedTerminalId
    const deletedTitle = managedTerminal?.title ?? managedTerminalId
    const management = await withManagementApi()
    await management.api.deleteTerminal(deletedTerminalId)
    if (activeTerminalId === deletedTerminalId) {
      setActiveTerminalId(null)
      setSplitTerminalId(null)
      setActiveTerminalSlot(0)
      setSyncSplitInput(false)
      setPage('terminal-list')
    }
    if (fileTerminalId === deletedTerminalId) {
      setFilesOpen(false)
      setFileTerminalId(null)
    }
    if (splitTerminalId === deletedTerminalId) {
      setSplitTerminalId(null)
      setActiveTerminalSlot(0)
      setSyncSplitInput(false)
    }
    await refreshTerminals()
    setPairStatus(`Deleted ${deletedTitle}`)
    setMobileSheet(null)
  }, [activeTerminalId, canManageTerminals, fileTerminalId, managedTerminal, managedTerminalId, refreshTerminals, splitTerminalId, withManagementApi])

  const openTerminalPanel = useCallback(() => {
    setFilesOpen(false)
    window.setTimeout(() => {
      terminalRef.current?.fit()
      splitTerminalRef.current?.fit()
      focusActiveTerminal()
    }, 0)
  }, [focusActiveTerminal])

  const resetKeyboardOffset = useCallback(() => {
    terminalRef.current?.adjustInputPosition(0)
    splitTerminalRef.current?.adjustInputPosition(0)
  }, [])

  const setTerminalToolbarModeAndReset = useCallback((mode: TerminalToolbarMode) => {
    setTerminalToolbarMode(mode)
    if (mode !== 'selection') {
      setHasTerminalSelection(false)
      activeTerminalHandle()?.clearSelection()
    }
  }, [activeTerminalHandle])

  useEffect(() => {
    if (!terminalToolbarOpen || terminalToolbarMode !== 'selection') return
    const timer = window.setInterval(() => {
      setHasTerminalSelection(activeTerminalHandle()?.hasSelection() ?? false)
    }, 200)
    return () => window.clearInterval(timer)
  }, [activeTerminalHandle, terminalToolbarMode, terminalToolbarOpen])

  const handleTerminalPaste = useCallback(async () => {
    try {
      const text = await navigator.clipboard.readText()
      if (!text) {
        setPairStatus('Clipboard is empty')
        return
      }
      const needsConfirm = text.length > 200 || text.includes('\n') || text.includes('\r')
      if (needsConfirm) {
        setPasteConfirmText(text)
        return
      }
      pasteTerminalText(text)
      setTerminalToolbarOpen(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to read clipboard')
    }
  }, [pasteTerminalText])

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
    setTerminalResizeControl(defaultTerminalResizeControl)
    if (typeof window === 'undefined' || !window.visualViewport) {
      terminalRef.current?.adjustInputPosition(0)
      splitTerminalRef.current?.adjustInputPosition(0)
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
        const keyboardOffset = bottomOffset > 80 ? bottomOffset : 0
        terminalRef.current?.adjustInputPosition(keyboardOffset)
        splitTerminalRef.current?.adjustInputPosition(keyboardOffset)
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
      splitTerminalRef.current?.adjustInputPosition(0)
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
        <header className="absolute inset-x-0 top-0 z-30 flex h-10 items-center justify-between gap-1 border-b border-zinc-800/70 bg-zinc-950/70 px-1.5 backdrop-blur-lg md:hidden">
          <div className="flex min-w-0 flex-1 items-center gap-1">
            <button
              type="button"
              aria-label="Back to terminal list"
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-zinc-400 transition-colors active:bg-zinc-800"
              onClick={showTerminalListPage}
            >
              <ChevronLeft className="h-5 w-5" />
            </button>
            <button
              type="button"
              aria-label="Switch terminal"
              className="flex min-w-0 flex-1 flex-col items-start justify-center rounded-md px-1.5 py-0.5 text-left transition-colors active:bg-zinc-800"
              onClick={() => setMobileSheet('terminals')}
            >
              <span className="max-w-full truncate text-[9px] font-bold uppercase tracking-wider text-zinc-500">{machine.name}</span>
              <span className="max-w-full truncate text-[12px] font-semibold leading-tight text-zinc-100" data-testid="termx-terminal-title">{terminalHeaderTitle}</span>
            </button>
          </div>

          <div className="flex shrink-0 items-center gap-0.5">
            <button
              type="button"
              aria-label="Split terminal"
              aria-pressed={Boolean(splitTerminalId)}
              onClick={openSplitTerminalSheet}
              className={`flex h-8 w-8 items-center justify-center rounded-md transition-colors active:scale-95 ${splitTerminalId ? 'bg-zinc-100 text-zinc-900' : 'text-zinc-400 active:bg-zinc-800'}`}
            >
              <Rows2 className="h-4 w-4" />
            </button>
            {splitTerminalId ? (
              <>
                <button
                  type="button"
                  aria-label={syncSplitInput ? 'Disable synchronized input' : 'Enable synchronized input'}
                  aria-pressed={syncSplitInput}
                  onClick={() => setSyncSplitInput((current) => !current)}
                  className={`flex h-8 w-8 items-center justify-center rounded-md transition-colors active:scale-95 ${syncSplitInput ? 'bg-blue-500 text-white' : 'text-zinc-400 active:bg-zinc-800'}`}
                >
                  {syncSplitInput ? <Link2 className="h-4 w-4" /> : <Link2Off className="h-4 w-4" />}
                </button>
                <button
                  type="button"
                  aria-label="Close split terminal"
                  onClick={closeSplitTerminal}
                  className="flex h-8 w-8 items-center justify-center rounded-md text-zinc-400 transition-colors active:scale-95 active:bg-zinc-800"
                >
                  <PanelBottomClose className="h-4 w-4" />
                </button>
              </>
            ) : null}
            <button
              type="button"
              aria-label="Terminal tools"
              onClick={() => {
                setTerminalToolbarOpen((current) => {
                  const next = !current
                  if (next) setTerminalFnOpen(false)
                  if (!next) setTerminalToolbarModeAndReset('default')
                  return next
                })
              }}
              className={`flex h-8 w-8 items-center justify-center rounded-md transition-colors active:scale-95 ${terminalToolbarOpen ? 'bg-zinc-100 text-zinc-900' : 'text-zinc-400 active:bg-zinc-800'}`}
            >
              <span className="text-[13px] font-bold leading-none">•••</span>
            </button>
            <button
              type="button"
              aria-label="Open files"
              onClick={openFiles}
              className={`flex h-8 w-8 items-center justify-center rounded-md transition-colors active:scale-95 ${filesOpen ? 'bg-zinc-100 text-zinc-900' : 'text-zinc-400 active:bg-zinc-800'}`}
            >
              <Folder className="h-4 w-4" />
            </button>
          </div>
        </header>

        {terminalToolbarOpen ? (
          <TerminalActionToolbar
            mode={terminalToolbarMode}
            hasSelection={hasTerminalSelection}
            onModeChange={setTerminalToolbarModeAndReset}
            onSelectAll={() => {
              activeTerminalHandle()?.selectAll()
              setHasTerminalSelection(true)
            }}
            onSelectVisible={() => {
              activeTerminalHandle()?.selectVisible()
              setHasTerminalSelection(true)
            }}
            onCopy={() => {
              const selected = activeTerminalHandle()?.getSelection() ?? ''
              if (!selected) return
              void navigator.clipboard.writeText(selected).then(() => {
                setPairStatus('Copied')
                setTerminalToolbarOpen(false)
                setTerminalToolbarModeAndReset('default')
              }).catch((err: unknown) => {
                setError(err instanceof Error ? err.message : 'Copy failed')
              })
            }}
            onPaste={() => { void handleTerminalPaste() }}
            onOpenSnippets={() => {
              setTerminalFnOpen((current) => !current)
              setTerminalToolbarOpen(false)
            }}
          />
        ) : null}

        {terminalFnOpen ? (
          <TerminalFnPanel
            command={activeToolTerminal?.command || activeToolTerminal?.title}
            onSend={(data) => {
              sendTerminalInput(data)
              if (data.endsWith('\n')) setTerminalFnOpen(false)
            }}
          />
        ) : null}

        {error ? (
          <div className="absolute inset-x-0 top-10 z-40 border-y border-red-500/30 bg-red-950/90 px-3 py-2 text-[12px] font-medium text-red-100 shadow-lg backdrop-blur md:top-3" role="alert">
            {error}
          </div>
        ) : null}

        <div className={`relative flex min-h-0 flex-1 flex-col overflow-hidden bg-black md:bg-zinc-950 ${splitTerminalId ? 'gap-px md:gap-3 md:p-3' : ''}`}>
          <div
            className={`relative min-h-0 flex-1 bg-black ${splitTerminalId ? `border-b border-zinc-800 md:rounded-xl md:border md:shadow-2xl md:overflow-hidden ${activeTerminalSlot === 0 ? 'shadow-[inset_0_0_0_1px_rgba(96,165,250,0.55)]' : ''}` : 'md:m-3 md:rounded-2xl md:border md:border-zinc-800 md:shadow-2xl md:overflow-hidden'}`}
            data-active-slot={activeTerminalSlot === 0 ? 'true' : 'false'}
            data-testid="termx-terminal-panel"
            onPointerDown={() => setActiveTerminalSlot(0)}
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
                onResizeControl={setTerminalResizeControl}
                selectionMode={terminalToolbarOpen && terminalToolbarMode === 'selection' && activeTerminalSlot === 0}
              />
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-zinc-500">
                {activeTerminalId && connectingTerminalId === activeTerminalId ? 'Connecting terminal...' : 'No active terminal'}
              </div>
            )}
            {activeTerminalId && connectedSession && connectedTerminalId === activeTerminalId && activeTerminalResizeLocked ? (
              <button
                type="button"
                aria-label="Unlock terminal resize"
                className={`absolute right-2 z-20 flex min-h-7 items-center gap-1.5 rounded-md border border-zinc-700 bg-zinc-950/90 px-2 text-[11px] font-semibold text-zinc-100 shadow-lg backdrop-blur active:bg-zinc-800 disabled:opacity-60 ${splitTerminalId ? 'top-16' : 'top-12'}`}
                disabled={unlockingResize}
                onClick={() => { void unlockTerminalResize() }}
              >
                <Unlock className="h-3.5 w-3.5" />
                {unlockingResize ? 'Unlocking...' : 'Unlock resize'}
              </button>
            ) : null}
          </div>

          {splitTerminalId ? (
            <div
              className={`relative min-h-0 flex-1 bg-black md:rounded-xl md:border md:border-zinc-800 md:shadow-2xl md:overflow-hidden ${activeTerminalSlot === 1 ? 'shadow-[inset_0_0_0_1px_rgba(96,165,250,0.55)]' : ''}`}
              data-active-slot={activeTerminalSlot === 1 ? 'true' : 'false'}
              data-testid="termx-split-terminal-panel"
              onPointerDown={() => setActiveTerminalSlot(1)}
            >
              {connectedSession ? (
                <Terminal
                  ref={splitTerminalRef}
                  machineId={machine.machineId}
                  terminalId={splitTerminalId}
                  session={connectedSession}
                  className="absolute inset-0 outline-none"
                  modifierState={modifierState}
                  onModifierStateChange={setModifierState}
                  onCursorMove={resetKeyboardOffset}
                  selectionMode={terminalToolbarOpen && terminalToolbarMode === 'selection' && activeTerminalSlot === 1}
                />
              ) : (
                <div className="absolute inset-0 flex items-center justify-center text-sm text-zinc-500">
                  Connecting terminal...
                </div>
              )}
            </div>
          ) : null}
        </div>

        <MobileTerminalKeybar
          onInput={sendTerminalInput}
          onFocusKeyboard={focusActiveTerminal}
          onBlurKeyboard={() => activeTerminalHandle()?.blur()}
          fnOpen={terminalFnOpen}
          onToggleFn={() => {
            setTerminalFnOpen((current) => !current)
            setTerminalToolbarOpen(false)
          }}
          modifierState={modifierState}
          onModifierStateChange={setModifierState}
        />

        {pasteConfirmText ? (
          <PasteConfirmDialog
            text={pasteConfirmText}
            onCancel={() => setPasteConfirmText('')}
            onConfirm={() => {
              pasteTerminalText(pasteConfirmText)
              setPasteConfirmText('')
              setTerminalToolbarOpen(false)
              setTerminalToolbarModeAndReset('default')
            }}
          />
        ) : null}

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

        {mobileSheet === 'split-terminal' ? (
          <MobileSheetPanel title="Split Terminal" testId="termx-split-terminal-sheet" onClose={() => setMobileSheet(null)}>
            {terminals.filter((terminal) => terminal.terminalId !== activeTerminalId).length > 0 ? (
              <TerminalList
                machineId={machine.machineId}
                terminals={terminals.filter((terminal) => terminal.terminalId !== activeTerminalId)}
                onOpenTerminal={selectSplitTerminal}
                activeTerminalId={splitTerminalId ?? undefined}
              />
            ) : (
              <div className="flex min-h-24 items-center justify-center rounded-xl border border-dashed border-zinc-200 bg-white text-sm font-medium text-zinc-500">
                No other terminal available
              </div>
            )}
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
            key={`${fileTerminalId}:${fileInitialPath}:${fileOpenNonce}`}
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

function normalizeTerminalDirectory(path: string | undefined): string {
  const trimmed = path?.trim()
  if (!trimmed) return ''
  if (trimmed.startsWith('file://')) {
    try {
      const url = new URL(trimmed)
      if (url.pathname) return decodeURIComponent(url.pathname)
    } catch {
      return ''
    }
  }
  return trimmed.startsWith('/') ? trimmed : ''
}

function isTerminalInventoryRuntimeEvent(event: RtcEvent): boolean {
  if (event.type === 'inventory_changed') return true
  if (event.type === 'terminal_changed') return true
  return event.type === 'terminal_created' ||
    event.type === 'terminal_state_changed' ||
    event.type === 'terminal_resized' ||
    event.type === 'terminal_removed' ||
    event.type === 'terminal_metadata_changed'
}
