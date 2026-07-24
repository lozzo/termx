import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties, type ChangeEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ArrowLeft, Camera, ChevronRight, Cloud, Copy, Download, Info, Keyboard, LaptopMinimal, LogIn, LogOut, Monitor, MoreHorizontal, Plus, QrCode, RefreshCw, Server, Settings, ShieldCheck, Trash2, Wifi, X } from 'lucide-react'
import { MachineWorkspace, type MachineWorkspaceInventoryApi, type MachineWorkspaceConnector } from './MachineWorkspace'
import { createMachineStore, type StoredMachineRecord } from '../state/machineStore'
import type { MachineConnectionSnapshot } from '../connection/machineConnectionSnapshot'
import { FileTransferPanel } from '../files/FileTransferPanel'
import { hapticError, hapticImpact, hapticSelection, hapticSuccess } from '../platform/haptics'
import { addNativeBackHandler } from '../platform/nativeBack'
import type { FileTransferContext, TransferInfo } from '../files/fileApi'
import type { MachineConnectionStateEvents, RemoteNetworkRuntime, RemoteRuntimeFetch, RemoteRuntimeStorage, RtcConnectOptions, TerminalInventoryEvents } from '../core/transport'
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
import type { MachineAccessClass } from '../state/appMachine'
import { muxviaIntlLocale, muxviaLanguages, normalizeMuxviaLanguage } from '../i18n'

const storageKeys = {
  accessToken: 'muxvia.remote.accessToken',
} as const

const defaultWebControlUrl = ''
const appName = 'Muxvia Remote App'

function noopSubscribe(_listener: () => void): () => void { return () => {} }

function appErrorCode(error: unknown): string {
  if (!error || typeof error !== 'object' || !('code' in error)) return ''
  const code = (error as { code?: unknown }).code
  return typeof code === 'string' ? code.trim().toLowerCase() : ''
}

function localizedAppError(error: unknown, t: TFunction): string {
  switch (appErrorCode(error)) {
    case 'login_required':
    case 'unauthenticated':
      return t('errors.loginRequired')
    case 'capability_invalid':
    case 'capability_expired':
    case 'authorization_revoked':
      return t('errors.pairAgain')
    case 'unavailable':
    case 'route_unavailable':
      return t('errors.connectionFailed')
    case 'temporary':
      return t('errors.temporary')
    case 'entitlement_denied':
      return t('errors.relayEntitlementDenied')
    default:
      return t('errors.generic')
  }
}

function isCameraUnavailableError(error: unknown): boolean {
  const detail = error instanceof Error ? `${error.name} ${error.message}` : String(error ?? '')
  return /NotAllowedError|Permission denied|PermissionDenied|NotFoundError|DevicesNotFoundError/i.test(detail)
}

function isCancelledAppError(error: unknown): boolean {
  const code = appErrorCode(error)
  return code === 'cancelled' || code === 'canceled'
}

type AppView = 'home' | 'settings' | 'machine'
type PairIntent = 'add-local' | 'authorize-machine'
type ScanFlowState = 'idle' | 'scanning' | 'pairing'
type MachineAuthorizationState = 'ready' | 'expired' | 'unauthorized'
type DisplayMachine = WebControlMachine & {
  reachability?: MachineReachabilityView | undefined
  accessClass: MachineAccessClass
  terminalCount?: number | undefined
}
interface MachineReachabilityView {
  hubOnline: boolean
  localChecked: boolean
  localOnline: boolean
  localOnlineUrls: string[]
}
interface LocalHubReachabilityTarget {
  machineId: string
  urls: string[]
}
interface LocalHubReachabilitySnapshot {
  machineId: string
  urls: string[]
  onlineUrls: string[]
  checkedAt: number
}
const emptyMachineConnectionSnapshot: MachineConnectionSnapshot = {
  machineId: '',
  phase: 'idle',
  statusText: 'Ready',
  connectionInfo: null,
  forceRelay: false,
  relayInUse: false,
  reconnectAttempt: 0,
  error: null,
}
const getEmptyMachineConnectionSnapshot = () => emptyMachineConnectionSnapshot
const localHubReachabilityProbeTimeoutMs = 2_500
export interface ScanPairingCodeOptions {
  onCancel?: (() => void) | undefined
  onManualEntry?: (() => void) | undefined
}

/** ExternalPairingImportResult 是平台 secure-store 导入成功后可进入共享 UI 的非秘密机器投影。 */
export interface ExternalPairingImportResult {
  machine: { id: string; name: string; hostname?: string | undefined; accessClass?: MachineAccessClass | undefined }
  expiresAt?: string | undefined
  authorizationRequired?: boolean | undefined
  sshCredentials?: { routeId: string; authorizedKey: string; fingerprint: string }[] | undefined
}

/** EndpointSharePreviewView 是 Go Client Engine 验证并计算后的 config-only 导入差异。 */
export interface EndpointSharePreviewView {
  importToken: string
  endpointId: string
  label: string
  deviceId: string
  deviceFingerprint: string
  routes: { id: string; kind: string; action: string }[]
  connectModeChanged: boolean
  selectionPolicyChanged: boolean
  credentialKinds: string[]
}

/**
 * ExternalPairingAdapter 允许 Android/iOS 把 bearer capability 留在 native secure-store。
 * 共享 UI 只查询 grant_ref 是否存在，不接收、持久化或转发原始 grant。
 */
export interface ExternalPairingAdapter {
  /** import 必须在写平台凭据前校验 expectedMachineId；未指定时表示全局新增设备。 */
  import(rawValue: string, expectedMachineId?: string): Promise<ExternalPairingImportResult | null>
  inspectShare?(rawValue: string): Promise<EndpointSharePreviewView>
  commitShare?(importToken: string): Promise<ExternalPairingImportResult>
  isAuthorized(machineId: string): boolean
  authorizationExpiresAt?(machineId: string): string | undefined
  forget(machineId: string): void | Promise<void>
}

export interface CloudAccountSummary {
  accountId: string
  accountLabel: string
  planId: string
  planName: string
  subscriptionStatus: string
  subscriptionRevision: number
}

/** CloudAccountAdapter 让 Official App 通过 native 私有模块登录；共享 UI 不接收 edge token。 */
export interface CloudAccountAdapter {
  current(): Promise<CloudAccountSummary | null>
  refresh?(): Promise<CloudAccountSummary | null>
  beginActivation(): Promise<{ userCode: string; expiresAtUnix: number }>
  claimActivation(payload: string): Promise<{ userCode: string; expiresAtUnix: number }>
  awaitActivation(): Promise<CloudAccountSummary>
  cancelActivation(): Promise<void>
  /** listMachines 只返回同账号 daemon 发现投影；授权状态由 ExternalPairingAdapter 决定。 */
  listMachines(): Promise<WebControlMachine[]>
  logout(): Promise<void>
}
export type MachineRuntimeFactory = (input: {
  machine: WebControlMachine
  storage: RemoteRuntimeStorage
}) => MachineRuntime

export interface MachineRuntime {
  api: MachineWorkspaceInventoryApi
  connector: MachineWorkspaceConnector
  inventoryEvents?: TerminalInventoryEvents | undefined
  connectionStateEvents?: MachineConnectionStateEvents | undefined
  listConnectionState?: {
    getSnapshot(): MachineConnectionSnapshot
    subscribe(listener: () => void): () => void
  } | undefined
  fileTransfer?: FileTransferContext | undefined
  dispose?(): void | Promise<void>
}

export interface RemoteControlAppProps {
  defaultControlUrl?: string | undefined
  storage?: RemoteRuntimeStorage | undefined
  networkRuntime?: RemoteNetworkRuntime | undefined
  machineRuntimeFactory?: MachineRuntimeFactory | undefined
  globalFileTransfer?: FileTransferContext | undefined
  scanPairingCode?: ((options?: ScanPairingCodeOptions) => Promise<string | null>) | undefined
  externalPairingAdapter?: ExternalPairingAdapter | undefined
  exportDebugLogs?: (() => Promise<void>) | undefined
  cloudAccountAdapter?: CloudAccountAdapter | undefined
}

