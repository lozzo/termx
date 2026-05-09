import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties, type ChangeEvent, type ReactNode } from 'react'
import { ArrowLeft, Camera, Download, Keyboard, Loader2, LogIn, Monitor, Plus, QrCode, RefreshCw, Server, Settings, ShieldCheck, Wifi, WifiOff, X } from 'lucide-react'
import { createMachineSessionStore, type MachineSessionStore } from './localAppIdentity'
import { LocalRemoteApp, type LocalRemoteInventoryApi, type LocalRemoteSessionConnector } from './LocalRemoteApp'
import { createMachineStore, type StoredMachineRecord } from './machineStore'
import { createConnectionOrchestrator, type ConnectionAttemptSnapshot } from './connectionOrchestrator'
import { connectionStateFromAttempt, createConnectionStatePublisher } from './connectionState'
import { createManagedHubRtcConnector } from './managedHubRtcConnector'
import { consoleConnectionLogger } from './connectionLogger'
import { createManagedHubApi } from './managedHubApi'
import { MachineConnectionStore } from './machineConnectionStore'
import { RemoteNetworkStateManager } from './remoteNetworkState'
import { FileTransferPanel } from './FileTransferPanel'
import { haptic } from './haptics'
import { addNativeBackHandler } from './nativeBack'
import { parsePairingPayload, type PairingPayload } from './pairingPayload'
import type { FileTransferContext, TransferInfo } from './fileApi'
import type { ConnectionInfo, LocalPairingApi, LocalStatus, MachineConnectionStateEvents, RemoteNetworkRuntime, RemoteRuntimeStorage, RtcBinaryChannel, RtcConnectOptions, RtcConnectionStateSnapshot, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSessionConnectionStateEvents, RtcSessionLiveness, RtcSessionNegotiator, RtcSubscription, RtcTerminalDataChannelController, TerminalInventoryEvents } from './transport'
import { normalizeTerminalInventory } from './terminalInventory'
import { createWebControlApi, type WebControlApi, type WebControlMachine, type WebControlUser } from './webControlApi'
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
} from './terminalSettings'
import type { TerminalRenderer } from './Terminal'

const storageKeys = {
  accessToken: 'termx.remote.accessToken',
} as const

const defaultWebControlUrl = ''
const appName = 'TermX Remote App'

function noopSubscribe(_listener: () => void): () => void { return () => {} }

type AppView = 'home' | 'settings' | 'machine'
type PairIntent = 'add-local' | 'authorize-machine'
type PairApi = LocalPairingApi
type PairMethod = 'local-hub' | 'web-control'
type ManagedRtcSessionFactory = (input: { machineId: string }) => RtcSession & RtcSessionNegotiator
type MachineAuthorizationState = 'ready' | 'needs-session' | 'unpaired'
type MachineRuntimeFactory = (input: {
  machine: WebControlMachine
  storage: RemoteRuntimeStorage
  api: WebControlApi
  networkRuntime: RemoteNetworkRuntime
  networkStateManager: RemoteNetworkStateManager
  createSession?: ManagedRtcSessionFactory | undefined
}) => MachineRuntime

interface MachineRuntime {
  api: LocalRemoteInventoryApi
  connector: LocalRemoteSessionConnector
  inventoryEvents?: TerminalInventoryEvents | undefined
  connectionStateEvents?: MachineConnectionStateEvents | undefined
  fileTransfer?: FileTransferContext | undefined
  dispose?(): void | Promise<void>
}

export interface WebControlRemoteAppProps {
  defaultControlUrl?: string | undefined
  storage?: RemoteRuntimeStorage | undefined
  networkRuntime?: RemoteNetworkRuntime | undefined
  managedRtcSessionFactory?: ManagedRtcSessionFactory | undefined
  pairApiFactory?: ((payload: PairingPayload, machine: WebControlMachine) => PairApi) | undefined
  machineRuntimeFactory?: MachineRuntimeFactory | undefined
  globalFileTransfer?: FileTransferContext | undefined
  scanPairingCode?: (() => Promise<string | null>) | undefined
}

