import type { RtcBinaryChannel } from '../core/transport'
import {
  TERMX_FRAME_TYPES,
  TERMX_PROTOCOL_VERSION,
  decodeTermxFrame,
  encodeTermxFrame,
  type TermxFrame,
} from './termxProtocol'
import {
  decodeTerminalErrorPayload,
  decodeTerminalHelloPayload,
  decodeTerminalMethodResult,
  decodeTerminalRequestPayload,
  decodeTerminalResponsePayload,
  encodeTerminalErrorEnvelope,
  encodeTerminalHelloPayload,
  encodeTerminalRequestEnvelope,
  encodeTerminalRequestPayload,
  encodeTerminalResponseEnvelope,
} from './terminalWireProtocol'

const requestTimeoutMs = 10_000

/**
 * TermxProtocolMultiplexer 在一个已完成 E2E 授权和 Hello 的物理 protocol transport 上复用控制请求与 terminal stream。
 * request id 和 stream channel 的 owner 都由该 connection-level 对象维护；它不拥有 terminal lifecycle 或 history truth。
 */
export interface TermxProtocolMultiplexer {
  /** request 发送 daemon control method，并按同一 method 的 protobuf result codec 解码。 */
  request<TResponse>(method: string, params?: unknown): Promise<TResponse>
  /** openTerminalChannel 返回只属于一个 terminal protocol client 的虚拟通道。 */
  openTerminalChannel(terminalId: string): Promise<RtcBinaryChannel>
  /** openFileChannel 注册 daemon control response 分配的 session-local 文件流 channel。 */
  openFileChannel(channel: number, transferId: string): RtcBinaryChannel
  /** close 只关闭当前物理连接及其虚拟投影，不修改 daemon terminal。 */
  close(reason?: string): void
}

/** createTermxProtocolMultiplexer 为单一可靠有序 `protocol` DataChannel 创建 connection-level 复用器。 */
export function createTermxProtocolMultiplexer(channel: RtcBinaryChannel): TermxProtocolMultiplexer {
  return new ProtocolMultiplexer(channel)
}

interface ApiPending {
  kind: 'api'
  method: string
  resolve(value: unknown): void
  reject(error: Error): void
  timer: ReturnType<typeof setTimeout>
}

interface VirtualPending {
  kind: 'virtual'
  method: string
  originalId: number
  channel: VirtualProtocolChannel
  timer: ReturnType<typeof setTimeout> | undefined
}

type PendingRequest = ApiPending | VirtualPending

class ProtocolMultiplexer implements TermxProtocolMultiplexer {
  private nextRequestId = 1
  private closed = false
  private readonly pending = new Map<number, PendingRequest>()
  private readonly virtualChannels = new Set<VirtualProtocolChannel>()
  private readonly streamOwners = new Map<number, ProtocolStreamOwner>()
  private readonly earlyStreamFrames = new Map<number, TermxFrame[]>()
  private readonly messageSubscription: { close(): void }
  private readonly closeSubscription: { close(): void }

  constructor(private readonly physical: RtcBinaryChannel) {
    this.messageSubscription = physical.onMessage((data) => this.handlePhysicalFrame(data))
    this.closeSubscription = physical.onClose(() => this.close('termx protocol transport closed'))
  }

  async request<TResponse>(method: string, params: unknown = {}): Promise<TResponse> {
    this.assertOpen()
    if (this.physical.readyState !== 'open') await this.physical.waitOpen()
    const id = this.allocateRequestId()
    return await new Promise<TResponse>((resolve, reject) => {
      const timer = setTimeout(() => {
        const pending = this.pending.get(id)
        if (pending?.kind !== 'api') return
        this.pending.delete(id)
        reject(new Error(`termx protocol ${method} timed out`))
      }, requestTimeoutMs)
      this.pending.set(id, {
        kind: 'api',
        method,
        resolve: resolve as (value: unknown) => void,
        reject,
        timer,
      })
      try {
        this.physical.send(encodeTermxFrame(0, TERMX_FRAME_TYPES.request, encodeTerminalRequestPayload(id, method, params)))
      } catch (error) {
        clearTimeout(timer)
        this.pending.delete(id)
        reject(asError(error))
      }
    })
  }

