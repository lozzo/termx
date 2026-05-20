import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties, type ChangeEvent, type ReactNode } from 'react'
import { ArrowLeft, Camera, Download, Keyboard, LaptopMinimal, Loader2, LogIn, Monitor, QrCode, RefreshCw, Server, Settings, ShieldCheck, Trash2, X } from 'lucide-react'
import { createMachineSessionStore, type MachineSessionStore } from '../state/localAppIdentity'
import { MachineWorkspace, type MachineWorkspaceInventoryApi, type MachineWorkspaceConnector } from './MachineWorkspace'
import { createMachineStore, type StoredMachineRecord } from '../state/machineStore'
import { createConnectionOrchestrator, type ConnectionAttemptSnapshot, type HubEndpoint } from '../connection/connectionOrchestrator'
import { connectionStateFromAttempt, createConnectionStatePublisher } from '../connection/connectionState'
import { createHubRtcConnector } from '../webrtc/hubRtcConnector'
import { consoleConnectionLogger } from '../connection/connectionLogger'
import { createHubApi, type HubPairInput, type HubPairResult } from '../api/hubApi'
import { MachineConnectionStore } from '../connection/machineConnectionStore'
import { RemoteNetworkStateManager } from '../connection/remoteNetworkState'
import { FileTransferPanel } from '../files/FileTransferPanel'
import { hapticError, hapticImpact, hapticSelection, hapticSuccess } from '../platform/haptics'
import { addNativeBackHandler } from '../platform/nativeBack'
import { parsePairingPayload, type PairingPayload } from '../state/pairingPayload'
import type { FileTransferContext, TransferInfo } from '../files/fileApi'
import type { ConnectionInfo, LocalStatus, MachineConnectionStateEvents, RemoteNetworkRuntime, RemoteRuntimeStorage, RtcBinaryChannel, RtcConnectOptions, RtcConnectionStateSnapshot, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSessionConnectionStateEvents, RtcSessionLiveness, RtcSessionNegotiator, RtcSubscription, RtcTerminalDataChannelController, TerminalInventoryEvents } from '../core/transport'
import { normalizeTerminalInventory } from '../terminal/terminalInventory'
import { createWebControlApi, type WebControlApi, type WebControlMachine, type WebControlUser } from '../api/webControlApi'
import { normalizeHubBaseUrlCandidate } from '../api/hubUrl'
import {
  TERMINAL_FONT_OPTIONS,
  TERMINAL_THEME_OPTIONS,
  readTerminalSettings,
  resolveTerminalThemeUi,
  terminalThemeCssVariables,
  writeTerminalSettings,
  type TerminalKeyboardMode,
  type TerminalSettings,
  type TerminalThemeOption,
} from '../terminal/terminalSettings'
import type { TerminalRenderer } from '../terminal/Terminal'

const storageKeys = {
  accessToken: 'termx.remote.accessToken',
} as const

const defaultWebControlUrl = ''
const appName = 'TermX Remote App'

function noopSubscribe(_listener: () => void): () => void { return () => {} }

type AppView = 'home' | 'settings' | 'machine'
type PairIntent = 'add-local' | 'authorize-machine'
type ScanFlowState = 'idle' | 'scanning' | 'pairing'
interface PairApi {
  pair(input: PairInput, options?: RtcConnectOptions): Promise<PairResult>
}
type PairInput = HubPairInput
type PairResult = HubPairResult
type HubRtcSessionFactory = (input: { machineId: string }) => RtcSession & RtcSessionNegotiator
type MachineAuthorizationState = 'ready' | 'expired' | 'unauthorized'
const pairingClaimTimeoutMs = 15_000
export interface ScanPairingCodeOptions {
  onCancel?: (() => void) | undefined
  onManualEntry?: (() => void) | undefined
}
type MachineRuntimeFactory = (input: {
  machine: WebControlMachine
  storage: RemoteRuntimeStorage
  api: WebControlApi
  networkRuntime: RemoteNetworkRuntime
  networkStateManager: RemoteNetworkStateManager
  createSession?: HubRtcSessionFactory | undefined
}) => MachineRuntime

interface MachineRuntime {
  api: MachineWorkspaceInventoryApi
  connector: MachineWorkspaceConnector
  inventoryEvents?: TerminalInventoryEvents | undefined
  connectionStateEvents?: MachineConnectionStateEvents | undefined
  fileTransfer?: FileTransferContext | undefined
  dispose?(): void | Promise<void>
}

export interface RemoteControlAppProps {
  defaultControlUrl?: string | undefined
  storage?: RemoteRuntimeStorage | undefined
  networkRuntime?: RemoteNetworkRuntime | undefined
  hubRtcSessionFactory?: HubRtcSessionFactory | undefined
  pairApiFactory?: ((payload: PairingPayload, machine: WebControlMachine) => PairApi) | undefined
  machineRuntimeFactory?: MachineRuntimeFactory | undefined
  globalFileTransfer?: FileTransferContext | undefined
  scanPairingCode?: ((options?: ScanPairingCodeOptions) => Promise<string | null>) | undefined
  exportDebugLogs?: (() => Promise<void>) | undefined
}

