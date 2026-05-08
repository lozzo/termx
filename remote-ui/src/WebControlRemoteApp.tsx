import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowLeft, CheckCircle2, Loader2, LogIn, Monitor, QrCode, RefreshCw, Server, Settings, ShieldCheck, Wifi, WifiOff, X } from 'lucide-react'
import { createMachineSessionStore, type MachineSessionStore } from './localAppIdentity'
import { LocalRemoteApp, type LocalRemoteInventoryApi, type LocalRemoteSessionConnector } from './LocalRemoteApp'
import { createMachineStore, type StoredMachineRecord } from './machineStore'
import { createConnectionOrchestrator, type ConnectionAttemptSnapshot } from './connectionOrchestrator'
import { createManagedHubRtcConnector } from './managedHubRtcConnector'
import { consoleConnectionLogger } from './connectionLogger'
import { createManagedHubApi } from './managedHubApi'
import { parsePairingPayload, type PairingPayload } from './pairingPayload'
import type { ConnectionInfo, LocalPairingApi, LocalStatus, RemoteNetworkRuntime, RemoteRuntimeStorage, RtcBinaryChannel, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSessionLiveness, RtcSessionNegotiator, RtcSubscription, RtcTerminalDataChannelController, TerminalInventoryEvents } from './transport'
import { normalizeTerminalInventory } from './terminalInventory'
import { createWebControlApi, type WebControlApi, type WebControlMachine, type WebControlUser } from './webControlApi'

const storageKeys = {
  controlUrl: 'termx.remote.controlUrl',
  accessToken: 'termx.remote.accessToken',
} as const

const defaultWebControlUrl = ''
const appName = 'TermX Remote App'

type AppView = 'home' | 'settings' | 'machine'
type PairApi = LocalPairingApi
type ManagedRtcSessionFactory = (input: { machineId: string }) => RtcSession & RtcSessionNegotiator
type MachineRuntimeFactory = (input: {
  machine: WebControlMachine
  user: WebControlUser
  storage: RemoteRuntimeStorage
  api: WebControlApi
  networkRuntime: RemoteNetworkRuntime
  createSession: ManagedRtcSessionFactory
}) => MachineRuntime

interface MachineRuntime {
  api: LocalRemoteInventoryApi
  connector: LocalRemoteSessionConnector
  inventoryEvents?: TerminalInventoryEvents | undefined
  dispose?(): void | Promise<void>
}

export interface WebControlRemoteAppProps {
  defaultControlUrl?: string | undefined
  storage?: RemoteRuntimeStorage | undefined
  networkRuntime?: RemoteNetworkRuntime | undefined
  managedRtcSessionFactory?: ManagedRtcSessionFactory | undefined
  pairApiFactory?: ((payload: PairingPayload, machine: WebControlMachine) => PairApi) | undefined
  machineRuntimeFactory?: MachineRuntimeFactory | undefined
}

