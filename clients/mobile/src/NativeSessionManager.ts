import type { ConnectionInfo, ConnectionPolicy, ConnectionPolicyState, MachineConnectionSnapshot, ProtoClientSession, ProtoClientSubscription, ProtoResourceStream, RtcConnectOptions, RtcConnectionStateSnapshot } from '@anytty/ui'
import type { CommandEnvelope, EventEnvelope, ResultEnvelope } from '../../ui/src/generated/apipb/application_pb'
import type { EndpointSessionStamp, ResourceHandle } from '../../ui/src/generated/apipb/common_pb'
import { ConnectionCandidateType, ConnectionObservedPath, ConnectionRouteKind } from '../../ui/src/generated/bindingpb/client_binding_pb'

type ProtoClientSessionCloseHandler = Parameters<ProtoClientSession['subscribeClosed']>[0]
type ProtoClientSessionCloseError = Parameters<ProtoClientSessionCloseHandler>[0]

// Android 总窗口覆盖 route planning、signaling/answer、ICE、鉴权和 Hello；
// Go peer 仍用独立 deadline 约束 answer 之后的 ICE/DataChannel，不能由 UI 提前截断。
const NATIVE_SESSION_READY_TIMEOUT_MS = 45_000

/** NativeSessionConnector 是 Android UI 到 Go binding session 的窄连接入口，不拥有 route 或 generation 真值。 */
export type NativeSessionConnector = {
  connect(input: { machineId: string }, options?: RtcConnectOptions): Promise<ProtoClientSession>
  getConnectionPolicy?(signal?: AbortSignal): Promise<ConnectionPolicyState>
  applyConnectionPolicy?(policy: ConnectionPolicy, signal?: AbortSignal): Promise<void>
  release?(machineId: string): Promise<void>
}

/**
 * NativeSessionManager 是单个 Endpoint 在 Android UI generation 内的 session lease owner。
 *
 * Go Client Engine 仍拥有 route、PeerSession 与 generation 真值；这里仅保证 workspace、inventory
 * 和文件传输复用同一个 binding session handle，避免 UI 消费者各自 openSession 后互相使 generation
 * 失效。页面、terminal 和文件资源的 lease 关闭都不关闭底层 session；reset 只在 Android
 * generation 更换、显式重连或 Endpoint 配置失效时关闭底层 session。
 */
export class NativeSessionManager {
  private session: ProtoClientSession | null = null
  private pending: Promise<ProtoClientSession> | null = null
  private pendingController: AbortController | null = null
  private sessionClosedSubscription: ProtoClientSubscription | null = null
  private epoch = 0
  private reconnectAttempt = 0
  private snapshot: MachineConnectionSnapshot
  private readonly stateListeners = new Set<() => void>()

  readonly connectionState = {
    getSnapshot: (): MachineConnectionSnapshot => this.snapshot,
    subscribe: (listener: () => void): (() => void) => {
      this.stateListeners.add(listener)
      return () => this.stateListeners.delete(listener)
    },
  }

  constructor(
    private readonly machineId: string,
    private readonly connector: NativeSessionConnector,
  ) {
    this.snapshot = idleMachineConnectionSnapshot(machineId)
  }

  /** machineID 仅供 generation owner 释放同一 Endpoint 的 connector 资源。 */
  machineID(): string { return this.machineId }

  /** get 为 inventory 和文件传输取得当前 Endpoint 的独立 UI lease。 */
  get(options?: RtcConnectOptions): Promise<ProtoClientSession> {
    return this.acquire(options)
  }

  /** lease 为 workspace 取得当前 Endpoint 的独立 UI lease。 */
  lease(options?: RtcConnectOptions): Promise<ProtoClientSession> {
    return this.acquire(options)
  }