  async openTerminalChannel(terminalId: string): Promise<RtcBinaryChannel> {
    this.assertOpen()
    const normalized = terminalId.trim()
    if (!normalized) throw new Error('terminal id is required')
    if (this.physical.readyState !== 'open') await this.physical.waitOpen()
    const channel = new VirtualProtocolChannel(this, normalized)
    this.virtualChannels.add(channel)
    return channel
  }

  openFileChannel(channel: number, transferId: string): RtcBinaryChannel {
    this.assertOpen()
    if (!Number.isInteger(channel) || channel <= 0 || channel > 0xffff) throw new Error('invalid file stream channel')
    if (!transferId.trim()) throw new Error('file transfer id is required')
    if (this.streamOwners.has(channel)) throw new Error(`termx protocol stream ${channel} already has an owner`)
    const owner = new FileProtocolChannel(this, channel, transferId.trim())
    this.streamOwners.set(channel, owner)
    this.flushEarlyFrames(channel, owner)
    return owner
  }

  close(reason = 'termx protocol multiplexer closed'): void {
    if (this.closed) return
    this.closed = true
    const error = new Error(reason)
    this.messageSubscription.close()
    this.closeSubscription.close()
    for (const pending of this.pending.values()) {
      if (pending.kind === 'api') {
        clearTimeout(pending.timer)
        pending.reject(error)
      } else {
        if (pending.timer) clearTimeout(pending.timer)
      }
    }
    this.pending.clear()
    for (const channel of Array.from(this.virtualChannels)) channel.closeFromOwner(error)
    this.virtualChannels.clear()
    for (const owner of new Set(this.streamOwners.values())) owner.closeFromOwner(error)
    this.streamOwners.clear()
    this.earlyStreamFrames.clear()
    this.physical.close()
  }

