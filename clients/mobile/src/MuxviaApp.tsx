import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { App as CapApp } from '@capacitor/app'
import { Capacitor, CapacitorHttp } from '@capacitor/core'
import { Keyboard } from '@capacitor/keyboard'
import { create } from '@bufbuild/protobuf'
import { Html5Qrcode } from 'html5-qrcode'
import {
  RemoteControlApp,
  createMachineStore,
  dispatchNativeKeyboardEvent,
  dispatchNativeBack,
  normalizeTerminalInventory,
  openProtoEventSubscription,
  MuxviaClientBinding,
  MuxviaApiApplication,
  MuxviaApiEvents,
  MuxviaApiTerminal,
  MuxviaRemoteAuth,
  muxviaI18n,
} from '@muxvia/ui'
import type {
  FileTransferContext,
  MachineWorkspaceProps,
  LocalStatus,
  Machine,
  RemoteNetworkRuntime,
  RemoteRuntimeFetch,
  RemoteRuntimeStorage,
  RtcConnectOptions,
  RtcConnectionStateSnapshot,
  RtcEvent,
  RtcSubscription,
  TerminalInventoryEvents,
  WebControlMachine,
  RemoteControlAppProps,
  ExternalPairingAdapter,
  ProtoClientSession,
} from '@muxvia/ui'
import { NativeConnection } from './plugins/nativeConnection'
import { NativeFileTransferStore } from './NativeFileTransferStore'
import { GoBindingClient, GoBindingConnector } from './GoBindingClient'
import { settleBindingGeneration } from './BindingGeneration'
import { NativeSessionManager, type NativeSessionConnector } from './NativeSessionManager'
import NativeFilePicker from './plugins/nativeFilePicker'
import { useNativeStatusBarSync } from './nativeStatusBar'
import { NativeForegroundBarrier, runAcrossNativePicker } from './NativeForegroundBarrier'

const defaultControlUrl = import.meta.env.VITE_CONTROL_URL || ''
const qrScannerRootId = 'muxvia-camera-qr-scanner'
const qrScannerReaderId = 'muxvia-camera-qr-reader'
const nativeHttpConnectTimeoutMs = 8_000
const nativeHttpReadTimeoutMs = 15_000
let goBindingClient = new GoBindingClient()

type MachineRuntimeFactory = NonNullable<RemoteControlAppProps['machineRuntimeFactory']>
type MachineRuntime = ReturnType<MachineRuntimeFactory>
type NativeSessionEntry = {
  endpointIdentity: string
  connector: NativeSessionConnector
  manager: NativeSessionManager
}
type NativeSessionLease = ProtoClientSession

export function MuxviaApp() {
  useAndroidBackButton()
  useNativeKeyboardEvents()
  useNativeStatusBarSync()

  const networkRuntime = useMemo(() => createNativeNetworkRuntime(), [])
  const endpointRegistry = useMemo(() => new NativeEndpointRegistryProjection(), [])
  const nativeAppRuntime = useMemo(() => createNativeAppRuntime(endpointRegistry), [endpointRegistry])
  const [registryReady, setRegistryReady] = useState(false)
  const [registryError, setRegistryError] = useState<string | null>(null)
  const refreshRegistry = useCallback(async (client: GoBindingClient = goBindingClient) => {
    try {
      const loaded = await settleBindingGeneration(
        client,
        () => goBindingClient,
        () => client.getEndpointRegistry(),
      )
      if (!loaded.current) return
      endpointRegistry.replace(loaded.value)
      if (networkRuntime.storage) syncRegistryMachineProjection(networkRuntime.storage, endpointRegistry.snapshot())
      setRegistryError(null)
      setRegistryReady(true)
    } catch (error) {
      setRegistryError(error instanceof Error ? error.message : String(error))
      setRegistryReady(false)
      throw error
    }
  }, [endpointRegistry, networkRuntime])
  useEffect(() => { void refreshRegistry().catch(() => undefined) }, [refreshRegistry])
  useAppResumeSync(refreshRegistry, nativeAppRuntime.resetGeneration)
  const externalPairingAdapter = useMemo(
    () => createNativeExternalPairingAdapter(endpointRegistry),
    [endpointRegistry],
  )
  const machineRuntimeFactory = useMemo<MachineRuntimeFactory>(
    () => nativeAppRuntime.createMachineRuntime,
    [nativeAppRuntime],
  )
  const globalFileTransfer = useMemo(
    () => nativeAppRuntime.fileTransfer,
    [nativeAppRuntime],
  )

  if (!registryReady) {
    return (
      <section
        aria-live="polite"
        className="muxvia-app-page flex h-[100dvh] w-screen items-center justify-center bg-[var(--muxvia-app-bg)]"
      >
        {registryError ? (
          <span className="px-6 text-center text-sm font-medium text-red-600">{muxviaI18n.t('errors.generic')}</span>
        ) : (
          <span aria-label={muxviaI18n.t('common.loading')} className="muxvia-square-spinner h-6 w-6 text-[var(--muxvia-app-accent)]" role="status" />
        )}
      </section>
    )
  }

  return (
    <section className="muxvia-app-page flex h-[100dvh] w-screen flex-col overflow-hidden antialiased">
      <RemoteControlApp
        accountAccessEnabled={false}
        defaultControlUrl={defaultControlUrl}
        exportDebugLogs={exportNativeDebugLogs}
        externalPairingAdapter={externalPairingAdapter}
        globalFileTransfer={globalFileTransfer}
        machineRuntimeFactory={machineRuntimeFactory}
        networkRuntime={networkRuntime}
        scanPairingCode={scanPairingCode}
      />
    </section>
  )
}