  /** reset 在 native generation 更换、显式重连或 Endpoint 配置失效时关闭唯一底层 binding session。 */
  async reset(): Promise<void> {
    this.epoch += 1
    const session = this.session
    const pending = this.pending
    const pendingController = this.pendingController
    const sessionClosedSubscription = this.sessionClosedSubscription
    this.session = null
    this.pending = null
    this.pendingController = null
    this.sessionClosedSubscription = null
    sessionClosedSubscription?.close()
    this.publish(idleMachineConnectionSnapshot(this.machineId, this.reconnectAttempt))
    pendingController?.abort(new Error('native session generation changed while connecting'))
    // native generation owner 已经关闭旧 engine；close 只做幂等清理，不能等待已经失联的 bridge。
    void session?.close().catch(() => undefined)
    if (pending) {
      // generation replacement 不能等待旧网络上的 connect 结束；epoch fence 会让迟到 session
      // 在 acquire 链路中自行关闭，这里只补充幂等清理，避免阻塞新 bridge 发布。
      void pending.then((late) => late.close(), () => undefined).catch(() => undefined)
    }
  }

  /** resetClientOnly 响应 UI 重连请求，但仍由 Go 在下一次 openSession 时重新选择 route。 */
  async resetClientOnly(_options?: { forceRelay?: boolean }): Promise<void> {
    await this.reset()
  }

  private async acquire(options?: RtcConnectOptions): Promise<ProtoClientSession> {
    const stopForwarding = this.forwardState(options)
    try {
      if (this.session?.isAlive()) {
        this.publish(connectedMachineConnectionSnapshot(this.machineId, this.session, this.snapshot.forceRelay, this.reconnectAttempt))
        return new NativeSessionLease(this.session)
      }
      if (this.session) {
        this.sessionClosedSubscription?.close()
        this.sessionClosedSubscription = null
        await this.session.close().catch(() => undefined)
        this.session = null
      }
      if (!this.pending) {
        const epoch = this.epoch
        this.reconnectAttempt += 1
        this.publish({
          machineId: this.machineId,
          phase: 'connecting',
          statusText: 'Connecting...',
          connectionInfo: null,
          forceRelay: options?.forceRelay === true,
          relayInUse: options?.forceRelay === true,
          reconnectAttempt: this.reconnectAttempt,
          error: null,
        })
        // 底层 connect 属于 manager，而不是任一 UI lease；单个 consumer 只能取消自己的等待。
        // manager-owned signal 同时约束完整 binding operation，并在 generation reset 时主动释放旧 Go attempt。
        const controller = new AbortController()
        this.pendingController = controller
        const timeout = globalThis.setTimeout(() => controller.abort(new Error('client session timed out')), NATIVE_SESSION_READY_TIMEOUT_MS)
        const connectOptions: RtcConnectOptions = {
          forceRelay: options?.forceRelay,
          signal: controller.signal,
          onStatus: (status) => {
            if (epoch !== this.epoch) return
            this.publish({ ...this.snapshot, statusText: status })
          },
          onConnectionState: (snapshot) => {
            if (epoch !== this.epoch) return
            this.publish(connectionStateSnapshot(this.machineId, snapshot, options?.forceRelay === true, this.reconnectAttempt))
          },
        }
        const pending = this.connector.connect({ machineId: this.machineId }, connectOptions).then(async (opened) => {
          if (epoch !== this.epoch) {
            await opened.close().catch(() => undefined)
            throw new Error('native session generation changed while connecting')
          }
          if (!opened.isAlive()) throw new Error('Go client session is unavailable')
          this.session = opened
          this.sessionClosedSubscription = opened.subscribeClosed((error) => {
            this.handleSessionClosed(epoch, opened, error)
          })
          this.publish(connectedMachineConnectionSnapshot(this.machineId, opened, options?.forceRelay === true, this.reconnectAttempt))
          return opened
        }).catch((error: unknown) => {
          if (epoch === this.epoch) {
            const failure = connectionFailure(error)
            this.publish({
              machineId: this.machineId,
              phase: 'failed',
              statusText: failure.message,
              connectionInfo: null,
              forceRelay: options?.forceRelay === true,
              relayInUse: options?.forceRelay === true,
              reconnectAttempt: this.reconnectAttempt,
              error: failure,
            })
          }
          throw error
        })
        this.pending = pending
        void pending.finally(() => {
          globalThis.clearTimeout(timeout)
          if (this.pendingController === controller) this.pendingController = null
          if (this.pending === pending) this.pending = null
        }).catch(() => undefined)
      }
      const opened = await awaitSessionLease(this.pending, options?.signal)
      if (!opened.isAlive()) throw new Error('Go client session is unavailable')
      return new NativeSessionLease(opened)
    } finally {
      stopForwarding()
    }
  }

