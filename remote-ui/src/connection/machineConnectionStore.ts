import {
  connectionPhaseLabel,
  connectionSnapshotFromStatus,
  createConnectionStatePublisher,
} from './connectionState'
import type { RemoteNetworkState, RemoteNetworkStateManager } from './remoteNetworkState'
import type {
  ConnectionInfo,
  RtcConnectOptions,
  RtcConnectionPhase,
  RtcConnectionStateSnapshot,
  RtcEvent,
  RtcSession,
  RtcSessionConnectionStateEvents,
  RtcSessionLiveness,
  RtcSubscription,
} from '../core/transport'

export interface MachineConnectionSnapshot {
  machineId: string
  phase: RtcConnectionPhase
  statusText: string
  session: RtcSession | null
  connectionInfo: ConnectionInfo | null
  forceRelay: boolean
  relayInUse: boolean
  reconnectAttempt: number
  error: string | null
}

export interface MachineConnectionStoreOptions {
  machineId: string
  connect(options?: RtcConnectOptions): Promise<RtcSession>
  createLease?: ((session: RtcSession) => RtcSession) | undefined
  networkStateManager?: RemoteNetworkStateManager | undefined
}

type SnapshotListener = () => void

interface LogicalEventSubscription {
  handler: (event: RtcEvent) => void
  inner: RtcSubscription | null
  closed: boolean
}

const RECONNECT_DELAYS_MS = [500, 2000, 4000, 8000, 15000]
const RESUME_RECONNECT_DELAYS_MS = [0, 500, 2000, 4000, 8000]
const MAX_RECONNECT_ATTEMPTS = 20
const RESUME_VERIFY_TIMEOUT_MS = 8500
const CONNECTION_INFO_TIMEOUT_MS = 3000

export class MachineConnectionStore {
  private readonly connectionState = createConnectionStatePublisher()
  private readonly snapshotListeners = new Set<SnapshotListener>()
  private readonly eventSubscriptions = new Set<LogicalEventSubscription>()
  private currentSession: RtcSession | null = null
  private sessionPromise: Promise<RtcSession> | null = null
  private connectionStateSubscription: RtcSubscription | null = null
  private disconnectSubscription: RtcSubscription | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private abortController: AbortController | null = null
  private networkUnsubscribe: (() => void) | null = null
  private released = false
  private currentForceRelay = false
  private pendingForceRelay = false
  private reconnectAttempt = 0
  private resumeReconnect = false
  private verificationGeneration = 0
  private snapshot: MachineConnectionSnapshot

  constructor(private readonly options: MachineConnectionStoreOptions) {
    this.snapshot = {
      machineId: options.machineId,
      phase: 'idle',
      statusText: 'Ready',
      session: null,
      connectionInfo: null,
      forceRelay: false,
      relayInUse: false,
      reconnectAttempt: 0,
      error: null,
    }
    this.networkUnsubscribe = options.networkStateManager?.subscribe((curr, prev) => {
      this.onNetworkStateChange(curr, prev)
    }) ?? null
  }

  getSnapshot(): MachineConnectionSnapshot {
    return this.snapshot
  }

  subscribe(listener: SnapshotListener): () => void {
    this.snapshotListeners.add(listener)
    return () => this.snapshotListeners.delete(listener)
  }

