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
  rowsToReplay,
  rowsToText,
  screenRowsToPlainText,
  screenRowsToReplay,
  screenUpdatePayloadToReplay,
  snapshotScrollbackRows,
  snapshotUsesAlternateScreen,
  snapshotToReplay,
  type TermxFrame,
} from './termxProtocol'
import { logTerminalDiagnostic, terminalNow } from './terminalDiagnostics'
import {
  decodeTerminalErrorPayload,
  decodeTerminalHelloPayload,
  decodeTerminalMethodResult,
  decodeTerminalResponsePayload,
  decodeGridViewportPayload,
  encodeTerminalHelloPayload,
  encodeTerminalRequestPayload,
} from './terminalWireProtocol'
import type { ConnectionInfo, RtcBinaryChannel } from '../core/transport'

export interface TerminalProtocolClientOptions {
  channel: RtcBinaryChannel
  machineId: string
  terminalId: string
  connectionInfo: ConnectionInfo
  resizePolicy?: TerminalResizePolicy
  surfaceId?: string | undefined
  handshakeTimeoutMs?: number | undefined
  autoRequestResizeOwner?: boolean | undefined
}

interface PendingRequest {
  method: string
  resolve: (value: unknown) => void
  reject: (err: Error) => void
}

interface PendingHistoryReplay {
  beforeOffset: number
  limit: number
  alternate: boolean
  resolve: (page: TerminalScrollbackPage) => void
  reject: (err: Error) => void
}

const defaultResizePolicy: TerminalResizePolicy = 'follower'
const inputEnsureResizeFallbackMs = 100
const streamSnapshotRefreshDelayMs = 100
const streamSnapshotRefreshMinIntervalMs = 500
const streamSnapshotRefreshMaxIntervalMs = 4000
const streamSnapshotRefreshBackoffResetMs = 10_000
const snapshotRefreshTimeoutMs = 8000
const streamStatsIntervalMs = 1000
const largeFrameBytes = 64 * 1024

type SnapshotRefreshReason = 'open' | 'live_invalidated' | 'sync_lost' | 'manual_sync_lost'