function createNativeExternalPairingAdapter(registry: NativeEndpointRegistryProjection): ExternalPairingAdapter {
  return {
    async import(rawValue, expectedMachineId) {
    const imported = await goBindingClient.importPairing(create(MuxviaClientBinding.ImportPairingRequestSchema, {
    requestId: crypto.randomUUID(),
    portablePayload: rawValue,
    expectedEndpointId: expectedMachineId ?? '',
    }))
    const endpoint = imported.endpoint
    if (!endpoint?.endpointId || !endpoint.identity || endpoint.routes.length === 0) return null
    const expiresAt = new Date(Number(imported.expiresAtUnixNano / 1_000_000n)).toISOString()
    registry.replace(imported.registry ?? await goBindingClient.getEndpointRegistry())
    registry.setAuthorizationExpiry(endpoint.endpointId, expiresAt)
      return {
    machine: { id: endpoint.endpointId, name: endpoint.label || endpoint.endpointId, accessClass: 'local' },
    expiresAt,
      }
    },
  async inspectShare(rawValue) {
    const received = await goBindingClient.receiveEndpointShare(rawValue)
    const preview = received.preview
    if (!preview?.importToken || !preview.identity) throw new Error('Endpoint share preview is incomplete')
    return {
    importToken: preview.importToken,
    endpointId: preview.endpointId,
    label: preview.label || preview.endpointId,
    deviceId: preview.identity.deviceId,
    deviceFingerprint: preview.identity.deviceFingerprint,
    routes: preview.routeDiffs.map((route) => ({ id: route.routeId, kind: route.routeKind, action: route.action })),
    connectModeChanged: preview.connectModeChanged,
    selectionPolicyChanged: preview.selectionPolicyChanged,
    credentialKinds: preview.credentialDescriptors.map((descriptor) => String(descriptor.kind)),
    }
  },
    async commitShare(importToken) {
      const committed = await goBindingClient.commitEndpointShare(importToken)
      let endpoint = committed.endpoint
      if (!endpoint?.endpointId || !endpoint.identity || !committed.registry) throw new Error('Endpoint share commit is incomplete')
      registry.replace(committed.registry)
      const sshCredentials: NonNullable<import('@muxvia/ui').ExternalPairingImportResult['sshCredentials']> = []
      for (const route of endpoint.routes) {
      if (route.route.case !== 'sshWebrtcTcp' || route.route.value.credentialDescriptor?.kind !== MuxviaRemoteAuth.EndpointCredentialKind.SSH_PRIVATE_KEY) continue
      const provisioned = await goBindingClient.provisionSSHCredential(endpoint.endpointId, route.routeId)
      if (!provisioned.endpoint || !provisioned.registry) throw new Error('SSH credential provision result is incomplete')
      registry.replace(provisioned.registry)
      endpoint = provisioned.endpoint
      sshCredentials.push({ routeId: route.routeId, authorizedKey: provisioned.authorizedKey, fingerprint: provisioned.keyFingerprint })
      }
      return {
      machine: { id: endpoint.endpointId, name: endpoint.label || endpoint.endpointId, accessClass: 'local' },
      authorizationRequired: !registry.isAuthorized(endpoint.endpointId),
      ...(sshCredentials.length > 0 ? { sshCredentials } : {}),
      }
  },
    isAuthorized(machineId) {
    return registry.isAuthorized(machineId)
    },
    authorizationExpiresAt(machineId) {
    return registry.authorizationExpiry(machineId)
    },
    async forget(machineId) {
    const deleted = await goBindingClient.deleteEndpoint(machineId)
    if (deleted.registry) registry.replace(deleted.registry)
    registry.setAuthorizationExpiry(machineId, undefined)
    },
  }
}

