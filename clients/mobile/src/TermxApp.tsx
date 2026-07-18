import { useEffect, useMemo, useRef } from 'react'
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
  TermxClientBinding,
  TermxApiApplication,
  TermxApiTerminal,
} from '@termx/ui'
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
  StoredMachineRecord,
  TerminalInventoryEvents,
  WebControlMachine,
  RemoteControlAppProps,
  ExternalPairingAdapter,
  CloudAccountAdapter,
  ProtoClientSession,
} from '@termx/ui'
import { NativeConnection, type NativeRelayMode } from './plugins/nativeConnection'
import { NativeFileTransferStore } from './NativeFileTransferStore'
import { GoBindingClient, GoBindingConnector } from './GoBindingClient'
import NativeFilePicker from './plugins/nativeFilePicker'
import { useNativeStatusBarSync } from './nativeStatusBar'

const defaultControlUrl = import.meta.env.VITE_CONTROL_URL || 'http://114.66.58.243:12306'
const qrScannerRootId = 'termx-camera-qr-scanner'
const qrScannerReaderId = 'termx-camera-qr-reader'
const nativeHttpConnectTimeoutMs = 8_000
const nativeHttpReadTimeoutMs = 15_000
let goBindingClient = new GoBindingClient()

type MachineRuntimeFactory = NonNullable<RemoteControlAppProps['machineRuntimeFactory']>
type MachineRuntime = ReturnType<MachineRuntimeFactory>
type NativeSessionManager = ReturnType<typeof createNativeSessionManager>
type NativeConnector = {
  connect(input: { machineId: string }, options?: RtcConnectOptions): Promise<ProtoClientSession>
  release?(machineId: string): Promise<void>
}
type NativeSessionEntry = {
  endpointIdentity: string
  connector: NativeConnector
  manager: NativeSessionManager
}
type NativeSessionLease = ProtoClientSession

