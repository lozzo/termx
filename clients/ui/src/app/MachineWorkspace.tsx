import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react'
import { create } from '@bufbuild/protobuf'
import { Bookmark, BookmarkMinus, BookmarkPlus, ChevronLeft, ClipboardList, Folder, FolderOpen, Info, KeyRound, Link2, Link2Off, Monitor, MoreHorizontal, PanelBottomClose, Plus, RefreshCw, Rows2, SlidersHorizontal, SquarePen, Trash2, Unlock, X } from 'lucide-react'
import { connectionPhaseLabel, connectionSnapshotFromStatus } from '../connection/connectionState'
import { FileTransferPanel } from '../files/FileTransferPanel'
import { FileManager } from '../files/FileManager'
import { createFileApi, type FileEntry } from '../files/fileApi'
import { joinPath, normalizeFilePath, parentPath } from '../files/fileUtils'
import { createPathBookmarkApi, type PathBookmark } from '../files/pathBookmarks'
import { hapticImpact, hapticSelection } from '../platform/haptics'
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
import type { ProtoClientSession } from '../core/protoClientSession'
import { openProtoEventSubscription } from '../core/protoEventSubscription'
import { ApplicationEventType, EventSubscribeCommandSchema } from '../generated/apipb/events_pb'
import type { ConnectionInfo, ConnectionPolicy, ConnectionPolicyState, LocalAgentApi, LocalCreateTerminalInput, LocalUpdateTerminalInput, MachineConnectionStateEvents, RtcConnectOptions, RtcConnectionStateSnapshot, RtcEvent, RtcSubscription, TerminalInventoryEvents } from '../core/transport'
import { ConnectionCandidateType, ConnectionRouteKind, ConnectionTransport } from '../generated/bindingpb/client_binding_pb'
import { ObservedPath as CloudObservedPath } from '../generated/cloudpb/cloud_topology_pb'
import { useTerminalKeyboard } from '../terminal/useTerminalKeyboard'
import { useTranslation } from 'react-i18next'
import '../i18n'

export interface MachineWorkspaceInventoryApi extends Pick<LocalAgentApi, 'getStatus'> {
  listTerminals(options?: Pick<RtcConnectOptions, 'forceRelay' | 'onStatus' | 'onConnectionState'>): Promise<RemoteTerminal[]>
}

export interface MachineWorkspaceSessionInput {
  machineId: string
}

export type MachineWorkspaceClientSession = ProtoClientSession

export type MachineWorkspaceConnector = {
  connect(input: MachineWorkspaceSessionInput, options?: RtcConnectOptions): Promise<MachineWorkspaceClientSession>
  reconnect?: ((options?: { forceRelay?: boolean | undefined }) => void | Promise<void>) | undefined
  getConnectionPolicy?: ((signal?: AbortSignal) => Promise<ConnectionPolicyState>) | undefined
  applyConnectionPolicy?: ((policy: ConnectionPolicy, signal?: AbortSignal) => Promise<void>) | undefined
}

export interface MachineWorkspaceProps {
  api: MachineWorkspaceInventoryApi
  connector: MachineWorkspaceConnector
  className?: string | undefined
  initialMachine?: Machine | undefined
  inventoryEvents?: TerminalInventoryEvents | undefined
  connectionStateEvents?: MachineConnectionStateEvents | undefined
  subscribeRuntimeInventoryEvents?: boolean | undefined
  onBack?: (() => void) | undefined
  fileTransfer?: import('../files/fileApi').FileTransferContext | undefined
  terminalSettings?: TerminalSettings | undefined
  onNeedsReauthorization?: ((machineId: string) => void) | undefined
  onTerminalSettingsChange?: ((patch: Partial<TerminalSettings>) => void) | undefined
}

type TerminalEditorSheet = 'create-terminal' | 'edit-terminal'
type MobileSheet = 'terminals' | 'terminal-menu' | 'split-terminal' | 'manage-terminal' | TerminalEditorSheet | 'terminal-path-picker' | 'terminal-path-bookmarks' | 'clipboard-history' | null
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