export function RemoteControlApp({
  defaultControlUrl,
  storage: storageProp,
  networkRuntime: networkRuntimeProp,
  hubRtcSessionFactory,
  pairApiFactory,
  machineRuntimeFactory = createHubMachineRuntime,
  globalFileTransfer,
  scanPairingCode,
  exportDebugLogs,
}: RemoteControlAppProps) {
  const networkRuntime = networkRuntimeProp ?? unavailableNetworkRuntime
  const storage = storageProp ?? networkRuntime.storage
  const networkStateManagerRef = useRef<RemoteNetworkStateManager | null>(null)
  if (!networkStateManagerRef.current) {
    networkStateManagerRef.current = new RemoteNetworkStateManager()
  }
  const networkStateManager = networkStateManagerRef.current
  const [view, setView] = useState<AppView>('home')
  const controlUrl = useMemo(() => initialControlUrl(defaultControlUrl, networkRuntime), [defaultControlUrl, networkRuntime])
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [accessToken, setAccessToken] = useState(() => storage?.getItem(storageKeys.accessToken) ?? '')
  const [terminalSettings, setTerminalSettings] = useState<TerminalSettings>(() => readTerminalSettings(storage))
  const [user, setUser] = useState<WebControlUser | null>(null)
  const [localMachines, setLocalMachines] = useState<StoredMachineRecord[]>(() => {
    return storage ? createMachineStore({ storage }).listMachines() : []
  })
  const [machines, setMachines] = useState<WebControlMachine[]>([])
  const [selectedMachineId, setSelectedMachineId] = useState<string | null>(null)
  const [scanOpen, setScanOpen] = useState(false)
  const [pairIntent, setPairIntent] = useState<PairIntent>('add-local')
  const [transferCenterOpen, setTransferCenterOpen] = useState(false)
  const [manualScanValue, setManualScanValue] = useState('')
  const [manualEntryOpen, setManualEntryOpen] = useState(false)
  const [lastImported, setLastImported] = useState<PairingPayload | null>(null)
  const [authorizedMachineIds, setAuthorizedMachineIds] = useState(() => readAuthorizedMachineIds(storage, undefined))
  const [authorizationExpiries, setAuthorizationExpiries] = useState(() => readAuthorizationExpiries(storage))
  const [pairVersion, setPairVersion] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [pairing, setPairing] = useState(false)
  const [cameraScanning, setCameraScanning] = useState(false)
  const [scanFlowState, setScanFlowState] = useState<ScanFlowState>('idle')
  const [scanAutoStartToken, setScanAutoStartToken] = useState(0)
  const signedIn = accessToken.trim() !== ''
  const appThemeStyle = useMemo(() => terminalThemeCssVariables(terminalSettings.themeId) as CSSProperties, [terminalSettings.themeId])
  const autoStartedScanTokenRef = useRef(0)
  const cameraScanInFlightRef = useRef(false)
  const runtimeCacheRef = useRef<{
    api: WebControlApi
    createSession: HubRtcSessionFactory | undefined
    networkRuntime: RemoteNetworkRuntime
    runtimeFactory: MachineRuntimeFactory
    storage: RemoteRuntimeStorage
    runtimes: Map<string, MachineRuntime>
  } | null>(null)

  const api = useMemo(() => {
    if (controlUrl.trim() === '') {
      return createUnavailableWebControlApi('Web Control URL is required')
    }
    return createWebControlApi({
      baseUrl: controlUrl,
      ...(accessToken ? { accessToken } : {}),
      fetch: networkRuntime.fetch,
    })
  }, [accessToken, controlUrl, networkRuntime])

  if (storage) {
    const cache = runtimeCacheRef.current
    const cacheMatches = cache &&
      cache.api === api &&
      cache.createSession === hubRtcSessionFactory &&
      cache.networkRuntime === networkRuntime &&
      cache.runtimeFactory === machineRuntimeFactory &&
      cache.storage === storage
    if (!cacheMatches) {
      if (cache) {
        for (const runtime of cache.runtimes.values()) void runtime.dispose?.()
      }
      runtimeCacheRef.current = {
        api,
        createSession: hubRtcSessionFactory,
        networkRuntime,
        runtimeFactory: machineRuntimeFactory,
        storage,
        runtimes: new Map(),
      }
    }
  }

  const dropMachineRuntime = useCallback((machineId: string) => {
    const runtime = runtimeCacheRef.current?.runtimes.get(machineId)
    if (!runtime) return
    runtimeCacheRef.current?.runtimes.delete(machineId)
    void runtime.dispose?.()
  }, [])

  const getMachineRuntime = useCallback((machine: WebControlMachine): MachineRuntime | null => {
    if (!storage || !runtimeCacheRef.current) return null
    const cache = runtimeCacheRef.current.runtimes
    const existing = cache.get(machine.id)
    if (existing) return existing
    const created = machineRuntimeFactory({
      machine,
      storage,
      api,
      networkRuntime,
      networkStateManager,
      createSession: hubRtcSessionFactory,
    })
    cache.set(machine.id, created)
    return created
  }, [api, machineRuntimeFactory, hubRtcSessionFactory, networkRuntime, networkStateManager, storage])

  useEffect(() => {
    networkStateManager.init()
    return () => networkStateManager.destroy()
  }, [networkStateManager])

  useEffect(() => {
    return () => {
      const cache = runtimeCacheRef.current
      if (!cache) return
      runtimeCacheRef.current = null
      for (const runtime of cache.runtimes.values()) void runtime.dispose?.()
    }
  }, [])

  useEffect(() => {
    if (storage) {
      setLocalMachines(createMachineStore({ storage }).listMachines())
    }
  }, [storage, pairVersion])

  const displayMachines = useMemo(() => {
    const map = new Map<string, WebControlMachine>()
    const localById = new Map(localMachines.map((machine) => [machine.machineId, machine]))
    for (const local of localMachines) {
      map.set(local.machineId, {
        id: local.machineId,
        name: local.name,
        hostname: local.hostname,
        online: local.state === 'online',
        source: local.source === 'hub' ? 'hub' : 'local',
        hubUrls: hubUrlsFromStoredMachine(local),
        localHubUrls: localHubUrlsFromStoredMachine(local),
        localFallbackHubUrls: localFallbackHubUrlsFromStoredMachine(local),
      })
    }
    for (const hub of machines) {
      const local = localById.get(hub.id)
      map.set(hub.id, {
        ...hub,
        ...(local ? {
          localHubUrls: localHubUrlsFromStoredMachine(local),
          localFallbackHubUrls: localFallbackHubUrlsFromStoredMachine(local, hub.hubUrls),
        } : {}),
      })
    }
    return Array.from(map.values())
  }, [localMachines, machines])

  const selectedMachine = displayMachines.find((machine) => machine.id === selectedMachineId) ?? null
  const emptyTransferSnapshot = useMemo(() => ({ transfers: [], hasActiveTransfers: false }), [])
  const globalTransferState = useSyncExternalStore(
    globalFileTransfer?.subscribe ?? noopSubscribe,
    globalFileTransfer?.getSnapshot ?? (() => emptyTransferSnapshot),
  )

  const refreshMachines = useCallback(async () => {
    if (!accessToken) return
    setLoading(true)
    try {
      let localMachineList = storage ? createMachineStore({ storage }).listMachines() : []
      const [profile, hubMachines] = await Promise.all([
        api.me(),
        api.listMachines(),
      ])
      setUser(profile)
      setMachines(hubMachines)
      if (storage) {
        syncSignedInHubMachines(storage, hubMachines)
        localMachineList = createMachineStore({ storage }).listMachines()
      }
      setLocalMachines(localMachineList)
      setAuthorizedMachineIds(readAuthorizedMachineIds(storage, profile.id))
      setAuthorizationExpiries(readAuthorizationExpiries(storage))
      setSelectedMachineId((current) => {
        if (
          current &&
          (hubMachines.some((machine) => machine.id === current) ||
            localMachineList.some((machine) => machine.machineId === current))
        ) {
          return current
        }
        return null
      })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [accessToken, api, storage])

  const prepareTransferMachineRuntime = useCallback((transferId?: string) => {
    if (!globalFileTransfer || !transferId) return
    const transfer = globalFileTransfer.getSnapshot().transfers.find((item) => item.id === transferId)
    const machineId = transfer?.machineId
    if (!machineId) return
    const machine = displayMachines.find((item) => item.id === machineId)
    if (!machine) return
    getMachineRuntime(machine)
  }, [displayMachines, getMachineRuntime, globalFileTransfer])

  const resumeGlobalTransfer = useCallback(async (transferId: string) => {
    prepareTransferMachineRuntime(transferId)
    await globalFileTransfer?.resumeTransfer?.(transferId)
  }, [globalFileTransfer, prepareTransferMachineRuntime])

  const resumeAllGlobalTransfers = useCallback(async () => {
    if (!globalFileTransfer) return
    const machineIds = new Set(
      globalFileTransfer.getSnapshot().transfers
        .map((transfer) => transfer.machineId)
        .filter((machineId): machineId is string => Boolean(machineId)),
    )
    for (const machineId of machineIds) {
      const machine = displayMachines.find((item) => item.id === machineId)
      if (!machine) continue
      getMachineRuntime(machine)
    }
    await globalFileTransfer.resumeAllTransfers?.()
  }, [displayMachines, getMachineRuntime, globalFileTransfer])

  useEffect(() => {
    void refreshMachines()
  }, [refreshMachines])

  useEffect(() => {
    setAuthorizedMachineIds(readAuthorizedMachineIds(storage, user?.id))
    setAuthorizationExpiries(readAuthorizationExpiries(storage))
  }, [pairVersion, storage, user?.id])

  const submitLogin = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const auth = await api.login({ login, password })
      storage?.setItem(storageKeys.accessToken, auth.accessToken)
      setAccessToken(auth.accessToken)
      setUser(auth.user)
      setPassword('')
      setView('home')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [api, controlUrl, login, password, storage])

  const signOut = useCallback(() => {
    storage?.removeItem(storageKeys.accessToken)
    const signedOutMachines = pruneMachinesForSignOut(storage)
    setAccessToken('')
    setUser(null)
    setMachines([])
    setLocalMachines(signedOutMachines)
    setAuthorizedMachineIds(readAuthorizedMachineIds(storage, undefined))
    setAuthorizationExpiries(readAuthorizationExpiries(storage))
    setPairVersion((current) => current + 1)
    setSelectedMachineId(null)
    setError(null)
    setView('settings')
  }, [storage])

  const updateTerminalSettings = useCallback((patch: Partial<TerminalSettings>) => {
    setTerminalSettings((current) => writeTerminalSettings({ ...current, ...patch }, storage))
  }, [storage])

  const openAddLocalSheet = useCallback(() => {
    hapticImpact()
    setSelectedMachineId(null)
    setPairIntent('add-local')
    setManualScanValue('')
    setManualEntryOpen(false)
    setLastImported(null)
    setError(null)
    setScanOpen(true)
    setScanAutoStartToken((current) => current + 1)
  }, [])

  const openPairSheet = useCallback((machineId: string) => {
    hapticImpact()
    setSelectedMachineId(machineId)
    setPairIntent('authorize-machine')
    setManualScanValue('')
    setManualEntryOpen(false)
    setLastImported(null)
    setError(null)
    setScanOpen(true)
    setScanAutoStartToken((current) => current + 1)
  }, [])

  const openMachinePairSheet = useCallback((machine: WebControlMachine) => {
    openPairSheet(machine.id)
  }, [openPairSheet])

  const selectMachine = useCallback((machine: WebControlMachine) => {
    hapticImpact()
    setSelectedMachineId(machine.id)
    if (!authorizedMachineIds.has(machine.id)) {
      openMachinePairSheet(machine)
      return
    }
    setView('machine')
    setError(null)
  }, [openMachinePairSheet, authorizedMachineIds])

  const pairScannedValue = useCallback(async (rawValue: string) => {
    if (!storage) {
      setError('Local storage is required before importing a TermX QR')
      return
    }
    setPairing(true)
    setScanFlowState('pairing')
    setError(null)
    try {
      const payload = parsePairingPayload(rawValue)
      console.info('[termx:pairing] QR parsed', {
        machineId: payload.machine.id,
        localHubUrlCount: payload.local.hubUrls.length,
      })
      const hubMachine = machines.find((machine) => machine.id === payload.machine.id)
      const localHubUrls = localHubUrlsFromPairingPayload(payload)

      let targetMachine: WebControlMachine

      if (pairIntent === 'authorize-machine' && !selectedMachine) {
        throw new Error('Choose a machine before re-authorizing it')
      }
      if (selectedMachine && selectedMachine.id !== payload.machine.id) {
        throw new Error(`This pairing code belongs to ${payload.machine.name}, not ${selectedMachine.name}`)
      }

      if (hubMachine) {
        targetMachine = {
          ...hubMachine,
          localHubUrls,
          localFallbackHubUrls: [],
        }
      } else {
        if (localHubUrls.length === 0) {
          throw new Error('This pairing code does not include local addresses. Sign in and pair the machine from your Web Control list.')
        }
        targetMachine = {
          id: payload.machine.id,
          name: payload.machine.name,
          hostname: payload.machine.hostname,
          online: true,
          source: 'local',
          hubUrls: [],
          localHubUrls,
          localFallbackHubUrls: [],
        }
      }

      const pairInput = {
        machineId: targetMachine.id,
        pairSessionId: payload.pairing.sessionId,
        pairSecret: payload.pairing.secret,
        appDeviceId: createBrowserAppDeviceId(),
        appName,
        requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
      }
      const pairApi = pairApiFactory
        ? pairApiFactory(payload, targetMachine)
        : createPairApiForScan({
          candidateHubUrls: candidatePairingHubUrls(targetMachine, localHubUrls),
          machine: targetMachine,
          networkRuntime,
        })
      const pairResult = await runWithTimeout(
        (signal) => pairApi.pair(pairInput, { signal }),
        pairingClaimTimeoutMs,
        'Pairing timed out. Make sure this phone can reach the QR code address and that the TermX agent is still online.',
      )
      console.info('[termx:pairing] pair claim succeeded', {
        machineId: pairResult.machineId,
        hubEndpointCount: candidatePairingHubUrls(targetMachine, localHubUrls).length,
      })
      if (pairResult.machineId !== targetMachine.id) {
        throw new Error(`pairing response machine mismatch: ${pairResult.machineId} != ${targetMachine.id}`)
      }
      createMachineSessionStore(storage).saveSessionToken(pairResult.machineId, pairResult.sessionToken, pairResult.expiresAt, payload.pairing.answerProofSecret)
      dropMachineRuntime(pairResult.machineId)
      const store = createMachineStore({ storage })
      const saved = store.saveFromPairingPayload(payload)
      if (hubMachine) {
        store.saveMachine(mergeHubMachine(saved, targetMachine))
      }
      setSelectedMachineId(targetMachine.id)
      setAuthorizedMachineIds(readAuthorizedMachineIds(storage, user?.id))
      setAuthorizationExpiries(readAuthorizationExpiries(storage))
      setPairVersion((current) => current + 1)
      setLastImported(payload)
      setError(null)
      setManualScanValue('')
      setScanOpen(false)
      setView('machine')
      hapticSuccess()
    } catch (err) {
      hapticError()
      console.warn('[termx:pairing] pair claim failed', err instanceof Error ? err.message : String(err))
      setError(err instanceof Error ? err.message : String(err))
      setManualEntryOpen(true)
    } finally {
      setPairing(false)
      setScanFlowState('idle')
    }
  }, [dropMachineRuntime, machines, networkRuntime, pairApiFactory, pairIntent, selectedMachine, storage, user?.id])

  const importManualScan = useCallback(async () => {
    hapticImpact()
    await pairScannedValue(manualScanValue)
  }, [manualScanValue, pairScannedValue])

  const scanWithCamera = useCallback(async () => {
    if (!scanPairingCode) return
    if (cameraScanInFlightRef.current) return
    cameraScanInFlightRef.current = true
    setScanAutoStartToken(0)
    hapticImpact()
    setManualEntryOpen(false)
    setCameraScanning(true)
    setScanFlowState('scanning')
    setError(null)
    try {
      const value = await scanPairingCode({
        onCancel: () => setScanOpen(false),
        onManualEntry: () => setManualEntryOpen(true),
      })
      if (!value) return
      setManualScanValue(value)
      await pairScannedValue(value)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      cameraScanInFlightRef.current = false
      setCameraScanning(false)
      setScanFlowState((current) => current === 'scanning' ? 'idle' : current)
    }
  }, [pairScannedValue, scanPairingCode])

  useEffect(() => {
    if (!scanOpen || !scanPairingCode || manualEntryOpen || cameraScanning || scanAutoStartToken === 0) return
    if (autoStartedScanTokenRef.current === scanAutoStartToken) return
    autoStartedScanTokenRef.current = scanAutoStartToken
    void scanWithCamera()
  }, [cameraScanning, manualEntryOpen, scanAutoStartToken, scanOpen, scanPairingCode, scanWithCamera])

  const handleMachineNeedsReauthorization = useCallback((machineId: string) => {
    if (!storage) return
    createMachineSessionStore(storage).clearSessionToken(machineId)
    dropMachineRuntime(machineId)
    setAuthorizedMachineIds(readAuthorizedMachineIds(storage, user?.id))
    setAuthorizationExpiries(readAuthorizationExpiries(storage))
    setPairVersion((current) => current + 1)
    setSelectedMachineId(machineId)
    setPairIntent('authorize-machine')
    setManualScanValue('')
    setManualEntryOpen(false)
    setLastImported(null)
    setError('This phone needs a fresh machine authorization. Scan the machine QR again.')
    setScanOpen(true)
    setScanAutoStartToken((current) => current + 1)
  }, [dropMachineRuntime, storage, user?.id])

  const forgetMachineAuthorization = useCallback((machine: WebControlMachine) => {
    if (!storage) return
    const store = createMachineStore({ storage })
    const sessionStore = createMachineSessionStore(storage)
    sessionStore.clearSessionToken(machine.id)
    dropMachineRuntime(machine.id)
    const stillVisibleFromHub = machines.some((hubMachine) => hubMachine.id === machine.id)
    if (!stillVisibleFromHub) {
      store.forgetMachine(machine.id)
    }
    setLocalMachines(store.listMachines())
    setAuthorizedMachineIds(readAuthorizedMachineIds(storage, user?.id))
    setAuthorizationExpiries(readAuthorizationExpiries(storage))
    setPairVersion((current) => current + 1)
    setSelectedMachineId((current) => current === machine.id ? null : current)
    setView((current) => current === 'machine' && selectedMachineId === machine.id ? 'home' : current)
    setError(null)
  }, [dropMachineRuntime, machines, selectedMachineId, storage, user?.id])

  useEffect(() => addNativeBackHandler(() => {
    if (scanOpen) {
      setScanOpen(false)
      return true
    }
    if (view === 'settings') {
      setView('home')
      return true
    }
    if (view === 'machine') {
      setView('home')
      setError(null)
      return true
    }
    return false
  }, 10), [scanOpen, view])

  return (
    <main
      className="flex h-full min-h-0 flex-col bg-zinc-50 text-zinc-950"
      data-testid="termx-web-control-remote"
      style={appThemeStyle}
    >
      {view === 'settings' ? (
        <SettingsView
          error={error}
          controlUrl={controlUrl}
          loading={loading}
          login={login}
          password={password}
          signedIn={signedIn}
          terminalSettings={terminalSettings}
          user={user}
          onBack={() => { hapticSelection(); setView('home') }}
          onLoginChange={setLogin}
          onPasswordChange={setPassword}
          onRefresh={() => { hapticImpact(); void refreshMachines() }}
          onSignIn={() => { hapticImpact(); void submitLogin() }}
          onSignOut={() => { hapticImpact(); signOut() }}
          onTerminalSettingsChange={updateTerminalSettings}
          onExportDebugLogs={exportDebugLogs}
        />
      ) : view === 'machine' && selectedMachine ? (
        <MachineTerminalListView
          machine={selectedMachine}
          storage={storage}
          terminalSettings={terminalSettings}
          runtime={getMachineRuntime(selectedMachine)}
          onBack={() => {
            hapticSelection()
            setView('home')
            setError(null)
          }}
          onNeedsReauthorization={handleMachineNeedsReauthorization}
          onTerminalSettingsChange={updateTerminalSettings}
        />
      ) : (
        <HomeView
          fileTransfer={globalFileTransfer}
          transferState={globalTransferState as { transfers: TransferInfo[]; hasActiveTransfers: boolean }}
          loading={loading}
          machines={displayMachines}
          authorizedMachineIds={authorizedMachineIds}
          authorizationExpiries={authorizationExpiries}
          signedIn={signedIn}
          user={user}
          onAddLocalDevice={openAddLocalSheet}
          onOpenSettings={() => { hapticSelection(); setView('settings') }}
          onOpenTransferCenter={() => { hapticSelection(); setTransferCenterOpen(true) }}
          onForgetMachineAuthorization={forgetMachineAuthorization}
          onPairMachine={openMachinePairSheet}
          onRefresh={() => { hapticImpact(); void refreshMachines() }}
          onSelectMachine={selectMachine}
          onSignIn={() => { hapticSelection(); setView('settings') }}
        />
      )}

      {scanOpen ? (
        <PairSheet
          lastImported={lastImported}
          manualEntryOpen={manualEntryOpen}
          manualScanValue={manualScanValue}
          pairError={error}
          scanFlowState={scanFlowState}
          pairing={pairing}
          cameraScanning={cameraScanning}
          pairIntent={pairIntent}
          selectedMachine={selectedMachine}
          signedIn={signedIn}
          canScanWithCamera={Boolean(scanPairingCode)}
          onClose={() => { hapticSelection(); setScanOpen(false) }}
          onImport={() => void importManualScan()}
          onManualEntryOpen={() => { hapticSelection(); setManualEntryOpen(true) }}
          onManualScanValueChange={setManualScanValue}
          onScanWithCamera={() => void scanWithCamera()}
        />
      ) : null}
      {transferCenterOpen ? (
        <GlobalTransferCenter
          fileTransfer={globalFileTransfer}
          resolveMachineLabel={(machineId) => displayMachines.find((machine) => machine.id === machineId)?.name ?? machineId}
          onClose={() => { hapticSelection(); setTransferCenterOpen(false) }}
          onResumeTransfer={resumeGlobalTransfer}
          onResumeAllTransfers={resumeAllGlobalTransfers}
        />
      ) : null}
    </main>
  )
}

function GlobalTransferCenter({
  fileTransfer,
  resolveMachineLabel,
  onClose,
  onResumeTransfer,
  onResumeAllTransfers,
}: {
  fileTransfer: FileTransferContext | undefined
  resolveMachineLabel?: ((machineId: string | undefined) => string | null | undefined) | undefined
  onClose: () => void
  onResumeTransfer?: ((id: string) => void | Promise<void>) | undefined
  onResumeAllTransfers?: (() => void | Promise<void>) | undefined
}) {
  const emptySnapshot = useMemo(() => ({ transfers: [], hasActiveTransfers: false }), [])
  const transferState = useSyncExternalStore(
    fileTransfer?.subscribe ?? noopSubscribe,
    fileTransfer?.getSnapshot ?? (() => emptySnapshot),
  )

  if (!fileTransfer) return null

  return (
    <FileTransferPanel
      transfers={transferState.transfers}
      hasActiveTransfers={transferState.hasActiveTransfers}
      resolveMachineLabel={resolveMachineLabel}
      onCancel={(id) => fileTransfer.cancelTransfer(id)}
      onDismiss={(id) => fileTransfer.dismissTransfer(id)}
      onPause={(id) => fileTransfer.pauseTransfer?.(id)}
      onResume={onResumeTransfer ?? ((id) => fileTransfer.resumeTransfer?.(id))}
      onResumeAll={onResumeAllTransfers ?? (() => fileTransfer.resumeAllTransfers?.())}
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    />
  )
}

function MachineTerminalListView({
  machine,
  storage,
  terminalSettings,
  runtime,
  onBack,
  onNeedsReauthorization,
  onTerminalSettingsChange,
}: {
  machine: WebControlMachine
  storage: RemoteRuntimeStorage | undefined
  terminalSettings: TerminalSettings
  runtime: MachineRuntime | null
  onBack: () => void
  onNeedsReauthorization: (machineId: string) => void
  onTerminalSettingsChange: (patch: Partial<TerminalSettings>) => void
}) {
  if (!storage || !runtime) {
    return (
      <MachineRuntimeErrorShell
        machine={machine}
        onBack={onBack}
      />
    )
  }
  return (
    <section className="flex min-h-0 flex-1 flex-col bg-zinc-50 animate-in fade-in slide-in-from-right-4 duration-200" data-testid="termx-machine-terminal-list">
      <MachineWorkspace
        api={runtime.api}
        connector={runtime.connector}
        className="min-h-0 flex-1"
        initialMachine={{
          machineId: machine.id,
          name: machine.name,
          state: machine.online ? 'online' : 'offline',
          ...(machine.lastSeen ? { lastSeenAt: machine.lastSeen } : {}),
        }}
        connectionStateEvents={runtime.connectionStateEvents}
        inventoryEvents={runtime.inventoryEvents}
        fileTransfer={runtime.fileTransfer}
        terminalSettings={terminalSettings}
        onNeedsReauthorization={onNeedsReauthorization}
        onTerminalSettingsChange={onTerminalSettingsChange}
        onBack={onBack}
      />
    </section>
  )
}

function MachineRuntimeHeader({ machine, onBack }: { machine: WebControlMachine; onBack: () => void }) {
  return (
    <header className="flex min-h-14 shrink-0 items-center gap-3 border-b border-zinc-200 bg-white px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
      <button
        aria-label="Back to machines"
        className="inline-flex h-10 w-10 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
        type="button"
        onClick={() => { hapticSelection(); onBack() }}
      >
        <ArrowLeft className="h-5 w-5" />
      </button>
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <Monitor className="h-5 w-5 shrink-0 text-zinc-500" />
        <div className="min-w-0">
          <h1 className="truncate text-base font-semibold leading-6 text-zinc-950">{machine.name}</h1>
          <p className="truncate text-xs font-medium text-zinc-500">{machine.hostname || machine.id}</p>
        </div>
      </div>
      <span className={`shrink-0 rounded-md px-2 py-0.5 text-[11px] font-semibold leading-4 ring-1 ${machine.online ? 'bg-emerald-50 text-emerald-700 ring-emerald-200' : 'bg-zinc-100 text-zinc-600 ring-zinc-200'}`}>
        {machine.online ? 'Online' : 'Offline'}
      </span>
    </header>
  )
}

function MachineRuntimeErrorShell({
  machine,
  onBack,
}: {
  machine: WebControlMachine
  onBack: () => void
}) {
  return (
    <section className="flex min-h-0 flex-1 flex-col bg-zinc-50 animate-in fade-in slide-in-from-right-4 duration-200" data-testid="termx-machine-terminal-list">
      <MachineRuntimeHeader machine={machine} onBack={onBack} />
    </section>
  )
}

function HomeView({
  fileTransfer,
  transferState,
  loading,
  machines,
  authorizedMachineIds,
  authorizationExpiries,
  signedIn,
  user,
  onAddLocalDevice,
  onForgetMachineAuthorization,
  onOpenSettings,
  onOpenTransferCenter,
  onPairMachine,
  onRefresh,
  onSelectMachine,
  onSignIn,
}: {
  fileTransfer?: FileTransferContext | undefined
  transferState: { transfers: TransferInfo[]; hasActiveTransfers: boolean }
  loading: boolean
  machines: WebControlMachine[]
  authorizedMachineIds: Set<string>
  authorizationExpiries: Map<string, string>
  signedIn: boolean
  user: WebControlUser | null
  onAddLocalDevice: () => void
  onForgetMachineAuthorization: (machine: WebControlMachine) => void
  onOpenSettings: () => void
  onOpenTransferCenter: () => void
  onPairMachine: (machine: WebControlMachine) => void
  onRefresh: () => void
  onSelectMachine: (machine: WebControlMachine) => void
  onSignIn: () => void
}) {
  return (
    <section className="flex min-h-0 flex-1 flex-col" data-testid="termx-app-home">
      <header className="flex min-h-14 shrink-0 items-center justify-between gap-3 border-b border-zinc-200 bg-white px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
        <div className="min-w-0">
          <h1 className="text-lg font-semibold leading-6">Machines</h1>
          <p className="truncate text-xs font-medium text-zinc-500">
            {signedIn ? `${machines.length} available${user?.email ? ` / ${user.email}` : ''}` : 'Sign in to sync devices'}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {signedIn ? (
            <button
              aria-label="Refresh machines"
              className="inline-flex h-10 w-10 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
              type="button"
              onClick={onRefresh}
              disabled={loading}
            >
              <RefreshCw className={`h-5 w-5 ${loading ? 'animate-spin' : ''}`} />
            </button>
          ) : null}
          <button
            aria-label="Scan new machine"
            className="inline-flex h-10 w-10 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            type="button"
            onClick={onAddLocalDevice}
          >
            <QrCode className="h-5 w-5" />
          </button>
          {fileTransfer ? (
            <button
              aria-label="Open data transfer center"
              className="relative inline-flex h-10 w-10 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
              type="button"
              onClick={onOpenTransferCenter}
            >
              <Download className="h-5 w-5" />
              {transferState.hasActiveTransfers ? <span className="absolute right-2 top-2 h-2 w-2 rounded-full bg-emerald-500" /> : null}
            </button>
          ) : null}
          <button
            aria-label="Open settings"
            className="inline-flex h-10 w-10 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            type="button"
            onClick={onOpenSettings}
          >
            <Settings className="h-5 w-5" />
          </button>
        </div>
      </header>

      {machines.length === 0 ? (
        <EmptyState
          actionLabel="Scan QR"
          icon="scan"
          message="No devices found. Add a local device, or sign in to sync your Hub devices."
          onAction={onAddLocalDevice}
          title="No machines yet"
        />
      ) : (
        <ul aria-label="Machines" className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
          {machines.map((machine) => (
            <li key={machine.id} className="mb-2 last:mb-0">
              <MachineRow
                authorizationExpiresAt={authorizationExpiries.get(machine.id)}
                authorizationState={machineAuthorizationState(machine, authorizedMachineIds, authorizationExpiries)}
                machine={machine}
                onForgetMachineAuthorization={onForgetMachineAuthorization}
                onPairMachine={onPairMachine}
                onSelectMachine={onSelectMachine}
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function SettingsView({
  controlUrl,
  error,
  loading,
  login,
  password,
  signedIn,
  terminalSettings,
  user,
  onBack,
  onLoginChange,
  onPasswordChange,
  onRefresh,
  onSignIn,
  onSignOut,
  onTerminalSettingsChange,
  onExportDebugLogs,
}: {
  controlUrl: string
  error: string | null
  loading: boolean
  login: string
  password: string
  signedIn: boolean
  terminalSettings: TerminalSettings
  user: WebControlUser | null
  onBack: () => void
  onLoginChange: (value: string) => void
  onPasswordChange: (value: string) => void
  onRefresh: () => void
  onSignIn: () => void
  onSignOut: () => void
  onTerminalSettingsChange: (patch: Partial<TerminalSettings>) => void
  onExportDebugLogs?: (() => Promise<void>) | undefined
}) {
  const handleNumberSetting = (key: 'fontSize' | 'scrollback' | 'scrollbackPrefetchThresholdRows', min: number, max: number) =>
    (event: ChangeEvent<HTMLInputElement>) => {
      const value = Number(event.currentTarget.value)
      if (!Number.isFinite(value)) return
      onTerminalSettingsChange({ [key]: Math.max(min, Math.min(max, Math.round(value))) })
    }
  const themeGroups = useMemo(() => ({
    dark: TERMINAL_THEME_OPTIONS.filter((option) => option.group === 'dark'),
    light: TERMINAL_THEME_OPTIONS.filter((option) => option.group === 'light'),
  }), [])

  return (
    <section className="flex min-h-0 flex-1 flex-col bg-zinc-50 text-zinc-950 animate-in fade-in slide-in-from-bottom-4 duration-200" data-testid="termx-app-settings">
      <header className="flex min-h-14 shrink-0 items-center gap-3 border-b border-zinc-200/70 bg-white px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
        <button
          aria-label="Back to machines"
          className="inline-flex h-10 w-10 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-700 active:opacity-75 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
          type="button"
          onClick={onBack}
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div className="min-w-0">
          <h1 className="text-lg font-semibold leading-6 text-zinc-900">Settings</h1>
          <p className="truncate text-xs font-medium text-zinc-500">{signedIn ? user?.email ?? 'Signed in' : 'Web Control sign in'}</p>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-5 pb-[calc(env(safe-area-inset-bottom)+1.5rem)]">
        <div className="mx-auto flex w-full max-w-xl flex-col gap-6">
          {error ? (
            <p className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm font-medium text-red-700">{error}</p>
          ) : null}

          <SettingsSection title="Connection">
            <SettingsRow
              label="Web Control"
              value={controlUrl || 'Built-in endpoint'}
            />
          </SettingsSection>

          {onExportDebugLogs ? (
            <SettingsSection title="Diagnostics">
              <div className="px-4 py-3">
                <button
                  className="inline-flex h-11 w-full items-center justify-center gap-2 rounded-lg bg-zinc-900 px-3 text-sm font-semibold text-white hover:bg-zinc-800 active:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-60"
                  type="button"
                  onClick={() => {
                    hapticImpact()
                    void onExportDebugLogs()
                  }}
                >
                  <Download className="h-4 w-4" />
                  Export logs
                </button>
              </div>
            </SettingsSection>
          ) : null}

          <SettingsSection title="Terminal">
            <SettingsRow label="Font size">
              <div className="inline-flex h-9 items-center overflow-hidden rounded-lg border border-zinc-200 bg-white">
                <button
                  aria-label="Decrease terminal font size"
                  className="h-9 w-9 text-lg font-semibold text-zinc-700 hover:bg-zinc-50 active:bg-zinc-100"
                  type="button"
                  onClick={() => { hapticSelection(); onTerminalSettingsChange({ fontSize: Math.max(8, terminalSettings.fontSize - 1) }) }}
                >
                  -
                </button>
                <input
                  aria-label="Terminal font size"
                  className="h-9 w-12 border-x border-zinc-200 bg-zinc-50 px-1 text-center text-sm font-semibold text-zinc-900 outline-none focus:ring-2 focus:ring-blue-500/25"
                  inputMode="numeric"
                  max={32}
                  min={8}
                  type="number"
                  value={terminalSettings.fontSize}
                  onChange={handleNumberSetting('fontSize', 8, 32)}
                />
                <button
                  aria-label="Increase terminal font size"
                  className="h-9 w-9 text-lg font-semibold text-zinc-700 hover:bg-zinc-50 active:bg-zinc-100"
                  type="button"
                  onClick={() => { hapticSelection(); onTerminalSettingsChange({ fontSize: Math.min(32, terminalSettings.fontSize + 1) }) }}
                >
                  +
                </button>
              </div>
            </SettingsRow>
            <SettingsRow label="Font" stacked>
              <FontPicker
                value={terminalSettings.fontFamily}
                onChange={(value) => onTerminalSettingsChange({ fontFamily: value })}
              />
            </SettingsRow>
            <SettingsRow label="Theme" stacked>
              <ThemePicker
                groups={themeGroups}
                value={terminalSettings.themeId}
                onChange={(value) => onTerminalSettingsChange({ themeId: value })}
              />
            </SettingsRow>
            <SettingsRow label="Renderer">
              <SettingsSelect
                ariaLabel="Terminal renderer"
                value={terminalSettings.renderer}
                onChange={(value) => { hapticSelection(); onTerminalSettingsChange({ renderer: value as TerminalRenderer }) }}
              >
                <option value="auto">Auto</option>
                <option value="webgl">WebGL</option>
                <option value="canvas">Canvas</option>
                <option value="dom">DOM</option>
              </SettingsSelect>
            </SettingsRow>
            <SettingsRow label="Keyboard">
              <SettingsSelect
                ariaLabel="Terminal keyboard mode"
                value={terminalSettings.keyboardMode}
                onChange={(value) => { hapticSelection(); onTerminalSettingsChange({ keyboardMode: value as TerminalKeyboardMode }) }}
              >
                <option value="auto">Auto</option>
                <option value="resize">Resize</option>
                <option value="shift">Shift up</option>
              </SettingsSelect>
            </SettingsRow>
            <SettingsRow label="Scrollback">
              <input
                aria-label="Terminal scrollback"
                className="h-9 w-28 rounded-lg border border-zinc-200 bg-white px-3 text-right text-sm font-semibold text-zinc-900 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/25"
                inputMode="numeric"
                max={50000}
                min={500}
                step={500}
                type="number"
                value={terminalSettings.scrollback}
                onChange={handleNumberSetting('scrollback', 500, 50000)}
              />
            </SettingsRow>
            <SettingsRow label="Prefetch threshold">
              <input
                aria-label="Terminal scrollback prefetch threshold"
                className="h-9 w-28 rounded-lg border border-zinc-200 bg-white px-3 text-right text-sm font-semibold text-zinc-900 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/25"
                inputMode="numeric"
                max={1000}
                min={0}
                step={10}
                type="number"
                value={terminalSettings.scrollbackPrefetchThresholdRows}
                onChange={handleNumberSetting('scrollbackPrefetchThresholdRows', 0, 1000)}
              />
            </SettingsRow>
            <SettingsRow label="Cursor blink">
              <Switch
                ariaLabel="Terminal cursor blink"
                checked={terminalSettings.cursorBlink}
                onChange={(checked) => onTerminalSettingsChange({ cursorBlink: checked })}
              />
            </SettingsRow>
          </SettingsSection>

          <SettingsSection title="Account">
            {signedIn ? (
              <>
                <SettingsRow label="Signed in" value={user?.email ?? 'Account'} />
                <div className="grid grid-cols-2 gap-2 px-4 py-3">
                  <button
                    className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 active:bg-zinc-50 disabled:cursor-not-allowed disabled:opacity-60"
                    type="button"
                    onClick={onRefresh}
                    disabled={loading}
                  >
                    <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                    Refresh
                  </button>
                  <button
                    className="inline-flex h-10 items-center justify-center rounded-lg bg-blue-600 px-3 text-sm font-semibold text-white active:bg-blue-700"
                    type="button"
                    onClick={onSignOut}
                  >
                    Sign out
                  </button>
                </div>
              </>
            ) : (
              <>
                <div className="px-4 py-3">
                  <label className="block text-sm font-medium text-zinc-500">
                    Email or username
                    <input
                      className="mt-2 h-11 w-full rounded-lg border border-zinc-200 bg-white px-3 text-sm text-zinc-900 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/25"
                      value={login}
                      onChange={(event) => onLoginChange(event.target.value)}
                      autoComplete="username"
                    />
                  </label>
                </div>
                <div className="border-t border-zinc-200 px-4 py-3">
                  <label className="block text-sm font-medium text-zinc-500">
                    Password
                    <input
                      className="mt-2 h-11 w-full rounded-lg border border-zinc-200 bg-white px-3 text-sm text-zinc-900 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/25"
                      value={password}
                      onChange={(event) => onPasswordChange(event.target.value)}
                      type="password"
                      autoComplete="current-password"
                    />
                  </label>
                </div>
                <div className="border-t border-zinc-200 px-4 py-3">
                  <button
                    className="inline-flex h-11 w-full items-center justify-center gap-2 rounded-lg bg-blue-600 px-3 text-sm font-semibold text-white hover:bg-blue-600/90 active:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-60"
                    type="button"
                    onClick={onSignIn}
                    disabled={loading}
                  >
                    <LogIn className="h-4 w-4" />
                    Sign in
                  </button>
                </div>
              </>
            )}
          </SettingsSection>
        </div>
      </div>
    </section>
  )
}

function SettingsSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h2 className="mb-2 px-4 text-xs font-semibold uppercase text-zinc-500">{title}</h2>
      <div className="overflow-hidden rounded-xl border border-zinc-200/70 bg-white shadow-sm">
        {children}
      </div>
    </section>
  )
}

function SettingsRow({
  children,
  label,
  stacked = false,
  value,
}: {
  children?: ReactNode
  label: string
  stacked?: boolean
  value?: string | undefined
}) {
  if (stacked) {
    return (
      <div className="flex min-h-12 flex-col items-stretch gap-3 border-b border-zinc-200 px-4 py-3 last:border-b-0">
        <div className="min-w-0 text-sm font-medium text-zinc-900">{label}</div>
        {children ? (
          <div className="min-w-0 w-full">{children}</div>
        ) : (
          <div className="min-w-0 truncate text-sm font-medium text-zinc-500">{value}</div>
        )}
      </div>
    )
  }

  return (
    <div className="flex min-h-12 items-center justify-between gap-4 border-b border-zinc-200 px-4 py-2 last:border-b-0">
      <div className="min-w-0 text-sm font-medium text-zinc-900">{label}</div>
      {children ? (
        <div className="shrink-0">{children}</div>
      ) : (
        <div className="min-w-0 truncate text-right text-sm font-medium text-zinc-500">{value}</div>
      )}
    </div>
  )
}

function SettingsSelect({
  ariaLabel,
  children,
  onChange,
  value,
}: {
  ariaLabel: string
  children: ReactNode
  onChange: (value: string) => void
  value: string
}) {
  return (
    <select
      aria-label={ariaLabel}
      className="h-9 max-w-[54vw] rounded-lg border border-zinc-200 bg-white px-3 text-right text-sm font-semibold text-zinc-900 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/25 sm:max-w-xs"
      value={value}
      onChange={(event) => {
        hapticSelection()
        onChange(event.currentTarget.value)
      }}
    >
      {children}
    </select>
  )
}

function FontPicker({
  onChange,
  value,
}: {
  onChange: (value: string) => void
  value: string
}) {
  return (
    <>
      <select
        aria-label="Terminal font"
        className="sr-only"
        value={value}
        onChange={(event) => {
          hapticSelection()
          onChange(event.currentTarget.value)
        }}
      >
        {TERMINAL_FONT_OPTIONS.map((option) => (
          <option key={option.value} value={option.value}>{option.label}</option>
        ))}
      </select>
      <div
        aria-label="Font previews"
        className="grid w-full min-w-0 gap-2"
        role="radiogroup"
        style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 13rem), 1fr))' }}
      >
        {TERMINAL_FONT_OPTIONS.map((option) => (
          <FontPreviewButton
            key={option.value}
            option={option}
            selected={option.value === value}
            onSelect={() => onChange(option.value)}
          />
        ))}
      </div>
    </>
  )
}

function FontPreviewButton({
  onSelect,
  option,
  selected,
}: {
  onSelect: () => void
  option: { label: string; value: string }
  selected: boolean
}) {
  return (
    <button
      aria-checked={selected}
      className={`min-w-0 rounded-lg border p-3 text-left transition active:scale-[0.99] ${
        selected
          ? 'border-blue-500 bg-blue-50 shadow-[0_0_0_1px_rgba(59,130,246,0.65)]'
          : 'border-zinc-200 bg-white hover:bg-zinc-50 active:bg-zinc-50'
      }`}
      role="radio"
      style={{ fontFamily: option.value }}
      type="button"
      onClick={() => {
        hapticSelection()
        onSelect()
      }}
    >
      <div className="flex min-w-0 items-center gap-2">
        <span className="truncate text-sm font-semibold leading-5 text-zinc-950">{option.label}</span>
        <span className={`ml-auto h-2 w-2 shrink-0 rounded-full ${selected ? 'bg-blue-500' : 'bg-zinc-200'}`} />
      </div>
      <div className="mt-2 rounded-md bg-zinc-950 px-2 py-2 text-[12px] leading-5 text-zinc-100">
        <div className="truncate">$ termx --font</div>
        <div className="truncate text-zinc-300">AaBb 012345 &lt;&gt; ~/</div>
      </div>
    </button>
  )
}

function ThemePicker({
  groups,
  onChange,
  value,
}: {
  groups: Record<'dark' | 'light', TerminalThemeOption[]>
  onChange: (value: TerminalSettings['themeId']) => void
  value: TerminalSettings['themeId']
}) {
  return (
    <>
      <select
        aria-label="Terminal theme"
        className="sr-only"
        value={value}
        onChange={(event) => {
          hapticSelection()
          onChange(event.currentTarget.value as TerminalSettings['themeId'])
        }}
      >
        <optgroup label="Dark">
          {groups.dark.map((option) => (
            <option key={option.id} value={option.id}>{option.label}</option>
          ))}
        </optgroup>
        <optgroup label="Light">
          {groups.light.map((option) => (
            <option key={option.id} value={option.id}>{option.label}</option>
          ))}
        </optgroup>
      </select>
      <div
        aria-label="Theme previews"
        className="grid w-full min-w-0 gap-2"
        role="radiogroup"
        style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 12rem), 1fr))' }}
      >
        {[...groups.dark, ...groups.light].map((option) => (
          <ThemePreviewButton
            key={option.id}
            option={option}
            selected={option.id === value}
            onSelect={() => onChange(option.id)}
          />
        ))}
      </div>
    </>
  )
}