  sendVirtual(channel: VirtualProtocolChannel, data: Uint8Array): void {
    this.assertOpen()
    const frame = decodeTermxFrame(data)
    if (frame.channel === 0 && frame.type === TERMX_FRAME_TYPES.hello) {
      const hello = decodeTerminalHelloPayload(frame.payload)
      if (hello.version !== 0 && hello.version !== TERMX_PROTOCOL_VERSION) {
        channel.closeFromOwner(new Error(`unsupported termx protocol version ${hello.version}`))
        return
      }
      channel.emit(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeTerminalHelloPayload({
        version: TERMX_PROTOCOL_VERSION,
        server: 'termx-android-multiplexer',
      })))
      return
    }
    if (frame.channel === 0 && frame.type === TERMX_FRAME_TYPES.request) {
      const request = decodeTerminalRequestPayload(frame.payload)
      const physicalId = this.allocateRequestId()
      const timer = request.method === 'live.invalidation.next'
        ? undefined
        : setTimeout(() => {
            const pending = this.pending.get(physicalId)
            if (pending?.kind !== 'virtual') return
            this.pending.delete(physicalId)
            pending.channel.emit(encodeTermxFrame(0, TERMX_FRAME_TYPES.error, encodeTerminalErrorEnvelope({
              id: pending.originalId,
              code: 503,
              message: `termx protocol ${pending.method} timed out`,
            })))
          }, requestTimeoutMs)
      this.pending.set(physicalId, {
        kind: 'virtual',
        method: request.method,
        originalId: request.id,
        channel,
        timer,
      })
      try {
        this.physical.send(encodeTermxFrame(0, TERMX_FRAME_TYPES.request, encodeTerminalRequestEnvelope({
          id: physicalId,
          method: request.method,
          params: request.params,
        })))
      } catch (error) {
        if (timer) clearTimeout(timer)
        this.pending.delete(physicalId)
        throw asError(error)
      }
      return
    }
    if (frame.channel <= 0 || this.streamOwners.get(frame.channel) !== channel) {
      throw new Error(`termx protocol stream ${frame.channel} does not belong to ${channel.label}`)
    }
    this.physical.send(data)
  }

  closeVirtual(channel: VirtualProtocolChannel): void {
    if (!this.virtualChannels.delete(channel)) return
    for (const [id, pending] of Array.from(this.pending.entries())) {
      if (pending.kind === 'virtual' && pending.channel === channel) {
        if (pending.timer) clearTimeout(pending.timer)
        this.pending.delete(id)
      }
    }
    for (const streamChannel of channel.streamChannels()) {
      this.streamOwners.delete(streamChannel)
      this.earlyStreamFrames.delete(streamChannel)
      if (!this.closed) {
        void this.request('detach', {
          terminal_id: channel.terminalId,
          channel: streamChannel,
          surface_id: `app:terminal:${channel.terminalId}`,
        }).catch(() => {})
      }
    }
  }

  sendFileStream(owner: FileProtocolChannel, data: Uint8Array): void {
    this.assertOpen()
    const frame = decodeTermxFrame(data)
    if (frame.channel !== owner.channel || this.streamOwners.get(frame.channel) !== owner) {
      throw new Error(`termx protocol stream ${frame.channel} does not belong to ${owner.label}`)
    }
    this.physical.send(data)
  }

  closeFileStream(owner: FileProtocolChannel): void {
    if (this.streamOwners.get(owner.channel) === owner) this.streamOwners.delete(owner.channel)
    this.earlyStreamFrames.delete(owner.channel)
  }

  private handlePhysicalFrame(data: Uint8Array): void {
    if (this.closed) return
    let frame: TermxFrame
    try {
      frame = decodeTermxFrame(data)
    } catch {
      this.close('daemon sent an invalid termx protocol frame')
      return
    }
    if (frame.channel === 0) {
      try {
        this.handleControlFrame(frame)
      } catch {
        this.close('daemon sent an invalid termx protocol control frame')
      }
      return
    }
    const owner = this.streamOwners.get(frame.channel)
    if (owner) {
      owner.emit(data)
      return
    }
    const queued = this.earlyStreamFrames.get(frame.channel) ?? []
    queued.push(frame)
    this.earlyStreamFrames.set(frame.channel, queued.slice(-64))
  }

  private handleControlFrame(frame: TermxFrame): void {
    if (frame.type === TERMX_FRAME_TYPES.response || frame.type === TERMX_FRAME_TYPES.responseBinary) {
      const response = decodeTerminalResponsePayload(frame.payload)
      const pending = this.pending.get(response.id)
      if (!pending) return
      this.pending.delete(response.id)
      if (pending.timer) clearTimeout(pending.timer)
      if (pending.kind === 'api') {
        try {
          pending.resolve(decodeTerminalMethodResult(pending.method, response.result))
        } catch (error) {
          pending.reject(asError(error))
          this.close('daemon sent an invalid termx protocol result')
        }
        return
      }
      const result = decodeTerminalMethodResult(pending.method, response.result)
      if (pending.method === 'attach') {
        const streamChannel = attachedStreamChannel(result)
        if (streamChannel <= 0 || this.streamOwners.has(streamChannel)) {
          pending.channel.closeFromOwner(new Error('daemon returned an invalid terminal stream channel'))
          return
        }
        pending.channel.addStreamChannel(streamChannel)
        this.streamOwners.set(streamChannel, pending.channel)
      }
      pending.channel.emit(encodeTermxFrame(0, frame.type, encodeTerminalResponseEnvelope({
        id: pending.originalId,
        result: response.result,
      })))
      for (const streamChannel of pending.channel.streamChannels()) this.flushEarlyFrames(streamChannel, pending.channel)
      return
    }
    if (frame.type === TERMX_FRAME_TYPES.error) {
      const response = decodeTerminalErrorPayload(frame.payload)
      const pending = this.pending.get(response.id)
      if (!pending) return
      this.pending.delete(response.id)
      if (pending.timer) clearTimeout(pending.timer)
      if (pending.kind === 'api') {
        pending.reject(new Error(response.message))
      } else {
        pending.channel.emit(encodeTermxFrame(0, TERMX_FRAME_TYPES.error, encodeTerminalErrorEnvelope({
          ...response,
          id: pending.originalId,
        })))
      }
    }
  }

  private flushEarlyFrames(streamChannel: number, owner: ProtocolStreamOwner): void {
    const frames = this.earlyStreamFrames.get(streamChannel) ?? []
    this.earlyStreamFrames.delete(streamChannel)
    for (const frame of frames) owner.emit(encodeTermxFrame(frame.channel, frame.type, frame.payload))
  }

  private allocateRequestId(): number {
    const id = this.nextRequestId
    this.nextRequestId += 1
    if (!Number.isSafeInteger(id) || id <= 0) throw new Error('termx protocol request id exhausted')
    return id
  }

  private assertOpen(): void {
    if (this.closed || this.physical.readyState === 'closed' || this.physical.readyState === 'closing') {
      throw new Error('termx protocol multiplexer is closed')
    }
  }
}