interface SnapshotRefreshRecovery {
  reason: string
  syncLostCount: number
  droppedBytes: number
}

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
  private snapshotRefreshReason: SnapshotRefreshReason | null = null
  private snapshotRecovery: SnapshotRefreshRecovery | null = null
  private recoveryRevision = 0
  private lastSnapshotRefreshAt = 0
  private consecutiveRecoveryRefreshes = 0
  private observedLiveRevision = 0
  private liveInvalidationWatchActive = false
  private receivedFrames = 0
  private receivedBytes = 0
  private screenUpdateFrames = 0
  private screenUpdateBytes = 0
  private lastStatsLogAt = terminalNow()

  constructor(private readonly options: TerminalProtocolClientOptions) {
    this.messageSubscription = options.channel.onMessage((data) => this.handleFrame(data))
    this.closeSubscription = options.channel.onClose(() => {
      const reason = `terminal data channel ${options.channel.label} closed`
      this.log('channel_close_event', { level: 'warn', details: { reason } })
      this.handleChannelClosed(reason)
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
    if (this.options.autoRequestResizeOwner === true && !this.resizeControl.sizeLocked && this.resizeControl.reason !== 'size_locked') {
      try {
        const control = await this.requestResizeOwner(terminalId)
        this.resizeControl = control
      } catch (err) {
        this.log('auto_resize_owner_skipped', {
          level: 'warn',
          details: {
            reason: err instanceof Error ? err.message : String(err),
          },
        })
      }
    }
    const channel = this.options.channel
    void this.refreshSnapshot('open').catch(() => {})
    this.log('open_terminal_ready', {
      level: 'info',
      details: {
        streamChannel: this.streamChannel,
        earlyFrames: this.earlyStreamFrames.length,
        resizeControl: this.resizeControl,
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

  async loadScrollback(terminalId: string, offset: number, limit: number, alternate = false): Promise<TerminalScrollbackPage> {
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
        alternate,
        resolve: (page) => {
          clearTimeout(timer)
          resolve(page)
        },
        reject: (err) => {
          clearTimeout(timer)
          reject(err)
        },
      }
      const payload = encodeHistoryRequestPayload(normalizedOffset, normalizedLimit, alternate)
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

  markSyncLost(terminalId: string, reason = 'terminal stream sync lost'): void {
    this.assertTerminal(terminalId)
    this.log('manual_sync_lost', {
      level: 'warn',
      details: {
        reason,
        ...this.streamStatsDetails(),
      },
    })
    this.scheduleSnapshotRefresh('manual_sync_lost', reason)
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
          method: 'hello',
          resolve: () => {
            this.log('hello_ack')
            resolve()
          },
          reject,
        })
        void this.sendFrame(0, TERMX_FRAME_TYPES.hello, encodeTerminalHelloPayload({
          version: TERMX_PROTOCOL_VERSION,
          client: 'termx-local-web',
        })).catch(reject)
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
        method,
        resolve: (value) => {
          if (timer) clearTimeout(timer)
          this.log('request_resolve', {
            details: {
              id,
              method,
              elapsedMs: Math.round(terminalNow() - startedAt),
              resultBytes: estimateResultBytes(value),
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
      void this.sendFrame(0, TERMX_FRAME_TYPES.request, encodeTerminalRequestPayload(id, method, params)).catch((err: unknown) => {
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
    if (frame.channel !== 0 || frame.type !== TERMX_FRAME_TYPES.response) {
      this.log('frame_received', {
        details: {
          channel: frame.channel,
          type: frame.type,
          payloadBytes: frame.payload.byteLength,
          streamChannel: this.streamChannel,
          receivedFrames: this.receivedFrames,
        },
      })
    }
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
      case TERMX_FRAME_TYPES.resize:
        return
      case TERMX_FRAME_TYPES.screenUpdate: {
        this.screenUpdateFrames += 1
        this.screenUpdateBytes += frame.payload.byteLength
        this.maybeLogStreamStats()
        this.log('screen_update_received', {
          details: {
            payloadBytes: frame.payload.byteLength,
            screenUpdateFrames: this.screenUpdateFrames,
            screenUpdateBytes: this.screenUpdateBytes,
          },
        })
        let replay: string | null = null
        try {
          replay = screenUpdatePayloadToReplay(frame.payload)
        } catch (err) {
          this.log('screen_update_decode_failed', {
            level: 'warn',
            details: {
              message: err instanceof Error ? err.message : String(err),
              payloadBytes: frame.payload.byteLength,
              ...this.streamStatsDetails(),
            },
          })
        }
        if (replay === null) {
          this.scheduleSnapshotRefresh('sync_lost', 'terminal screen update could not be replayed')
          return
        }
        this.log('screen_update_replay', {
          details: {
            replayChars: replay.length,
          },
        })
        if (replay.length > 0) {
          this.emit(this.options.terminalId, { type: 'output', data: new TextEncoder().encode(replay) })
        }
        return
      }
      case TERMX_FRAME_TYPES.syncLost:
        const droppedBytes = decodeSyncLostDroppedBytes(frame.payload)
        this.log('sync_lost', {
          level: 'warn',
          details: {
            droppedBytes,
            ...this.streamStatsDetails(),
          },
        })
        this.scheduleSnapshotRefresh('sync_lost', 'terminal stream sync lost', droppedBytes)
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
          const viewport = decodeGridViewportPayload(replay)
          const viewportRows = historyViewportRows(viewport)
          const loadedRows = historyViewportLoadedRows(viewport, rows)
          pending.resolve({
            beforeOffset: pending.beforeOffset,
            limit: pending.limit,
            rows: loadedRows,
            hasMore,
            ...historyViewportMetadata(viewport),
            alternate: pending.alternate,
            replay: rowsToReplay(viewportRows),
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
      decodeTerminalHelloPayload(payload)
      this.pending.get(0)?.resolve(undefined)
      this.pending.delete(0)
      return
    }
    if (type === TERMX_FRAME_TYPES.response || type === TERMX_FRAME_TYPES.responseBinary) {
      const response = decodeTerminalResponsePayload(payload)
      const pending = this.pending.get(response.id)
      if (!pending) return
      this.pending.delete(response.id)
      pending.resolve(decodeTerminalMethodResult(pending.method, response.result))
      return
    }
    if (type === TERMX_FRAME_TYPES.error) {
      const response = decodeTerminalErrorPayload(payload)
      const pending = this.pending.get(response.id)
      if (!pending) return
      this.pending.delete(response.id)
      pending.reject(new Error(response.message))
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
      this.log('send_terminal_input_message', {
        details: {
          chars: message.data.length,
          cols: message.cols,
          rows: message.rows,
          streamChannel: this.streamChannel,
        },
      })
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
    this.log('send_input_frame', {
      details: {
        chars: data.length,
        streamChannel: this.streamChannel,
      },
    })
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

  private scheduleSnapshotRefresh(
    reason: SnapshotRefreshReason = 'sync_lost',
    recoveryReason = 'terminal stream sync lost',
    droppedBytes = 0,
  ): void {
    if (this.closed) return
    this.snapshotRefreshQueued = true
    this.snapshotRefreshReason = reason
    if (reason !== 'open') {
      const recovery = this.snapshotRecovery ?? {
        reason: recoveryReason,
        syncLostCount: 0,
        droppedBytes: 0,
      }
      recovery.reason = recoveryReason
      recovery.syncLostCount += 1
      recovery.droppedBytes += droppedBytes
      this.snapshotRecovery = recovery
    }
    this.armSnapshotRefreshTimer(reason)
  }

  private armSnapshotRefreshTimer(reason: SnapshotRefreshReason): void {
    if (this.snapshotRefreshTimer !== null || this.snapshotRefreshInFlight) return
    const minInterval = reason === 'open'
      ? streamSnapshotRefreshMinIntervalMs
      : this.currentRecoveryRefreshInterval()
    const elapsed = Date.now() - this.lastSnapshotRefreshAt
    const intervalDelay = this.lastSnapshotRefreshAt > 0
      ? Math.max(0, minInterval - elapsed)
      : 0
    const delay = Math.max(streamSnapshotRefreshDelayMs, intervalDelay)
    this.log('snapshot_refresh_scheduled', {
      level: reason === 'open' ? 'debug' : 'warn',
      details: {
        reason,
        delay,
        minInterval,
        recovery: this.snapshotRecovery,
      },
    })
    this.snapshotRefreshTimer = setTimeout(() => {
      this.snapshotRefreshTimer = null
      void this.refreshQueuedSnapshot()
    }, delay)
  }

  private async refreshQueuedSnapshot(): Promise<void> {
    if (this.closed || !this.snapshotRefreshQueued || this.snapshotRefreshInFlight) return
    try {
      await this.refreshSnapshot(this.snapshotRefreshReason ?? 'sync_lost')
    } catch {
      // Live stream updates are best-effort; the explicit snapshot request on open remains authoritative.
    }
  }

  private async refreshSnapshot(reason: SnapshotRefreshReason): Promise<void> {
    const recovery = this.snapshotRecovery
    this.snapshotRefreshQueued = false
    this.snapshotRefreshReason = null
    this.snapshotRecovery = null
    this.clearSnapshotRefreshTimer()
    this.snapshotRefreshInFlight = true
    try {
      const snapshot = await this.request('live.screen.get', {
        terminal_id: this.options.terminalId,
      }, snapshotRefreshTimeoutMs)
      const liveRevision = snapshotLiveRevision(snapshot)
      if (liveRevision <= 0 || liveRevision < this.observedLiveRevision) {
        throw new Error(`invalid live screen revision ${liveRevision}`)
      }
      this.observedLiveRevision = liveRevision
      this.lastSnapshotRefreshAt = Date.now()
      if (recovery) this.recoveryRevision += 1
      const normalized: TerminalSnapshotPayload = {
        ...normalizeSnapshot(snapshot),
        refreshReason: reason,
      }
      this.log('snapshot_normalized', {
        details: {
          reason,
          textChars: normalized.text.length,
          replayChars: normalized.replay?.length ?? 0,
          screenTextChars: normalized.screenText?.length ?? 0,
          screenReplayChars: normalized.screenReplay?.length ?? 0,
          rows: normalized.rows,
          cols: normalized.cols,
          alternateScreen: normalized.alternateScreen === true,
        },
      })
      if (recovery) {
        this.consecutiveRecoveryRefreshes += 1
      } else {
        this.consecutiveRecoveryRefreshes = 0
      }
      this.emit(this.options.terminalId, {
        type: 'snapshot',
        snapshot: recovery ? {
          ...normalized,
          recovery: {
            revision: this.recoveryRevision,
            reason: recovery.reason,
            syncLostCount: recovery.syncLostCount,
            droppedBytes: recovery.droppedBytes,
          },
        } : normalized,
      })
      this.log('snapshot_refresh_done', {
        level: recovery ? 'warn' : 'debug',
        details: {
          reason,
          recovery,
          queuedDuringRefresh: this.snapshotRefreshQueued,
          consecutiveRecoveryRefreshes: this.consecutiveRecoveryRefreshes,
        },
      })
      this.startLiveInvalidationWatch()
    } catch (err) {
      const message = errorMessage(err)
      this.log('snapshot_refresh_failed', {
        level: 'warn',
        details: {
          reason,
          recovery,
          message,
          ...this.streamStatsDetails(),
        },
      })
      if (isRecoverableSnapshotRefreshFailure(message)) {
        this.handleChannelClosed(message)
      }
      throw err
    } finally {
      this.snapshotRefreshInFlight = false
      if (this.snapshotRefreshQueued && !this.closed) {
        this.armSnapshotRefreshTimer(this.snapshotRefreshReason ?? 'sync_lost')
      }
    }
  }

  private startLiveInvalidationWatch(): void {
    if (this.closed || this.liveInvalidationWatchActive) return
    this.liveInvalidationWatchActive = true
    void this.watchLiveInvalidations().catch((error: unknown) => {
      if (!this.closed) this.handleChannelClosed(errorMessage(error))
    }).finally(() => {
      this.liveInvalidationWatchActive = false
    })
  }

  private async watchLiveInvalidations(): Promise<void> {
    while (!this.closed) {
      const event = await this.request('live.invalidation.next', {
        terminal_id: this.options.terminalId,
        observed_revision: this.observedLiveRevision,
      })
      const revision = liveInvalidationRevision(event, this.options.terminalId)
      if (revision <= this.observedLiveRevision) {
        throw new Error(`invalid live invalidation revision ${revision}`)
      }
      this.log('live_invalidated', {
        details: {
          observedRevision: this.observedLiveRevision,
          revision,
        },
      })
      await this.refreshSnapshot('live_invalidated')
      if (this.observedLiveRevision < revision) {
        throw new Error(`live screen revision ${this.observedLiveRevision} precedes invalidation ${revision}`)
      }
    }
  }

  private clearSnapshotRefreshTimer(): void {
    if (this.snapshotRefreshTimer === null) return
    clearTimeout(this.snapshotRefreshTimer)
    this.snapshotRefreshTimer = null
  }

  private currentRecoveryRefreshInterval(): number {
    if (Date.now() - this.lastSnapshotRefreshAt > streamSnapshotRefreshBackoffResetMs) {
      this.consecutiveRecoveryRefreshes = 0
    }
    if (this.consecutiveRecoveryRefreshes <= 0) return streamSnapshotRefreshMinIntervalMs
    const interval = streamSnapshotRefreshMinIntervalMs * (2 ** Math.min(this.consecutiveRecoveryRefreshes, 3))
    return Math.min(streamSnapshotRefreshMaxIntervalMs, interval)
  }

  private sendFrame(channel: number, type: number, payload: Uint8Array): Promise<void> {
    const frame = encodeTermxFrame(channel, type, payload)
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
      screenUpdateFrames: this.screenUpdateFrames,
      screenUpdateBytes: this.screenUpdateBytes,
      screenUpdateBytesPerSecond: Math.round(this.screenUpdateBytes / elapsedSeconds),
      streamChannel: this.streamChannel,
      pendingRequests: this.pending.size,
      snapshotRefreshInFlight: this.snapshotRefreshInFlight,
      snapshotRefreshQueued: this.snapshotRefreshQueued,
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

function estimateResultBytes(value: unknown): number {
  try {
    if (value instanceof Uint8Array) return value.byteLength
    return new TextEncoder().encode(JSON.stringify(value)).byteLength
  } catch {
    return 0
  }
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function isRecoverableSnapshotRefreshFailure(message: string): boolean {
  return /timed out|not open|closed|native bridge|send failed/i.test(message)
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

function decodeSyncLostDroppedBytes(payload: Uint8Array): number {
  if (payload.byteLength !== 4) return 0
  const dropped = new DataView(payload.buffer, payload.byteOffset, payload.byteLength).getUint32(0)
  return Number.isFinite(dropped) ? dropped : 0
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

function snapshotLiveRevision(value: unknown): number {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return 0
  const revision = (value as Record<string, unknown>).live_revision
  return safePositiveInteger(revision)
}

function liveInvalidationRevision(value: unknown, terminalId: string): number {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('live invalidation response is invalid')
  }
  const record = value as Record<string, unknown>
  if (record.type !== 7 || record.terminal_id !== terminalId) {
    throw new Error('live invalidation response identity is invalid')
  }
  const revision = safePositiveInteger(record.live_revision)
  if (revision <= 0) throw new Error('live invalidation response revision is invalid')
  return revision
}

function safePositiveInteger(value: unknown): number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0 ? value : 0
}

function historyViewportRows(value: unknown): unknown[] {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return []
  }
  const rows = (value as Record<string, unknown>).rows
  return Array.isArray(rows) ? rows : []
}

function historyViewportLoadedRows(value: unknown, fallback: number): number {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return fallback
  }
  const loadedRows = (value as Record<string, unknown>).loaded_rows
  if (typeof loadedRows === 'number' && Number.isFinite(loadedRows) && loadedRows > 0) {
    return Math.floor(loadedRows)
  }
  return fallback
}

function historyViewportCommittedTotalRows(value: unknown): number | undefined {
  return historyViewportPositiveIntField(value, 'scrollback_total')
}

function historyViewportLogicalTotalRows(value: unknown): number | undefined {
  return historyViewportPositiveIntField(value, 'scrollback_logical_total')
}

function historyViewportGeneration(value: unknown): number | undefined {
  return historyViewportPositiveIntField(value, 'history_generation')
}

function historyViewportFirstRowID(value: unknown): number | undefined {
  return historyViewportNonNegativeIntField(value, 'first_row_id')
}

function historyViewportLastRowID(value: unknown): number | undefined {
  return historyViewportNonNegativeIntField(value, 'last_row_id')
}

function historyViewportMetadata(value: unknown): Partial<TerminalScrollbackPage> {
  const committedTotalRows = historyViewportCommittedTotalRows(value)
  const logicalTotalRows = historyViewportLogicalTotalRows(value)
  const historyGeneration = historyViewportGeneration(value)
  const firstRowId = historyViewportFirstRowID(value)
  const lastRowId = historyViewportLastRowID(value)
  return {
    ...(committedTotalRows !== undefined ? { committedTotalRows } : {}),
    ...(logicalTotalRows !== undefined ? { logicalTotalRows } : {}),
    ...(historyGeneration !== undefined ? { historyGeneration } : {}),
    ...(firstRowId !== undefined ? { firstRowId } : {}),
    ...(lastRowId !== undefined ? { lastRowId } : {}),
  }
}

function historyViewportPositiveIntField(value: unknown, key: string): number | undefined {
  const number = historyViewportNumberField(value, key)
  if (number === undefined || number <= 0) return undefined
  return number
}

function historyViewportNonNegativeIntField(value: unknown, key: string): number | undefined {
  const number = historyViewportNumberField(value, key)
  if (number === undefined || number < 0) return undefined
  return number
}

function historyViewportNumberField(value: unknown, key: string): number | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return undefined
  }
  const number = (value as Record<string, unknown>)[key]
  if (typeof number !== 'number' || !Number.isFinite(number)) {
    return undefined
  }
  return Math.floor(number)
}

function closedReason(payload: Uint8Array): string | undefined {
  if (payload.length === 0) return undefined
  const text = new TextDecoder().decode(payload).trim()
  return text || undefined
}

function streamErrorMessage(payload: Uint8Array): string {
  try {
    return decodeTerminalErrorPayload(payload).message
  } catch {
    return 'terminal stream error'
  }
}