function ThemePreviewButton({
  onSelect,
  option,
  selected,
}: {
  onSelect: () => void
  option: TerminalThemeOption
  selected: boolean
}) {
  const theme = option.theme
  const ui = resolveTerminalThemeUi(option.id)
  const colors = [theme.red, theme.green, theme.yellow, theme.blue, theme.magenta, theme.cyan]
    .filter((color): color is string => typeof color === 'string')

  return (
    <button
      aria-checked={selected}
      className="min-w-0 rounded-lg border p-2 text-left transition-transform active:scale-[0.99]"
      role="radio"
      style={{
        backgroundColor: ui.surface,
        borderColor: selected ? ui.accent : ui.borderSubtle,
        boxShadow: selected ? `0 0 0 1px ${ui.accent}` : 'none',
      }}
      type="button"
      onClick={() => {
        hapticSelection()
        onSelect()
      }}
    >
      <div className="rounded-md p-2" style={{ backgroundColor: ui.terminalBackground }}>
        <div className="mb-2 flex gap-1">
          {colors.map((color) => (
            <span key={color} className="h-2.5 flex-1 rounded-sm" style={{ backgroundColor: color }} />
          ))}
        </div>
        <div className="space-y-1">
          <div className="h-1.5 w-4/5 rounded-full" style={{ backgroundColor: ui.terminalForeground, opacity: 0.72 }} />
          <div className="flex items-center gap-1">
            <div className="h-1.5 w-1/2 rounded-full" style={{ backgroundColor: ui.terminalForeground, opacity: 0.42 }} />
            <div className="h-2.5 w-1 rounded-sm" style={{ backgroundColor: ui.terminalCursor }} />
          </div>
        </div>
      </div>
      <div className="mt-2 flex min-w-0 items-center gap-1.5">
        <span className="truncate text-xs font-semibold" style={{ color: ui.text }}>{option.label}</span>
        <span className="ml-auto h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: selected ? ui.accent : ui.borderSubtle }} />
      </div>
    </button>
  )
}

