import type {
  TerminalResizeControl,
  TerminalResizePolicy,
  TerminalSnapshotPayload,
  TerminalProtocolEvent,
  TerminalProtocolChannel,
  TerminalProtocolSession,
} from './terminalClient'
import {
  TERMX_FRAME_TYPES,
  TERMX_PROTOCOL_VERSION,
  decodeTermxFrame,
  encodeResizePayload,
  encodeTermxFrame,
  rowsToText,
  snapshotToReplay,
  type TermxFrame,
} from './termxProtocol'
import type { ConnectionInfo, RtcBinaryChannel } from './transport'

export interface TerminalProtocolClientOptions {
  channel: RtcBinaryChannel
  machineId: string
  terminalId: string
  connectionInfo: ConnectionInfo
  resizePolicy?: TerminalResizePolicy
  handshakeTimeoutMs?: number | undefined
}

interface PendingRequest {
  resolve: (value: unknown) => void
  reject: (err: Error) => void
}

const defaultResizePolicy: TerminalResizePolicy = 'owner'
const inputEnsureResizeFallbackMs = 100

export function createTerminalProtocolClient(options: TerminalProtocolClientOptions): TerminalProtocolSession {
  return new TerminalProtocolClient(options)
}

class TerminalProtocolClient implements TerminalProtocolSession {
  private nextRequestID = 1
  private streamChannel = 0
  private helloDone: Promise<void> | null = null
  private attachDone: Promise<void> | null = null
  private readonly pending = new Map<number, PendingRequest>()
  private readonly earlyStreamFrames: TermxFrame[] = []
  private readonly subscribers = new Map<string, Set<(event: TerminalProtocolEvent) => void>>()
  private readonly messageSubscription: { close(): void }
  private readonly closeSubscription: { close(): void }
  private resizeControl: TerminalResizeControl = { canResize: false, reason: 'unknown' }
  private ensureResizeAvailable = true
  private closed = false

  constructor(private readonly options: TerminalProtocolClientOptions) {
    this.messageSubscription = options.channel.onMessage((data) => this.handleFrame(data))
    this.closeSubscription = options.channel.onClose(() => this.handleChannelClosed())
  }

  async openTerminal(terminalId: string): Promise<TerminalProtocolChannel> {
    this.assertTerminal(terminalId)
    if (this.options.channel.readyState !== 'open') {
      await this.waitOpen()
    }
    await this.withHandshakeTimeout(this.hello(), 'hello')
    await this.withHandshakeTimeout(this.attach(), 'attach')
    const channel = this.options.channel
    void this.request('snapshot', {
      terminal_id: this.options.terminalId,
      scrollback_offset: 0,
      scrollback_limit: 500,
    }).then((snapshot) => {
      this.emit(this.options.terminalId, {
        type: 'snapshot',
        snapshot: normalizeSnapshot(snapshot),
      })
    }).catch(() => {})
    return {
      label: `terminal:${terminalId}`,
      get readyState() {
        return channel.readyState
      },
      send: (data) => this.sendTerminalMessage(data),
      close: () => this.closeTerminalChannel(terminalId),
    }
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    return this.options.connectionInfo
  }

  subscribeTerminal(terminalId: string, handler: (event: TerminalProtocolEvent) => void): () => void {
    this.assertTerminal(terminalId)
    let handlers = this.subscribers.get(terminalId)
    if (!handlers) {
      handlers = new Set()
      this.subscribers.set(terminalId, handlers)
    }
    handlers.add(handler)
    return () => {
      handlers?.delete(handler)
    }
  }

  closeTerminalChannel(terminalId: string): void {
    this.assertTerminal(terminalId)
    this.closed = true
    this.messageSubscription.close()
    this.closeSubscription.close()
    this.rejectPending(new Error(`terminal data channel ${this.options.channel.label} closed`))
    this.options.channel.close()
  }

  private withHandshakeTimeout<T>(promise: Promise<T>, stage: string): Promise<T> {
    const timer = setTimeout(() => {
      const err = new Error(`terminal protocol ${stage} timed out for ${this.options.terminalId}`)
      this.rejectPending(err)
      this.options.channel.close()
    }, this.options.handshakeTimeoutMs ?? 8000)
    promise.then(
      () => clearTimeout(timer),
      () => clearTimeout(timer),
    )
    return promise
  }

  private hello(): Promise<void> {
    if (!this.helloDone) {
      this.helloDone = new Promise<void>((resolve, reject) => {
        this.pending.set(0, {
          resolve: () => resolve(),
          reject,
        })
        this.sendFrame(0, TERMX_FRAME_TYPES.hello, {
          version: TERMX_PROTOCOL_VERSION,
          client: 'termx-local-web',
          capabilities: ['terminal'],
        })
      })
    }
    return this.helloDone
  }

  private async waitOpen(): Promise<void> {
    if (this.options.channel.readyState === 'open') return
    await this.options.channel.waitOpen()
  }

