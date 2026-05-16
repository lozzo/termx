import { useEffect, useMemo, useRef } from 'react'
import { App as CapApp } from '@capacitor/app'
import { Capacitor, CapacitorHttp } from '@capacitor/core'
import { Keyboard } from '@capacitor/keyboard'
import { Html5Qrcode } from 'html5-qrcode'
import {
  RemoteControlApp,
  createMachineSessionStore,
  createMachineStore,
  dispatchNativeKeyboardEvent,
  dispatchNativeBack,
  normalizeTerminalInventory,
} from '@termx/remote-ui'
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
  RtcSession,
  RtcSessionConnectionStateEvents,
  RtcSubscription,
  StoredMachineRecord,
  TerminalInventoryEvents,
  WebControlMachine,
  RemoteControlAppProps,
} from '@termx/remote-ui'
import { NativeConnection, type NativeConnectOpts, type NativeConnectionSnapshot, type NativeStateChangeEvent } from './plugins/nativeConnection'
import { NativeRtcConnector, recoverNativeBridgeAfterResume, type NativeRtcSession } from './NativeConnectionProxy'
import { NativeFileTransferStore } from './NativeFileTransferStore'
import NativeFilePicker from './plugins/nativeFilePicker'
import NativeHaptic from './plugins/nativeHaptic'
import { useNativeStatusBarSync } from './nativeStatusBar'

const defaultControlUrl = import.meta.env.VITE_CONTROL_URL || 'http://114.66.58.243:12306'
const qrScannerRootId = 'termx-camera-qr-scanner'
const qrScannerReaderId = 'termx-camera-qr-reader'