  private forwardState(options: RtcConnectOptions | undefined): () => void {
    if (!options?.onConnectionState && !options?.onStatus) return () => {}
    const forward = () => {
      const snapshot = this.snapshot
      options.onStatus?.(snapshot.statusText)
      options.onConnectionState?.({
        machineId: snapshot.machineId,
        phase: snapshot.phase,
        ...(snapshot.connectionInfo?.path ? { path: snapshot.connectionInfo.path } : {}),
        ...(snapshot.connectionInfo?.observedPath ? { observedPath: snapshot.connectionInfo.observedPath } : {}),
        ...(snapshot.connectionInfo?.routeSelectionReason ? { routeSelectionReason: snapshot.connectionInfo.routeSelectionReason } : {}),
        statusText: snapshot.statusText,
        relayInUse: snapshot.relayInUse,
        ...(snapshot.error ? { error: snapshot.error } : {}),
      })
    }
    if (this.snapshot.phase !== 'idle') forward()
    this.stateListeners.add(forward)
    return () => this.stateListeners.delete(forward)
  }

  private publish(snapshot: MachineConnectionSnapshot): void {
    this.snapshot = snapshot
    for (const listener of this.stateListeners) listener()
  }

  private handleSessionClosed(
    epoch: number,
    session: ProtoClientSession,
    error: ProtoClientSessionCloseError,
  ): void {
    if (epoch !== this.epoch || this.session !== session) return
    this.sessionClosedSubscription?.close()
    this.sessionClosedSubscription = null
    this.session = null
    const failure = connectionFailure(error)
    this.publish({
      machineId: this.machineId,
      phase: 'failed',
      statusText: failure.message,
      connectionInfo: null,
      forceRelay: this.snapshot.forceRelay,
      relayInUse: this.snapshot.relayInUse,
      reconnectAttempt: this.reconnectAttempt,
      error: failure,
    })
  }
}

function idleMachineConnectionSnapshot(machineId: string, reconnectAttempt = 0): MachineConnectionSnapshot {
  return {
    machineId,
    phase: 'idle',
    statusText: 'Ready',
    connectionInfo: null,
    forceRelay: false,
    relayInUse: false,
    reconnectAttempt,
    error: null,
  }
}

function connectionStateSnapshot(
  machineId: string,
  snapshot: RtcConnectionStateSnapshot,
  forceRelay: boolean,
  reconnectAttempt: number,
): MachineConnectionSnapshot {
  return {
    machineId,
    phase: snapshot.phase,
    statusText: snapshot.statusText,
    connectionInfo: null,
    forceRelay,
    relayInUse: snapshot.relayInUse,
    reconnectAttempt,
    error: snapshot.phase === 'failed' ? connectionFailure(snapshot.error ?? snapshot.statusText) : null,
  }
}

function connectedMachineConnectionSnapshot(
  machineId: string,
  session: ProtoClientSession,
  forceRelay: boolean,
  reconnectAttempt: number,
): MachineConnectionSnapshot {
  const connection = session.connection
  const observedPath = connection?.observedPath === ConnectionObservedPath.DIRECT
    ? 'direct'
    : connection?.observedPath === ConnectionObservedPath.SINGLE_RELAY
      ? 'single_relay'
      : undefined
  const routeKind: ConnectionInfo['routeKind'] = connection?.routeKind === ConnectionRouteKind.DIRECT
    ? 'direct'
    : connection?.routeKind === ConnectionRouteKind.SSH
      ? 'ssh'
      : connection?.routeKind === ConnectionRouteKind.CLOUD
        ? 'cloud'
        : connection?.routeKind === ConnectionRouteKind.LOCAL
          ? 'local'
          : undefined
  const relayInUse = observedPath === 'single_relay' ||
    connection?.localCandidateType === ConnectionCandidateType.RELAY ||
    connection?.remoteCandidateType === ConnectionCandidateType.RELAY
  const connectionInfo: ConnectionInfo = {
    path: routeKind === 'cloud' ? 'hub' : 'local',
    ...(connection?.routeId || session.stamp.routeId ? { routeId: connection?.routeId || session.stamp.routeId } : {}),
    ...(routeKind ? { routeKind } : {}),
    ...(observedPath ? { observedPath } : {}),
    connectionId: `${session.stamp.endpointId}:${session.stamp.generation}`,
    machineId,
    relayInUse,
    type: relayInUse ? 'relay' : observedPath === 'direct' ? 'p2p' : 'unknown',
    generation: session.stamp.generation,
  }
  return {
    machineId,
    phase: 'connected',
    statusText: 'Connected',
    connectionInfo,
    forceRelay,
    relayInUse,
    reconnectAttempt,
    error: null,
  }
}

