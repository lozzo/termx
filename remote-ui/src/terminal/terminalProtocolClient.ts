import type {
  TerminalResizeControl,
  TerminalResizePolicy,
  TerminalScrollbackPage,
  TerminalSnapshotPayload,
  TerminalProtocolEvent,
  TerminalProtocolChannel,
  TerminalProtocolSession,
} from './terminalClient'
import {
  TERMX_FRAME_TYPES,
  TERMX_PROTOCOL_VERSION,
  decodeTermxFrame,
  decodeHistoryReplayPayload,
  encodeHistoryRequestPayload,
  encodeResizePayload,
  encodeTermxFrame,
  rowsToText,
  screenRowsToPlainText,
  screenRowsToReplay,
  snapshotScrollbackRows,
  snapshotUsesAlternateScreen,
  snapshotToReplay,
  type TermxFrame,
} from './termxProtocol'
import { logTerminalDiagnostic, terminalNow } from './terminalDiagnostics'
import type { ConnectionInfo, RtcBinaryChannel } from '../core/transport'

export interface TerminalProtocolClientOptions {
  channel: RtcBinaryChannel
  machineId: string
  terminalId: string
  connectionInfo: ConnectionInfo
  resizePolicy?: TerminalResizePolicy
  surfaceId?: string | undefined
  handshakeTimeoutMs?: number | undefined
}

interface PendingRequest {
  resolve: (value: unknown) => void
  reject: (err: Error) => void
}

interface PendingHistoryReplay {
  beforeOffset: number
  limit: number
  resolve: (page: TerminalScrollbackPage) => void
  reject: (err: Error) => void
}

const defaultResizePolicy: TerminalResizePolicy = 'follower'
const inputEnsureResizeFallbackMs = 100
const initialSnapshotScrollbackLimit = 1
const streamSnapshotRefreshDelayMs = 100
const streamSnapshotRefreshMinIntervalMs = 500
const streamStatsIntervalMs = 1000
const largeFrameBytes = 64 * 1024

export function createTerminalProtocolClient(options: TerminalProtocolClientOptions): TerminalProtocolSession {
  return new TerminalProtocolClient(options)
}

class TerminalProtocolClient implements TerminalProtocolSession {
  private nextRequestID = 1
  private streamChannel = 0
  private helloDone: Promise<void> | null = null
  private attachDone: Promise<void> | null = null
  private readonly pending = new Map<number, PendingRequest>()
  private pendingHistoryReplay: PendingHistoryReplay | null = null
  private readonly earlyStreamFrames: TermxFrame[] = []
  private readonly subscribers = new Map<string, Set<(event: TerminalProtocolEvent) => void>>()
  private readonly messageSubscription: { close(): void }
  private readonly closeSubscription: { close(): void }
  private resizeControl: TerminalResizeControl = { canResize: false, reason: 'unknown' }
  private ensureResizeAvailable = true
  private closed = false
  private snapshotRefreshTimer: ReturnType<typeof setTimeout> | null = null
  private snapshotRefreshInFlight = false
  private snapshotRefreshQueued = false
  private lastSnapshotRefreshAt = 0
  private receivedFrames = 0
  private receivedBytes = 0
  private outputFrames = 0
  private outputBytes = 0
  private lastStatsLogAt = terminalNow()

  constructor(private readonly options: TerminalProtocolClientOptions) {
    this.messageSubscription = options.channel.onMessage((data) => this.handleFrame(data))
    this.closeSubscription = options.channel.onClose(() => {
      this.log('channel_close_event', { level: 'warn' })
      this.handleChannelClosed()
    })
    this.log('client_created', {
      level: 'info',
      details: {
        readyState: options.channel.readyState,
        path: options.connectionInfo.path,
      },
    })
  }

