import type {
  TerminalResizeControl,
  TerminalResizePolicy,
  TerminalSnapshotPayload,
  TerminalTransport,
  TerminalTransportEvent,
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
import type { BinaryChannel, ConnectionInfo } from './transport'

export interface LocalTerminalProtocolTransportOptions {
  channel: BinaryChannel & {
    onMessage?: (handler: (data: Uint8Array) => void) => void
    onClose?: (handler: () => void) => void
    waitOpen?: () => Promise<void>
  }
  machineId: string
  terminalId: string
  connectionInfo: ConnectionInfo
  resizePolicy?: TerminalResizePolicy
}

interface PendingRequest {
  resolve: (value: unknown) => void
  reject: (err: Error) => void
}

export function createLocalTerminalProtocolTransport(options: LocalTerminalProtocolTransportOptions): TerminalTransport {
  return new LocalTerminalProtocolTransport(options)
}

class LocalTerminalProtocolTransport implements TerminalTransport {
  private nextRequestID = 1
  private streamChannel = 0
  private helloDone: Promise<void> | null = null
  private attachDone: Promise<void> | null = null
  private readonly pending = new Map<number, PendingRequest>()
  private readonly earlyStreamFrames: TermxFrame[] = []
  private readonly subscribers = new Map<string, Set<(event: TerminalTransportEvent) => void>>()
  private resizeControl: TerminalResizeControl = { canResize: false, reason: 'unknown' }
  private closed = false

  constructor(private readonly options: LocalTerminalProtocolTransportOptions) {
    options.channel.onMessage?.((data) => this.handleFrame(data))
    options.channel.onClose?.(() => this.emitClosed())
  }

  async openTerminal(terminalId: string): Promise<BinaryChannel> {
    this.assertTerminal(terminalId)
    if (this.options.channel.readyState !== 'open') {
      await this.waitOpen()
    }
    await this.hello()
    await this.attach()
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

  subscribeTerminal(terminalId: string, handler: (event: TerminalTransportEvent) => void): () => void {
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
    this.options.channel.close()
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
    if (!this.options.channel.waitOpen) {
      throw new Error('terminal protocol channel is not open')
    }
    await this.options.channel.waitOpen()
  }

  private async attach(): Promise<void> {
    if (!this.attachDone) {
      this.attachDone = this.request('attach', {
        terminal_id: this.options.terminalId,
        mode: 'collaborator',
        resize_policy: this.options.resizePolicy ?? 'follower',
      }).then((result) => {
        const channel = attachChannel(result)
        if (channel <= 0) {
          throw new Error('attach response channel is required')
        }
        this.resizeControl = attachResizeControl(result, this.options.resizePolicy ?? 'follower')
        this.emit(this.options.terminalId, { type: 'resizeControl', control: this.resizeControl })
        this.streamChannel = channel
        this.flushEarlyStreamFrames(channel)
      })
    }
    await this.attachDone
  }

  private request(method: string, params: unknown): Promise<unknown> {
    const id = this.nextRequestID++
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject })
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
        this.emitClosed()
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
      | { type: 'input'; data: string }
      | { type: 'resize'; cols: number; rows: number }
    if (message.type === 'input') {
      this.options.channel.send(encodeTermxFrame(this.streamChannel, TERMX_FRAME_TYPES.input, new TextEncoder().encode(message.data)))
      return
    }
    if (message.type === 'resize') {
      if (!this.resizeControl.canResize) return
      this.options.channel.send(encodeTermxFrame(this.streamChannel, TERMX_FRAME_TYPES.resize, encodeResizePayload(message.cols, message.rows)))
    }
  }

  private sendFrame(channel: number, type: number, payload: unknown): void {
    const bytes = payload instanceof Uint8Array
      ? payload
      : new TextEncoder().encode(JSON.stringify(payload))
    this.options.channel.send(encodeTermxFrame(channel, type, bytes))
  }

  private emit(terminalId: string, event: TerminalTransportEvent): void {
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

  private emitClosed(): void {
    if (this.closed) return
    this.closed = true
    this.emit(this.options.terminalId, { type: 'closed' })
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
