import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties, type ReactNode } from 'react'
import { ChevronLeft, Folder, Info, KeyRound, Link2, Link2Off, Monitor, PanelBottomClose, Plus, Rows2, SquarePen, Trash2, Unlock, X } from 'lucide-react'
import { connectionPhaseLabel, connectionSnapshotFromStatus } from '../connection/connectionState'
import { FileTransferPanel } from '../files/FileTransferPanel'
import { FileManager } from '../files/FileManager'
import { haptic } from '../platform/haptics'
import { PairDevicePanel } from '../pairing/PairDevicePanel'
import type { MachineSessionStore } from '../state/localAppIdentity'
import { MachineNetworkStatusOverlay } from '../machine-runtime/MachineNetworkStatusOverlay'
import { useMachineNetworkStatus } from '../machine-runtime/useMachineNetworkStatus'
import { MobileTerminalKeybar } from '../terminal/MobileTerminalKeybar'
import type { TerminalModifierState } from '../terminal/mobileTerminalInput'
import { PasteConfirmDialog } from '../terminal/PasteConfirmDialog'
import { Terminal, type TerminalHandle } from '../terminal/Terminal'
import { TerminalActionToolbar, type TerminalToolbarMode } from '../terminal/TerminalActionToolbar'
import { TerminalFnPanel } from '../terminal/TerminalFnPanel'
import { addNativeBackHandler } from '../platform/nativeBack'
import { defaultTerminalResizeControl, type TerminalResizeControl } from '../terminal/terminalClient'
import { TerminalList } from '../terminal/TerminalList'
import { createTerminalManagementApi } from '../terminal/terminalManagementApi'
import { readTerminalSettings, terminalThemeCssVariables, writeTerminalSettings, type TerminalSettings } from '../terminal/terminalSettings'
import type { Machine, Terminal as RemoteTerminal } from '../core/model'
import type { ConnectionInfo, LocalAgentApi, LocalCreateTerminalInput, LocalPairingApi, LocalUpdateTerminalInput, MachineConnectionStateEvents, RtcConnectOptions, RtcConnectionStateSnapshot, RtcConnector, RtcEvent, RtcSession, RtcSessionConnectionStateEvents, RtcSessionLiveness, RtcSubscription, TerminalInventoryEvents } from '../core/transport'
import { useTerminalKeyboard } from '../terminal/useTerminalKeyboard'

export interface MachineWorkspaceInventoryApi extends Pick<LocalAgentApi, 'getStatus'> {
  listTerminals(options?: Pick<RtcConnectOptions, 'forceRelay' | 'onStatus' | 'onConnectionState'>): Promise<RemoteTerminal[]>
}

export interface MachineWorkspaceSessionInput {
  machineId: string
}

export type MachineWorkspaceConnector = RtcConnector<MachineWorkspaceSessionInput> & {
  reconnect?: ((options?: { forceRelay?: boolean | undefined }) => void) | undefined
}

export interface MachineWorkspaceProps {
  api: MachineWorkspaceInventoryApi
  connector: MachineWorkspaceConnector
  className?: string | undefined
  inventoryEvents?: TerminalInventoryEvents | undefined
  connectionStateEvents?: MachineConnectionStateEvents | undefined
  subscribeRuntimeInventoryEvents?: boolean | undefined
  pair?: {
    api: LocalPairingApi
    sessionStore: MachineSessionStore
    appName: string
  } | undefined
  onBack?: (() => void) | undefined
  fileTransfer?: import('../files/fileApi').FileTransferContext | undefined
  terminalSettings?: TerminalSettings | undefined
  onTerminalSettingsChange?: ((patch: Partial<TerminalSettings>) => void) | undefined
}

type MobileSheet = 'terminals' | 'split-terminal' | 'pair' | 'manage-terminal' | 'edit-terminal' | 'create-terminal' | null
type AppPage = 'terminal-list' | 'terminal'
type TerminalSlot = 0 | 1
const TERMINAL_CONNECTION_PROGRESS_DELAY_MS = 450

function noopSubscribe(_listener: () => void): () => void { return () => {} }