  private async attach(): Promise<void> {
    if (!this.attachDone) {
      this.attachDone = this.request('attach', {
        terminal_id: this.options.terminalId,
        mode: 'collaborator',
        resize_policy: this.options.resizePolicy ?? defaultResizePolicy,
      }).then((result) => {
        const channel = attachChannel(result)
        if (channel <= 0) {
          throw new Error('attach response channel is required')
        }
        this.resizeControl = attachResizeControl(result, this.options.resizePolicy ?? defaultResizePolicy)
        this.emit(this.options.terminalId, { type: 'resizeControl', control: this.resizeControl })
        this.streamChannel = channel
        this.flushEarlyStreamFrames(channel)
      })
    }
    await this.attachDone
  }

  private request(method: string, params: unknown, timeoutMs?: number): Promise<unknown> {
    const id = this.nextRequestID++
    return new Promise((resolve, reject) => {
      let timer: ReturnType<typeof setTimeout> | undefined
      this.pending.set(id, {
        resolve: (value) => {
          if (timer) clearTimeout(timer)
          resolve(value)
        },
        reject: (err) => {
          if (timer) clearTimeout(timer)
          reject(err)
        },
      })
      if (timeoutMs !== undefined) {
        timer = setTimeout(() => {
          this.pending.delete(id)
          reject(new Error(`terminal protocol ${method} timed out for ${this.options.terminalId}`))
        }, timeoutMs)
      }
      this.sendFrame(0, TERMX_FRAME_TYPES.request, {
        id,
        method,
        params,
      })
    })
  }

  private handleFrame(data: Uint8Array): void {
    const frame = decodeTermxFrame(data)
    if (frame.channel === 0) {
      this.handleControlFrame(frame.type, frame.payload)
      return
    }
    if (this.streamChannel <= 0) {
      this.bufferEarlyStreamFrame(frame)
      return
    }
    this.handleStreamFrame(frame)
  }

  private handleStreamFrame(frame: TermxFrame): void {
    if (frame.channel !== this.streamChannel) return
    switch (frame.type) {
      case TERMX_FRAME_TYPES.output:
        this.emit(this.options.terminalId, { type: 'output', data: frame.payload })
        return
      case TERMX_FRAME_TYPES.closed:
        this.emitClosed(closedReason(frame.payload))
        return
    }
  }

  private handleControlFrame(type: number, payload: Uint8Array): void {
    if (type === TERMX_FRAME_TYPES.hello) {
      this.pending.get(0)?.resolve(undefined)
      this.pending.delete(0)
      return
    }
    if (type === TERMX_FRAME_TYPES.response) {
      const response = JSON.parse(new TextDecoder().decode(payload)) as { id: number; result?: unknown }
      const pending = this.pending.get(response.id)
      if (!pending) return
      this.pending.delete(response.id)
      pending.resolve(parseProtocolResult(response.result))
      return
    }
    if (type === TERMX_FRAME_TYPES.error) {
      const response = JSON.parse(new TextDecoder().decode(payload)) as { id: number; error?: { message?: string } }
      const pending = this.pending.get(response.id)
      if (!pending) return
      this.pending.delete(response.id)
      pending.reject(new Error(response.error?.message ?? 'termx protocol error'))
    }
  }

  private sendTerminalMessage(data: Uint8Array): void {
    if (this.streamChannel <= 0) {
      throw new Error('terminal protocol stream is not attached')
    }
    const message = JSON.parse(new TextDecoder().decode(data)) as
      | { type: 'input'; data: string; cols?: number; rows?: number }
      | { type: 'resize'; cols: number; rows: number }
    if (message.type === 'input') {
      if (this.shouldEnsureResizeForInput(message)) {
        void this.ensureResizeForInput(message).finally(() => this.sendInputFrame(message.data))
      } else {
        this.sendInputFrame(message.data)
      }
      return
    }
    if (message.type === 'resize') {
      if (!this.resizeControl.canResize) return
      this.options.channel.send(encodeTermxFrame(this.streamChannel, TERMX_FRAME_TYPES.resize, encodeResizePayload(message.cols, message.rows)))
    }
  }

  private sendInputFrame(data: string): void {
    if (this.streamChannel <= 0 || this.options.channel.readyState !== 'open') return
    this.options.channel.send(encodeTermxFrame(this.streamChannel, TERMX_FRAME_TYPES.input, new TextEncoder().encode(data)))
  }

  private shouldEnsureResizeForInput(message: { cols?: number; rows?: number }): boolean {
    return validTerminalSize(message.cols, message.rows) &&
      this.ensureResizeAvailable &&
      (this.options.resizePolicy ?? defaultResizePolicy) === 'owner'
  }