export function WebControlRemoteApp({
  defaultControlUrl,
  storage: storageProp,
  networkRuntime: networkRuntimeProp,
  managedRtcSessionFactory,
  pairApiFactory,
  machineRuntimeFactory = createManagedMachineRuntime,
  globalFileTransfer,
  scanPairingCode,
}: WebControlRemoteAppProps) {
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
  const [localMachines, setLocalMachines] = useState<StoredMachineRecord[]>([])
  const [machines, setMachines] = useState<WebControlMachine[]>([])
  const [selectedMachineId, setSelectedMachineId] = useState<string | null>(null)
  const [scanOpen, setScanOpen] = useState(false)
  const [pairIntent, setPairIntent] = useState<PairIntent>('add-local')
  const [transferCenterOpen, setTransferCenterOpen] = useState(false)
  const [manualScanValue, setManualScanValue] = useState('')
  const [lastImported, setLastImported] = useState<PairingPayload | null>(null)
  const [pairedMachineIds, setPairedMachineIds] = useState(() => readPairedMachineIds(storage, undefined))
  const [pairVersion, setPairVersion] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [pairing, setPairing] = useState(false)
  const [cameraScanning, setCameraScanning] = useState(false)
  const signedIn = accessToken.trim() !== ''
  const appThemeStyle = useMemo(() => terminalThemeCssVariables(terminalSettings.themeId) as CSSProperties, [terminalSettings.themeId])
  const runtimeCacheRef = useRef<{
    api: WebControlApi
    createSession: ManagedRtcSessionFactory | undefined
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
      cache.createSession === managedRtcSessionFactory &&
      cache.networkRuntime === networkRuntime &&
      cache.runtimeFactory === machineRuntimeFactory &&
      cache.storage === storage
    if (!cacheMatches) {
      if (cache) {
        for (const runtime of cache.runtimes.values()) void runtime.dispose?.()
      }
      runtimeCacheRef.current = {
        api,
        createSession: managedRtcSessionFactory,
        networkRuntime,
        runtimeFactory: machineRuntimeFactory,
        storage,
        runtimes: new Map(),
      }
    }
  }

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
      createSession: managedRtcSessionFactory,
    })
    cache.set(machine.id, created)
    return created
  }, [api, machineRuntimeFactory, managedRtcSessionFactory, networkRuntime, networkStateManager, storage])

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
    for (const local of localMachines) {
      map.set(local.machineId, {
        id: local.machineId,
        name: local.name,
        hostname: local.hostname,
        online: local.state === 'online',
        paired: true,
        source: local.source === 'cloud' ? 'cloud' : 'local',
        hubUrls: hubUrlsFromStoredMachine(local),
      })
    }
    for (const cloud of machines) {
      map.set(cloud.id, cloud)
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
      const [profile, cloudMachines] = await Promise.all([
        api.me(),
        api.listMachines(),
      ])
      setUser(profile)
      setMachines(cloudMachines)
      if (storage) {
        mergeSignedInCloudMachines(storage, cloudMachines)
        localMachineList = createMachineStore({ storage }).listMachines()
      }
      setLocalMachines(localMachineList)
      setSelectedMachineId((current) => {
        if (
          current &&
          (cloudMachines.some((machine) => machine.id === current) ||
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
    setPairedMachineIds(readPairedMachineIds(storage, user?.id))
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
    setPairedMachineIds(readPairedMachineIds(storage, undefined))
    setPairVersion((current) => current + 1)
    setSelectedMachineId(null)
    setError(null)
    setView('settings')
  }, [storage])

  const updateTerminalSettings = useCallback((patch: Partial<TerminalSettings>) => {
    haptic()
    setTerminalSettings((current) => writeTerminalSettings({ ...current, ...patch }, storage))
  }, [storage])

  const openAddLocalSheet = useCallback(() => {
    haptic()
    setSelectedMachineId(null)
    setPairIntent('add-local')
    setManualScanValue('')
    setLastImported(null)
    setError(null)
    setScanOpen(true)
  }, [])

  const openPairSheet = useCallback((machineId: string) => {
    haptic()
    setSelectedMachineId(machineId)
    setPairIntent('authorize-machine')
    setManualScanValue('')
    setLastImported(null)
    setError(null)
    setScanOpen(true)
  }, [])

  const openMachinePairSheet = useCallback((machine: WebControlMachine) => {
    openPairSheet(machine.id)
    if (machine.paired && !pairedMachineIds.has(machine.id)) {
      setError('This machine is already paired with your account, but this phone needs a fresh local session. Scan the machine QR again to re-authorize this phone.')
    }
  }, [openPairSheet, pairedMachineIds])

  const selectMachine = useCallback((machine: WebControlMachine) => {
    haptic()
    setSelectedMachineId(machine.id)
    if (!pairedMachineIds.has(machine.id)) {
      openMachinePairSheet(machine)
      return
    }
    setView('machine')
    setError(null)
  }, [openMachinePairSheet, pairedMachineIds])

  const pairScannedValue = useCallback(async (rawValue: string) => {
    if (!storage) {
      setError('Local storage is required before importing a TermX QR')
      return
    }
    setPairing(true)
    setError(null)
    try {
      const payload = parsePairingPayload(rawValue)
      const cloudMachine = machines.find((machine) => machine.id === payload.machine.id)
      const isCloud = pairIntent === 'authorize-machine' && cloudMachine !== undefined
      const requiresWebControl = payload.endpoints.webControl !== undefined
      if (pairIntent === 'add-local' && requiresWebControl) {
        throw new Error('This QR belongs to an online Web Control device. Sign in and re-authorize it from your machine list.')
      }

      if (requiresWebControl && !signedIn) {
        setScanOpen(false)
        setView('settings')
        setError('The scanned agent is online. Please sign in to your account first to fetch this agent.')
        return
      }

      let targetMachine: WebControlMachine

      if (pairIntent === 'authorize-machine' && !selectedMachine) {
        throw new Error('Choose a machine before re-authorizing it')
      }

      if (isCloud) {
        if (!user) throw new Error('Account profile is required before pairing a cloud device')
        targetMachine = cloudMachine
      } else if (requiresWebControl) {
        if (!user) throw new Error('Account profile is required before pairing a cloud device')
        throw new Error('This pairing code does not match a Web Control device in this account')
      } else {
        targetMachine = {
          id: payload.machine.id,
          name: payload.machine.name,
          hostname: payload.machine.hostname,
          online: true,
          paired: false,
          source: 'local',
          hubUrls: hubUrlsFromPairingPayload(payload),
        }
      }

      if (selectedMachine && selectedMachine.id !== targetMachine.id) {
        throw new Error(`This pairing code belongs to ${targetMachine.name}, not ${selectedMachine.name}`)
      }
      const pairInput = {
        machineId: targetMachine.id,
        pairSessionId: payload.pairing.sessionId,
        pairSecret: payload.pairing.secret,
        appDeviceId: createBrowserAppDeviceId(),
        appName,
        requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
      }
      const pairMethod: PairMethod = isCloud ? 'web-control' : 'local-hub'
      const pairResult = pairApiFactory
        ? await pairApiFactory(payload, targetMachine).pair(pairInput)
        : pairMethod === 'web-control'
          ? await api.pairMachine(pairInput)
          : await createPairApiFromMachine(targetMachine, networkRuntime).pair(pairInput)
      if (pairResult.machineId !== targetMachine.id) {
        throw new Error(`pairing response machine mismatch: ${pairResult.machineId} != ${targetMachine.id}`)
      }
      createMachineSessionStore(storage).saveSessionToken(pairResult.machineId, pairResult.sessionToken, pairResult.expiresAt, payload.pairing.answerProofSecret)
      const store = createMachineStore({ storage })
      const saved = store.saveFromPairingPayload(payload)
      if (isCloud) {
        store.saveMachine(mergeCloudMachine(saved, targetMachine))
      }
      setSelectedMachineId(targetMachine.id)
      setPairedMachineIds(readPairedMachineIds(storage, user?.id))
      setPairVersion((current) => current + 1)
      setLastImported(payload)
      setError(null)
      setManualScanValue('')
      setScanOpen(false)
      setView('machine')
      haptic(25)
    } catch (err) {
      haptic([12, 30, 12])
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setPairing(false)
    }
  }, [machines, networkRuntime, pairApiFactory, pairIntent, selectedMachine, signedIn, storage, user])

  const importManualScan = useCallback(async () => {
    haptic()
    await pairScannedValue(manualScanValue)
  }, [manualScanValue, pairScannedValue])

  const scanWithCamera = useCallback(async () => {
    if (!scanPairingCode) return
    haptic()
    setCameraScanning(true)
    setError(null)
    try {
      const value = await scanPairingCode()
      if (!value) return
      setManualScanValue(value)
      await pairScannedValue(value)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setCameraScanning(false)
    }
  }, [pairScannedValue, scanPairingCode])

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
      className="flex h-full min-h-[100dvh] flex-col bg-[var(--termx-bg)] text-[var(--termx-text)]"
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
          onBack={() => { haptic(); setView('home') }}
          onLoginChange={setLogin}
          onPasswordChange={setPassword}
          onRefresh={() => { haptic(); void refreshMachines() }}
          onSignIn={() => { haptic(); void submitLogin() }}
          onSignOut={signOut}
          onTerminalSettingsChange={updateTerminalSettings}
        />
      ) : view === 'machine' && selectedMachine ? (
        <MachineTerminalListView
          machine={selectedMachine}
          storage={storage}
          terminalSettings={terminalSettings}
          runtime={getMachineRuntime(selectedMachine)}
          onBack={() => {
            haptic()
            setView('home')
            setError(null)
          }}
          onTerminalSettingsChange={updateTerminalSettings}
        />
      ) : (
        <HomeView
          fileTransfer={globalFileTransfer}
          transferState={globalTransferState as { transfers: TransferInfo[]; hasActiveTransfers: boolean }}
          loading={loading}
          machines={displayMachines}
          pairedMachineIds={pairedMachineIds}
          signedIn={signedIn}
          user={user}
          onAddLocalDevice={openAddLocalSheet}
          onOpenSettings={() => { haptic(); setView('settings') }}
          onOpenTransferCenter={() => { haptic(); setTransferCenterOpen(true) }}
          onPairMachine={openMachinePairSheet}
          onRefresh={() => { haptic(); void refreshMachines() }}
          onSelectMachine={selectMachine}
          onSignIn={() => { haptic(); setView('settings') }}
        />
      )}

      {scanOpen ? (
        <PairSheet
          lastImported={lastImported}
          manualScanValue={manualScanValue}
          pairError={error}
          pairing={pairing}
          cameraScanning={cameraScanning}
          pairIntent={pairIntent}
          selectedMachine={selectedMachine}
          signedIn={signedIn}
          canScanWithCamera={Boolean(scanPairingCode)}
          onClose={() => { haptic(); setScanOpen(false) }}
          onImport={() => void importManualScan()}
          onManualScanValueChange={setManualScanValue}
          onScanWithCamera={() => void scanWithCamera()}
        />
      ) : null}
      {transferCenterOpen ? (
        <GlobalTransferCenter
          fileTransfer={globalFileTransfer}
          onClose={() => { haptic(); setTransferCenterOpen(false) }}
          onResumeTransfer={resumeGlobalTransfer}
          onResumeAllTransfers={resumeAllGlobalTransfers}
        />
      ) : null}
    </main>
  )
}

