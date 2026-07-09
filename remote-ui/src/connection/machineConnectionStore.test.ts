import { describe, expect, it, vi } from 'vitest'
import { MachineConnectionStore } from './machineConnectionStore'
import type {
  ConnectionCapabilities,
  ConnectionInfo,
  RtcBinaryChannel,
  RtcConnectionStateSnapshot,
  RtcEvent,
  RtcJsonRpcChannel,
  ManagedRtcSession,
  RtcSubscription,
} from '../core/transport'
import type { RemoteNetworkState, RemoteNetworkStateManager } from './remoteNetworkState'

describe('MachineConnectionStore', () => {
  it('returns disposable leases without disconnecting the app-level machine session', async () => {
    const session = new StoreTestSession('machine-1')
    const connect = vi.fn(async () => session)
    const store = new MachineConnectionStore({
      machineId: 'machine-1',
      connect,
      createLease: createLease,
    })

    const first = await store.get()
    await first.disconnect()
    const second = await store.get()

    expect(connect).toHaveBeenCalledTimes(1)
    expect(second).not.toBe(first)
    expect(session.disconnectCalls).toBe(0)

    await store.release()
    expect(session.disconnectCalls).toBe(1)
  })

  it('reconnects explicitly through the store and releases the previous machine session', async () => {
    const sessions = [new StoreTestSession('machine-1'), new StoreTestSession('machine-1')]
    const first = sessions[0]!
    const connect = vi.fn(async () => {
      const session = sessions.shift()
      if (!session) throw new Error('unexpected reconnect')
      return session
    })
    const store = new MachineConnectionStore({
      machineId: 'machine-1',
      connect,
      createLease,
    })

    await store.get({ forceRelay: false })
    store.reconnect({ forceRelay: true })
    await waitForConnectCount(connect, 2)

    expect(store.getSnapshot().forceRelay).toBe(true)
    expect(first?.disconnectCalls).toBe(1)
    await store.release()
  })

  it('publishes failed session state and schedules reconnect when the active session disconnects', async () => {
    vi.useFakeTimers()
    try {
      const first = new StoreTestSession('machine-1')
      const second = new StoreTestSession('machine-1')
      const connect = vi.fn(async () => connect.mock.calls.length === 1 ? first : second)
      const states: RtcConnectionStateSnapshot[] = []
      const store = new MachineConnectionStore({
        machineId: 'machine-1',
        connect,
        createLease,
      })
      store.subscribeConnectionState((snapshot) => states.push(snapshot))

      await store.get()
      first.emitDisconnect()
      await vi.advanceTimersByTimeAsync(500)

      expect(connect).toHaveBeenCalledTimes(2)
      expect(states.some((state) => state.phase === 'connected')).toBe(true)
      expect(store.getSnapshot().phase).toBe('connected')
      await store.release()
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps logical event subscriptions attached after automatic reconnect', async () => {
    vi.useFakeTimers()
    try {
      const first = new StoreTestSession('machine-1')
      const second = new StoreTestSession('machine-1')
      const connect = vi.fn(async () => connect.mock.calls.length === 1 ? first : second)
      const events: RtcEvent[] = []
      const store = new MachineConnectionStore({
        machineId: 'machine-1',
        connect,
        createLease,
      })

      const subscription = store.subscribeSessionEvents((event) => events.push(event))
      await store.get()
      first.emitEvent({ type: 'inventory_changed', payload: { generation: 1 } })
      first.emitDisconnect()
      await vi.advanceTimersByTimeAsync(500)
      second.emitEvent({ type: 'inventory_changed', payload: { generation: 2 } })

      expect(events.map((event) => event.payload)).toEqual([{ generation: 1 }, { generation: 2 }])
      subscription.close()
      await store.release()
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not publish failed when an in-flight connection is cancelled by explicit reconnect', async () => {
    const firstConnect = deferred<ManagedRtcSession>()
    const second = new StoreTestSession('machine-1')
    const connect = vi.fn((options?: { signal?: AbortSignal }) => {
      if (connect.mock.calls.length === 1) {
        options?.signal?.addEventListener('abort', () => {
          firstConnect.reject(new Error('machine connection reset'))
        }, { once: true })
        return firstConnect.promise
      }
      return Promise.resolve(second)
    })
    const states: RtcConnectionStateSnapshot[] = []
    const store = new MachineConnectionStore({
      machineId: 'machine-1',
      connect,
      createLease,
    })
    store.subscribeConnectionState((snapshot) => states.push(snapshot))

    const firstGet = store.get().catch((err: unknown) => err)
    store.reconnect()
    await waitForConnectCount(connect, 2)
    await firstGet

    expect(states.some((state) => state.phase === 'failed')).toBe(false)
    expect(store.getSnapshot().phase).toBe('connected')
    await store.release()
  })

  it('keeps relay state visible during fresh relay reconnect before connection info resolves', async () => {
    const session = new StoreTestSession('machine-1')
    session.connectionInfoDelay = deferred<ConnectionInfo>()
    const connect = vi.fn(async (options?: { onConnectionState?: (snapshot: RtcConnectionStateSnapshot) => void }) => {
      options?.onConnectionState?.({
        machineId: 'machine-1',
        phase: 'connecting',
        statusText: 'Connecting through relay...',
        relayInUse: true,
      })
      return session
    })
    const states: RtcConnectionStateSnapshot[] = []
    const store = new MachineConnectionStore({
      machineId: 'machine-1',
      connect,
      createLease,
    })
    store.subscribeConnectionState((snapshot) => states.push(snapshot))

    const pending = store.get({ forceRelay: true })
    await waitForConnectCount(connect, 1)

    expect(states.some((state) => state.phase === 'connecting' && state.relayInUse)).toBe(true)
    session.connectionInfoDelay.resolve({
      path: 'hub',
      connectionId: 'conn-machine-1',
      machineId: 'machine-1',
      relayInUse: true,
    })
    await pending
    expect(store.getSnapshot().relayInUse).toBe(true)
    await store.release()
  })

  it('publishes verifying while checking an existing session after network recovery', async () => {
    const session = new StoreTestSession('machine-1')
    const networkStateManager = new TestNetworkStateManager()
    const states: RtcConnectionStateSnapshot[] = []
    const store = new MachineConnectionStore({
      machineId: 'machine-1',
      connect: vi.fn(async () => session),
      createLease,
      networkStateManager: networkStateManager as unknown as RemoteNetworkStateManager,
    })
    store.subscribeConnectionState((snapshot) => states.push(snapshot))

    await store.get()
    networkStateManager.emit(
      networkState({ phoneOnline: true, networkReady: true }),
      networkState({ phoneOnline: false, networkReady: false }),
    )

    expect(states.some((state) => state.phase === 'verifying')).toBe(true)
    await store.release()
  })

  it('times out a stuck resume verification and starts a fresh reconnect', async () => {
    vi.useFakeTimers()
    try {
      const first = new StoreTestSession('machine-1')
      first.apiRequestDelay = deferred<unknown>()
      const second = new StoreTestSession('machine-1')
      const sessions = [first, second]
      const connect = vi.fn(async () => {
        const session = sessions.shift()
        if (!session) throw new Error('unexpected reconnect')
        return session
      })
      const networkStateManager = new TestNetworkStateManager()
      const store = new MachineConnectionStore({
        machineId: 'machine-1',
        connect,
        createLease,
        networkStateManager: networkStateManager as unknown as RemoteNetworkStateManager,
      })

      await store.get()
      networkStateManager.emit(
        networkState({ resumeType: 'normal', resumeDuration: 60_000 }),
        networkState(),
      )
      expect(store.getSnapshot().phase).toBe('verifying')

      await vi.advanceTimersByTimeAsync(8500)
      await vi.runOnlyPendingTimersAsync()
      await waitForConnectCount(connect, 2)

      expect(first.disconnectCalls).toBe(1)
      expect(store.getSnapshot().phase).toBe('connected')
      await store.release()
    } finally {
      vi.useRealTimers()
    }
  })

  it('cancels an in-flight session request when resume recovery starts', async () => {
    const firstConnect = deferred<ManagedRtcSession>()
    const second = new StoreTestSession('machine-1')
    const connect = vi.fn((options?: { signal?: AbortSignal }) => {
      if (connect.mock.calls.length === 1) {
        options?.signal?.addEventListener('abort', () => {
          firstConnect.reject(new Error('aborted stale connection'))
        }, { once: true })
        return firstConnect.promise
      }
      return Promise.resolve(second)
    })
    const networkStateManager = new TestNetworkStateManager()
    const store = new MachineConnectionStore({
      machineId: 'machine-1',
      connect,
      createLease,
      networkStateManager: networkStateManager as unknown as RemoteNetworkStateManager,
    })

    const firstGet = store.get().catch((err: unknown) => err)
    await waitForConnectCount(connect, 1)
    networkStateManager.emit(
      networkState({ resumeType: 'normal', resumeDuration: 60_000 }),
      networkState(),
    )
    await waitForConnectCount(connect, 2)
    const firstResult = await firstGet

    expect(firstResult).toBeInstanceOf(Error)
    expect(store.getSnapshot().phase).toBe('connected')
    await store.release()
  })
})

function createLease(session: ManagedRtcSession): ManagedRtcSession {
  return {
    openTerminal: (terminalId) => session.openTerminal(terminalId),
    openApi: () => session.openApi(),
    openFileTransfer: (transferId) => session.openFileTransfer(transferId),
    subscribeEvents: (handler) => session.subscribeEvents(handler),
    subscribeConnectionState: (handler) => session.subscribeConnectionState(handler),
    onDisconnect: (handler) => session.onDisconnect(handler),
    isAlive: () => session.isAlive(),
    handleAppResume: () => session.handleAppResume(),
    waitUntilConnected: (signal) => session.waitUntilConnected(signal),
    closeTerminalDataChannel: (terminalId) => session.closeTerminalDataChannel(terminalId),
    getConnectionInfo: () => session.getConnectionInfo(),
    getCapabilities: () => session.getCapabilities(),
    async disconnect() {},
  }
}

async function waitForConnectCount(connect: ReturnType<typeof vi.fn>, count: number): Promise<void> {
  for (let i = 0; i < 20; i += 1) {
    if (connect.mock.calls.length >= count) return
    await Promise.resolve()
  }
  throw new Error(`expected ${count} connect calls, saw ${connect.mock.calls.length}`)
}

function deferred<T>(): {
  promise: Promise<T>
  resolve(value: T): void
  reject(error: unknown): void
} {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

class StoreTestSession implements ManagedRtcSession {
  disconnectCalls = 0
  connectionInfoDelay: ReturnType<typeof deferred<ConnectionInfo>> | null = null
  apiRequestDelay: ReturnType<typeof deferred<unknown>> | null = null
  private readonly eventHandlers = new Set<(event: RtcEvent) => void>()
  private readonly disconnectHandlers = new Set<() => void>()
  private readonly connectionStateHandlers = new Set<(snapshot: RtcConnectionStateSnapshot) => void>()

  constructor(private readonly machineId: string) {}

  async openTerminal(terminalId: string): Promise<RtcBinaryChannel> {
    return new StoreTestBinaryChannel(`terminal:${terminalId}`)
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    const delay = this.apiRequestDelay
    return {
      async request<TResponse>(): Promise<TResponse> {
        if (delay) return delay.promise as Promise<TResponse>
        return undefined as TResponse
      },
      close() {},
    }
  }

  async openFileTransfer(transferId: string): Promise<RtcBinaryChannel> {
    return new StoreTestBinaryChannel(`file:${transferId}`)
  }

  subscribeEvents(handler: (event: RtcEvent) => void): RtcSubscription {
    this.eventHandlers.add(handler)
    return { close: () => this.eventHandlers.delete(handler) }
  }

  subscribeConnectionState(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription {
    this.connectionStateHandlers.add(handler)
    handler({
      machineId: this.machineId,
      phase: 'connected',
      statusText: 'Connected',
      relayInUse: false,
    })
    return { close: () => this.connectionStateHandlers.delete(handler) }
  }

  onDisconnect(handler: () => void): RtcSubscription {
    this.disconnectHandlers.add(handler)
    return { close: () => this.disconnectHandlers.delete(handler) }
  }

  isAlive(): boolean {
    return true
  }

  handleAppResume(): Promise<boolean> {
    return Promise.resolve(true)
  }

  waitUntilConnected(): Promise<void> {
    return Promise.resolve()
  }

  closeTerminalDataChannel(): void {}

  async getConnectionInfo(): Promise<ConnectionInfo> {
    if (this.connectionInfoDelay) return this.connectionInfoDelay.promise
    return {
      path: 'hub',
      connectionId: `conn-${this.machineId}`,
      machineId: this.machineId,
      relayInUse: false,
    }
  }

  async getCapabilities(): Promise<ConnectionCapabilities> {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: false,
    }
  }

  async disconnect(): Promise<void> {
    this.disconnectCalls += 1
  }

  emitDisconnect(): void {
    for (const handler of Array.from(this.disconnectHandlers)) handler()
  }

  emitEvent(event: RtcEvent): void {
    for (const handler of Array.from(this.eventHandlers)) handler(event)
  }
}

class StoreTestBinaryChannel implements RtcBinaryChannel {
  readyState: RtcBinaryChannel['readyState'] = 'open'

  constructor(readonly label: string) {}

  send(): void {}

  close(): void {
    this.readyState = 'closed'
  }

  onMessage(): RtcSubscription {
    return { close() {} }
  }

  onClose(): RtcSubscription {
    return { close() {} }
  }

  waitOpen(): Promise<void> {
    return Promise.resolve()
  }
}

class TestNetworkStateManager {
  private listener: ((state: RemoteNetworkState, prevState: RemoteNetworkState) => void) | null = null

  subscribe(listener: (state: RemoteNetworkState, prevState: RemoteNetworkState) => void): () => void {
    this.listener = listener
    return () => {
      if (this.listener === listener) this.listener = null
    }
  }

  emit(state: RemoteNetworkState, prevState: RemoteNetworkState): void {
    this.listener?.(state, prevState)
  }
}

function networkState(overrides: Partial<{
  phoneOnline: boolean
  connectionType: string
  appActive: boolean
  jsFrozenRecovery: boolean
  networkReady: boolean
  resumeType: 'quick' | 'normal' | 'cold' | null
  resumeDuration: number
}> = {}): RemoteNetworkState {
  return {
    phoneOnline: true,
    connectionType: 'wifi',
    appActive: true,
    jsFrozenRecovery: false,
    networkReady: true,
    resumeType: null,
    resumeDuration: 0,
    ...overrides,
  }
}