/** NativeEndpointRegistryProjection 是 Go registry 的只读 UI projection，不执行字段合并或持久化。 */
class NativeEndpointRegistryProjection {
  private registry = create(MuxviaRemoteAuth.EndpointRegistryV1Schema, { schemaVersion: 1 })
  private readonly expiries = new Map<string, string>()
  private versionValue = 0

  replace(registry: MuxviaRemoteAuth.EndpointRegistryV1): void {
    this.registry = create(MuxviaRemoteAuth.EndpointRegistryV1Schema, registry)
    this.versionValue += 1
  }

  snapshot(): MuxviaRemoteAuth.EndpointRegistryV1 { return create(MuxviaRemoteAuth.EndpointRegistryV1Schema, this.registry) }
  has(endpointId: string): boolean { return this.registry.endpoints.some((endpoint) => endpoint.endpointId === endpointId) }
  isAuthorized(endpointId: string): boolean {
  const endpoint = this.registry.endpoints.find((candidate) => candidate.endpointId === endpointId)
  return endpoint?.routes.some((route) => route.credentialRef.trim() !== '') ?? false
  }
  version(): number { return this.versionValue }
  authorizationExpiry(endpointId: string): string | undefined { return this.expiries.get(endpointId) }
  setAuthorizationExpiry(endpointId: string, expiresAt: string | undefined): void {
    if (expiresAt) this.expiries.set(endpointId, expiresAt)
    else this.expiries.delete(endpointId)
  }
}

function syncRegistryMachineProjection(storage: RemoteRuntimeStorage, registry: MuxviaRemoteAuth.EndpointRegistryV1): void {
  const store = createMachineStore({ storage })
  const now = new Date().toISOString()
  for (const endpoint of registry.endpoints) {
    const existing = store.getMachine(endpoint.endpointId)
    store.saveMachine({
      machineId: endpoint.endpointId,
      name: endpoint.label || existing?.name || endpoint.endpointId,
      state: existing?.state ?? 'offline',
      terminalCount: existing?.terminalCount ?? 0,
      source: existing?.source ?? 'manual',
      accessClass: existing?.accessClass ?? 'local',
      addresses: existing?.addresses ?? { local: [], lan: [], public: [] },
      endpoints: existing?.endpoints ?? {},
      addedAt: existing?.addedAt ?? now,
      updatedAt: now,
    })
  }
}

async function exportNativeDebugLogs(): Promise<void> {
  await NativeConnection.exportDebugLogs()
}

function useNativeKeyboardEvents(): void {
  useEffect(() => {
    const subscriptions = [
      Keyboard.addListener('keyboardWillShow', (info) => {
        dispatchNativeKeyboardEvent({ visible: true, keyboardHeight: info.keyboardHeight })
      }),
      Keyboard.addListener('keyboardDidShow', (info) => {
        dispatchNativeKeyboardEvent({ visible: true, keyboardHeight: info.keyboardHeight })
      }),
      Keyboard.addListener('keyboardWillHide', () => {
        dispatchNativeKeyboardEvent({ visible: false })
      }),
      Keyboard.addListener('keyboardDidHide', () => {
        dispatchNativeKeyboardEvent({ visible: false })
      }),
    ]

    return () => {
      for (const subscription of subscriptions) {
        void subscription.then((handle) => handle.remove())
      }
    }
  }, [])
}

const nativeForegroundBarrier = new NativeForegroundBarrier()

function markNativeBackground(): void {
  nativeForegroundBarrier.markBackground()
}

function finishNativeForeground(failure?: unknown): void {
  nativeForegroundBarrier.finishForeground(failure)
}

function reportNativeGenerationFailure(failure: unknown): void {
  const message = failure instanceof Error ? failure.message : String(failure)
  console.error('[muxvia:native-generation]', message)
  void NativeConnection.writeDebugLog({ level: 'error', tag: 'NativeGeneration', message }).catch(() => undefined)
}

let nativeGenerationReplacement: Promise<void> = Promise.resolve()