function Switch({
  ariaLabel,
  checked,
  onChange,
}: {
  ariaLabel: string
  checked: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <button
      aria-label={ariaLabel}
      aria-pressed={checked}
      className={`relative h-8 w-12 rounded-full transition-colors ${checked ? 'bg-[var(--termx-accent)]' : 'bg-[var(--termx-border)]'}`}
      type="button"
      onClick={() => {
        hapticSelection()
        onChange(!checked)
      }}
    >
      <span className={`absolute left-1 top-1 h-6 w-6 rounded-full bg-white shadow-sm transition-transform ${checked ? 'translate-x-4' : 'translate-x-0'}`} />
    </button>
  )
}

function PairSheet({
  cameraScanning,
  canScanWithCamera,
  lastImported,
  manualEntryOpen,
  manualScanValue,
  pairError,
  scanFlowState,
  pairIntent,
  pairing,
  selectedMachine,
  signedIn,
  onClose,
  onImport,
  onManualEntryOpen,
  onManualScanValueChange,
  onScanWithCamera,
}: {
  cameraScanning: boolean
  canScanWithCamera: boolean
  lastImported: PairingPayload | null
  manualEntryOpen: boolean
  manualScanValue: string
  pairError: string | null
  scanFlowState: ScanFlowState
  pairIntent: PairIntent
  pairing: boolean
  selectedMachine: WebControlMachine | null
  signedIn: boolean
  onClose: () => void
  onImport: () => void
  onManualEntryOpen: () => void
  onManualScanValueChange: (value: string) => void
  onScanWithCamera: () => void
}) {
  const title = pairIntent === 'add-local' ? 'Add Local Device' : 'Authorize Device'
  const primaryLabel = pairIntent === 'add-local' ? 'Add Device' : 'Pair Device'
  const showManualEntry = manualEntryOpen || !canScanWithCamera
  const statusMessage = scanFlowState === 'pairing'
    ? 'QR code scanned. Pairing this phone with the machine...'
    : scanFlowState === 'scanning'
      ? 'Camera is scanning for a TermX QR code...'
      : null
  return (
    <div className="fixed inset-0 z-50 bg-white text-zinc-950" role="dialog" aria-modal="true">
      <section className="flex h-full min-h-0 flex-col bg-white" data-testid="termx-pair-sheet">
        <header className="flex min-h-14 shrink-0 items-center justify-between gap-3 border-b border-zinc-200 px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
          <div className="flex min-w-0 items-center gap-2">
            <QrCode className="h-5 w-5 shrink-0 text-[var(--termx-accent)]" />
            <h2 className="truncate text-base font-semibold">{title}</h2>
          </div>
          <button
            aria-label="Close pairing"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md text-zinc-500 hover:bg-zinc-50 active:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--termx-accent)]"
            type="button"
            onClick={onClose}
          >
            <X className="h-5 w-5" />
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-5 pb-[calc(env(safe-area-inset-bottom)+1.5rem)]">
          <div className="mx-auto w-full max-w-md">
            {selectedMachine ? (
              <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2">
                <div className="truncate text-sm font-semibold text-zinc-950">{selectedMachine.name}</div>
                <div className="mt-0.5 truncate text-xs font-medium text-zinc-500">{selectedMachine.hostname || selectedMachine.id}</div>
              </div>
            ) : null}

            {canScanWithCamera && !showManualEntry ? (
              <button
                className="mt-4 inline-flex h-12 w-full items-center justify-center gap-2 rounded-md bg-[var(--termx-accent)] px-3 text-sm font-semibold text-[var(--termx-accent-text)] active:opacity-85 disabled:cursor-not-allowed disabled:opacity-60"
                type="button"
                onClick={onScanWithCamera}
                disabled={pairing || cameraScanning}
              >
                {cameraScanning || pairing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Camera className="h-4 w-4" />}
                {pairing ? 'Pairing device...' : cameraScanning ? 'Scanning QR...' : 'Scan QR with camera'}
              </button>
            ) : null}

            {!showManualEntry ? (
              <button
                className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-md border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-950 hover:bg-zinc-50 active:bg-zinc-50"
                type="button"
                onClick={onManualEntryOpen}
              >
                <Keyboard className="h-4 w-4" />
                Enter content manually
              </button>
            ) : (
              <div className="mt-4 rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2">
                <label className="block text-xs font-semibold text-zinc-500">
                  TermX QR content
                  <textarea
                    className="mt-1 h-44 w-full resize-none rounded-md border border-zinc-200 bg-white p-2 font-mono text-xs leading-5 text-zinc-950 placeholder:text-zinc-400 outline-none focus:border-[var(--termx-accent)] focus:ring-2 focus:ring-[var(--termx-accent)]/25"
                    value={manualScanValue}
                    onChange={(event) => onManualScanValueChange(event.target.value)}
                    placeholder="termx://pair?payload=..."
                    spellCheck={false}
                  />
                </label>
              </div>
            )}

            {showManualEntry ? (
              <button
                className="mt-3 inline-flex h-11 w-full items-center justify-center gap-2 rounded-md border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-950 hover:bg-zinc-50 active:bg-zinc-50 disabled:cursor-not-allowed disabled:opacity-50"
                type="button"
                onClick={onImport}
                disabled={pairing || cameraScanning || manualScanValue.trim() === ''}
              >
                {pairing ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
                {primaryLabel}
              </button>
            ) : null}

            {statusMessage ? (
              <p className="mt-3 rounded-md bg-blue-500/10 px-3 py-2 text-sm font-medium text-blue-700">{statusMessage}</p>
            ) : null}

            {pairError ? (
              <p className="mt-3 rounded-md bg-red-500/10 px-3 py-2 text-sm font-medium text-red-500">{pairError}</p>
            ) : null}

            {lastImported ? (
              <div className="mt-3 rounded-md bg-emerald-500/10 px-3 py-2 text-xs font-medium text-emerald-500">
                <div className="truncate font-semibold">{lastImported.machine.name}</div>
                <div className="truncate">{lastImported.machine.id}</div>
              </div>
            ) : null}
          </div>
        </div>
      </section>
    </div>
  )
}