function GlobalTransferCenter({
  fileTransfer,
  onClose,
  onResumeTransfer,
  onResumeAllTransfers,
}: {
  fileTransfer: FileTransferContext | undefined
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
  onTerminalSettingsChange,
}: {
  machine: WebControlMachine
  storage: RemoteRuntimeStorage | undefined
  terminalSettings: TerminalSettings
  runtime: MachineRuntime | null
  onBack: () => void
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
    <section className="flex min-h-0 flex-1 flex-col bg-zinc-50" data-testid="termx-machine-terminal-list">
      <LocalRemoteApp
        api={runtime.api}
        connector={runtime.connector}
        className="min-h-0 flex-1"
        connectionStateEvents={runtime.connectionStateEvents}
        inventoryEvents={runtime.inventoryEvents}
        fileTransfer={runtime.fileTransfer}
        terminalSettings={terminalSettings}
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
        onClick={onBack}
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
    <section className="flex min-h-0 flex-1 flex-col bg-zinc-50" data-testid="termx-machine-terminal-list">
      <MachineRuntimeHeader machine={machine} onBack={onBack} />
    </section>
  )
}

function HomeView({
  fileTransfer,
  transferState,
  loading,
  machines,
  pairedMachineIds,
  signedIn,
  user,
  onAddLocalDevice,
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
  pairedMachineIds: Set<string>
  signedIn: boolean
  user: WebControlUser | null
  onAddLocalDevice: () => void
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
            aria-label="Add local device"
            className="inline-flex h-10 w-10 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            type="button"
            onClick={onAddLocalDevice}
          >
            <Plus className="h-5 w-5" />
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
          message="No devices found. Add a local device, or sign in to sync your cloud devices."
          onAction={onAddLocalDevice}
          title="No machines yet"
        />
      ) : (
        <ul aria-label="Machines" className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
          {machines.map((machine) => (
            <li key={machine.id} className="mb-2 last:mb-0">
              <MachineRow
                authorizationState={machineAuthorizationState(machine, pairedMachineIds)}
                machine={machine}
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
}) {
  const handleNumberSetting = (key: 'fontSize' | 'scrollback', min: number, max: number) =>
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
    <section className="flex min-h-0 flex-1 flex-col bg-[var(--termx-bg)] text-[var(--termx-text)]" data-testid="termx-app-settings">
      <header className="flex min-h-14 shrink-0 items-center gap-3 border-b border-[var(--termx-border-subtle)] bg-[var(--termx-surface)] px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
        <button
          aria-label="Back to machines"
          className="inline-flex h-10 w-10 items-center justify-center rounded-md border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)] text-[var(--termx-text)] active:opacity-75 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--termx-accent)]"
          type="button"
          onClick={onBack}
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div className="min-w-0">
          <h1 className="text-lg font-semibold leading-6">Settings</h1>
          <p className="truncate text-xs font-medium text-[var(--termx-muted)]">{signedIn ? user?.email ?? 'Signed in' : 'Web Control sign in'}</p>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-5">
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

          <SettingsSection title="Terminal">
            <SettingsRow label="Font size">
              <div className="inline-flex h-9 items-center overflow-hidden rounded-lg border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)]">
                <button
                  aria-label="Decrease terminal font size"
                  className="h-9 w-9 text-lg font-semibold text-[var(--termx-text)] active:opacity-70"
                  type="button"
                  onClick={() => onTerminalSettingsChange({ fontSize: Math.max(8, terminalSettings.fontSize - 1) })}
                >
                  -
                </button>
                <input
                  aria-label="Terminal font size"
                  className="h-9 w-12 border-x border-[var(--termx-border-subtle)] bg-[var(--termx-surface)] px-1 text-center text-sm font-semibold text-[var(--termx-text)] outline-none focus:ring-2 focus:ring-[var(--termx-accent)]/25"
                  inputMode="numeric"
                  max={32}
                  min={8}
                  type="number"
                  value={terminalSettings.fontSize}
                  onChange={handleNumberSetting('fontSize', 8, 32)}
                />
                <button
                  aria-label="Increase terminal font size"
                  className="h-9 w-9 text-lg font-semibold text-[var(--termx-text)] active:opacity-70"
                  type="button"
                  onClick={() => onTerminalSettingsChange({ fontSize: Math.min(32, terminalSettings.fontSize + 1) })}
                >
                  +
                </button>
              </div>
            </SettingsRow>
            <SettingsRow label="Font">
              <SettingsSelect
                ariaLabel="Terminal font"
                value={terminalSettings.fontFamily}
                onChange={(value) => onTerminalSettingsChange({ fontFamily: value })}
              >
                {TERMINAL_FONT_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </SettingsSelect>
            </SettingsRow>
            <SettingsRow label="Theme">
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
                onChange={(value) => onTerminalSettingsChange({ renderer: value as TerminalRenderer })}
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
                onChange={(value) => onTerminalSettingsChange({ keyboardMode: value as TerminalKeyboardMode })}
              >
                <option value="auto">Auto</option>
                <option value="resize">Resize</option>
                <option value="shift">Shift up</option>
              </SettingsSelect>
            </SettingsRow>
            <SettingsRow label="Scrollback">
              <input
                aria-label="Terminal scrollback"
                className="h-9 w-28 rounded-lg border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)] px-3 text-right text-sm font-semibold text-[var(--termx-text)] outline-none focus:border-[var(--termx-accent)] focus:ring-2 focus:ring-[var(--termx-accent)]/25"
                inputMode="numeric"
                max={50000}
                min={500}
                step={500}
                type="number"
                value={terminalSettings.scrollback}
                onChange={handleNumberSetting('scrollback', 500, 50000)}
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
                    className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)] px-3 text-sm font-semibold text-[var(--termx-text)] active:opacity-75 disabled:cursor-not-allowed disabled:opacity-60"
                    type="button"
                    onClick={onRefresh}
                    disabled={loading}
                  >
                    <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                    Refresh
                  </button>
                  <button
                    className="inline-flex h-10 items-center justify-center rounded-lg bg-[var(--termx-accent)] px-3 text-sm font-semibold text-[var(--termx-accent-text)] active:opacity-85"
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
                  <label className="block text-sm font-medium text-[var(--termx-muted)]">
                    Email or username
                    <input
                      className="mt-2 h-11 w-full rounded-lg border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)] px-3 text-sm text-[var(--termx-text)] outline-none focus:border-[var(--termx-accent)] focus:ring-2 focus:ring-[var(--termx-accent)]/25"
                      value={login}
                      onChange={(event) => onLoginChange(event.target.value)}
                      autoComplete="username"
                    />
                  </label>
                </div>
                <div className="border-t border-[var(--termx-border-subtle)] px-4 py-3">
                  <label className="block text-sm font-medium text-[var(--termx-muted)]">
                    Password
                    <input
                      className="mt-2 h-11 w-full rounded-lg border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)] px-3 text-sm text-[var(--termx-text)] outline-none focus:border-[var(--termx-accent)] focus:ring-2 focus:ring-[var(--termx-accent)]/25"
                      value={password}
                      onChange={(event) => onPasswordChange(event.target.value)}
                      type="password"
                      autoComplete="current-password"
                    />
                  </label>
                </div>
                <div className="border-t border-[var(--termx-border-subtle)] px-4 py-3">
                  <button
                    className="inline-flex h-11 w-full items-center justify-center gap-2 rounded-lg bg-[var(--termx-accent)] px-3 text-sm font-semibold text-[var(--termx-accent-text)] active:opacity-85 disabled:cursor-not-allowed disabled:opacity-60"
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
      <h2 className="mb-2 px-4 text-xs font-semibold uppercase text-[var(--termx-muted)]">{title}</h2>
      <div className="overflow-hidden rounded-xl border border-[var(--termx-border-subtle)] bg-[var(--termx-surface)] shadow-sm">
        {children}
      </div>
    </section>
  )
}

function SettingsRow({
  children,
  label,
  value,
}: {
  children?: ReactNode
  label: string
  value?: string | undefined
}) {
  return (
    <div className="flex min-h-12 items-center justify-between gap-4 border-b border-[var(--termx-border-subtle)] px-4 py-2 last:border-b-0">
      <div className="min-w-0 text-sm font-medium text-[var(--termx-text)]">{label}</div>
      {children ? (
        <div className="shrink-0">{children}</div>
      ) : (
        <div className="min-w-0 truncate text-right text-sm font-medium text-[var(--termx-muted)]">{value}</div>
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
      className="h-9 max-w-[54vw] rounded-lg border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)] px-3 text-right text-sm font-semibold text-[var(--termx-text)] outline-none focus:border-[var(--termx-accent)] focus:ring-2 focus:ring-[var(--termx-accent)]/25 sm:max-w-xs"
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
    >
      {children}
    </select>
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
        onChange={(event) => onChange(event.currentTarget.value as TerminalSettings['themeId'])}
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
      <div aria-label="Theme previews" className="grid w-[min(28rem,calc(100vw-3rem))] grid-cols-2 gap-2" role="radiogroup">
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
      onClick={onSelect}
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
      onClick={() => onChange(!checked)}
    >
      <span className={`absolute left-1 top-1 h-6 w-6 rounded-full bg-white shadow-sm transition-transform ${checked ? 'translate-x-4' : 'translate-x-0'}`} />
    </button>
  )
}

function PairSheet({
  cameraScanning,
  canScanWithCamera,
  lastImported,
  manualScanValue,
  pairError,
  pairIntent,
  pairing,
  selectedMachine,
  signedIn,
  onClose,
  onImport,
  onManualScanValueChange,
  onScanWithCamera,
}: {
  cameraScanning: boolean
  canScanWithCamera: boolean
  lastImported: PairingPayload | null
  manualScanValue: string
  pairError: string | null
  pairIntent: PairIntent
  pairing: boolean
  selectedMachine: WebControlMachine | null
  signedIn: boolean
  onClose: () => void
  onImport: () => void
  onManualScanValueChange: (value: string) => void
  onScanWithCamera: () => void
}) {
  const title = pairIntent === 'add-local' ? 'Add Local Device' : 'Re-authorize Device'
  const primaryLabel = pairIntent === 'add-local' ? 'Add Device' : 'Pair Device'
  return (
    <div className="fixed inset-0 z-50 flex items-end bg-[var(--termx-overlay)] sm:items-center sm:justify-center" role="dialog" aria-modal="true">
      <section className="max-h-[88dvh] w-full overflow-y-auto rounded-t-lg border border-[var(--termx-border-subtle)] bg-[var(--termx-surface)] p-4 text-[var(--termx-text)] shadow-xl sm:max-w-md sm:rounded-lg" data-testid="termx-pair-sheet">
        <div className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2">
            <QrCode className="h-5 w-5 shrink-0 text-[var(--termx-accent)]" />
            <h2 className="truncate text-base font-semibold">{title}</h2>
          </div>
          <button
            aria-label="Close pairing"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md text-[var(--termx-muted)] active:bg-[var(--termx-surface-raised)] focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--termx-accent)]"
            type="button"
            onClick={onClose}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {selectedMachine ? (
          <div className="mt-4 rounded-md border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)] px-3 py-2">
            <div className="truncate text-sm font-semibold text-[var(--termx-text)]">{selectedMachine.name}</div>
            <div className="mt-0.5 truncate text-xs font-medium text-[var(--termx-muted)]">{selectedMachine.hostname || selectedMachine.id}</div>
          </div>
        ) : null}

        {canScanWithCamera ? (
          <button
            className="mt-4 inline-flex h-12 w-full items-center justify-center gap-2 rounded-md bg-[var(--termx-accent)] px-3 text-sm font-semibold text-[var(--termx-accent-text)] active:opacity-85 disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            onClick={onScanWithCamera}
            disabled={pairing || cameraScanning}
          >
            {cameraScanning ? <Loader2 className="h-4 w-4 animate-spin" /> : <Camera className="h-4 w-4" />}
            Scan QR with camera
          </button>
        ) : null}

        <details className="mt-4 rounded-md border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)] px-3 py-2">
          <summary className="flex cursor-pointer list-none items-center gap-2 text-xs font-semibold text-[var(--termx-muted)]">
            <Keyboard className="h-4 w-4" />
            Enter QR content manually
          </summary>
          <label className="mt-3 block text-xs font-semibold text-[var(--termx-muted)]">
            TermX QR content
            <textarea
              className="mt-1 h-36 w-full resize-none rounded-md border border-[var(--termx-border-subtle)] bg-[var(--termx-surface)] p-2 font-mono text-xs leading-5 text-[var(--termx-text)] outline-none focus:border-[var(--termx-accent)] focus:ring-2 focus:ring-[var(--termx-accent)]/25"
              value={manualScanValue}
              onChange={(event) => onManualScanValueChange(event.target.value)}
              placeholder="termx://pair?payload=..."
              spellCheck={false}
            />
          </label>
        </details>

        <button
          className="mt-3 inline-flex h-11 w-full items-center justify-center gap-2 rounded-md border border-[var(--termx-border-subtle)] bg-[var(--termx-surface-raised)] px-3 text-sm font-semibold text-[var(--termx-text)] active:opacity-80 disabled:cursor-not-allowed disabled:opacity-50"
          type="button"
          onClick={onImport}
          disabled={pairing || cameraScanning || manualScanValue.trim() === ''}
        >
          {pairing ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
          {primaryLabel}
        </button>

        {pairError ? (
          <p className="mt-3 rounded-md bg-red-500/10 px-3 py-2 text-sm font-medium text-red-500">{pairError}</p>
        ) : null}

        {lastImported ? (
          <div className="mt-3 rounded-md bg-emerald-500/10 px-3 py-2 text-xs font-medium text-emerald-500">
            <div className="truncate font-semibold">{lastImported.machine.name}</div>
            <div className="truncate">{lastImported.machine.id}</div>
          </div>
        ) : null}
      </section>
    </div>
  )
}

function MachineRow({
  authorizationState,
  machine,
  onPairMachine,
  onSelectMachine,
}: {
  authorizationState: MachineAuthorizationState
  machine: WebControlMachine
  onPairMachine: (machine: WebControlMachine) => void
  onSelectMachine: (machine: WebControlMachine) => void
}) {
  const actionLabel = authorizationState === 'ready'
    ? 'Open'
    : authorizationState === 'needs-session'
      ? 'Re-authorize'
      : 'Pair'
  const authPill = authorizationState === 'ready'
    ? 'Ready'
    : authorizationState === 'needs-session'
      ? 'Re-authorize'
      : 'Scan QR'
  return (
    <div className="grid w-full grid-cols-[minmax(0,1fr)_auto] overflow-hidden rounded-lg border border-zinc-200 bg-white shadow-sm">
      <button
        aria-label={`${actionLabel} ${machine.name}`}
        className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-3 px-3 py-3 text-left hover:bg-zinc-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-500"
        type="button"
        onClick={() => onSelectMachine(machine)}
      >
        <div className="flex h-11 w-11 items-center justify-center rounded-md bg-zinc-100 text-zinc-600">
          {machine.online ? <Wifi className="h-5 w-5" /> : <WifiOff className="h-5 w-5" />}
        </div>
        <div className="min-w-0">
          <div className="flex min-w-0 items-center justify-between gap-2">
            <span className="truncate text-[15px] font-semibold leading-5 text-zinc-950">{machine.name}</span>
            <span className={`shrink-0 rounded-md px-2 py-0.5 text-[11px] font-semibold leading-4 ring-1 ${machine.online ? 'bg-emerald-50 text-emerald-700 ring-emerald-200' : 'bg-zinc-100 text-zinc-600 ring-zinc-200'}`}>
              {machine.online ? 'Online' : 'Offline'}
            </span>
          </div>
          <div className="mt-0.5 truncate text-xs font-medium text-zinc-500">{machine.hostname || machine.id}</div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            <InfoPill>{machine.source === 'cloud' ? 'Cloud node' : 'Local node'}</InfoPill>
            <InfoPill>{machine.hubStatus === 'online' ? 'Hub online' : (machine.source === 'local' ? 'Direct/Local' : 'Offline')}</InfoPill>
            <InfoPill>{authPill}</InfoPill>
            {machine.lastSeen ? <InfoPill>{formatLastSeen(machine.lastSeen)}</InfoPill> : null}
          </div>
        </div>
      </button>
      {authorizationState !== 'ready' ? (
        <button
          aria-label={`Scan to ${authorizationState === 'needs-session' ? 're-authorize' : 'pair'} ${machine.name}`}
          className="flex h-full w-12 items-center justify-center border-l border-zinc-100 text-zinc-600 hover:bg-zinc-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-500"
          type="button"
          onClick={() => onPairMachine(machine)}
        >
          <QrCode className="h-5 w-5" />
        </button>
      ) : null}
    </div>
  )
}

function machineAuthorizationState(machine: WebControlMachine, pairedMachineIds: Set<string>): MachineAuthorizationState {
  if (pairedMachineIds.has(machine.id)) return 'ready'
  if (machine.paired) return 'needs-session'
  return 'unpaired'
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
          onClick={onAction}
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
    pairMachine: fail,
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

function readPairedMachineIds(storage: RemoteRuntimeStorage | undefined, userId: string | undefined): Set<string> {
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

function pruneMachinesForSignOut(storage: RemoteRuntimeStorage | undefined): StoredMachineRecord[] {
  if (!storage) return []
  const store = createMachineStore({ storage })
  const sessionStore = createMachineSessionStore(storage)
  for (const machine of store.listMachines()) {
    if (machine.source !== 'cloud') continue
    if (hasLocalAddresses(machine)) {
      store.saveMachine(downgradeCloudMachineToLocal(machine))
    } else {
      store.forgetMachine(machine.machineId)
      sessionStore.clearSessionToken(machine.machineId)
    }
  }
  return store.listMachines()
}

function mergeSignedInCloudMachines(storage: RemoteRuntimeStorage, cloudMachines: WebControlMachine[]): void {
  const store = createMachineStore({ storage })
  for (const cloud of cloudMachines) {
    const stored = store.getMachine(cloud.id)
    if (!stored) continue
    if (stored.source !== 'local' && stored.source !== 'manual') continue
    store.saveMachine(mergeCloudMachine(stored, cloud))
  }
}

function hasLocalAddresses(machine: StoredMachineRecord): boolean {
  return machine.addresses.local.length > 0 || machine.addresses.lan.length > 0
}

function downgradeCloudMachineToLocal(machine: StoredMachineRecord): StoredMachineRecord {
  return {
    ...machine,
    state: machine.state === 'online' ? 'unknown' : machine.state,
    source: 'local',
    preferredPath: 'local',
    addresses: {
      local: machine.addresses.local,
      lan: machine.addresses.lan,
      public: [],
    },
    endpoints: {},
    updatedAt: new Date().toISOString(),
  }
}

function createPairApiFromMachine(machine: WebControlMachine, networkRuntime: RemoteNetworkRuntime): PairApi {
  const [hubUrl] = nonEmptyHubUrls(machine)
  if (!hubUrl) {
    throw new Error('Hub endpoint is required before pairing this device')
  }
  return createManagedHubApi({ baseUrl: hubUrl, fetch: networkRuntime.fetch })
}

function createManagedMachineRuntime(input: {
  machine: WebControlMachine
  storage: RemoteRuntimeStorage
  api: WebControlApi
  networkRuntime: RemoteNetworkRuntime
  networkStateManager: RemoteNetworkStateManager
  createSession?: ManagedRtcSessionFactory | undefined
}): MachineRuntime {
  const sessionStore = createMachineSessionStore(input.storage)
  const [summaryHubUrl] = nonEmptyHubUrls(input.machine)
  const machineSession = createManagedMachineSessionManager({
    machine: input.machine,
    sessionStore,
    networkRuntime: input.networkRuntime,
    networkStateManager: input.networkStateManager,
    createSession: requiredManagedRtcSessionFactory(input.createSession),
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

function createManagedMachineSessionManager(input: {
  machine: WebControlMachine
  sessionStore: MachineSessionStore
  networkRuntime: RemoteNetworkRuntime
  networkStateManager: RemoteNetworkStateManager
  createSession: ManagedRtcSessionFactory
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
    createLease: createManagedMachineSessionLease,
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
    const hubUrls = nonEmptyHubUrls(input.machine)
    if (hubUrls.length === 0) throw new Error('Hub endpoint is required before opening this machine runtime')
    const sessionToken = input.sessionStore.getSessionToken(input.machine.id)
    if (!sessionToken) {
      throw new Error('Pair this machine before opening the runtime channel')
    }
    const answerProofSecret = input.sessionStore.getAnswerProofSecret(input.machine.id) ?? undefined
    const orchestrator = createConnectionOrchestrator({
      managedHubApiFactory: (hubUrl) => createManagedHubApi({ baseUrl: hubUrl, fetch: input.networkRuntime.fetch }),
      managedHubRtcConnectorFactory: ({ hubUrl, api }) => createManagedHubRtcConnector({
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
      hubUrls,
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

function requiredManagedRtcSessionFactory(factory: ManagedRtcSessionFactory | undefined): ManagedRtcSessionFactory {
  if (!factory) {
    throw new Error('managed RTC session factory is required')
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

function createManagedMachineSessionLease(session: RtcSession): RtcSession & RtcTerminalDataChannelController & RtcSessionLiveness {
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

function mergeCloudMachine(saved: StoredMachineRecord, machine: WebControlMachine): StoredMachineRecord {
  const [summaryHubUrl] = nonEmptyHubUrls(machine)
  return {
    machineId: saved.machineId,
    name: machine.name || saved.name,
    ...(machine.hostname || saved.hostname ? { hostname: machine.hostname ?? saved.hostname } : {}),
    state: machine.online ? 'online' : 'offline',
    terminalCount: saved.terminalCount,
    ...(machine.lastSeen || saved.lastSeenAt ? { lastSeenAt: machine.lastSeen ?? saved.lastSeenAt } : {}),
    ...(saved.lastConnectionPath ? { lastConnectionPath: saved.lastConnectionPath } : {}),
    preferredPath: 'managed',
    ...(saved.relayInUse !== undefined ? { relayInUse: saved.relayInUse } : {}),
    source: 'cloud',
    addresses: saved.addresses,
    endpoints: {
      ...saved.endpoints,
      ...(machine.controlUrl ? { webControl: machine.controlUrl } : {}),
      ...(summaryHubUrl ? { hub: summaryHubUrl } : {}),
    },
    ...(saved.pairing ? { pairing: saved.pairing } : {}),
    ...(saved.appBootstrap ? { appBootstrap: saved.appBootstrap } : {}),
    addedAt: saved.addedAt,
    updatedAt: saved.updatedAt,
  }
}

function hubUrlsFromStoredMachine(machine: StoredMachineRecord): string[] {
  return compactHubUrls([machine.endpoints.hub, ...machine.addresses.local, ...machine.addresses.lan, ...machine.addresses.public])
}

function hubUrlsFromPairingPayload(payload: PairingPayload): string[] {
  return compactHubUrls([...payload.addresses.local, ...payload.addresses.lan, ...payload.addresses.public])
}

function nonEmptyHubUrls(machine: WebControlMachine): string[] {
  return compactHubUrls(machine.hubUrls)
}

function compactHubUrls(values: readonly (string | undefined)[]): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const raw of values) {
    const value = raw?.trim()
    if (!value || seen.has(value)) continue
    seen.add(value)
    out.push(value)
  }
  return out
}

function formatLastSeen(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