function replaceNativeGeneration(
  refreshRegistry: (client?: GoBindingClient) => Promise<void>,
  resetRuntime: () => Promise<void>,
  reloadRegistry: boolean,
): Promise<void> {
  const replacement = nativeGenerationReplacement.catch(() => undefined).then(async () => {
    const staleClient = goBindingClient
    const currentClient = new GoBindingClient()
    goBindingClient = currentClient
    // Endpoint runtime 仍被 React workspace 缓存；必须先清除其旧 binding session，下一次操作才会
    // 通过动态 connector 进入 currentClient，而不是继续返回已失效的 generation。
    await resetRuntime()
    await staleClient.close()
    // 网络切换不会修改 Endpoint registry；此时重读 registry 会让恢复依赖刚启动 engine 的
    // 额外 operation。只有 WebView 前后台恢复才重新读取 Go-owned 持久投影。
    if (reloadRegistry) await refreshRegistry(currentClient)
    document.dispatchEvent(new Event('muxvia:resume'))
  })
  nativeGenerationReplacement = replacement
  return replacement
}

/** When the app resumes from background, trigger a sync request on all active sessions. */
function useAppResumeSync(
  refreshRegistry: (client?: GoBindingClient) => Promise<void>,
  resetRuntime: () => Promise<void>,
): void {
  useEffect(() => {
    const promise = CapApp.addListener('appStateChange', (state) => {
      if (!state.isActive) {
        markNativeBackground()
        return
      }
    void NativeConnection.handleForegroundResume().then(async () => {
    // Native 已创建新 generation 后再通知 UI；冻结前的 session/resource handle 不得继续使用。
    await replaceNativeGeneration(refreshRegistry, resetRuntime, true)
    }).then(
    () => finishNativeForeground(),
    (failure) => {
      reportNativeGenerationFailure(failure)
      finishNativeForeground(failure)
    },
    )
    })
    const generationPromise = NativeConnection.addListener('generationChanged', () => {
      markNativeBackground()
      void replaceNativeGeneration(refreshRegistry, resetRuntime, false).then(
        () => finishNativeForeground(),
        (failure) => {
          reportNativeGenerationFailure(failure)
          finishNativeForeground(failure)
        },
      )
    })
    return () => {
      void promise.then((sub) => sub.remove())
      void generationPromise.then((sub) => sub.remove())
    }
  }, [refreshRegistry, resetRuntime])
}

function useAndroidBackButton(): void {
  useEffect(() => {
    const promise = CapApp.addListener('backButton', () => {
      if (dispatchNativeBack()) return
      const backButton = document.querySelector<HTMLButtonElement>(
        [
          'button[aria-label="Back to machines"]',
          'button[aria-label="Close pairing"]',
          'button[aria-label="Close"]',
        ].join(','),
      )
      if (backButton) {
        backButton.click()
        return
      }
    })
    return () => {
      void promise.then((subscription) => subscription.remove())
    }
  }, [])
}

function createNativeNetworkRuntime(): RemoteNetworkRuntime {
  return {
    fetch: nativeFetch,
    storage: browserStorage(),
    queryParam(name) {
      return new URLSearchParams(globalThis.location?.search ?? '').get(name)
    },
  }
}

