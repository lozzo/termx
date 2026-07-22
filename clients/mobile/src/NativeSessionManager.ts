import type { ProtoClientSession, ProtoClientSubscription, ProtoResourceStream, RtcConnectOptions } from '@muxvia/ui'
import type { CommandEnvelope, EventEnvelope, ResultEnvelope } from '../../ui/src/generated/apipb/application_pb'
import type { EndpointSessionStamp, ResourceHandle } from '../../ui/src/generated/apipb/common_pb'

/** NativeSessionConnector 是 Android UI 到 Go binding session 的窄连接入口，不拥有 route 或 generation 真值。 */
export type NativeSessionConnector = {
  connect(input: { machineId: string }, options?: RtcConnectOptions): Promise<ProtoClientSession>
  release?(machineId: string): Promise<void>
}

/**
 * NativeSessionManager 是单个 Endpoint 在 Android UI generation 内的 session lease owner。
 *
 * Go Client Engine 仍拥有 route、PeerSession 与 generation 真值；这里仅保证 workspace、inventory
 * 和文件传输复用同一个 binding session handle，避免 UI 消费者各自 openSession 后互相使 generation
 * 失效。reset 只在 Android generation 更换或 machine runtime 销毁时关闭底层 session。
 */
export class NativeSessionManager {
  private session: ProtoClientSession | null = null
  private pending: Promise<ProtoClientSession> | null = null
  private epoch = 0

  constructor(
    private readonly machineId: string,
    private readonly connector: NativeSessionConnector,
  ) {}

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

  /** reset 在 runtime 销毁或 native generation 更换时关闭唯一底层 binding session。 */
  async reset(): Promise<void> {
    this.epoch += 1
    const session = this.session
    const pending = this.pending
    this.session = null
    this.pending = null
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
    if (this.session?.isAlive()) return new NativeSessionLease(this.session)
    if (this.session) {
      await this.session.close().catch(() => undefined)
      this.session = null
    }
    if (!this.pending) {
      const epoch = this.epoch
      // 底层 connect 属于 manager，而不是任一 UI lease；单个调用者只能取消自己的等待。
      const connectOptions = options ? { ...options, signal: undefined } : undefined
      const pending = this.connector.connect({ machineId: this.machineId }, connectOptions).then(async (opened) => {
        if (epoch !== this.epoch) {
          await opened.close().catch(() => undefined)
          throw new Error('native session generation changed while connecting')
        }
        this.session = opened
        return opened
      })
      this.pending = pending
      void pending.finally(() => {
        if (this.pending === pending) this.pending = null
      }).catch(() => undefined)
    }
    const opened = await awaitSessionLease(this.pending, options?.signal)
    if (!opened.isAlive()) throw new Error('Go client session is unavailable')
    return new NativeSessionLease(opened)
  }
}

class NativeSessionLease implements ProtoClientSession {
  private alive = true
  private readonly subscriptions = new Set<ProtoClientSubscription>()

  constructor(private readonly session: ProtoClientSession) {}

  get stamp(): EndpointSessionStamp { return this.session.stamp }

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

  openResourceStream(resource: ResourceHandle, options?: { initialUploadOffset?: bigint; signal?: AbortSignal }): Promise<ProtoResourceStream> {
    if (!this.isAlive()) return Promise.reject(new Error('Proto session lease is closed'))
    return this.session.openResourceStream(resource, options)
  }

  isAlive(): boolean { return this.alive && this.session.isAlive() }

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