  private async ensureResizeForInput(message: { cols?: number; rows?: number }): Promise<void> {
    try {
      const result = await this.request('ensure_resize', {
        terminal_id: this.options.terminalId,
        channel: this.streamChannel,
        cols: message.cols,
        rows: message.rows,
        resize_policy: this.options.resizePolicy ?? defaultResizePolicy,
      }, inputEnsureResizeFallbackMs)
      const control = ensureResizeControl(result)
      if (control) {
        this.resizeControl = control
        this.emit(this.options.terminalId, { type: 'resizeControl', control })
      }
    } catch {
      this.ensureResizeAvailable = false
      // Input must stay responsive if the connected daemon predates ensure_resize.
    }
  }

  private sendFrame(channel: number, type: number, payload: unknown): void {
    const bytes = payload instanceof Uint8Array
      ? payload
      : new TextEncoder().encode(JSON.stringify(payload))
    this.options.channel.send(encodeTermxFrame(channel, type, bytes))
  }

  private emit(terminalId: string, event: TerminalProtocolEvent): void {
    for (const handler of this.subscribers.get(terminalId) ?? []) {
      handler(event)
    }
  }

  private bufferEarlyStreamFrame(frame: TermxFrame): void {
    this.earlyStreamFrames.push(frame)
  }

  private flushEarlyStreamFrames(channel: number): void {
    const pending = this.earlyStreamFrames.splice(0)
    for (const frame of pending) {
      if (frame.channel === channel) this.handleStreamFrame(frame)
    }
  }

  private emitClosed(reason?: string): void {
    if (this.closed) return
    this.closed = true
    this.emit(this.options.terminalId, { type: 'closed', ...(reason ? { reason } : {}) })
  }

  private handleChannelClosed(reason?: string): void {
    this.rejectPending(new Error(reason ?? `terminal data channel ${this.options.channel.label} closed`))
    this.emitClosed(reason)
  }

  private rejectPending(err: Error): void {
    for (const [id, pending] of Array.from(this.pending.entries())) {
      this.pending.delete(id)
      pending.reject(err)
    }
  }

  private assertTerminal(terminalId: string): void {
    if (this.options.connectionInfo.machineId !== this.options.machineId) {
      throw new Error(`terminal protocol machine mismatch: ${this.options.connectionInfo.machineId} != ${this.options.machineId}`)
    }
    if (terminalId !== this.options.terminalId) {
      throw new Error(`terminal protocol terminal mismatch: ${terminalId} != ${this.options.terminalId}`)
    }
  }
}

function parseProtocolResult(value: unknown): unknown {
  if (typeof value === 'string') {
    return JSON.parse(value) as unknown
  }
  return value
}


function attachChannel(value: unknown): number {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return 0
  const channel = (value as Record<string, unknown>).channel
  return typeof channel === 'number' ? channel : 0
}

function attachResizeControl(value: unknown, requestedPolicy: TerminalResizePolicy): TerminalResizeControl {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return { canResize: false, reason: 'unknown' }
  }
  const result = value as Record<string, unknown>
  const control = result.resize_control
  if (typeof control === 'object' && control !== null && !Array.isArray(control)) {
    const record = control as Record<string, unknown>
    return {
      canResize: record.can_resize === true,
      reason: normalizeResizeReason(record.reason),
      ...(record.size_locked === true ? { sizeLocked: true } : {}),
    }
  }
  return {
    canResize: result.mode === 'collaborator' && requestedPolicy === 'owner',
    reason: result.mode === 'collaborator' && requestedPolicy === 'owner' ? 'owner' : 'follower',
  }
}

function normalizeResizeReason(value: unknown): TerminalResizeControl['reason'] {
  switch (value) {
    case 'owner':
    case 'follower':
    case 'observer':
    case 'size_locked':
      return value
    default:
      return 'unknown'
  }
}

function ensureResizeControl(value: unknown): TerminalResizeControl | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null
  const control = (value as Record<string, unknown>).resize_control
  if (typeof control !== 'object' || control === null || Array.isArray(control)) return null
  return attachResizeControl({ resize_control: control }, defaultResizePolicy)
}

function validTerminalSize(cols: unknown, rows: unknown): cols is number {
  return typeof cols === 'number' &&
    typeof rows === 'number' &&
    Number.isFinite(cols) &&
    Number.isFinite(rows) &&
    cols > 0 &&
    rows > 0
}

function normalizeSnapshot(value: unknown): TerminalSnapshotPayload {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return { text: '', cols: 0, rows: 0 }
  }
  const record = value as Record<string, unknown>
  const size = record.size as Record<string, unknown> | undefined
  const replay = snapshotToReplay(record)
  return {
    text: rowsToText(record),
    cols: typeof size?.cols === 'number' ? size.cols : 0,
    rows: typeof size?.rows === 'number' ? size.rows : 0,
    ...(replay ? { replay } : {}),
  }
}

function closedReason(payload: Uint8Array): string | undefined {
  if (payload.length === 0) return undefined
  const text = new TextDecoder().decode(payload).trim()
  return text || undefined
}