export function MachineWorkspace({ api, connector, className, initialMachine, inventoryEvents, connectionStateEvents, subscribeRuntimeInventoryEvents = false, onBack, fileTransfer, terminalSettings: terminalSettingsProp, onNeedsReauthorization, onTerminalSettingsChange }: MachineWorkspaceProps) {
  const { t } = useTranslation()
  const initialInventory = initialMachine ? inventoryCacheForConnector(connector).get(initialMachine.machineId) : undefined
  const [machine, setMachine] = useState<Machine | null>(() => initialInventory?.machine ?? initialMachine ?? null)
  const [terminals, setTerminals] = useState<RemoteTerminal[]>(() => initialInventory?.terminals ?? [])
  const [hasLoadedTerminals, setHasLoadedTerminals] = useState(() => Boolean(initialInventory))
  const [loadingTerminals, setLoadingTerminals] = useState(() => !initialInventory)
  const [activeTerminalId, setActiveTerminalId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pairStatus, setPairStatus] = useState<string | null>(null)
  const [verifiedDevice, setVerifiedDevice] = useState<boolean | null>(true)
  const [connectedSession, setConnectedSession] = useState<MachineWorkspaceClientSession | null>(null)
  const [connectedTerminalId, setConnectedTerminalId] = useState<string | null>(null)
  const [connectingTerminalId, setConnectingTerminalId] = useState<string | null>(null)
  const [fileTerminalId, setFileTerminalId] = useState<string | null>(null)
  const [fileInitialPath, setFileInitialPath] = useState('/')
  const [fileContextKey, setFileContextKey] = useState('machine:/')
  const [connectionRetryToken, setConnectionRetryToken] = useState(0)
  const [forceRelayConnection, setForceRelayConnection] = useState<boolean | undefined>(undefined)
  const [connectionInfoOpen, setConnectionInfoOpen] = useState(false)
  const [connectionInfo, setConnectionInfo] = useState<ConnectionInfo | null>(null)
  const [connectionInfoLoading, setConnectionInfoLoading] = useState(false)
  const [connectionInfoError, setConnectionInfoError] = useState<string | null>(null)
  const [connectionPolicyState, setConnectionPolicyState] = useState<ConnectionPolicyState | null>(null)
  const [connectionPolicyApplying, setConnectionPolicyApplying] = useState(false)
  const connectionPolicyReconnectPendingRef = useRef(false)
  const connectionPolicyFailureRef = useRef<{ stage: 'refresh' | 'apply' | 'reconnect'; policy?: ConnectionPolicy } | null>(null)
  const [p2pFallbackPromptOpen, setP2PFallbackPromptOpen] = useState(false)
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
  const [terminalSubmitError, setTerminalSubmitError] = useState<string | null>(null)
  const [terminalSubmitting, setTerminalSubmitting] = useState(false)
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
    session: MachineWorkspaceClientSession
    forceRelay: boolean | undefined
  } | null>(null)
  const machineSessionPromiseRef = useRef<{
    connector: MachineWorkspaceConnector
    machineId: string
    retryToken: number
    forceRelay: boolean | undefined
    promise: Promise<MachineWorkspaceClientSession>
  } | null>(null)
  const machineSessionConnectSeqRef = useRef(0)
  const terminalRefreshSeqRef = useRef(0)
  const runtimeInventorySubscriptionRef = useRef<{
    connector: MachineWorkspaceConnector
    machineId: string
    retryToken: number
    session: MachineWorkspaceClientSession
    subscription: { close(): void }
  } | null>(null)
  const connectionStateSubscriptionRef = useRef<RtcSubscription | null>(null)
  const passiveConnectionPhaseRef = useRef<RtcConnectionStateSnapshot['phase'] | null>(null)
  const sessionConnectionPhaseRef = useRef<RtcConnectionStateSnapshot['phase'] | null>(null)
  const latestActiveTerminalIdRef = useRef<string | null>(null)
  const latestSplitTerminalIdRef = useRef<string | null>(null)
  const p2pProbeRef = useRef(false)
  const handledManualReconnectNonceRef = useRef(0)
  const resizeLockedHintShownRef = useRef(false)
  const hasLoadedTerminalsRef = useRef(hasLoadedTerminals)
  const activeTerminal = terminals.find((terminal) => terminal.terminalId === activeTerminalId)
  const splitTerminal = terminals.find((terminal) => terminal.terminalId === splitTerminalId)
  const activeToolTerminal = activeTerminalSlot === 1 && splitTerminal ? splitTerminal : activeTerminal
  const selectedTerminal = terminals.find((terminal) => terminal.terminalId === selectedTerminalId)
  const activeTerminalTitle = activeTerminal?.title || activeTerminal?.command || activeTerminalId || t('terminal.defaultTitle')
  const splitTerminalTitle = splitTerminal?.title || splitTerminal?.command || splitTerminalId || t('terminal.defaultTitle')
  const terminalHeaderTitle = splitTerminalId ? `${activeTerminalTitle} / ${splitTerminalTitle}` : activeTerminalTitle
  const terminalHeaderDirectory = activeToolTerminal?.cwd || activeTerminal?.cwd || splitTerminal?.cwd || ''
  const activeTerminalResizeLocked = terminalResizeControl.sizeLocked === true || terminalResizeControl.reason === 'size_locked'
  const activeTerminalOwnsResize = terminalResizeControl.canResize === true
  const requireVerification = verifiedDevice === false
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
    // managed endpoint 的持久策略固定为 auto；forceRelay 只属于当前 workspace 的一次连接尝试。
    setForceRelayConnection(false)
    p2pProbeRef.current = false
    setP2PFallbackPromptOpen(false)
  }, [machine?.machineId])

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
    setVerifiedDevice(false)
    onNeedsReauthorization?.(targetMachineId)
  }, [initialMachine?.machineId, onNeedsReauthorization])

  const updateFromConnectionState = useCallback((snapshot: RtcConnectionStateSnapshot, session?: MachineWorkspaceClientSession) => {
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
    connectionPolicyFailureRef.current = null
    if (connectionPolicyReconnectPendingRef.current) {
    connectionPolicyReconnectPendingRef.current = false
    setConnectionPolicyApplying(false)
    setConnectionInfoOpen(false)
    }
      return
    }
    if (snapshot.phase === 'idle') {
      clearConnectionStatus()
      return
    }
    if (snapshot.phase === 'reconnecting' || snapshot.phase === 'waiting_network') setError(null)
    if (snapshot.phase === 'failed') {
      const message = connectionErrorDisplayMessage(snapshot.failReason || snapshot.statusText || 'Connection failed')
      if (p2pProbeRef.current && isP2PRouteUnavailable(snapshot.failReason || snapshot.statusText)) {
        p2pProbeRef.current = false
        setP2PFallbackPromptOpen(true)
      }
      if (isAuthConnectionError(snapshot.failReason || snapshot.statusText)) handleConnectionAuthFailure(snapshot.machineId)
      setError(message)
    if (connectionPolicyReconnectPendingRef.current) {
    connectionPolicyReconnectPendingRef.current = false
    setConnectionPolicyApplying(false)
    setConnectionInfoError(message)
    connectionPolicyFailureRef.current = { stage: 'reconnect' }
    setConnectionInfoOpen(true)
    clearConnectionStatus()
    return
    }
      updateConnectionStatus(message, 'failed')
      return
    }
    updateConnectionStatus(snapshot.statusText || connectionPhaseLabel(snapshot.phase), snapshot.phase)
  }, [clearConnectionStatus, clearConnectionStatusSoon, handleConnectionAuthFailure, updateConnectionStatus])

  const updateFromPassiveConnectionState = useCallback((snapshot: RtcConnectionStateSnapshot, session?: MachineWorkspaceClientSession) => {
    if (isTransientConnectionPhase(snapshot.phase)) return
    updateFromConnectionState(snapshot, session)
  }, [updateFromConnectionState])

  const reattachActiveTerminals = useCallback((session: MachineWorkspaceClientSession) => {
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
    if (current) void closeMachineWorkspaceSession(current.session)
  }, [])

  const attachConnectionStateSubscription = useCallback((session: MachineWorkspaceClientSession) => {
    connectionStateSubscriptionRef.current?.close()
    sessionConnectionPhaseRef.current = null
    connectionStateSubscriptionRef.current = null
  }, [])

  const releaseMachineSession = useCallback(() => {
    disconnectMachineSession()
    setConnectedSession(null)
    setConnectedTerminalId(null)
    setConnectingTerminalId(null)
  }, [disconnectMachineSession])

  const ensureMachineSession = useCallback(async (machineId: string, connectOptions?: RtcConnectOptions): Promise<MachineWorkspaceClientSession> => {
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
      if (isProtoSessionAlive(reusable.session)) return reusable.session
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
      forceRelay: boolean | undefined
      promise: Promise<MachineWorkspaceClientSession>
    } = {
      connector,
      machineId,
      retryToken: connectionRetryToken,
      forceRelay,
      promise: Promise.resolve(null as unknown as MachineWorkspaceClientSession),
    }
    entry.promise = connector.connect({ machineId }, effectiveConnectOptions).then((session) => {
      if (machineSessionPromiseRef.current !== entry) {
        void closeMachineWorkspaceSession(session)
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
      const terminalList = await api.listTerminals({ forceRelay: forceRelayConnection })
      if (terminalRefreshSeqRef.current !== seq) return
      setTerminals(terminalList)
      setHasLoadedTerminals(true)
      setError(null)
      // generation 恢复可能先收到旧 binding 的失败，再由新 session 成功提交 inventory。
      // 新 inventory 是当前 workspace 的成功真值，必须同时清除旧 generation 留下的网络遮罩。
      clearConnectionStatus()
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
  }, [api, clearConnectionStatus, forceRelayConnection, handleConnectionAuthFailure, initialMachine?.machineId, setMachineNetworkMachineId, updateConnectionStatus])

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
        const cachedInventory = inventoryCacheForConnector(connector).get(status.machine.machineId)
        if (cachedInventory && !hasLoadedTerminalsRef.current) {
          setTerminals(cachedInventory.terminals)
          setHasLoadedTerminals(true)
          setLoadingTerminals(false)
        }
        const terminalList = await api.listTerminals({
          forceRelay: false,
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
  }, [api, clearConnectionStatus, connector, handleConnectionAuthFailure, initialMachine?.machineId, setMachineNetworkMachineId, updateConnectionStatus])

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
        (!activeSession || !isProtoSessionAlive(activeSession) || recoveredFromInterruption)
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
      const subscription = subscribeMachineWorkspaceEvents(session, (event) => {
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
      if (isProtoSessionAlive(reusable.session)) {
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
      setError(null)
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
        const authFailure = isAuthConnectionError(err)
        if (authFailure) handleConnectionAuthFailure(machineId)
        if (p2pProbeRef.current && isP2PRouteUnavailable(err)) {
          p2pProbeRef.current = false
          setP2PFallbackPromptOpen(true)
        }
        setConnectedSession(null)
        setConnectedTerminalId(null)
        setConnectingTerminalId(null)
        updateConnectionStatus(message, 'failed')
      }
    })
    return () => {
      cancelled = true
      window.clearTimeout(progressTimer)
    }
  }, [activeTerminalId, clearConnectionStatus, clearConnectionStatusSoon, connector, connectionRetryToken, ensureMachineSession, forceRelayConnection, handleConnectionAuthFailure, machine?.machineId, manualReconnectNonce, page, releaseMachineSession, updateConnectionStatus, updateFromConnectionState])

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
      // 只有本次 manual reconnect 真正完成，才能消费 one-shot P2P probe；旧 Relay 的迟到快照无权清理该意图。
      if (p2pProbeRef.current && !forceRelayConnection) {
        p2pProbeRef.current = false
        setP2PFallbackPromptOpen(false)
      }
      setError(null)
      setConnectedSession(session)
      if (page === 'terminal' && activeTerminalId) {
        reattachActiveTerminals(session)
      }
      updateFromConnectionState({
        machineId,
        phase: 'connected',
        statusText: 'Connected',
        relayInUse: forceRelayConnection === true,
      }, session)
    }).catch((err: unknown) => {
      if (cancelled) return
      const message = connectionErrorDisplayMessage(err)
      if (isAuthConnectionError(err)) handleConnectionAuthFailure(machineId)
      if (p2pProbeRef.current && isP2PRouteUnavailable(err)) {
        p2pProbeRef.current = false
        setP2PFallbackPromptOpen(true)
      }
      setConnectedSession(null)
      setConnectedTerminalId(null)
      setConnectingTerminalId(null)
      updateFromConnectionState({
        machineId,
        phase: 'failed',
        statusText: message,
        relayInUse: forceRelayConnection === true,
        failReason: message,
      })
    })
    return () => {
      cancelled = true
    }
  }, [activeTerminalId, ensureMachineSession, forceRelayConnection, handleConnectionAuthFailure, machine?.machineId, manualReconnectNonce, page, reattachActiveTerminals, requireVerification, updateConnectionStatus, updateFromConnectionState])

  useEffect(() => {
    const handleResume = () => {
      resetKeyboardLayout()
      if (page === 'terminal-list') {
        // 网络 generation 更换时 terminal list 也必须重新取得 Go-owned session；没有已打开 terminal
        // 不代表 workspace 可以继续展示旧 inventory projection。
        void refreshTerminals()
        return
      }
      if (page !== 'terminal') return
      const session = machineSessionRef.current?.session ?? connectedSession
      if (!activeTerminalId && !splitTerminalId) return
      if (!session || !isProtoSessionAlive(session)) {
        setManualReconnectNonce((value) => value + 1)
        return
      }

      setConnectedSession(session)
      setError(null)
      reattachActiveTerminals(session)
    }
    document.addEventListener('muxvia:resume', handleResume)
    document.addEventListener('muxvia:binding-closed', handleResume)
    return () => {
      document.removeEventListener('muxvia:resume', handleResume)
      document.removeEventListener('muxvia:binding-closed', handleResume)
    }
  }, [activeTerminalId, connectedSession, page, reattachActiveTerminals, refreshTerminals, resetKeyboardLayout, splitTerminalId])

  const openTerminal = useCallback((intent: { machineId: string; terminalId: string }) => {
    if (requireVerification) {
      handleConnectionAuthFailure(intent.machineId)
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
  }, [handleConnectionAuthFailure, machine, requireVerification, splitTerminalId])

  const openSplitTerminalSheet = useCallback(() => {
    if (requireVerification) {
      handleConnectionAuthFailure(machine?.machineId)
      return
    }
    if (!activeTerminalId) {
      setError(t('workspace.openBeforeSplit'))
      return
    }
    const availableTerminals = terminals.filter((terminal) => terminal.terminalId !== activeTerminalId)
    if (availableTerminals.length === 0) {
      setPairStatus(t('workspace.noOtherTerminal'))
      return
    }
    setTerminalToolbarOpen(false)
    setTerminalFnOpen(false)
    setMobileSheet('split-terminal')
  }, [activeTerminalId, handleConnectionAuthFailure, machine?.machineId, requireVerification, terminals])

  const selectSplitTerminal = useCallback((intent: { machineId: string; terminalId: string }) => {
    if (machine && intent.machineId !== machine.machineId) {
      setError(`terminal machine mismatch: ${intent.machineId} != ${machine.machineId}`)
      return
    }
    if (intent.terminalId === activeTerminalId) {
      setPairStatus(t('workspace.chooseDifferentTerminal'))
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

  const retryConnection = useCallback(async (options: { forceRelay?: boolean; closeDialog?: boolean; p2pProbe?: boolean; preservePolicy?: boolean } = {}) => {
  const targetForceRelay = options.preservePolicy ? undefined : (options.forceRelay ?? forceRelayConnection)
    p2pProbeRef.current = options.p2pProbe === true
    setP2PFallbackPromptOpen(false)
    setForceRelayConnection(targetForceRelay)
    if (options.closeDialog !== false) setConnectionInfoOpen(false)
    setConnectionInfo(null)
    setConnectionInfoError(null)
    updateConnectionStatus(targetForceRelay ? 'Reconnecting through relay...' : 'Reconnecting...', 'reconnecting')
    if (connector.reconnect) {
      const current = machineSessionRef.current
    await connector.reconnect({ forceRelay: targetForceRelay })
      machineSessionConnectSeqRef.current += 1
      connectionStateSubscriptionRef.current?.close()
      connectionStateSubscriptionRef.current = null
      machineSessionPromiseRef.current = null
      machineSessionRef.current = null
      const runtimeInventorySubscription = runtimeInventorySubscriptionRef.current
      runtimeInventorySubscriptionRef.current = null
      runtimeInventorySubscription?.subscription.close()
      if (current) void closeMachineWorkspaceSession(current.session)
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
  }, [activeTerminalId, connector, forceRelayConnection, releaseMachineSession, updateConnectionStatus])

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
      handleConnectionAuthFailure(machine?.machineId)
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
      })
    setFilesOpen(true)
    setMobileSheet(null)
  }, [activeToolTerminal, ensureMachineSession, fileContextKey, filesOpen, forceRelayConnection, handleConnectionAuthFailure, machine, page, requireVerification, terminals, updateConnectionStatus, updateFromConnectionState])

  const openManageTerminal = useCallback((intent: { machineId: string; terminalId: string }) => {
    if (requireVerification) {
      handleConnectionAuthFailure(intent.machineId)
      return
    }
    if (!canManageTerminals) return
    if (machine && intent.machineId !== machine.machineId) {
      setError(`terminal machine mismatch: ${intent.machineId} != ${machine.machineId}`)
      return
    }
    setSelectedTerminalId(intent.terminalId)
    setMobileSheet('manage-terminal')
  }, [canManageTerminals, handleConnectionAuthFailure, machine, requireVerification])

  const openCreateTerminal = useCallback(() => {
    if (requireVerification) {
      handleConnectionAuthFailure(machine?.machineId)
      return
    }
    if (!canManageTerminals) return
    setSelectedTerminalId(null)
    setTerminalSubmitError(null)
    setTerminalForm({
      name: '',
      command: '',
      cwd: '',
      environment: '',
      sizeLockMode: 'off',
    })
    setMobileSheet('create-terminal')
  }, [canManageTerminals, handleConnectionAuthFailure, machine?.machineId, requireVerification])

  const openEditTerminal = useCallback(() => {
    if (!selectedTerminal) return
    setTerminalSubmitError(null)
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
    if (!canManageTerminals || terminalSubmitting) return
    setTerminalSubmitError(null)
    setTerminalSubmitting(true)
    const command = terminalForm.command.trim().split(/\s+/).filter(Boolean)
    const input: LocalCreateTerminalInput = {
      name: terminalForm.name.trim() || undefined,
      ...(command.length > 0 ? { command } : {}),
      cwd: terminalForm.cwd.trim() || undefined,
      environment: terminalForm.environment.trim() || undefined,
      sizeLockMode: terminalForm.sizeLockMode,
    }
    try {
      const management = await withManagementApi()
      const created = await management.api.createTerminal(input)
      await refreshTerminals()
      setPairStatus(`Created ${created.terminalId || input.name || 'terminal'}`)
      setMobileSheet(null)
    } catch (err) {
      setTerminalSubmitError(err instanceof Error ? err.message : String(err))
    } finally {
      setTerminalSubmitting(false)
    }
  }, [canManageTerminals, refreshTerminals, terminalForm, terminalSubmitting, withManagementApi])

  const submitUpdateTerminal = useCallback(async () => {
    if (!canManageTerminals || !selectedTerminalId || terminalSubmitting) return
    setTerminalSubmitError(null)
    setTerminalSubmitting(true)
    const input: LocalUpdateTerminalInput = {
      terminalId: selectedTerminalId,
      name: terminalForm.name.trim() || undefined,
      cwd: terminalForm.cwd.trim() || undefined,
      environment: terminalForm.environment.trim() || undefined,
      sizeLockMode: terminalForm.sizeLockMode,
    }
    try {
      const management = await withManagementApi()
      await management.api.updateTerminal(input)
      await refreshTerminals()
      setPairStatus(`Updated ${input.name || selectedTerminal?.title || selectedTerminalId}`)
      setMobileSheet(null)
    } catch (err) {
      setTerminalSubmitError(err instanceof Error ? err.message : String(err))
    } finally {
      setTerminalSubmitting(false)
    }
  }, [canManageTerminals, selectedTerminal, selectedTerminalId, refreshTerminals, terminalForm, terminalSubmitting, withManagementApi])

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
      setPairStatus(t('workspace.resize.unlocked'))
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
        setPairStatus(t('workspace.resize.acquired'))
        window.setTimeout(() => {
          activeTerminalHandle()?.fit()
        }, 0)
      } else if (control?.sizeLocked || control?.reason === 'size_locked') {
        setPairStatus(t('workspace.resize.locked'))
      } else {
        setPairStatus(t('workspace.resize.unavailable'))
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
      setPairStatus(t('workspace.resize.released'))
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
      setConnectionInfoError(t('workspace.connection.unavailable'))
      setConnectionInfo(null)
      setConnectionInfoOpen(true)
      return
    }
    setConnectionInfoOpen(true)
    setConnectionInfoLoading(true)
    setConnectionInfoError(null)
    connectionPolicyFailureRef.current = null
    const sessionPromise = existingSession
      ? Promise.resolve(existingSession)
      : ensureMachineSession(machine!.machineId, { forceRelay: forceRelayConnection })
  Promise.all([
    sessionPromise.then(machineWorkspaceConnectionInfo),
    connector.getConnectionPolicy ? connector.getConnectionPolicy() : Promise.resolve(null),
  ]).then(([info, policy]) => {
    setConnectionInfo(info)
    setConnectionPolicyState(policy)
  }).catch((err: unknown) => {
      connectionPolicyFailureRef.current = { stage: 'refresh' }
      setConnectionInfoError(err instanceof Error ? err.message : String(err))
    }).finally(() => {
      setConnectionInfoLoading(false)
    })
  }, [connectedSession, connector, ensureMachineSession, forceRelayConnection, machine])

  const applyConnectionPolicy = useCallback(async (policy: ConnectionPolicy) => {
  if (!connector.applyConnectionPolicy) {
    setConnectionInfoError(t('workspace.connection.policyUnavailable'))
    return
  }
  if (activeTerminalId && !window.confirm(t('workspace.connection.reconnectConfirm'))) return
  setConnectionPolicyApplying(true)
  setConnectionInfoError(null)
  connectionPolicyFailureRef.current = null
  try {
    await connector.applyConnectionPolicy(policy)
  } catch (err) {
    connectionPolicyFailureRef.current = { stage: 'apply', policy }
    setConnectionInfoError(err instanceof Error ? err.message : String(err))
    setConnectionPolicyApplying(false)
    return
  }
  setConnectionPolicyState((current) => current ? { ...current, policy } : current)
  connectionPolicyReconnectPendingRef.current = true
  connectionPolicyFailureRef.current = { stage: 'reconnect' }
  try {
    await retryConnection({ preservePolicy: true, closeDialog: false })
  } catch (err) {
    connectionPolicyReconnectPendingRef.current = false
    setConnectionInfoError(err instanceof Error ? err.message : String(err))
    setConnectionPolicyApplying(false)
  }
  }, [activeTerminalId, connector, retryConnection, t])

  const retryConnectionPolicy = useCallback(() => {
  connectionPolicyReconnectPendingRef.current = true
  setConnectionPolicyApplying(true)
  setConnectionInfoError(null)
  void retryConnection({ preservePolicy: true, closeDialog: false })
  }, [retryConnection])

  const retryConnectionPolicyFailure = useCallback(() => {
    const failure = connectionPolicyFailureRef.current
    if (failure?.stage === 'refresh') {
      openConnectionInfo()
      return
    }
    if (failure?.stage === 'apply' && failure.policy) {
      void applyConnectionPolicy(failure.policy)
      return
    }
    retryConnectionPolicy()
  }, [applyConnectionPolicy, openConnectionInfo, retryConnectionPolicy])

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
      setPairStatus(t('workspace.clipboardEmpty'))
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
      setClipboardError(t('workspace.clipboardTextEmpty'))
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
      setPairStatus(t(editingClipboardId ? 'workspace.clipboardUpdated' : 'workspace.clipboardSaved'))
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
      setClipboardError(err instanceof Error ? err.message : t('workspace.browserClipboardError'))
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
      const fallbackMessage = err instanceof Error ? err.message : t('workspace.clipboardReadError')
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
      <MobileSheetPanel title={t('workspace.chooseDirectory')} testId="muxvia-terminal-path-picker-sheet" onClose={() => setMobileSheet(terminalPathReturnSheet)}>
        <div className="flex flex-col gap-3">
          <div className="muxvia-app-panel p-3">
            <div className="break-all font-mono text-[12px] font-semibold text-zinc-800">{normalizedPath}</div>
            <div className="mt-3 grid grid-cols-2 gap-2">
              <button
                type="button"
                className="muxvia-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold disabled:text-zinc-300"
                disabled={normalizedPath === '/'}
                onClick={() => { hapticImpact(); void loadTerminalPathPicker(parentPath(normalizedPath)) }}
              >
                <ChevronLeft className="h-4 w-4" />
                Parent
              </button>
              <button
                type="button"
                className="muxvia-app-primary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
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
            className="muxvia-app-panel flex h-80 max-h-[45vh] min-h-0 flex-col overflow-hidden"
            data-testid="muxvia-terminal-path-picker-list"
          >
            {terminalPathPickerLoading ? (
              <div className="flex h-full items-center justify-center gap-2 text-[13px] font-medium text-zinc-500">
                <span className="muxvia-square-spinner" aria-hidden="true" />
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
      <MobileSheetPanel title={t('files.bookmarks.title')} testId="muxvia-terminal-path-bookmarks-sheet" onClose={() => setMobileSheet(terminalPathReturnSheet)}>
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-2 gap-2">
            <button
              type="button"
              className="muxvia-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
              onClick={() => { hapticImpact(); void addTerminalPathBookmark() }}
            >
              <BookmarkPlus className="h-4 w-4" />
              Save path
            </button>
            <button
              type="button"
              className="muxvia-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
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

          <div className="muxvia-app-panel overflow-hidden">
            {terminalPathBookmarksLoading ? (
              <div className="flex min-h-20 items-center justify-center gap-2 text-[13px] font-medium text-zinc-500">
                <span className="muxvia-square-spinner" aria-hidden="true" />
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
      <MobileSheetPanel title={t('workspace.clipboard')} testId="muxvia-clipboard-history-sheet" onClose={() => setMobileSheet(null)}>
        <div className="flex flex-col gap-3">
          <div className="muxvia-app-panel p-3">
            <textarea
              aria-label={t('workspace.clipboardText')}
              className="min-h-24 w-full resize-none border border-[var(--muxvia-app-line)] bg-zinc-50 px-3 py-2 text-[13px] font-medium text-zinc-900 outline-none"
              value={clipboardDraft}
              onChange={(event) => setClipboardDraft(event.currentTarget.value)}
            />
            <div className="mt-2 grid grid-cols-3 gap-2">
              <button
                type="button"
                className="muxvia-app-secondary-button min-h-11 gap-1.5 px-2 text-[12px] font-semibold"
                onClick={() => { hapticImpact(); void loadBrowserClipboardDraft() }}
              >
                <ClipboardList className="h-4 w-4" />
                {t('workspace.browserClipboard')}
              </button>
              <button
                type="button"
                className="muxvia-app-secondary-button min-h-11 gap-1.5 px-2 text-[12px] font-semibold"
                onClick={() => { hapticImpact(); void refreshClipboardEntries() }}
              >
                <RefreshCw className="h-4 w-4" />
                {t('common.refresh')}
              </button>
              <button
                type="button"
                className="muxvia-app-primary-button min-h-11 px-2 text-[12px] font-semibold disabled:bg-zinc-300 disabled:text-zinc-500"
                disabled={!clipboardDraft || clipboardLoading}
                onClick={() => { hapticImpact(); void saveClipboardDraft() }}
              >
                {t(editingClipboardId ? 'workspace.update' : 'files.actions.save')}
              </button>
            </div>
          </div>

          {clipboardError ? (
            <div className="border border-amber-200 bg-amber-50 px-3 py-2 text-[13px] font-medium text-amber-800" role="alert">
              {clipboardError}
            </div>
          ) : null}

          <div className="muxvia-app-panel overflow-hidden">
            {clipboardLoading && clipboardEntries.length === 0 ? (
              <div className="flex min-h-20 items-center justify-center gap-2 text-[13px] font-medium text-zinc-500">
                <span className="muxvia-square-spinner" aria-hidden="true" />
                {t('common.loading')}
              </div>
            ) : clipboardEntries.length === 0 ? (
              <div className="flex min-h-20 items-center justify-center px-3 text-center text-[13px] font-medium text-zinc-500">
                {t('workspace.noClipboardHistory')}
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
                      className="muxvia-app-secondary-button min-h-11 gap-1.5 px-2 text-[12px] font-semibold"
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
        className={`muxvia-app-page relative min-h-0 flex-1 flex-col md:flex md:w-72 md:flex-none md:border-r md:border-[var(--muxvia-app-line)] ${page === 'terminal' ? 'hidden' : 'flex'}`}
        data-testid={page === 'terminal' ? undefined : 'muxvia-terminal-list-page'}
      >
        <header className="muxvia-app-header flex min-h-14 shrink-0 items-center justify-between border-b px-3 pt-[env(safe-area-inset-top)] md:pt-0">
          <div className="flex min-w-0 items-center gap-2">
            {onBack ? (
              <button
                type="button"
                aria-label={t('common.backToMachines')}
                className="muxvia-app-icon-button mr-1 border-transparent bg-transparent"
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
              aria-label={t('workspace.connectionInfo')}
              className="muxvia-app-icon-button border-transparent bg-transparent"
              tabIndex={page === 'terminal' ? -1 : undefined}
              onClick={() => { hapticSelection(); openConnectionInfo() }}
            >
              <Info className="h-5 w-5" />
            </button>
            <button
              type="button"
              aria-hidden={page === 'terminal' ? 'true' : undefined}
              aria-label={t('workspace.openFiles')}
              className="muxvia-app-icon-button border-transparent bg-transparent"
              tabIndex={page === 'terminal' ? -1 : undefined}
              onClick={() => { hapticSelection(); openFiles() }}
            >
              <Folder className="h-5 w-5" />
            </button>
            {canManageTerminals ? (
              <button
                type="button"
                aria-label={t('workspace.createTerminal')}
                className="muxvia-app-icon-button border-transparent bg-transparent"
                onClick={() => { hapticImpact(); openCreateTerminal() }}
              >
                <Plus className="h-5 w-5" />
              </button>
            ) : null}
          </div>
        </header>
        {showDelayedMachineNetworkOverlay ? (
          <div className="flex animate-in fade-in slide-in-from-top-1 duration-200 items-center justify-center gap-2 border-b border-zinc-200 bg-blue-50/50 px-3 py-1.5">
            <span className="muxvia-square-spinner h-3.5 w-3.5 text-blue-600" aria-hidden="true" />
            <span className="text-[11px] font-medium text-blue-700">
              {connectionStatus || t('workspace.connecting')}
            </span>
          </div>
        ) : null}
        {error ? (
          <div className="m-3 shrink-0 border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800" role="alert">
            {error}
          </div>
        ) : null}
        {requireVerification ? (
          <section className="muxvia-app-panel mx-3 mt-3 p-5" data-testid="muxvia-verification-gate">
            <div className="mb-4 flex items-center gap-3">
              <div className="flex h-11 w-11 items-center justify-center border border-[var(--muxvia-app-line)] bg-[var(--muxvia-app-soft)] text-zinc-600">
                <KeyRound className="h-5 w-5" />
              </div>
              <div>
                <h2 className="text-[17px] font-bold tracking-tight text-zinc-900">{t('workspace.verifyDevice')}</h2>
                <p className="text-[13px] font-medium text-zinc-500">{t('workspace.verifyDeviceCopy')}</p>
              </div>
            </div>
            <button
              type="button"
              className="muxvia-app-primary-button min-h-12 w-full gap-2 px-4 text-[15px] font-semibold"
              onClick={() => { hapticImpact(); handleConnectionAuthFailure(machine.machineId) }}
            >
              <KeyRound className="h-4 w-4" />
              Verify device
            </button>
          </section>
        ) : null}
        <div
          className="min-h-0 flex-1 overflow-y-auto p-3"
          data-testid="muxvia-terminal-list-scroll"
        >
          <h2 className="mb-2 px-1 text-xs font-semibold uppercase tracking-wider text-zinc-500">{t('terminal.list')}</h2>
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
          <MobileSheetPanel title={selectedTerminal.title || t('terminal.defaultTitle')} testId="muxvia-terminal-actions-sheet" onClose={() => setMobileSheet(null)}>
            <div className="flex flex-col gap-3">
              {selectedTerminal.state === 'exited' ? (
                <button
                  type="button"
                  className="muxvia-app-secondary-button min-h-12 w-full justify-between px-4 text-left text-[15px] font-medium"
                  onClick={() => { hapticImpact(); void restartManagedTerminal() }}
                >
                  <span>{t('workspace.restartTerminal')}</span>
                  <RefreshCw className="h-4 w-4 text-zinc-500" />
                </button>
              ) : null}
              <button
                type="button"
                className="muxvia-app-secondary-button min-h-12 w-full justify-between px-4 text-left text-[15px] font-medium"
                onClick={() => { hapticImpact(); openEditTerminal() }}
              >
                <span>{t('workspace.editTerminal')}</span>
                <SquarePen className="h-4 w-4 text-zinc-500" />
              </button>
              <button
                type="button"
                className="flex min-h-12 w-full items-center justify-between border border-red-200 bg-red-50 px-4 text-left text-[15px] font-medium text-red-700"
                onClick={() => { hapticImpact(); void deleteManagedTerminal() }}
              >
                <span>{t('workspace.deleteTerminal')}</span>
                <Trash2 className="h-4 w-4 text-red-500" />
              </button>
            </div>
          </MobileSheetPanel>
        ) : null}

        {(mobileSheet === 'create-terminal' || mobileSheet === 'edit-terminal') ? (
          <MobileSheetPanel
            title={t(mobileSheet === 'create-terminal' ? 'workspace.newTerminal' : 'workspace.editTerminal')}
            testId="muxvia-terminal-editor-sheet"
            onClose={() => setMobileSheet(null)}
          >
            <div className="flex flex-col gap-4">
              {terminalSubmitError ? (
                <div className="border border-red-200 bg-red-50 px-3 py-2 text-[13px] font-medium text-red-700" role="alert">
                  {terminalSubmitError}
                </div>
              ) : null}
              <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                {t('workspace.terminalForm.name')}
                <input
                  className="min-h-12 border border-[var(--muxvia-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                  value={terminalForm.name}
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setTerminalForm((current) => ({ ...current, name: value }))
                  }}
                />
              </label>
              {mobileSheet === 'create-terminal' ? (
                <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                  {t('workspace.terminalForm.command')}
                  <input
                    className="min-h-12 border border-[var(--muxvia-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                    value={terminalForm.command}
                    onChange={(event) => {
                      const value = event.currentTarget.value
                      setTerminalForm((current) => ({ ...current, command: value }))
                    }}
                  />
                </label>
              ) : null}
              <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                {t('workspace.terminalForm.cwd')}
                <input
                  className="min-h-12 border border-[var(--muxvia-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                  value={terminalForm.cwd}
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setTerminalForm((current) => ({ ...current, cwd: value }))
                  }}
                />
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    className="muxvia-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
                    onClick={() => {
                      hapticImpact()
                      openTerminalPathPicker()
                    }}
                  >
                    <FolderOpen className="h-4 w-4" />
                    {t('workspace.browse')}
                  </button>
                  <button
                    type="button"
                    className="muxvia-app-secondary-button min-h-11 gap-2 px-3 text-[13px] font-semibold"
                    onClick={() => {
                      hapticImpact()
                      openTerminalPathBookmarks()
                    }}
                  >
                    <Bookmark className="h-4 w-4" />
                    {t('files.bookmarks.title')}
                  </button>
                </div>
              </label>
              <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                {t('workspace.terminalForm.environment')}
                <input
                  className="min-h-12 border border-[var(--muxvia-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                  value={terminalForm.environment}
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setTerminalForm((current) => ({ ...current, environment: value }))
                  }}
                />
              </label>
              <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
                {t('workspace.terminalForm.sizeLock')}
                <select
                  className="min-h-12 border border-[var(--muxvia-app-line)] bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 outline-none"
                  value={terminalForm.sizeLockMode}
                  onChange={(event) => {
                    const value = event.currentTarget.value as 'off' | 'warn' | 'lock'
                    setTerminalForm((current) => ({ ...current, sizeLockMode: value }))
                  }}
                >
                  <option value="off">{t('workspace.resizeMode.resizable')}</option>
                  <option value="warn">{t('workspace.resizeMode.warn')}</option>
                  <option value="lock">{t('workspace.resizeMode.locked')}</option>
                </select>
              </label>
              <button
                type="button"
                className="muxvia-app-primary-button mt-2 min-h-12 w-full gap-2 px-4 text-[15px] font-semibold"
                disabled={terminalSubmitting}
                onClick={() => {
                  hapticImpact()
                  if (mobileSheet === 'create-terminal') {
                    void submitCreateTerminal()
                    return
                  }
                  void submitUpdateTerminal()
                }}
              >
                {terminalSubmitting ? t('workspace.saving') : t(mobileSheet === 'create-terminal' ? 'workspace.createTerminal' : 'workspace.saveChanges')}
              </button>
            </div>
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
        <div className="muxvia-app-panel w-full max-w-md border-red-200 p-4 text-sm text-red-700" role="alert">
          <h2 className="mb-2 font-semibold text-red-900">{t('workspace.connectionError')}</h2>
          <p>{error}</p>
        </div>
      </div>
    )
  }

  if (!machine) {
    return (
      <div className={`flex h-full min-h-0 items-center justify-center bg-zinc-50 ${className || ''}`}>
        <div className="flex items-center gap-2 text-sm text-zinc-500">
          <span className="muxvia-square-spinner text-zinc-600" aria-hidden="true" />
          Connecting to Muxvia...
        </div>
      </div>
    )
  }

  return (
    <div
      ref={outerContainerRef}
      className={`relative flex h-full min-h-0 w-full max-w-full flex-col overflow-hidden bg-[var(--muxvia-bg)] font-sans text-[var(--muxvia-text)] md:flex-row ${className || ''}`}
      data-machine-id={machine.machineId}
      style={terminalThemeStyle}
    >
      {renderTerminalListPage()}

      <main
        className={`relative min-h-0 min-w-0 max-w-full flex-1 overflow-hidden bg-[var(--muxvia-terminal-bg)] ${page === 'terminal-list' ? 'hidden md:flex md:items-center md:justify-center md:bg-zinc-50/50' : 'grid grid-rows-[auto_minmax(0,1fr)_auto] md:grid-rows-[minmax(0,1fr)]'}`}
        data-testid="muxvia-terminal-page"
      >
        {page === 'terminal-list' ? (
          <div className="flex flex-col items-center gap-3 text-zinc-400">
            <Monitor className="h-12 w-12 opacity-20" />
            <p className="text-sm font-medium">{t('workspace.selectTerminal')}</p>
          </div>
        ) : (
          <>
        <header
          className="relative z-30 row-start-1 flex min-h-12 min-w-0 max-w-full shrink-0 items-center justify-between gap-1 overflow-hidden border-b border-[var(--muxvia-border-subtle)] bg-[var(--muxvia-surface)] px-1.5 pt-[env(safe-area-inset-top)] md:hidden"
          data-testid="muxvia-terminal-header"
        >
          <div className="flex min-w-0 flex-1 items-center gap-1">
            <button
              type="button"
              aria-label={t('workspace.showTerminalList')}
              className="flex h-11 w-11 shrink-0 items-center justify-center text-[var(--muxvia-muted)] transition-colors active:bg-[var(--muxvia-surface-raised)]"
              onClick={() => { hapticSelection(); showTerminalListPage() }}
            >
              <ChevronLeft className="h-5 w-5" />
            </button>
            <button
              type="button"
              aria-label={t('workspace.switchTerminal')}
              className="flex min-h-11 min-w-0 flex-1 flex-col items-start justify-center px-1.5 py-0.5 text-left transition-colors active:bg-[var(--muxvia-surface-raised)]"
              onClick={() => { hapticSelection(); setMobileSheet('terminals') }}
            >
              <span className="max-w-full truncate text-[9px] font-bold uppercase tracking-wider text-[var(--muxvia-muted)]">{machine.name}</span>
              <span className="max-w-full truncate text-[12px] font-semibold leading-tight text-[var(--muxvia-text)]" data-testid="muxvia-terminal-title">{terminalHeaderTitle}</span>
              {terminalHeaderDirectory ? (
                <span className="max-w-full truncate text-[10px] font-medium leading-tight text-[var(--muxvia-muted)]">{terminalHeaderDirectory}</span>
              ) : null}
            </button>
          </div>

          <button
            type="button"
            aria-label={t('workspace.openTerminalMenu')}
            className="flex h-11 w-11 shrink-0 items-center justify-center text-[var(--muxvia-muted)] transition-colors active:bg-[var(--muxvia-surface-raised)]"
            onClick={() => { hapticSelection(); setMobileSheet('terminal-menu') }}
          >
            <MoreHorizontal className="h-5 w-5" />
          </button>
        </header>

        <div
          ref={terminalAreaRef}
          className="relative row-start-2 h-full min-h-0 min-w-0 flex-1 overflow-hidden bg-[var(--muxvia-terminal-bg)] md:row-start-1"
          data-testid="muxvia-terminal-body"
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
                  setPairStatus(t('workspace.copied'))
                  setTerminalToolbarOpen(false)
                  setTerminalToolbarModeAndReset('default')
                }).catch((err: unknown) => {
                  setError(err instanceof Error ? err.message : t('workspace.copyFailed'))
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

          <div ref={terminalWrapperRef} className={`absolute inset-0 flex flex-col bg-[var(--muxvia-terminal-bg)] ${splitTerminalId ? 'gap-px' : ''}`}>
            <div
              className={`relative min-h-0 flex-1 overflow-hidden bg-[var(--muxvia-terminal-bg)] ${splitTerminalId ? `border-b border-[var(--muxvia-border-subtle)] ${activeTerminalSlot === 0 ? 'ring-1 ring-inset ring-[var(--muxvia-accent)]' : ''}` : ''}`}
              data-active-slot={activeTerminalSlot === 0 ? 'true' : 'false'}
              data-testid="muxvia-terminal-panel"
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
                <div className="flex h-full items-center justify-center text-sm text-[var(--muxvia-muted)]">
                  {showMachineNetworkOverlay ? null : activeTerminalId && connectingTerminalId === activeTerminalId ? (connectionStatus ?? t('workspace.connectingTerminal')) : t('terminal.noActive')}
                </div>
              )}
              {activeTerminalId && connectedSession && connectedTerminalId === activeTerminalId && activeTerminalResizeLocked ? (
                <button
                  type="button"
                  aria-label={t('workspace.unlockResize')}
                  className={`absolute right-2 z-20 flex min-h-8 items-center gap-1.5 border border-[var(--muxvia-border-subtle)] bg-[var(--muxvia-overlay)] px-2 text-[11px] font-semibold text-[var(--muxvia-text)] backdrop-blur active:opacity-85 disabled:opacity-60 ${splitTerminalId ? 'top-16' : 'top-2'}`}
                  disabled={unlockingResize}
                  onClick={() => { hapticImpact(); void unlockTerminalResize() }}
                >
                  <Unlock className="h-3.5 w-3.5" />
                  {unlockingResize ? t('workspace.unlocking') : t('workspace.unlockResize')}
                </button>
              ) : null}
            </div>

            {splitTerminalId ? (
              <div
                className={`relative min-h-0 flex-1 overflow-hidden bg-[var(--muxvia-terminal-bg)] ${activeTerminalSlot === 1 ? 'ring-1 ring-inset ring-[var(--muxvia-accent)]' : ''}`}
                data-active-slot={activeTerminalSlot === 1 ? 'true' : 'false'}
                data-testid="muxvia-split-terminal-panel"
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
                  <div className="absolute inset-0 flex items-center justify-center text-sm text-[var(--muxvia-muted)]">
                    {showMachineNetworkOverlay ? null : connectionStatus ?? t('workspace.connectingTerminal')}
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
          <MobileSheetPanel title={t('terminal.list')} testId="muxvia-terminal-switcher-sheet" onClose={() => setMobileSheet(null)}>
            <TerminalList
              machineId={machine.machineId}
              terminals={terminals}
              onOpenTerminal={openTerminal}
              activeTerminalId={activeTerminalId ?? undefined}
            />
          </MobileSheetPanel>
        ) : null}

        {mobileSheet === 'terminal-menu' ? (
          <MobileSheetPanel title={t('workspace.terminalTools')} testId="muxvia-terminal-menu-sheet" onClose={() => setMobileSheet(null)}>
            <div className="grid grid-cols-2 border-l border-t border-[var(--muxvia-app-line)]">
              <button type="button" className="muxvia-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticImpact(); openSplitTerminalSheet() }}>
                <Rows2 className="h-4 w-4 text-[var(--muxvia-app-accent)]" />
                {splitTerminalId ? t('workspace.changeSplit') : t('workspace.splitTerminal')}
              </button>
              <button type="button" className="muxvia-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticImpact(); setMobileSheet(null); void (activeTerminalOwnsResize ? releaseActiveResizeOwner() : acquireActiveResizeOwner()) }}>
                <span className="w-4 font-mono text-[11px] font-extrabold text-[var(--muxvia-app-accent)]">{resizeControlBadgeText(terminalResizeControl)}</span>
                {activeTerminalOwnsResize ? t('workspace.releaseResize') : t('workspace.controlResize')}
              </button>
              {splitTerminalId ? (
                <>
                  <button type="button" className="muxvia-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticImpact(); setSyncSplitInput((current) => !current); setMobileSheet(null) }}>
                    {syncSplitInput ? <Link2 className="h-4 w-4 text-[var(--muxvia-app-accent)]" /> : <Link2Off className="h-4 w-4 text-[var(--muxvia-app-muted)]" />}
                    {t('workspace.syncInput')}
                  </button>
                  <button type="button" className="muxvia-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticImpact(); closeSplitTerminal(); setMobileSheet(null) }}>
                    <PanelBottomClose className="h-4 w-4 text-[var(--muxvia-app-danger)]" />
                    {t('workspace.closeSplit')}
                  </button>
                </>
              ) : null}
              <button type="button" className="muxvia-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticSelection(); setMobileSheet(null); setTerminalToolbarOpen((current) => { const next = !current; if (next) setTerminalFnOpen(false); if (!next) setTerminalToolbarModeAndReset('default'); return next }) }}>
                <SlidersHorizontal className="h-4 w-4 text-[var(--muxvia-app-accent)]" />
                {t('workspace.terminalTools')}
              </button>
              <button type="button" className="muxvia-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticSelection(); setMobileSheet(null); openConnectionInfo() }}>
                <Info className="h-4 w-4 text-[var(--muxvia-app-accent)]" />
                {t('workspace.connection.label')}
              </button>
              <button type="button" className="muxvia-app-secondary-button min-h-14 justify-start gap-3 border-l-0 border-t-0 px-4 text-left text-sm font-semibold" onClick={() => { hapticSelection(); setMobileSheet(null); openFiles() }}>
                <Folder className="h-4 w-4 text-[var(--muxvia-app-accent)]" />
                {t('files.list')}
              </button>
            </div>
          </MobileSheetPanel>
        ) : null}

        {mobileSheet === 'split-terminal' ? (
          <MobileSheetPanel title={t('workspace.splitTerminal')} testId="muxvia-split-terminal-sheet" onClose={() => setMobileSheet(null)}>
            {terminals.filter((terminal) => terminal.terminalId !== activeTerminalId).length > 0 ? (
              <TerminalList
                machineId={machine.machineId}
                terminals={terminals.filter((terminal) => terminal.terminalId !== activeTerminalId)}
                onOpenTerminal={selectSplitTerminal}
                activeTerminalId={splitTerminalId ?? undefined}
              />
            ) : (
              <div className="flex min-h-24 items-center justify-center border border-dashed border-zinc-300 bg-white text-sm font-medium text-zinc-500">
                {t('workspace.noOtherTerminal')}
              </div>
            )}
          </MobileSheetPanel>
        ) : null}

        {renderClipboardHistorySheet()}
          </>
        )}
      </main>

      <div
        className={`absolute inset-0 z-30 flex flex-col bg-white transition-transform duration-200 md:left-auto md:right-0 md:w-[450px] md:border-l md:border-[var(--muxvia-app-line)] ${filesOpen ? 'translate-y-0 md:translate-x-0 visible' : 'translate-y-full md:translate-y-0 md:translate-x-full invisible'}`}
        data-testid="muxvia-machine-files-overlay"
      >
        <div className="muxvia-app-header flex shrink-0 items-center justify-between border-b px-4 pb-2 pt-[calc(env(safe-area-inset-top)+0.5rem)] md:h-14 md:pb-0 md:pt-0">
          <div className="flex items-center gap-2">
            <Folder className="h-5 w-5 text-zinc-500" />
            <span className="text-[17px] font-bold tracking-tight text-zinc-900">{t('files.list')}</span>
          </div>
          <button
            type="button"
            aria-label={t('workspace.closeFiles')}
            className="muxvia-app-icon-button border-transparent bg-transparent"
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
                <span className="muxvia-square-spinner" aria-hidden="true" />
                <span>{t('workspace.connecting')}</span>
              </div>
            ) : t('workspace.fileAccessNotReady')}
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
      policyState={connectionPolicyState}
      applying={connectionPolicyApplying}
          onClose={() => setConnectionInfoOpen(false)}
          onRefresh={openConnectionInfo}
      onRetry={retryConnectionPolicyFailure}
      onApply={applyConnectionPolicy}
      onRestoreAuto={() => applyConnectionPolicy({ route: 'auto', cloud: 'auto', relayTransport: 'auto' })}
        />
      ) : null}
      {p2pFallbackPromptOpen ? (
        <P2PFallbackDialog
          onCancel={() => setP2PFallbackPromptOpen(false)}
          onUseRelay={() => retryConnection({ forceRelay: true })}
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
        className="muxvia-app-page relative max-h-[85vh] w-full overflow-hidden border-t border-[var(--muxvia-app-line)] md:max-w-md md:border"
        onClick={(e) => e.stopPropagation()}
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        <div className="absolute left-1/2 top-3 h-1 w-12 -translate-x-1/2 bg-[var(--muxvia-app-line-strong)] md:hidden" />
        <header className="flex h-16 items-center justify-between border-b border-[var(--muxvia-app-line)] px-5 pt-3">
          <h2 className="text-[17px] font-bold tracking-tight text-zinc-900">{title}</h2>
          <button
            type="button"
            aria-label={`Close ${title}`}
            className="muxvia-app-icon-button border-transparent bg-transparent"
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

/** ConnectionInfoDialog 分离持久连接偏好、当前 ReadySession 和脱敏诊断，不在 UI 推断网络路径。 */
export function ConnectionInfoDialog({
  info,
  loading,
  error,
  policyState,
  applying,
  onClose,
  onRefresh,
  onRetry,
  onApply,
  onRestoreAuto,
}: {
  info: ConnectionInfo | null
  loading: boolean
  error: string | null
  policyState: ConnectionPolicyState | null
  applying: boolean
  onClose: () => void
  onRefresh: () => void
  onRetry: () => void
  onApply: (policy: ConnectionPolicy) => void
  onRestoreAuto: () => void
}) {
  const { t } = useTranslation()
  const overlayRef = useRef<HTMLDivElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const [draft, setDraft] = useState<ConnectionPolicy>({ route: 'auto', cloud: 'auto', relayTransport: 'auto' })
  useEffect(() => {
    if (policyState) setDraft(policyState.policy)
  }, [policyState])
  useEffect(() => {
    const overlay = overlayRef.current
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    if (!overlay) return
    const siblings = Array.from(overlay.parentElement?.children ?? []).filter((element): element is HTMLElement => element instanceof HTMLElement && element !== overlay)
    const previous = siblings.map((element) => ({
      element,
      ariaHidden: element.getAttribute('aria-hidden'),
      inert: element.hasAttribute('inert'),
    }))
    for (const element of siblings) {
      element.setAttribute('aria-hidden', 'true')
      element.setAttribute('inert', '')
    }
    closeRef.current?.focus()
    return () => {
      for (const item of previous) {
        if (item.ariaHidden === null) item.element.removeAttribute('aria-hidden')
        else item.element.setAttribute('aria-hidden', item.ariaHidden)
        if (!item.inert) item.element.removeAttribute('inert')
      }
      previousFocus?.focus()
    }
  }, [])
  const type = info?.type ?? (info?.relayInUse ? 'relay' : 'unknown')
  const policyChanged = Boolean(policyState) && (
    draft.route !== policyState?.policy.route || draft.cloud !== policyState?.policy.cloud || draft.relayTransport !== policyState?.policy.relayTransport
  )
  const routeOptions: Array<{ value: ConnectionPolicy['route']; label: string; available: boolean; reason: string | undefined }> = [
    { value: 'auto', label: t('workspace.connection.routeAuto'), available: true, reason: undefined },
    { value: 'direct', label: t('workspace.connection.routeDirect'), available: policyState?.available.direct ?? false, reason: connectionPolicyUnavailableLabel(policyState?.unavailableReasons.direct, t) },
    { value: 'ssh', label: t('workspace.connection.routeSSH'), available: policyState?.available.ssh ?? false, reason: connectionPolicyUnavailableLabel(policyState?.unavailableReasons.ssh, t) },
    { value: 'cloud', label: t('workspace.connection.routeCloud'), available: policyState?.available.cloud ?? false, reason: connectionPolicyUnavailableLabel(policyState?.unavailableReasons.cloud, t) },
  ]
  return (
    <div ref={overlayRef} className="absolute inset-0 z-50 flex items-end justify-center bg-black/45 backdrop-blur-sm md:items-center md:p-4" onClick={() => { hapticSelection(); onClose() }} onKeyDown={(event) => trapConnectionDialogFocus(event, overlayRef.current, onClose)}>
      <section className="muxvia-app-page flex max-h-[96dvh] w-full max-w-xl flex-col overflow-hidden border-t border-[var(--muxvia-app-line)] md:max-h-[90vh] md:border" role="dialog" aria-modal="true" aria-labelledby="muxvia-connection-title" onClick={(event) => event.stopPropagation()}>
        <header className="flex items-center justify-between gap-3 border-b border-zinc-200 px-4 py-3">
          <div className="min-w-0">
            <h2 id="muxvia-connection-title" className="text-[17px] font-semibold text-zinc-950">{t('workspace.connection.title')}</h2>
            <p className="mt-0.5 text-[12px] font-medium text-zinc-500">{connectionTypeLabel(type, t)}</p>
          </div>
          <button ref={closeRef} type="button" aria-label={t('workspace.connection.closeInfo')} className="muxvia-app-icon-button border-transparent bg-transparent" onClick={() => { hapticSelection(); onClose() }}>
            <X className="h-5 w-5" />
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {error ? (
            <div className="border-b border-red-200 bg-red-50 px-4 py-3 text-[13px] font-medium text-red-800" role="alert">
              <p>{error}</p>
              <div className="mt-3 flex flex-wrap gap-2">
                <button type="button" className="muxvia-app-secondary-button min-h-12 px-3 text-[13px] font-semibold" onClick={onRetry}>{t('workspace.connection.retry')}</button>
                <button type="button" className="muxvia-app-secondary-button min-h-12 px-3 text-[13px] font-semibold" onClick={onRestoreAuto}>{t('workspace.connection.restoreAuto')}</button>
              </div>
            </div>
          ) : null}
          <section className="border-b border-[var(--muxvia-app-line)] px-4 py-4">
            <h3 className="text-[13px] font-semibold text-zinc-950">{t('workspace.connection.current')}</h3>
            <dl className="mt-2 overflow-hidden border border-[var(--muxvia-app-line)]">
              <ConnectionInfoRow label={t('workspace.connection.route')} value={loading ? t('workspace.connection.reading') : connectionRouteLabel(info?.routeKind, t)} strong />
              <ConnectionInfoRow label={t('workspace.connection.path')} value={observedPathLabel(info?.observedPath, t)} />
              <ConnectionInfoRow label={t('workspace.connection.relayTransport')} value={displayDiagnostic(info?.relayTransport, t)} />
              <ConnectionInfoRow label={t('workspace.connection.rtt')} value={info?.rtt !== undefined ? `${Math.round(info.rtt)} ms` : t('workspace.connection.notProvided')} />
            </dl>
          </section>

          <fieldset className="border-b border-[var(--muxvia-app-line)] px-4 py-4" disabled={loading || applying || !policyState}>
            <legend className="text-[13px] font-semibold text-zinc-950">{t('workspace.connection.preference')}</legend>
            <div className="mt-2 divide-y divide-[var(--muxvia-app-line)] border-y border-[var(--muxvia-app-line)]">
              {routeOptions.map((option) => (
                <label key={option.value} className={`flex min-h-12 items-center gap-3 py-2 text-[14px] ${option.available ? 'text-zinc-900' : 'text-zinc-400'}`}>
                  <input aria-label={option.label} aria-describedby={!option.available ? `connection-route-${option.value}-reason` : undefined} type="radio" name="connection-route" value={option.value} checked={draft.route === option.value} disabled={!option.available} onChange={() => setDraft((current) => ({ ...current, route: option.value }))} className="h-5 w-5 shrink-0 accent-zinc-900" />
                  <span className="min-w-0 flex-1 font-medium">{option.label}</span>
                  {!option.available ? <span id={`connection-route-${option.value}-reason`} className="max-w-[55%] text-right text-[11px] leading-4">{option.reason ?? t('workspace.connection.unavailableShort')}</span> : null}
                </label>
              ))}
            </div>
          </fieldset>

          <details className="border-b border-[var(--muxvia-app-line)] px-4 py-3" open={draft.route === 'cloud'}>
            <summary className="flex min-h-12 cursor-pointer items-center text-[13px] font-semibold text-zinc-950">{t('workspace.connection.cloudAdvanced')}</summary>
            <div className="space-y-5 pb-2">
              <ConnectionRadioGroup label={t('workspace.connection.cloudPath')} name="cloud-path" value={draft.cloud} options={[
                ['auto', t('workspace.connection.cloudAuto')], ['p2p', t('workspace.connection.cloudP2P')], ['relay', t('workspace.connection.cloudRelay')],
              ]} disabled={!policyState?.available.cloud || (draft.route !== 'auto' && draft.route !== 'cloud')} onChange={(cloud) => setDraft((current) => ({ ...current, cloud }))} />
              <ConnectionRadioGroup label={t('workspace.connection.relayTransport')} name="relay-transport" value={draft.relayTransport} options={[
                ['auto', t('workspace.connection.transportAuto')], ['udp', t('workspace.connection.transportUDP')], ['tcp', t('workspace.connection.transportTCP')],
              ]} disabled={!policyState?.available.cloud || (draft.route !== 'auto' && draft.route !== 'cloud') || draft.cloud === 'p2p'} onChange={(relayTransport) => setDraft((current) => ({ ...current, relayTransport }))} />
            </div>
          </details>

          <details className="px-4 py-3">
            <summary className="flex min-h-12 cursor-pointer items-center text-[13px] font-semibold text-zinc-950">{t('workspace.connection.diagnostics')}</summary>
            <dl className="mb-2 overflow-hidden border border-[var(--muxvia-app-line)]">
              <ConnectionInfoRow label={t('workspace.connection.routeId')} value={displayDiagnostic(info?.routeId, t)} />
              <ConnectionInfoRow label={t('workspace.connection.generation')} value={info?.generation?.toString() ?? t('workspace.connection.notProvided')} />
              <ConnectionInfoRow label={t('workspace.connection.reason')} value={displayDiagnostic(info?.routeSelectionReason, t)} />
              <ConnectionInfoRow label={t('workspace.connection.candidates')} value={candidateTypeText(info, t)} />
              <ConnectionInfoRow label={t('workspace.connection.protocols')} value={`${displayDiagnostic(info?.localProtocol, t)} / ${displayDiagnostic(info?.remoteProtocol, t)}`} />
              <ConnectionInfoRow label={t('workspace.connection.networkClass')} value={displayDiagnostic(info?.networkClass, t)} />
              <ConnectionInfoRow label={t('workspace.connection.sampledAt')} value={info?.sampledAt ? new Date(info.sampledAt).toLocaleString() : t('workspace.connection.notProvided')} />
              <ConnectionInfoRow label={t('workspace.connection.traffic')} value={info?.bytesSent !== undefined && info?.bytesReceived !== undefined ? `${info.bytesSent.toString()} / ${info.bytesReceived.toString()} B` : t('workspace.connection.notProvided')} />
            </dl>
          </details>
        </div>

        <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-zinc-200 px-4 py-3">
          <button type="button" className="muxvia-app-secondary-button min-h-12 px-3 text-[13px] font-semibold" disabled={applying} onClick={() => { hapticImpact(); onRefresh() }}>
            {t('common.refresh')}
          </button>
          <button
            type="button"
            className="muxvia-app-primary-button min-h-12 px-4 text-[13px] font-semibold disabled:bg-zinc-300 disabled:text-zinc-500"
            disabled={loading || applying || !policyState || !policyChanged}
            onClick={() => { hapticImpact(); onApply(draft) }}
          >
      {applying ? t('workspace.connection.applying') : t('workspace.connection.applyReconnect')}
          </button>
        </footer>
      </section>
    </div>
  )
}

function ConnectionRadioGroup<T extends string>({ label, name, value, options, disabled, onChange }: {
  label: string
  name: string
  value: T
  options: Array<readonly [T, string]>
  disabled: boolean
  onChange: (value: T) => void
}) {
  return (
    <fieldset disabled={disabled}>
      <legend className="text-[12px] font-semibold text-zinc-600">{label}</legend>
      <div className="mt-1 grid grid-cols-3 gap-1 border border-[var(--muxvia-app-line)] p-1">
        {options.map(([option, text]) => (
          <label key={option} className={`flex min-h-12 items-center justify-center px-2 text-center text-[12px] font-semibold ${value === option ? 'bg-zinc-900 text-white' : 'bg-zinc-50 text-zinc-700'} disabled:text-zinc-400`}>
            <input className="sr-only" type="radio" name={name} value={option} checked={value === option} onChange={() => onChange(option)} />
            <span>{text}</span>
          </label>
        ))}
      </div>
    </fieldset>
  )
}

function ConnectionInfoRow({ label, value, strong = false }: { label: string; value: string; strong?: boolean | undefined }) {
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-start gap-3 border-b border-[var(--muxvia-app-line)] bg-zinc-50 px-3 py-2 last:border-b-0">
      <dt className="text-[12px] font-semibold text-zinc-500">{label}</dt>
      <dd className={`min-w-0 break-words text-[12px] ${strong ? 'font-semibold text-zinc-950' : 'font-medium text-zinc-700'}`}>{value}</dd>
    </div>
  )
}

/** P2PFallbackDialog 只处理一次 direct probe 的失败决策，不修改 managed endpoint 的持久 auto 策略。 */
function P2PFallbackDialog({ onCancel, onUseRelay }: { onCancel: () => void; onUseRelay: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="absolute inset-0 z-[60] flex items-center justify-center bg-black/45 p-4 backdrop-blur-sm" role="dialog" aria-modal="true" aria-labelledby="muxvia-p2p-fallback-title">
      <section className="muxvia-app-panel w-full max-w-sm overflow-hidden">
        <div className="border-b border-[var(--muxvia-app-line)] px-4 py-4">
          <h2 id="muxvia-p2p-fallback-title" className="text-[16px] font-semibold text-zinc-950">{t('workspace.p2pUnavailable')}</h2>
          <p className="mt-2 text-[13px] leading-5 text-zinc-600">{t('workspace.p2pUnavailableCopy')}</p>
        </div>
        <div className="flex items-center justify-end gap-2 px-4 py-3">
          <button type="button" className="muxvia-app-secondary-button px-3 text-[13px] font-semibold" onClick={() => { hapticSelection(); onCancel() }}>
            {t('workspace.notNow')}
          </button>
          <button type="button" className="muxvia-app-primary-button px-3 text-[13px] font-semibold" onClick={() => { hapticImpact(); onUseRelay() }}>
            {t('workspace.connection.useRelay')}
          </button>
        </div>
      </section>
    </div>
  )
}

function connectionTypeLabel(type: ConnectionInfo['type'], t: ReturnType<typeof useTranslation>['t']): string {
  if (type === 'p2p') return t('machines.path.direct')
  if (type === 'relay') return t('machines.path.relay')
  return t('terminal.state.unknown')
}

function candidateTypeText(info: ConnectionInfo | null, t: ReturnType<typeof useTranslation>['t']): string {
  const local = displayDiagnostic(info?.candidateType, t)
  const remote = displayDiagnostic(info?.remoteCandidateType, t)
  return `${local} / ${remote}`
}

function displayDiagnostic(value: string | undefined, t: ReturnType<typeof useTranslation>['t']): string {
  return value?.trim() || t('workspace.connection.notProvided')
}

function connectionRouteLabel(kind: ConnectionInfo['routeKind'], t: ReturnType<typeof useTranslation>['t']): string {
  switch (kind) {
    case 'local': return t('workspace.connection.routeLocal')
    case 'direct': return t('workspace.connection.routeDirect')
    case 'ssh': return t('workspace.connection.routeSSH')
    case 'cloud': return t('workspace.connection.routeCloud')
    default: return t('workspace.connection.notProvided')
  }
}

function observedPathLabel(path: ConnectionInfo['observedPath'], t: ReturnType<typeof useTranslation>['t']): string {
  switch (path) {
    case 'direct': return t('workspace.connection.pathDirect')
    case 'single_relay': return t('workspace.connection.pathRelay')
    case 'relay_mesh': return t('workspace.connection.pathRelayMesh')
    default: return t('workspace.connection.notProvided')
  }
}

function isProtoSessionAlive(session: MachineWorkspaceClientSession): boolean { return session.isAlive() }

function closeTerminalDataChannel(session: MachineWorkspaceClientSession, terminalId: string): void {
  void session
  void terminalId
}

function closeMachineWorkspaceSession(session: MachineWorkspaceClientSession): Promise<void> {
  return session.close()
}

async function machineWorkspaceConnectionInfo(session: MachineWorkspaceClientSession): Promise<ConnectionInfo> {
  const snapshot = await session.getConnectionSnapshot?.() ?? session.connection
  const routeKind = connectionRouteKindFromProto(snapshot?.routeKind)
  const observedPath = observedPathFromProto(snapshot?.observedPath)
  const relayInUse = observedPath === 'single_relay' || snapshot?.localCandidateType === ConnectionCandidateType.RELAY || snapshot?.remoteCandidateType === ConnectionCandidateType.RELAY
  return {
  path: routeKind === 'cloud' ? 'hub' : 'local',
  routeId: snapshot?.routeId || session.stamp.routeId,
  routeKind,
  observedPath,
  routeSelectionReason: snapshot?.selectionReason as ConnectionInfo['routeSelectionReason'],
    connectionId: `${session.stamp.endpointId}:${session.stamp.generation}`,
    machineId: session.stamp.endpointId,
  relayInUse,
  type: relayInUse ? 'relay' : observedPath === 'direct' ? 'p2p' : 'unknown',
  candidateType: candidateTypeFromProto(snapshot?.localCandidateType),
  remoteCandidateType: candidateTypeFromProto(snapshot?.remoteCandidateType),
  localProtocol: transportFromProto(snapshot?.localProtocol),
  remoteProtocol: transportFromProto(snapshot?.remoteProtocol),
  relayTransport: transportFromProto(snapshot?.relayTransport),
  networkClass: snapshot?.networkClass || undefined,
  rtt: snapshot?.roundTripNanos ? Number(snapshot.roundTripNanos / 1_000_000n) : undefined,
  sampledAt: snapshot?.sampledAtUnixNano ? Number(snapshot.sampledAtUnixNano / 1_000_000n) : undefined,
  bytesSent: snapshot?.bytesSent,
  bytesReceived: snapshot?.bytesReceived,
  packetsSent: snapshot?.packetsSent,
  lossEvents: snapshot?.lossEvents,
  generation: session.stamp.generation,
  }
}

function trapConnectionDialogFocus(event: ReactKeyboardEvent<HTMLDivElement>, overlay: HTMLDivElement | null, onClose: () => void): void {
  if (event.key === 'Escape') {
    event.preventDefault()
    onClose()
    return
  }
  if (event.key !== 'Tab' || !overlay) return
  const focusable = Array.from(overlay.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), summary, [href], [tabindex]:not([tabindex="-1"])'))
    .filter((element) => !element.hasAttribute('hidden'))
  if (focusable.length === 0) return
  const first = focusable[0]!
  const last = focusable[focusable.length - 1]!
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

function connectionPolicyUnavailableLabel(reason: ConnectionPolicyState['unavailableReasons']['direct'] | undefined, t: (key: string) => string): string | undefined {
  if (!reason) return undefined
  return t(`workspace.connection.unavailableReason.${reason}`)
}

function connectionRouteKindFromProto(value: ConnectionRouteKind | undefined): ConnectionInfo['routeKind'] {
  switch (value) {
    case ConnectionRouteKind.LOCAL: return 'local'
    case ConnectionRouteKind.DIRECT: return 'direct'
    case ConnectionRouteKind.SSH: return 'ssh'
    case ConnectionRouteKind.MANAGED_CLOUD: return 'cloud'
    default: return undefined
  }
}

function observedPathFromProto(value: CloudObservedPath | undefined): ConnectionInfo['observedPath'] {
  switch (value) {
    case CloudObservedPath.DIRECT: return 'direct'
    case CloudObservedPath.SINGLE_RELAY: return 'single_relay'
    case CloudObservedPath.RELAY_MESH: return 'relay_mesh'
    default: return undefined
  }
}

function candidateTypeFromProto(value: ConnectionCandidateType | undefined): string | undefined {
  switch (value) {
    case ConnectionCandidateType.HOST: return 'host'
    case ConnectionCandidateType.SERVER_REFLEXIVE: return 'srflx'
    case ConnectionCandidateType.PEER_REFLEXIVE: return 'prflx'
    case ConnectionCandidateType.RELAY: return 'relay'
    default: return undefined
  }
}

function transportFromProto(value: ConnectionTransport | undefined): string | undefined {
  switch (value) {
    case ConnectionTransport.UDP: return 'UDP'
    case ConnectionTransport.TCP: return 'TCP'
    default: return undefined
  }
}

function subscribeMachineWorkspaceEvents(session: MachineWorkspaceClientSession, handler: (event: RtcEvent) => void): RtcSubscription {
  let closed = false
  let subscription: RtcSubscription | null = null
  void openProtoEventSubscription(session, create(EventSubscribeCommandSchema, {
    types: [ApplicationEventType.TERMINAL_LIFECYCLE],
  }), (event) => {
    if (event.event.case === 'terminalLifecycle') handler({ type: 'terminal_changed' })
  }).then((opened) => {
    if (closed) opened.close()
    else subscription = opened
  }).catch(() => undefined)
  return { close() { closed = true; subscription?.close(); subscription = null } }
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

function isP2PRouteUnavailable(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error ?? '')
  const normalized = message.trim().toLowerCase()
  return normalized === 'route_unavailable' || normalized.includes('route unavailable') || normalized.includes('webrtc failed')
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