  async openTerminal(terminalId: string): Promise<TerminalProtocolChannel> {
    this.assertTerminal(terminalId)
    this.log('open_terminal_start', {
      level: 'info',
      details: { readyState: this.options.channel.readyState },
    })
    if (this.options.channel.readyState !== 'open') {
      await this.waitOpen()
    }
    await this.withHandshakeTimeout(this.hello(), 'hello')
    await this.withHandshakeTimeout(this.attach(), 'attach')
    const channel = this.options.channel
    void this.refreshSnapshot().catch(() => {})
    this.log('open_terminal_ready', {
      level: 'info',
      details: {
        streamChannel: this.streamChannel,
        earlyFrames: this.earlyStreamFrames.length,
      },
    })
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

  async loadScrollback(terminalId: string, offset: number, limit: number): Promise<TerminalScrollbackPage> {
    this.assertTerminal(terminalId)
    await this.withHandshakeTimeout(this.hello(), 'hello')
    await this.withHandshakeTimeout(this.attach(), 'attach')
    const normalizedOffset = Math.max(0, Math.floor(offset))
    const normalizedLimit = Math.max(1, Math.floor(limit))
    if (this.pendingHistoryReplay) {
      throw new Error(`terminal history replay already in flight for ${this.options.terminalId}`)
    }
    return await new Promise<TerminalScrollbackPage>((resolve, reject) => {
      const timer = setTimeout(() => {
        if (this.pendingHistoryReplay?.resolve !== resolve) return
        this.pendingHistoryReplay = null
        reject(new Error(`terminal history replay timed out for ${this.options.terminalId}`))
      }, 8000)
      this.pendingHistoryReplay = {
        beforeOffset: normalizedOffset,
        limit: normalizedLimit,
        resolve: (page) => {
          clearTimeout(timer)
          resolve(page)
        },
        reject: (err) => {
          clearTimeout(timer)
          reject(err)
        },
      }
      const payload = encodeHistoryRequestPayload(normalizedOffset, normalizedLimit)
      this.sendFrame(this.streamChannel, TERMX_FRAME_TYPES.historyRequest, payload).catch((err: unknown) => {
        if (this.pendingHistoryReplay?.resolve !== resolve) return
        this.pendingHistoryReplay = null
        reject(err instanceof Error ? err : new Error(String(err)))
      })
    })
  }

  closeTerminalChannel(terminalId: string): void {
    this.assertTerminal(terminalId)
    this.closed = true
    this.log('close_terminal_channel', {
      level: 'info',
      details: this.streamStatsDetails(),
    })
    this.clearSnapshotRefreshTimer()
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
        this.log('hello_send')
        this.pending.set(0, {
          resolve: () => {
            this.log('hello_ack')
            resolve()
          },
          reject,
        })
        void this.sendFrame(0, TERMX_FRAME_TYPES.hello, {
          version: TERMX_PROTOCOL_VERSION,
          client: 'termx-local-web',
          capabilities: ['terminal'],
        }).catch(reject)
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
      this.log('attach_request')
      this.attachDone = this.request('attach', {
        terminal_id: this.options.terminalId,
        mode: 'collaborator',
        resize_policy: this.options.resizePolicy ?? defaultResizePolicy,
        stream_mode: 'raw',
        surface_id: this.surfaceId(),
      }).then((result) => {
        const channel = attachChannel(result)
        if (channel <= 0) {
          throw new Error('attach response channel is required')
        }
        this.resizeControl = attachResizeControl(result, this.options.resizePolicy ?? defaultResizePolicy)
        this.emit(this.options.terminalId, { type: 'resizeControl', control: this.resizeControl })
        this.streamChannel = channel
        this.log('attach_ack', {
          level: 'info',
          details: {
            streamChannel: channel,
            resizeControl: this.resizeControl,
            earlyFrames: this.earlyStreamFrames.length,
          },
        })
        this.flushEarlyStreamFrames(channel)
      })
    }
    await this.attachDone
  }