  subscribeConnectionState(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription {
    return this.connectionState.subscribe(handler)
  }

  subscribeSessionEvents(handler: (event: RtcEvent) => void): RtcSubscription {
    const logical: LogicalEventSubscription = {
      handler,
      inner: null,
      closed: false,
    }
    this.eventSubscriptions.add(logical)
    if (this.currentSession) this.bindLogicalEventSubscription(logical, this.currentSession)
    else void this.ensureSession().catch(() => {})
    return {
      close: () => {
        logical.closed = true
        logical.inner?.close()
        logical.inner = null
        this.eventSubscriptions.delete(logical)
      },
    }
  }

  async get(options: RtcConnectOptions = {}): Promise<RtcSession> {
    return this.createLease(await this.ensureSession(options))
  }

  reconnect(options: { forceRelay?: boolean | undefined } = {}): void {
    const forceRelay = options.forceRelay ?? this.currentForceRelay
    this.pendingForceRelay = forceRelay
    this.currentForceRelay = forceRelay
    this.resumeReconnect = false
    this.reconnectAttempt = 0
    this.verificationGeneration += 1
    this.transition({
      phase: 'reconnecting',
      statusText: forceRelay ? 'Reconnecting through relay...' : 'Reconnecting...',
      forceRelay,
      error: null,
    })
    void this.resetCurrentSession({ keepForceRelay: true }).then(() => {
      if (this.released) return
      void this.ensureSession({ forceRelay }, { publishFailure: false }).catch(() => {
        this.scheduleReconnect()
      })
    })
  }

  async release(): Promise<void> {
    if (this.released) return
    this.released = true
    this.networkUnsubscribe?.()
    this.networkUnsubscribe = null
    this.clearReconnectTimer()
    this.abortController?.abort(new Error('machine connection store released'))
    this.abortController = null
    await this.resetCurrentSession({ keepForceRelay: true })
    this.snapshotListeners.clear()
    this.eventSubscriptions.clear()
    this.publishCurrentConnectionState()
  }

  private async ensureSession(options: RtcConnectOptions = {}, behavior: { publishFailure?: boolean } = {}): Promise<RtcSession> {
    if (this.released) throw new Error('machine connection store released')
    const forceRelay = options.forceRelay ?? (this.currentForceRelay || this.pendingForceRelay)
    if (this.currentSession) {
      if (this.currentForceRelay === forceRelay && isRtcSessionAlive(this.currentSession)) {
        return this.currentSession
      }
      await this.resetCurrentSession({ keepForceRelay: true })
    }
    if (this.sessionPromise) {
      if (this.pendingForceRelay === forceRelay) return this.sessionPromise
      await this.resetCurrentSession({ keepForceRelay: true })
    }

    this.pendingForceRelay = forceRelay
    const ac = new AbortController()
    this.abortController = ac
    const signal = combineSignals(options.signal, ac.signal)
    let cancelled = false
    signal.addEventListener('abort', () => {
      cancelled = true
    }, { once: true })
    const promise = this.options.connect({
      ...options,
      forceRelay,
      signal,
      onStatus: (status) => {
        this.publishStatus(status)
        options.onStatus?.(status)
      },
      onConnectionState: (snapshot) => {
        this.publishConnectionSnapshot(snapshot)
        options.onConnectionState?.(snapshot)
      },
    }).then(async (session) => {
      const ownsPromise = this.sessionPromise === promise
      if (this.released || signal.aborted || !ownsPromise) {
        await session.disconnect().catch(() => {})
        throw new Error('machine connection request cancelled')
      }
      this.sessionPromise = null
      this.abortController = null
      this.currentSession = session
      this.currentForceRelay = forceRelay
      this.pendingForceRelay = forceRelay
      this.reconnectAttempt = 0
      this.resumeReconnect = false
      await this.attachSession(session)
      return session
    }).catch((err: unknown) => {
      const ownsPromise = this.sessionPromise === promise
      if (ownsPromise) {
        this.sessionPromise = null
        this.abortController = null
      }
      if (cancelled || signal.aborted || this.released || !ownsPromise) {
        throw err
      }
      if (behavior.publishFailure === false) {
        throw err
      }
      const message = errorMessage(err)
      this.transition({
        phase: 'failed',
        statusText: message,
        error: message,
        session: null,
        connectionInfo: null,
        forceRelay,
        relayInUse: forceRelay,
      })
      throw err
    })
    this.sessionPromise = promise
    return promise
  }

  private async attachSession(session: RtcSession): Promise<void> {
    this.connectionStateSubscription?.close()
    this.connectionStateSubscription = subscribeSessionConnectionState(session, (snapshot) => {
      this.publishConnectionSnapshot(snapshot)
    })
    this.disconnectSubscription?.close()
    this.disconnectSubscription = subscribeSessionDisconnect(session, () => {
      if (this.currentSession !== session || this.released) return
      this.currentSession = null
      this.connectionStateSubscription?.close()
      this.connectionStateSubscription = null
      this.disconnectSubscription?.close()
      this.disconnectSubscription = null
      this.unbindLogicalEventSubscriptions()
      this.transition({
        phase: 'reconnecting',
        statusText: 'Connection lost. Reconnecting...',
        session: null,
        connectionInfo: null,
        error: null,
      })
      this.scheduleReconnect()
    })
    this.bindLogicalEventSubscriptions(session)
    try {
      const info = await session.getConnectionInfo()
      if (this.currentSession !== session || this.released) return
      this.transition({
        phase: 'connected',
        statusText: 'Connected',
        session,
        connectionInfo: info,
        forceRelay: this.currentForceRelay,
        relayInUse: info.relayInUse,
        reconnectAttempt: 0,
        error: null,
      })
    } catch (err) {
      if (this.currentSession === session && !this.released) {
        await this.handleSessionDead(session, errorMessage(err))
      }
    }
  }

  private publishStatus(status: string): void {
    const snapshot = connectionSnapshotFromStatus({
      machineId: this.options.machineId,
      statusText: status,
    })
    this.publishConnectionSnapshot(snapshot)
  }

  private publishConnectionSnapshot(snapshot: RtcConnectionStateSnapshot): void {
    this.transition({
      phase: snapshot.phase,
      statusText: snapshot.statusText || connectionPhaseLabel(snapshot.phase),
      forceRelay: this.pendingForceRelay,
      relayInUse: snapshot.relayInUse,
      error: snapshot.phase === 'failed' ? snapshot.failReason ?? snapshot.statusText : null,
      connectionInfo: this.snapshot.connectionInfo
        ? {
            ...this.snapshot.connectionInfo,
            ...(snapshot.path ? { path: snapshot.path } : {}),
            relayInUse: snapshot.relayInUse,
          }
        : this.snapshot.connectionInfo,
    })
  }

  private onNetworkStateChange(curr: RemoteNetworkState, prev: RemoteNetworkState): void {
    if (this.released) return
    if (prev.networkReady && !curr.networkReady) {
      this.abortController?.abort(new Error('network unavailable'))
      this.abortController = null
      this.clearReconnectTimer()
      if (this.snapshot.phase !== 'idle') {
        this.transition({
          phase: 'waiting_network',
          statusText: 'Waiting for network...',
          error: null,
        })
      }
      return
    }
    if (curr.jsFrozenRecovery) {
      void this.verifyOrReconnect()
      return
    }
    if (!prev.networkReady && curr.networkReady) {
      void this.verifyOrReconnect()
      return
    }
    if (curr.resumeType && curr.resumeType !== 'quick') {
      void this.verifyOrReconnect()
    }
  }

  private async verifyOrReconnect(): Promise<void> {
    if (this.released) return
    this.clearReconnectTimer()
    this.resumeReconnect = true
    const generation = this.verificationGeneration + 1
    this.verificationGeneration = generation
    const session = this.currentSession
    if (!session) {
      this.reconnectAttempt = 0
      this.transition({
        phase: 'reconnecting',
        statusText: 'Restoring connection...',
        error: null,
      })
      await this.ensureSession({ forceRelay: this.currentForceRelay }, { publishFailure: false }).catch(() => {
        this.scheduleReconnect()
      })
      return
    }
    this.transition({
      phase: 'verifying',
      statusText: 'Verifying connection...',
      error: null,
    })
    try {
      await verifySession(session, RESUME_VERIFY_TIMEOUT_MS)
      if (this.currentSession !== session || this.released || this.verificationGeneration !== generation) return
      const info = await withTimeout(
        session.getConnectionInfo(),
        CONNECTION_INFO_TIMEOUT_MS,
        () => new Error('connection info timed out after resume'),
      )
      if (this.currentSession !== session || this.released || this.verificationGeneration !== generation) return
      this.reconnectAttempt = 0
      this.resumeReconnect = false
      this.transition({
        phase: 'connected',
        statusText: 'Connected',
        connectionInfo: info,
        session,
        relayInUse: info.relayInUse,
        error: null,
      })
    } catch (err) {
      if (this.currentSession !== session || this.released || this.verificationGeneration !== generation) return
      await this.handleSessionDead(session, errorMessage(err))
    }
  }

  private async handleSessionDead(session: RtcSession, message: string): Promise<void> {
    if (this.currentSession !== session || this.released) return
    this.verificationGeneration += 1
    this.currentSession = null
    this.connectionStateSubscription?.close()
    this.connectionStateSubscription = null
    this.disconnectSubscription?.close()
    this.disconnectSubscription = null
    this.unbindLogicalEventSubscriptions()
    await session.disconnect().catch(() => {})
    this.transition({
      phase: 'reconnecting',
      statusText: message || 'Connection lost. Reconnecting...',
      session: null,
      connectionInfo: null,
      error: null,
    })
    this.scheduleReconnect()
  }

  private scheduleReconnect(): void {
    if (this.released || this.reconnectTimer) return
    if (this.reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
      const message = 'Connection failed after repeated retries'
      this.resumeReconnect = false
      this.transition({
        phase: 'failed',
        statusText: message,
        error: message,
        session: null,
        connectionInfo: null,
      })
      return
    }
    const attempt = this.reconnectAttempt
    const delays = this.resumeReconnect ? RESUME_RECONNECT_DELAYS_MS : RECONNECT_DELAYS_MS
    const delay = delays[Math.min(attempt, delays.length - 1)] ?? delays[delays.length - 1] ?? 1000
    this.reconnectAttempt += 1
    this.transition({
      phase: 'reconnecting',
      statusText: this.resumeReconnect ? 'Restoring connection...' : `Reconnecting (${attempt + 1}/${MAX_RECONNECT_ATTEMPTS})...`,
      reconnectAttempt: this.reconnectAttempt,
      error: null,
    })
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      if (this.released) return
      void this.ensureSession({ forceRelay: this.currentForceRelay }, { publishFailure: false }).catch(() => {
        this.scheduleReconnect()
      })
    }, delay)
  }

  private async resetCurrentSession(options: { keepForceRelay: boolean }): Promise<void> {
    this.clearReconnectTimer()
    this.abortController?.abort(new Error('machine connection reset'))
    this.abortController = null
    this.verificationGeneration += 1
    const session = this.currentSession
    this.currentSession = null
    this.sessionPromise = null
    this.connectionStateSubscription?.close()
    this.connectionStateSubscription = null
    this.disconnectSubscription?.close()
    this.disconnectSubscription = null
    this.unbindLogicalEventSubscriptions()
    if (!options.keepForceRelay) {
      this.currentForceRelay = false
      this.pendingForceRelay = false
    }
    await session?.disconnect().catch(() => {})
    this.transition({
      session: null,
      connectionInfo: null,
      relayInUse: this.snapshot.forceRelay,
      reconnectAttempt: this.reconnectAttempt,
    })
  }

  private clearReconnectTimer(): void {
    if (!this.reconnectTimer) return
    clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
  }

  private transition(update: Partial<MachineConnectionSnapshot>): void {
    this.snapshot = {
      ...this.snapshot,
      ...update,
      machineId: this.options.machineId,
    }
    for (const listener of this.snapshotListeners) listener()
    this.publishCurrentConnectionState()
  }

  private publishCurrentConnectionState(): void {
    this.connectionState.publish({
      machineId: this.options.machineId,
      phase: this.snapshot.phase,
      ...(this.snapshot.connectionInfo?.path ? { path: this.snapshot.connectionInfo.path } : {}),
      statusText: this.snapshot.statusText || connectionPhaseLabel(this.snapshot.phase),
      relayInUse: this.snapshot.connectionInfo?.relayInUse ?? this.snapshot.relayInUse,
      ...(this.snapshot.phase === 'failed' && this.snapshot.error ? { failReason: this.snapshot.error } : {}),
    })
  }

  private bindLogicalEventSubscriptions(session: RtcSession): void {
    for (const logical of this.eventSubscriptions) {
      this.bindLogicalEventSubscription(logical, session)
    }
  }

  private bindLogicalEventSubscription(logical: LogicalEventSubscription, session: RtcSession): void {
    if (logical.closed || logical.inner) return
    logical.inner = session.subscribeEvents(logical.handler)
  }

  private unbindLogicalEventSubscriptions(): void {
    for (const logical of this.eventSubscriptions) {
      logical.inner?.close()
      logical.inner = null
    }
  }

  private createLease(session: RtcSession): RtcSession {
    return this.options.createLease?.(session) ?? session
  }
}