interface ProtocolStreamOwner {
  emit(data: Uint8Array): void
  closeFromOwner(error: Error): void
}

class VirtualProtocolChannel implements RtcBinaryChannel {
  private closed = false
  private readonly messageHandlers = new Set<(data: Uint8Array) => void>()
  private readonly closeHandlers = new Set<() => void>()
  private readonly ownedStreams = new Set<number>()

  constructor(private readonly owner: ProtocolMultiplexer, readonly terminalId: string) {}

  get label(): string {
    return `terminal:${this.terminalId}`
  }

  get readyState(): RtcBinaryChannel['readyState'] {
    return this.closed ? 'closed' : 'open'
  }

  send(data: Uint8Array): void {
    if (this.closed) throw new Error(`terminal protocol channel ${this.label} is closed`)
    this.owner.sendVirtual(this, data)
  }

  close(): void {
    if (this.closed) return
    this.closed = true
    this.owner.closeVirtual(this)
    for (const handler of this.closeHandlers) handler()
    this.messageHandlers.clear()
    this.closeHandlers.clear()
  }

  onMessage(handler: (data: Uint8Array) => void): { close(): void } {
    this.messageHandlers.add(handler)
    return { close: () => this.messageHandlers.delete(handler) }
  }

  onClose(handler: () => void): { close(): void } {
    this.closeHandlers.add(handler)
    return { close: () => this.closeHandlers.delete(handler) }
  }

  async waitOpen(): Promise<void> {
    if (this.closed) throw new Error(`terminal protocol channel ${this.label} is closed`)
  }

  addStreamChannel(channel: number): void {
    this.ownedStreams.add(channel)
  }

  streamChannels(): number[] {
    return Array.from(this.ownedStreams)
  }

  emit(data: Uint8Array): void {
    if (this.closed) return
    queueMicrotask(() => {
      if (this.closed) return
      for (const handler of this.messageHandlers) handler(data)
    })
  }

  closeFromOwner(error: Error): void {
    if (this.closed) return
    this.closed = true
    this.owner.closeVirtual(this)
    void error
    for (const handler of this.closeHandlers) handler()
    this.messageHandlers.clear()
    this.closeHandlers.clear()
  }
}

class FileProtocolChannel implements RtcBinaryChannel, ProtocolStreamOwner {
  private closed = false
  private readonly messageHandlers = new Set<(data: Uint8Array) => void>()
  private readonly closeHandlers = new Set<() => void>()

  constructor(private readonly owner: ProtocolMultiplexer, readonly channel: number, readonly transferId: string) {}

  get label(): string { return `file:${this.transferId}` }
  get readyState(): RtcBinaryChannel['readyState'] { return this.closed ? 'closed' : 'open' }
  send(data: Uint8Array): void { if (this.closed) throw new Error(`file protocol channel ${this.label} is closed`); this.owner.sendFileStream(this, data) }
  close(): void { if (this.closed) return; this.closed = true; this.owner.closeFileStream(this); for (const handler of this.closeHandlers) handler(); this.messageHandlers.clear(); this.closeHandlers.clear() }
  onMessage(handler: (data: Uint8Array) => void): { close(): void } { this.messageHandlers.add(handler); return { close: () => this.messageHandlers.delete(handler) } }
  onClose(handler: () => void): { close(): void } { this.closeHandlers.add(handler); return { close: () => this.closeHandlers.delete(handler) } }
  async waitOpen(): Promise<void> { if (this.closed) throw new Error(`file protocol channel ${this.label} is closed`) }
  emit(data: Uint8Array): void { if (this.closed) return; queueMicrotask(() => { if (!this.closed) for (const handler of this.messageHandlers) handler(data) }) }
  closeFromOwner(error: Error): void { if (this.closed) return; this.closed = true; void error; for (const handler of this.closeHandlers) handler(); this.messageHandlers.clear(); this.closeHandlers.clear() }
}

function attachedStreamChannel(value: unknown): number {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return 0
  const channel = (value as Record<string, unknown>).channel
  return typeof channel === 'number' && Number.isInteger(channel) ? channel : 0
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error))
}