type MachineRuntimeFactory = NonNullable<RemoteControlAppProps['machineRuntimeFactory']>
type MachineRuntime = ReturnType<MachineRuntimeFactory>
type NativeSessionManager = ReturnType<typeof createNativeSessionManager>
type NativeSessionLease = RtcSession & {
  isAlive(): boolean
  waitUntilConnected(signal?: AbortSignal): Promise<void>
  closeTerminalDataChannel(terminalId: string): void
  subscribeConnectionState(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription
  onTransferSync: NativeRtcSession['onTransferSync']
  onSyncResponse: NativeRtcSession['onSyncResponse']
  sendTransferRequest: NativeRtcSession['sendTransferRequest']
  sendSyncRequest: NativeRtcSession['sendSyncRequest']
}

export function TermxApp() {
  useAndroidBackButton()
  useAppResumeSync()
  useNativeKeyboardEvents()
  useNativeStatusBarSync()

  const networkRuntime = useMemo(() => createNativeNetworkRuntime(), [])
  const nativeAppRuntime = useMemo(() => createNativeAppRuntime(), [])
  const machineRuntimeFactory = useMemo<MachineRuntimeFactory>(
    () => nativeAppRuntime.createMachineRuntime,
    [nativeAppRuntime],
  )
  const globalFileTransfer = useMemo(
    () => nativeAppRuntime.fileTransfer,
    [nativeAppRuntime],
  )

  return (
    <section className="flex h-[100dvh] w-screen flex-col overflow-hidden bg-[var(--termx-bg,#0c0c0c)] text-[var(--termx-text,#f4f4f5)] antialiased">
      <RemoteControlApp
        defaultControlUrl={defaultControlUrl}
        exportDebugLogs={exportNativeDebugLogs}
        globalFileTransfer={globalFileTransfer}
        machineRuntimeFactory={machineRuntimeFactory}
        networkRuntime={networkRuntime}
        scanPairingCode={scanPairingCode}
      />
    </section>
  )
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
      recoverNativeBridgeAfterResume()
      // Broadcast a visibility change so existing bridge clients can re-sync
      document.dispatchEvent(new Event('termx:resume'))
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
  const existing = document.getElementById(qrScannerRootId)
  existing?.remove()
  const scannerSize = scannerSquareSize()
  const qrboxSize = Math.min(220, Math.max(180, scannerSize - 32))

  const root = document.createElement('div')
  root.id = qrScannerRootId
  root.className = 'fixed inset-0 z-[2147483647] flex flex-col items-stretch bg-zinc-50 text-zinc-900 px-4 pb-[calc(env(safe-area-inset-bottom)+12px)] pt-[calc(env(safe-area-inset-top)+12px)]'

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
  cancelButton.className = 'flex h-10 items-center justify-center rounded-xl bg-zinc-200/50 px-4 text-[14px] font-semibold text-zinc-700 active:bg-zinc-200'

  const manualContainer = document.createElement('div')
  manualContainer.className = 'mt-auto flex flex-col gap-3'

  const manualInput = document.createElement('textarea')
  manualInput.placeholder = 'Or enter TermX QR content manually...'
  manualInput.className = 'h-[90px] w-full resize-none rounded-xl border border-zinc-200 bg-white p-3 font-mono text-[13px] text-zinc-900 shadow-sm outline-none focus:border-zinc-400 focus:ring-1 focus:ring-zinc-400'

  const manualSubmit = document.createElement('button')
  manualSubmit.type = 'button'
  manualSubmit.textContent = 'Add Device'
  manualSubmit.className = 'flex min-h-12 w-full items-center justify-center rounded-xl bg-zinc-900 px-4 text-[15px] font-semibold text-white shadow-md transition-all active:scale-[0.98] active:bg-zinc-800 disabled:opacity-50 disabled:active:scale-100'
  manualSubmit.disabled = true

  manualInput.oninput = () => {
    const hasValue = manualInput.value.trim().length > 0
    manualSubmit.disabled = !hasValue
  }

  manualContainer.append(manualInput, manualSubmit)

  const reader = document.createElement('div')
  reader.id = qrScannerReaderId
  reader.className = 'mt-4 self-center overflow-hidden rounded-2xl bg-black shadow-sm ring-1 ring-zinc-200/60'
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

  return new Promise((resolve, reject) => {
    const finish = (value: string | null, error?: unknown) => {
      if (settled) return
      settled = true
      cancelButton.disabled = true
      void scanner.stop()
        .catch(() => {})
        .then(() => scanner.clear())
        .catch(() => {})
        .finally(() => {
          root.remove()
          if (error) {
            reject(error instanceof Error ? error : new Error(String(error)))
            return
          }
          resolve(value)
        })
    }

  cancelButton.onclick = () => {
      void NativeHaptic.impact({ pattern: 8 }).catch(() => {})
      options?.onCancel?.()
      finish(null)
    }
    manualSubmit.onclick = () => {
      const value = manualInput.value.trim()
      if (!value) return
      void NativeHaptic.impact({ pattern: 10 }).catch(() => {})
      options?.onManualEntry?.()
      finish(value)
    }
    scanner.start(
      { facingMode: 'environment' },
      { fps: 10, qrbox: { width: qrboxSize, height: qrboxSize }, aspectRatio: 1.0 },
      (decodedText) => {
        void NativeHaptic.impact({ pattern: 25 }).catch(() => {})
        finish(decodedText)
      },
      () => {},
    ).catch((error) => finish(null, error))
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
  const sessionManagers = new Map<string, NativeSessionManager>()
  transferStore.setSessionResolver(async (machineId) => {
    const session = await sessionManagers.get(machineId)?.get()
    const nativeSession = session as RtcSession & Partial<NativeRtcSession>
    return typeof nativeSession.onTransferSync === 'function'
      ? nativeSession as NativeRtcSession
      : null
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
    sessionManagers: Map<string, NativeSessionManager>
    transferStore: NativeFileTransferStore
  },
): MachineRuntime {
  const sessionStore = createMachineSessionStore(storage)
  const machineStore = createMachineStore({ storage })
  const storedMachine = machineStore.getMachine(machine.id)
  let sessionManager = shared.sessionManagers.get(machine.id)
  if (!sessionManager) {
    const connector = createNativeConnector(machine, storedMachine, storage)
    sessionManager = createNativeSessionManager(machine.id, connector)
    shared.sessionManagers.set(machine.id, sessionManager)
  }
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
      const sessionToken = sessionStore.getSessionToken(machine.id)
      if (!sessionToken) throw new Error('Pair this machine before opening the runtime channel')
      emitNativeRuntimeConnectionState(options, machine.id, 'connecting', 'Connecting through native runtime...')
      const session = await sessionManager.get({
        forceRelay: options?.forceRelay,
        onStatus: options?.onStatus,
        onConnectionState: options?.onConnectionState,
      })
      const channel = await session.openApi()
      try {
        emitNativeRuntimeConnectionState(options, machine.id, 'connecting', 'Fetching terminals...')
        const response = await channel.request<{ terminals: Record<string, unknown>[] }>('list', {})
        emitNativeRuntimeConnectionState(options, machine.id, 'connected', 'Connected')
        return normalizeTerminalInventory({
          machine_id: machine.id,
          terminals: response.terminals ?? [],
        }).terminals
      } finally {
        channel.close()
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
        // Bind transfer store to the new session when it connects
        sessionPromise.then((session) => {
          const nativeSession = session as RtcSession & Partial<NativeRtcSession>
          if (typeof nativeSession.onTransferSync === 'function') {
            transferStore.setSession(nativeSession as NativeRtcSession)
          }
        }).catch(() => {})
        return sessionPromise
      },
      reconnect(options) {
        void sessionManager.resetClientOnly(options)
      },
    },
    connectionStateEvents: createNativeConnectionStateEvents(machine.id),
    inventoryEvents: createNativeInventoryEvents(machine.id, sessionManager),
    fileTransfer: createFileTransferContext(machine.id, transferStore),
    dispose: () => {},
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
    startDownload(mid, transferId, fileName, fileSize, filePath, offset) {
      store.startDownload(mid, transferId, fileName, fileSize, filePath, offset)
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
  storedMachine: StoredMachineRecord | null,
  storage: RemoteRuntimeStorage,
): NativeRtcConnector {
  const sessionStore = createMachineSessionStore(storage)
  const sessionToken = sessionStore.getSessionToken(machine.id)
  if (!sessionToken) {
    throw new Error('Pair this machine before opening the runtime channel')
  }

  const localAddresses = compactStrings([
    ...(storedMachine?.addresses.local ?? []),
    ...(storedMachine?.addresses.lan ?? []),
  ])
  const hubUrls = compactStrings([
    machine.currentHubUrl,
    ...machine.hubUrls,
    storedMachine?.endpoints.hub,
    ...(storedMachine?.addresses.public ?? []),
  ])
  const preferredPath = preferredNativePath(storedMachine, machine, localAddresses, hubUrls)
  const connectOpts: Omit<NativeConnectOpts, 'machineId'> = {
    localAddresses,
    hubUrls,
    sessionToken,
    answerProofSecret: sessionStore.getAnswerProofSecret(machine.id) ?? undefined,
    preferredPath,
  }
  return new NativeRtcConnector(connectOpts)
}

function createNativeSessionManager(machineId: string, connector: NativeRtcConnector) {
  let sessionPromise: Promise<RtcSession> | null = null
  let currentSession: RtcSession | null = null
  let currentForceRelay = false
  let pendingForceRelay = false
  let connectAbortController: AbortController | null = null
  let resetSeq = 0
  let disconnectSubscription: RtcSubscription | null = null

  const reset = async () => {
    const requestedForceRelay = pendingForceRelay
    resetSeq += 1
    connectAbortController?.abort()
    connectAbortController = null
    const session = currentSession
    currentSession = null
    currentForceRelay = requestedForceRelay
    pendingForceRelay = requestedForceRelay
    sessionPromise = null
    disconnectSubscription?.close()
    disconnectSubscription = null
    await session?.disconnect()
  }

  const resetClientOnly = async (options?: { forceRelay?: boolean | undefined }) => {
    const requestedForceRelay = options?.forceRelay ?? pendingForceRelay
    resetSeq += 1
    connectAbortController?.abort()
    connectAbortController = null
    const session = currentSession
    currentSession = null
    currentForceRelay = requestedForceRelay
    pendingForceRelay = requestedForceRelay
    sessionPromise = null
    disconnectSubscription?.close()
    disconnectSubscription = null
    if (session) closeNativeSessionClientOnly(session)
  }

  const get = async (options?: RtcConnectOptions): Promise<RtcSession> => {
    const signal = options?.signal
    const requestedForceRelay = options?.forceRelay
    const forceRelay = requestedForceRelay ?? (currentForceRelay || pendingForceRelay)
    if (signal?.aborted) throw abortError(signal)
    if (currentSession && isSessionAlive(currentSession) && currentForceRelay === forceRelay) {
      await waitForSessionConnected(currentSession, signal)
      return currentSession
    }
    if (currentSession) await reset()
    if (!sessionPromise) {
      const seq = resetSeq
      const controller = new AbortController()
      connectAbortController = controller
      pendingForceRelay = forceRelay
      const promise = connector.connect({
        machineId,
      }, {
        signal: combineAbortSignals(signal, controller.signal),
        forceRelay,
        onStatus: options?.onStatus,
        onConnectionState: options?.onConnectionState,
      }).then((session) => {
        if (connectAbortController === controller) connectAbortController = null
        if (seq !== resetSeq || controller.signal.aborted) {
          closeNativeSessionClientOnly(session)
          throw new Error('native runtime connection was superseded')
        }
        currentSession = session
        currentForceRelay = forceRelay
        disconnectSubscription?.close()
        disconnectSubscription = attachDisconnectReset(session, () => {
          if (currentSession === session) {
            currentSession = null
            sessionPromise = null
            currentForceRelay = pendingForceRelay
            disconnectSubscription?.close()
            disconnectSubscription = null
          }
        })
        return session
      }).catch((error) => {
        if (connectAbortController === controller) connectAbortController = null
        if (sessionPromise === promise) sessionPromise = null
        pendingForceRelay = currentForceRelay
        throw error
      })
      sessionPromise = promise
    }
    if (pendingForceRelay !== forceRelay) {
      await reset()
      return get(options)
    }
    return sessionPromise
  }

  return {
    get,
    async lease(options?: RtcConnectOptions): Promise<RtcSession> {
      const session = await get(options)
      return createSessionLease(session)
    },
    reset,
    resetClientOnly,
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
      void sessionManager.get().then((session) => {
        if (closed) return
        subscription = session.subscribeEvents((event) => {
          if (isTerminalInventoryEvent(event)) handler({ type: 'inventory_changed', payload: event.payload })
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
    },
  }
}

function createNativeConnectionStateEvents(machineId: string) {
  return {
    subscribe(targetMachineId: string, handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription {
      if (targetMachineId !== machineId) return { close() {} }
      let closed = false
      let listenerHandle: { remove(): void } | null = null
      NativeConnection.addListener('stateChange', (event: NativeStateChangeEvent) => {
        if (!closed && event.machineId === machineId) handler(nativeConnectionStateSnapshot(event))
      }).then((handle) => {
        if (closed) {
          void handle.remove()
          return
        }
        listenerHandle = handle
      }).catch(() => {})
      NativeConnection.getSnapshot({ machineId }).then((snapshot) => {
        if (!closed) handler(nativeConnectionStateSnapshot(snapshot))
      }).catch(() => {})
      return {
        close() {
          closed = true
          void listenerHandle?.remove()
          listenerHandle = null
        },
      }
    },
  }
}

function nativeConnectionStateSnapshot(data: NativeConnectionSnapshot | NativeStateChangeEvent): RtcConnectionStateSnapshot {
  return {
    machineId: data.machineId,
    phase: nativeConnectionPhase(data.phase),
    ...(data.path ? { path: nativeConnectionPath(data.path) } : {}),
    statusText: data.statusText || 'Connecting...',
    relayInUse: data.relayInUse === true,
    ...(data.failReason ? { failReason: data.failReason } : {}),
  }
}

function nativeConnectionPhase(phase: string): RtcConnectionStateSnapshot['phase'] {
  if (
    phase === 'idle' ||
    phase === 'probing' ||
    phase === 'connecting' ||
    phase === 'connected' ||
    phase === 'verifying' ||
    phase === 'reconnecting' ||
    phase === 'waiting_network' ||
    phase === 'failed'
  ) {
    return phase
  }
  return 'connecting'
}

function nativeConnectionPath(path: string): RtcConnectionStateSnapshot['path'] {
  if (path === 'public_p2p' || path === 'managed') return path
  return 'local'
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

function combineAbortSignals(...signals: Array<AbortSignal | undefined>): AbortSignal | undefined {
  const activeSignals = signals.filter((signal): signal is AbortSignal => Boolean(signal))
  if (activeSignals.length === 0) return undefined
  if (activeSignals.length === 1) return activeSignals[0]
  const controller = new AbortController()
  const abort = (signal: AbortSignal) => {
    if (!controller.signal.aborted) controller.abort(signal.reason)
  }
  for (const signal of activeSignals) {
    if (signal.aborted) {
      abort(signal)
      break
    }
    signal.addEventListener('abort', () => abort(signal), { once: true })
  }
  return controller.signal
}

function closeNativeSessionClientOnly(session: RtcSession): void {
  const candidate = session as RtcSession & Partial<{ closeBridgeOnly(): void }>
  if (typeof candidate.closeBridgeOnly === 'function') {
    candidate.closeBridgeOnly()
    return
  }
  void session.disconnect()
}

function createSessionLease(session: RtcSession): NativeSessionLease {
  const subscriptions = new Set<RtcSubscription>()
  const terminalChannels = new Map<string, Awaited<ReturnType<RtcSession['openTerminal']>>>()
  const nativeSession = session as RtcSession & Partial<NativeRtcSession>
  let closed = false
  const closeOnce = () => {
    if (closed) return
    closed = true
    for (const subscription of subscriptions) subscription.close()
    for (const channel of terminalChannels.values()) channel.close()
    subscriptions.clear()
    terminalChannels.clear()
  }

  return {
    async openTerminal(terminalId) {
      const channel = await session.openTerminal(terminalId)
      terminalChannels.set(terminalId, channel)
      channel.onClose(() => terminalChannels.delete(terminalId))
      return channel
    },
    openApi: () => session.openApi(),
    openFileTransfer: (transferId) => session.openFileTransfer(transferId),
    subscribeEvents(handler) {
      const subscription = session.subscribeEvents(handler)
      subscriptions.add(subscription)
      return {
        close() {
          subscription.close()
          subscriptions.delete(subscription)
        },
      }
    },
    getConnectionInfo: () => session.getConnectionInfo(),
    getCapabilities: () => session.getCapabilities(),
    subscribeConnectionState(handler) {
      const connectionState = session as RtcSession & Partial<RtcSessionConnectionStateEvents>
      const subscription = connectionState.subscribeConnectionState?.(handler)
      if (!subscription) return { close() {} }
      subscriptions.add(subscription)
      return {
        close() {
          subscription.close()
          subscriptions.delete(subscription)
        },
      }
    },
    onTransferSync(handler) {
      return nativeSession.onTransferSync?.(handler) ?? (() => {})
    },
    onSyncResponse(handler) {
      return nativeSession.onSyncResponse?.(handler) ?? (() => {})
    },
    sendTransferRequest(request) {
      nativeSession.sendTransferRequest?.(request)
    },
    sendSyncRequest() {
      nativeSession.sendSyncRequest?.()
    },
    async disconnect() {
      closeOnce()
    },
    isAlive() {
      return !closed && isSessionAlive(session)
    },
    waitUntilConnected(signal?: AbortSignal) {
      return waitForSessionConnected(session, signal)
    },
    closeTerminalDataChannel(terminalId: string) {
      terminalChannels.get(terminalId)?.close()
      terminalChannels.delete(terminalId)
      nativeSession.closeTerminalDataChannel?.(terminalId)
    },
  }
}

function preferredNativePath(
  storedMachine: StoredMachineRecord | null,
  machine: WebControlMachine,
  localAddresses: string[],
  hubUrls: string[],
): NativeConnectOpts['preferredPath'] {
  const storedPreference = storedMachine?.preferredPath
  if (storedPreference === 'local' && localAddresses.length > 0) return 'local'
  if ((storedPreference === 'public_p2p' || storedPreference === 'managed') && hubUrls.length > 0) return storedPreference
  if (machine.source === 'cloud' && hubUrls.length > 0) return 'managed'
  if (localAddresses.length > 0) return 'local'
  return 'managed'
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

function isSessionAlive(session: RtcSession): boolean {
  const candidate = session as RtcSession & Partial<{ isAlive(): boolean }>
  return typeof candidate.isAlive === 'function' ? candidate.isAlive() : true
}

function waitForSessionConnected(session: RtcSession, signal?: AbortSignal): Promise<void> {
  const candidate = session as RtcSession & Partial<{ waitUntilConnected(signal?: AbortSignal): Promise<void> }>
  return typeof candidate.waitUntilConnected === 'function'
    ? candidate.waitUntilConnected(signal)
    : Promise.resolve()
}

function attachDisconnectReset(session: RtcSession, handler: () => void): RtcSubscription | null {
  const candidate = session as RtcSession & Partial<{ onDisconnect(handler: () => void): RtcSubscription }>
  return candidate.onDisconnect?.(handler) ?? null
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