export function MachineWorkspace({ api, connector, className, inventoryEvents, connectionStateEvents, subscribeRuntimeInventoryEvents = false, pair, onBack, fileTransfer, terminalSettings: terminalSettingsProp, onTerminalSettingsChange }: MachineWorkspaceProps) {
  const [machine, setMachine] = useState<Machine | null>(null)
  const [terminals, setTerminals] = useState<RemoteTerminal[]>([])
  const [loadingTerminals, setLoadingTerminals] = useState(true)
  const [activeTerminalId, setActiveTerminalId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pairStatus, setPairStatus] = useState<string | null>(null)
  const [verifiedDevice, setVerifiedDevice] = useState(() => pair && machine ? Boolean(pair.sessionStore.getSessionToken(machine.machineId)) : !pair)
  const [connectedSession, setConnectedSession] = useState<RtcSession | null>(null)
  const [connectedTerminalId, setConnectedTerminalId] = useState<string | null>(null)
  const [connectingTerminalId, setConnectingTerminalId] = useState<string | null>(null)
  const [fileTerminalId, setFileTerminalId] = useState<string | null>(null)
  const [fileInitialPath, setFileInitialPath] = useState('/')
  const [fileContextKey, setFileContextKey] = useState('machine:/')
  const [connectionRetryToken, setConnectionRetryToken] = useState(0)
  const [forceRelayConnection, setForceRelayConnection] = useState(false)
  const [connectionInfoOpen, setConnectionInfoOpen] = useState(false)
  const [connectionInfo, setConnectionInfo] = useState<ConnectionInfo | null>(null)
  const [connectionInfoLoading, setConnectionInfoLoading] = useState(false)
  const [connectionInfoError, setConnectionInfoError] = useState<string | null>(null)
  const [manualReconnectNonce, setManualReconnectNonce] = useState(0)
  const [terminalResizeControl, setTerminalResizeControl] = useState<TerminalResizeControl>(defaultTerminalResizeControl)
  const [unlockingResize, setUnlockingResize] = useState(false)

  const [page, setPage] = useState<AppPage>('terminal-list')
  const [filesOpen, setFilesOpen] = useState(false)
  const [transferCenterOpen, setTransferCenterOpen] = useState(false)
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
    command: '',
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
  const [terminalSettings, setTerminalSettings] = useState<TerminalSettings>(() => readTerminalSettings())
  const terminalRef = useRef<TerminalHandle | null>(null)
  const splitTerminalRef = useRef<TerminalHandle | null>(null)
  const outerContainerRef = useRef<HTMLDivElement | null>(null)
  const terminalAreaRef = useRef<HTMLDivElement | null>(null)
  const terminalWrapperRef = useRef<HTMLDivElement | null>(null)
  const mobileKeybarRef = useRef<HTMLDivElement | null>(null)
  const activeTerminalSlotRef = useRef<TerminalSlot>(0)
  const [keyboardLocked, setKeyboardLocked] = useState(false)
  const keyboardLockedRef = useRef(false)
  const machineSessionRef = useRef<{
    connector: MachineWorkspaceConnector
    machineId: string
    retryToken: number
    session: RtcSession
    forceRelay: boolean
  } | null>(null)
  const machineSessionPromiseRef = useRef<{
    connector: MachineWorkspaceConnector
    machineId: string
    retryToken: number
    forceRelay: boolean
    promise: Promise<RtcSession>
  } | null>(null)
  const machineSessionConnectSeqRef = useRef(0)
  const terminalRefreshSeqRef = useRef(0)
  const runtimeInventorySubscriptionRef = useRef<{
    connector: MachineWorkspaceConnector
    machineId: string
    retryToken: number
    session: RtcSession
    subscription: { close(): void }
  } | null>(null)
  const connectionStateSubscriptionRef = useRef<RtcSubscription | null>(null)
  const passiveConnectionPhaseRef = useRef<RtcConnectionStateSnapshot['phase'] | null>(null)
  const latestActiveTerminalIdRef = useRef<string | null>(null)
  const handledManualReconnectNonceRef = useRef(0)
  const activeTerminal = terminals.find((terminal) => terminal.terminalId === activeTerminalId)
  const splitTerminal = terminals.find((terminal) => terminal.terminalId === splitTerminalId)
  const activeToolTerminal = activeTerminalSlot === 1 && splitTerminal ? splitTerminal : activeTerminal
  const managedTerminal = terminals.find((terminal) => terminal.terminalId === managedTerminalId)
  const activeTerminalTitle = activeTerminal?.title || activeTerminal?.command || activeTerminalId || 'Terminal'
  const splitTerminalTitle = splitTerminal?.title || splitTerminal?.command || splitTerminalId || 'Terminal'
  const terminalHeaderTitle = splitTerminalId ? `${activeTerminalTitle} / ${splitTerminalTitle}` : activeTerminalTitle
  const terminalHeaderDirectory = activeToolTerminal?.cwd || activeTerminal?.cwd || splitTerminal?.cwd || ''
  const activeTerminalResizeLocked = terminalResizeControl.sizeLocked === true || terminalResizeControl.reason === 'size_locked'
  const requireVerification = Boolean(pair && !verifiedDevice)
  const canManageTerminals = true
  const emptyTransferSnapshot = useMemo(() => ({ transfers: [], hasActiveTransfers: false }), [])
  const transferState = useSyncExternalStore(
    fileTransfer?.subscribe ?? noopSubscribe,
    fileTransfer?.getSnapshot ?? (() => emptyTransferSnapshot),
  )
  const {
    connectionStatus,
    connectionPhase,
    showMachineNetworkOverlay,
    setMachineNetworkMachineId,
    updateConnectionStatus,
    clearConnectionStatus,
    clearConnectionStatusSoon,
  } = useMachineNetworkStatus()
  const machineForceRelayKey = machine?.machineId ? forceRelayStorageKey(machine.machineId) : null
  const effectiveTerminalSettings = terminalSettingsProp ?? terminalSettings
  const terminalThemeStyle = useMemo(
    () => terminalThemeCssVariables(effectiveTerminalSettings.themeId) as CSSProperties,
    [effectiveTerminalSettings.themeId],
  )

  useEffect(() => {
    latestActiveTerminalIdRef.current = activeTerminalId
  }, [activeTerminalId])

  useEffect(() => {
    setMachineNetworkMachineId(machine?.machineId ?? null)
  }, [machine?.machineId, setMachineNetworkMachineId])

  useEffect(() => {
    if (!machineForceRelayKey) return
    setForceRelayConnection(readStoredForceRelay(machineForceRelayKey))
  }, [machineForceRelayKey])

  const { keyboardVisible, handleBufferChange, handleCursorMove } = useTerminalKeyboard({
    containerRef: outerContainerRef,
    mainRef: terminalAreaRef,
    termWrapperRef: terminalWrapperRef,
    getTermRef: () => activeTerminalSlotRef.current === 1 ? splitTerminalRef.current : terminalRef.current,
    shouldResize: () => {
      if (splitTerminalId) return true
      if (effectiveTerminalSettings.keyboardMode === 'resize') return true
      if (effectiveTerminalSettings.keyboardMode === 'shift') return false
      return terminalRef.current?.getBufferType() === 'alternate' ||
        splitTerminalRef.current?.getBufferType() === 'alternate'
    },
    onKeyboardHide: () => {
      requestAnimationFrame(() => {
        terminalRef.current?.fit()
        splitTerminalRef.current?.fit()
      })
    },
  })

  useEffect(() => {
    const keybar = mobileKeybarRef.current
    const terminalArea = terminalAreaRef.current
    if (!keybar || !terminalArea || typeof ResizeObserver === 'undefined') return

    let fitFrame = 0
    const refitTerminals = () => {
      if (fitFrame) cancelAnimationFrame(fitFrame)
      fitFrame = requestAnimationFrame(() => {
        fitFrame = requestAnimationFrame(() => {
          fitFrame = 0
          terminalRef.current?.fit()
          splitTerminalRef.current?.fit()
        })
      })
    }

    refitTerminals()
    const observer = new ResizeObserver(refitTerminals)
    observer.observe(keybar)
    observer.observe(terminalArea)
    return () => {
      observer.disconnect()
      if (fitFrame) cancelAnimationFrame(fitFrame)
    }
  }, [page])

  const updateTerminalSettings = useCallback((patch: Partial<TerminalSettings>) => {
    haptic()
    if (onTerminalSettingsChange) {
      onTerminalSettingsChange(patch)
      return
    }
    setTerminalSettings((current) => writeTerminalSettings({ ...current, ...patch }))
  }, [onTerminalSettingsChange])

  const updateFromConnectionState = useCallback((snapshot: RtcConnectionStateSnapshot, session?: RtcSession) => {
    if (snapshot.phase === 'connected') {
      setError(null)
      if (session) {
        setConnectedSession(session)
        const terminalId = latestActiveTerminalIdRef.current
        if (terminalId) {
          setConnectedTerminalId(terminalId)
          setConnectingTerminalId(null)
        }
      }
      updateConnectionStatus(snapshot.statusText || 'Connected', 'connected')
      clearConnectionStatusSoon()
      return
    }
    if (snapshot.phase === 'idle') {
      clearConnectionStatus()
      return
    }
    if (snapshot.phase === 'reconnecting' || snapshot.phase === 'waiting_network') setError(null)
    updateConnectionStatus(snapshot.statusText || connectionPhaseLabel(snapshot.phase), snapshot.phase)
    if (snapshot.phase === 'failed') setError(snapshot.failReason || snapshot.statusText || 'Connection failed')
  }, [clearConnectionStatus, clearConnectionStatusSoon, updateConnectionStatus])

  const updateFromPassiveConnectionState = useCallback((snapshot: RtcConnectionStateSnapshot, session?: RtcSession) => {
    if (isTransientConnectionPhase(snapshot.phase)) return
    updateFromConnectionState(snapshot, session)
  }, [updateFromConnectionState])

  const disconnectMachineSession = useCallback(() => {
    machineSessionConnectSeqRef.current += 1
    connectionStateSubscriptionRef.current?.close()
    connectionStateSubscriptionRef.current = null
    const current = machineSessionRef.current
    machineSessionPromiseRef.current = null
    machineSessionRef.current = null
    const runtimeInventorySubscription = runtimeInventorySubscriptionRef.current
    runtimeInventorySubscriptionRef.current = null
    runtimeInventorySubscription?.subscription.close()
    void current?.session.disconnect()
  }, [])

  const attachConnectionStateSubscription = useCallback((session: RtcSession) => {
    connectionStateSubscriptionRef.current?.close()
    const candidate = session as RtcSession & Partial<RtcSessionConnectionStateEvents>
    if (typeof candidate.subscribeConnectionState !== 'function') {
      connectionStateSubscriptionRef.current = null
      return
    }
    connectionStateSubscriptionRef.current = candidate.subscribeConnectionState((snapshot) => {
      updateFromPassiveConnectionState(snapshot, session)
    })
  }, [updateFromPassiveConnectionState])

  const releaseMachineSession = useCallback(() => {
    disconnectMachineSession()
    setConnectedSession(null)
    setConnectedTerminalId(null)
    setConnectingTerminalId(null)
  }, [disconnectMachineSession])

  const ensureMachineSession = useCallback(async (machineId: string, connectOptions?: RtcConnectOptions): Promise<RtcSession> => {
    const forceRelay = connectOptions?.forceRelay ?? forceRelayConnection
    const effectiveConnectOptions: RtcConnectOptions = { ...connectOptions, forceRelay }
    const reusable = machineSessionRef.current
    if (
      reusable &&
      reusable.connector === connector &&
      reusable.machineId === machineId &&
      reusable.retryToken === connectionRetryToken &&
      reusable.forceRelay === forceRelay
    ) {
      if (isRtcSessionAlive(reusable.session)) return reusable.session
      releaseMachineSession()
    }
    const pending = machineSessionPromiseRef.current
    if (
      pending &&
      pending.connector === connector &&
      pending.machineId === machineId &&
      pending.retryToken === connectionRetryToken &&
      pending.forceRelay === forceRelay
    ) {
      return pending.promise
    }
    const entry: {
      connector: MachineWorkspaceConnector
      machineId: string
      retryToken: number
      forceRelay: boolean
      promise: Promise<RtcSession>
    } = {
      connector,
      machineId,
      retryToken: connectionRetryToken,
      forceRelay,
      promise: Promise.resolve(null as unknown as RtcSession),
    }
    entry.promise = connector.connect({ machineId }, effectiveConnectOptions).then((session) => {
      if (machineSessionPromiseRef.current !== entry) {
        void session.disconnect()
        return session
      }
      machineSessionPromiseRef.current = null
      machineSessionRef.current = {
        connector,
        machineId,
        retryToken: connectionRetryToken,
        forceRelay,
        session,
      }
      attachConnectionStateSubscription(session)
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
  }, [attachConnectionStateSubscription, connector, connectionRetryToken, forceRelayConnection, releaseMachineSession])

  const withManagementApi = useCallback(async () => {
    if (!machine) throw new Error('machine is required before managing terminals')
    const session = await ensureMachineSession(machine.machineId, { forceRelay: forceRelayConnection })
    return {
      session,
      api: createTerminalManagementApi(session, machine.machineId),
    }
  }, [ensureMachineSession, forceRelayConnection, machine])

  const refreshTerminals = useCallback(async () => {
    const seq = terminalRefreshSeqRef.current + 1
    terminalRefreshSeqRef.current = seq
    try {
      const status = await api.getStatus()
      if (terminalRefreshSeqRef.current !== seq) return
      setMachineNetworkMachineId(status.machine.machineId)
      setMachine(status.machine)
      const forceRelay = forceRelayPreference(status.machine.machineId)
      setForceRelayConnection(forceRelay)
      const terminalList = await api.listTerminals({ forceRelay })
      if (terminalRefreshSeqRef.current !== seq) return
      setTerminals(terminalList)
      setError(null)
    } catch (err) {
      if (terminalRefreshSeqRef.current === seq) {
        const message = err instanceof Error ? err.message : String(err)
        setError(message)
        updateConnectionStatus(message, 'failed')
      }
    } finally {
      if (terminalRefreshSeqRef.current === seq) {
        setLoadingTerminals(false)
      }
    }
  }, [api, setMachineNetworkMachineId, updateConnectionStatus])

  useEffect(() => {
    let cancelled = false
    const seq = terminalRefreshSeqRef.current + 1
    terminalRefreshSeqRef.current = seq
    async function load() {
      setLoadingTerminals(true)
      let failed = false
      try {
        const status = await api.getStatus()
        if (cancelled || terminalRefreshSeqRef.current !== seq) return
        setMachineNetworkMachineId(status.machine.machineId)
        setMachine(status.machine)
        const forceRelay = forceRelayPreference(status.machine.machineId)
        setForceRelayConnection(forceRelay)
        const terminalList = await api.listTerminals({
          forceRelay,
          onStatus: (status) => {
            if (!cancelled && terminalRefreshSeqRef.current === seq) updateConnectionStatus(status)
          },
        })
        if (cancelled || terminalRefreshSeqRef.current !== seq) return
        setTerminals(terminalList)
      } catch (err) {
        if (!cancelled && terminalRefreshSeqRef.current === seq) {
          failed = true
          const message = err instanceof Error ? err.message : String(err)
          setError(message)
          updateConnectionStatus(message, 'failed')
        }
      } finally {
        if (!cancelled && terminalRefreshSeqRef.current === seq) {
          setLoadingTerminals(false)
          if (!failed) clearConnectionStatus()
        }
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [api, clearConnectionStatus, setMachineNetworkMachineId, updateConnectionStatus])

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
    if (!connectionStateEvents || !machine) return
    passiveConnectionPhaseRef.current = null
    const subscription = connectionStateEvents.subscribe(machine.machineId, (snapshot) => {
      const previousPhase = passiveConnectionPhaseRef.current
      passiveConnectionPhaseRef.current = snapshot.phase
      const activeSession = machineSessionRef.current?.session
      const recoveredFromInterruption = previousPhase !== null && previousPhase !== 'connected'
      if (
        snapshot.phase === 'connected' &&
        activeTerminalId &&
        (!activeSession || !isRtcSessionAlive(activeSession) || recoveredFromInterruption)
      ) {
        void ensureMachineSession(machine.machineId, { forceRelay: forceRelayConnection })
          .then((session) => {
            setConnectedSession(session)
            setConnectedTerminalId(activeTerminalId)
            setConnectingTerminalId(null)
            terminalRef.current?.reattach(session)
            splitTerminalRef.current?.reattach(session)
            updateFromPassiveConnectionState(snapshot, session)
          })
          .catch((err: unknown) => {
            const message = err instanceof Error ? err.message : String(err)
            updateConnectionStatus(message, 'failed')
          })
        return
      }
      updateFromPassiveConnectionState(snapshot, activeSession)
    })
    return () => {
      subscription.close()
    }
  }, [activeTerminalId, connectionStateEvents, ensureMachineSession, forceRelayConnection, machine, updateConnectionStatus, updateFromPassiveConnectionState])

  useEffect(() => {
    const machineId = machine?.machineId
    if (!subscribeRuntimeInventoryEvents || requireVerification || !machineId) return
    let cancelled = false
    void ensureMachineSession(machineId, { forceRelay: forceRelayConnection }).then((session) => {
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
  }, [connectionRetryToken, connector, ensureMachineSession, forceRelayConnection, machine?.machineId, refreshTerminals, requireVerification, subscribeRuntimeInventoryEvents])

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
    let showConnectionProgress = false
    let lastConnectionSnapshot: RtcConnectionStateSnapshot | null = null
    let lastConnectionStatus: string | null = null
    const updateFromConnectionStatusText = (status: string) => {
      updateFromConnectionState(connectionSnapshotFromStatus({
        machineId,
        statusText: status,
      }))
    }
    const progressTimer = window.setTimeout(() => {
      if (cancelled || machineSessionConnectSeqRef.current !== connectSeq) return
      showConnectionProgress = true
      if (lastConnectionSnapshot) {
        updateFromConnectionState(lastConnectionSnapshot)
        return
      }
      updateFromConnectionStatusText(lastConnectionStatus ?? (forceRelayConnection ? 'Connecting through relay...' : 'Connecting...'))
    }, TERMINAL_CONNECTION_PROGRESS_DELAY_MS)
    ensureMachineSession(machineId, {
      forceRelay: forceRelayConnection,
      onConnectionState: (snapshot) => {
        if (cancelled || machineSessionConnectSeqRef.current !== connectSeq) return
        lastConnectionSnapshot = snapshot
        if (showConnectionProgress || snapshot.phase === 'failed' || snapshot.phase === 'reconnecting' || snapshot.phase === 'waiting_network') {
          updateFromConnectionState(snapshot)
        }
      },
      onStatus: (status) => {
        if (cancelled || machineSessionConnectSeqRef.current !== connectSeq) return
        lastConnectionStatus = status
        const snapshot = connectionSnapshotFromStatus({ machineId, statusText: status })
        if (showConnectionProgress || snapshot.phase === 'failed' || snapshot.phase === 'reconnecting' || snapshot.phase === 'waiting_network') {
          updateFromConnectionState(snapshot)
        }
      },
    }).then((session) => {
      window.clearTimeout(progressTimer)
      if (cancelled || machineSessionConnectSeqRef.current !== connectSeq) return
      setConnectedSession(session)
      setConnectedTerminalId(activeTerminalId)
      setConnectingTerminalId(null)
      if (showConnectionProgress) {
        updateConnectionStatus('Connected', 'connected')
        clearConnectionStatusSoon()
      } else {
        clearConnectionStatus()
      }
    }).catch((err: unknown) => {
      window.clearTimeout(progressTimer)
      if (!cancelled && machineSessionConnectSeqRef.current === connectSeq) {
        const message = err instanceof Error ? err.message : String(err)
        setConnectedSession(null)
        setConnectedTerminalId(null)
        setConnectingTerminalId(null)
        updateConnectionStatus(message, 'failed')
        if (pair) setMobileSheet('pair')
      }
    })
    return () => {
      cancelled = true
      window.clearTimeout(progressTimer)
    }
  }, [activeTerminalId, clearConnectionStatus, clearConnectionStatusSoon, connector, connectionRetryToken, ensureMachineSession, forceRelayConnection, machine?.machineId, manualReconnectNonce, page, pair, releaseMachineSession, updateConnectionStatus, updateFromConnectionState])

  useEffect(() => {
    if (manualReconnectNonce === 0 || handledManualReconnectNonceRef.current === manualReconnectNonce) return
    const machineId = machine?.machineId
    if (!machineId || requireVerification) return
    handledManualReconnectNonceRef.current = manualReconnectNonce
    let cancelled = false
    void ensureMachineSession(machineId, {
      forceRelay: forceRelayConnection,
      onConnectionState: (snapshot) => {
        if (!cancelled) updateFromConnectionState(snapshot)
      },
      onStatus: (status) => {
        if (!cancelled) updateConnectionStatus(status, 'connecting')
      },
    }).then((session) => {
      if (cancelled) return
      setConnectedSession(session)
      if (page === 'terminal' && activeTerminalId) {
        setConnectedTerminalId(activeTerminalId)
        setConnectingTerminalId(null)
        terminalRef.current?.reattach(session)
        splitTerminalRef.current?.reattach(session)
      }
      updateConnectionStatus('Connected', 'connected')
      clearConnectionStatusSoon()
    }).catch((err: unknown) => {
      if (cancelled) return
      const message = err instanceof Error ? err.message : String(err)
      setConnectedSession(null)
      setConnectedTerminalId(null)
      setConnectingTerminalId(null)
      updateConnectionStatus(message, 'failed')
    })
    return () => {
      cancelled = true
    }
  }, [activeTerminalId, clearConnectionStatusSoon, ensureMachineSession, forceRelayConnection, machine?.machineId, manualReconnectNonce, page, requireVerification, updateConnectionStatus, updateFromConnectionState])

  useEffect(() => {
    const handleResume = () => {
      if (page !== 'terminal') return
      const session = machineSessionRef.current?.session ?? connectedSession
      if (!session || !isRtcSessionAlive(session)) return
      if (!activeTerminalId && !splitTerminalId) return

      setConnectedSession(session)
      if (activeTerminalId) {
        setConnectedTerminalId(activeTerminalId)
        setConnectingTerminalId(null)
        terminalRef.current?.reattach(session)
      }
      if (splitTerminalId) {
        splitTerminalRef.current?.reattach(session)
      }
    }
    document.addEventListener('termx:resume', handleResume)
    return () => {
      document.removeEventListener('termx:resume', handleResume)
    }
  }, [activeTerminalId, connectedSession, page, splitTerminalId])

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

  const retryConnection = useCallback((options: { forceRelay?: boolean; closeDialog?: boolean } = {}) => {
    const targetForceRelay = options.forceRelay ?? forceRelayConnection
    setForceRelayConnection(targetForceRelay)
    if (machineForceRelayKey) writeStoredForceRelay(machineForceRelayKey, targetForceRelay)
    if (options.closeDialog !== false) setConnectionInfoOpen(false)
    setConnectionInfo(null)
    setConnectionInfoError(null)
    updateConnectionStatus(targetForceRelay ? 'Reconnecting through relay...' : 'Reconnecting...', 'reconnecting')
    if (connector.reconnect) {
      const current = machineSessionRef.current
      connector.reconnect({ forceRelay: targetForceRelay })
      machineSessionConnectSeqRef.current += 1
      connectionStateSubscriptionRef.current?.close()
      connectionStateSubscriptionRef.current = null
      machineSessionPromiseRef.current = null
      machineSessionRef.current = null
      const runtimeInventorySubscription = runtimeInventorySubscriptionRef.current
      runtimeInventorySubscriptionRef.current = null
      runtimeInventorySubscription?.subscription.close()
      void current?.session.disconnect()
      setConnectedSession(null)
      setConnectedTerminalId(null)
      setConnectingTerminalId(activeTerminalId)
    } else {
      releaseMachineSession()
      setConnectedSession(null)
      setConnectedTerminalId(null)
      setConnectingTerminalId(activeTerminalId)
    }
    setManualReconnectNonce((value) => value + 1)
    setConnectionRetryToken((value) => value + 1)
  }, [activeTerminalId, connector, forceRelayConnection, machineForceRelayKey, releaseMachineSession, updateConnectionStatus])

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
    if (!machine) {
      setError('No machine is available for file access')
      return
    }
    const fileTerminal = page === 'terminal' ? activeToolTerminal : (activeToolTerminal ?? terminals[0] ?? null)
    const fallbackPath = fileTerminal?.cwd || '/'
    const resolveTerminalDirectory = page === 'terminal' && Boolean(fileTerminal)
    const contextScope = page === 'terminal' ? 'terminal' : 'list'
    const nextContextKey = `${contextScope}:${fileTerminal?.terminalId ?? 'machine'}:${fallbackPath}`
    setFileTerminalId(fileTerminal?.terminalId ?? null)
    setFileInitialPath(fallbackPath)
    if (!filesOpen && fileContextKey !== nextContextKey) setFileContextKey(nextContextKey)
    void ensureMachineSession(machine.machineId, {
      forceRelay: forceRelayConnection,
      onConnectionState: updateFromConnectionState,
      onStatus: (status) => updateConnectionStatus(status),
    })
      .then(async (session) => {
        if (!resolveTerminalDirectory || !fileTerminal) return
        try {
          const directory = await createTerminalManagementApi(session, machine.machineId)
            .getTerminalDirectory(fileTerminal.terminalId)
          const livePath = normalizeTerminalDirectory(directory.path)
          if (livePath) {
            setFileInitialPath(livePath)
            const liveContextKey = `${contextScope}:${fileTerminal.terminalId}:${livePath}`
            if (!filesOpen && fileContextKey !== liveContextKey) setFileContextKey(liveContextKey)
          }
        } catch {
          setFileInitialPath(fallbackPath)
          if (!filesOpen && fileContextKey !== nextContextKey) setFileContextKey(nextContextKey)
        }
      })
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : String(err)
        updateConnectionStatus(message, 'failed')
        setFilesOpen(false)
        setFileTerminalId(null)
        if (pair) setMobileSheet('pair')
      })
    setFilesOpen(true)
    setMobileSheet(null)
  }, [activeToolTerminal, ensureMachineSession, fileContextKey, filesOpen, forceRelayConnection, machine, page, pair, requireVerification, terminals, updateConnectionStatus, updateFromConnectionState])

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
      command: '',
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
      command: managedTerminal.command ?? '',
      cwd: managedTerminal.cwd ?? '',
      environment: managedTerminal.environment ?? '',
      sizeLockMode: managedTerminal.sizeLockMode ?? 'off',
    })
    setMobileSheet('edit-terminal')
  }, [managedTerminal])

  const submitCreateTerminal = useCallback(async () => {
    if (!canManageTerminals) return
    const command = terminalForm.command.trim().split(/\s+/).filter(Boolean)
    const input: LocalCreateTerminalInput = {
      name: terminalForm.name.trim() || undefined,
      ...(command.length > 0 ? { command } : {}),
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
      const message = err instanceof Error ? err.message : String(err)
      updateConnectionStatus(message, 'failed')
    } finally {
      setUnlockingResize(false)
    }
  }, [activeTerminalId, canManageTerminals, refreshTerminals, updateConnectionStatus, withManagementApi])

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

  const openConnectionInfo = useCallback(() => {
    const existingSession = connectedSession ?? machineSessionRef.current?.session ?? null
    if (!existingSession && !machine) {
      setConnectionInfoError('No machine connection is available')
      setConnectionInfo(null)
      setConnectionInfoOpen(true)
      return
    }
    setConnectionInfoOpen(true)
    setConnectionInfoLoading(true)
    setConnectionInfoError(null)
    const sessionPromise = existingSession
      ? Promise.resolve(existingSession)
      : ensureMachineSession(machine!.machineId, { forceRelay: forceRelayConnection })
    sessionPromise.then((session) => session.getConnectionInfo()).then((info) => {
      setConnectionInfo(info)
    }).catch((err: unknown) => {
      setConnectionInfoError(err instanceof Error ? err.message : String(err))
    }).finally(() => {
      setConnectionInfoLoading(false)
    })
  }, [connectedSession, ensureMachineSession, forceRelayConnection, machine])

  const toggleConnectionMode = useCallback(() => {
    retryConnection({ forceRelay: !forceRelayConnection })
  }, [forceRelayConnection, retryConnection])

  const lockKeyboard = useCallback(() => {
    setKeyboardLocked((prev) => {
      const next = !prev
      keyboardLockedRef.current = next
      if (next) {
        terminalRef.current?.blur()
        splitTerminalRef.current?.blur()
      } else {
        terminalRef.current?.focus()
      }
      return next
    })
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

  useEffect(() => addNativeBackHandler(() => {
    if (pasteConfirmText) {
      setPasteConfirmText('')
      return true
    }
    if (connectionInfoOpen) {
      setConnectionInfoOpen(false)
      return true
    }
    if (filesOpen) {
      openTerminalPanel()
      return true
    }
    if (mobileSheet) {
      setMobileSheet(null)
      return true
    }
    if (terminalFnOpen) {
      setTerminalFnOpen(false)
      return true
    }
    if (terminalToolbarOpen) {
      setTerminalToolbarOpen(false)
      setTerminalToolbarModeAndReset('default')
      return true
    }
    if (splitTerminalId) {
      closeSplitTerminal()
      return true
    }
    if (page === 'terminal') {
      showTerminalListPage()
      return true
    }
    return false
  }, 20), [
    closeSplitTerminal,
    connectionInfoOpen,
    filesOpen,
    mobileSheet,
    openTerminalPanel,
    page,
    pasteConfirmText,
    setTerminalToolbarModeAndReset,
    showTerminalListPage,
    splitTerminalId,
    terminalFnOpen,
    terminalToolbarOpen,
  ])

  const renderTerminalListPage = () => {
    if (!machine) return null

    return (
      <main className="relative flex min-h-0 flex-1 flex-col bg-zinc-50" data-testid="termx-terminal-list-page">
        <header className="flex min-h-12 shrink-0 items-center justify-between border-b border-zinc-200 bg-white px-4 pt-[env(safe-area-inset-top)]">
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
              aria-label="Connection info"
              className="flex h-9 w-9 items-center justify-center rounded-md text-zinc-600 active:bg-zinc-100"
              onClick={openConnectionInfo}
            >
              <Info className="h-5 w-5" />
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
        {error && !showMachineNetworkOverlay ? (
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
          className="min-h-0 flex-1 overflow-y-auto p-3"
          data-testid="termx-terminal-list-scroll"
        >
          <h2 className="mb-2 px-1 text-xs font-semibold uppercase tracking-wider text-zinc-500">Terminals</h2>
          {loadingTerminals ? (
            <div className="flex flex-col items-center justify-center p-8 text-sm text-zinc-500">
              <div className="mb-3 h-6 w-6 animate-spin rounded-full border-2 border-zinc-300 border-t-zinc-600"></div>
              <p>Loading terminals...</p>
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
            <PairDevicePanel
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
  }, [activeTerminalId, connectedSession])

  useEffect(() => () => {
    disconnectMachineSession()
  }, [disconnectMachineSession])

  if (error && !machine) {
    return (
      <div className={`flex h-full min-h-0 items-center justify-center bg-zinc-50 p-4 ${className || ''}`}>
        <div className="w-full max-w-md rounded-lg border border-red-200 bg-white p-4 text-sm text-red-700 shadow-sm" role="alert">
          <h2 className="mb-2 font-semibold text-red-900">Connection Error</h2>
          <p>{error}</p>
        </div>
      </div>
    )
  }

  if (!machine) {
    return (
      <div className={`flex h-full min-h-0 items-center justify-center bg-zinc-50 ${className || ''}`}>
        <div className="flex items-center gap-2 text-sm text-zinc-500">
          <div className="h-4 w-4 animate-spin rounded-full border-2 border-zinc-300 border-t-zinc-600"></div>
          Connecting to TermX...
        </div>
      </div>
    )
  }

  return (
    <div
      ref={outerContainerRef}
      className={`relative flex h-full min-h-0 w-full max-w-full flex-col overflow-hidden bg-[var(--termx-bg)] font-sans text-[var(--termx-text)] md:flex-row ${className || ''}`}
      data-machine-id={machine.machineId}
      style={terminalThemeStyle}
    >
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
              <p>Loading terminals...</p>
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
      <main
        className="relative grid min-h-0 min-w-0 max-w-full flex-1 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden bg-[var(--termx-terminal-bg)]"
        data-testid="termx-terminal-page"
      >
        <header
          className="relative z-30 flex min-h-10 min-w-0 max-w-full shrink-0 items-center justify-between gap-1 overflow-hidden border-b border-[var(--termx-border-subtle)] bg-[var(--termx-surface)] px-1.5 pt-[env(safe-area-inset-top)] md:hidden"
          data-testid="termx-terminal-header"
        >
          <div className="flex min-w-0 flex-1 items-center gap-1">
            <button
              type="button"
              aria-label="Back to terminal list"
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-[var(--termx-muted)] transition-colors active:bg-[var(--termx-surface-raised)]"
              onClick={() => { haptic(); showTerminalListPage() }}
            >
              <ChevronLeft className="h-5 w-5" />
            </button>
            <button
              type="button"
              aria-label="Switch terminal"
              className="flex min-w-0 flex-1 flex-col items-start justify-center rounded-md px-1.5 py-0.5 text-left transition-colors active:bg-[var(--termx-surface-raised)]"
              onClick={() => { haptic(); setMobileSheet('terminals') }}
            >
              <span className="max-w-full truncate text-[9px] font-bold uppercase tracking-wider text-[var(--termx-muted)]">{machine.name}</span>
              <span className="max-w-full truncate text-[12px] font-semibold leading-tight text-[var(--termx-text)]" data-testid="termx-terminal-title">{terminalHeaderTitle}</span>
              {terminalHeaderDirectory ? (
                <span className="max-w-full truncate text-[10px] font-medium leading-tight text-[var(--termx-muted)]">{terminalHeaderDirectory}</span>
              ) : null}
            </button>
          </div>

          <div className="flex shrink-0 items-center gap-0.5">
            <button
              type="button"
              aria-label="Split terminal"
              aria-pressed={Boolean(splitTerminalId)}
              onClick={() => { haptic(); openSplitTerminalSheet() }}
              className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md transition-colors active:scale-95 ${splitTerminalId ? 'bg-[var(--termx-accent)] text-[var(--termx-accent-text)]' : 'text-[var(--termx-muted)] active:bg-[var(--termx-surface-raised)]'}`}
            >
              <Rows2 className="h-4 w-4" />
            </button>
            {splitTerminalId ? (
              <>
                <button
                  type="button"
                  aria-label={syncSplitInput ? 'Disable synchronized input' : 'Enable synchronized input'}
                  aria-pressed={syncSplitInput}
                  onClick={() => { haptic(); setSyncSplitInput((current) => !current) }}
                  className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md transition-colors active:scale-95 ${syncSplitInput ? 'bg-[var(--termx-accent)] text-[var(--termx-accent-text)]' : 'text-[var(--termx-muted)] active:bg-[var(--termx-surface-raised)]'}`}
                >
                  {syncSplitInput ? <Link2 className="h-4 w-4" /> : <Link2Off className="h-4 w-4" />}
                </button>
                <button
                  type="button"
                  aria-label="Close split terminal"
                  onClick={() => { haptic(); closeSplitTerminal() }}
                  className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-[var(--termx-muted)] transition-colors active:scale-95 active:bg-[var(--termx-surface-raised)]"
                >
                  <PanelBottomClose className="h-4 w-4" />
                </button>
              </>
            ) : null}
            <button
              type="button"
              aria-label="Terminal tools"
              onClick={() => {
                haptic()
                setTerminalToolbarOpen((current) => {
                  const next = !current
                  if (next) setTerminalFnOpen(false)
                  if (!next) setTerminalToolbarModeAndReset('default')
                  return next
                })
              }}
              className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md transition-colors active:scale-95 ${terminalToolbarOpen ? 'bg-[var(--termx-accent)] text-[var(--termx-accent-text)]' : 'text-[var(--termx-muted)] active:bg-[var(--termx-surface-raised)]'}`}
            >
              <span className="text-[13px] font-bold leading-none">•••</span>
            </button>
            <button
              type="button"
              aria-label="Connection info"
              onClick={() => { haptic(); openConnectionInfo() }}
              className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md transition-colors active:scale-95 ${connectionInfoOpen ? 'bg-[var(--termx-accent)] text-[var(--termx-accent-text)]' : 'text-[var(--termx-muted)] active:bg-[var(--termx-surface-raised)]'}`}
            >
              <Info className="h-4 w-4" />
            </button>
            <button
              type="button"
              aria-label="Open files"
              onClick={() => { haptic(); openFiles() }}
              className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md transition-colors active:scale-95 ${filesOpen ? 'bg-[var(--termx-accent)] text-[var(--termx-accent-text)]' : 'text-[var(--termx-muted)] active:bg-[var(--termx-surface-raised)]'}`}
            >
              <Folder className="h-4 w-4" />
            </button>
          </div>
        </header>

        <div
          ref={terminalAreaRef}
          className="relative min-h-0 min-w-0 flex-1 overflow-hidden bg-[var(--termx-terminal-bg)]"
          data-testid="termx-terminal-body"
        >
          {terminalToolbarOpen ? (
            <TerminalActionToolbar
              mode={terminalToolbarMode}
              hasSelection={hasTerminalSelection}
              renderer={effectiveTerminalSettings.renderer}
              fontSize={effectiveTerminalSettings.fontSize}
              onModeChange={setTerminalToolbarModeAndReset}
              onClose={() => setTerminalToolbarOpen(false)}
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
              onRendererChange={(renderer) => updateTerminalSettings({ renderer })}
              onFontSizeChange={(fontSize) => updateTerminalSettings({ fontSize })}
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

          {error && !showMachineNetworkOverlay ? (
            <div className="absolute inset-x-0 top-0 z-40 border-y border-red-500/30 bg-red-950/90 px-3 py-2 text-[12px] font-medium text-red-100 shadow-lg backdrop-blur md:top-3" role="alert">
              {error}
            </div>
          ) : null}

          <div ref={terminalWrapperRef} className={`absolute inset-0 flex flex-col bg-[var(--termx-terminal-bg)] md:bg-[var(--termx-bg)] ${splitTerminalId ? 'gap-px md:gap-3 md:p-3' : ''}`}>
            <div
              className={`relative min-h-0 flex-1 bg-[var(--termx-terminal-bg)] ${splitTerminalId ? `border-b border-[var(--termx-border-subtle)] md:rounded-xl md:border md:shadow-2xl md:overflow-hidden ${activeTerminalSlot === 0 ? 'ring-1 ring-inset ring-[var(--termx-accent)]' : ''}` : 'md:m-3 md:rounded-2xl md:border md:border-[var(--termx-border-subtle)] md:shadow-2xl md:overflow-hidden'}`}
              data-active-slot={activeTerminalSlot === 0 ? 'true' : 'false'}
              data-testid="termx-terminal-panel"
              onPointerDown={() => { haptic(); setActiveTerminalSlot(0); activeTerminalSlotRef.current = 0 }}
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
                  onCursorMove={handleCursorMove}
                  onInput={sendTerminalInput}
                  onBufferChange={handleBufferChange}
                  onResizeControl={setTerminalResizeControl}
                  selectionMode={terminalToolbarOpen && terminalToolbarMode === 'selection' && activeTerminalSlot === 0}
                  settings={effectiveTerminalSettings}
                  preventFocus={keyboardLocked}
                  suppressConnectingOverlay={showMachineNetworkOverlay}
                />
              ) : (
                <div className="flex h-full items-center justify-center text-sm text-[var(--termx-muted)]">
                  {showMachineNetworkOverlay ? null : activeTerminalId && connectingTerminalId === activeTerminalId ? (connectionStatus ?? 'Connecting terminal...') : 'No active terminal'}
                </div>
              )}
              {activeTerminalId && connectedSession && connectedTerminalId === activeTerminalId && activeTerminalResizeLocked ? (
                <button
                  type="button"
                  aria-label="Unlock terminal resize"
                  className={`absolute right-2 z-20 flex min-h-7 items-center gap-1.5 rounded-md border border-[var(--termx-border-subtle)] bg-[var(--termx-overlay)] px-2 text-[11px] font-semibold text-[var(--termx-text)] shadow-lg backdrop-blur active:opacity-85 disabled:opacity-60 ${splitTerminalId ? 'top-16' : 'top-2'}`}
                  disabled={unlockingResize}
                  onClick={() => { haptic(); void unlockTerminalResize() }}
                >
                  <Unlock className="h-3.5 w-3.5" />
                  {unlockingResize ? 'Unlocking...' : 'Unlock resize'}
                </button>
              ) : null}
            </div>

            {splitTerminalId ? (
              <div
                className={`relative min-h-0 flex-1 bg-[var(--termx-terminal-bg)] md:rounded-xl md:border md:border-[var(--termx-border-subtle)] md:shadow-2xl md:overflow-hidden ${activeTerminalSlot === 1 ? 'ring-1 ring-inset ring-[var(--termx-accent)]' : ''}`}
                data-active-slot={activeTerminalSlot === 1 ? 'true' : 'false'}
                data-testid="termx-split-terminal-panel"
                onPointerDown={() => { haptic(); setActiveTerminalSlot(1); activeTerminalSlotRef.current = 1 }}
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
                    onCursorMove={handleCursorMove}
                    onInput={sendTerminalInput}
                    onBufferChange={handleBufferChange}
                    selectionMode={terminalToolbarOpen && terminalToolbarMode === 'selection' && activeTerminalSlot === 1}
                    settings={effectiveTerminalSettings}
                    preventFocus={keyboardLocked}
                    suppressConnectingOverlay={showMachineNetworkOverlay}
                  />
                ) : (
                  <div className="absolute inset-0 flex items-center justify-center text-sm text-[var(--termx-muted)]">
                    {showMachineNetworkOverlay ? null : connectionStatus ?? 'Connecting terminal...'}
                  </div>
                )}
              </div>
            ) : null}
          </div>
        </div>

        <MobileTerminalKeybar
          ref={mobileKeybarRef}
          className="relative z-20 w-full max-w-full"
          onInput={sendTerminalInput}
          onFocusKeyboard={focusActiveTerminal}
          onBlurKeyboard={() => activeTerminalHandle()?.blur()}
          onLockKeyboard={lockKeyboard}
          fnOpen={terminalFnOpen}
          onToggleFn={() => {
            setTerminalFnOpen((current) => !current)
            setTerminalToolbarOpen(false)
          }}
          modifierState={modifierState}
          onModifierStateChange={setModifierState}
          keyboardVisible={keyboardVisible}
          keyboardLocked={keyboardLocked}
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
            <PairDevicePanel
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
        className={`absolute inset-0 z-30 flex flex-col bg-white shadow-[0_-20px_40px_rgba(0,0,0,0.15)] transition-all duration-300 md:m-6 md:rounded-2xl md:border md:border-zinc-200/60 ${filesOpen ? 'translate-y-0 visible' : 'translate-y-full invisible'}`}
        data-testid="termx-machine-files-overlay"
      >
        <div className="flex shrink-0 items-center justify-between border-b border-zinc-200/70 bg-white px-4 pb-2 pt-[calc(env(safe-area-inset-top)+0.5rem)] md:h-14 md:pb-0 md:pt-0">
          <div className="flex items-center gap-2">
            <Folder className="h-5 w-5 text-zinc-500" />
            <span className="text-[17px] font-bold tracking-tight text-zinc-900">Files</span>
          </div>
          <button
            type="button"
            aria-label="Close files"
            className="flex h-9 w-9 items-center justify-center rounded-lg text-zinc-500 transition-colors active:scale-95 active:bg-zinc-100"
            onClick={openTerminalPanel}
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        {connectedSession ? (
          <FileManager
            key={fileContextKey}
            machineId={machine.machineId}
            terminalId={fileTerminalId ?? undefined}
            session={connectedSession}
            initialPath={fileInitialPath}
            className="flex h-full min-h-0 flex-col relative"
            active={filesOpen}
            fileTransfer={fileTransfer}
            onOpenTransferCenter={() => setTransferCenterOpen(true)}
          />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-zinc-500">
            {showMachineNetworkOverlay ? null : filesOpen ? 'Connecting...' : 'File access is not ready'}
          </div>
        )}
      </div>

      <div className={`pointer-events-none absolute bottom-8 left-1/2 z-50 flex -translate-x-1/2 transform flex-col items-center gap-2 transition-all duration-300 ${pairStatus ? 'translate-y-0 opacity-100' : 'translate-y-4 opacity-0'}`}>
        <div className="flex items-center gap-2 rounded-full bg-zinc-900/95 px-4 py-2.5 text-sm font-medium text-white shadow-[0_8px_30px_rgb(0,0,0,0.12)] backdrop-blur-md ring-1 ring-white/10" role="status" aria-live="polite">
          {pairStatus}
        </div>
      </div>
      {connectionInfoOpen ? (
        <ConnectionInfoDialog
          info={connectionInfo}
          loading={connectionInfoLoading}
          error={connectionInfoError}
          forceRelayActive={forceRelayConnection}
          onClose={() => setConnectionInfoOpen(false)}
          onRefresh={openConnectionInfo}
          onReconnect={() => retryConnection()}
          onToggleMode={toggleConnectionMode}
        />
      ) : null}
      {showMachineNetworkOverlay ? (
        <MachineNetworkStatusOverlay
          phase={connectionPhase}
          status={connectionStatus}
        />
      ) : null}
      {fileTransfer && transferCenterOpen ? (
        <FileTransferPanel
          transfers={transferState.transfers}
          hasActiveTransfers={transferState.hasActiveTransfers}
          onCancel={(id) => fileTransfer.cancelTransfer(id)}
          onDismiss={(id) => fileTransfer.dismissTransfer(id)}
          onPause={(id) => fileTransfer.pauseTransfer?.(id)}
          onResume={(id) => fileTransfer.resumeTransfer?.(id)}
          onResumeAll={() => fileTransfer.resumeAllTransfers?.(machine.machineId)}
          open
          onOpenChange={(open) => {
            if (!open) setTransferCenterOpen(false)
          }}
        />
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

function ConnectionInfoDialog({
  info,
  loading,
  error,
  forceRelayActive,
  onClose,
  onRefresh,
  onReconnect,
  onToggleMode,
}: {
  info: ConnectionInfo | null
  loading: boolean
  error: string | null
  forceRelayActive: boolean
  onClose: () => void
  onRefresh: () => void
  onReconnect: () => void
  onToggleMode: () => void
}) {
  const type = info?.type ?? (info?.relayInUse ? 'relay' : 'unknown')
  const isP2P = type === 'p2p'
  const canToggleMode = forceRelayActive || isP2P
  const modeActionLabel = forceRelayActive ? 'Try P2P' : 'Use relay'
  return (
    <div className="absolute inset-0 z-50 flex items-center justify-center bg-black/45 p-4 backdrop-blur-sm" role="dialog" aria-modal="true" onClick={onClose}>
      <section className="w-full max-w-md overflow-hidden rounded-lg border border-zinc-200 bg-white shadow-2xl" onClick={(event) => event.stopPropagation()}>
        <header className="flex items-center justify-between gap-3 border-b border-zinc-200 px-4 py-3">
          <div className="min-w-0">
            <h2 className="text-[15px] font-semibold text-zinc-950">Connection Info</h2>
            <p className="mt-0.5 text-[12px] font-medium text-zinc-500">{connectionTypeLabel(type)}</p>
          </div>
          <button type="button" aria-label="Close connection info" className="flex h-9 w-9 items-center justify-center rounded-md text-zinc-500 active:bg-zinc-100" onClick={onClose}>
            <X className="h-5 w-5" />
          </button>
        </header>

        <div className="space-y-3 px-4 py-4">
          {error ? (
            <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] font-medium text-amber-800">{error}</div>
          ) : null}
          <div className="grid grid-cols-1 gap-2">
            <ConnectionInfoRow label="Mode" value={loading ? 'Reading stats...' : connectionTypeLabel(type)} strong />
            <ConnectionInfoRow label="Path" value={info?.path ?? '-'} />
            <ConnectionInfoRow label="Local" value={info?.localAddr ?? '-'} />
            <ConnectionInfoRow label="Remote" value={info?.remoteAddr ?? '-'} />
            <ConnectionInfoRow label="Candidates" value={candidateTypeText(info)} />
            <ConnectionInfoRow label="RTT" value={info?.rtt !== undefined ? `${Math.round(info.rtt)} ms` : '-'} />
            <ConnectionInfoRow label="Connection" value={info?.connectionId ?? '-'} />
          </div>
        </div>

        <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-zinc-200 px-4 py-3">
          <button type="button" className="flex min-h-9 items-center justify-center rounded-md border border-zinc-200 bg-white px-3 text-[13px] font-semibold text-zinc-700 active:bg-zinc-100" onClick={onRefresh}>
            Refresh
          </button>
          <button type="button" className="flex min-h-9 items-center justify-center rounded-md border border-zinc-200 bg-white px-3 text-[13px] font-semibold text-zinc-700 active:bg-zinc-100" onClick={onReconnect}>
            Reconnect
          </button>
          <button
            type="button"
            className="flex min-h-9 items-center justify-center rounded-md bg-zinc-900 px-3 text-[13px] font-semibold text-white disabled:bg-zinc-300 disabled:text-zinc-500"
            disabled={loading || !canToggleMode}
            onClick={onToggleMode}
          >
            {modeActionLabel}
          </button>
        </footer>
      </section>
    </div>
  )
}

function ConnectionInfoRow({ label, value, strong = false }: { label: string; value: string; strong?: boolean | undefined }) {
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-start gap-3 rounded-md bg-zinc-50 px-3 py-2">
      <dt className="text-[12px] font-semibold text-zinc-500">{label}</dt>
      <dd className={`min-w-0 break-words text-[12px] ${strong ? 'font-semibold text-zinc-950' : 'font-medium text-zinc-700'}`}>{value}</dd>
    </div>
  )
}

function connectionTypeLabel(type: ConnectionInfo['type']): string {
  if (type === 'p2p') return 'P2P direct'
  if (type === 'relay') return 'Relay'
  return 'Unknown'
}

function candidateTypeText(info: ConnectionInfo | null): string {
  const local = info?.candidateType ?? '-'
  const remote = info?.remoteCandidateType ?? '-'
  return `${local} / ${remote}`
}

function isRtcSessionAlive(session: RtcSession): boolean {
  const candidate = session as RtcSession & Partial<RtcSessionLiveness>
  if (typeof candidate.isAlive !== 'function') return true
  return candidate.isAlive()
}

function isTransientConnectionPhase(phase: RtcConnectionStateSnapshot['phase']): boolean {
  return phase === 'probing' || phase === 'connecting'
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

function forceRelayStorageKey(machineId: string): string {
  return `termx.forceRelay.${machineId}`
}

function forceRelayPreference(machineId: string): boolean {
  return readStoredForceRelay(forceRelayStorageKey(machineId))
}

function readStoredForceRelay(key: string): boolean {
  try {
    return localStorage.getItem(key) === '1'
  } catch {
    return false
  }
}

function writeStoredForceRelay(key: string, value: boolean): void {
  try {
    if (value) {
      localStorage.setItem(key, '1')
    } else {
      localStorage.removeItem(key)
    }
  } catch {
    // Storage can be unavailable in restricted WebViews.
  }
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