function MachineRow({
  authorizationExpiresAt,
  authorizationState,
  machine,
  onForgetMachineAuthorization,
  onPairMachine,
  onSelectMachine,
}: {
  authorizationExpiresAt?: string | undefined
  authorizationState: MachineAuthorizationState
  machine: WebControlMachine
  onForgetMachineAuthorization: (machine: WebControlMachine) => void
  onPairMachine: (machine: WebControlMachine) => void
  onSelectMachine: (machine: WebControlMachine) => void
}) {
  const actionLabel = authorizationState === 'ready'
    ? 'Open'
    : 'Pair'
  const authPill = authorizationState === 'ready'
    ? 'Ready'
    : authorizationState === 'expired'
      ? 'Expired'
    : 'Scan QR'
  const subtitle = machine.hostname || shortenMachineId(machine.id)
  const availability = authorizationAvailabilityText(machine, authorizationState, authorizationExpiresAt)
  const sourcePill = machine.source === 'hub' ? 'Hub' : 'Local'
  const DeviceIcon = machine.source === 'hub' ? Server : LaptopMinimal
  const canForget = Boolean(authorizationExpiresAt || authorizationState === 'ready')
  return (
    <div className="overflow-hidden rounded-2xl border border-zinc-200 bg-white">
      <button
        aria-label={`${actionLabel} ${machine.name}`}
        className="grid min-w-0 w-full grid-cols-[auto_minmax(0,1fr)] gap-3 px-4 py-3.5 text-left hover:bg-zinc-50 active:bg-zinc-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-500"
        type="button"
        onClick={() => onSelectMachine(machine)}
      >
        <div className="relative flex h-11 w-11 items-center justify-center rounded-2xl bg-zinc-100 text-zinc-700">
          <DeviceIcon className="h-5 w-5" />
          <span className={`absolute bottom-0.5 right-0.5 h-2.5 w-2.5 rounded-full ring-2 ring-white ${
            machine.online ? 'bg-emerald-500' : 'bg-zinc-400'
          }`} />
        </div>
        <div className="min-w-0">
          <div className="flex min-w-0 items-center justify-between gap-2">
            <span className="truncate text-[15px] font-semibold leading-5 text-zinc-950">{machine.name}</span>
            <span className={`shrink-0 rounded-md px-2 py-0.5 text-[11px] font-semibold leading-4 ring-1 ${machine.online ? 'bg-emerald-50 text-emerald-700 ring-emerald-200' : 'bg-zinc-100 text-zinc-600 ring-zinc-200'}`}>
              {machine.online ? 'Online' : 'Offline'}
            </span>
          </div>
          <div className="mt-1 truncate text-xs font-medium text-zinc-500">{subtitle}</div>
          <div className="mt-2 flex items-center justify-between gap-3">
            <span className={`truncate text-[12px] font-medium ${machine.online && authorizationState === 'ready' ? 'text-zinc-900' : 'text-zinc-500'}`}>
              {availability}
            </span>
            <div className="flex shrink-0 flex-wrap gap-1.5">
              <InfoPill>{sourcePill}</InfoPill>
              <InfoPill>{authPill}</InfoPill>
            </div>
          </div>
        </div>
      </button>
      <div className="flex items-center gap-2 border-t border-zinc-100 px-4 py-2.5">
        {authorizationState !== 'ready' ? (
          <button
            aria-label={`Scan to pair ${machine.name}`}
            className="inline-flex h-9 items-center gap-2 rounded-full bg-zinc-100 px-3 text-[12px] font-semibold text-zinc-700 hover:bg-zinc-100 active:bg-zinc-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            type="button"
            onClick={() => onPairMachine(machine)}
          >
            <QrCode className="h-4 w-4" />
            Scan to pair
          </button>
        ) : null}
        {canForget ? (
          <button
            aria-label={`Remove authorization for ${machine.name}`}
            className="ml-auto inline-flex h-9 items-center gap-2 rounded-full border border-red-200 bg-white px-3 text-[12px] font-semibold text-red-600 hover:bg-red-50 active:bg-red-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500"
            type="button"
            onClick={() => onForgetMachineAuthorization(machine)}
          >
            <Trash2 className="h-4 w-4" />
            Remove
          </button>
        ) : null}
        {authorizationState === 'ready' ? <span className="min-h-9 flex-1" aria-hidden="true" /> : null}
      </div>
    </div>
  )
}