function subscribeSessionConnectionState(
  session: RtcSession,
  handler: (snapshot: RtcConnectionStateSnapshot) => void,
): RtcSubscription | null {
  const candidate = session as RtcSession & Partial<RtcSessionConnectionStateEvents>
  if (typeof candidate.subscribeConnectionState !== 'function') return null
  return candidate.subscribeConnectionState(handler)
}

function subscribeSessionDisconnect(session: RtcSession, handler: () => void): RtcSubscription | null {
  const candidate = session as RtcSession & Partial<{ onDisconnect(handler: () => void): RtcSubscription }>
  if (typeof candidate.onDisconnect !== 'function') return null
  return candidate.onDisconnect(handler)
}

function isRtcSessionAlive(session: RtcSession): boolean {
  const candidate = session as RtcSession & Partial<RtcSessionLiveness>
  if (typeof candidate.isAlive !== 'function') return true
  return candidate.isAlive()
}

async function verifySession(session: RtcSession, timeoutMs: number): Promise<void> {
  const candidate = session as RtcSession & Partial<{ handleAppResume(): Promise<boolean> }>
  if (typeof candidate.handleAppResume === 'function') {
    const ok = await withTimeout(
      candidate.handleAppResume(),
      timeoutMs,
      () => new Error('connection verification timed out'),
    )
    if (!ok) throw new Error('connection verification failed')
    return
  }
  if (!isRtcSessionAlive(session)) throw new Error('connection is no longer alive')
  const api = await session.openApi()
  await withTimeout(
    api.request('GET', { path: '/status' }),
    timeoutMs,
    () => new Error('connection verification timed out'),
  )
}

function withTimeout<T>(promise: Promise<T>, timeoutMs: number, errorFactory: () => Error): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | null = null
  const timeout = new Promise<T>((_, reject) => {
    timer = setTimeout(() => reject(errorFactory()), timeoutMs)
  })
  return Promise.race([promise, timeout]).finally(() => {
    if (timer) clearTimeout(timer)
  })
}

function combineSignals(...signals: Array<AbortSignal | undefined>): AbortSignal {
  const ac = new AbortController()
  for (const signal of signals) {
    if (!signal) continue
    if (signal.aborted) {
      ac.abort(signal.reason)
      return ac.signal
    }
    signal.addEventListener('abort', () => ac.abort(signal.reason), { once: true })
  }
  return ac.signal
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
