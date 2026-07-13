import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties, type ReactNode } from 'react'
import { Bookmark, BookmarkMinus, BookmarkPlus, ChevronLeft, ClipboardList, Folder, FolderOpen, Info, KeyRound, Link2, Link2Off, Monitor, MoreHorizontal, PanelBottomClose, Plus, RefreshCw, Rows2, SlidersHorizontal, SquarePen, Trash2, Unlock, X } from 'lucide-react'
import { connectionPhaseLabel, connectionSnapshotFromStatus } from '../connection/connectionState'
import { FileTransferPanel } from '../files/FileTransferPanel'
import { FileManager } from '../files/FileManager'
import { createFileApi, type FileEntry } from '../files/fileApi'
import { joinPath, normalizeFilePath, parentPath } from '../files/fileUtils'
import { createPathBookmarkApi, type PathBookmark } from '../files/pathBookmarks'
import { hapticImpact, hapticSelection } from '../platform/haptics'
import { PairDevicePanel } from '../pairing/PairDevicePanel'
import type { MachineSessionStore } from '../state/localAppIdentity'
import { MachineNetworkStatusOverlay } from '../machine-runtime/MachineNetworkStatusOverlay'
import { useMachineNetworkStatus } from '../machine-runtime/useMachineNetworkStatus'
import { createRemoteClipboardApi, type RemoteClipboardEntry } from '../clipboard/clipboardApi'
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
import type { ConnectionInfo, LocalAgentApi, LocalCreateTerminalInput, LocalPairingApi, LocalUpdateTerminalInput, MachineConnectionStateEvents, RtcConnectOptions, RtcConnectionStateSnapshot, RtcConnector, RtcEvent, RtcSession, RtcSessionConnectionStateEvents, RtcSessionLiveness, RtcSubscription, RtcTerminalDataChannelController, TerminalInventoryEvents } from '../core/transport'
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
  initialMachine?: Machine | undefined
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
  onNeedsReauthorization?: ((machineId: string) => void) | undefined
  onTerminalSettingsChange?: ((patch: Partial<TerminalSettings>) => void) | undefined
}

type TerminalEditorSheet = 'create-terminal' | 'edit-terminal'
type MobileSheet = 'terminals' | 'terminal-menu' | 'split-terminal' | 'pair' | 'manage-terminal' | TerminalEditorSheet | 'terminal-path-picker' | 'terminal-path-bookmarks' | 'clipboard-history' | null
type AppPage = 'terminal-list' | 'terminal'
type TerminalSlot = 0 | 1
const TERMINAL_CONNECTION_PROGRESS_DELAY_MS = 450
const AUTH_CONNECTION_MESSAGE = 'This phone needs to re-authorize this machine. Scan the machine QR again.'
const machineWorkspaceInventoryCache = new WeakMap<MachineWorkspaceConnector, Map<string, {
  machine: Machine
  terminals: RemoteTerminal[]
}>>()

function noopSubscribe(_listener: () => void): () => void { return () => {} }

function inventoryCacheForConnector(connector: MachineWorkspaceConnector): Map<string, {
  machine: Machine
  terminals: RemoteTerminal[]
}> {
  const existing = machineWorkspaceInventoryCache.get(connector)
  if (existing) return existing
  const created = new Map<string, {
    machine: Machine
    terminals: RemoteTerminal[]
  }>()
  machineWorkspaceInventoryCache.set(connector, created)
  return created
}