export function TermxApp() {
  useAndroidBackButton()
  useAppResumeSync()
  useNativeKeyboardEvents()
  useNativeStatusBarSync()

  const networkRuntime = useMemo(() => createNativeNetworkRuntime(), [])
  const nativeAppRuntime = useMemo(() => createNativeAppRuntime(), [])
  const externalPairingAdapter = useMemo(
    () => networkRuntime.storage ? createNativeExternalPairingAdapter(networkRuntime.storage) : undefined,
    [networkRuntime],
  )
  const cloudAccountAdapter = useMemo<CloudAccountAdapter>(() => ({
    async current() {
      const account = await NativeConnection.getCloudAccount()
      return account.accountId && account.accountLabel ? { accountId: account.accountId, accountLabel: account.accountLabel } : null
    },
    beginActivation: () => NativeConnection.cloudBeginActivation(),
    claimActivation: (payload) => NativeConnection.cloudClaimActivation({ payload }),
    async awaitActivation() {
      const account = await NativeConnection.cloudAwaitActivation()
      if (!account.accountId || !account.accountLabel) throw new Error('TermX Cloud returned an invalid account')
      return { accountId: account.accountId, accountLabel: account.accountLabel }
    },
    cancelActivation: () => NativeConnection.cloudCancelActivation(),
    async listMachines() {
      const result = await NativeConnection.cloudListDevices()
      return result.devices
        .filter((device) => device.kind === 'daemon' && !device.revoked)
        .map((device) => ({
          id: device.deviceId,
          name: device.displayName,
          osInfo: device.platform,
          online: device.online,
          source: 'hub' as const,
          hubUrls: [],
          hubStatus: device.online ? 'online' : 'offline',
        }))
    },
    logout: () => NativeConnection.cloudLogout(),
  }), [])
  const machineRuntimeFactory = useMemo<MachineRuntimeFactory>(
    () => nativeAppRuntime.createMachineRuntime,
    [nativeAppRuntime],
  )
  const globalFileTransfer = useMemo(
    () => nativeAppRuntime.fileTransfer,
    [nativeAppRuntime],
  )

  return (
    <section className="termx-app-page flex h-[100dvh] w-screen flex-col overflow-hidden antialiased">
      <RemoteControlApp
        defaultControlUrl={defaultControlUrl}
        cloudAccountAdapter={cloudAccountAdapter}
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

function createNativeExternalPairingAdapter(storage: RemoteRuntimeStorage): ExternalPairingAdapter {
  const key = (machineId: string, field: string) => `termx.endpoint.${machineId}.${field}`
  return {
    async import(rawValue, expectedMachineId) {
	  const imported = await goBindingClient.importPairing(create(TermxClientBinding.ImportPairingRequestSchema, {
		requestId: crypto.randomUUID(),
		portablePayload: rawValue,
		expectedEndpointId: expectedMachineId ?? '',
	  }))
	  if (!imported.endpointId || !imported.credentialRef) return null
	  const expiresAt = new Date(Number(imported.expiresAtUnixNano / 1_000_000n)).toISOString()
      storage.setItem(key(imported.endpointId, 'targetDeviceId'), imported.targetDeviceId)
      storage.setItem(key(imported.endpointId, 'deviceFingerprint'), imported.deviceFingerprint)
	  storage.setItem(key(imported.endpointId, 'grantRef'), imported.credentialRef)
	  storage.setItem(key(imported.endpointId, 'grantExpiresAt'), expiresAt)
      storage.setItem(key(imported.endpointId, 'relayMode'), 'direct')
      return {
        machine: { id: imported.endpointId, name: imported.label, accessClass: 'cloud' },
		expiresAt,
      }
    },
    isAuthorized(machineId) {
      return Boolean(
        storage.getItem(key(machineId, 'targetDeviceId'))?.trim() &&
        storage.getItem(key(machineId, 'deviceFingerprint'))?.trim() &&
        storage.getItem(key(machineId, 'grantRef'))?.trim(),
      )
    },
    authorizationExpiresAt(machineId) {
      return storage.getItem(key(machineId, 'grantExpiresAt'))?.trim() || undefined
    },
    async forget(machineId) {
      const grantRef = storage.getItem(key(machineId, 'grantRef'))?.trim()
      storage.removeItem(key(machineId, 'targetDeviceId'))
      storage.removeItem(key(machineId, 'deviceFingerprint'))
      storage.removeItem(key(machineId, 'grantRef'))
      storage.removeItem(key(machineId, 'grantExpiresAt'))
      storage.removeItem(key(machineId, 'relayMode'))
	  if (grantRef) {
		await goBindingClient.deleteCredential(create(TermxClientBinding.DeleteCredentialRequestSchema, {
		  requestId: crypto.randomUUID(),
		  credentialRef: grantRef,
		}))
	  }
    },
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

/** When the app resumes from background, trigger a sync request on all active sessions. */
function useAppResumeSync(): void {
  useEffect(() => {
    const promise = CapApp.addListener('appStateChange', (state) => {
      if (!state.isActive) {
        return
      }
	  void NativeConnection.handleForegroundResume().then(async () => {
		const staleClient = goBindingClient
		goBindingClient = new GoBindingClient()
		await staleClient.close()
        // Native 已创建新 generation 后再通知 UI；冻结前的 session/resource handle 不得继续使用。
        document.dispatchEvent(new Event('termx:resume'))
	  })
    })
    return () => { void promise.then((sub) => sub.remove()) }
  }, [])
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
  console.info('[termx:scan] camera scan requested')
  const existing = document.getElementById(qrScannerRootId)
  existing?.remove()
  const scannerSize = scannerSquareSize()
  const qrboxSize = Math.min(220, Math.max(180, scannerSize - 32))

  const root = document.createElement('div')
  root.id = qrScannerRootId
  root.className = 'termx-app-page fixed inset-0 z-[2147483647] flex flex-col items-stretch px-4 pb-[calc(env(safe-area-inset-bottom)+12px)] pt-[calc(env(safe-area-inset-top)+12px)]'

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
  title.textContent = 'Scan TermX QR'
  title.className = 'text-[17px] font-bold tracking-tight text-zinc-900'

  const cancelButton = document.createElement('button')
  cancelButton.type = 'button'
  cancelButton.textContent = 'Cancel'
  cancelButton.className = 'termx-app-secondary-button px-4 text-[14px] font-semibold'

  const manualContainer = document.createElement('div')
  manualContainer.className = 'mt-auto flex flex-col gap-3'

  const manualInput = document.createElement('textarea')
  manualInput.placeholder = 'Or enter TermX QR content manually...'
  manualInput.className = 'h-[90px] w-full resize-none border border-[var(--termx-app-line)] bg-white p-3 font-mono text-[13px] text-zinc-900 outline-none focus:border-[var(--termx-app-accent)] focus:ring-1 focus:ring-[var(--termx-app-accent)]'

  const manualSubmit = document.createElement('button')
  manualSubmit.type = 'button'
  manualSubmit.textContent = 'Add Device'
  manualSubmit.className = 'termx-app-primary-button min-h-12 w-full px-4 text-[15px] font-semibold disabled:opacity-50'
  manualSubmit.disabled = true

  manualInput.oninput = () => {
    const hasValue = manualInput.value.trim().length > 0
    manualSubmit.disabled = !hasValue
  }

  manualContainer.append(manualInput, manualSubmit)

  const reader = document.createElement('div')
  reader.id = qrScannerReaderId
  reader.className = 'mt-4 self-center overflow-hidden border border-[var(--termx-app-line)] bg-black'
  reader.style.width = `${scannerSize}px`
  reader.style.height = `${scannerSize}px`
  reader.style.minWidth = `${scannerSize}px`
  reader.style.minHeight = `${scannerSize}px`
  reader.style.maxWidth = `${scannerSize}px`
  reader.style.maxHeight = `${scannerSize}px`

  const hint = document.createElement('div')
  hint.textContent = 'Point the camera at the QR code shown on the TermX device.'
  hint.className = 'mt-4 px-4 text-center text-[13px] font-medium leading-[20px] text-zinc-500'

  header.append(title, cancelButton)
  root.append(scannerStyle, header, reader, hint, manualContainer)
  document.body.append(root)

  const scanner = new Html5Qrcode(qrScannerReaderId)
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
        console.warn('[termx:scan] camera scan failed', error instanceof Error ? error.message : String(error))
        reject(error instanceof Error ? error : new Error(String(error)))
        return
      }
      console.info(value ? '[termx:scan] QR decoded' : '[termx:scan] scan cancelled')
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

function createNativeAppRuntime(): {
  createMachineRuntime: MachineRuntimeFactory
  fileTransfer: FileTransferContext
} {
  const transferStore = new NativeFileTransferStore()
  const sessionManagers = new Map<string, NativeSessionEntry>()
  transferStore.setSessionResolver(async (machineId) => {
    return await sessionManagers.get(machineId)?.manager.get() ?? null
  })

  return {
    fileTransfer: createFileTransferContext(undefined, transferStore),
    createMachineRuntime(input) {
      return createNativeMachineRuntime(input.machine, input.storage, {
        sessionManagers,
        transferStore,
      })
    },
  }
}

function createNativeMachineRuntime(
  machine: WebControlMachine,
  storage: RemoteRuntimeStorage,
  shared: {
    sessionManagers: Map<string, NativeSessionEntry>
    transferStore: NativeFileTransferStore
  },
): MachineRuntime {
  const machineStore = createMachineStore({ storage })
  const storedMachine = machineStore.getMachine(machine.id)
  const endpointIdentity = [
    machine.id,
    storage.getItem(`termx.endpoint.${machine.id}.deviceFingerprint`)?.trim() ?? '',
    storage.getItem(`termx.endpoint.${machine.id}.grantRef`)?.trim() ?? '',
  ].join('|')
  let entry = shared.sessionManagers.get(machine.id)
  if (!entry || entry.endpointIdentity !== endpointIdentity) {
    void entry?.manager.reset().catch(() => {})
    void entry?.connector.release?.(machine.id).catch(() => {})
    const connector = createNativeConnector(machine, storedMachine, storage)
    entry = {
      endpointIdentity,
      connector,
      manager: createNativeSessionManager(machine.id, connector),
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
      emitNativeRuntimeConnectionState(options, machine.id, 'connecting', 'Fetching terminals...')
      try {
        const response = await session.execute(create(TermxApiApplication.CommandEnvelopeSchema, {
          command: { case: 'terminalList', value: create(TermxApiTerminal.TerminalListCommandSchema) },
        }))
        if (response.result.case !== 'terminalList') throw new Error('terminal list returned no result')
        emitNativeRuntimeConnectionState(options, machine.id, 'connected', 'Connected')
        return normalizeTerminalInventory({
          machine_id: machine.id,
          terminals: response.result.value.terminals.map((terminal) => ({
            terminal_id: terminal.ref?.terminalId ?? '',
            name: terminal.name,
            state: terminal.state === TermxApiTerminal.TerminalState.RUNNING ? 'running' : terminal.state === TermxApiTerminal.TerminalState.EXITED ? 'exited' : 'unknown',
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
        void sessionManager.resetClientOnly(options)
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
      NativeFilePicker.pickFiles({ multiple: true }).then((result) => {
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
  _storedMachine: StoredMachineRecord | null,
  storage: RemoteRuntimeStorage,
): NativeConnector {
  const targetDeviceId = storage.getItem(`termx.endpoint.${machine.id}.targetDeviceId`)?.trim() ?? machine.id
  const deviceFingerprint = storage.getItem(`termx.endpoint.${machine.id}.deviceFingerprint`)?.trim() ?? ''
  const grantRef = storage.getItem(`termx.endpoint.${machine.id}.grantRef`)?.trim() ?? ''
  if (!deviceFingerprint || !grantRef) {
    return {
      async connect() {
        throw new Error('Managed endpoint requires a device fingerprint and grant reference from the new pairing flow')
      },
    }
  }

  const relayModeValue = storage.getItem(`termx.endpoint.${machine.id}.relayMode`)?.trim() ?? ''
  if (relayModeValue && !isNativeRelayMode(relayModeValue)) {
    return {
      async connect() {
        throw new Error('Managed endpoint has an invalid relay mode')
      },
    }
  }

  const connector = new GoBindingConnector(() => goBindingClient, {
    endpointId: machine.id,
    targetDeviceId,
    deviceFingerprint,
    credentialRef: grantRef,
    relayMode: isNativeRelayMode(relayModeValue) ? relayModeValue : 'auto',
  })
  return {
    connect: (target, options) => connector.connect(target, options),
  }
}

function isNativeRelayMode(value: string): value is NativeRelayMode {
  return value === 'auto' || value === 'direct' || value === 'relay_only' || value === 'smart_route'
}

function createNativeSessionManager(machineId: string, connector: NativeConnector) {
  return {
    get: (options?: RtcConnectOptions) => connector.connect({ machineId }, options),
    lease: (options?: RtcConnectOptions): Promise<NativeSessionLease> => connector.connect({ machineId }, options),
    reset: async () => {},
    resetClientOnly: async (_options?: { forceRelay?: boolean }) => {},
  }
}

function createNativeInventoryEvents(
  machineId: string,
  sessionManager: ReturnType<typeof createNativeSessionManager>,
): TerminalInventoryEvents {
  return {
    subscribe(targetMachineId, handler) {
      if (targetMachineId !== machineId) return { close() {} }
      let closed = false
      let subscription: RtcSubscription | null = null
      let session: NativeSessionLease | null = null
      void sessionManager.get().then((connectedSession) => {
        if (closed) {
          void connectedSession.close()
          return
        }
        session = connectedSession
        subscription = connectedSession.subscribeEvents((event) => {
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
): void {
  options?.onStatus?.(statusText)
  options?.onConnectionState?.({
    machineId,
    phase,
    statusText,
    relayInUse: false,
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