function authorizationAvailabilityText(
  machine: WebControlMachine,
  authorizationState: MachineAuthorizationState,
  expiresAt: string | undefined,
): string {
  if (authorizationState === 'ready') {
    return expiresAt ? `Authorized until ${formatAuthorizationExpiry(expiresAt)}` : machine.online ? 'Tap to connect' : 'Authorized'
  }
  if (authorizationState === 'expired') {
    return expiresAt ? `Authorization expired ${formatAuthorizationExpiry(expiresAt)}` : 'Authorization expired'
  }
  return 'Pair this phone to open it'
}

function formatAuthorizationExpiry(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  if (date.getFullYear() >= 2099) return value
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function machineAuthorizationState(
  machine: WebControlMachine,
  authorizedMachineIds: Set<string>,
  authorizationExpiries: Map<string, string>,
): MachineAuthorizationState {
  if (authorizedMachineIds.has(machine.id)) return 'ready'
  const expiresAt = authorizationExpiries.get(machine.id)
  if (expiresAt && isExpiredAuthorization(expiresAt)) return 'expired'
  return 'unauthorized'
}

function isExpiredAuthorization(value: string): boolean {
  const date = new Date(value)
  return !Number.isNaN(date.getTime()) && date.getTime() <= Date.now()
}

function EmptyState({
  actionLabel,
  icon,
  message,
  onAction,
  title,
}: {
  actionLabel: string
  icon: 'login' | 'scan'
  message: string
  onAction: () => void
  title: string
}) {
  return (
    <div className="flex flex-1 items-center justify-center px-4 py-8">
      <div className="flex w-full max-w-sm flex-col items-center gap-4 rounded-lg border border-dashed border-zinc-300 bg-white px-5 py-7 text-center" data-testid="termx-machine-empty-state">
        <div className="flex h-12 w-12 items-center justify-center rounded-md bg-zinc-100 text-zinc-500">
          {icon === 'login' ? <Server className="h-6 w-6" /> : <QrCode className="h-6 w-6" />}
        </div>
        <div className="space-y-1">
          <h2 className="text-base font-semibold text-zinc-950">{title}</h2>
          <p className="text-sm leading-5 text-zinc-500">{message}</p>
        </div>
        <button
          className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-zinc-900 px-3 text-sm font-semibold text-white hover:bg-zinc-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
          type="button"
          onClick={() => { hapticImpact(); onAction() }}
        >
          {icon === 'login' ? <LogIn className="h-4 w-4" /> : <QrCode className="h-4 w-4" />}
          {actionLabel}
        </button>
      </div>
    </div>
  )
}

function InfoPill({ children }: { children: string }) {
  return (
    <span className="inline-flex h-6 items-center rounded-md bg-zinc-100 px-2 text-[11px] font-semibold text-zinc-600">
      {children}
    </span>
  )
}

function initialControlUrl(fallback: string | undefined, networkRuntime: RemoteNetworkRuntime): string {
  const fromQuery = networkRuntime.queryParam('control')
  const queryValue = cleanControlUrl(fromQuery)
  if (queryValue) return queryValue
  return cleanControlUrl(fallback) || defaultWebControlUrl
}

function cleanControlUrl(value: string | null | undefined): string {
  return value?.trim() ?? ''
}

function createUnavailableWebControlApi(message: string): WebControlApi {
  const fail = async (): Promise<never> => {
    throw new Error(message)
  }
  return {
    login: fail,
    me: fail,
    listMachines: fail,
  }
}

function createBrowserAppDeviceId(): string {
  const cryptoImpl = globalThis.crypto
  if (cryptoImpl?.randomUUID) {
    return `appweb_${cryptoImpl.randomUUID()}`
  }
  const bytes = new Uint8Array(16)
  cryptoImpl?.getRandomValues?.(bytes)
  if (bytes.some((value) => value !== 0)) {
    return `appweb_${Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')}`
  }
  return `appweb_${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}`
}

function readAuthorizedMachineIds(storage: RemoteRuntimeStorage | undefined, userId: string | undefined): Set<string> {
  if (!storage) return new Set()
  try {
    const sessionStore = createMachineSessionStore(storage)
    return new Set(createMachineStore({ storage }).listMachines()
      .filter((machine) => sessionStore.getSessionToken(machine.machineId))
      .map((machine) => machine.machineId))
  } catch {
    return new Set()
  }
}

function readAuthorizationExpiries(storage: RemoteRuntimeStorage | undefined): Map<string, string> {
  if (!storage) return new Map()
  try {
    const sessionStore = createMachineSessionStore(storage)
    const expiries = new Map<string, string>()
    for (const machine of createMachineStore({ storage }).listMachines()) {
      const expiresAt = sessionStore.getSessionExpiry(machine.machineId)
      if (expiresAt) expiries.set(machine.machineId, expiresAt)
    }
    return expiries
  } catch {
    return new Map()
  }
}

function pruneMachinesForSignOut(storage: RemoteRuntimeStorage | undefined): StoredMachineRecord[] {
  if (!storage) return []
  const store = createMachineStore({ storage })
  const sessionStore = createMachineSessionStore(storage)
  for (const machine of store.listMachines()) {
    if (machine.source !== 'hub') continue
    if (hasLocalAddresses(machine)) {
      store.saveMachine(downgradeHubMachineToLocal(machine))
    } else {
      store.forgetMachine(machine.machineId)
      sessionStore.clearSessionToken(machine.machineId)
    }
  }
  return store.listMachines()
}

function syncSignedInHubMachines(storage: RemoteRuntimeStorage, hubMachines: WebControlMachine[]): void {
  const store = createMachineStore({ storage })
  for (const hub of hubMachines) {
    const stored = store.getMachine(hub.id)
    if (stored) {
      store.saveMachine(mergeHubMachine(stored, hub))
      continue
    }
    const timestamp = new Date().toISOString()
    store.saveMachine(mergeHubMachine({
      machineId: hub.id,
      name: hub.name,
      ...(hub.hostname ? { hostname: hub.hostname } : {}),
      state: hub.online ? 'online' : 'offline',
      terminalCount: 0,
      source: 'hub',
      addresses: { local: [], lan: [], public: [] },
      endpoints: {},
      addedAt: timestamp,
      updatedAt: timestamp,
    }, hub))
  }
}

function hasLocalAddresses(machine: StoredMachineRecord): boolean {
  return machine.addresses.local.length > 0 || machine.addresses.lan.length > 0 || machine.addresses.public.length > 0
}

function downgradeHubMachineToLocal(machine: StoredMachineRecord): StoredMachineRecord {
  return {
    ...machine,
    state: machine.state === 'online' ? 'unknown' : machine.state,
    source: 'local',
    preferredPath: 'local',
    addresses: {
      local: machine.addresses.local,
      lan: machine.addresses.lan,
      public: machine.addresses.public,
    },
    endpoints: {},
    updatedAt: new Date().toISOString(),
  }
}

function createPairApiForScan(input: {
  candidateHubUrls: string[]
  machine: WebControlMachine
  networkRuntime: RemoteNetworkRuntime
}): PairApi {
  const contenders: Array<(pairInput: PairInput, options?: RtcConnectOptions) => Promise<PairResult>> = []
  for (const hubUrl of compactHubUrls(input.candidateHubUrls)) {
    contenders.push((pairInput, options) => createHubApi({ baseUrl: hubUrl, fetch: input.networkRuntime.fetch }).pair(pairInputWithMachineId(pairInput, input.machine.id), options))
  }
  return {
    pair(pairInput, options) {
      return racePairClaims(contenders.map((claim) => (claimOptions?: RtcConnectOptions) => claim(pairInput, claimOptions)), options)
    },
  }
}

function candidatePairingHubUrls(machine: WebControlMachine, localHubUrls: string[]): string[] {
  return compactHubUrls([
    ...localHubUrls,
    ...nonEmptyHubUrls(machine),
    ...(machine.localFallbackHubUrls ?? []),
  ])
}

function pairInputWithMachineId(input: PairInput, fallbackMachineId: string | undefined): PairInput & { machineId: string } {
  const machineId = input.machineId ?? fallbackMachineId
  if (!machineId) throw new Error('machine id is required before pairing this device')
  return {
    ...input,
    machineId,
  }
}

async function racePairClaims(
  claims: Array<(options?: RtcConnectOptions) => Promise<PairResult>>,
  options: RtcConnectOptions = {},
): Promise<PairResult> {
  if (claims.length === 0) throw new Error('Pairing endpoint is required before pairing this device')
  if (claims.length === 1) return claims[0]!(options)
  return new Promise((resolve, reject) => {
    let settled = false
    let remaining = claims.length
    let lastError: unknown
    const controllers = claims.map(() => new AbortController())
    const abortLosers = (reason: Error) => {
      for (const controller of controllers) {
        if (!controller.signal.aborted) controller.abort(reason)
      }
      options.signal?.removeEventListener('abort', abortFromParent)
    }
    const abortFromParent = () => {
      const reason = options.signal?.reason instanceof Error ? options.signal.reason : new Error('pairing request cancelled')
      if (!settled) {
        settled = true
        abortLosers(reason)
        reject(reason)
      }
    }
    if (options.signal?.aborted) {
      abortFromParent()
      return
    }
    options.signal?.addEventListener('abort', abortFromParent, { once: true })
    for (const claim of claims) {
      const index = claims.indexOf(claim)
      const controller = controllers[index]!
      claim({ ...options, signal: controller.signal }).then(
        (result) => {
          if (settled) return
          settled = true
          abortLosers(new Error('pairing endpoint race finished'))
          resolve(result)
        },
        (error) => {
          if (settled) return
          lastError = error
          remaining -= 1
          if (remaining === 0 && !settled) {
            settled = true
            options.signal?.removeEventListener('abort', abortFromParent)
            reject(errorFromUnknown(lastError, 'Pairing failed on every endpoint'))
          }
        },
      )
    }
  })
}

function errorFromUnknown(error: unknown, fallback: string): Error {
  if (error instanceof Error) return error
  if (error === undefined || error === null) return new Error(fallback)
  return new Error(String(error))
}

function runWithTimeout<T>(
  run: (signal: AbortSignal) => Promise<T>,
  timeoutMs: number,
  timeoutMessage: string,
): Promise<T> {
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  const timeout = new Promise<T>((_, reject) => {
    timer = setTimeout(() => {
      const error = new Error(timeoutMessage)
      controller.abort(error)
      reject(error)
    }, timeoutMs)
  })
  return Promise.race([run(controller.signal), timeout]).finally(() => {
    if (timer) clearTimeout(timer)
    if (!controller.signal.aborted) controller.abort(new Error('pairing request finished'))
  })
}

function createHubMachineRuntime(input: {
  machine: WebControlMachine
  storage: RemoteRuntimeStorage
  api: WebControlApi
  networkRuntime: RemoteNetworkRuntime
  networkStateManager: RemoteNetworkStateManager
  createSession?: HubRtcSessionFactory | undefined
}): MachineRuntime {
  const sessionStore = createMachineSessionStore(input.storage)
  const [summaryHubUrl] = nonEmptyHubUrls(input.machine)
  const machineSession = createHubMachineSessionManager({
    machine: input.machine,
    sessionStore,
    networkRuntime: input.networkRuntime,
    networkStateManager: input.networkStateManager,
    createSession: requiredHubRtcSessionFactory(input.createSession),
  })
  const machineStatus: LocalStatus = {
    machine: {
      machineId: input.machine.id,
      name: input.machine.name,
      state: input.machine.online ? 'online' : 'offline',
      ...(input.machine.lastSeen ? { lastSeenAt: input.machine.lastSeen } : {}),
    },
    localWeb: {
      httpUrl: input.machine.controlUrl ?? '',
      rtcOfferUrl: summaryHubUrl ?? '',
    },
  }
  return {
    api: {
      async getStatus() {
        return machineStatus
      },
      async listTerminals(options) {
        const session = await machineSession.get({
          forceRelay: options?.forceRelay,
          onStatus: options?.onStatus,
          onConnectionState: options?.onConnectionState,
        })
        const channel = await session.openApi()
        const response = await channel.request<{ terminals: Record<string, unknown>[] }>('list', {})
        return normalizeTerminalsForMachine(input.machine.id, response.terminals ?? [])
      },
    },
    connector: {
      async connect(target, options) {
        if (target.machineId !== input.machine.id) {
          throw new Error(`machine runtime mismatch: ${target.machineId} != ${input.machine.id}`)
        }
        return machineSession.get(options)
      },
      reconnect(options?: { forceRelay?: boolean | undefined }) {
        machineSession.reconnect(options)
      },
    },
    connectionStateEvents: {
      subscribe(machineId, handler) {
        if (machineId !== input.machine.id) return { close() {} }
        return machineSession.subscribeConnectionState(handler)
      },
    },
    inventoryEvents: {
      subscribe(machineId, handler) {
        if (machineId !== input.machine.id) {
          return { close() {} }
        }
        return machineSession.subscribeInventoryEvents(handler)
      },
    },
    dispose: machineSession.reset,
  }
}

function createHubMachineSessionManager(input: {
  machine: WebControlMachine
  sessionStore: MachineSessionStore
  networkRuntime: RemoteNetworkRuntime
  networkStateManager: RemoteNetworkStateManager
  createSession: HubRtcSessionFactory
}) {
  const connectionState = createConnectionStatePublisher()
  const publishAttempt = (snapshot: ConnectionAttemptSnapshot) => {
    connectionState.publish(connectionStateFromAttempt({
      machineId: input.machine.id,
      stage: snapshot.stage,
      message: snapshot.message,
      ...(snapshot.path ? { path: snapshot.path } : {}),
      relayInUse: snapshot.relayInUse,
    }))
  }
  const connectionStore = new MachineConnectionStore({
    machineId: input.machine.id,
    networkStateManager: input.networkStateManager,
    createLease: createHubMachineSessionLease,
    connect: (options = {}) => connect(options),
  })
  const subscribeInventoryEvents = (handler: (event: { type: 'inventory_changed'; payload?: unknown }) => void): RtcSubscription => {
    return connectionStore.subscribeSessionEvents((event) => {
      if (isTerminalInventoryRuntimeEvent(event)) handler({ type: 'inventory_changed', payload: event.payload })
    })
  }
  const connect = async (options?: RtcConnectOptions & { onSnapshot?: (snapshot: ConnectionAttemptSnapshot) => void }): Promise<RtcSession> => {
    if (options?.signal?.aborted) {
      throw options.signal.reason instanceof Error ? options.signal.reason : new Error('connection aborted')
    }
    const endpoints = endpointsFromMachine(input.machine)
    if (endpoints.length === 0) {
      throw new Error('Hub endpoint is required before opening this machine runtime')
    }
    const sessionToken = input.sessionStore.getSessionToken(input.machine.id)
    if (!sessionToken) {
      throw new Error('Pair this machine before opening the runtime channel')
    }
    const answerProofSecret = input.sessionStore.getAnswerProofSecret(input.machine.id) ?? undefined
    const orchestrator = createConnectionOrchestrator({
      hubApiFactory: (hubUrl) => createHubApi({ baseUrl: hubUrl, fetch: input.networkRuntime.fetch }),
      hubRtcConnectorFactory: ({ hubUrl, api }) => createHubRtcConnector({
        api,
        createSession: input.createSession,
        hubUrl,
        logger: consoleConnectionLogger,
      }),
      logger: consoleConnectionLogger,
    })
    const result = await orchestrator.connect({
      machineId: input.machine.id,
      sessionToken,
      answerProofSecret,
      policy: 'app_fastest',
      endpoints,
      onSnapshot: (snapshot) => {
        publishAttempt(snapshot)
        options?.onSnapshot?.(snapshot)
      },
    }, options)
    return result.session
  }
  return {
    get: (options?: RtcConnectOptions) => connectionStore.get(options),
    reconnect: (options?: { forceRelay?: boolean | undefined }) => connectionStore.reconnect(options),
    subscribeInventoryEvents,
    subscribeConnectionState: (handler: (snapshot: RtcConnectionStateSnapshot) => void) => {
      const storeSubscription = connectionStore.subscribeConnectionState(handler)
      const attemptSubscription = connectionState.subscribe(handler)
      return {
        close() {
          storeSubscription.close()
          attemptSubscription.close()
        },
      }
    },
    reset: () => connectionStore.release(),
  }
}

const unavailableNetworkRuntime: RemoteNetworkRuntime = {
  fetch() {
    throw new Error('remote network runtime is required')
  },
  queryParam() {
    return null
  },
}

function requiredHubRtcSessionFactory(factory: HubRtcSessionFactory | undefined): HubRtcSessionFactory {
  if (!factory) {
    throw new Error('hub RTC session factory is required')
  }
  return factory
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

function createHubMachineSessionLease(session: RtcSession): RtcSession & RtcTerminalDataChannelController & RtcSessionLiveness {
  const openedTerminals = new Map<string, RtcBinaryChannel>()
  const openedFiles = new Map<string, RtcBinaryChannel>()
  const subscriptions = new Set<RtcSubscription>()
  let closed = false
  return {
    async openTerminal(id: string) {
      if (closed) throw new Error('machine session lease is closed')
      const channel = await session.openTerminal(id)
      openedTerminals.set(id, channel)
      return channel
    },
    closeTerminalDataChannel(id: string) {
      const channel = openedTerminals.get(id)
      openedTerminals.delete(id)
      channel?.close()
      const controller = session as RtcSession & Partial<RtcTerminalDataChannelController>
      if (typeof controller.closeTerminalDataChannel === 'function') {
        controller.closeTerminalDataChannel(id)
      }
    },
    async openApi() {
      if (closed) throw new Error('machine session lease is closed')
      return createSharedApiLeaseChannel(await session.openApi())
    },
    async openFileTransfer(transferId: string) {
      if (closed) throw new Error('machine session lease is closed')
      const channel = await session.openFileTransfer(transferId)
      openedFiles.set(transferId, channel)
      return channel
    },
    subscribeEvents(handler: (event: RtcEvent) => void) {
      if (closed) return { close() {} }
      const subscription = session.subscribeEvents(handler)
      subscriptions.add(subscription)
      return {
        close() {
          subscriptions.delete(subscription)
          subscription.close()
        },
      }
    },
    async getConnectionInfo(): Promise<ConnectionInfo> {
      return session.getConnectionInfo()
    },
    getCapabilities() {
      return session.getCapabilities()
    },
    isAlive() {
      if (closed) return false
      const candidate = session as RtcSession & Partial<RtcSessionLiveness>
      if (typeof candidate.isAlive !== 'function') return true
      return candidate.isAlive()
    },
    async disconnect() {
      closed = true
      for (const subscription of Array.from(subscriptions)) {
        subscription.close()
      }
      subscriptions.clear()
      for (const channel of Array.from(openedTerminals.values())) {
        channel.close()
      }
      openedTerminals.clear()
      for (const channel of Array.from(openedFiles.values())) {
        channel.close()
      }
      openedFiles.clear()
    },
  }
}

function createSharedApiLeaseChannel(channel: RtcJsonRpcChannel): RtcJsonRpcChannel {
  return {
    request<TResponse>(method: string, params?: unknown) {
      return channel.request<TResponse>(method, params)
    },
    close() {},
  }
}

function normalizeTerminalsForMachine(machineId: string, terminals: Record<string, unknown>[]) {
  return normalizeTerminalInventory({
    machine_id: machineId,
    terminals: terminals.map((terminal) => ({
      ...terminal,
      machine_id: typeof terminal.machine_id === 'string' || typeof terminal.machineId === 'string'
        ? terminal.machine_id ?? terminal.machineId
        : machineId,
    })),
  }).terminals
}

function mergeHubMachine(saved: StoredMachineRecord, machine: WebControlMachine): StoredMachineRecord {
  const [summaryHubUrl] = nonEmptyHubUrls(machine)
  return {
    machineId: saved.machineId,
    name: machine.name || saved.name,
    ...(machine.hostname || saved.hostname ? { hostname: machine.hostname ?? saved.hostname } : {}),
    state: machine.online ? 'online' : 'offline',
    terminalCount: saved.terminalCount,
    ...(machine.lastSeen || saved.lastSeenAt ? { lastSeenAt: machine.lastSeen ?? saved.lastSeenAt } : {}),
    ...(saved.lastConnectionPath ? { lastConnectionPath: saved.lastConnectionPath } : {}),
    preferredPath: 'hub',
    ...(saved.relayInUse !== undefined ? { relayInUse: saved.relayInUse } : {}),
    source: 'hub',
    addresses: saved.addresses,
    endpoints: {
      ...saved.endpoints,
      ...(machine.controlUrl ? { webControl: machine.controlUrl } : {}),
      ...(summaryHubUrl ? { hub: summaryHubUrl } : {}),
    },
    ...(saved.pairing ? { pairing: saved.pairing } : {}),
    addedAt: saved.addedAt,
    updatedAt: saved.updatedAt,
  }
}

function hubUrlsFromStoredMachine(machine: StoredMachineRecord): string[] {
  return compactHubUrls([machine.endpoints.hub])
}

function localHubUrlsFromStoredMachine(machine: StoredMachineRecord): string[] {
  return compactHubUrls([...machine.addresses.local, ...machine.addresses.lan])
}

function localFallbackHubUrlsFromStoredMachine(
  machine: StoredMachineRecord,
  hubUrls: readonly (string | undefined)[] = [],
): string[] {
  const publicHubUrls = compactHubUrls(machine.addresses.public)
  if (machine.source !== 'hub') return publicHubUrls
  const hub = new Set(compactHubUrls([machine.endpoints.hub, ...hubUrls]))
  return publicHubUrls.filter((hubUrl) => !hub.has(hubUrl))
}

function localHubUrlsFromPairingPayload(payload: PairingPayload): string[] {
  return compactHubUrls(payload.local.hubUrls)
}

function nonEmptyHubUrls(machine: WebControlMachine): string[] {
  return compactHubUrls(machine.hubUrls)
}

function endpointsFromMachine(machine: WebControlMachine): HubEndpoint[] {
  return compactHubEndpoints([
    ...compactHubUrls(machine.localHubUrls ?? []).map((url) => ({
      url,
      kind: 'local' as const,
      scope: localHubScope(url),
      source: 'stored_machine' as const,
    })),
    ...compactHubUrls(machine.localFallbackHubUrls ?? []).map((url) => ({
      url,
      kind: 'local' as const,
      scope: 'public_mapping' as const,
      source: 'stored_machine' as const,
    })),
    ...(machine.source === 'local'
      ? []
      : nonEmptyHubUrls(machine).map((url) => ({
        url,
        kind: 'hub' as const,
        scope: 'hub' as const,
        source: 'web_control' as const,
      }))),
  ])
}

function compactHubEndpoints(endpoints: readonly HubEndpoint[]): HubEndpoint[] {
  const out: HubEndpoint[] = []
  const seen = new Set<string>()
  for (const endpoint of endpoints) {
    const url = normalizeHubBaseUrlCandidate(endpoint.url)
    if (!url) continue
    const key = `${endpoint.kind}:${url}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push({ ...endpoint, url })
  }
  return out
}

function localHubScope(url: string): HubEndpoint['scope'] {
  try {
    const host = new URL(url).hostname
    return host === 'localhost' || host === '127.0.0.1' || host === '::1' ? 'loopback' : 'lan'
  } catch {
    return 'lan'
  }
}

function compactHubUrls(values: readonly (string | undefined)[]): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const raw of values) {
    const value = normalizeHubBaseUrlCandidate(raw)
    if (!value || seen.has(value)) continue
    seen.add(value)
    out.push(value)
  }
  return out
}

function formatLastSeen(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const diffMs = Date.now() - date.getTime()
  if (diffMs < 60_000) return 'just now'
  const diffMinutes = Math.floor(diffMs / 60_000)
  if (diffMinutes < 60) return `${diffMinutes} min ago`
  const diffHours = Math.floor(diffMinutes / 60)
  if (diffHours < 24) return `${diffHours} hr ago`
  const diffDays = Math.floor(diffHours / 24)
  if (diffDays < 7) return `${diffDays} day${diffDays === 1 ? '' : 's'} ago`
  return date.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
  })
}

function shortenMachineId(value: string): string {
  if (value.length <= 18) return value
  return `${value.slice(0, 8)}...${value.slice(-6)}`
}