export function MachineWorkspace({ api, connector, className, initialMachine, inventoryEvents, connectionStateEvents, subscribeRuntimeInventoryEvents = false, pair, onBack, fileTransfer, terminalSettings: terminalSettingsProp, onNeedsReauthorization, onTerminalSettingsChange }: MachineWorkspaceProps) {
  const initialInventory = initialMachine ? inventoryCacheForConnector(connector).get(initialMachine.machineId) : undefined
  const [machine, setMachine] = useState<Machine | null>(() => initialInventory?.machine ?? initialMachine ?? null)
  const [terminals, setTerminals] = useState<RemoteTerminal[]>(() => initialInventory?.terminals ?? [])
  const [hasLoadedTerminals, setHasLoadedTerminals] = useState(() => Boolean(initialInventory))
  const [loadingTerminals, setLoadingTerminals] = useState(() => !initialInventory)
  const [activeTerminalId, setActiveTerminalId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pairStatus, setPairStatus] = useState<string | null>(null)
  const [verifiedDevice, setVerifiedDevice] = useState<boolean | null>(() => {
    if (!pair) return true
    const machineId = initialInventory?.machine.machineId ?? initialMachine?.machineId
    if (!machineId) return null
    return Boolean(pair.sessionStore.getSessionToken(machineId))
  })
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
  const [selectedTerminalId, setSelectedTerminalId] = useState<string | null>(null)
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
  const [terminalPathReturnSheet, setTerminalPathReturnSheet] = useState<TerminalEditorSheet>('create-terminal')
  const [terminalPathPickerPath, setTerminalPathPickerPath] = useState('/')
  const [terminalPathPickerEntries, setTerminalPathPickerEntries] = useState<FileEntry[]>([])
  const [terminalPathPickerLoading, setTerminalPathPickerLoading] = useState(false)
  const [terminalPathPickerError, setTerminalPathPickerError] = useState<string | null>(null)
  const [terminalPathBookmarks, setTerminalPathBookmarks] = useState<PathBookmark[]>([])
  const [terminalPathBookmarksLoading, setTerminalPathBookmarksLoading] = useState(false)
  const [terminalPathBookmarksError, setTerminalPathBookmarksError] = useState<string | null>(null)
  const [clipboardEntries, setClipboardEntries] = useState<RemoteClipboardEntry[]>([])
  const [clipboardLoading, setClipboardLoading] = useState(false)
  const [clipboardError, setClipboardError] = useState<string | null>(null)
  const [clipboardDraft, setClipboardDraft] = useState('')
  const [editingClipboardId, setEditingClipboardId] = useState<string | null>(null)
  const [modifierState, setModifierState] = useState<TerminalModifierState>({ ctrl: 'off', alt: 'off' })
  const [terminalToolbarOpen, setTerminalToolbarOpen] = useState(false)
  const [terminalToolbarMode, setTerminalToolbarMode] = useState<TerminalToolbarMode>('default')
  const [terminalFnOpen, setTerminalFnOpen] = useState(false)
  const [hasTerminalSelection, setHasTerminalSelection] = useState(false)
  const [pasteConfirmText, setPasteConfirmText] = useState('')
  const [splitTerminalId, setSplitTerminalId] = useState<string | null>(null)
  const [activeTerminalSlot, setActiveTerminalSlot] = useState<TerminalSlot>(0)
  const [syncSplitInput, setSyncSplitInput] = useState(false)
  const [terminalBufferBySlot, setTerminalBufferBySlot] = useState<Record<TerminalSlot, 'normal' | 'alternate'>>({
    0: 'normal',
    1: 'normal',
  })
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
  const sessionConnectionPhaseRef = useRef<RtcConnectionStateSnapshot['phase'] | null>(null)
  const latestActiveTerminalIdRef = useRef<string | null>(null)
  const latestSplitTerminalIdRef = useRef<string | null>(null)
  const handledManualReconnectNonceRef = useRef(0)
  const resizeLockedHintShownRef = useRef(false)
  const hasLoadedTerminalsRef = useRef(hasLoadedTerminals)
  const activeTerminal = terminals.find((terminal) => terminal.terminalId === activeTerminalId)
  const splitTerminal = terminals.find((terminal) => terminal.terminalId === splitTerminalId)
  const activeToolTerminal = activeTerminalSlot === 1 && splitTerminal ? splitTerminal : activeTerminal
  const selectedTerminal = terminals.find((terminal) => terminal.terminalId === selectedTerminalId)
  const activeTerminalTitle = activeTerminal?.title || activeTerminal?.command || activeTerminalId || 'Terminal'
  const splitTerminalTitle = splitTerminal?.title || splitTerminal?.command || splitTerminalId || 'Terminal'
  const terminalHeaderTitle = splitTerminalId ? `${activeTerminalTitle} / ${splitTerminalTitle}` : activeTerminalTitle
  const terminalHeaderDirectory = activeToolTerminal?.cwd || activeTerminal?.cwd || splitTerminal?.cwd || ''
  const activeTerminalResizeLocked = terminalResizeControl.sizeLocked === true || terminalResizeControl.reason === 'size_locked'
  const activeTerminalOwnsResize = terminalResizeControl.canResize === true
  const requireVerification = Boolean(pair && verifiedDevice === false)
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
    showDelayedMachineNetworkOverlay,
    setMachineNetworkMachineId,
    updateConnectionStatus,
    clearConnectionStatus,
    clearConnectionStatusSoon,
  } = useMachineNetworkStatus()
  const machineForceRelayKey = machine?.machineId ? forceRelayStorageKey(machine.machineId) : null
  const effectiveTerminalSettings = terminalSettingsProp ?? terminalSettings
  const activeResizeSurfaceId = activeTerminalId && machine?.machineId
    ? appTerminalSurfaceId(machine.machineId, activeTerminalId)
    : ''
  const terminalThemeStyle = useMemo(
    () => terminalThemeCssVariables(effectiveTerminalSettings.themeId) as CSSProperties,
    [effectiveTerminalSettings.themeId],
  )
  const keyboardShouldResize = useCallback(() => {
    if (splitTerminalId) return true
    if (effectiveTerminalSettings.keyboardMode === 'resize') return true
    if (effectiveTerminalSettings.keyboardMode === 'shift') return false
    return terminalBufferBySlot[0] === 'alternate'
  }, [effectiveTerminalSettings.keyboardMode, splitTerminalId, terminalBufferBySlot])
  const {
    keyboardVisible,
    reapplyKeyboardLayout,
    handleBufferChange,
    handleCursorMove,
    markKeyboardVisible,
    markKeyboardHidden,
    resetKeyboardLayout,
  } = useTerminalKeyboard({
    containerRef: outerContainerRef,
    mainRef: terminalAreaRef,
    termWrapperRef: terminalWrapperRef,
    getTermRef: () => activeTerminalSlotRef.current === 1 ? splitTerminalRef.current : terminalRef.current,
    shouldResize: keyboardShouldResize,
    onKeyboardHide: () => {
      requestAnimationFrame(() => {
        terminalRef.current?.fit()
        splitTerminalRef.current?.fit()
      })
    },
  })

  useEffect(() => {
    reapplyKeyboardLayout()
    if (keyboardShouldResize()) {
      requestAnimationFrame(() => {
        terminalRef.current?.fit()
        splitTerminalRef.current?.fit()
      })
    }
  }, [activeTerminalSlot, effectiveTerminalSettings.keyboardMode, keyboardShouldResize, reapplyKeyboardLayout, splitTerminalId, terminalBufferBySlot])

  useEffect(() => {
    latestActiveTerminalIdRef.current = activeTerminalId
  }, [activeTerminalId])

  useEffect(() => {
    latestSplitTerminalIdRef.current = splitTerminalId
  }, [splitTerminalId])

  useEffect(() => {
    resizeLockedHintShownRef.current = false
  }, [activeTerminalId])

  useEffect(() => {
    setTerminalBufferBySlot((current) => {
      if (current[0] === 'normal' && current[1] === 'normal') return current
      return { 0: 'normal', 1: 'normal' }
    })
  }, [activeTerminalId, splitTerminalId])

  useEffect(() => {
    hasLoadedTerminalsRef.current = hasLoadedTerminals
  }, [hasLoadedTerminals])

  useEffect(() => {
    if (!initialMachine) return
    setMachine((current) => {
      if (!current || current.machineId !== initialMachine.machineId) return initialMachine
      return current
    })
  }, [initialMachine])

  useEffect(() => {
    setMachineNetworkMachineId(machine?.machineId ?? null)
  }, [machine?.machineId, setMachineNetworkMachineId])

  useEffect(() => {
    if (!machineForceRelayKey) return
    setForceRelayConnection(readStoredForceRelay(machineForceRelayKey))
  }, [machineForceRelayKey])

  useEffect(() => {
    const keybar = mobileKeybarRef.current
    const terminalArea = terminalAreaRef.current
    if (!keybar || !terminalArea || typeof ResizeObserver === 'undefined') return

    let fitFrame = 0
    const refitTerminals = () => {
      if (keyboardVisible && !keyboardShouldResize()) {
        reapplyKeyboardLayout()
        return
      }
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
  }, [keyboardShouldResize, keyboardVisible, page, reapplyKeyboardLayout])

  const updateTerminalSettings = useCallback((patch: Partial<TerminalSettings>) => {
    if (onTerminalSettingsChange) {
      onTerminalSettingsChange(patch)
      return
    }
    setTerminalSettings((current) => writeTerminalSettings({ ...current, ...patch }))
  }, [onTerminalSettingsChange])

  const handleConnectionAuthFailure = useCallback((machineId?: string | null) => {
    const targetMachineId = machineId ?? initialMachine?.machineId
    if (!targetMachineId) return
    pair?.sessionStore.clearSessionToken(targetMachineId)
    setVerifiedDevice(false)
    if (onNeedsReauthorization) {
      onNeedsReauthorization(targetMachineId)
      return
    }
    if (!pair) return
    setMobileSheet('pair')
  }, [initialMachine?.machineId, onNeedsReauthorization, pair])

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
    if (snapshot.phase === 'failed') {
      const message = connectionErrorDisplayMessage(snapshot.failReason || snapshot.statusText || 'Connection failed')
      if (isAuthConnectionError(snapshot.failReason || snapshot.statusText)) handleConnectionAuthFailure(snapshot.machineId)
      setError(message)
      updateConnectionStatus(message, 'failed')
      return
    }
    updateConnectionStatus(snapshot.statusText || connectionPhaseLabel(snapshot.phase), snapshot.phase)
  }, [clearConnectionStatus, clearConnectionStatusSoon, handleConnectionAuthFailure, updateConnectionStatus])

  const updateFromPassiveConnectionState = useCallback((snapshot: RtcConnectionStateSnapshot, session?: RtcSession) => {
    if (isTransientConnectionPhase(snapshot.phase)) return
    updateFromConnectionState(snapshot, session)
  }, [updateFromConnectionState])

  const reattachActiveTerminals = useCallback((session: RtcSession) => {
    const terminalId = latestActiveTerminalIdRef.current
    const currentSplitTerminalId = latestSplitTerminalIdRef.current
    if (!terminalId && !currentSplitTerminalId) return
    setConnectedSession(session)
    if (terminalId) {
      setConnectedTerminalId(terminalId)
      setConnectingTerminalId(null)
      terminalRef.current?.reattach(session, { forceTerminalChannel: true })
    }
    if (currentSplitTerminalId) {
      splitTerminalRef.current?.reattach(session, { forceTerminalChannel: true })
    }
  }, [])

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
    sessionConnectionPhaseRef.current = null
    const candidate = session as RtcSession & Partial<RtcSessionConnectionStateEvents>
    if (typeof candidate.subscribeConnectionState !== 'function') {
      connectionStateSubscriptionRef.current = null
      return
    }
    connectionStateSubscriptionRef.current = candidate.subscribeConnectionState((snapshot) => {
      const previousPhase = sessionConnectionPhaseRef.current
      sessionConnectionPhaseRef.current = snapshot.phase
      if (snapshot.phase === 'connected' && previousPhase !== null && previousPhase !== 'connected') {
        reattachActiveTerminals(session)
      }
      updateFromPassiveConnectionState(snapshot, session)
    })
  }, [reattachActiveTerminals, updateFromPassiveConnectionState])

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

  const withMachineSession = useCallback(async () => {
    if (!machine) throw new Error('machine is required before using runtime storage')
    return await ensureMachineSession(machine.machineId, { forceRelay: forceRelayConnection })
  }, [ensureMachineSession, forceRelayConnection, machine])

  const refreshTerminals = useCallback(async () => {
    const seq = terminalRefreshSeqRef.current + 1
    terminalRefreshSeqRef.current = seq
    if (!hasLoadedTerminalsRef.current) setLoadingTerminals(true)
    let refreshMachineId = initialMachine?.machineId ?? null
    try {
      const status = await api.getStatus()
      if (terminalRefreshSeqRef.current !== seq) return
      refreshMachineId = status.machine.machineId
      setMachineNetworkMachineId(status.machine.machineId)
      setMachine(status.machine)
      if (pair) setVerifiedDevice(Boolean(pair.sessionStore.getSessionToken(status.machine.machineId)))
      const forceRelay = forceRelayPreference(status.machine.machineId)
      setForceRelayConnection(forceRelay)
      const terminalList = await api.listTerminals({ forceRelay })
      if (terminalRefreshSeqRef.current !== seq) return
      setTerminals(terminalList)
      setHasLoadedTerminals(true)
      setError(null)
    } catch (err) {
      if (terminalRefreshSeqRef.current === seq) {
        const message = connectionErrorDisplayMessage(err)
        if (isAuthConnectionError(err)) handleConnectionAuthFailure(refreshMachineId)
        setError(message)
        updateConnectionStatus(message, 'failed')
      }
    } finally {
      if (terminalRefreshSeqRef.current === seq) {
        setLoadingTerminals(false)
      }
    }
  }, [api, handleConnectionAuthFailure, initialMachine?.machineId, pair, setMachineNetworkMachineId, updateConnectionStatus])

  const applyRuntimeTerminalEvent = useCallback((event: RtcEvent | { payload?: unknown }): boolean => {
    const payload = event.payload
    if (typeof payload !== 'object' || payload === null || Array.isArray(payload)) return false
    const record = payload as Record<string, unknown>
    const terminal = normalizeRuntimeTerminalEvent(record)
    if (!terminal) return false

    setTerminals((current) => {
      const index = current.findIndex((item) => item.terminalId === terminal.terminalId)
      if (index < 0) return current
      const next = current.slice()
      next[index] = { ...next[index], ...terminal }
      return next
    })

    if (activeTerminalId === terminal.terminalId && activeResizeSurfaceId) {
      setTerminalResizeControl(resizeControlFromRuntimeTerminal(terminal, activeResizeSurfaceId))
    }
    return true
  }, [activeResizeSurfaceId, activeTerminalId])

  useEffect(() => {
    let cancelled = false
    const seq = terminalRefreshSeqRef.current + 1
    terminalRefreshSeqRef.current = seq
    async function load() {
      if (!hasLoadedTerminalsRef.current) setLoadingTerminals(true)
      let failed = false
      let loadMachineId = initialMachine?.machineId ?? null
      try {
        const status = await api.getStatus()
        if (cancelled || terminalRefreshSeqRef.current !== seq) return
        loadMachineId = status.machine.machineId
        setMachineNetworkMachineId(status.machine.machineId)
        setMachine(status.machine)
        if (pair) setVerifiedDevice(Boolean(pair.sessionStore.getSessionToken(status.machine.machineId)))
        const cachedInventory = inventoryCacheForConnector(connector).get(status.machine.machineId)
        if (cachedInventory && !hasLoadedTerminalsRef.current) {
          setTerminals(cachedInventory.terminals)
          setHasLoadedTerminals(true)
          setLoadingTerminals(false)
        }
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
        setHasLoadedTerminals(true)
      } catch (err) {
        if (!cancelled && terminalRefreshSeqRef.current === seq) {
          failed = true
          const message = connectionErrorDisplayMessage(err)
          if (isAuthConnectionError(err)) handleConnectionAuthFailure(loadMachineId)
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
  }, [api, clearConnectionStatus, connector, handleConnectionAuthFailure, initialMachine?.machineId, pair, setMachineNetworkMachineId, updateConnectionStatus])

  useEffect(() => {
    if (!machine || !hasLoadedTerminals) return
    inventoryCacheForConnector(connector).set(machine.machineId, {
      machine,
      terminals,
    })
  }, [connector, hasLoadedTerminals, machine, terminals])

  useEffect(() => {
    if (!inventoryEvents || !machine) return
    const subscription = inventoryEvents.subscribe(machine.machineId, (event) => {
      if (!applyRuntimeTerminalEvent(event)) {
        void refreshTerminals()
      }
    })
    return () => {
      subscription.close()
    }
  }, [applyRuntimeTerminalEvent, inventoryEvents, machine, refreshTerminals, connectionRetryToken])

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
            reattachActiveTerminals(session)
            updateFromPassiveConnectionState(snapshot, session)
          })
          .catch((err: unknown) => {
            const message = connectionErrorDisplayMessage(err)
            if (isAuthConnectionError(err)) handleConnectionAuthFailure(machine.machineId)
            updateConnectionStatus(message, 'failed')
          })
        return
      }
      updateFromPassiveConnectionState(snapshot, activeSession)
    })
    return () => {
      subscription.close()
    }
  }, [activeTerminalId, connectionStateEvents, ensureMachineSession, forceRelayConnection, handleConnectionAuthFailure, machine, reattachActiveTerminals, updateConnectionStatus, updateFromPassiveConnectionState])

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
        if (!isTerminalInventoryRuntimeEvent(event)) return
        if (!applyRuntimeTerminalEvent(event)) {
          void refreshTerminals()
        }
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
  }, [applyRuntimeTerminalEvent, connectionRetryToken, connector, ensureMachineSession, forceRelayConnection, machine?.machineId, refreshTerminals, requireVerification, subscribeRuntimeInventoryEvents])

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
        const message = connectionErrorDisplayMessage(err)
        if (isAuthConnectionError(err)) handleConnectionAuthFailure(machineId)
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
  }, [activeTerminalId, clearConnectionStatus, clearConnectionStatusSoon, connector, connectionRetryToken, ensureMachineSession, forceRelayConnection, handleConnectionAuthFailure, machine?.machineId, manualReconnectNonce, page, pair, releaseMachineSession, updateConnectionStatus, updateFromConnectionState])

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
        reattachActiveTerminals(session)
      }
      updateConnectionStatus('Connected', 'connected')
      clearConnectionStatusSoon()
    }).catch((err: unknown) => {
      if (cancelled) return
      const message = connectionErrorDisplayMessage(err)
      if (isAuthConnectionError(err)) handleConnectionAuthFailure(machineId)
      setConnectedSession(null)
      setConnectedTerminalId(null)
      setConnectingTerminalId(null)
      updateConnectionStatus(message, 'failed')
    })
    return () => {
      cancelled = true
    }
  }, [activeTerminalId, clearConnectionStatusSoon, ensureMachineSession, forceRelayConnection, handleConnectionAuthFailure, machine?.machineId, manualReconnectNonce, page, reattachActiveTerminals, requireVerification, updateConnectionStatus, updateFromConnectionState])

  useEffect(() => {
    const handleResume = () => {
      if (page !== 'terminal') return
      resetKeyboardLayout()
      const session = machineSessionRef.current?.session ?? connectedSession
      if (!session || !isRtcSessionAlive(session)) return
      if (!activeTerminalId && !splitTerminalId) return

      setConnectedSession(session)
      reattachActiveTerminals(session)
    }
    document.addEventListener('termx:resume', handleResume)
    return () => {
      document.removeEventListener('termx:resume', handleResume)
    }
  }, [activeTerminalId, connectedSession, page, reattachActiveTerminals, resetKeyboardLayout, splitTerminalId])

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

  const handleTerminalBufferChange = useCallback((slot: TerminalSlot, isAlternate: boolean) => {
    setTerminalBufferBySlot((current) => {
      const nextBuffer = isAlternate ? 'alternate' : 'normal'
      if (current[slot] === nextBuffer) return current
      return { ...current, [slot]: nextBuffer }
    })
    handleBufferChange(isAlternate)
  }, [handleBufferChange])

  const focusActiveTerminal = useCallback(() => {
    markKeyboardVisible()
    activeTerminalHandle()?.focus()
  }, [activeTerminalHandle, markKeyboardVisible])

  const blurActiveTerminal = useCallback(() => {
    activeTerminalHandle()?.blur()
    markKeyboardHidden()
  }, [activeTerminalHandle, markKeyboardHidden])

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
    void refreshTerminals()
  }, [refreshTerminals])

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
    const machineId = machine?.machineId ?? initialMachine?.machineId
    if (!machineId) {
      setVerifiedDevice(null)
      return
    }
    setVerifiedDevice(Boolean(pair.sessionStore.getSessionToken(machineId)))
  }, [machine?.machineId, pair, connectionRetryToken, initialMachine?.machineId])

  useEffect(() => {
    if (!pairStatus) return
    const timer = setTimeout(() => setPairStatus(null), pairStatus === 'Resize is locked. Tap LK to request manually.' ? 1800 : 3000)
    return () => clearTimeout(timer)
  }, [pairStatus])

  useEffect(() => {
    if (!(terminalResizeControl.sizeLocked || terminalResizeControl.reason === 'size_locked')) return
    if (resizeLockedHintShownRef.current) return
    resizeLockedHintShownRef.current = true
    setPairStatus('Resize is locked. Tap LK to request manually.')
  }, [terminalResizeControl.reason, terminalResizeControl.sizeLocked])

  const showTerminalListPage = useCallback(() => {
    setPage('terminal-list')
    setMobileSheet(null)
    setSplitTerminalId(null)
    setActiveTerminalSlot(0)
    setSyncSplitInput(false)
    resetKeyboardLayout()
    terminalRef.current?.adjustInputPosition(0)
    splitTerminalRef.current?.adjustInputPosition(0)
  }, [resetKeyboardLayout])

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
        const message = connectionErrorDisplayMessage(err)
        if (isAuthConnectionError(err)) handleConnectionAuthFailure(machine.machineId)
        updateConnectionStatus(message, 'failed')
        setFilesOpen(false)
        setFileTerminalId(null)
        if (pair) setMobileSheet('pair')
      })
    setFilesOpen(true)
    setMobileSheet(null)
  }, [activeToolTerminal, ensureMachineSession, fileContextKey, filesOpen, forceRelayConnection, handleConnectionAuthFailure, machine, page, pair, requireVerification, terminals, updateConnectionStatus, updateFromConnectionState])

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
    setSelectedTerminalId(intent.terminalId)
    setMobileSheet('manage-terminal')
  }, [canManageTerminals, machine, requireVerification])

  const openCreateTerminal = useCallback(() => {
    if (requireVerification) {
      setMobileSheet('pair')
      return
    }
    if (!canManageTerminals) return
    setSelectedTerminalId(null)
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
    if (!selectedTerminal) return
    setTerminalForm({
      name: selectedTerminal.title,
      command: selectedTerminal.command ?? '',
      cwd: selectedTerminal.cwd ?? '',
      environment: selectedTerminal.environment ?? '',
      sizeLockMode: selectedTerminal.sizeLockMode ?? 'off',
    })
    setMobileSheet('edit-terminal')
  }, [selectedTerminal])

  const selectTerminalWorkingDirectory = useCallback((path: string) => {
    setTerminalForm((current) => ({ ...current, cwd: normalizeFilePath(path) }))
    setMobileSheet(terminalPathReturnSheet)
  }, [terminalPathReturnSheet])

  const loadTerminalPathPicker = useCallback(async (path: string) => {
    const normalizedPath = normalizeDirectoryPickerPath(path)
    setTerminalPathPickerPath(normalizedPath)
    setTerminalPathPickerLoading(true)
    setTerminalPathPickerError(null)
    try {
      const session = await withMachineSession()
      const response = await createFileApi(session).listDir(normalizedPath)
      setTerminalPathPickerPath(response.path || normalizedPath)
      setTerminalPathPickerEntries(response.entries.filter(isDirectoryEntry))
    } catch (err) {
      setTerminalPathPickerEntries([])
      setTerminalPathPickerError(err instanceof Error ? err.message : String(err))
    } finally {
      setTerminalPathPickerLoading(false)
    }
  }, [withMachineSession])

  const openTerminalPathPicker = useCallback(() => {
    const returnSheet: TerminalEditorSheet = mobileSheet === 'edit-terminal' ? 'edit-terminal' : 'create-terminal'
    const startPath = normalizeTerminalDirectory(terminalForm.cwd) || normalizeTerminalDirectory(activeToolTerminal?.cwd) || '/'
    setTerminalPathReturnSheet(returnSheet)
    setMobileSheet('terminal-path-picker')
    void loadTerminalPathPicker(startPath)
  }, [activeToolTerminal?.cwd, loadTerminalPathPicker, mobileSheet, terminalForm.cwd])

  const loadTerminalPathBookmarks = useCallback(async () => {
    setTerminalPathBookmarksLoading(true)
    setTerminalPathBookmarksError(null)
    try {
      const session = await withMachineSession()
      setTerminalPathBookmarks(await createPathBookmarkApi(session).list())
    } catch (err) {
      setTerminalPathBookmarksError(err instanceof Error ? err.message : String(err))
    } finally {
      setTerminalPathBookmarksLoading(false)
    }
  }, [withMachineSession])

  const openTerminalPathBookmarks = useCallback(() => {
    const returnSheet: TerminalEditorSheet = mobileSheet === 'edit-terminal' ? 'edit-terminal' : 'create-terminal'
    setTerminalPathReturnSheet(returnSheet)
    setMobileSheet('terminal-path-bookmarks')
    void loadTerminalPathBookmarks()
  }, [loadTerminalPathBookmarks, mobileSheet])

  const addTerminalPathBookmark = useCallback(async () => {
    const path = normalizeTerminalDirectory(terminalForm.cwd) || normalizeTerminalDirectory(activeToolTerminal?.cwd) || '/'
    setTerminalPathBookmarksError(null)
    try {
      const session = await withMachineSession()
      await createPathBookmarkApi(session).add(path)
      setPairStatus(`Bookmarked ${path}`)
      await loadTerminalPathBookmarks()
    } catch (err) {
      setTerminalPathBookmarksError(err instanceof Error ? err.message : String(err))
    }
  }, [activeToolTerminal?.cwd, loadTerminalPathBookmarks, terminalForm.cwd, withMachineSession])

  const removeTerminalPathBookmark = useCallback(async (id: string) => {
    setTerminalPathBookmarksError(null)
    try {
      const session = await withMachineSession()
      await createPathBookmarkApi(session).remove(id)
      await loadTerminalPathBookmarks()
    } catch (err) {
      setTerminalPathBookmarksError(err instanceof Error ? err.message : String(err))
    }
  }, [loadTerminalPathBookmarks, withMachineSession])

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
    if (!canManageTerminals || !selectedTerminalId) return
    const input: LocalUpdateTerminalInput = {
      terminalId: selectedTerminalId,
      name: terminalForm.name.trim() || undefined,
      cwd: terminalForm.cwd.trim() || undefined,
      environment: terminalForm.environment.trim() || undefined,
      sizeLockMode: terminalForm.sizeLockMode,
    }
    const management = await withManagementApi()
    await management.api.updateTerminal(input)
    await refreshTerminals()
    setPairStatus(`Updated ${input.name || selectedTerminal?.title || selectedTerminalId}`)
    setMobileSheet(null)
  }, [canManageTerminals, selectedTerminal, selectedTerminalId, refreshTerminals, terminalForm, withManagementApi])

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
      const message = connectionErrorDisplayMessage(err)
      if (isAuthConnectionError(err)) handleConnectionAuthFailure(machine?.machineId)
      updateConnectionStatus(message, 'failed')
    } finally {
      setUnlockingResize(false)
    }
  }, [activeTerminalId, canManageTerminals, handleConnectionAuthFailure, machine?.machineId, refreshTerminals, updateConnectionStatus, withManagementApi])

  const acquireActiveResizeOwner = useCallback(async () => {
    try {
      const control = await activeTerminalHandle()?.requestResizeOwner()
      if (control?.canResize) {
        setPairStatus('Resize control acquired')
        window.setTimeout(() => {
          activeTerminalHandle()?.fit()
        }, 0)
      } else if (control?.sizeLocked || control?.reason === 'size_locked') {
        setPairStatus('Resize is locked')
      } else {
        setPairStatus('Resize control unavailable')
      }
    } catch (err) {
      const message = connectionErrorDisplayMessage(err)
      if (isAuthConnectionError(err)) handleConnectionAuthFailure(machine?.machineId)
      updateConnectionStatus(message, 'failed')
    }
  }, [activeTerminalHandle, handleConnectionAuthFailure, machine?.machineId, updateConnectionStatus])

  const releaseActiveResizeOwner = useCallback(async () => {
    try {
      await activeTerminalHandle()?.releaseResizeOwner()
      setPairStatus('Resize control released')
    } catch (err) {
      const message = connectionErrorDisplayMessage(err)
      if (isAuthConnectionError(err)) handleConnectionAuthFailure(machine?.machineId)
      updateConnectionStatus(message, 'failed')
    }
  }, [activeTerminalHandle, handleConnectionAuthFailure, machine?.machineId, updateConnectionStatus])

  const deleteManagedTerminal = useCallback(async () => {
    if (!canManageTerminals || !selectedTerminalId) return
    const deletedTerminalId = selectedTerminalId
    const deletedTitle = selectedTerminal?.title ?? selectedTerminalId
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
  }, [activeTerminalId, canManageTerminals, fileTerminalId, selectedTerminal, selectedTerminalId, refreshTerminals, splitTerminalId, withManagementApi])

  const restartManagedTerminal = useCallback(async () => {
    if (!canManageTerminals || !selectedTerminalId) return
    const restartedTerminalId = selectedTerminalId
    const restartedTitle = selectedTerminal?.title ?? selectedTerminalId
    const management = await withManagementApi()
    closeTerminalDataChannel(management.session, restartedTerminalId)
    await management.api.restartTerminal(restartedTerminalId)
    await refreshTerminals()
    if (activeTerminalId === restartedTerminalId) {
      setConnectedSession(management.session)
      setConnectedTerminalId(restartedTerminalId)
      setConnectingTerminalId(null)
      terminalRef.current?.reattach(management.session, { forceTerminalChannel: true })
      window.setTimeout(() => {
        terminalRef.current?.fit()
        terminalRef.current?.focus()
      }, 0)
    }
    if (splitTerminalId === restartedTerminalId) {
      splitTerminalRef.current?.reattach(management.session, { forceTerminalChannel: true })
      window.setTimeout(() => {
        splitTerminalRef.current?.fit()
      }, 0)
    }
    setPairStatus(`Restarted ${restartedTitle}`)
    setMobileSheet(null)
  }, [activeTerminalId, canManageTerminals, selectedTerminal, selectedTerminalId, refreshTerminals, splitTerminalId, withManagementApi])

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

  const pasteTerminalTextWithConfirm = useCallback((text: string): boolean => {
    if (!text) {
      setPairStatus('Clipboard is empty')
      return false
    }
    const needsConfirm = text.length > 200 || text.includes('\n') || text.includes('\r')
    if (needsConfirm) {
      setPasteConfirmText(text)
      setMobileSheet(null)
      return true
    }
    pasteTerminalText(text)
    setTerminalToolbarOpen(false)
    setTerminalToolbarModeAndReset('default')
    setMobileSheet(null)
    return true
  }, [pasteTerminalText, setTerminalToolbarModeAndReset])

  const refreshClipboardEntries = useCallback(async () => {
    setClipboardLoading(true)
    setClipboardError(null)
    try {
      const session = await withMachineSession()
      setClipboardEntries(await createRemoteClipboardApi(session).list())
    } catch (err) {
      setClipboardEntries([])
      setClipboardError(err instanceof Error ? err.message : String(err))
    } finally {
      setClipboardLoading(false)
    }
  }, [withMachineSession])

  const openClipboardHistory = useCallback(() => {
    setTerminalToolbarOpen(false)
    setTerminalToolbarModeAndReset('default')
    setMobileSheet('clipboard-history')
    void refreshClipboardEntries()
  }, [refreshClipboardEntries, setTerminalToolbarModeAndReset])

  const saveClipboardDraft = useCallback(async () => {
    const text = clipboardDraft
    if (!text) {
      setClipboardError('Clipboard text is empty')
      return
    }
    setClipboardLoading(true)
    setClipboardError(null)
    try {
      const session = await withMachineSession()
      const api = createRemoteClipboardApi(session)
      if (editingClipboardId) {
        await api.updateText(editingClipboardId, text)
      } else {
        await api.putText(text)
      }
      setClipboardDraft('')
      setEditingClipboardId(null)
      setClipboardEntries(await api.list())
      setPairStatus(editingClipboardId ? 'Clipboard entry updated' : 'Clipboard entry saved')
    } catch (err) {
      setClipboardError(err instanceof Error ? err.message : String(err))
    } finally {
      setClipboardLoading(false)
    }
  }, [clipboardDraft, editingClipboardId, withMachineSession])

  const deleteClipboardEntry = useCallback(async (id: string) => {
    setClipboardLoading(true)
    setClipboardError(null)
    try {
      const session = await withMachineSession()
      const api = createRemoteClipboardApi(session)
      await api.delete(id)
      if (editingClipboardId === id) {
        setEditingClipboardId(null)
        setClipboardDraft('')
      }
      setClipboardEntries(await api.list())
    } catch (err) {
      setClipboardError(err instanceof Error ? err.message : String(err))
    } finally {
      setClipboardLoading(false)
    }
  }, [editingClipboardId, withMachineSession])

  const loadBrowserClipboardDraft = useCallback(async () => {
    setClipboardError(null)
    try {
      const text = await navigator.clipboard.readText()
      setClipboardDraft(text)
      setEditingClipboardId(null)
    } catch (err) {
      setClipboardError(err instanceof Error ? err.message : 'Unable to read browser clipboard')
    }
  }, [])

  const handleTerminalPaste = useCallback(async () => {
    let remoteClipboardError: unknown
    try {
      const session = await withMachineSession()
      const [latest] = await createRemoteClipboardApi(session).list()
      if (latest?.text && pasteTerminalTextWithConfirm(latest.text)) return
    } catch (err) {
      remoteClipboardError = err
    }

    try {
      const text = await navigator.clipboard.readText()
      pasteTerminalTextWithConfirm(text)
    } catch (err) {
      const fallbackMessage = err instanceof Error ? err.message : 'Unable to read clipboard'
      setError(remoteClipboardError instanceof Error ? remoteClipboardError.message : fallbackMessage)
    }
  }, [pasteTerminalTextWithConfirm, withMachineSession])

  const renderTerminalPathPickerSheet = () => {
    if (mobileSheet !== 'terminal-path-picker') return null
    const normalizedPath = normalizeFilePath(terminalPathPickerPath)
    const directories = [...terminalPathPickerEntries]
      .filter(isDirectoryEntry)
      .sort((left, right) => left.name.localeCompare(right.name, undefined, { numeric: true, sensitivity: 'base' }))
    return (
      <MobileSheetPanel title="Choose directory" testId="termx-terminal-path-picker-sheet" onClose={() => setMobileSheet(terminalPathReturnSheet)}>
        <div className="flex flex-col gap-3">
          <div className="termx-app-panel p-3">
            <div className="break-all font-mono text-[12px] font-semibold text-zinc-800">{normalizedPath}</div>
            <div className="mt-3 grid grid-cols-2 gap-2">
              <button
                type="button"
                className="termx-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold disabled:text-zinc-300"
                disabled={normalizedPath === '/'}
                onClick={() => { hapticImpact(); void loadTerminalPathPicker(parentPath(normalizedPath)) }}
              >
                <ChevronLeft className="h-4 w-4" />
                Parent
              </button>
              <button
                type="button"
                className="termx-app-primary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
                onClick={() => { hapticImpact(); selectTerminalWorkingDirectory(normalizedPath) }}
              >
                <FolderOpen className="h-4 w-4" />
                Use this path
              </button>
            </div>
          </div>

          {terminalPathPickerError ? (
            <div className="border border-amber-200 bg-amber-50 px-3 py-2 text-[13px] font-medium text-amber-800" role="alert">
              {terminalPathPickerError}
            </div>
          ) : null}

          <div
            className="termx-app-panel flex h-80 max-h-[45vh] min-h-0 flex-col overflow-hidden"
            data-testid="termx-terminal-path-picker-list"
          >
            {terminalPathPickerLoading ? (
              <div className="flex h-full items-center justify-center gap-2 text-[13px] font-medium text-zinc-500">
                <span className="termx-square-spinner" aria-hidden="true" />
                Loading...
              </div>
            ) : directories.length === 0 ? (
              <div className="flex h-full items-center justify-center text-[13px] font-medium text-zinc-500">
                Empty
              </div>
            ) : (
              <div className="min-h-0 flex-1 overflow-y-auto">
                {directories.map((entry) => {
                  const path = joinPath(normalizedPath, entry.name)
                  return (
                    <button
                      key={path}
                      type="button"
                      className="flex min-h-12 w-full items-center gap-3 border-b border-zinc-100 px-3 text-left last:border-b-0 hover:bg-zinc-50 active:bg-zinc-50"
                      onClick={() => { hapticImpact(); void loadTerminalPathPicker(path) }}
                    >
                      <Folder className="h-4 w-4 shrink-0 text-zinc-500" />
                      <span className="min-w-0 flex-1 truncate text-[14px] font-semibold text-zinc-900">{entry.name}</span>
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        </div>
      </MobileSheetPanel>
    )
  }

  const renderTerminalPathBookmarksSheet = () => {
    if (mobileSheet !== 'terminal-path-bookmarks') return null
    return (
      <MobileSheetPanel title="Path bookmarks" testId="termx-terminal-path-bookmarks-sheet" onClose={() => setMobileSheet(terminalPathReturnSheet)}>
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-2 gap-2">
            <button
              type="button"
              className="termx-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
              onClick={() => { hapticImpact(); void addTerminalPathBookmark() }}
            >
              <BookmarkPlus className="h-4 w-4" />
              Save path
            </button>
            <button
              type="button"
              className="termx-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
              onClick={() => { hapticImpact(); void loadTerminalPathBookmarks() }}
            >
              <RefreshCw className="h-4 w-4" />
              Refresh
            </button>
          </div>

          {terminalPathBookmarksError ? (
            <div className="border border-amber-200 bg-amber-50 px-3 py-2 text-[13px] font-medium text-amber-800" role="alert">
              {terminalPathBookmarksError}
            </div>
          ) : null}

          <div className="termx-app-panel overflow-hidden">
            {terminalPathBookmarksLoading ? (
              <div className="flex min-h-20 items-center justify-center gap-2 text-[13px] font-medium text-zinc-500">
                <span className="termx-square-spinner" aria-hidden="true" />
                Loading...
              </div>
            ) : terminalPathBookmarks.length === 0 ? (
              <div className="flex min-h-20 items-center justify-center px-3 text-center text-[13px] font-medium text-zinc-500">
                No saved paths
              </div>
            ) : (
              terminalPathBookmarks.map((bookmark) => (
                <div key={bookmark.id} className="flex min-h-14 items-center gap-2 border-b border-zinc-100 px-3 last:border-b-0">
                  <button
                    type="button"
                    className="min-w-0 flex-1 text-left active:opacity-70"
                    onClick={() => { hapticImpact(); selectTerminalWorkingDirectory(bookmark.path) }}
                  >
                    <span className="block truncate text-[14px] font-semibold text-zinc-900">{bookmark.label}</span>
                    <span className="mt-0.5 block truncate font-mono text-[11px] font-medium text-zinc-500">{bookmark.path}</span>
                  </button>
                  <button
                    type="button"
                    aria-label={`Remove ${bookmark.label}`}
                    className="flex h-11 w-11 shrink-0 items-center justify-center text-red-500 hover:bg-red-50/80 active:bg-red-50"
                    onClick={() => { hapticImpact(); void removeTerminalPathBookmark(bookmark.id) }}
                  >
                    <BookmarkMinus className="h-4 w-4" />
                  </button>
                </div>
              ))
            )}
          </div>
        </div>
      </MobileSheetPanel>
    )
  }

  const renderClipboardHistorySheet = () => {
    if (mobileSheet !== 'clipboard-history') return null
    return (
      <MobileSheetPanel title="Clipboard" testId="termx-clipboard-history-sheet" onClose={() => setMobileSheet(null)}>
        <div className="flex flex-col gap-3">
          <div className="termx-app-panel p-3">
            <textarea
              aria-label="Clipboard text"
              className="min-h-24 w-full resize-none border border-[var(--termx-app-line)] bg-zinc-50 px-3 py-2 text-[13px] font-medium text-zinc-900 outline-none"
              value={clipboardDraft}
              onChange={(event) => setClipboardDraft(event.currentTarget.value)}
            />
            <div className="mt-2 grid grid-cols-3 gap-2">
              <button
                type="button"
                className="termx-app-secondary-button min-h-11 gap-1.5 px-2 text-[12px] font-semibold"
                onClick={() => { hapticImpact(); void loadBrowserClipboardDraft() }}
              >
                <ClipboardList className="h-4 w-4" />
                Browser
              </button>
              <button
                type="button"
                className="termx-app-secondary-button min-h-11 gap-1.5 px-2 text-[12px] font-semibold"
                onClick={() => { hapticImpact(); void refreshClipboardEntries() }}
              >
                <RefreshCw className="h-4 w-4" />
                Refresh
              </button>
              <button
                type="button"
                className="termx-app-primary-button min-h-11 px-2 text-[12px] font-semibold disabled:bg-zinc-300 disabled:text-zinc-500"
                disabled={!clipboardDraft || clipboardLoading}
                onClick={() => { hapticImpact(); void saveClipboardDraft() }}
              >
                {editingClipboardId ? 'Update' : 'Save'}
              </button>
            </div>
          </div>

          {clipboardError ? (
            <div className="border border-amber-200 bg-amber-50 px-3 py-2 text-[13px] font-medium text-amber-800" role="alert">
              {clipboardError}
            </div>
          ) : null}

          <div className="termx-app-panel overflow-hidden">
            {clipboardLoading && clipboardEntries.length === 0 ? (
              <div className="flex min-h-20 items-center justify-center gap-2 text-[13px] font-medium text-zinc-500">
                <span className="termx-square-spinner" aria-hidden="true" />
                Loading...
              </div>
            ) : clipboardEntries.length === 0 ? (
              <div className="flex min-h-20 items-center justify-center px-3 text-center text-[13px] font-medium text-zinc-500">
                No clipboard history
              </div>
            ) : (
              clipboardEntries.map((entry) => (
                <div key={entry.id} className="border-b border-zinc-100 p-3 last:border-b-0">
                  <button
                    type="button"
                    className="block w-full text-left active:opacity-70"
                    onClick={() => { hapticImpact(); pasteTerminalTextWithConfirm(entry.text) }}
                  >
                    <span className="block max-h-10 overflow-hidden text-[14px] font-semibold text-zinc-900">{entry.preview}</span>
                    <span className="mt-1 block text-[11px] font-medium text-zinc-500">{formatClipboardTimestamp(entry.createdAt)}</span>
                  </button>
                  <div className="mt-2 flex items-center justify-end gap-2">
                    <button
                      type="button"
                      className="termx-app-secondary-button min-h-11 gap-1.5 px-2 text-[12px] font-semibold"
                      onClick={() => { hapticImpact(); setEditingClipboardId(entry.id); setClipboardDraft(entry.text) }}
                    >
                      <SquarePen className="h-3.5 w-3.5" />
                      Edit
                    </button>
                    <button
                      type="button"
                      className="flex min-h-11 items-center gap-1.5 border border-red-200 px-2 text-[12px] font-semibold text-red-600 hover:bg-red-50/80 active:bg-red-50"
                      onClick={() => { hapticImpact(); void deleteClipboardEntry(entry.id) }}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                      Delete
                    </button>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      </MobileSheetPanel>
    )
  }

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
    const showTerminalListLoader = loadingTerminals && !hasLoadedTerminals

    return (
      <aside
        className={`termx-app-page relative min-h-0 flex-1 flex-col md:flex md:w-72 md:flex-none md:border-r md:border-[var(--termx-app-line)] ${page === 'terminal' ? 'hidden' : 'flex'}`}
        data-testid={page === 'terminal' ? undefined : 'termx-terminal-list-page'}
      >
        <header className="termx-app-header flex min-h-14 shrink-0 items-center justify-between border-b px-3 pt-[env(safe-area-inset-top)] md:pt-0">
          <div className="flex min-w-0 items-center gap-2">
            {onBack ? (
              <button
                type="button"
                aria-label="Back to machines"
                className="termx-app-icon-button mr-1 border-transparent bg-transparent"
                onClick={() => { hapticSelection(); onBack() }}
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
              aria-hidden={page === 'terminal' ? 'true' : undefined}
              aria-label="Connection info"
              className="termx-app-icon-button border-transparent bg-transparent"
              tabIndex={page === 'terminal' ? -1 : undefined}
              onClick={() => { hapticSelection(); openConnectionInfo() }}
            >
              <Info className="h-5 w-5" />
            </button>
            <button
              type="button"
              aria-hidden={page === 'terminal' ? 'true' : undefined}
              aria-label="Open files"
              className="termx-app-icon-button border-transparent bg-transparent"
              tabIndex={page === 'terminal' ? -1 : undefined}
              onClick={() => { hapticSelection(); openFiles() }}
            >
              <Folder className="h-5 w-5" />
            </button>
            {canManageTerminals ? (
              <button
                type="button"
                aria-label="Create terminal"
                className="termx-app-icon-button border-transparent bg-transparent"
                onClick={() => { hapticImpact(); openCreateTerminal() }}
              >
                <Plus className="h-5 w-5" />
              </button>
            ) : null}
          </div>
        </header>
        {showDelayedMachineNetworkOverlay ? (
          <div className="flex animate-in fade-in slide-in-from-top-1 duration-200 items-center justify-center gap-2 border-b border-zinc-200 bg-blue-50/50 px-3 py-1.5">
            <span className="termx-square-spinner h-3.5 w-3.5 text-blue-600" aria-hidden="true" />
            <span className="text-[11px] font-medium text-blue-700">
              {connectionStatus || 'Connecting...'}
            </span>
          </div>
        ) : null}
        {error ? (
          <div className="m-3 shrink-0 border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800" role="alert">
            {error}
          </div>
        ) : null}
        {requireVerification ? (
          <section className="termx-app-panel mx-3 mt-3 p-5" data-testid="termx-verification-gate">
            <div className="mb-4 flex items-center gap-3">
              <div className="flex h-11 w-11 items-center justify-center border border-[var(--termx-app-line)] bg-[var(--termx-app-soft)] text-zinc-600">
                <KeyRound className="h-5 w-5" />
              </div>
              <div>
                <h2 className="text-[17px] font-bold tracking-tight text-zinc-900">Verify This Device</h2>
                <p className="text-[13px] font-medium text-zinc-500">Pair first before opening or managing local terminals.</p>
              </div>
            </div>
            <button
              type="button"
              className="termx-app-primary-button min-h-12 w-full gap-2 px-4 text-[15px] font-semibold"
              onClick={() => { hapticImpact(); setMobileSheet('pair') }}
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
          <TerminalList
            machineId={machine.machineId}
            terminals={terminals}
            onOpenTerminal={openTerminal}
            onManageTerminal={openManageTerminal}
            activeTerminalId={activeTerminalId ?? undefined}
            loading={showTerminalListLoader}
          />
        </div>

        {mobileSheet === 'manage-terminal' && selectedTerminal ? (
          <MobileSheetPanel title={selectedTerminal.title || 'Terminal'} testId="termx-terminal-actions-sheet" onClose={() => setMobileSheet(null)}>
            <div className="flex flex-col gap-3">
              {selectedTerminal.state === 'exited' ? (
                <button
                  type="button"
                  className="termx-app-secondary-button min-h-12 w-full justify-between px-4 text-left text-[15px] font-medium"
                  onClick={() => { hapticImpact(); void restartManagedTerminal() }}
                >
                  <span>Restart terminal</span>
                  <RefreshCw className="h-4 w-4 text-zinc-500" />
                </button>
              ) : null}
              <button
                type="button"
                className="termx-app-secondary-button min-h-12 w-full justify-between px-4 text-left text-[15px] font-medium"
                onClick={() => { hapticImpact(); openEditTerminal() }}
              >
                <span>Edit terminal</span>
                <SquarePen className="h-4 w-4 text-zinc-500" />
              </button>
              <button
                type="button"
                className="flex min-h-12 w-full items-center justify-between border border-red-200 bg-red-50 px-4 text-left text-[15px] font-medium text-red-700"
                onClick={() => { hapticImpact(); void deleteManagedTerminal() }}
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
                  className="min-h-12 border border-[var(--termx-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
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
                    className="min-h-12 border border-[var(--termx-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
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
                  className="min-h-12 border border-[var(--termx-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                  value={terminalForm.cwd}
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setTerminalForm((current) => ({ ...current, cwd: value }))
                  }}
                />
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    className="termx-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
                    onClick={() => {
                      hapticImpact()
                      openTerminalPathPicker()
                    }}
                  >
                    <FolderOpen className="h-4 w-4" />
                    Browse
                  </button>
                  <button
                    type="button"
                    className="termx-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
                    onClick={() => {
                      hapticImpact()
                      openTerminalPathBookmarks()
                    }}
                  >
                    <Bookmark className="h-4 w-4" />
                    Bookmarks
                  </button>
                </div>
              </label>
              <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                Environment
                <input
                  className="min-h-12 border border-[var(--termx-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
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
                  className="min-h-12 border border-[var(--termx-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
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
                className="termx-app-primary-button mt-2 min-h-12 w-full gap-2 px-4 text-[15px] font-semibold"
                onClick={() => {
                  hapticImpact()
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
        {renderTerminalPathPickerSheet()}
        {renderTerminalPathBookmarksSheet()}
      </aside>
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
        <div className="termx-app-panel w-full max-w-md border-red-200 p-4 text-sm text-red-700" role="alert">
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
          <span className="termx-square-spinner text-zinc-600" aria-hidden="true" />
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
      {renderTerminalListPage()}

      <main
        className={`relative min-h-0 min-w-0 max-w-full flex-1 overflow-hidden bg-[var(--termx-terminal-bg)] ${page === 'terminal-list' ? 'hidden md:flex md:items-center md:justify-center md:bg-zinc-50/50' : 'grid grid-rows-[auto_minmax(0,1fr)_auto] md:grid-rows-[minmax(0,1fr)]'}`}
        data-testid="termx-terminal-page"
      >
        {page === 'terminal-list' ? (
          <div className="flex flex-col items-center gap-3 text-zinc-400">
            <Monitor className="h-12 w-12 opacity-20" />
            <p className="text-sm font-medium">Select a terminal to start</p>
          </div>
        ) : (
          <>
        <header
          className="relative z-30 row-start-1 flex min-h-12 min-w-0 max-w-full shrink-0 items-center justify-between gap-1 overflow-hidden border-b border-[var(--termx-border-subtle)] bg-[var(--termx-surface)] px-1.5 pt-[env(safe-area-inset-top)] md:hidden"
          data-testid="termx-terminal-header"
        >
          <div className="flex min-w-0 flex-1 items-center gap-1">
            <button
              type="button"
              aria-label="Back to terminal list / Show terminal list"
              className="flex h-11 w-11 shrink-0 items-center justify-center text-[var(--termx-muted)] transition-colors active:bg-[var(--termx-surface-raised)]"
              onClick={() => { hapticSelection(); showTerminalListPage() }}
            >
              <ChevronLeft className="h-5 w-5" />
            </button>
            <button
              type="button"
              aria-label="Switch terminal"
              className="flex min-h-11 min-w-0 flex-1 flex-col items-start justify-center px-1.5 py-0.5 text-left transition-colors active:bg-[var(--termx-surface-raised)]"
              onClick={() => { hapticSelection(); setMobileSheet('terminals') }}
            >
              <span className="max-w-full truncate text-[9px] font-bold uppercase tracking-wider text-[var(--termx-muted)]">{machine.name}</span>
              <span className="max-w-full truncate text-[12px] font-semibold leading-tight text-[var(--termx-text)]" data-testid="termx-terminal-title">{terminalHeaderTitle}</span>
              {terminalHeaderDirectory ? (
                <span className="max-w-full truncate text-[10px] font-medium leading-tight text-[var(--termx-muted)]">{terminalHeaderDirectory}</span>
              ) : null}
            </button>
          </div>

          <button
            type="button"
            aria-label="Open terminal menu"
            className="flex h-11 w-11 shrink-0 items-center justify-center text-[var(--termx-muted)] transition-colors active:bg-[var(--termx-surface-raised)]"
            onClick={() => { hapticSelection(); setMobileSheet('terminal-menu') }}
          >
            <MoreHorizontal className="h-5 w-5" />
          </button>
        </header>

        <div
          ref={terminalAreaRef}
          className="relative row-start-2 h-full min-h-0 min-w-0 flex-1 overflow-hidden bg-[var(--termx-terminal-bg)] md:row-start-1"
          data-testid="termx-terminal-body"
        >
          {terminalToolbarOpen ? (
            <TerminalActionToolbar
              mode={terminalToolbarMode}
              hasSelection={hasTerminalSelection}
              renderer={effectiveTerminalSettings.renderer}
              fontSize={effectiveTerminalSettings.fontSize}
              resizeControl={terminalResizeControl}
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
              onOpenClipboardHistory={openClipboardHistory}
              onOpenSnippets={() => {
                setTerminalFnOpen((current) => !current)
                setTerminalToolbarOpen(false)
              }}
              onRendererChange={(renderer) => updateTerminalSettings({ renderer })}
              onFontSizeChange={(fontSize) => updateTerminalSettings({ fontSize })}
              onAcquireResizeOwner={() => { void acquireActiveResizeOwner() }}
              onReleaseResizeOwner={() => { void releaseActiveResizeOwner() }}
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
            <div className="absolute inset-x-0 top-0 z-40 border-y border-red-500/30 bg-red-950/90 px-3 py-2 text-[12px] font-medium text-red-100 backdrop-blur" role="alert">
              {error}
            </div>
          ) : null}

          <div ref={terminalWrapperRef} className={`absolute inset-0 flex flex-col bg-[var(--termx-terminal-bg)] ${splitTerminalId ? 'gap-px' : ''}`}>
            <div
              className={`relative min-h-0 flex-1 overflow-hidden bg-[var(--termx-terminal-bg)] ${splitTerminalId ? `border-b border-[var(--termx-border-subtle)] ${activeTerminalSlot === 0 ? 'ring-1 ring-inset ring-[var(--termx-accent)]' : ''}` : ''}`}
              data-active-slot={activeTerminalSlot === 0 ? 'true' : 'false'}
              data-testid="termx-terminal-panel"
              onPointerDown={() => {
                if (activeTerminalSlot !== 0) hapticSelection()
                setActiveTerminalSlot(0)
                activeTerminalSlotRef.current = 0
              }}
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
                  onBufferChange={(isAlternate) => handleTerminalBufferChange(0, isAlternate)}
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
                  className={`absolute right-2 z-20 flex min-h-8 items-center gap-1.5 border border-[var(--termx-border-subtle)] bg-[var(--termx-overlay)] px-2 text-[11px] font-semibold text-[var(--termx-text)] backdrop-blur active:opacity-85 disabled:opacity-60 ${splitTerminalId ? 'top-16' : 'top-2'}`}
                  disabled={unlockingResize}
                  onClick={() => { hapticImpact(); void unlockTerminalResize() }}
                >
                  <Unlock className="h-3.5 w-3.5" />
                  {unlockingResize ? 'Unlocking...' : 'Unlock resize'}
                </button>
              ) : null}
            </div>

            {splitTerminalId ? (
              <div
                className={`relative min-h-0 flex-1 overflow-hidden bg-[var(--termx-terminal-bg)] ${activeTerminalSlot === 1 ? 'ring-1 ring-inset ring-[var(--termx-accent)]' : ''}`}
                data-active-slot={activeTerminalSlot === 1 ? 'true' : 'false'}
                data-testid="termx-split-terminal-panel"
                onPointerDown={() => {
                  if (activeTerminalSlot !== 1) hapticSelection()
                  setActiveTerminalSlot(1)
                  activeTerminalSlotRef.current = 1
                }}
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
                    onBufferChange={(isAlternate) => handleTerminalBufferChange(1, isAlternate)}
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
          className="relative z-20 row-start-3 w-full max-w-full"
          onInput={sendTerminalInput}
          onFocusKeyboard={focusActiveTerminal}
          onBlurKeyboard={blurActiveTerminal}
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

        {mobileSheet === 'terminal-menu' ? (
          <MobileSheetPanel title="Terminal tools" testId="termx-terminal-menu-sheet" onClose={() => setMobileSheet(null)}>
            <div className="grid grid-cols-2 border-l border-t border-[var(--termx-app-line)]">
              <button type="button" className="termx-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticImpact(); openSplitTerminalSheet() }}>
                <Rows2 className="h-4 w-4 text-[var(--termx-app-accent)]" />
                {splitTerminalId ? 'Change split' : 'Split terminal'}
              </button>
              <button type="button" className="termx-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticImpact(); setMobileSheet(null); void (activeTerminalOwnsResize ? releaseActiveResizeOwner() : acquireActiveResizeOwner()) }}>
                <span className="w-4 font-mono text-[11px] font-extrabold text-[var(--termx-app-accent)]">{resizeControlBadgeText(terminalResizeControl)}</span>
                {activeTerminalOwnsResize ? 'Release resize' : 'Control resize'}
              </button>
              {splitTerminalId ? (
                <>
                  <button type="button" className="termx-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticImpact(); setSyncSplitInput((current) => !current); setMobileSheet(null) }}>
                    {syncSplitInput ? <Link2 className="h-4 w-4 text-[var(--termx-app-accent)]" /> : <Link2Off className="h-4 w-4 text-[var(--termx-app-muted)]" />}
                    Sync input
                  </button>
                  <button type="button" className="termx-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticImpact(); closeSplitTerminal(); setMobileSheet(null) }}>
                    <PanelBottomClose className="h-4 w-4 text-[var(--termx-app-danger)]" />
                    Close split
                  </button>
                </>
              ) : null}
              <button type="button" className="termx-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticSelection(); setMobileSheet(null); setTerminalToolbarOpen((current) => { const next = !current; if (next) setTerminalFnOpen(false); if (!next) setTerminalToolbarModeAndReset('default'); return next }) }}>
                <SlidersHorizontal className="h-4 w-4 text-[var(--termx-app-accent)]" />
                Terminal tools
              </button>
              <button type="button" className="termx-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticSelection(); setMobileSheet(null); openConnectionInfo() }}>
                <Info className="h-4 w-4 text-[var(--termx-app-accent)]" />
                Connection
              </button>
              <button type="button" className="termx-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticSelection(); setMobileSheet(null); openFiles() }}>
                <Folder className="h-4 w-4 text-[var(--termx-app-accent)]" />
                Files
              </button>
            </div>
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
              <div className="flex min-h-24 items-center justify-center border border-dashed border-zinc-300 bg-white text-sm font-medium text-zinc-500">
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
        {renderClipboardHistorySheet()}
          </>
        )}
      </main>

      <div
        className={`absolute inset-0 z-30 flex flex-col bg-white transition-transform duration-200 md:left-auto md:right-0 md:w-[450px] md:border-l md:border-[var(--termx-app-line)] ${filesOpen ? 'translate-y-0 md:translate-x-0 visible' : 'translate-y-full md:translate-y-0 md:translate-x-full invisible'}`}
        data-testid="termx-machine-files-overlay"
      >
        <div className="termx-app-header flex shrink-0 items-center justify-between border-b px-4 pb-2 pt-[calc(env(safe-area-inset-top)+0.5rem)] md:h-14 md:pb-0 md:pt-0">
          <div className="flex items-center gap-2">
            <Folder className="h-5 w-5 text-zinc-500" />
            <span className="text-[17px] font-bold tracking-tight text-zinc-900">Files</span>
          </div>
          <button
            type="button"
            aria-label="Close files"
            className="termx-app-icon-button border-transparent bg-transparent"
            onClick={() => { hapticSelection(); openTerminalPanel() }}
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
            {showMachineNetworkOverlay ? null : filesOpen ? (
              <div className="flex items-center gap-2">
                <span className="termx-square-spinner" aria-hidden="true" />
                <span>Connecting...</span>
              </div>
            ) : 'File access is not ready'}
          </div>
        )}
      </div>

      <div className={`pointer-events-none absolute bottom-8 left-1/2 z-50 flex -translate-x-1/2 transform flex-col items-center gap-2 transition-all duration-300 ${pairStatus ? 'translate-y-0 opacity-100' : 'translate-y-4 opacity-0'}`}>
        <div className="flex items-center gap-2 border border-white/10 bg-zinc-900/95 px-4 py-2.5 text-sm font-medium text-white backdrop-blur-md" role="status" aria-live="polite">
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
          resolveMachineLabel={() => machine.name}
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
    <div className="absolute inset-0 z-40 flex items-end bg-black/40 backdrop-blur-sm transition-opacity md:items-center md:justify-center" data-testid={testId} onClick={() => { hapticSelection(); onClose() }}>
      <section
        className="termx-app-page relative max-h-[85vh] w-full overflow-hidden border-t border-[var(--termx-app-line)] md:max-w-md md:border"
        onClick={(e) => e.stopPropagation()}
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        <div className="absolute left-1/2 top-3 h-1 w-12 -translate-x-1/2 bg-[var(--termx-app-line-strong)] md:hidden" />
        <header className="flex h-16 items-center justify-between border-b border-[var(--termx-app-line)] px-5 pt-3">
          <h2 className="text-[17px] font-bold tracking-tight text-zinc-900">{title}</h2>
          <button
            type="button"
            aria-label={`Close ${title}`}
            className="termx-app-icon-button border-transparent bg-transparent"
            onClick={() => { hapticSelection(); onClose() }}
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
    <div className="absolute inset-0 z-50 flex items-center justify-center bg-black/45 p-4 backdrop-blur-sm" role="dialog" aria-modal="true" onClick={() => { hapticSelection(); onClose() }}>
      <section className="termx-app-panel w-full max-w-md overflow-hidden" onClick={(event) => event.stopPropagation()}>
        <header className="flex items-center justify-between gap-3 border-b border-zinc-200 px-4 py-3">
          <div className="min-w-0">
            <h2 className="text-[15px] font-semibold text-zinc-950">Connection Info</h2>
            <p className="mt-0.5 text-[12px] font-medium text-zinc-500">{connectionTypeLabel(type)}</p>
          </div>
          <button type="button" aria-label="Close connection info" className="termx-app-icon-button border-transparent bg-transparent" onClick={() => { hapticSelection(); onClose() }}>
            <X className="h-5 w-5" />
          </button>
        </header>

        <div className="space-y-3 px-4 py-4">
          {error ? (
            <div className="border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] font-medium text-amber-800">{error}</div>
          ) : null}
          <div className="grid grid-cols-1 gap-2">
            <ConnectionInfoRow label="Mode" value={loading ? 'Reading stats...' : connectionTypeLabel(type)} strong />
            <ConnectionInfoRow label="Transport" value={info?.path ?? '-'} />
            <ConnectionInfoRow label="Path" value={info?.observedPath ?? '-'} />
            <ConnectionInfoRow label="SmartRoute" value={info?.routeSelectionReason ?? '-'} />
            <ConnectionInfoRow label="Local" value={info?.localAddr ?? '-'} />
            <ConnectionInfoRow label="Remote" value={info?.remoteAddr ?? '-'} />
            <ConnectionInfoRow label="Candidates" value={candidateTypeText(info)} />
            <ConnectionInfoRow label="RTT" value={info?.rtt !== undefined ? `${Math.round(info.rtt)} ms` : '-'} />
            <ConnectionInfoRow label="Connection" value={info?.connectionId ?? '-'} />
          </div>
        </div>

        <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-zinc-200 px-4 py-3">
          <button type="button" className="termx-app-secondary-button px-3 text-[13px] font-semibold" onClick={() => { hapticImpact(); onRefresh() }}>
            Refresh
          </button>
          <button type="button" className="termx-app-secondary-button px-3 text-[13px] font-semibold" onClick={() => { hapticImpact(); onReconnect() }}>
            Reconnect
          </button>
          <button
            type="button"
            className="termx-app-primary-button px-3 text-[13px] font-semibold disabled:bg-zinc-300 disabled:text-zinc-500"
            disabled={loading || !canToggleMode}
            onClick={() => { hapticImpact(); onToggleMode() }}
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
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-start gap-3 border-b border-[var(--termx-app-line)] bg-zinc-50 px-3 py-2 last:border-b-0">
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

function closeTerminalDataChannel(session: RtcSession, terminalId: string): void {
  const controller = session as RtcSession & Partial<RtcTerminalDataChannelController>
  if (typeof controller.closeTerminalDataChannel === 'function') {
    controller.closeTerminalDataChannel(terminalId)
  }
}

function isDirectoryEntry(entry: FileEntry): boolean {
  return entry.type === 'dir' || entry.type === 'symlink-dir'
}

function formatClipboardTimestamp(value: string): string {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return value
  return new Date(timestamp).toLocaleString(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function isTransientConnectionPhase(phase: RtcConnectionStateSnapshot['phase']): boolean {
  return phase === 'probing' || phase === 'connecting'
}

function connectionErrorDisplayMessage(error: unknown): string {
  if (isAuthConnectionError(error)) return AUTH_CONNECTION_MESSAGE
  if (error instanceof Error) return error.message
  return String(error)
}

function isAuthConnectionError(error: unknown): boolean {
  const message = typeof error === 'string'
    ? error
    : error instanceof Error
      ? error.message
      : ''
  const normalized = message.trim().toLowerCase()
  return normalized === 'auth' ||
    normalized === 'authentication failed' ||
    normalized === 'unauthenticated' ||
    normalized === 'capability_invalid' ||
    normalized === 'capability_expired' ||
    normalized === 'device_identity_mismatch' ||
    normalized === 'scope_invalid' ||
    // 本地缓存的 runtime token 失效时，恢复路径必须回到重新配对。
    normalized.includes('stored session token is invalid') ||
    normalized.includes('pair this machine again') ||
    normalized.includes('invalid session token') ||
    normalized.includes('unauthorized') ||
    normalized.includes('forbidden')
}

function normalizeDirectoryPickerPath(path: string): string {
  return normalizeTerminalDirectory(path) || '/'
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
  if (!trimmed.startsWith('/')) return ''
  return normalizeFilePath(trimmed)
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

function normalizeRuntimeTerminalEvent(payload: Record<string, unknown>): RemoteTerminal | null {
  const terminal = payload.terminal
  if (typeof terminal !== 'object' || terminal === null || Array.isArray(terminal)) return null
  const record = terminal as Record<string, unknown>
  const terminalId = record.terminal_id ?? record.terminalId ?? record.id ?? record.ID
  const machineId = record.machine_id ?? record.machineId
  if (typeof terminalId !== 'string' || !terminalId.trim()) return null
  if (typeof machineId !== 'string' || !machineId.trim()) return null
  return {
    terminalId: terminalId.trim(),
    machineId: machineId.trim(),
    title: typeof record.title === 'string' && record.title.trim()
      ? record.title.trim()
      : typeof record.name === 'string' && record.name.trim()
        ? record.name.trim()
        : terminalId.trim(),
    state: record.state === 'running' || record.state === 'exited' ? record.state : 'unknown',
    command: typeof record.command === 'string'
      ? record.command
      : Array.isArray(record.command) && record.command.every((item) => typeof item === 'string')
        ? record.command.join(' ')
        : undefined,
    cols: typeof record.cols === 'number' ? record.cols : undefined,
    rows: typeof record.rows === 'number' ? record.rows : undefined,
    cwd: typeof record.cwd === 'string' ? record.cwd : undefined,
    environment: typeof record.environment === 'string' ? record.environment : undefined,
    sizeLocked: record.size_locked === true || record.sizeLocked === true,
    sizeLockMode: record.size_lock_mode === 'off' || record.size_lock_mode === 'warn' || record.size_lock_mode === 'lock'
      ? record.size_lock_mode
      : undefined,
    resizeOwnerSurfaceId: resizeOwnershipString(record, 'owner_surface_id'),
    resizeOwnerViewId: resizeOwnershipString(record, 'owner_view_id'),
    resizeOwnerAttachmentCount: typeof record.resize_owner_attachment_count === 'number'
      ? record.resize_owner_attachment_count
      : undefined,
  }
}

function resizeControlFromRuntimeTerminal(terminal: RemoteTerminal, surfaceId: string): TerminalResizeControl {
  if (terminal.sizeLocked) {
    return { canResize: false, reason: 'size_locked', sizeLocked: true }
  }
  if (terminal.resizeOwnerSurfaceId && terminal.resizeOwnerSurfaceId === surfaceId) {
    return {
      canResize: true,
      reason: 'owner',
      surfaceId,
      ownerSurfaceId: terminal.resizeOwnerSurfaceId,
      ...(terminal.resizeOwnerViewId ? { ownerViewId: terminal.resizeOwnerViewId } : {}),
    }
  }
  if (terminal.resizeOwnerSurfaceId) {
    return {
      canResize: false,
      reason: 'follower',
      surfaceId,
      ownerSurfaceId: terminal.resizeOwnerSurfaceId,
      ...(terminal.resizeOwnerViewId ? { ownerViewId: terminal.resizeOwnerViewId } : {}),
    }
  }
  return {
    canResize: false,
    reason: 'unknown',
    surfaceId,
  }
}

function resizeOwnershipString(record: Record<string, unknown>, key: 'owner_surface_id' | 'owner_view_id'): string | undefined {
  const direct = record[key]
  if (typeof direct === 'string' && direct.trim()) return direct.trim()
  const ownership = record.resize_ownership
  if (typeof ownership !== 'object' || ownership === null || Array.isArray(ownership)) return undefined
  const nested = (ownership as Record<string, unknown>)[key]
  return typeof nested === 'string' && nested.trim() ? nested.trim() : undefined
}

function appTerminalSurfaceId(machineId: string, terminalId: string): string {
  return `app:${machineId}:terminal:${terminalId}`
}

function resizeControlBadgeText(control: TerminalResizeControl): string {
  if (control.sizeLocked || control.reason === 'size_locked') return 'LK'
  if (control.canResize) return 'OW'
  if (control.reason === 'follower') return 'FL'
  if (control.reason === 'observer') return 'OB'
  return 'FL'
}