async function scanPairingCode(options?: { onCancel?: () => void; onManualEntry?: () => void }): Promise<string | null> {
  console.info('[muxvia:scan] camera scan requested')
  const existing = document.getElementById(qrScannerRootId)
  existing?.remove()
  const scannerSize = scannerSquareSize()
  const qrboxSize = Math.min(220, Math.max(180, scannerSize - 32))

  const root = document.createElement('div')
  root.id = qrScannerRootId
  root.className = 'muxvia-app-page fixed inset-0 z-[2147483647] flex flex-col items-stretch px-4 pb-[calc(env(safe-area-inset-bottom)+12px)] pt-[calc(env(safe-area-inset-top)+12px)]'

  const scannerStyle = document.createElement('style')
  scannerStyle.textContent = `
    #${qrScannerReaderId} {
      width: ${scannerSize}px !important;
      height: ${scannerSize}px !important;
      aspect-ratio: 1 / 1 !important;
      overflow: hidden !important;
      border: none !important;
    }
    #${qrScannerReaderId} > div,
    #${qrScannerReaderId}__scan_region,
    #${qrScannerReaderId}__scan_region > div,
    #${qrScannerReaderId} video,
    #${qrScannerReaderId} canvas {
      width: 100% !important;
      height: 100% !important;
    }
    #${qrScannerReaderId} video,
    #${qrScannerReaderId} canvas {
      object-fit: cover !important;
    }
    #${qrScannerReaderId} img {
      display: none !important;
    }
  `

  const header = document.createElement('div')
  header.className = 'flex items-center justify-between gap-3 min-h-[44px]'

  const title = document.createElement('div')
  title.textContent = muxviaI18n.t('scanner.title')
  title.className = 'text-[17px] font-bold tracking-tight text-zinc-900'

  const cancelButton = document.createElement('button')
  cancelButton.type = 'button'
  cancelButton.textContent = muxviaI18n.t('common.cancel')
  cancelButton.className = 'muxvia-app-secondary-button px-4 text-[14px] font-semibold'

  const manualContainer = document.createElement('div')
  manualContainer.className = 'mt-auto flex flex-col gap-3'

  const manualInput = document.createElement('textarea')
  manualInput.placeholder = muxviaI18n.t('scanner.manualPlaceholder')
  manualInput.className = 'h-[90px] w-full resize-none border border-[var(--muxvia-app-line)] bg-white p-3 font-mono text-[13px] text-zinc-900 outline-none focus:border-[var(--muxvia-app-accent)] focus:ring-1 focus:ring-[var(--muxvia-app-accent)]'

  const manualSubmit = document.createElement('button')
  manualSubmit.type = 'button'
  manualSubmit.textContent = muxviaI18n.t('pairing.add')
  manualSubmit.className = 'muxvia-app-primary-button min-h-12 w-full px-4 text-[15px] font-semibold disabled:opacity-50'
  manualSubmit.disabled = true

  manualInput.oninput = () => {
    const hasValue = manualInput.value.trim().length > 0
    manualSubmit.disabled = !hasValue
  }

  manualContainer.append(manualInput, manualSubmit)

  const reader = document.createElement('div')
  reader.id = qrScannerReaderId
  reader.className = 'mt-4 self-center overflow-hidden border border-[var(--muxvia-app-line)] bg-black'
  reader.style.width = `${scannerSize}px`
  reader.style.height = `${scannerSize}px`
  reader.style.minWidth = `${scannerSize}px`
  reader.style.minHeight = `${scannerSize}px`
  reader.style.maxWidth = `${scannerSize}px`
  reader.style.maxHeight = `${scannerSize}px`

  const hint = document.createElement('div')
  hint.textContent = muxviaI18n.t('scanner.hint')
  hint.className = 'mt-4 px-4 text-center text-[13px] font-medium leading-[20px] text-zinc-500'

  header.append(title, cancelButton)
  root.append(scannerStyle, header, reader, hint, manualContainer)
  document.body.append(root)

  // Android WebView 的 BarcodeDetector 可能在缺少 GMS provider 时让进程直接崩溃；扫码只使用库内 decoder。
  const scanner = new Html5Qrcode(qrScannerReaderId, {
    verbose: false,
    useBarCodeDetectorIfSupported: false,
  })
  let settled = false
  let started = false

  return new Promise((resolve, reject) => {
    const finish = (value: string | null, error?: unknown) => {
      if (settled) return
      settled = true
      cancelButton.disabled = true
      manualSubmit.disabled = true
      root.remove()

      if (started) {
        void scanner.stop()
          .catch(() => {})
          .then(() => {
            scanner.clear()
          })
          .catch(() => {})
      } else {
        try {
          scanner.clear()
        } catch {}
      }
      if (error) {
        console.warn('[muxvia:scan] camera scan failed', error instanceof Error ? error.message : String(error))
        reject(error instanceof Error ? error : new Error(String(error)))
        return
      }
      console.info(value ? '[muxvia:scan] QR decoded' : '[muxvia:scan] scan cancelled')
      resolve(value)
    }

  cancelButton.onclick = () => {
      if (settled) return
      options?.onCancel?.()
      finish(null)
    }
    manualSubmit.onclick = () => {
      if (settled) return
      const value = manualInput.value.trim()
      if (!value) return
      options?.onManualEntry?.()
      finish(value)
    }
    scanner.start(
      { facingMode: 'environment' },
      { fps: 10, qrbox: { width: qrboxSize, height: qrboxSize }, aspectRatio: 1.0 },
      (decodedText) => {
        if (!settled) {
          try {
            scanner.pause(true)
          } catch {}
        }
        finish(decodedText)
      },
      () => {},
    ).then(() => {
      started = true
    }).catch((error) => finish(null, error))
  })
}