  private request(method: string, params: unknown, timeoutMs?: number): Promise<unknown> {
    if (this.options.channel.readyState !== 'open') {
      return Promise.reject(new Error(`terminal data channel ${this.options.channel.label} is not open`))
    }
    const id = this.nextRequestID++
    const startedAt = terminalNow()
    this.log('request_send', {
      details: {
        id,
        method,
        timeoutMs,
      },
    })
    return new Promise((resolve, reject) => {
      let timer: ReturnType<typeof setTimeout> | undefined
      this.pending.set(id, {
        resolve: (value) => {
          if (timer) clearTimeout(timer)
          this.log('request_resolve', {
            details: {
              id,
              method,
              elapsedMs: Math.round(terminalNow() - startedAt),
              resultBytes: estimateJSONBytes(value),
            },
          })
          resolve(value)
        },
        reject: (err) => {
          if (timer) clearTimeout(timer)
          this.log('request_reject', {
            level: 'warn',
            details: {
              id,
              method,
              elapsedMs: Math.round(terminalNow() - startedAt),
              reason: err.message,
            },
          })
          reject(err)
        },
      })
      if (timeoutMs !== undefined) {
        timer = setTimeout(() => {
          this.pending.delete(id)
          const err = new Error(`terminal protocol ${method} timed out for ${this.options.terminalId}`)
          this.log('request_timeout', {
            level: 'warn',
            details: {
              id,
              method,
              elapsedMs: Math.round(terminalNow() - startedAt),
              pending: this.pending.size,
            },
          })
          reject(err)
        }, timeoutMs)
      }
      void this.sendFrame(0, TERMX_FRAME_TYPES.request, {
        id,
        method,
        params,
      }).catch((err: unknown) => {
        this.pending.delete(id)
        if (timer) clearTimeout(timer)
        reject(err instanceof Error ? err : new Error(String(err)))
      })
    })
  }