export function WebControlRemoteApp({
  defaultControlUrl,
  storage: storageProp,
  networkRuntime: networkRuntimeProp,
  managedRtcSessionFactory,
  pairApiFactory,
  machineRuntimeFactory = createManagedMachineRuntime,
}: WebControlRemoteAppProps) {
  const networkRuntime = networkRuntimeProp ?? unavailableNetworkRuntime
  const storage = storageProp ?? networkRuntime.storage
  const [view, setView] = useState<AppView>('home')
  const [controlUrl, setControlUrl] = useState(() => initialControlUrl(storage, defaultControlUrl, networkRuntime))
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [accessToken, setAccessToken] = useState(() => storage?.getItem(storageKeys.accessToken) ?? '')
  const [user, setUser] = useState<WebControlUser | null>(null)
  const [localMachines, setLocalMachines] = useState<StoredMachineRecord[]>([])
  const [machines, setMachines] = useState<WebControlMachine[]>([])
  const [selectedMachineId, setSelectedMachineId] = useState<string | null>(null)
  const [scanOpen, setScanOpen] = useState(false)
  const [manualScanValue, setManualScanValue] = useState('')
  const [lastImported, setLastImported] = useState<PairingPayload | null>(null)
  const [pairedMachineIds, setPairedMachineIds] = useState(() => readPairedMachineIds(storage, undefined))
  const [pairVersion, setPairVersion] = useState(0)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [pairing, setPairing] = useState(false)
  const signedIn = accessToken.trim() !== ''

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
        hubUrls: local.endpoints.hub ? [local.endpoints.hub] : [],
      })
    }
    for (const cloud of machines) {
      map.set(cloud.id, cloud)
    }
    return Array.from(map.values())
  }, [localMachines, machines])

  const selectedMachine = displayMachines.find((machine) => machine.id === selectedMachineId) ?? null

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

  const refreshMachines = useCallback(async () => {
    if (!accessToken) return
    setLoading(true)
    try {
      const [profile, cloudMachines] = await Promise.all([
        api.me(),
        api.listMachines(),
      ])
      setUser(profile)
      setMachines(cloudMachines)
      setSelectedMachineId((current) => {
        if (current && cloudMachines.some((machine) => machine.id === current)) return current
        return null
      })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [accessToken, api])

  useEffect(() => {
    void refreshMachines()
  }, [refreshMachines])

  useEffect(() => {
    setPairedMachineIds(readPairedMachineIds(storage, user?.id))
  }, [pairVersion, storage, user?.id])

  const submitLogin = useCallback(async () => {
    setLoading(true)
    setError(null)
    setMessage(null)
    try {
      const auth = await api.login({ login, password })
      storage?.setItem(storageKeys.controlUrl, controlUrl)
      storage?.setItem(storageKeys.accessToken, auth.accessToken)
      setAccessToken(auth.accessToken)
      setUser(auth.user)
      setPassword('')
      setMessage('Signed in')
      setView('home')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [api, controlUrl, login, password, storage])

  const signOut = useCallback(() => {
    storage?.removeItem(storageKeys.accessToken)
    setAccessToken('')
    setUser(null)
    setMachines([])
    setSelectedMachineId(null)
    setMessage(null)
    setError(null)
    setView('settings')
  }, [storage])

  const saveSettings = useCallback(() => {
    storage?.setItem(storageKeys.controlUrl, controlUrl)
    setMessage('Settings saved')
    setError(null)
    setView('home')
  }, [controlUrl, storage])

  const openPairSheet = useCallback((machineId?: string | undefined) => {
    if (machineId) setSelectedMachineId(machineId)
    setManualScanValue('')
    setLastImported(null)
    setError(null)
    setMessage(null)
    setScanOpen(true)
  }, [])

  const selectMachine = useCallback((machine: WebControlMachine) => {
    setSelectedMachineId(machine.id)
    if (!pairedMachineIds.has(machine.id)) {
      openPairSheet(machine.id)
      return
    }
    setView('machine')
    setMessage(null)
    setError(null)
  }, [openPairSheet, pairedMachineIds])

  const importManualScan = useCallback(async () => {
    if (!storage) {
      setError('Local storage is required before importing a TermX QR')
      setMessage(null)
      return
    }
    setPairing(true)
    setError(null)
    setMessage(null)
    try {
      const payload = parsePairingPayload(manualScanValue)
      const isCloud = payload.endpoints.webControl !== undefined || payload.preferredPath === 'managed'

      if (isCloud && !signedIn) {
        setScanOpen(false)
        setView('settings')
        setError('The scanned agent is online. Please sign in to your account first to fetch this agent.')
        return
      }

      let targetMachine: WebControlMachine

      if (isCloud) {
        if (!user) throw new Error('Account profile is required before pairing a cloud device')
        const cloudMachine = machines.find((machine) => machine.id === payload.machine.id)
        if (!cloudMachine) {
          throw new Error('This pairing code does not match a Web Control device in this account')
        }
        targetMachine = cloudMachine
      } else {
        targetMachine = {
          id: payload.machine.id,
          name: payload.machine.name,
          hostname: payload.machine.hostname,
          online: true,
          paired: false,
          source: 'local',
          hubUrls: payload.endpoints.hub ? [payload.endpoints.hub] : (payload.endpoints.localPairing ? [payload.endpoints.localPairing] : []),
        }
      }

      if (selectedMachine && selectedMachine.id !== targetMachine.id) {
        throw new Error(`This pairing code belongs to ${targetMachine.name}, not ${selectedMachine.name}`)
      }
      const pairApi = pairApiFactory?.(payload, targetMachine) ?? createPairApiFromMachine(targetMachine, networkRuntime)
      const pairResult = await pairApi.pair({
        machineId: targetMachine.id,
        pairSessionId: payload.pairing.sessionId,
        pairSecret: payload.pairing.secret,
        appDeviceId: createBrowserAppDeviceId(),
        appName,
        requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
      })
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
      setMessage(`Paired ${targetMachine.name}`)
      setManualScanValue('')
      setScanOpen(false)
      setView('machine')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setMessage(null)
    } finally {
      setPairing(false)
    }
  }, [machines, manualScanValue, networkRuntime, pairApiFactory, selectedMachine, signedIn, storage, user])

  return (
    <main className="flex h-full min-h-[100dvh] flex-col bg-zinc-50 text-zinc-950" data-testid="termx-web-control-remote">
      {view === 'settings' ? (
        <SettingsView
          controlUrl={controlUrl}
          error={error}
          loading={loading}
          login={login}
          message={message}
          password={password}
          signedIn={signedIn}
          user={user}
          onBack={() => setView('home')}
          onControlUrlChange={setControlUrl}
          onLoginChange={setLogin}
          onPasswordChange={setPassword}
          onRefresh={() => void refreshMachines()}
          onSaveSettings={saveSettings}
          onSignIn={() => void submitLogin()}
          onSignOut={signOut}
        />
      ) : view === 'machine' && selectedMachine ? (
        <MachineTerminalListView
          machine={selectedMachine}
          user={user}
          storage={storage}
          api={api}
          networkRuntime={networkRuntime}
          createSession={managedRtcSessionFactory}
          runtimeFactory={machineRuntimeFactory}
          message={message}
          error={error}
          onBack={() => {
            setView('home')
            setMessage(null)
            setError(null)
          }}
        />
      ) : (
        <HomeView
          error={error}
          loading={loading}
          machines={machines}
          message={message}
          pairedMachineIds={pairedMachineIds}
          signedIn={signedIn}
          user={user}
          onOpenPairSheet={() => openPairSheet()}
          onOpenSettings={() => setView('settings')}
          onRefresh={() => void refreshMachines()}
          onSelectMachine={selectMachine}
          onSignIn={() => setView('settings')}
        />
      )}

      {scanOpen ? (
        <PairSheet
          lastImported={lastImported}
          manualScanValue={manualScanValue}
          pairing={pairing}
          selectedMachine={selectedMachine}
          signedIn={signedIn}
          onClose={() => setScanOpen(false)}
          onImport={() => void importManualScan()}
          onManualScanValueChange={setManualScanValue}
        />
      ) : null}
    </main>
  )
}

function MachineTerminalListView({
  machine,
  user,
  storage,
  api,
  networkRuntime,
  createSession,
  runtimeFactory,
  message,
  error,
  onBack,
}: {
  machine: WebControlMachine
  user: WebControlUser | null
  storage: RemoteRuntimeStorage | undefined
  api: WebControlApi
  networkRuntime: RemoteNetworkRuntime
  createSession?: ManagedRtcSessionFactory | undefined
  runtimeFactory: MachineRuntimeFactory
  message: string | null
  error: string | null
  onBack: () => void
}) {
  const machineRef = useRef(machine)
  machineRef.current = machine
  const runtime = useMemo(() => {
    if (!user || !storage) return null
    return runtimeFactory({ machine: machineRef.current, user, storage, api, networkRuntime, createSession: requiredManagedRtcSessionFactory(createSession) })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, createSession, machine.id, networkRuntime, runtimeFactory, storage, user])
  useEffect(() => {
    if (!runtime) return
    return () => {
      void runtime.dispose?.()
    }
  }, [runtime])
  if (!user || !storage || !runtime) {
    return (
      <MachineRuntimeErrorShell
        machine={machine}
        message={message}
        error={error ?? 'Local app identity storage is required before opening this machine'}
        onBack={onBack}
      />
    )
  }
  return (
    <section className="flex min-h-0 flex-1 flex-col bg-zinc-50" data-testid="termx-machine-terminal-list">
      <StatusMessages error={error} message={message} />
      <LocalRemoteApp
        api={runtime.api}
        connector={runtime.connector}
        className="min-h-0 flex-1"
        inventoryEvents={runtime.inventoryEvents}
        onBack={onBack}
      />
    </section>
  )
}

function MachineRuntimeHeader({ machine, onBack }: { machine: WebControlMachine; onBack: () => void }) {
  return (
    <header className="flex shrink-0 items-center gap-3 border-b border-zinc-200 bg-white px-4 py-3">
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
  message,
  error,
  onBack,
}: {
  machine: WebControlMachine
  message: string | null
  error: string | null
  onBack: () => void
}) {
  return (
    <section className="flex min-h-0 flex-1 flex-col bg-zinc-50" data-testid="termx-machine-terminal-list">
      <MachineRuntimeHeader machine={machine} onBack={onBack} />
      <StatusMessages error={error} message={message} />
      <div className="flex flex-1 items-center justify-center p-4">
        <div className="w-full max-w-sm rounded-lg border border-red-200 bg-white p-4 text-sm font-medium text-red-700 shadow-sm">
          {error}
        </div>
      </div>
    </section>
  )
}

function HomeView({
  error,
  loading,
  machines,
  message,
  pairedMachineIds,
  signedIn,
  user,
  onOpenPairSheet,
  onOpenSettings,
  onRefresh,
  onSelectMachine,
  onSignIn,
}: {
  error: string | null
  loading: boolean
  machines: WebControlMachine[]
  message: string | null
  pairedMachineIds: Set<string>
  signedIn: boolean
  user: WebControlUser | null
  onOpenPairSheet: () => void
  onOpenSettings: () => void
  onRefresh: () => void
  onSelectMachine: (machine: WebControlMachine) => void
  onSignIn: () => void
}) {
  return (
    <section className="flex min-h-0 flex-1 flex-col" data-testid="termx-app-home">
      <header className="flex shrink-0 items-center justify-between gap-3 border-b border-zinc-200 bg-white px-4 py-3">
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
            aria-label="Scan pairing QR"
            className="inline-flex h-10 w-10 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            type="button"
            onClick={onOpenPairSheet}
          >
            <QrCode className="h-5 w-5" />
          </button>
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

      <StatusMessages error={error} message={message} />

      {machines.length === 0 ? (
        <EmptyState
          actionLabel="Scan QR"
          icon="scan"
          message="No devices found. Scan QR to add a local device, or sign in to sync your cloud devices."
          onAction={onOpenPairSheet}
          title="No machines yet"
        />
      ) : (
        <ul aria-label="Machines" className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
          {machines.map((machine) => (
            <li key={machine.id} className="mb-2 last:mb-0">
              <MachineRow
                machine={machine}
                paired={pairedMachineIds.has(machine.id)}
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
  message,
  password,
  signedIn,
  user,
  onBack,
  onControlUrlChange,
  onLoginChange,
  onPasswordChange,
  onRefresh,
  onSaveSettings,
  onSignIn,
  onSignOut,
}: {
  controlUrl: string
  error: string | null
  loading: boolean
  login: string
  message: string | null
  password: string
  signedIn: boolean
  user: WebControlUser | null
  onBack: () => void
  onControlUrlChange: (value: string) => void
  onLoginChange: (value: string) => void
  onPasswordChange: (value: string) => void
  onRefresh: () => void
  onSaveSettings: () => void
  onSignIn: () => void
  onSignOut: () => void
}) {
  return (
    <section className="flex min-h-0 flex-1 flex-col" data-testid="termx-app-settings">
      <header className="flex shrink-0 items-center gap-3 border-b border-zinc-200 bg-white px-4 py-3">
        <button
          aria-label="Back to machines"
          className="inline-flex h-10 w-10 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
          type="button"
          onClick={onBack}
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div className="min-w-0">
          <h1 className="text-lg font-semibold leading-6">Settings</h1>
          <p className="truncate text-xs font-medium text-zinc-500">{signedIn ? user?.email ?? 'Signed in' : 'Web Control sign in'}</p>
        </div>
      </header>

      <StatusMessages error={error} message={message} />

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
        <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
          <section className="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
            <label className="block text-sm font-semibold text-zinc-800">
              Web Control
              <input
                className="mt-1 h-11 w-full rounded-md border border-zinc-300 px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                value={controlUrl}
                onChange={(event) => onControlUrlChange(event.target.value)}
                placeholder="http://your-control-server:12306"
              />
            </label>
            <button
              className="mt-3 inline-flex h-10 w-full items-center justify-center rounded-md border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-800 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
              type="button"
              onClick={onSaveSettings}
            >
              Save settings
            </button>
          </section>

          <section className="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
            {signedIn ? (
              <div className="space-y-3">
                <div>
                  <h2 className="text-base font-semibold">Account</h2>
                  <p className="mt-1 truncate text-sm font-medium text-zinc-500">{user?.email ?? 'Signed in'}</p>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <button
                    className="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-800 hover:bg-zinc-100 disabled:cursor-not-allowed disabled:opacity-60"
                    type="button"
                    onClick={onRefresh}
                    disabled={loading}
                  >
                    <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                    Refresh
                  </button>
                  <button
                    className="inline-flex h-10 items-center justify-center rounded-md bg-zinc-900 px-3 text-sm font-semibold text-white hover:bg-zinc-800"
                    type="button"
                    onClick={onSignOut}
                  >
                    Sign out
                  </button>
                </div>
              </div>
            ) : (
              <div className="space-y-3">
                <h2 className="text-base font-semibold">Account</h2>
                <label className="block text-sm font-semibold text-zinc-800">
                  Email or username
                  <input
                    className="mt-1 h-11 w-full rounded-md border border-zinc-300 px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                    value={login}
                    onChange={(event) => onLoginChange(event.target.value)}
                    autoComplete="username"
                  />
                </label>
                <label className="block text-sm font-semibold text-zinc-800">
                  Password
                  <input
                    className="mt-1 h-11 w-full rounded-md border border-zinc-300 px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                    value={password}
                    onChange={(event) => onPasswordChange(event.target.value)}
                    type="password"
                    autoComplete="current-password"
                  />
                </label>
                <button
                  className="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md bg-zinc-900 px-3 text-sm font-semibold text-white hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-60"
                  type="button"
                  onClick={onSignIn}
                  disabled={loading}
                >
                  <LogIn className="h-4 w-4" />
                  Sign in
                </button>
              </div>
            )}
          </section>
        </div>
      </div>
    </section>
  )
}

function PairSheet({
  lastImported,
  manualScanValue,
  pairing,
  selectedMachine,
  signedIn,
  onClose,
  onImport,
  onManualScanValueChange,
}: {
  lastImported: PairingPayload | null
  manualScanValue: string
  pairing: boolean
  selectedMachine: WebControlMachine | null
  signedIn: boolean
  onClose: () => void
  onImport: () => void
  onManualScanValueChange: (value: string) => void
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-end bg-zinc-950/30 sm:items-center sm:justify-center" role="dialog" aria-modal="true">
      <section className="max-h-[88dvh] w-full overflow-y-auto rounded-t-lg border border-zinc-200 bg-white p-4 shadow-xl sm:max-w-md sm:rounded-lg" data-testid="termx-pair-sheet">
        <div className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2">
            <QrCode className="h-5 w-5 shrink-0 text-zinc-600" />
            <h2 className="truncate text-base font-semibold">Pair Device</h2>
          </div>
          <button
            aria-label="Close pairing"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md text-zinc-600 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            type="button"
            onClick={onClose}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {selectedMachine ? (
          <div className="mt-4 rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2">
            <div className="truncate text-sm font-semibold text-zinc-950">{selectedMachine.name}</div>
            <div className="mt-0.5 truncate text-xs font-medium text-zinc-500">{selectedMachine.hostname || selectedMachine.id}</div>
          </div>
        ) : null}

        <label className="mt-4 block text-xs font-semibold text-zinc-600">
          TermX QR content
          <textarea
            className="mt-1 h-44 w-full resize-none rounded-md border border-zinc-300 p-2 font-mono text-xs leading-5 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            value={manualScanValue}
            onChange={(event) => onManualScanValueChange(event.target.value)}
            placeholder="termx://pair?payload=..."
            spellCheck={false}
          />
        </label>

        <button
          className="mt-3 inline-flex h-11 w-full items-center justify-center gap-2 rounded-md bg-zinc-900 px-3 text-sm font-semibold text-white hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          onClick={onImport}
          disabled={!signedIn || pairing || manualScanValue.trim() === ''}
        >
          {pairing ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
          Pair Device
        </button>

        {lastImported ? (
          <div className="mt-3 rounded-md bg-emerald-50 px-3 py-2 text-xs font-medium text-emerald-700">
            <div className="truncate text-emerald-900">{lastImported.machine.name}</div>
            <div className="truncate">{lastImported.machine.id}</div>
          </div>
        ) : null}
      </section>
    </div>
  )
}

function MachineRow({
  machine,
  paired,
  onSelectMachine,
}: {
  machine: WebControlMachine
  paired: boolean
  onSelectMachine: (machine: WebControlMachine) => void
}) {
  return (
    <button
      aria-label={`${paired ? 'Open' : 'Pair'} ${machine.name}`}
      className="grid w-full grid-cols-[auto_minmax(0,1fr)] gap-3 rounded-lg border border-zinc-200 bg-white px-3 py-3 text-left shadow-sm hover:border-zinc-300 hover:bg-zinc-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
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
          <InfoPill>{paired ? 'Ready' : 'Scan QR'}</InfoPill>
          {machine.lastSeen ? <InfoPill>{formatLastSeen(machine.lastSeen)}</InfoPill> : null}
        </div>
      </div>
    </button>
  )
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

function StatusMessages({ error, message }: { error: string | null; message: string | null }) {
  if (!error && !message) return null
  return (
    <div className="shrink-0 space-y-2 px-4 py-3">
      {error ? <p className="rounded-md bg-red-50 px-3 py-2 text-sm font-medium text-red-700">{error}</p> : null}
      {message ? (
        <p className="inline-flex w-full items-center gap-2 rounded-md bg-emerald-50 px-3 py-2 text-sm font-medium text-emerald-700">
          <CheckCircle2 className="h-4 w-4 shrink-0" />
          {message}
        </p>
      ) : null}
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

function initialControlUrl(storage: RemoteRuntimeStorage | undefined, fallback: string | undefined, networkRuntime: RemoteNetworkRuntime): string {
  const fromQuery = networkRuntime.queryParam('control')
  const queryValue = cleanControlUrl(fromQuery)
  if (queryValue) return queryValue
  const storedValue = cleanControlUrl(storage?.getItem(storageKeys.controlUrl))
  if (storedValue && !isRemoteUiLocalUrl(storedValue)) return storedValue
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

function isRemoteUiLocalUrl(value: string): boolean {
  try {
    const url = new URL(value)
    const host = url.hostname.toLowerCase()
    return (host === 'localhost' || host === '127.0.0.1' || host === '[::1]' || host === '::1') &&
      (url.port === '5173' || url.port === '5174' || url.port === '18888')
  } catch {
    return false
  }
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

function createPairApiFromMachine(machine: WebControlMachine, networkRuntime: RemoteNetworkRuntime): PairApi {
  const [hubUrl] = nonEmptyHubUrls(machine)
  if (!hubUrl) {
    throw new Error('Hub endpoint is required before pairing this Web Control device')
  }
  return createManagedHubApi({ baseUrl: hubUrl, fetch: networkRuntime.fetch })
}

function createManagedMachineRuntime(input: {
  machine: WebControlMachine
  user: WebControlUser
  storage: RemoteRuntimeStorage
  api: WebControlApi
  networkRuntime: RemoteNetworkRuntime
  createSession: ManagedRtcSessionFactory
}): MachineRuntime {
  const sessionStore = createMachineSessionStore(input.storage)
  const [summaryHubUrl] = nonEmptyHubUrls(input.machine)
  const machineSession = createManagedMachineSessionManager({
    machine: input.machine,
    sessionStore,
    controlApi: input.api,
    networkRuntime: input.networkRuntime,
    createSession: input.createSession,
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
          onSnapshot: (snapshot) => options?.onStatus?.(snapshot.message),
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
        const session = await machineSession.get(options)
        return createManagedMachineSessionLease(session)
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
  controlApi: WebControlApi
  networkRuntime: RemoteNetworkRuntime
  createSession: ManagedRtcSessionFactory
}) {
  let sessionPromise: Promise<RtcSession> | null = null
  let currentSession: RtcSession | null = null
  const resetCurrentSession = async () => {
    const session = currentSession
    currentSession = null
    sessionPromise = null
    await session?.disconnect()
  }
  const subscribeInventoryEvents = (handler: (event: { type: 'inventory_changed'; payload?: unknown }) => void): RtcSubscription => {
    let closed = false
    let subscription: RtcSubscription | null = null
    void getSession().then((session) => {
      if (closed) return
      subscription = session.subscribeEvents((event) => {
        if (isTerminalInventoryRuntimeEvent(event)) handler({ type: 'inventory_changed', payload: event.payload })
      })
      if (closed) {
        subscription.close()
        subscription = null
      }
    }).catch(() => {})
    return {
      close() {
        closed = true
        subscription?.close()
        subscription = null
      },
    }
  }
  const connect = async (options?: { signal?: AbortSignal; onSnapshot?: (snapshot: ConnectionAttemptSnapshot) => void }): Promise<RtcSession> => {
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
      onSnapshot: options?.onSnapshot,
    }, options)
    return result.session
  }
  const getSession = async (options?: { signal?: AbortSignal; onSnapshot?: (snapshot: ConnectionAttemptSnapshot) => void }): Promise<RtcSession> => {
    if (currentSession) {
      if (isRtcSessionAlive(currentSession)) return currentSession
      await resetCurrentSession()
    }
    if (!sessionPromise) {
      sessionPromise = connect(options).then((session) => {
        currentSession = session
        const lifecycle = session as RtcSession & Partial<{
          onDisconnect(handler: () => void): RtcSubscription
        }>
        let subscription: RtcSubscription | null = null
        subscription = lifecycle.onDisconnect?.(() => {
          if (currentSession === session) {
            currentSession = null
            sessionPromise = null
          }
          subscription?.close()
        }) ?? null
        return session
      }).catch((err) => {
        sessionPromise = null
        throw err
      })
    }
    return sessionPromise
  }
  return {
    get: getSession,
    subscribeInventoryEvents,
    reset: resetCurrentSession,
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

function isRtcSessionAlive(session: RtcSession): boolean {
  const candidate = session as RtcSession & Partial<RtcSessionLiveness>
  if (typeof candidate.isAlive !== 'function') return true
  return candidate.isAlive()
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

function createManagedMachineSessionLease(session: RtcSession): RtcSession & RtcTerminalDataChannelController {
  const openedTerminals = new Map<string, RtcBinaryChannel>()
  const openedFiles = new Map<string, RtcBinaryChannel>()
  const subscriptions = new Set<RtcSubscription>()
  return {
    async openTerminal(id: string) {
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
      return createSharedApiLeaseChannel(await session.openApi())
    },
    async openFileTransfer(transferId: string) {
      const channel = await session.openFileTransfer(transferId)
      openedFiles.set(transferId, channel)
      return channel
    },
    subscribeEvents(handler: (event: RtcEvent) => void) {
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
    async disconnect() {
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
    ...(saved.preferredPath ? { preferredPath: saved.preferredPath } : {}),
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
    schemaVersion: 2,
    addedAt: saved.addedAt,
    updatedAt: saved.updatedAt,
  }
}

function nonEmptyHubUrls(machine: WebControlMachine): string[] {
  return machine.hubUrls.map((hubUrl) => hubUrl.trim()).filter((hubUrl) => hubUrl !== '')
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