export function RemoteControlApp({
  defaultControlUrl,
  storage: storageProp,
  networkRuntime: networkRuntimeProp,
  machineRuntimeFactory = createUnavailableMachineRuntime,
  globalFileTransfer,
  scanPairingCode,
  externalPairingAdapter,
  exportDebugLogs,
  cloudAccountAdapter,
}: RemoteControlAppProps) {
  const { t } = useTranslation()
  const networkRuntime = networkRuntimeProp ?? unavailableNetworkRuntime
  const storage = storageProp ?? networkRuntime.storage
  const [view, setView] = useState<AppView>('home')
  const controlUrl = useMemo(() => initialControlUrl(defaultControlUrl, networkRuntime), [defaultControlUrl, networkRuntime])
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [accessToken, setAccessToken] = useState(() => storage?.getItem(storageKeys.accessToken) ?? '')
  const [cloudAccount, setCloudAccount] = useState<CloudAccountSummary | null>(null)
  const [cloudActivation, setCloudActivation] = useState<{ userCode: string; expiresAtUnix: number } | null>(null)
  const [terminalSettings, setTerminalSettings] = useState<TerminalSettings>(() => readTerminalSettings(storage))
  const [user, setUser] = useState<WebControlUser | null>(null)
  const [localMachines, setLocalMachines] = useState<StoredMachineRecord[]>(() => {
    return storage ? createMachineStore({ storage }).listMachines() : []
  })
  const [machines, setMachines] = useState<WebControlMachine[]>([])
  const [localHubReachability, setLocalHubReachability] = useState<Map<string, LocalHubReachabilitySnapshot>>(() => new Map())
  const [selectedMachineId, setSelectedMachineId] = useState<string | null>(null)
  const [scanOpen, setScanOpen] = useState(false)
  const [pairIntent, setPairIntent] = useState<PairIntent>('add-local')
  const [transferCenterOpen, setTransferCenterOpen] = useState(false)
  const [manualScanValue, setManualScanValue] = useState('')
  const [authorizedMachineIds, setAuthorizedMachineIds] = useState(() => readAuthorizedMachineIds(storage, undefined, externalPairingAdapter))
  const [authorizationExpiries, setAuthorizationExpiries] = useState(() => readAuthorizationExpiries(storage, externalPairingAdapter))
  const [pairVersion, setPairVersion] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [pairing, setPairing] = useState(false)
  const [sharePreview, setSharePreview] = useState<EndpointSharePreviewView | null>(null)
  const [sshCredentialNotice, setSSHCredentialNotice] = useState<NonNullable<ExternalPairingImportResult['sshCredentials']> | null>(null)
  const [cameraScanning, setCameraScanning] = useState(false)
  const [scanFlowState, setScanFlowState] = useState<ScanFlowState>('idle')
  const signedIn = cloudAccountAdapter ? cloudAccount !== null : accessToken.trim() !== ''
  const appThemeStyle = useMemo(() => terminalThemeCssVariables(terminalSettings.themeId) as CSSProperties, [terminalSettings.themeId])
  const cameraScanInFlightRef = useRef(false)
  const runtimeCacheRef = useRef<{
    api: WebControlApi
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
      cache.networkRuntime === networkRuntime &&
      cache.runtimeFactory === machineRuntimeFactory &&
      cache.storage === storage
    if (!cacheMatches) {
      if (cache) {
        for (const runtime of cache.runtimes.values()) void runtime.dispose?.()
      }
      runtimeCacheRef.current = {
        api,
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

  const getMachineRuntime = useCallback((machine: DisplayMachine): MachineRuntime | null => {
    if (!storage || !runtimeCacheRef.current) return null
    const cache = runtimeCacheRef.current.runtimes
    const existing = cache.get(machine.id)
    if (existing) return existing
    const created = machineRuntimeFactory({
      machine,
      storage,
    })
    cache.set(machine.id, created)
    return created
  }, [machineRuntimeFactory, storage])

  const getExistingMachineRuntime = useCallback((machine: DisplayMachine): MachineRuntime | null => {
    return runtimeCacheRef.current?.runtimes.get(machine.id) ?? null
  }, [])

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

  const localHubReachabilityTargets = useMemo(() => {
    return buildLocalHubReachabilityTargets(localMachines, machines, signedIn)
  }, [localMachines, machines, signedIn])

  useEffect(() => {
    setLocalHubReachability((current) => pruneLocalHubReachability(current, localHubReachabilityTargets))
    if (localHubReachabilityTargets.length === 0) return
    const controller = new AbortController()
    for (const target of localHubReachabilityTargets) {
      void probeLocalHubReachability(networkRuntime.fetch, target, controller.signal).then((snapshot) => {
        if (controller.signal.aborted) return
        setLocalHubReachability((current) => {
          const previous = current.get(snapshot.machineId)
          if (sameReachabilitySnapshot(previous, snapshot)) return current
          const next = new Map(current)
          next.set(snapshot.machineId, snapshot)
          return next
        })
      })
    }
    return () => controller.abort()
  }, [localHubReachabilityTargets, networkRuntime.fetch])

  const displayMachines = useMemo(() => {
    const map = new Map<string, DisplayMachine>()
    const localById = new Map(localMachines.map((machine) => [machine.machineId, machine]))
    for (const local of localMachines) {
      const reachability = localHubReachability.get(local.machineId)
      const localOnline = localMachineOnline(local, reachability)
      map.set(local.machineId, {
        id: local.machineId,
        name: userFacingMachineName(local.machineId, local.name, local.hostname, t),
        hostname: local.hostname,
        online: localOnline,
        source: local.source === 'hub' ? 'hub' : 'local',
        hubUrls: hubUrlsFromStoredMachine(local),
        localHubUrls: localHubUrlsFromStoredMachine(local),
        localFallbackHubUrls: localFallbackHubUrlsFromStoredMachine(local),
        reachability: machineReachabilityView({
          hubOnline: false,
          localOnline,
          snapshot: reachability,
        }),
        accessClass: local.accessClass ?? 'local',
        terminalCount: local.terminalCount,
      })
    }
    for (const hub of machines) {
      const local = localById.get(hub.id)
      const reachability = local ? localHubReachability.get(local.machineId) : undefined
      const localOnline = local ? localMachineOnline(local, reachability) : false
      map.set(hub.id, {
        ...hub,
        name: userFacingMachineName(hub.id, hub.name, hub.hostname, t),
        online: hub.online || localOnline,
        ...(local ? {
          localHubUrls: localHubUrlsFromStoredMachine(local),
          localFallbackHubUrls: localFallbackHubUrlsFromStoredMachine(local, hub.hubUrls),
        } : {}),
        reachability: machineReachabilityView({
          hubOnline: hub.online,
          localOnline,
          snapshot: reachability,
        }),
        accessClass: local?.accessClass === 'cloud'
          ? 'cloud'
          : local
            ? 'local_cloud'
            : 'cloud',
        terminalCount: local?.terminalCount,
      })
    }
    return Array.from(map.values())
  }, [localHubReachability, localMachines, machines, t])

  const selectedMachine = displayMachines.find((machine) => machine.id === selectedMachineId) ?? null
  const emptyTransferSnapshot = useMemo(() => ({ transfers: [], hasActiveTransfers: false }), [])
  const globalTransferState = useSyncExternalStore(
    globalFileTransfer?.subscribe ?? noopSubscribe,
    globalFileTransfer?.getSnapshot ?? (() => emptyTransferSnapshot),
  )

  const refreshMachines = useCallback(async (refreshCloudAccount = false) => {
    if (cloudAccountAdapter) {
      const localMachineList = storage ? createMachineStore({ storage }).listMachines() : []
      let resolvedAccountId: string | undefined
      setLocalMachines(localMachineList)
      setLoading(true)
      try {
        // Cloud 账号是可选能力。账号摘要是目录查询的准入真值，未登录时不得用一次
        // 必然失败的目录请求制造全局 login_required，也不得影响本地 Endpoint。
        const account = refreshCloudAccount && cloudAccountAdapter.refresh
          ? await cloudAccountAdapter.refresh()
          : await cloudAccountAdapter.current()
        if (!account) {
          setCloudAccount(null)
          setUser(null)
          setMachines([])
          setError(null)
          setAuthorizedMachineIds(readAuthorizedMachineIds(storage, undefined, externalPairingAdapter))
          setAuthorizationExpiries(readAuthorizationExpiries(storage, externalPairingAdapter))
          return
        }
        resolvedAccountId = account.accountId
        setCloudAccount(account)
        setUser({ id: account.accountId, username: account.accountLabel, email: account.accountLabel })
        setMachines(await cloudAccountAdapter.listMachines())
        setError(null)
      } catch (err) {
        setError(localizedAppError(err, t))
        const account = await cloudAccountAdapter.current().catch(() => null)
        if (!account) {
          setCloudAccount(null)
          setUser(null)
          setMachines([])
        }
      } finally {
        setLoading(false)
      }
      setAuthorizedMachineIds(readAuthorizedMachineIds(storage, resolvedAccountId, externalPairingAdapter))
      setAuthorizationExpiries(readAuthorizationExpiries(storage, externalPairingAdapter))
      return
    }
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
      setAuthorizedMachineIds(readAuthorizedMachineIds(storage, profile.id, externalPairingAdapter))
      setAuthorizationExpiries(readAuthorizationExpiries(storage, externalPairingAdapter))
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
      setError(localizedAppError(err, t))
    } finally {
      setLoading(false)
    }
  }, [accessToken, api, cloudAccountAdapter, externalPairingAdapter, storage, t])

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
    setAuthorizedMachineIds(readAuthorizedMachineIds(storage, user?.id, externalPairingAdapter))
    setAuthorizationExpiries(readAuthorizationExpiries(storage, externalPairingAdapter))
  }, [externalPairingAdapter, pairVersion, storage, user?.id])

  const completeCloudActivation = useCallback(async (expectedUserCode: string) => {
    if (!cloudAccountAdapter) return
    try {
      const account = await cloudAccountAdapter.awaitActivation()
      setCloudAccount(account)
      setCloudActivation((current) => current?.userCode === expectedUserCode ? null : current)
      setUser({ id: account.accountId, username: account.accountLabel, email: account.accountLabel })
      setView('home')
    } catch (err) {
      if (!isCancelledAppError(err)) setError(localizedAppError(err, t))
      setCloudActivation((current) => current?.userCode === expectedUserCode ? null : current)
    }
  }, [cloudAccountAdapter, t])

  const scanCloudActivation = useCallback(async () => {
    if (!cloudAccountAdapter || !scanPairingCode) return
    if (cloudAccount) {
      setError(t('errors.alreadySignedIn', { account: cloudAccount.accountLabel }))
      return
    }
    setLoading(true)
    setError(null)
    try {
      const payload = await scanPairingCode()
      if (!payload) return
      const activation = await cloudAccountAdapter.claimActivation(payload)
      setCloudActivation(activation)
      void completeCloudActivation(activation.userCode)
    } catch (err) {
      setError(localizedAppError(err, t))
    } finally {
      setLoading(false)
    }
  }, [cloudAccount, cloudAccountAdapter, completeCloudActivation, scanPairingCode, t])

  const cancelCloudActivation = useCallback(async () => {
    if (!cloudAccountAdapter) return
    await cloudAccountAdapter.cancelActivation()
    setCloudActivation(null)
    setError(null)
  }, [cloudAccountAdapter])

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
      setError(localizedAppError(err, t))
    } finally {
      setLoading(false)
    }
  }, [api, login, password, storage, t])

  const signOut = useCallback(async () => {
    if (cloudAccountAdapter) await cloudAccountAdapter.logout()
    storage?.removeItem(storageKeys.accessToken)
    const signedOutMachines = pruneMachinesForSignOut(storage)
    setAccessToken('')
    setCloudAccount(null)
    setUser(null)
    setMachines([])
    setLocalMachines(signedOutMachines)
    setAuthorizedMachineIds(readAuthorizedMachineIds(storage, undefined, externalPairingAdapter))
    setAuthorizationExpiries(readAuthorizationExpiries(storage, externalPairingAdapter))
    setPairVersion((current) => current + 1)
    setSelectedMachineId(null)
    setError(null)
    setView('settings')
  }, [cloudAccountAdapter, externalPairingAdapter, storage])

  const updateTerminalSettings = useCallback((patch: Partial<TerminalSettings>) => {
    setTerminalSettings((current) => writeTerminalSettings({ ...current, ...patch }, storage))
  }, [storage])

  const openAddLocalSheet = useCallback(() => {
    hapticImpact()
    setSelectedMachineId(null)
    setPairIntent('add-local')
    setManualScanValue('')
	setSharePreview(null)
    setError(null)
    setScanOpen(true)
  }, [])

  const openPairSheet = useCallback((machineId: string) => {
    hapticImpact()
    setSelectedMachineId(machineId)
    setPairIntent('authorize-machine')
    setManualScanValue('')
	setSharePreview(null)
    setError(null)
    setScanOpen(true)
  }, [])

  const openMachinePairSheet = useCallback((machine: DisplayMachine) => {
    openPairSheet(machine.id)
  }, [openPairSheet])

  const selectMachine = useCallback((machine: DisplayMachine) => {
    hapticImpact()
    setSelectedMachineId(machine.id)
    if (!authorizedMachineIds.has(machine.id)) {
      openMachinePairSheet(machine)
      return
    }
    setView('machine')
    setError(null)
  }, [openMachinePairSheet, authorizedMachineIds])

  const storeImportedMachine = useCallback((external: ExternalPairingImportResult) => {
    if (!storage) throw new Error('Local storage is required before importing a Muxvia QR')
    if (selectedMachine && selectedMachine.id !== external.machine.id) {
      throw new Error(`This code belongs to ${external.machine.name}, not ${selectedMachine.name}`)
    }
    const store = createMachineStore({ storage })
    const timestamp = new Date().toISOString()
    const existing = store.getMachine(external.machine.id)
    const directoryMachine = machines.find((machine) => machine.id === external.machine.id)
    const directoryHubUrl = directoryMachine?.currentHubUrl ?? directoryMachine?.hubUrls[0]
    store.saveMachine({
      machineId: external.machine.id,
      name: selectedMachine?.name ?? external.machine.name,
      ...((selectedMachine?.hostname ?? external.machine.hostname) ? { hostname: selectedMachine?.hostname ?? external.machine.hostname } : {}),
      state: external.authorizationRequired ? 'offline' : 'online',
      terminalCount: existing?.terminalCount ?? 0,
      source: directoryMachine ? 'hub' : existing?.source ?? 'manual',
      accessClass: external.machine.accessClass ?? 'local',
      addresses: existing?.addresses ?? { local: [], lan: [], public: [] },
      endpoints: {
        ...(existing?.endpoints ?? {}),
        ...(directoryMachine?.controlUrl ? { webControl: directoryMachine.controlUrl } : {}),
        ...(directoryHubUrl ? { hub: directoryHubUrl } : {}),
      },
      addedAt: existing?.addedAt ?? timestamp,
      updatedAt: timestamp,
    })
    dropMachineRuntime(external.machine.id)
    setLocalMachines(store.listMachines())
    setSelectedMachineId(external.machine.id)
    setAuthorizedMachineIds(readAuthorizedMachineIds(storage, user?.id, externalPairingAdapter))
    setAuthorizationExpiries(readAuthorizationExpiries(storage, externalPairingAdapter))
    setPairVersion((current) => current + 1)
    setManualScanValue('')
    setSharePreview(null)
    const sshCredentials = external.sshCredentials?.filter((credential) => credential.authorizedKey.trim() !== '') ?? []
    setSSHCredentialNotice(sshCredentials.length > 0 ? sshCredentials : null)
    setScanOpen(sshCredentials.length > 0)
    setView(external.authorizationRequired ? 'home' : 'machine')
    hapticSuccess()
  }, [dropMachineRuntime, externalPairingAdapter, machines, selectedMachine, storage, user?.id])

  const pairScannedValue = useCallback(async (rawValue: string) => {
    const trimmedValue = rawValue.trim()
    const isCloudActivation = trimmedValue.startsWith('muxvia-cloud-activate:v1:') || /^MXA(?:-[0-9A-HJKMNP-TV-Z]{4}){5}-[0-9A-HJKMNP-TV-Z]{6}$/i.test(trimmedValue)
    if (isCloudActivation && cloudAccountAdapter) {
      // 账号 Session 是独立持久真值；已登录时禁止把新的 MXA 事务与旧 Session 混在一起。
      // 用户必须先显式退出，避免误以为未批准的新 flow 已经完成登录。
      if (cloudAccount) {
        hapticError()
        setError(t('errors.alreadySignedIn', { account: cloudAccount.accountLabel }))
        setScanOpen(false)
        setView('settings')
        return
      }
      setPairing(true)
      setScanFlowState('pairing')
      setError(null)
      try {
        const activation = await cloudAccountAdapter.claimActivation(rawValue.trim())
        setCloudActivation(activation)
        setManualScanValue('')
        setScanOpen(false)
        setView('settings')
        hapticSuccess()
        void completeCloudActivation(activation.userCode)
      } catch (err) {
        hapticError()
        setError(localizedAppError(err, t))
        setScanOpen(false)
        setView('settings')
      } finally {
        setPairing(false)
        setScanFlowState('idle')
      }
      return
    }
    if (!storage) {
      setError(t('errors.storageRequired'))
      return
    }
    setPairing(true)
    setScanFlowState('pairing')
    setError(null)
    try {
	  if (rawValue.trim().startsWith('muxvia://share?payload=')) {
		if (!externalPairingAdapter?.inspectShare) throw new Error('Endpoint share is unavailable in this client')
		const preview = await externalPairingAdapter.inspectShare(rawValue)
		setSharePreview(preview)
		setScanFlowState('idle')
		return
	  }
      const external = await externalPairingAdapter?.import(rawValue, selectedMachine?.id)
      if (external) {
		storeImportedMachine(external)
        return
      }
      throw new Error('Proto binding pairing adapter is required')
    } catch (err) {
      hapticError()
      console.warn('[muxvia:pairing] pair claim failed', err instanceof Error ? err.message : String(err))
      setError(localizedAppError(err, t))
    } finally {
      setPairing(false)
      setScanFlowState('idle')
    }
  }, [cloudAccount, cloudAccountAdapter, completeCloudActivation, externalPairingAdapter, selectedMachine?.id, storage, storeImportedMachine, t])

  const commitEndpointShare = useCallback(async () => {
	if (!sharePreview || !externalPairingAdapter?.commitShare) return
	setPairing(true)
	setError(null)
	try {
	  const imported = await externalPairingAdapter.commitShare(sharePreview.importToken)
	  storeImportedMachine(imported)
	} catch (err) {
	  hapticError()
	  setError(localizedAppError(err, t))
	} finally {
	  setPairing(false)
	}
  }, [externalPairingAdapter, sharePreview, storeImportedMachine, t])

  const importManualScan = useCallback(async () => {
    hapticImpact()
    await pairScannedValue(manualScanValue)
  }, [manualScanValue, pairScannedValue])

  const scanWithCamera = useCallback(async () => {
    if (!scanPairingCode) return
    if (cameraScanInFlightRef.current) return
    cameraScanInFlightRef.current = true
    hapticImpact()
    setCameraScanning(true)
    setScanFlowState('scanning')
    setError(null)
    try {
      const value = await scanPairingCode({
        onCancel: () => setScanOpen(false),
      })
      if (!value) return
      setManualScanValue(value)
      await pairScannedValue(value)
    } catch (err) {
      setError(isCameraUnavailableError(err) ? t('pairing.cameraUnavailable') : localizedAppError(err, t))
    } finally {
      cameraScanInFlightRef.current = false
      setCameraScanning(false)
      setScanFlowState((current) => current === 'scanning' ? 'idle' : current)
    }
  }, [pairScannedValue, scanPairingCode, t])

  const handleMachineNeedsReauthorization = useCallback((machineId: string) => {
    if (!storage) return
    void externalPairingAdapter?.forget(machineId)
    dropMachineRuntime(machineId)
    setAuthorizedMachineIds(readAuthorizedMachineIds(storage, user?.id, externalPairingAdapter))
    setAuthorizationExpiries(readAuthorizationExpiries(storage, externalPairingAdapter))
    setPairVersion((current) => current + 1)
    setSelectedMachineId(machineId)
    setPairIntent('authorize-machine')
    setManualScanValue('')
    setError(t('errors.pairAgain'))
    setScanOpen(true)
  }, [dropMachineRuntime, externalPairingAdapter, storage, t, user?.id])

  const forgetMachineAuthorization = useCallback((machine: WebControlMachine) => {
    if (!storage) return
    const store = createMachineStore({ storage })
    void externalPairingAdapter?.forget(machine.id)
    dropMachineRuntime(machine.id)
    const stillVisibleFromHub = machines.some((hubMachine) => hubMachine.id === machine.id)
    if (!stillVisibleFromHub) {
      store.forgetMachine(machine.id)
    }
    setLocalMachines(store.listMachines())
    setAuthorizedMachineIds(readAuthorizedMachineIds(storage, user?.id, externalPairingAdapter))
    setAuthorizationExpiries(readAuthorizationExpiries(storage, externalPairingAdapter))
    setPairVersion((current) => current + 1)
    setSelectedMachineId((current) => current === machine.id ? null : current)
    setView((current) => current === 'machine' && selectedMachineId === machine.id ? 'home' : current)
    setError(null)
  }, [dropMachineRuntime, externalPairingAdapter, machines, selectedMachineId, storage, user?.id])

  useEffect(() => addNativeBackHandler(() => {
    if (scanOpen) {
      setSSHCredentialNotice(null)
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
      className="muxvia-app-page flex h-full min-h-0 flex-col"
      data-testid="muxvia-web-control-remote"
      style={appThemeStyle}
    >
      {view === 'settings' ? (
        <SettingsView
          error={error}
          controlUrl={controlUrl}
          loading={loading}
          cloudActivation={cloudActivation}
          login={login}
          password={password}
          signedIn={signedIn}
          cloudAccount={cloudAccount}
          terminalSettings={terminalSettings}
          user={user}
          onBack={() => { hapticSelection(); setView('home') }}
          onLoginChange={setLogin}
          onPasswordChange={setPassword}
          onRefresh={() => { hapticImpact(); void refreshMachines(true) }}
          onSignIn={() => { hapticImpact(); void submitLogin() }}
          onScanCloudActivation={() => { hapticImpact(); void scanCloudActivation() }}
          onSubmitCloudActivationCode={(code) => { hapticImpact(); void pairScannedValue(code) }}
          onCancelCloudActivation={() => { hapticSelection(); void cancelCloudActivation() }}
          onSignOut={() => { hapticImpact(); void signOut() }}
          nativeCloudLogin={Boolean(cloudAccountAdapter)}
          canScanCloudActivation={Boolean(cloudAccountAdapter && scanPairingCode)}
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
          getConnectionStateSource={(machine) => authorizedMachineIds.has(machine.id) ? getExistingMachineRuntime(machine)?.listConnectionState : undefined}
          authorizedMachineIds={authorizedMachineIds}
          authorizationExpiries={authorizationExpiries}
          signedIn={signedIn}
          user={user}
          onAddLocalDevice={openAddLocalSheet}
          onOpenSettings={() => { hapticSelection(); setView('settings') }}
          onOpenTransferCenter={() => { hapticSelection(); setTransferCenterOpen(true) }}
          onForgetMachineAuthorization={forgetMachineAuthorization}
          onRefresh={() => { hapticImpact(); void refreshMachines() }}
          onSelectMachine={selectMachine}
          onSignIn={() => { hapticSelection(); setView('settings') }}
        />
      )}

      {scanOpen ? (
        <PairSheet
          manualScanValue={manualScanValue}
          pairError={error}
          scanFlowState={scanFlowState}
          pairing={pairing}
          sharePreview={sharePreview}
          sshCredentialNotice={sshCredentialNotice}
          cameraScanning={cameraScanning}
          pairIntent={pairIntent}
          selectedMachine={selectedMachine}
          canScanWithCamera={Boolean(scanPairingCode)}
		  onCommitShare={() => void commitEndpointShare()}
          onClose={() => { hapticSelection(); setSharePreview(null); setSSHCredentialNotice(null); setScanOpen(false) }}
          onImport={() => void importManualScan()}
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
  machine: DisplayMachine
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
    <section className="muxvia-app-page flex min-h-0 flex-1 flex-col animate-in fade-in slide-in-from-right-4 duration-200" data-testid="muxvia-machine-terminal-list">
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

function MachineRuntimeHeader({ machine, onBack }: { machine: DisplayMachine; onBack: () => void }) {
  return (
    <header className="muxvia-app-header flex min-h-14 shrink-0 items-center gap-3 border-b px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
      <button
        aria-label="Back to machines"
        className="muxvia-app-icon-button focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)]"
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
      <span className={`shrink-0 border px-2 py-1 text-[10px] font-semibold leading-4 ${machine.online ? 'border-emerald-200 text-emerald-700' : 'border-zinc-300 text-zinc-600'}`}>
        {machine.online ? 'Online' : 'Offline'}
      </span>
    </header>
  )
}

function MachineRuntimeErrorShell({
  machine,
  onBack,
}: {
  machine: DisplayMachine
  onBack: () => void
}) {
  return (
    <section className="muxvia-app-page flex min-h-0 flex-1 flex-col animate-in fade-in slide-in-from-right-4 duration-200" data-testid="muxvia-machine-terminal-list">
      <MachineRuntimeHeader machine={machine} onBack={onBack} />
    </section>
  )
}

function HomeView({
  fileTransfer,
  transferState,
  loading,
  machines,
  getConnectionStateSource,
  authorizedMachineIds,
  authorizationExpiries,
  signedIn,
  user,
  onAddLocalDevice,
  onForgetMachineAuthorization,
  onOpenSettings,
  onOpenTransferCenter,
  onRefresh,
  onSelectMachine,
  onSignIn,
}: {
  fileTransfer?: FileTransferContext | undefined
  transferState: { transfers: TransferInfo[]; hasActiveTransfers: boolean }
  loading: boolean
  machines: DisplayMachine[]
  getConnectionStateSource: (machine: DisplayMachine) => MachineRuntime['listConnectionState']
  authorizedMachineIds: Set<string>
  authorizationExpiries: Map<string, string>
  signedIn: boolean
  user: WebControlUser | null
  onAddLocalDevice: () => void
  onForgetMachineAuthorization: (machine: DisplayMachine) => void
  onOpenSettings: () => void
  onOpenTransferCenter: () => void
  onRefresh: () => void
  onSelectMachine: (machine: DisplayMachine) => void
  onSignIn: () => void
}) {
  const { t } = useTranslation()
  const [detailMachine, setDetailMachine] = useState<DisplayMachine | null>(null)
  return (
    <section className="muxvia-app-page flex min-h-0 flex-1 flex-col" data-testid="muxvia-app-home">
      <header className="muxvia-app-header flex min-h-14 shrink-0 items-center justify-between gap-3 border-b px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)] lg:h-16 lg:px-6 lg:py-0">
        <div className="flex min-w-0 items-center gap-3 lg:gap-5">
          <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center bg-[var(--muxvia-app-text)] font-mono text-[10px] font-semibold text-white lg:hidden">MV</span>
          <span aria-hidden="true" className="hidden text-base font-bold text-zinc-950 lg:inline">Muxvia</span>
          <div className="hidden h-5 w-px bg-zinc-200 lg:block" />
          <div className="min-w-0 lg:flex lg:items-center lg:gap-3">
            <h1 className="text-lg font-semibold leading-6 lg:text-sm">{t('machines.title')}</h1>
            <p className="truncate text-xs font-medium text-zinc-500 lg:border-l lg:border-zinc-200 lg:pl-3">
            {signedIn ? (user?.email ? t('machines.availableFor', { count: machines.length, account: user.email }) : t('machines.availableCount', { count: machines.length })) : t('machines.signInToSync')}
            </p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {signedIn ? (
            <button
              aria-label={t('machines.refresh')}
              className="muxvia-app-icon-button gap-2 px-2.5 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)]"
              type="button"
              onClick={onRefresh}
              disabled={loading}
            >
              <RefreshCw className={`h-5 w-5 ${loading ? 'animate-spin' : ''}`} />
              <span className="hidden text-xs font-semibold xl:inline">{t('common.refresh')}</span>
            </button>
          ) : null}
          <button
            aria-label={t('machines.add')}
            className="muxvia-app-primary-button min-w-11 gap-2 px-2.5 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)] lg:px-3"
            type="button"
            onClick={onAddLocalDevice}
          >
            <Plus className="h-5 w-5" />
            <span className="hidden text-xs font-semibold lg:inline">{t('machines.add')}</span>
          </button>
          {fileTransfer ? (
            <button
              aria-label={t('machines.transfers')}
              className="muxvia-app-icon-button relative focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)]"
              type="button"
              onClick={onOpenTransferCenter}
            >
              <Download className="h-5 w-5" />
              {transferState.hasActiveTransfers ? <span className="absolute right-2 top-2 h-2 w-2 rounded-full bg-emerald-500" /> : null}
            </button>
          ) : null}
          <button
            aria-label={t('machines.openSettings')}
            className="muxvia-app-icon-button focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)]"
            type="button"
            onClick={onOpenSettings}
          >
            <Settings className="h-5 w-5" />
          </button>
        </div>
      </header>

      {machines.length === 0 ? (
        <FirstUseState
          signedIn={signedIn}
          onAddLocalDevice={onAddLocalDevice}
          onSignIn={onSignIn}
        />
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto py-4 lg:px-8 lg:py-7">
          <div className="muxvia-app-panel mx-auto w-full max-w-7xl border-x-0 lg:overflow-visible lg:border-x">
            <div className="hidden grid-cols-[40px_minmax(180px,1.3fr)_minmax(160px,.8fr)_minmax(180px,1fr)_32px] items-center gap-4 border-b border-zinc-200 bg-zinc-50 px-4 py-2.5 text-[11px] font-semibold uppercase text-zinc-500 lg:grid">
              <span aria-hidden="true" />
              <span>{t('machines.columns.machine')}</span>
              <span>{t('machines.columns.access')}</span>
              <span>{t('machines.columns.connection')}</span>
              <span aria-hidden="true" />
            </div>
            <ul aria-label={t('machines.title')} className="divide-y divide-[var(--muxvia-app-line)]">
          {machines.map((machine) => (
            <li key={machine.id}>
              <MachineRow
                authorizationExpiresAt={authorizationExpiries.get(machine.id)}
                authorizationState={machineAuthorizationState(machine, authorizedMachineIds, authorizationExpiries)}
                machine={machine}
                connectionStateSource={getConnectionStateSource(machine)}
                onForgetMachineAuthorization={onForgetMachineAuthorization}
                onSelectMachine={onSelectMachine}
                onShowDetails={setDetailMachine}
              />
            </li>
          ))}
            </ul>
          </div>
        </div>
      )}
      {detailMachine ? <DisplayMachineDetailSheet machine={detailMachine} onClose={() => setDetailMachine(null)} /> : null}
    </section>
  )
}

function FirstUseState({
  signedIn,
  onAddLocalDevice,
  onSignIn,
}: {
  signedIn: boolean
  onAddLocalDevice: () => void
  onSignIn: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex min-h-0 flex-1 items-start justify-center overflow-y-auto px-4 py-10 md:items-center">
      <section className="muxvia-app-panel w-full max-w-md p-6" data-testid="muxvia-first-use">
        <div className="flex h-12 w-12 items-center justify-center border border-[var(--muxvia-app-line)] bg-[var(--muxvia-app-soft)] text-[var(--muxvia-app-accent)]">
          <Server className="h-6 w-6" />
        </div>
        <h2 className="mt-5 text-lg font-semibold text-zinc-950">{t('machines.emptyTitle')}</h2>
        <p className="mt-2 text-sm leading-6 text-zinc-600">{t(signedIn ? 'machines.emptySignedInCopy' : 'machines.emptyCopy')}</p>
        <div className="mt-6 grid gap-3">
          {!signedIn ? (
            <button className="muxvia-app-primary-button h-12 gap-2 px-4 text-sm font-semibold" type="button" onClick={onSignIn}>
              <LogIn className="h-4 w-4" />
              {t('machines.signInCloud')}
            </button>
          ) : null}
          <button className={`${signedIn ? 'muxvia-app-primary-button' : 'muxvia-app-secondary-button'} h-12 gap-2 px-4 text-sm font-semibold`} type="button" onClick={onAddLocalDevice}>
            <Plus className="h-4 w-4" />
            {t('machines.addLocal')}
          </button>
        </div>
      </section>
    </div>
  )
}

function SettingsView({
  canScanCloudActivation,
  cloudActivation,
  controlUrl,
  error,
  loading,
  login,
  password,
  signedIn,
  cloudAccount,
  terminalSettings,
  user,
  onBack,
  onCancelCloudActivation,
  onLoginChange,
  onPasswordChange,
  onRefresh,
  onScanCloudActivation,
  onSubmitCloudActivationCode,
  onSignIn,
  onSignOut,
  onTerminalSettingsChange,
  onExportDebugLogs,
  nativeCloudLogin,
}: {
  canScanCloudActivation: boolean
  cloudActivation: { userCode: string; expiresAtUnix: number } | null
  controlUrl: string
  error: string | null
  loading: boolean
  login: string
  password: string
  signedIn: boolean
  cloudAccount: CloudAccountSummary | null
  terminalSettings: TerminalSettings
  user: WebControlUser | null
  onBack: () => void
  onCancelCloudActivation: () => void
  onLoginChange: (value: string) => void
  onPasswordChange: (value: string) => void
  onRefresh: () => void
  onScanCloudActivation: () => void
  onSubmitCloudActivationCode: (code: string) => void
  onSignIn: () => void
  onSignOut: () => void
  onTerminalSettingsChange: (patch: Partial<TerminalSettings>) => void
  onExportDebugLogs?: (() => Promise<void>) | undefined
  nativeCloudLogin: boolean
}) {
  const { t, i18n } = useTranslation()
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
    <section className="muxvia-app-page flex min-h-0 flex-1 flex-col animate-in fade-in slide-in-from-bottom-4 duration-200" data-testid="muxvia-app-settings">
      <header className="muxvia-app-header flex min-h-14 shrink-0 items-center gap-3 border-b px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
        <button
          aria-label={t('common.backToMachines')}
          className="muxvia-app-icon-button focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)]"
          type="button"
          onClick={onBack}
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div className="min-w-0 flex-1">
          <h1 className="text-lg font-semibold leading-6 text-zinc-900">{t('common.settings')}</h1>
          <p className="truncate text-xs font-medium text-zinc-500">{signedIn ? user?.email ?? t('common.signedIn') : nativeCloudLogin ? t('settings.cloudSignIn') : t('settings.webSignIn')}</p>
        </div>
        {signedIn ? (
          <button
            aria-label={t('common.signOut')}
            className="muxvia-app-icon-button focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)]"
            title={t('common.signOut')}
            type="button"
            onClick={onSignOut}
          >
            <LogOut className="h-5 w-5" />
          </button>
        ) : null}
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-5 pb-[calc(env(safe-area-inset-bottom)+1.5rem)]">
        <div className="mx-auto flex w-full max-w-xl flex-col gap-6">
          {error ? (
            <p className="border border-red-200 bg-red-50 px-4 py-3 text-sm font-medium text-red-700">{error}</p>
          ) : null}

          <SettingsSection title={t('common.account')}>
            {signedIn ? (
              <>
                <SettingsRow label={t('common.signedIn')} value={user?.email ?? t('common.account')} />
                {cloudAccount ? (
                  <>
                    <SettingsRow label={t('settings.plan')} value={cloudAccount.planName} />
                    <SettingsRow label={t('settings.subscriptionStatus')} value={subscriptionStatusLabel(cloudAccount.subscriptionStatus, t)} />
                  </>
                ) : null}
                <div className="px-4 py-3">
                  <button
                    className="muxvia-app-secondary-button h-11 w-full gap-2 px-3 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-60"
                    type="button"
                    onClick={onRefresh}
                    disabled={loading}
                  >
                    <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                    {t('common.refresh')}
                  </button>
                </div>
              </>
            ) : (
              <>
                {!nativeCloudLogin && <div className="px-4 py-3">
                  <label className="block text-sm font-medium text-zinc-500">
                    {t('settings.emailOrUsername')}
                    <input
                      className="mt-2 h-11 w-full border border-[var(--muxvia-app-line)] bg-white px-3 text-sm text-zinc-900 outline-none focus:border-[var(--muxvia-app-accent)] focus:ring-2 focus:ring-blue-500/25"
                      value={login}
                      onChange={(event) => onLoginChange(event.target.value)}
                      autoComplete="username"
                    />
                  </label>
                </div>}
                {!nativeCloudLogin && <div className="border-t border-zinc-200 px-4 py-3">
                  <label className="block text-sm font-medium text-zinc-500">
                    {t('settings.password')}
                    <input
                      className="mt-2 h-11 w-full border border-[var(--muxvia-app-line)] bg-white px-3 text-sm text-zinc-900 outline-none focus:border-[var(--muxvia-app-accent)] focus:ring-2 focus:ring-blue-500/25"
                      value={password}
                      onChange={(event) => onPasswordChange(event.target.value)}
                      type="password"
                      autoComplete="current-password"
                    />
                  </label>
                </div>}
                {nativeCloudLogin ? (
                  <CloudActivationPanel
                    activation={cloudActivation}
                    canScan={canScanCloudActivation}
                    loading={loading}
                    onCancel={onCancelCloudActivation}
                    onScan={onScanCloudActivation}
                    onSubmitCode={onSubmitCloudActivationCode}
                  />
                ) : (
                  <div className="border-t border-zinc-200 px-4 py-3">
                    <button
                      className="muxvia-app-primary-button h-11 w-full gap-2 px-3 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-60"
                      type="button"
                      onClick={onSignIn}
                      disabled={loading}
                    >
                      <LogIn className="h-4 w-4" />
                      {t('common.signIn')}
                    </button>
                  </div>
                )}
              </>
            )}
          </SettingsSection>

          {!nativeCloudLogin ? (
            <SettingsSection title={t('settings.connection')}>
              <SettingsRow
                label={t('settings.webControl')}
                value={controlUrl || t('settings.builtInEndpoint')}
              />
            </SettingsSection>
          ) : null}

          <SettingsSection title={t('common.language')}>
            <SettingsRow label={t('settings.languageHint')}>
              <SettingsSelect
                ariaLabel={t('common.language')}
                value={normalizeMuxviaLanguage(i18n.resolvedLanguage)}
                onChange={(value) => { hapticSelection(); void i18n.changeLanguage(value) }}
              >
                {muxviaLanguages.map((language) => <option key={language.id} value={language.id}>{language.label}</option>)}
              </SettingsSelect>
            </SettingsRow>
          </SettingsSection>

          {onExportDebugLogs ? (
            <SettingsSection title={t('settings.diagnostics')}>
              <div className="px-4 py-3">
                <button
                  className="muxvia-app-primary-button h-11 w-full gap-2 px-3 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-60"
                  type="button"
                  onClick={() => {
                    hapticImpact()
                    void onExportDebugLogs()
                  }}
                >
                  <Download className="h-4 w-4" />
                  {t('settings.exportLogs')}
                </button>
              </div>
            </SettingsSection>
          ) : null}

          <SettingsSection title={t('settings.terminal')}>
            <SettingsRow label={t('settings.fontSize')}>
              <div className="inline-flex h-11 items-center overflow-hidden border border-[var(--muxvia-app-line)] bg-white">
                <button
                  aria-label={t('settings.decreaseFont')}
                  className="h-11 w-11 text-lg font-semibold text-zinc-700 hover:bg-zinc-50 active:bg-zinc-100"
                  type="button"
                  onClick={() => { hapticSelection(); onTerminalSettingsChange({ fontSize: Math.max(8, terminalSettings.fontSize - 1) }) }}
                >
                  -
                </button>
                <input
                  aria-label={t('settings.fontSize')}
                  className="h-11 w-12 border-x border-[var(--muxvia-app-line)] bg-zinc-50 px-1 text-center text-sm font-semibold text-zinc-900 outline-none focus:ring-2 focus:ring-blue-500/25"
                  inputMode="numeric"
                  max={32}
                  min={8}
                  type="number"
                  value={terminalSettings.fontSize}
                  onChange={handleNumberSetting('fontSize', 8, 32)}
                />
                <button
                  aria-label={t('settings.increaseFont')}
                  className="h-11 w-11 text-lg font-semibold text-zinc-700 hover:bg-zinc-50 active:bg-zinc-100"
                  type="button"
                  onClick={() => { hapticSelection(); onTerminalSettingsChange({ fontSize: Math.min(32, terminalSettings.fontSize + 1) }) }}
                >
                  +
                </button>
              </div>
            </SettingsRow>
            <SettingsRow label={t('settings.font')} stacked>
              <FontPicker
                value={terminalSettings.fontFamily}
                onChange={(value) => onTerminalSettingsChange({ fontFamily: value })}
              />
            </SettingsRow>
            <SettingsRow label={t('settings.theme')} stacked>
              <ThemePicker
                groups={themeGroups}
                value={terminalSettings.themeId}
                onChange={(value) => onTerminalSettingsChange({ themeId: value })}
              />
            </SettingsRow>
            <SettingsRow label={t('settings.renderer')}>
              <SettingsSelect
                ariaLabel={t('settings.renderer')}
                value={terminalSettings.renderer}
                onChange={(value) => { hapticSelection(); onTerminalSettingsChange({ renderer: value as TerminalRenderer }) }}
              >
                <option value="auto">{t('settings.auto')}</option>
                <option value="webgl">WebGL</option>
                <option value="canvas">Canvas</option>
                <option value="dom">DOM</option>
              </SettingsSelect>
            </SettingsRow>
            <SettingsRow label={t('settings.keyboard')}>
              <SettingsSelect
                ariaLabel={t('settings.keyboard')}
                value={terminalSettings.keyboardMode}
                onChange={(value) => { hapticSelection(); onTerminalSettingsChange({ keyboardMode: value as TerminalKeyboardMode }) }}
              >
                <option value="auto">{t('settings.auto')}</option>
                <option value="resize">{t('settings.resize')}</option>
                <option value="shift">{t('settings.shiftUp')}</option>
              </SettingsSelect>
            </SettingsRow>
            <SettingsRow label={t('settings.scrollback')}>
              <input
                aria-label={t('settings.scrollback')}
                className="h-11 w-28 border border-[var(--muxvia-app-line)] bg-white px-3 text-right text-sm font-semibold text-zinc-900 outline-none focus:border-[var(--muxvia-app-accent)] focus:ring-2 focus:ring-blue-500/25"
                inputMode="numeric"
                max={50000}
                min={500}
                step={500}
                type="number"
                value={terminalSettings.scrollback}
                onChange={handleNumberSetting('scrollback', 500, 50000)}
              />
            </SettingsRow>
            <SettingsRow label={t('settings.prefetch')}>
              <input
                aria-label={t('settings.prefetch')}
                className="h-11 w-28 border border-[var(--muxvia-app-line)] bg-white px-3 text-right text-sm font-semibold text-zinc-900 outline-none focus:border-[var(--muxvia-app-accent)] focus:ring-2 focus:ring-blue-500/25"
                inputMode="numeric"
                max={1000}
                min={0}
                step={10}
                type="number"
                value={terminalSettings.scrollbackPrefetchThresholdRows}
                onChange={handleNumberSetting('scrollbackPrefetchThresholdRows', 0, 1000)}
              />
            </SettingsRow>
            <SettingsRow label={t('settings.cursorBlink')}>
              <Switch
                ariaLabel={t('settings.cursorBlink')}
                checked={terminalSettings.cursorBlink}
                onChange={(checked) => onTerminalSettingsChange({ cursorBlink: checked })}
              />
            </SettingsRow>
          </SettingsSection>

        </div>
      </div>
    </section>
  )
}

function subscriptionStatusLabel(status: string, t: TFunction): string {
  switch (status.trim().toUpperCase()) {
    case 'SUBSCRIPTION_STATUS_ACTIVE': return t('settings.subscriptionActive')
    case 'SUBSCRIPTION_STATUS_TRIALING': return t('settings.subscriptionTrial')
    case 'SUBSCRIPTION_STATUS_PAST_DUE': return t('settings.subscriptionPastDue')
    case 'SUBSCRIPTION_STATUS_CANCEL_AT_PERIOD_END': return t('settings.subscriptionCanceling')
    case 'SUBSCRIPTION_STATUS_CANCELED': return t('settings.subscriptionCanceled')
    case 'SUBSCRIPTION_STATUS_SUSPENDED': return t('settings.subscriptionSuspended')
    case 'SUBSCRIPTION_STATUS_EXPIRED': return t('settings.subscriptionExpired')
    case 'SUBSCRIPTION_STATUS_GRACE': return t('settings.subscriptionGrace')
    default: return t('settings.subscriptionPending')
  }
}

function CloudActivationPanel({
  activation,
  canScan,
  loading,
  onCancel,
  onScan,
  onSubmitCode,
}: {
  activation: { userCode: string; expiresAtUnix: number } | null
  canScan: boolean
  loading: boolean
  onCancel: () => void
  onScan: () => void
  onSubmitCode: (code: string) => void
}) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => Date.now())
  const [code, setCode] = useState('')
  useEffect(() => {
    if (!activation) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [activation])
  const remainingSeconds = activation ? Math.max(0, Math.ceil(activation.expiresAtUnix - now / 1000)) : 0
  const remaining = `${Math.floor(remainingSeconds / 60)}:${String(remainingSeconds % 60).padStart(2, '0')}`

  if (activation) {
    return (
      <div className="border-t border-zinc-200 px-4 py-4">
        <div aria-live="polite" className="border border-[var(--muxvia-app-line)] bg-white px-4 py-5 text-center">
          <p className="text-sm font-medium text-zinc-500">{t('activation.enterOnComputer')}</p>
          <strong className="mt-3 block font-mono text-2xl font-medium text-zinc-950">{activation.userCode}</strong>
          <p className="mt-3 font-mono text-sm text-zinc-500">{t('activation.waiting', { remaining })}</p>
        </div>
        <button
          className="muxvia-app-secondary-button mt-3 h-12 w-full gap-2 px-3 text-sm font-semibold"
          type="button"
          onClick={onCancel}
        >
          <X className="h-4 w-4" />
          {t('activation.cancel')}
        </button>
      </div>
    )
  }

  return (
    <div className="border-t border-zinc-200 px-4 py-4">
      <p className="mb-4 text-sm leading-6 text-zinc-500">{t('activation.intro')}</p>
      {canScan ? (
        <button
          className="muxvia-app-primary-button h-12 w-full gap-2 px-3 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          onClick={onScan}
          disabled={loading}
        >
          <QrCode className="h-4 w-4" />
          {t('activation.scanWeb')}
        </button>
      ) : <p className="text-sm text-zinc-500">{t('activation.scanUnavailable')}</p>}
      <label className="mt-4 block text-xs font-semibold text-zinc-500">
        {t('activation.loginCode')}
        <input
          className="mt-2 h-11 w-full border border-[var(--muxvia-app-line)] bg-white px-3 font-mono text-sm uppercase text-zinc-950 outline-none focus:border-[var(--muxvia-app-accent)] focus:ring-2 focus:ring-blue-500/25"
          value={code}
          onChange={(event) => setCode(event.target.value.toUpperCase())}
          placeholder="MXA-XXXX-XXXX-XXXX-XXXX-XXXX-XXXXXX"
          autoCapitalize="characters"
          autoCorrect="off"
          spellCheck={false}
        />
      </label>
      <button
        className="muxvia-app-secondary-button mt-3 h-11 w-full gap-2 px-3 text-sm font-semibold disabled:opacity-50"
        type="button"
        onClick={() => onSubmitCode(code.trim())}
        disabled={loading || code.trim() === ''}
      >
        <Keyboard className="h-4 w-4" />
        {t('activation.useCode')}
      </button>
    </div>
  )
}

function SettingsSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h2 className="mb-2 px-1 text-[10px] font-semibold uppercase text-[var(--muxvia-app-muted)]">{title}</h2>
      <div className="muxvia-app-panel overflow-hidden">
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
      <div className="flex min-h-12 flex-col items-stretch gap-3 border-b border-[var(--muxvia-app-line)] px-4 py-3 last:border-b-0">
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
    <div className="flex min-h-12 items-center justify-between gap-4 border-b border-[var(--muxvia-app-line)] px-4 py-2 last:border-b-0">
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
      className="h-11 max-w-[54vw] border border-[var(--muxvia-app-line)] bg-white px-3 text-right text-sm font-semibold text-zinc-900 outline-none focus:border-[var(--muxvia-app-accent)] focus:ring-2 focus:ring-blue-500/25 sm:max-w-xs"
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
      className={`min-w-0 border p-3 text-left transition-colors duration-200 ${
        selected
          ? 'border-[var(--muxvia-app-accent)] bg-blue-50'
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
        <span className={`ml-auto h-2 w-2 shrink-0 ${selected ? 'bg-[var(--muxvia-app-accent)]' : 'bg-zinc-200'}`} />
      </div>
      <div className="mt-2 bg-zinc-950 px-2 py-2 text-[12px] leading-5 text-zinc-100">
        <div className="truncate">$ muxvia --font</div>
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
      className="min-w-0 border p-2 text-left transition-colors duration-200"
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
      <div className="p-2" style={{ backgroundColor: ui.terminalBackground }}>
        <div className="mb-2 flex gap-1">
          {colors.map((color) => (
            <span key={color} className="h-2.5 flex-1" style={{ backgroundColor: color }} />
          ))}
        </div>
        <div className="space-y-1">
          <div className="h-1.5 w-4/5" style={{ backgroundColor: ui.terminalForeground, opacity: 0.72 }} />
          <div className="flex items-center gap-1">
            <div className="h-1.5 w-1/2" style={{ backgroundColor: ui.terminalForeground, opacity: 0.42 }} />
            <div className="h-2.5 w-1" style={{ backgroundColor: ui.terminalCursor }} />
          </div>
        </div>
      </div>
      <div className="mt-2 flex min-w-0 items-center gap-1.5">
        <span className="truncate text-xs font-semibold" style={{ color: ui.text }}>{option.label}</span>
        <span className="ml-auto h-2 w-2 shrink-0" style={{ backgroundColor: selected ? ui.accent : ui.borderSubtle }} />
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
      className={`relative h-8 w-12 rounded-full transition-colors ${checked ? 'bg-[var(--muxvia-accent)]' : 'bg-[var(--muxvia-border)]'}`}
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
  manualScanValue,
  pairError,
  scanFlowState,
  pairIntent,
  pairing,
  sharePreview,
  sshCredentialNotice,
  selectedMachine,
  onClose,
  onCommitShare,
  onImport,
  onManualScanValueChange,
  onScanWithCamera,
}: {
  cameraScanning: boolean
  canScanWithCamera: boolean
  manualScanValue: string
  pairError: string | null
  scanFlowState: ScanFlowState
  pairIntent: PairIntent
  pairing: boolean
  sharePreview: EndpointSharePreviewView | null
  sshCredentialNotice: NonNullable<ExternalPairingImportResult['sshCredentials']> | null
  selectedMachine: WebControlMachine | null
  onClose: () => void
  onCommitShare: () => void
  onImport: () => void
  onManualScanValueChange: (value: string) => void
  onScanWithCamera: () => void
}) {
  const { t } = useTranslation()
  const title = sshCredentialNotice ? t('pairing.sshReady') : sharePreview ? t('pairing.importConfig') : pairIntent === 'add-local' ? t('pairing.addLocal') : t('pairing.authorize')
  const primaryLabel = pairIntent === 'add-local' ? t('pairing.add') : t('pairing.pair')
  const statusMessage = scanFlowState === 'pairing'
    ? t('pairing.scanned')
    : scanFlowState === 'scanning'
      ? t('pairing.scanning')
      : null
  return (
    <div className="muxvia-app-page fixed inset-0 z-50" role="dialog" aria-modal="true">
      <section className="flex h-full min-h-0 flex-col bg-white" data-testid="muxvia-pair-sheet">
        <header className="muxvia-app-header flex min-h-14 shrink-0 items-center justify-between gap-3 border-b px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
          <div className="flex min-w-0 items-center gap-2">
            <QrCode className="h-5 w-5 shrink-0 text-[var(--muxvia-accent)]" />
            <h2 className="truncate text-base font-semibold">{title}</h2>
          </div>
          <button
            aria-label={t('pairing.close')}
            className="muxvia-app-icon-button border-transparent bg-transparent focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)]"
            type="button"
            onClick={onClose}
          >
            <X className="h-5 w-5" />
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-5 pb-[calc(env(safe-area-inset-bottom)+1.5rem)]">
          <div className="mx-auto w-full max-w-md">
            {sshCredentialNotice ? (
              <div className="muxvia-app-panel bg-[var(--muxvia-app-soft)] px-3 py-3">
                {sshCredentialNotice.map((credential) => (
                  <div className="mb-4 last:mb-0" key={credential.routeId}>
                    <div className="flex items-center justify-between gap-3">
                      <span className="text-sm font-semibold text-zinc-950">{credential.routeId}</span>
                      <span className="font-mono text-xs text-zinc-500">{credential.fingerprint}</span>
                    </div>
                    <textarea
                      aria-label={`SSH authorized key for ${credential.routeId}`}
                      className="mt-3 h-32 w-full resize-none border border-[var(--muxvia-app-line)] bg-white p-2 font-mono text-xs leading-5 text-zinc-950 outline-none"
                      value={credential.authorizedKey}
                      readOnly
                    />
                    <button
                      className="muxvia-app-secondary-button mt-3 h-11 w-full gap-2 px-3 text-sm font-semibold"
                      type="button"
                      onClick={() => { void navigator.clipboard.writeText(credential.authorizedKey); hapticSuccess() }}
                    >
                      <Copy className="h-4 w-4" />
                      {t('pairing.copyKey')}
                    </button>
                  </div>
                ))}
              </div>
            ) : sharePreview ? (
              <div className="muxvia-app-panel bg-[var(--muxvia-app-soft)] px-3 py-3">
                <div className="text-sm font-semibold text-zinc-950">{sharePreview.label || sharePreview.endpointId}</div>
                <div className="mt-1 break-all font-mono text-xs text-zinc-500">{sharePreview.deviceFingerprint}</div>
                <div className="mt-3 space-y-1">
                  {sharePreview.routes.map((route) => (
                    <div className="flex items-center justify-between gap-3 text-xs" key={route.id}>
                      <span className="truncate font-medium text-zinc-700">{route.id} · {route.kind}</span>
                      <span className="shrink-0 font-semibold uppercase text-[var(--muxvia-app-accent)]">{route.action}</span>
                    </div>
                  ))}
                </div>
                <div className="mt-3 text-xs leading-5 text-zinc-500">
                  {sharePreview.connectModeChanged ? `${t('pairing.connectModeChanged')} ` : ''}
                  {sharePreview.selectionPolicyChanged ? `${t('pairing.selectionPolicyChanged')} ` : ''}
                  {t('pairing.credentialsStayLocal')}
                </div>
                <button
                  className="muxvia-app-primary-button mt-4 h-11 w-full gap-2 px-3 text-sm font-semibold disabled:opacity-50"
                  type="button"
                  onClick={onCommitShare}
                  disabled={pairing}
                >
                  {pairing ? <span className="muxvia-square-spinner" aria-hidden="true" /> : <Download className="h-4 w-4" />}
                  {t('pairing.importConfig')}
                </button>
              </div>
            ) : null}

            {!sharePreview && !sshCredentialNotice ? <>
            {selectedMachine ? (
              <div className="muxvia-app-panel bg-[var(--muxvia-app-soft)] px-3 py-2">
                <div className="truncate text-sm font-semibold text-zinc-950">{selectedMachine.name}</div>
                <div className="mt-0.5 truncate text-xs font-medium text-zinc-500">{selectedMachine.hostname || selectedMachine.id}</div>
              </div>
            ) : null}

            {canScanWithCamera ? (
              <button
                className="muxvia-app-primary-button mt-4 h-12 w-full gap-2 px-3 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-60"
                type="button"
                onClick={onScanWithCamera}
                disabled={pairing || cameraScanning}
              >
                {cameraScanning || pairing ? <span className="muxvia-square-spinner" aria-hidden="true" /> : <Camera className="h-4 w-4" />}
                {pairing ? t('pairing.pairing') : cameraScanning ? t('pairing.scanProgress') : t('pairing.scanCamera')}
              </button>
            ) : (
              <p className="mt-4 border border-[var(--muxvia-app-line)] bg-[var(--muxvia-app-soft)] px-3 py-2 text-sm text-zinc-600">{t('pairing.cameraUnavailable')}</p>
            )}

            <div className="muxvia-app-panel mt-4 bg-[var(--muxvia-app-soft)] px-3 py-3">
              <label className="block text-xs font-semibold text-zinc-500">
                {t('pairing.content')}
                <textarea
                  className="mt-2 h-28 w-full resize-none border border-[var(--muxvia-app-line)] bg-white p-3 font-mono text-sm leading-5 text-zinc-950 placeholder:text-zinc-400 outline-none focus:border-[var(--muxvia-app-accent)] focus:ring-2 focus:ring-blue-500/25"
                  value={manualScanValue}
                  onChange={(event) => onManualScanValueChange(event.target.value)}
                  placeholder={t('pairing.manualPlaceholder')}
                  autoCapitalize="characters"
                  autoCorrect="off"
                  spellCheck={false}
                />
              </label>
              <button
                className="muxvia-app-secondary-button mt-3 h-11 w-full gap-2 px-3 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50"
                type="button"
                onClick={onImport}
                disabled={pairing || cameraScanning || manualScanValue.trim() === ''}
              >
                {pairing ? <span className="muxvia-square-spinner" aria-hidden="true" /> : <ShieldCheck className="h-4 w-4" />}
                {primaryLabel}
              </button>
            </div>

            {statusMessage ? (
              <p className="mt-3 border border-blue-500/20 bg-blue-500/10 px-3 py-2 text-sm font-medium text-blue-700">{statusMessage}</p>
            ) : null}

            {pairError ? (
              <p className="mt-3 border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm font-medium text-red-500">{pairError}</p>
            ) : null}
				</> : !sshCredentialNotice && pairError ? (
			  <p className="mt-3 border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm font-medium text-red-500">{pairError}</p>
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
  connectionStateSource,
  onForgetMachineAuthorization,
  onSelectMachine,
  onShowDetails,
}: {
  authorizationExpiresAt?: string | undefined
  authorizationState: MachineAuthorizationState
  machine: DisplayMachine
  connectionStateSource?: MachineRuntime['listConnectionState']
  onForgetMachineAuthorization: (machine: DisplayMachine) => void
  onSelectMachine: (machine: DisplayMachine) => void
  onShowDetails: (machine: DisplayMachine) => void
}) {
  const { t } = useTranslation()
  const [menuOpen, setMenuOpen] = useState(false)
  const connection = useSyncExternalStore(
    connectionStateSource?.subscribe ?? noopSubscribe,
    connectionStateSource?.getSnapshot ?? getEmptyMachineConnectionSnapshot,
    connectionStateSource?.getSnapshot ?? getEmptyMachineConnectionSnapshot,
  )
  const actionLabel = authorizationState === 'ready' ? t('machines.open') : t('machines.pair')
  const subtitle = machine.hostname || t('machines.daemonHost')
  const card = machineCardProjection(machine, authorizationState, authorizationExpiresAt, connection, t)
  const DeviceIcon = machine.accessClass === 'cloud' ? Cloud : LaptopMinimal
  const canForget = Boolean(authorizationExpiresAt || authorizationState === 'ready')
  return (
    <div className="relative bg-white">
      <button
        aria-label={`${actionLabel} ${machine.name}`}
        className="relative grid min-h-[108px] min-w-0 w-full grid-cols-[40px_minmax(0,1fr)_20px] grid-rows-[auto_auto_auto] gap-x-3 gap-y-1 px-4 py-3 text-left transition-colors duration-200 hover:bg-zinc-50 active:bg-[var(--muxvia-app-soft)] focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--muxvia-app-accent)] lg:min-h-[72px] lg:grid-cols-[40px_minmax(180px,1.3fr)_minmax(160px,.8fr)_minmax(180px,1fr)_32px] lg:grid-rows-1 lg:items-center lg:gap-4 lg:px-4 lg:py-2.5"
        type="button"
        onClick={() => onSelectMachine(machine)}
      >
        <div className="relative col-start-1 row-start-1 flex h-10 w-10 items-center justify-center border border-[var(--muxvia-app-line)] bg-[var(--muxvia-app-soft)] text-zinc-700 lg:col-start-1">
          <DeviceIcon className="h-5 w-5" />
          <span className={`absolute bottom-0 right-0 h-2.5 w-2.5 border-2 border-white ${
            card.tone === 'online' ? 'bg-emerald-500' : card.tone === 'active' ? 'bg-blue-500' : card.tone === 'warning' ? 'bg-amber-500' : 'bg-zinc-400'
          }`} />
        </div>
        <div className="col-start-2 row-start-1 min-w-0 self-start pr-24 lg:col-start-2 lg:self-center lg:pr-0">
          <div className="truncate text-[15px] font-semibold leading-5 text-zinc-950">{machine.name}</div>
          <div className="mt-0.5 truncate text-xs font-medium text-zinc-500">{subtitle}</div>
        </div>
          <div className="col-start-2 row-start-2 flex min-w-0 items-center gap-2 text-[11px] font-semibold text-zinc-600 lg:col-start-3 lg:row-start-1">
            <AccessClassLabel accessClass={machine.accessClass} />
            {machine.accessClass === 'local_cloud' ? <ReachabilityLabel reachability={machine.reachability} /> : null}
          </div>
          <div className="contents lg:col-start-4 lg:row-start-1 lg:block lg:min-w-0">
            <span className={`absolute right-12 top-3 shrink-0 text-[11px] font-semibold lg:static lg:block ${card.statusClass}`}>{card.status}</span>
            <div className="col-start-2 row-start-3 truncate text-[12px] font-medium text-zinc-600 lg:mt-1">
            {card.detail}
            </div>
          </div>
        <ChevronRight className="col-start-3 row-span-3 row-start-1 h-4 w-4 shrink-0 self-center text-zinc-400 lg:hidden" />
      </button>
      <div className="absolute right-9 top-2.5 z-10 lg:right-3 lg:top-1/2 lg:-translate-y-1/2">
          <button
            aria-label={t('machines.more', { name: machine.name })}
            className="inline-flex h-11 w-11 items-center justify-center text-zinc-500 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)] lg:h-10 lg:w-10"
            type="button"
            onClick={() => setMenuOpen((open) => !open)}
          >
            <MoreHorizontal className="h-4 w-4" />
          </button>
          {menuOpen ? (
            <div className="absolute right-0 top-11 min-w-44 border border-[var(--muxvia-app-line)] bg-white p-1 shadow-lg">
              <button className="flex h-11 w-full items-center gap-2 px-2.5 text-left text-xs font-semibold text-zinc-700 hover:bg-zinc-50" type="button" onClick={() => { setMenuOpen(false); onShowDetails(machine) }}>
                <Info className="h-4 w-4" />
                {t('machines.details')}
              </button>
              {canForget ? (
                <button
                  aria-label={t('machines.removeAuthorization')}
                  className="flex h-11 w-full items-center gap-2 px-2.5 text-left text-xs font-semibold text-red-600 hover:bg-red-50"
                  type="button"
                  onClick={() => onForgetMachineAuthorization(machine)}
                >
                  <Trash2 className="h-4 w-4" />
                  {t('machines.removeAuthorization')}
                </button>
              ) : null}
            </div>
          ) : null}
        </div>
    </div>
  )
}

function DisplayMachineDetailSheet({ machine, onClose }: { machine: DisplayMachine; onClose: () => void }) {
  const { t } = useTranslation()
  const source = machine.accessClass === 'cloud'
    ? t('machines.source.hub')
    : machine.accessClass === 'local_cloud'
      ? t('machines.source.localCloud')
      : t('machines.source.local')
  const fields = [
    [t('machines.fields.id'), machine.id],
    [t('machines.fields.hostname'), machine.hostname || '-'],
    [t('machines.fields.platform'), machine.osInfo || '-'],
    [t('machines.fields.source'), source],
    [t('machines.fields.hub'), machine.hubId || '-'],
    [t('machines.fields.lastOnline'), machine.lastSeen ? formatAuthorizationExpiry(machine.lastSeen) : '-'],
  ] as const
  return (
    <div className="fixed inset-0 z-40 flex items-end bg-black/40 md:items-center md:justify-center" role="dialog" aria-modal="true" aria-labelledby="muxvia-device-details-title" onClick={onClose}>
      <section className="max-h-[85dvh] w-full overflow-hidden border-t border-[var(--muxvia-app-line)] bg-white md:max-w-md md:border" onClick={(event) => event.stopPropagation()}>
        <header className="flex min-h-16 items-center justify-between gap-3 border-b border-[var(--muxvia-app-line)] px-4">
          <div className="min-w-0">
            <h2 className="truncate text-base font-semibold text-zinc-950" id="muxvia-device-details-title">{machine.name}</h2>
            <p className="mt-0.5 text-xs text-zinc-500">{t('machines.details')}</p>
          </div>
          <button aria-label={t('machines.closeDetails')} className="muxvia-app-icon-button border-transparent bg-transparent" type="button" onClick={onClose}><X className="h-5 w-5" /></button>
        </header>
        <dl className="max-h-[calc(85dvh-4rem)] overflow-y-auto p-4">
          {fields.map(([label, value]) => (
            <div className="border-b border-[var(--muxvia-app-line)] py-3 last:border-b-0" key={label}>
              <dt className="text-xs font-semibold text-zinc-500">{label}</dt>
              <dd className="mt-1 break-all font-mono text-sm text-zinc-950">{value}</dd>
            </div>
          ))}
        </dl>
      </section>
    </div>
  )
}

function authorizationAvailabilityText(
  machine: DisplayMachine,
  authorizationState: MachineAuthorizationState,
  expiresAt: string | undefined,
  t: TFunction,
): string {
  if (authorizationState === 'ready') {
    return expiresAt ? t('machines.authorizedUntil', { time: formatAuthorizationExpiry(expiresAt) }) : machine.online ? t('machines.tapToConnect') : t('machines.authorized')
  }
  if (authorizationState === 'expired') {
    return expiresAt ? t('machines.authorizationExpiredAt', { time: formatAuthorizationExpiry(expiresAt) }) : t('machines.authorizationExpired')
  }
  return t('machines.pairToOpen')
}

interface MachineCardProjection {
  status: string
  statusClass: string
  tone: 'online' | 'active' | 'warning' | 'offline'
  detail: string
}

function machineCardProjection(
  machine: DisplayMachine,
  authorizationState: MachineAuthorizationState,
  expiresAt: string | undefined,
  connection: MachineConnectionSnapshot,
  t: TFunction,
): MachineCardProjection {
  if (authorizationState !== 'ready') {
    return {
      status: authorizationState === 'expired' ? t('machines.authorizationExpired') : t('machines.actionRequired'),
      statusClass: 'text-amber-700',
      tone: 'warning',
      detail: authorizationAvailabilityText(machine, authorizationState, expiresAt, t),
    }
  }
  if (connection.phase === 'connected') {
    const path = connectionPathDetail(connection, t)
    return {
      status: t('machines.connected'),
      statusClass: 'text-emerald-700',
      tone: 'online',
      detail: joinCardDetail(terminalCountLabel(machine.terminalCount, t), path),
    }
  }
  if (connection.phase === 'failed') {
    return {
      status: t('machines.failed'),
      statusClass: 'text-red-600',
      tone: 'warning',
      detail: connection.error || connection.statusText || t('machines.connectionFailed'),
    }
  }
  if (connection.phase !== 'idle') {
    return {
      status: connectionPhaseShortLabel(connection.phase, t),
      statusClass: 'text-blue-700',
      tone: 'active',
      detail: connection.statusText,
    }
  }
  if (machine.online) {
    return {
      status: t('machines.available'),
      statusClass: 'text-emerald-700',
      tone: 'online',
      detail: joinCardDetail(terminalCountLabel(machine.terminalCount, t), availablePathLabel(machine, t)),
    }
  }
  return {
    status: t('machines.offline'),
    statusClass: 'text-zinc-500',
    tone: 'offline',
    detail: machine.lastSeen ? t('machines.lastOnline', { time: formatAuthorizationExpiry(machine.lastSeen) }) : t('machines.notReachable'),
  }
}

function AccessClassLabel({ accessClass }: { accessClass: MachineAccessClass }) {
  const { t } = useTranslation()
  if (accessClass === 'local_cloud') {
    return <span className="inline-flex items-center gap-1.5"><Wifi className="h-3.5 w-3.5" />{t('machines.source.localCloud')}</span>
  }
  if (accessClass === 'cloud') {
    return <span className="inline-flex items-center gap-1.5"><Cloud className="h-3.5 w-3.5" />{t('machines.source.hub')}</span>
  }
  return <span className="inline-flex items-center gap-1.5"><Wifi className="h-3.5 w-3.5" />{t('machines.source.local')}</span>
}

function ReachabilityLabel({ reachability }: { reachability?: MachineReachabilityView | undefined }) {
  const { t } = useTranslation()
  const local = reachability?.localChecked ? (reachability.localOnline ? t('machines.reachability.localOnline') : t('machines.reachability.localOffline')) : t('machines.reachability.localChecking')
  const cloud = reachability?.hubOnline ? t('machines.reachability.cloudOnline') : t('machines.reachability.cloudOffline')
  return <span className="truncate font-medium text-zinc-400">{local} · {cloud}</span>
}

function connectionPathDetail(connection: MachineConnectionSnapshot, t: TFunction): string {
  const info = connection.connectionInfo
  const rtt = info?.rtt !== undefined ? `${Math.round(info.rtt)} ms` : ''
  let path = t('machines.connected')
  if (info?.path === 'local') path = t('machines.path.local')
  else if (info?.observedPath === 'single_relay' || info?.relayInUse) path = t('machines.path.relay')
  else if (info?.observedPath === 'direct') path = t('machines.path.direct')
  else if (info?.path === 'hub') path = t('machines.path.cloud')
  return joinCardDetail(path, rtt)
}

function availablePathLabel(machine: DisplayMachine, t: TFunction): string {
  if (machine.accessClass === 'local_cloud') {
    if (machine.reachability?.localOnline) return t('machines.path.localAvailable')
    if (machine.reachability?.hubOnline) return t('machines.path.cloudAvailable')
  }
  if (machine.accessClass === 'cloud') return t('machines.path.cloudAvailable')
  return t('machines.path.localAvailable')
}

function userFacingMachineName(machineId: string, name: string, hostname: string | undefined, t: TFunction): string {
  const candidate = name.trim()
  if (candidate !== '' && candidate !== machineId) return candidate
  const host = hostname?.trim()
  return host || t('machines.daemonHost')
}

function terminalCountLabel(count: number | undefined, t: TFunction): string {
  if (count === undefined) return ''
  return t('machines.terminals', { count })
}

function joinCardDetail(...parts: string[]): string {
  return parts.filter(Boolean).join(' · ')
}

function connectionPhaseShortLabel(phase: MachineConnectionSnapshot['phase'], t: TFunction): string {
  if (phase === 'probing') return t('machines.phase.probing')
  if (phase === 'resolving') return t('machines.phase.resolving')
  if (phase === 'signaling') return t('machines.phase.signaling')
  if (phase === 'authorizing') return t('machines.phase.authorizing')
  if (phase === 'verifying') return t('machines.phase.verifying')
  if (phase === 'reconnecting') return t('machines.phase.reconnecting')
  if (phase === 'waiting_network') return t('machines.phase.waiting_network')
  return t('machines.phase.connecting')
}

function formatAuthorizationExpiry(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  if (date.getFullYear() >= 2099) return value
  return date.toLocaleString(muxviaIntlLocale(), {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function machineAuthorizationState(
  machine: DisplayMachine,
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
    <div className="flex flex-1 items-start justify-center pt-16 lg:items-center lg:py-8 lg:pt-8">
      <div className="muxvia-app-panel flex w-full max-w-md flex-col items-start gap-5 border-x-0 px-6 py-8 text-left sm:border-x" data-testid="muxvia-machine-empty-state">
        <div className="flex h-12 w-12 items-center justify-center border border-[var(--muxvia-app-line)] bg-[var(--muxvia-app-soft)] text-[var(--muxvia-app-accent)]">
          {icon === 'login' ? <Server className="h-6 w-6" /> : <QrCode className="h-6 w-6" />}
        </div>
        <div className="space-y-1.5">
          <h2 className="text-base font-semibold text-zinc-950">{title}</h2>
          <p className="text-sm leading-5 text-zinc-500">{message}</p>
        </div>
        <button
          className="muxvia-app-primary-button h-11 w-full gap-2 px-3 text-sm font-semibold focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--muxvia-app-accent)]"
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


function readAuthorizedMachineIds(
  storage: RemoteRuntimeStorage | undefined,
  userId: string | undefined,
  externalPairingAdapter?: ExternalPairingAdapter,
): Set<string> {
  void userId
  if (!storage || !externalPairingAdapter) return new Set()
  try {
    return new Set(createMachineStore({ storage }).listMachines()
      .filter((machine) => {
        if (!externalPairingAdapter.isAuthorized(machine.machineId)) return false
        const expiresAt = externalPairingAdapter.authorizationExpiresAt?.(machine.machineId)
        return !expiresAt || !isExpiredAuthorization(expiresAt)
      })
      .map((machine) => machine.machineId))
  } catch {
    return new Set()
  }
}

function readAuthorizationExpiries(
  storage: RemoteRuntimeStorage | undefined,
  externalPairingAdapter?: ExternalPairingAdapter,
): Map<string, string> {
  if (!storage || !externalPairingAdapter) return new Map()
  try {
    const expiries = new Map<string, string>()
    for (const machine of createMachineStore({ storage }).listMachines()) {
      const externalExpiry = externalPairingAdapter.authorizationExpiresAt?.(machine.machineId)
      if (externalExpiry) expiries.set(machine.machineId, externalExpiry)
    }
    return expiries
  } catch {
    return new Map()
  }
}

function buildLocalHubReachabilityTargets(
  localMachines: StoredMachineRecord[],
  hubMachines: WebControlMachine[],
  signedIn: boolean,
): LocalHubReachabilityTarget[] {
  const targets = new Map<string, string[]>()
  for (const machine of localMachines) {
    const urls = compactHubUrls([
      ...localHubUrlsFromStoredMachine(machine),
      ...localFallbackHubUrlsFromStoredMachine(machine),
    ])
    if (urls.length > 0) targets.set(machine.machineId, urls)
  }
  if (signedIn) {
    for (const machine of hubMachines) {
      const urls = compactHubUrls([
        ...(machine.localHubUrls ?? []),
        ...(machine.localFallbackHubUrls ?? []),
        ...(targets.get(machine.id) ?? []),
      ])
      if (urls.length > 0) targets.set(machine.id, urls)
    }
  }
  return Array.from(targets.entries())
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([machineId, urls]) => ({ machineId, urls }))
}

function pruneLocalHubReachability(
  snapshots: Map<string, LocalHubReachabilitySnapshot>,
  targets: LocalHubReachabilityTarget[],
): Map<string, LocalHubReachabilitySnapshot> {
  if (snapshots.size === 0) return snapshots
  const liveTargets = new Map(targets.map((target) => [target.machineId, target.urls.join('\n')]))
  let changed = false
  const next = new Map<string, LocalHubReachabilitySnapshot>()
  for (const [machineId, snapshot] of snapshots) {
    if (liveTargets.get(machineId) !== snapshot.urls.join('\n')) {
      changed = true
      continue
    }
    next.set(machineId, snapshot)
  }
  return changed ? next : snapshots
}

async function probeLocalHubReachability(
  fetchImpl: RemoteRuntimeFetch,
  target: LocalHubReachabilityTarget,
  parentSignal: AbortSignal,
): Promise<LocalHubReachabilitySnapshot> {
  const results = await Promise.all(target.urls.map(async (url) => {
    const online = await probeLocalHubHealth(fetchImpl, url, parentSignal)
    return { url, online }
  }))
  return {
    machineId: target.machineId,
    urls: target.urls,
    onlineUrls: results.filter((result) => result.online).map((result) => result.url),
    checkedAt: Date.now(),
  }
}

async function probeLocalHubHealth(
  fetchImpl: RemoteRuntimeFetch,
  hubUrl: string,
  parentSignal: AbortSignal,
): Promise<boolean> {
  const baseUrl = normalizeHubBaseUrlCandidate(hubUrl)
  if (!baseUrl || parentSignal.aborted) return false
  const controller = new AbortController()
  const abort = () => controller.abort(parentSignal.reason)
  parentSignal.addEventListener('abort', abort, { once: true })
  let timeout: ReturnType<typeof setTimeout> | undefined
  try {
    // 本地模式的 Hub 和 daemon 是同一个运行时；health 可达即认为本地通路在线。
    const request = fetchImpl(`${baseUrl}/api/health`, {
      method: 'GET',
      headers: { accept: 'application/json' },
      signal: controller.signal,
    }).then((response) => response.ok, () => false)
    const deadline = new Promise<boolean>((resolve) => {
      timeout = setTimeout(() => {
        controller.abort(new Error('local Hub health probe timeout'))
        resolve(false)
      }, localHubReachabilityProbeTimeoutMs)
    })
    return await Promise.race([request, deadline])
  } catch {
    return false
  } finally {
    if (timeout) clearTimeout(timeout)
    parentSignal.removeEventListener('abort', abort)
  }
}

function sameReachabilitySnapshot(
  left: LocalHubReachabilitySnapshot | undefined,
  right: LocalHubReachabilitySnapshot,
): boolean {
  return Boolean(left) &&
    left!.machineId === right.machineId &&
    left!.urls.join('\n') === right.urls.join('\n') &&
    left!.onlineUrls.join('\n') === right.onlineUrls.join('\n')
}

function localMachineOnline(
  machine: StoredMachineRecord,
  snapshot: LocalHubReachabilitySnapshot | undefined,
): boolean {
  if (snapshot) return snapshot.onlineUrls.length > 0
  return machine.state === 'online'
}

function machineReachabilityView(input: {
  hubOnline: boolean
  localOnline: boolean
  snapshot?: LocalHubReachabilitySnapshot | undefined
}): MachineReachabilityView {
  return {
    hubOnline: input.hubOnline,
    localChecked: Boolean(input.snapshot),
    localOnline: input.localOnline,
    localOnlineUrls: input.snapshot?.onlineUrls ?? [],
  }
}

function pruneMachinesForSignOut(storage: RemoteRuntimeStorage | undefined): StoredMachineRecord[] {
  if (!storage) return []
  const store = createMachineStore({ storage })
  for (const machine of store.listMachines()) {
    if (machine.source !== 'hub') continue
    if (hasLocalAddresses(machine)) {
      store.saveMachine(downgradeHubMachineToLocal(machine))
    } else {
      store.forgetMachine(machine.machineId)
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
    accessClass: 'local',
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


function createUnavailableMachineRuntime(): MachineRuntime {
  const unavailable = async () => { throw new Error('a Proto binding machine runtime is required') }
  return { api: { getStatus: unavailable, listTerminals: unavailable }, connector: { connect: unavailable } }
}

const unavailableNetworkRuntime: RemoteNetworkRuntime = {
  fetch() {
    throw new Error('remote network runtime is required')
  },
  queryParam() {
    return null
  },
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
    accessClass: saved.accessClass === 'local' || saved.accessClass === 'local_cloud' || hasLocalAddresses(saved)
      ? 'local_cloud'
      : 'cloud',
    addresses: saved.addresses,
    endpoints: {
      ...saved.endpoints,
      ...(machine.controlUrl ? { webControl: machine.controlUrl } : {}),
      ...(summaryHubUrl ? { hub: summaryHubUrl } : {}),
    },
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

function nonEmptyHubUrls(machine: WebControlMachine): string[] {
  return compactHubUrls(machine.hubUrls)
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
  const relative = new Intl.RelativeTimeFormat(muxviaIntlLocale(), { numeric: 'auto' })
  const diffMinutes = Math.round(diffMs / 60_000)
  if (Math.abs(diffMinutes) < 60) return relative.format(-diffMinutes, 'minute')
  const diffHours = Math.round(diffMs / 3_600_000)
  if (Math.abs(diffHours) < 24) return relative.format(-diffHours, 'hour')
  const diffDays = Math.round(diffMs / 86_400_000)
  if (Math.abs(diffDays) < 7) return relative.format(-diffDays, 'day')
  return date.toLocaleDateString(muxviaIntlLocale(), {
    month: 'short',
    day: 'numeric',
  })
}