function scannerSquareSize(): number {
  const width = Math.max(0, window.innerWidth || document.documentElement.clientWidth || 360)
  const height = Math.max(0, window.innerHeight || document.documentElement.clientHeight || 640)
  const availableHeight = Math.max(180, height - 340)
  return Math.floor(Math.max(220, Math.min(width * 0.78, availableHeight, 280)))
}

const nativeFetch: RemoteRuntimeFetch = async (input, init = {}) => {
  if (!Capacitor.isNativePlatform()) {
    return globalThis.fetch(input, init)
  }

  const url = String(input)
  const method = init.method ?? 'GET'
  const headers = headersRecord(init.headers)
  const data = requestData(init.body)
  const response = await CapacitorHttp.request({
    url,
    method,
    headers,
    ...(data !== undefined ? { data } : {}),
    responseType: 'text',
    connectTimeout: nativeHttpConnectTimeoutMs,
    readTimeout: nativeHttpReadTimeoutMs,
  })

  return new Response(responseText(response.data), {
    status: response.status,
    headers: response.headers,
  })
}

function browserStorage(): RemoteRuntimeStorage | undefined {
  const storage = globalThis.localStorage
  if (
    !storage ||
    typeof storage.getItem !== 'function' ||
    typeof storage.setItem !== 'function' ||
    typeof storage.removeItem !== 'function'
  ) {
    return undefined
  }
  return storage
}

function createNativeAppRuntime(endpointRegistry: NativeEndpointRegistryProjection): {
  createMachineRuntime: MachineRuntimeFactory
  fileTransfer: FileTransferContext
  resetGeneration: () => Promise<void>
} {
  const transferStore = new NativeFileTransferStore()
  const sessionManagers = new Map<string, NativeSessionEntry>()
  transferStore.setSessionResolver(async (machineId, signal) => {
    return await sessionManagers.get(machineId)?.manager.get({ signal }) ?? null
  })

  return {
    fileTransfer: createFileTransferContext(undefined, transferStore),
    async resetGeneration() {
      await Promise.all([...sessionManagers.values()].map(async (entry) => {
        await entry.manager.reset()
        await entry.connector.release?.(entry.manager.machineID())
      }))
    },
    createMachineRuntime(input) {
      return createNativeMachineRuntime(input.machine, input.storage, endpointRegistry, {
        sessionManagers,
        transferStore,
      })
    },
  }
}