class NativeSessionLease implements ProtoClientSession {
  private alive = true
  private readonly subscriptions = new Set<ProtoClientSubscription>()

  constructor(private readonly session: ProtoClientSession) {}

  get stamp(): EndpointSessionStamp { return this.session.stamp }
  get connection() { return this.session.connection }

  execute(command: CommandEnvelope, options?: { signal?: AbortSignal }): Promise<ResultEnvelope> {
    if (!this.isAlive()) return Promise.reject(new Error('Proto session lease is closed'))
    return this.session.execute(command, options)
  }

  subscribeEvents(handler: (event: EventEnvelope) => void): ProtoClientSubscription {
    const subscription = this.requireAlive().subscribeEvents(handler)
    const leaseSubscription: ProtoClientSubscription = {
      close: () => {
        subscription.close()
        this.subscriptions.delete(leaseSubscription)
      },
    }
    this.subscriptions.add(leaseSubscription)
    return leaseSubscription
  }

  subscribeClosed(handler: (error: ProtoClientSessionCloseError) => void): ProtoClientSubscription {
    const subscription = this.requireAlive().subscribeClosed(handler)
    const leaseSubscription: ProtoClientSubscription = {
      close: () => {
        subscription.close()
        this.subscriptions.delete(leaseSubscription)
      },
    }
    this.subscriptions.add(leaseSubscription)
    return leaseSubscription
  }

  openResourceStream(resource: ResourceHandle, options?: { initialUploadOffset?: bigint; signal?: AbortSignal }): Promise<ProtoResourceStream> {
    if (!this.isAlive()) return Promise.reject(new Error('Proto session lease is closed'))
    return this.session.openResourceStream(resource, options)
  }

  isAlive(): boolean { return this.alive && this.session.isAlive() }

  getConnectionSnapshot() {
    if (!this.isAlive()) return Promise.reject(new Error('Proto session lease is closed'))
    return this.session.getConnectionSnapshot?.() ?? Promise.resolve(this.session.connection)
  }

  async close(): Promise<void> {
    if (!this.alive) return
    this.alive = false
    for (const subscription of [...this.subscriptions]) subscription.close()
    this.subscriptions.clear()
  }

  private requireAlive(): ProtoClientSession {
    if (!this.isAlive()) throw new Error('Proto session lease is closed')
    return this.session
  }
}

async function awaitSessionLease(pending: Promise<ProtoClientSession>, signal: AbortSignal | undefined): Promise<ProtoClientSession> {
  if (!signal) return await pending
  if (signal.aborted) throw abortError(signal)
  return await new Promise<ProtoClientSession>((resolve, reject) => {
    let settled = false
    const abort = () => {
      if (settled) return
      settled = true
      signal.removeEventListener('abort', abort)
      reject(abortError(signal))
    }
    signal.addEventListener('abort', abort, { once: true })
    void pending.then(
      (session) => {
        if (settled) return
        settled = true
        signal.removeEventListener('abort', abort)
        resolve(session)
      },
      (error) => {
        if (settled) return
        settled = true
        signal.removeEventListener('abort', abort)
        reject(error)
      },
    )
  })
}

function abortError(signal: AbortSignal): Error {
  return signal.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError')
}

function connectionFailure(error: unknown): ProtoClientSessionCloseError {
  if (error instanceof Error) return error as ProtoClientSessionCloseError
  return new Error(typeof error === 'string' && error.trim() ? error : 'Connection failed')
}