  private handleFrame(data: Uint8Array): void {
    this.receivedFrames += 1
    this.receivedBytes += data.byteLength
    const frame = decodeTermxFrame(data)
    if (data.byteLength >= largeFrameBytes) {
      this.log('large_frame_received', {
        level: 'warn',
        details: {
          bytes: data.byteLength,
          channel: frame.channel,
          type: frame.type,
        },
      })
    }
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
        this.outputFrames += 1
        this.outputBytes += frame.payload.byteLength
        this.maybeLogStreamStats()
        this.emit(this.options.terminalId, { type: 'output', data: frame.payload })
        return
      case TERMX_FRAME_TYPES.resize:
        return
      case TERMX_FRAME_TYPES.screenUpdate:
        // The local terminal is xterm.js, so steady-state rendering must be driven by
        // raw PTY output. Replaying server screen snapshots here resets xterm state and
        // can lock up full-screen programs with large output.
        return
      case TERMX_FRAME_TYPES.syncLost:
        this.log('sync_lost', {
          level: 'warn',
          details: this.streamStatsDetails(),
        })
        this.scheduleSnapshotRefresh()
        return
      case TERMX_FRAME_TYPES.bootstrapDone:
        return
      case TERMX_FRAME_TYPES.error:
        this.log('stream_error', {
          level: 'warn',
          details: { message: streamErrorMessage(frame.payload) },
        })
        this.handleChannelClosed(streamErrorMessage(frame.payload))
        return
      case TERMX_FRAME_TYPES.closed:
        this.log('stream_closed', {
          level: 'warn',
          details: { reason: closedReason(frame.payload) },
        })
        this.emitClosed(closedReason(frame.payload))
        return
      case TERMX_FRAME_TYPES.historyReplay: {
        const pending = this.pendingHistoryReplay
        if (!pending) return
        this.pendingHistoryReplay = null
        try {
          const { rows, hasMore, replay } = decodeHistoryReplayPayload(frame.payload)
          pending.resolve({
            beforeOffset: pending.beforeOffset,
            limit: pending.limit,
            rows,
            hasMore,
            replay: new TextDecoder().decode(replay),
          })
        } catch (error) {
          pending.reject(error instanceof Error ? error : new Error(String(error)))
        }
        return
      }
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
        void this.ensureResizeForInput(message).then(() => {
          try {
            this.sendInputFrame(message.data)
          } catch (err) {
            this.handleAsyncSendFailure(err)
          }
        })
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
    if (this.streamChannel <= 0) {
      throw new Error('terminal protocol stream is not attached')
    }
    if (this.options.channel.readyState !== 'open') {
      throw new Error(`terminal data channel ${this.options.channel.label} is not open`)
    }
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
        surface_id: this.surfaceId(),
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

  private scheduleSnapshotRefresh(): void {
    if (this.closed) return
    this.snapshotRefreshQueued = true
    if (this.snapshotRefreshTimer !== null || this.snapshotRefreshInFlight) return
    const elapsed = Date.now() - this.lastSnapshotRefreshAt
    const intervalDelay = this.lastSnapshotRefreshAt > 0
      ? Math.max(0, streamSnapshotRefreshMinIntervalMs - elapsed)
      : 0
    const delay = Math.max(streamSnapshotRefreshDelayMs, intervalDelay)
    this.snapshotRefreshTimer = setTimeout(() => {
      this.snapshotRefreshTimer = null
      void this.refreshQueuedSnapshot()
    }, delay)
  }

  private async refreshQueuedSnapshot(): Promise<void> {
    if (this.closed || !this.snapshotRefreshQueued || this.snapshotRefreshInFlight) return
    try {
      await this.refreshSnapshot()
    } catch {
      // Live stream updates are best-effort; the explicit snapshot request on open remains authoritative.
    }
  }

  private async refreshSnapshot(): Promise<void> {
    this.snapshotRefreshQueued = false
    this.clearSnapshotRefreshTimer()
    this.snapshotRefreshInFlight = true
    try {
      const snapshot = await this.request('snapshot', {
        terminal_id: this.options.terminalId,
        scrollback_offset: 0,
        scrollback_limit: initialSnapshotScrollbackLimit,
      })
      this.lastSnapshotRefreshAt = Date.now()
      this.emit(this.options.terminalId, {
        type: 'snapshot',
        snapshot: normalizeSnapshot(snapshot),
      })
    } finally {
      this.snapshotRefreshInFlight = false
      if (this.snapshotRefreshQueued && !this.closed) {
        this.scheduleSnapshotRefresh()
      }
    }
  }

  private clearSnapshotRefreshTimer(): void {
    if (this.snapshotRefreshTimer === null) return
    clearTimeout(this.snapshotRefreshTimer)
    this.snapshotRefreshTimer = null
  }

  private sendFrame(channel: number, type: number, payload: unknown): Promise<void> {
    const bytes = payload instanceof Uint8Array
      ? payload
      : new TextEncoder().encode(JSON.stringify(payload))
    const frame = encodeTermxFrame(channel, type, bytes)
    if (this.options.channel.readyState === 'open') {
      try {
        this.options.channel.send(frame)
        return Promise.resolve()
      } catch (err) {
        return Promise.reject(err instanceof Error ? err : new Error(String(err)))
      }
    }
    return Promise.reject(new Error(`terminal data channel ${this.options.channel.label} is not open`))
  }

  private emit(terminalId: string, event: TerminalProtocolEvent): void {
    for (const handler of this.subscribers.get(terminalId) ?? []) {
      handler(event)
    }
  }

  async requestResizeOwner(terminalId: string, size?: { cols: number; rows: number }): Promise<TerminalResizeControl> {
    this.assertTerminal(terminalId)
    await this.withHandshakeTimeout(this.hello(), 'hello')
    await this.withHandshakeTimeout(this.attach(), 'attach')
    const result = await this.request('ensure_resize', {
      terminal_id: this.options.terminalId,
      channel: this.streamChannel,
      ...(validTerminalSize(size?.cols, size?.rows) ? { cols: size?.cols, rows: size?.rows } : {}),
      resize_policy: 'owner',
      surface_id: this.surfaceId(),
    }, 8000)
    const control = ensureResizeControl(result) ?? { canResize: false, reason: 'unknown' as const }
    this.resizeControl = control
    this.emit(this.options.terminalId, { type: 'resizeControl', control })
    return control
  }

  async releaseResizeOwner(terminalId: string): Promise<TerminalResizeControl> {
    this.assertTerminal(terminalId)
    await this.withHandshakeTimeout(this.hello(), 'hello')
    await this.withHandshakeTimeout(this.attach(), 'attach')
    const result = await this.request('ensure_resize', {
      terminal_id: this.options.terminalId,
      channel: this.streamChannel,
      resize_policy: 'follower',
      surface_id: this.surfaceId(),
    }, 8000)
    const control = ensureResizeControl(result) ?? { canResize: false, reason: 'follower' as const }
    this.resizeControl = control
    this.emit(this.options.terminalId, { type: 'resizeControl', control })
    return control
  }

  private surfaceId(): string {
    return this.options.surfaceId || `app:terminal:${this.options.terminalId}`
  }

  private bufferEarlyStreamFrame(frame: TermxFrame): void {
    this.earlyStreamFrames.push(frame)
    if (this.earlyStreamFrames.length === 1 || this.earlyStreamFrames.length % 25 === 0) {
      this.log('early_stream_frame_buffered', {
        level: 'debug',
        details: {
          count: this.earlyStreamFrames.length,
          channel: frame.channel,
          type: frame.type,
          payloadBytes: frame.payload.byteLength,
        },
      })
    }
  }

  private flushEarlyStreamFrames(channel: number): void {
    const pending = this.earlyStreamFrames.splice(0)
    if (pending.length > 0) {
      this.log('early_stream_frames_flush', {
        details: {
          count: pending.length,
          channel,
        },
      })
    }
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
    this.log('channel_closed', {
      level: 'warn',
      details: {
        reason,
        ...this.streamStatsDetails(),
      },
    })
    this.rejectPending(new Error(reason ?? `terminal data channel ${this.options.channel.label} closed`))
    this.emitClosed(reason)
  }

  private handleAsyncSendFailure(err: unknown): void {
    this.handleChannelClosed(err instanceof Error ? err.message : String(err))
  }

  private rejectPending(err: Error): void {
    if (this.pending.size > 0) {
      this.log('pending_rejected', {
        level: 'warn',
        details: {
          count: this.pending.size,
          reason: err.message,
        },
      })
    }
    for (const [id, pending] of Array.from(this.pending.entries())) {
      this.pending.delete(id)
      pending.reject(err)
    }
    const pendingHistoryReplay = this.pendingHistoryReplay
    this.pendingHistoryReplay = null
    pendingHistoryReplay?.reject(err)
  }

  private assertTerminal(terminalId: string): void {
    if (this.options.connectionInfo.machineId !== this.options.machineId) {
      throw new Error(`terminal protocol machine mismatch: ${this.options.connectionInfo.machineId} != ${this.options.machineId}`)
    }
    if (terminalId !== this.options.terminalId) {
      throw new Error(`terminal protocol terminal mismatch: ${terminalId} != ${this.options.terminalId}`)
    }
  }

  private maybeLogStreamStats(): void {
    const now = terminalNow()
    if (now - this.lastStatsLogAt < streamStatsIntervalMs) return
    this.log('stream_stats', {
      details: this.streamStatsDetails(now),
    })
    this.lastStatsLogAt = now
  }

  private streamStatsDetails(now = terminalNow()): Record<string, unknown> {
    const elapsedSeconds = Math.max(0.001, (now - this.lastStatsLogAt) / 1000)
    return {
      receivedFrames: this.receivedFrames,
      receivedBytes: this.receivedBytes,
      outputFrames: this.outputFrames,
      outputBytes: this.outputBytes,
      outputBytesPerSecond: Math.round(this.outputBytes / elapsedSeconds),
      streamChannel: this.streamChannel,
      pendingRequests: this.pending.size,
      readyState: this.options.channel.readyState,
    }
  }

  private log(event: string, input: {
    level?: 'debug' | 'info' | 'warn' | 'error'
    details?: Record<string, unknown> | undefined
  } = {}): void {
    logTerminalDiagnostic(`protocol.${event}`, {
      level: input.level,
      machineId: this.options.machineId,
      terminalId: this.options.terminalId,
      connectionId: this.options.connectionInfo.connectionId,
      channelLabel: this.options.channel.label,
      details: input.details,
    })
  }
}

function estimateJSONBytes(value: unknown): number {
  try {
    return new TextEncoder().encode(JSON.stringify(value)).byteLength
  } catch {
    return 0
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
    raw: record,
    scrollbackRows: snapshotScrollbackRows(record),
    screenText: screenRowsToPlainText(record),
    screenReplay: screenRowsToReplay(record),
    alternateScreen: snapshotUsesAlternateScreen(record),
    ...(replay ? { replay } : {}),
  }
}


function closedReason(payload: Uint8Array): string | undefined {
  if (payload.length === 0) return undefined
  const text = new TextDecoder().decode(payload).trim()
  return text || undefined
}

function streamErrorMessage(payload: Uint8Array): string {
  try {
    const message = JSON.parse(new TextDecoder().decode(payload)) as { error?: { message?: string } }
    return message.error?.message || 'terminal stream error'
  } catch {
    return 'terminal stream error'
  }
}