function createNativeMachineRuntime(
  machine: WebControlMachine,
  storage: RemoteRuntimeStorage,
  endpointRegistry: NativeEndpointRegistryProjection,
  shared: {
    sessionManagers: Map<string, NativeSessionEntry>
    transferStore: NativeFileTransferStore
  },
): MachineRuntime {
  const machineStore = createMachineStore({ storage })
  const storedMachine = machineStore.getMachine(machine.id)
  const endpointIdentity = [
    machine.id,
  endpointRegistry.version(),
  ].join('|')
  let entry = shared.sessionManagers.get(machine.id)
  if (!entry || entry.endpointIdentity !== endpointIdentity) {
    void entry?.manager.reset().catch(() => {})
    void entry?.connector.release?.(machine.id).catch(() => {})
    const connector = createNativeConnector(machine, endpointRegistry)
    entry = {
      endpointIdentity,
      connector,
      manager: new NativeSessionManager(machine.id, connector),
    }
    shared.sessionManagers.set(machine.id, entry)
  }
  const sessionManager = entry.manager
  const connector = entry.connector
  const transferStore = shared.transferStore

  const api: MachineWorkspaceProps['api'] = {
    async getStatus(): Promise<LocalStatus> {
      const statusMachine: Machine = {
        machineId: machine.id,
        name: machine.name,
        state: machine.online ? 'online' : 'offline',
        terminalCount: storedMachine?.terminalCount,
        ...(machine.lastSeen || storedMachine?.lastSeenAt ? { lastSeenAt: machine.lastSeen ?? storedMachine?.lastSeenAt } : {}),
      }
      return {
        machine: statusMachine,
        localWeb: {
          httpUrl: machine.controlUrl ?? storedMachine?.endpoints.webControl ?? '',
          rtcOfferUrl: firstNonEmpty(machine.hubUrls) ?? storedMachine?.endpoints.hub ?? '',
        },
      }
    },
    async listTerminals(options) {
      emitNativeRuntimeConnectionState(options, machine.id, 'connecting', 'Connecting through native runtime...')
      const session = await sessionManager.get({
        forceRelay: options?.forceRelay,
        onStatus: options?.onStatus,
        onConnectionState: options?.onConnectionState,
      })
    const relayInUse = session.connection?.localCandidateType === MuxviaClientBinding.ConnectionCandidateType.RELAY
      emitNativeRuntimeConnectionState(options, machine.id, 'connecting', 'Fetching terminals...', relayInUse)
      try {
        const response = await session.execute(create(MuxviaApiApplication.CommandEnvelopeSchema, {
          command: { case: 'terminalList', value: create(MuxviaApiTerminal.TerminalListCommandSchema) },
        }))
        if (response.result.case !== 'terminalList') throw new Error('terminal list returned no result')
        emitNativeRuntimeConnectionState(options, machine.id, 'connected', 'Connected', relayInUse)
        return normalizeTerminalInventory({
          machine_id: machine.id,
          terminals: response.result.value.terminals.map((terminal) => ({
            terminal_id: terminal.ref?.terminalId ?? '',
            name: terminal.name,
            state: terminal.state === MuxviaApiTerminal.TerminalState.RUNNING ? 'running' : terminal.state === MuxviaApiTerminal.TerminalState.EXITED ? 'exited' : 'unknown',
            command: terminal.command,
            cwd: terminal.cwd,
            live_cwd: terminal.liveCwd,
            cols: terminal.size?.cols ?? 0,
            rows: terminal.size?.rows ?? 0,
          })),
        }).terminals
      } finally {
        await session.close()
      }
    },
  }

  return {
    api,
    connector: {
      connect(target, options) {
        if (target.machineId !== machine.id) {
          throw new Error(`machine runtime mismatch: ${target.machineId} != ${machine.id}`)
        }
        const sessionPromise = sessionManager.lease(options)
        return sessionPromise
      },
      reconnect(options) {
    return sessionManager.resetClientOnly(options)
      },
      getConnectionPolicy: (signal) => connector.getConnectionPolicy?.(signal) ?? Promise.reject(new Error('Connection policy is unavailable')),
      applyConnectionPolicy: (policy, signal) => connector.applyConnectionPolicy?.(policy, signal) ?? Promise.reject(new Error('Connection policy is unavailable')),
      routeManagement: {
        async load(signal) {
          const registry = await goBindingClient.getEndpointRegistry(signal)
          const endpoint = registry.endpoints.find((candidate) => candidate.endpointId === machine.id)
          if (!endpoint) throw new Error('Endpoint is not configured')
          return create(MuxviaRemoteAuth.EndpointConfigV1Schema, endpoint)
        },
        async save(endpoint, signal) {
          await sessionManager.reset()
          const result = await goBindingClient.upsertEndpoint(endpoint, false, signal)
          if (result.registry) endpointRegistry.replace(result.registry)
          if (!result.endpoint) throw new Error('Endpoint update returned no endpoint')
          return result.endpoint
        },
        async test(routeId, signal) {
          await sessionManager.reset()
          const routeConnector = new GoBindingConnector(() => goBindingClient, { endpointId: machine.id, routeId })
          const session = await routeConnector.connect({ machineId: machine.id }, { signal })
          await session.close()
        },
        async provisionSSH(routeId, signal) {
          await sessionManager.reset()
          const result = await goBindingClient.provisionSSHCredential(machine.id, routeId, signal)
          if (result.registry) endpointRegistry.replace(result.registry)
          return result
        },
      },
    },
    inventoryEvents: createNativeInventoryEvents(machine.id, sessionManager),
    fileTransfer: createFileTransferContext(machine.id, transferStore),
    dispose: () => {
      if (shared.sessionManagers.get(machine.id)?.manager === sessionManager) {
        shared.sessionManagers.delete(machine.id)
      }
      return sessionManager.reset().finally(() => connector.release?.(machine.id))
    },
  }
}

function createFileTransferContext(machineId: string | undefined, store: NativeFileTransferStore): FileTransferContext {
  return {
    subscribe: (listener) => store.subscribe(listener),
    getSnapshot: () => store.getSnapshot(machineId),
    isNative: true,
    getDownloadResumeOffset(mid, filePath, fileSize) {
      return store.getDownloadResumeOffset(mid, filePath, fileSize)
    },
    startDownload(mid, fileName, fileSize, filePath, offset) {
      store.startDownload(mid, fileName, fileSize, filePath, offset)
    },
    startUpload(mid, files, targetDir) {
      for (const f of files) {
        store.startUpload(mid, f.uri, f.name, f.size, targetDir)
      }
    },
    pickAndUpload(mid, targetDir) {
      runAcrossNativePicker(nativeForegroundBarrier, () => NativeFilePicker.pickFiles({ multiple: true })).then((result) => {
        // SAF 返回后只保存平台 URI；upload session 必须在新 foreground generation 上重新取得。
        for (const f of result.files) {
          store.startUpload(mid, f.uri, f.name, f.size, targetDir)
        }
      }).catch(() => {})
    },
    pauseTransfer(id) { store.pauseTransfer(id) },
    resumeTransfer(id) { store.resumeTransfer(id) },
    resumeAllTransfers(machineId) { store.resumeAllTransfers(machineId) },
    cancelTransfer(id) { store.cancelTransfer(id) },
    dismissTransfer(id) { store.dismissTransfer(id) },
  }
}

function createNativeConnector(
  machine: WebControlMachine,
  endpointRegistry: NativeEndpointRegistryProjection,
): NativeSessionConnector {
  if (!endpointRegistry.has(machine.id)) {
    return {
      async connect() {
    throw new Error('Endpoint requires a valid Proto configuration from the pairing flow')
      },
    }
  }

  const connector = new GoBindingConnector(() => goBindingClient, {
  endpointId: machine.id,
  })
  return {
    connect: (target, options) => connector.connect(target, options),
    getConnectionPolicy: (signal) => connector.getConnectionPolicy(signal),
    applyConnectionPolicy: (policy, signal) => connector.applyConnectionPolicy(policy, signal),
  }
}

function createNativeInventoryEvents(
  machineId: string,
  sessionManager: NativeSessionManager,
): TerminalInventoryEvents {
  return {
    subscribe(targetMachineId, handler) {
      if (targetMachineId !== machineId) return { close() {} }
      let closed = false
      let subscription: RtcSubscription | null = null
      let session: NativeSessionLease | null = null
      void sessionManager.get().then(async (connectedSession) => {
        if (closed) {
          void connectedSession.close()
          return
        }
        session = connectedSession
        subscription = await openProtoEventSubscription(connectedSession, create(MuxviaApiEvents.EventSubscribeCommandSchema, {
          types: [MuxviaApiEvents.ApplicationEventType.TERMINAL_LIFECYCLE],
        }), (event) => {
          if (event.event.case === 'terminalLifecycle') handler({ type: 'inventory_changed', payload: event.event.value })
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
          void session?.close()
          session = null
        },
      }
    },
  }
}

function emitNativeRuntimeConnectionState(
  options: Pick<RtcConnectOptions, 'onStatus' | 'onConnectionState'> | undefined,
  machineId: string,
  phase: RtcConnectionStateSnapshot['phase'],
  statusText: string,
  relayInUse = false,
): void {
  options?.onStatus?.(statusText)
  options?.onConnectionState?.({
    machineId,
    phase,
    statusText,
  relayInUse,
  })
}

function firstNonEmpty(values: readonly (string | undefined)[]): string | undefined {
  return compactStrings(values)[0]
}

function compactStrings(values: readonly (string | undefined)[]): string[] {
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

function isTerminalInventoryEvent(event: RtcEvent): boolean {
  return event.type === 'inventory_changed' ||
    event.type === 'terminal_changed' ||
    event.type === 'terminal_created' ||
    event.type === 'terminal_state_changed' ||
    event.type === 'terminal_resized' ||
    event.type === 'terminal_removed' ||
    event.type === 'terminal_metadata_changed'
}

function abortError(signal: AbortSignal): Error {
  return signal.reason instanceof Error ? signal.reason : new Error('connection aborted')
}

function headersRecord(headers: HeadersInit | undefined): Record<string, string> {
  if (!headers) return {}
  if (headers instanceof Headers) return Object.fromEntries(headers.entries())
  if (Array.isArray(headers)) return Object.fromEntries(headers)
  return { ...headers }
}

function requestData(body: BodyInit | null | undefined): string | undefined {
  if (body === undefined || body === null) return undefined
  if (typeof body === 'string') return body
  throw new Error('native fetch only supports string request bodies')
}

function responseText(data: unknown): string {
  if (data === undefined || data === null) return ''
  if (typeof data === 'string') return data
  return JSON.stringify(data)
}
