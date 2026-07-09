/**
 * NativeConnectionProxy — 将 Native WebRTC 连接包装为 RtcSession / RtcConnector
 *
 * 控制面：通过 Capacitor NativeConnection 插件调用 Kotlin 层
 * 数据面：通过带一次性 token 鉴权的 BridgeServer (localhost WebSocket) 收发二进制帧
 *
 * BridgeServer 帧格式：
 *   [1 byte frameType][2 bytes channelId BE][4 bytes payloadLen BE][payload]
 */

import type {
  ConnectionCapabilities,
  ConnectionInfo,
  ConnectionPath,
  ManagedRtcConnector,
  ManagedRtcSession,
  RtcBinaryChannel,
  RtcConnectOptions,
  RtcConnectionStateSnapshot,
  RtcEvent,
  RtcJsonRpcChannel,
  RtcSubscription,
} from '@termx/remote-ui'
import {
  decodeRuntimeAPIResponse,
  decodeRuntimeEventEnvelope,
  decodeRuntimeResponseBody,
  encodeRuntimeAPIRequest,
  encodeRuntimeEventSubscribeRequest,
  encodeRuntimeRequestBody,
  runtimeEventEnvelopeToRtcEvent,
} from '@termx/remote-ui'
import { NativeConnection, type NativeConnectOpts, type NativeConnectionInfo, type NativeConnectionSnapshot, type NativeStateChangeEvent } from './plugins/nativeConnection'
import { writeNativeDebugLog } from './nativeDebugLog'

// ─── Frame constants ──────────────────────────────────────────────────────────

const FRAME_DATA: number = 0x01
const FRAME_OPEN_CHAN: number = 0x02
const FRAME_CHAN_OPENED: number = 0x03
const FRAME_CLOSE_CHAN: number = 0x04
const FRAME_CHAN_ERROR: number = 0x05
const FRAME_AUTH: number = 0x06
const FRAME_AUTH_OK: number = 0x07
const FRAME_STATE_UPDATE: number = 0x10
const FRAME_TRANSFER_SYNC: number = 0x11     // Native→JS: transfer progress updates
const FRAME_TRANSFER_REQUEST: number = 0x12  // JS→Native: start/cancel transfer
const FRAME_SYNC_REQUEST: number = 0x22      // JS→Native: request full state
const FRAME_SYNC_RESPONSE: number = 0x23     // Native→JS: full state snapshot
const CHAN_CONTROL = 0x0000
const HEADER_SIZE = 7
const CONNECT_TIMEOUT_MS = 90_000
const CHANNEL_OPEN_TIMEOUT_MS = 10_000
const API_REQUEST_TIMEOUT_MS = 60_000
const NATIVE_RESUME_WAIT_TIMEOUT_MS = 8_000
const API_CHUNK_MAGIC = 0xc0
const API_CHUNK_LAST = 0x02
const BRIDGE_STATS_INTERVAL_MS = 1000
const BRIDGE_LARGE_PAYLOAD_BYTES = 64 * 1024

export interface TransferSyncItem {
  id: string
  machineId?: string
  name: string
  direction: 'download' | 'upload'
  totalSize: number
  transferredSize: number
  status: 'pending' | 'transferring' | 'paused' | 'completed' | 'failed' | 'cancelled' | 'missing'
  startedAt: number
  updatedAt?: number
  bytesPerSecond?: number
  filePath?: string
  localUri?: string | undefined
  targetDir?: string | undefined
  savedPath?: string | undefined
  savedUri?: string | undefined
  error?: string
  storeKey?: string
}

type NativeRuntimePath = ConnectionPath

export interface TransferSyncPayload {
  transfers: TransferSyncItem[]
}

export interface SyncResponsePayload {
  stores?: Array<NativeConnectionSnapshot | NativeStateChangeEvent | Record<string, unknown>>
  transfers?: TransferSyncItem[]
}

interface PendingChannelOpen {
  promise: Promise<number>
  resolve(channelId: number): void
  reject(error: Error): void
  timer: ReturnType<typeof setTimeout>
}

interface PendingApiRequest {
  chunks: Uint8Array[]
  timer: ReturnType<typeof setTimeout>
  method: string
  path: string
  startedAt: number
  bytes: number
  chunksSeen: number
  resolve(value: unknown): void
  reject(error: Error): void
}

type SharedApiChannel = RtcJsonRpcChannel & {
  isOpen(): boolean
  forceClose(error: Error): void
}

type BridgeChannelDataHandler = (payload: Uint8Array) => void
type BridgeChannelCloseHandler = (error: Error) => void
type BridgeLifecycleHandler = () => void

class NativeBridgeClient {
  private ws: WebSocket | null = null
  private connectPromise: Promise<WebSocket> | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private destroyed = false
  private generationValue = 0
  private socketEpoch = 0
  private ignoredCloseSockets = new WeakSet<WebSocket>()

  private labelToChannel = new Map<string, number>()
  private channelToLabel = new Map<number, string>()
  private pendingOpens = new Map<string, PendingChannelOpen>()
  private activeLabels = new Set<string>()

  private dataHandlers = new Map<string, Set<BridgeChannelDataHandler>>()
  private closeHandlers = new Map<string, Set<BridgeChannelCloseHandler>>()
  private stateUpdateHandlers = new Set<(snapshot: NativeConnectionSnapshot | NativeStateChangeEvent) => void>()
  private transferSyncHandlers = new Set<(data: TransferSyncPayload) => void>()
  private syncResponseHandlers = new Set<(data: SyncResponsePayload) => void>()
  private disconnectHandlers = new Set<() => void>()
  private readyHandlers = new Set<BridgeLifecycleHandler>()
  private bridgeDisconnectHandlers = new Set<BridgeLifecycleHandler>()
  private frameStats = new Map<string, {
    rxFrames: number
    rxBytes: number
    txFrames: number
    txBytes: number
    lastLogAt: number
    lastRxBytes: number
    lastTxBytes: number
  }>()

  get isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }

  get generation(): number {
    return this.generationValue
  }

  async ensureConnected(signal?: AbortSignal): Promise<void> {
    await this.getSocket(signal)
  }

  isChannelOpen(label: string): boolean {
    return this.isConnected && this.labelToChannel.has(label)
  }

  async openChannel(label: string, signal?: AbortSignal): Promise<void> {
    if (signal?.aborted) throw new Error('aborted')
    this.activeLabels.add(label)
    if (this.isChannelOpen(label)) return
    const existing = this.pendingOpens.get(label)
    if (existing) {
      await abortable(existing.promise, signal)
      return
    }
    await this.getSocket(signal)
    if (this.isChannelOpen(label)) return

    const startedAt = bridgeNow()
    nativeBridgeLog('channel_open_request', {
      label,
      activeLabels: this.activeLabels.size,
      pendingOpens: this.pendingOpens.size,
      generation: this.generationValue,
    })
    let resolve!: (channelId: number) => void
    let reject!: (error: Error) => void
    const promise = new Promise<number>((res, rej) => {
      resolve = res
      reject = rej
    })
    const timer = setTimeout(() => {
      const current = this.pendingOpens.get(label)
      if (current?.promise !== promise) return
      this.pendingOpens.delete(label)
      nativeBridgeLog('channel_open_timeout', {
        level: 'warn',
        label,
        elapsedMs: Math.round(bridgeNow() - startedAt),
      })
      reject(new Error(`native bridge channel open timed out: ${label}`))
    }, CHANNEL_OPEN_TIMEOUT_MS)
    this.pendingOpens.set(label, { promise, resolve, reject, timer })

    const payload = new TextEncoder().encode(label)
    if (!this.sendFrame(FRAME_OPEN_CHAN, 0, payload)) {
      const current = this.pendingOpens.get(label)
      if (current?.promise === promise) {
        this.pendingOpens.delete(label)
        clearTimeout(timer)
      }
      reject(new Error('native bridge WebSocket is not open'))
    }
    try {
      await abortable(promise, signal)
      nativeBridgeLog('channel_open_success', {
        label,
        elapsedMs: Math.round(bridgeNow() - startedAt),
        channelId: this.labelToChannel.get(label),
      })
    } catch (error) {
      nativeBridgeLog('channel_open_failed', {
        level: 'warn',
        label,
        elapsedMs: Math.round(bridgeNow() - startedAt),
        reason: error instanceof Error ? error.message : String(error),
      })
      this.releaseLabelIfUnused(label)
      throw error
    }
  }

  closeChannel(label: string): void {
    this.activeLabels.delete(label)
    const channelId = this.labelToChannel.get(label)
    if (channelId !== undefined) {
      this.sendFrame(FRAME_CLOSE_CHAN, channelId, new Uint8Array(0))
      this.labelToChannel.delete(label)
      this.channelToLabel.delete(channelId)
    }
    const pending = this.pendingOpens.get(label)
    if (pending) {
      this.pendingOpens.delete(label)
      clearTimeout(pending.timer)
      pending.reject(new Error(`native bridge channel closed: ${label}`))
    }
  }

  sendData(label: string, payload: Uint8Array): boolean {
    const channelId = this.labelToChannel.get(label)
    if (channelId === undefined) {
      nativeBridgeLog('send_data_missing_channel', {
        level: 'warn',
        label,
        bytes: payload.byteLength,
      })
      return false
    }
    return this.sendFrame(FRAME_DATA, channelId, payload)
  }

  sendControl(frameType: number, payload: Uint8Array): boolean {
    return this.sendFrame(frameType, CHAN_CONTROL, payload)
  }

  onChannelData(label: string, handler: BridgeChannelDataHandler): () => void {
    let handlers = this.dataHandlers.get(label)
    if (!handlers) {
      handlers = new Set()
      this.dataHandlers.set(label, handlers)
    }
    handlers.add(handler)
    this.activeLabels.add(label)
    void this.openChannel(label).catch(() => {})
    return () => {
      handlers?.delete(handler)
      if (handlers?.size === 0) {
        this.dataHandlers.delete(label)
        this.releaseLabelIfUnused(label)
      }
    }
  }

  onChannelClose(label: string, handler: BridgeChannelCloseHandler): () => void {
    let handlers = this.closeHandlers.get(label)
    if (!handlers) {
      handlers = new Set()
      this.closeHandlers.set(label, handlers)
    }
    handlers.add(handler)
    return () => {
      handlers?.delete(handler)
      if (handlers?.size === 0) {
        this.closeHandlers.delete(label)
        this.releaseLabelIfUnused(label)
      }
    }
  }

  onStateUpdate(handler: (snapshot: NativeConnectionSnapshot | NativeStateChangeEvent) => void): () => void {
    this.stateUpdateHandlers.add(handler)
    return () => this.stateUpdateHandlers.delete(handler)
  }

  onTransferSync(handler: (data: TransferSyncPayload) => void): () => void {
    this.transferSyncHandlers.add(handler)
    return () => this.transferSyncHandlers.delete(handler)
  }

  onSyncResponse(handler: (data: SyncResponsePayload) => void): () => void {
    this.syncResponseHandlers.add(handler)
    return () => this.syncResponseHandlers.delete(handler)
  }

  onReady(handler: BridgeLifecycleHandler): () => void {
    this.readyHandlers.add(handler)
    return () => this.readyHandlers.delete(handler)
  }

  onBridgeDisconnected(handler: BridgeLifecycleHandler): () => void {
    this.bridgeDisconnectHandlers.add(handler)
    return () => this.bridgeDisconnectHandlers.delete(handler)
  }

  onDisconnect(handler: () => void): () => void {
    this.disconnectHandlers.add(handler)
    return () => this.disconnectHandlers.delete(handler)
  }

  forceReconnect(): void {
    if (this.destroyed) return
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
    nativeBridgeLog('force_reconnect', {
      level: 'warn',
      activeLabels: this.activeLabels.size,
      generation: this.generationValue,
    })
    const ws = this.ws
    this.socketEpoch += 1
    this.ws = null
    this.connectPromise = null
    if (ws) {
      try {
        this.ignoredCloseSockets.add(ws)
        ws.close()
      } catch { /* ignore */ }
    }
    this.generationValue += 1
    this.clearChannelMappings(new Error('native bridge reconnecting'), { notifyChannels: false })
    for (const handler of this.bridgeDisconnectHandlers) handler()
    void this.getSocket().catch(() => {})
  }

  destroy(): void {
    this.destroyed = true
    this.socketEpoch += 1
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
    const ws = this.ws
    if (ws) {
      try {
        this.ignoredCloseSockets.add(ws)
        ws.close()
      } catch { /* ignore */ }
    }
    this.ws = null
    this.connectPromise = null
    this.clearChannelMappings(new Error('native bridge destroyed'), { notifyChannels: true })
    for (const handler of this.disconnectHandlers) handler()
  }

  private async getSocket(signal?: AbortSignal): Promise<WebSocket> {
    if (signal?.aborted) throw new Error('aborted')
    if (this.ws?.readyState === WebSocket.OPEN) return this.ws
    if (!this.connectPromise) {
      const epoch = this.socketEpoch
      let promise!: Promise<WebSocket>
      const startedAt = bridgeNow()
      nativeBridgeLog('socket_connect_start', {
        epoch,
        activeLabels: this.activeLabels.size,
      })
      promise = NativeConnection.getBridgeEndpoint()
        .then(({ port, token }) => {
          nativeBridgeLog('socket_endpoint', { port, tokenBytes: token.length })
          return openAuthenticatedWebSocket(`ws://127.0.0.1:${port}`, token, signal)
        })
        .then((ws) => {
          if (this.destroyed || epoch !== this.socketEpoch) {
            try {
              this.ignoredCloseSockets.add(ws)
              ws.close()
            } catch { /* ignore */ }
            throw new Error('stale native bridge WebSocket')
          }
          this.ws = ws
          if (this.connectPromise === promise) this.connectPromise = null
          nativeBridgeLog('socket_connected', {
            level: 'info',
            elapsedMs: Math.round(bridgeNow() - startedAt),
            epoch,
            activeLabels: this.activeLabels.size,
          })
          ws.addEventListener('message', (ev: MessageEvent) => {
            if (this.ws !== ws || this.socketEpoch !== epoch) return
            if (ev.data instanceof ArrayBuffer) this.handleFrame(new Uint8Array(ev.data))
          })
          ws.addEventListener('close', () => {
            if (this.ignoredCloseSockets.has(ws)) {
              this.ignoredCloseSockets.delete(ws)
              if (this.ws === ws) this.ws = null
              return
            }
            if (this.ws !== ws) return
            nativeBridgeLog('socket_closed', {
              level: 'warn',
              activeLabels: this.activeLabels.size,
              mappedChannels: this.channelToLabel.size,
            })
            this.ws = null
            this.clearChannelMappings(new Error('native bridge WebSocket closed'), { notifyChannels: false })
            this.generationValue += 1
            for (const handler of this.bridgeDisconnectHandlers) handler()
            this.scheduleReconnect()
          }, { once: true })
          this.reopenActiveLabels()
          for (const handler of this.readyHandlers) handler()
          return ws
        })
        .catch((error) => {
          nativeBridgeLog('socket_connect_failed', {
            level: 'warn',
            elapsedMs: Math.round(bridgeNow() - startedAt),
            reason: error instanceof Error ? error.message : String(error),
          })
          if (this.connectPromise === promise) {
            this.connectPromise = null
            this.scheduleReconnect()
          }
          throw error
        })
      this.connectPromise = promise
    }
    return abortable(this.connectPromise, signal)
  }

  private scheduleReconnect(): void {
    if (this.destroyed || this.reconnectTimer || this.activeLabels.size === 0) return
    nativeBridgeLog('socket_reconnect_scheduled', {
      activeLabels: this.activeLabels.size,
      generation: this.generationValue,
    })
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      if (this.destroyed || this.activeLabels.size === 0) return
      void this.getSocket().catch(() => {})
    }, 300)
  }

  private reopenActiveLabels(): void {
    const labels = Array.from(this.activeLabels)
    const reopenedLabels = labels.filter((label) => !label.startsWith('terminal:'))
    nativeBridgeLog('reopen_active_labels', {
      activeLabels: this.activeLabels.size,
      labels: reopenedLabels.slice(0, 10),
      skippedTerminalLabels: labels.length - reopenedLabels.length,
    })
    for (const label of reopenedLabels) {
      void this.openChannel(label).catch(() => {})
    }
    this.sendControl(FRAME_SYNC_REQUEST, new Uint8Array(0))
  }

  private releaseLabelIfUnused(label: string): void {
    if (this.labelToChannel.has(label) || this.pendingOpens.has(label)) return
    if ((this.dataHandlers.get(label)?.size ?? 0) > 0) return
    if ((this.closeHandlers.get(label)?.size ?? 0) > 0) return
    this.activeLabels.delete(label)
  }

  private clearChannelMappings(error: Error, options: { notifyChannels: boolean }): void {
    if (this.labelToChannel.size > 0 || this.pendingOpens.size > 0) {
      nativeBridgeLog('clear_channel_mappings', {
        level: 'warn',
        mappedChannels: this.labelToChannel.size,
        pendingOpens: this.pendingOpens.size,
        notifyChannels: options.notifyChannels,
        reason: error.message,
      })
    }
    this.labelToChannel.clear()
    this.channelToLabel.clear()
    for (const pending of this.pendingOpens.values()) {
      clearTimeout(pending.timer)
      pending.reject(error)
    }
    this.pendingOpens.clear()
    if (options.notifyChannels) {
      for (const handlers of this.closeHandlers.values()) {
        for (const handler of handlers) handler(error)
      }
    }
  }

  private handleFrame(buf: Uint8Array): void {
    if (buf.length < HEADER_SIZE) {
      nativeBridgeLog('short_frame', { level: 'warn', bytes: buf.byteLength })
      return
    }
    const view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength)
    const frameType = view.getUint8(0)
    const channelId = view.getUint16(1, false)
    const payloadLen = view.getUint32(3, false)
    if (payloadLen > buf.length - HEADER_SIZE) {
      nativeBridgeLog('truncated_frame', {
        level: 'warn',
        frameType,
        channelId,
        payloadLen,
        frameBytes: buf.byteLength,
      })
      return
    }
    const payload = buf.slice(HEADER_SIZE, HEADER_SIZE + payloadLen)

    if (frameType === FRAME_CHAN_OPENED) {
      const label = new TextDecoder().decode(payload)
      this.labelToChannel.set(label, channelId)
      this.channelToLabel.set(channelId, label)
      nativeBridgeLog('channel_opened_frame', {
        label,
        channelId,
        pending: this.pendingOpens.has(label),
      })
      const pending = this.pendingOpens.get(label)
      if (pending) {
        this.pendingOpens.delete(label)
        clearTimeout(pending.timer)
        pending.resolve(channelId)
      }
      return
    }

    if (frameType === FRAME_DATA) {
      const label = this.channelToLabel.get(channelId)
      if (!label) {
        nativeBridgeLog('data_unknown_channel', {
          level: 'warn',
          channelId,
          bytes: payload.byteLength,
        })
        return
      }
      this.noteFrameStat(label, 'rx', payload.byteLength)
      const handlers = this.dataHandlers.get(label)
      if (handlers) {
        for (const handler of handlers) handler(payload)
      }
      return
    }

    if (frameType === FRAME_STATE_UPDATE) {
      try {
        const snapshot = JSON.parse(new TextDecoder().decode(payload)) as NativeConnectionSnapshot | NativeStateChangeEvent
        cacheNativeState(snapshot)
        nativeBridgeLog('state_update', {
          machineId: snapshot.machineId,
          phase: snapshot.phase,
          path: snapshot.path,
          relayInUse: snapshot.relayInUse === true,
          statusText: snapshot.statusText,
        })
        for (const handler of this.stateUpdateHandlers) handler(snapshot)
      } catch (error) {
        nativeBridgeLog('state_update_parse_failed', {
          level: 'warn',
          bytes: payload.byteLength,
          reason: error instanceof Error ? error.message : String(error),
        })
      }
      return
    }

    if (frameType === FRAME_TRANSFER_SYNC) {
      try {
        const data = JSON.parse(new TextDecoder().decode(payload)) as TransferSyncPayload
        for (const handler of this.transferSyncHandlers) handler(data)
      } catch { /* ignore malformed */ }
      return
    }

    if (frameType === FRAME_SYNC_RESPONSE) {
      try {
        const data = JSON.parse(new TextDecoder().decode(payload)) as SyncResponsePayload
        nativeBridgeLog('sync_response', {
          stores: Array.isArray(data.stores) ? data.stores.length : 0,
          transfers: Array.isArray(data.transfers) ? data.transfers.length : 0,
          bytes: payload.byteLength,
        })
        if (Array.isArray(data.stores)) {
          for (const store of data.stores) {
            if (isNativeStatePayload(store)) {
              cacheNativeState(store)
              for (const handler of this.stateUpdateHandlers) handler(store)
            }
          }
        }
        for (const handler of this.syncResponseHandlers) handler(data)
      } catch (error) {
        nativeBridgeLog('sync_response_parse_failed', {
          level: 'warn',
          bytes: payload.byteLength,
          reason: error instanceof Error ? error.message : String(error),
        })
      }
      return
    }

    if (frameType === FRAME_CLOSE_CHAN || frameType === FRAME_CHAN_ERROR) {
      const label = this.channelToLabel.get(channelId)
      if (!label) return
      const error = new Error(frameType === FRAME_CHAN_ERROR
        ? new TextDecoder().decode(payload) || 'native bridge channel error'
        : 'native bridge channel closed')
      nativeBridgeLog(frameType === FRAME_CHAN_ERROR ? 'channel_error_frame' : 'channel_close_frame', {
        level: 'warn',
        label,
        channelId,
        message: error.message,
      })
      this.labelToChannel.delete(label)
      this.channelToLabel.delete(channelId)
      this.activeLabels.delete(label)
      const pending = this.pendingOpens.get(label)
      if (pending) {
        this.pendingOpens.delete(label)
        clearTimeout(pending.timer)
        pending.reject(error)
      }
      const handlers = this.closeHandlers.get(label)
      if (handlers) {
        for (const handler of handlers) handler(error)
      }
    }
  }

  private sendFrame(frameType: number, channelId: number, payload: Uint8Array): boolean {
    const ws = this.ws
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      nativeBridgeLog('send_frame_socket_closed', {
        level: 'warn',
        frameType,
        channelId,
        bytes: payload.byteLength,
        readyState: ws?.readyState,
      })
      return false
    }
    const label = channelId === CHAN_CONTROL ? 'control' : this.channelToLabel.get(channelId)
    if (label) this.noteFrameStat(label, 'tx', payload.byteLength)
    const buf = new ArrayBuffer(HEADER_SIZE + payload.length)
    const view = new DataView(buf)
    view.setUint8(0, frameType)
    view.setUint16(1, channelId, false)
    view.setUint32(3, payload.length, false)
    new Uint8Array(buf, HEADER_SIZE).set(payload)
    ws.send(buf)
    return true
  }

  private noteFrameStat(label: string, direction: 'rx' | 'tx', bytes: number): void {
    const now = bridgeNow()
    let stats = this.frameStats.get(label)
    if (!stats) {
      stats = {
        rxFrames: 0,
        rxBytes: 0,
        txFrames: 0,
        txBytes: 0,
        lastLogAt: now,
        lastRxBytes: 0,
        lastTxBytes: 0,
      }
      this.frameStats.set(label, stats)
    }
    if (direction === 'rx') {
      stats.rxFrames += 1
      stats.rxBytes += bytes
    } else {
      stats.txFrames += 1
      stats.txBytes += bytes
    }
    if (bytes >= BRIDGE_LARGE_PAYLOAD_BYTES) {
      nativeBridgeLog('large_payload', {
        level: 'warn',
        label,
        direction,
        bytes,
        rxBytes: stats.rxBytes,
        txBytes: stats.txBytes,
      })
    }
    if (now - stats.lastLogAt < BRIDGE_STATS_INTERVAL_MS) return
    const elapsedSeconds = Math.max(0.001, (now - stats.lastLogAt) / 1000)
    const intervalRxBytes = stats.rxBytes - stats.lastRxBytes
    const intervalTxBytes = stats.txBytes - stats.lastTxBytes
    nativeBridgeLog('channel_stats', {
      label,
      rxFrames: stats.rxFrames,
      rxBytes: stats.rxBytes,
      txFrames: stats.txFrames,
      txBytes: stats.txBytes,
      intervalRxBytes,
      intervalTxBytes,
      rxBytesPerSecond: Math.round(intervalRxBytes / elapsedSeconds),
      txBytesPerSecond: Math.round(intervalTxBytes / elapsedSeconds),
    })
    stats.lastLogAt = now
    stats.lastRxBytes = stats.rxBytes
    stats.lastTxBytes = stats.txBytes
  }
}

// ─── NativeRtcSession ─────────────────────────────────────────────────────────

export class NativeRtcSession implements ManagedRtcSession {
  private bridge: NativeBridgeClient
  private machineId: string
  private path: NativeRuntimePath
  private relayInUse: boolean
  private connectionId: string

  private cleanups: Array<() => void> = []
  private channelCloseHandlers = new Map<string, Set<() => void>>()
  // events channel subscribers
  private eventHandlers = new Set<(event: RtcEvent) => void>()
  private eventsChannelPromise: Promise<void> | null = null
  private eventsSubscribed = false
  private eventsDataCleanup: (() => void) | null = null
  private eventsCloseCleanup: (() => void) | null = null
  private apiChannelPromise: Promise<SharedApiChannel> | null = null
  private apiChannel: SharedApiChannel | null = null

  // transfer sync subscribers
  private connectionStateHandlers = new Set<(snapshot: RtcConnectionStateSnapshot) => void>()
  private transferSyncHandlers = new Set<(data: TransferSyncPayload) => void>()
  private syncResponseHandlers = new Set<(data: SyncResponsePayload) => void>()
  private bridgeDisconnected = false

  private closed = false
  private disconnectHandlers = new Set<() => void>()
  private connectedWaiters = new Set<{
    resolve(): void
    reject(error: Error): void
    cleanup?: (() => void) | undefined
  }>()

  constructor(
    bridge: NativeBridgeClient,
    machineId: string,
    path: NativeRuntimePath,
    relayInUse: boolean,
  ) {
    this.bridge = bridge
    this.machineId = machineId
    this.path = path
    this.relayInUse = relayInUse
    this.connectionId = `${machineId}-${Date.now()}`
    this.cleanups.push(this.bridge.onDisconnect(() => {
      this.closeClientState(new Error('native bridge WebSocket closed'), { notifyDisconnect: true })
    }))
    this.cleanups.push(this.bridge.onBridgeDisconnected(() => {
      this.handleBridgeDisconnected()
    }))
    this.cleanups.push(this.bridge.onReady(() => {
      this.handleBridgeReady()
    }))
    this.cleanups.push(this.bridge.onStateUpdate((data) => {
      const snapshot = normalizeNativeConnectionState(data)
      if (snapshot.machineId !== this.machineId) return
      this.path = snapshot.path ?? this.path
      this.relayInUse = snapshot.relayInUse
      for (const h of this.connectionStateHandlers) h(snapshot)
      if (snapshot.phase === 'connected' && this.eventHandlers.size > 0) {
        this.eventsSubscribed = false
        this.ensureEventsChannel()
      } else if (snapshot.phase === 'reconnecting' || snapshot.phase === 'waiting_network' || snapshot.phase === 'failed') {
        this.resetEventsChannel()
      }
      if (snapshot.phase === 'connected') {
        this.resolveConnectedWaiters()
      } else if (snapshot.phase === 'failed') {
        this.rejectConnectedWaiters(new Error(snapshot.failReason ?? snapshot.statusText ?? 'connection failed'))
      }
    }))
    this.cleanups.push(this.bridge.onTransferSync((data) => {
      for (const h of this.transferSyncHandlers) h(data)
    }))
    this.cleanups.push(this.bridge.onSyncResponse((data) => {
      for (const h of this.syncResponseHandlers) h(data)
    }))
  }

  private handleBridgeDisconnected(): void {
    if (this.closed) return
    this.bridgeDisconnected = true
    this.eventsChannelPromise = null
    this.resetEventsChannel()
    this.apiChannel?.forceClose(new Error('native bridge disconnected'))
    this.apiChannel = null
    this.apiChannelPromise = null
    this.emitSyntheticConnectionState('reconnecting', 'Reconnecting native bridge...')
  }

  private handleBridgeReady(): void {
    if (this.closed) return
    this.sendSyncRequest()
    if (this.eventHandlers.size > 0) {
      this.eventsSubscribed = false
      this.ensureEventsChannel()
    }
    if (this.bridgeDisconnected) {
      this.bridgeDisconnected = false
      this.emitSyntheticConnectionState('connected', 'Connected')
      this.resolveConnectedWaiters()
    }
  }

  private closeClientState(error: Error, options: { notifyDisconnect: boolean }): void {
    if (this.closed) return
    this.closed = true
    for (const cleanup of this.cleanups) cleanup()
    this.cleanups = []
    this.eventsChannelPromise = null
    this.resetEventsChannel()
    this.apiChannel?.forceClose(error)
    this.apiChannel = null
    this.apiChannelPromise = null
    this.connectionStateHandlers.clear()
    this.transferSyncHandlers.clear()
    this.syncResponseHandlers.clear()
    this.rejectConnectedWaiters(error)
    if (options.notifyDisconnect) {
      for (const h of this.disconnectHandlers) h()
    }
    for (const handlers of this.channelCloseHandlers.values()) {
      for (const h of handlers) h()
    }
    this.channelCloseHandlers.clear()
  }

  private sendFrame(frameType: number, channelId: number, payload: Uint8Array): boolean {
    if (channelId === CHAN_CONTROL) return this.bridge.sendControl(frameType, payload)
    return false
  }

  private emitSyntheticConnectionState(
    phase: RtcConnectionStateSnapshot['phase'],
    statusText: string,
  ): void {
    const snapshot: RtcConnectionStateSnapshot = {
      machineId: this.machineId,
      phase,
      path: this.path,
      statusText,
      relayInUse: this.relayInUse,
    }
    nativeStateCache.set(this.machineId, snapshot)
    nativeBridgeLog('session_connection_state', {
      machineId: snapshot.machineId,
      phase: snapshot.phase,
      path: snapshot.path,
      relayInUse: snapshot.relayInUse,
      statusText: snapshot.statusText,
    })
    for (const handler of this.connectionStateHandlers) handler(snapshot)
  }

  private async openChannel(label: string): Promise<void> {
    if (this.closed) throw new Error('native bridge session is closed')
    await this.bridge.openChannel(label)
  }

  private async ensureChannelOpen(label: string): Promise<void> {
    if (this.closed) throw new Error('native bridge session is closed')
    if (this.bridge.isChannelOpen(label)) return
    await this.bridge.openChannel(label)
  }

  private makeBinaryChannel(label: string): RtcBinaryChannel {
    const session = this
    return {
      label,
      get readyState() {
        return (!session.closed && session.bridge.isChannelOpen(label)) ? 'open' as const : 'closed' as const
      },
      send: (data: Uint8Array) => {
        if (!this.bridge.sendData(label, data)) {
          throw new Error(`native bridge channel is not open: ${label}`)
        }
      },
      close: () => {
        this.bridge.closeChannel(label)
        this.channelCloseHandlers.delete(label)
      },
      onMessage: (handler: (data: Uint8Array) => void): RtcSubscription => {
        const unsubscribe = this.bridge.onChannelData(label, handler)
        return { close: unsubscribe }
      },
      onClose: (handler: () => void): RtcSubscription => {
        let handlers = this.channelCloseHandlers.get(label)
        if (!handlers) {
          handlers = new Set()
          this.channelCloseHandlers.set(label, handlers)
        }
        handlers.add(handler)
        const unsubscribe = this.bridge.onChannelClose(label, () => handler())
        return {
          close: () => {
            handlers!.delete(handler)
            unsubscribe()
          },
        }
      },
      waitOpen: () => this.ensureChannelOpen(label),
    }
  }

  async openTerminal(terminalId: string): Promise<RtcBinaryChannel> {
    const label = `terminal:${this.machineId}:${terminalId}`
    await this.openChannel(label)
    return this.makeBinaryChannel(label)
  }

  closeTerminalDataChannel(terminalId: string): void {
    this.bridge.closeChannel(`terminal:${this.machineId}:${terminalId}`)
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    if (this.apiChannel?.isOpen()) return this.apiChannel
    if (this.apiChannelPromise) return this.apiChannelPromise
    this.apiChannelPromise = this.createApiChannel().then((channel) => {
      this.apiChannel = channel
      return channel
    }).catch((error) => {
      this.apiChannelPromise = null
      throw error
    })
    return this.apiChannelPromise
  }

  private async createApiChannel(): Promise<SharedApiChannel> {
    const label = `api:${this.machineId}`
    await this.openChannel(label)
    const binaryChannel = this.makeBinaryChannel(label)

    const session = this
    let closed = false
    let nextId = 1
    const pending = new Map<string, PendingApiRequest>()
    let apiChannel!: SharedApiChannel
    let dataSubscription: RtcSubscription | null = null
    let closeSubscription: RtcSubscription | null = null

    const forceClose = (error: Error) => {
      if (closed) return
      closed = true
      for (const request of pending.values()) {
        clearTimeout(request.timer)
        request.reject(error)
      }
      pending.clear()
      dataSubscription?.close()
      closeSubscription?.close()
      if (this.apiChannel === apiChannel) this.apiChannel = null
      if (this.apiChannelPromise) this.apiChannelPromise = null
    }

    const rejectRequest = (id: string, error: Error) => {
      const request = pending.get(id)
      if (!request) return
      pending.delete(id)
      clearTimeout(request.timer)
      nativeBridgeLog('api_request_reject', {
        level: 'warn',
        id,
        method: request.method,
        path: request.path,
        elapsedMs: Math.round(bridgeNow() - request.startedAt),
        bytes: request.bytes,
        chunks: request.chunksSeen,
        reason: error.message,
      })
      request.reject(error)
    }

    const resolveRequest = (id: string, value: unknown) => {
      const request = pending.get(id)
      if (!request) return
      pending.delete(id)
      clearTimeout(request.timer)
      nativeBridgeLog('api_request_resolve', {
        id,
        method: request.method,
        path: request.path,
        elapsedMs: Math.round(bridgeNow() - request.startedAt),
        bytes: request.bytes,
        chunks: request.chunksSeen,
        responseBytes: estimateJSONBytes(value),
      })
      request.resolve(value)
    }

    const handleApiChunk = (data: Uint8Array) => {
      let chunk: { id: string; payload: Uint8Array; last: boolean }
      try {
        chunk = parseApiChunk(data)
      } catch (error) {
        rejectOldestApiRequest(pending, error instanceof Error ? error : new Error(String(error)))
        return
      }
      const request = pending.get(chunk.id)
      if (!request) return
      request.chunks.push(chunk.payload)
      request.bytes += chunk.payload.byteLength
      request.chunksSeen += 1
      if (!chunk.last) return
      let response: ReturnType<typeof decodeRuntimeAPIResponse>
      let body: unknown
      try {
        response = decodeRuntimeAPIResponse(concatChunks(request.chunks))
        body = decodeRuntimeResponseBody(request.path, request.method, response.body)
      } catch (error) {
        rejectRequest(chunk.id, error instanceof Error ? error : new Error(String(error)))
        return
      }
      if (response.status >= 400) {
        rejectRequest(chunk.id, new Error(response.error || apiResponseErrorMessage(body, response.status)))
        return
      }
      resolveRequest(chunk.id, body)
    }

    dataSubscription = binaryChannel.onMessage((data) => {
      handleApiChunk(data)
    })
    closeSubscription = binaryChannel.onClose(() => {
      forceClose(new Error('native api channel closed'))
    })

    apiChannel = {
      request<TResponse>(method: string, params?: unknown): Promise<TResponse> {
        if (closed || binaryChannel.readyState !== 'open') {
          return Promise.reject(new Error('native api channel is not open'))
        }
        const payload = normalizeApiRequest(method, params)
        const id = `req_${nextId++}`
        return new Promise<TResponse>((resolve, reject) => {
          const timer = setTimeout(() => {
            rejectRequest(id, new Error(`native api request timed out: ${payload.method} ${payload.path}`))
          }, API_REQUEST_TIMEOUT_MS)
          pending.set(id, {
            chunks: [],
            timer,
            method: payload.method,
            path: payload.path,
            startedAt: bridgeNow(),
            bytes: 0,
            chunksSeen: 0,
            resolve: resolve as (value: unknown) => void,
            reject,
          })
          const requestBytes = encodeRuntimeAPIRequest({
            id,
            method: payload.method,
            path: payload.path,
            body: encodeRuntimeRequestBody(payload.path, payload.method, payload.body),
          })
          nativeBridgeLog('api_request_send', {
            id,
            method: payload.method,
            path: payload.path,
            requestBytes: requestBytes.byteLength,
          })
          if (!session.bridge.sendData(label, requestBytes)) {
            rejectRequest(id, new Error('native bridge WebSocket is not open'))
          }
        })
      },
      close() {
        // API is shared for the lifetime of the native session. Lease-level
        // close calls must not tear down the underlying runtime API channel.
      },
      isOpen() {
        return !closed && binaryChannel.readyState === 'open'
      },
      forceClose,
    }
    return apiChannel
  }

  async openFileTransfer(transferId: string): Promise<RtcBinaryChannel> {
    const label = `file:${this.machineId}:${transferId}`
    await this.openChannel(label)
    return this.makeBinaryChannel(label)
  }

  /** Subscribe to native file transfer progress updates. */
  onTransferSync(handler: (data: TransferSyncPayload) => void): () => void {
    this.transferSyncHandlers.add(handler)
    return () => { this.transferSyncHandlers.delete(handler) }
  }

  subscribeConnectionState(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription {
    this.connectionStateHandlers.add(handler)
    void NativeConnection.getSnapshot({ machineId: this.machineId }).then((snapshot) => {
      if (this.closed) return
      handler(normalizeNativeConnectionState(snapshot))
    }).catch(() => {})
    return { close: () => { this.connectionStateHandlers.delete(handler) } }
  }

  /** Subscribe to full state sync responses. */
  onSyncResponse(handler: (data: SyncResponsePayload) => void): () => void {
    this.syncResponseHandlers.add(handler)
    return () => { this.syncResponseHandlers.delete(handler) }
  }

  /** Tell native to start/cancel a file transfer. */
  sendTransferRequest(request: Record<string, unknown>): void {
    const payload = new TextEncoder().encode(JSON.stringify(request))
    if (!this.sendFrame(FRAME_TRANSFER_REQUEST, CHAN_CONTROL, payload)) {
      throw new Error('native bridge WebSocket is not open')
    }
  }

  /** Request a full state snapshot from native (e.g. after resume). */
  sendSyncRequest(): void {
    this.sendFrame(FRAME_SYNC_REQUEST, CHAN_CONTROL, new Uint8Array(0))
  }

  subscribeEvents(handler: (event: RtcEvent) => void): RtcSubscription {
    this.eventHandlers.add(handler)
    this.ensureEventsChannel()
    return { close: () => this.eventHandlers.delete(handler) }
  }

  private ensureEventsChannel(): void {
    const label = `events:${this.machineId}`
    if (this.bridge.isChannelOpen(label)) {
      this.sendEventsSubscribe(label)
      return
    }
    if (this.eventsChannelPromise) return
    this.eventsChannelPromise = this.openChannel(label).then(() => {
      this.eventsChannelPromise = null
      this.eventsDataCleanup?.()
      this.eventsDataCleanup = this.bridge.onChannelData(label, (payload) => {
        try {
          const event = this.parseRtcEvent(payload)
          for (const h of this.eventHandlers) h(event)
        } catch { /* ignore malformed events */ }
      })
      this.eventsCloseCleanup?.()
      this.eventsCloseCleanup = this.bridge.onChannelClose(label, () => {
        this.eventsChannelPromise = null
        this.resetEventsChannel()
        if (!this.closed && this.eventHandlers.size > 0) {
          this.ensureEventsChannel()
        }
      })
      this.sendEventsSubscribe(label)
    }).catch((error) => {
      this.eventsChannelPromise = null
      throw error
    })
    void this.eventsChannelPromise.catch(() => {})
  }

  private sendEventsSubscribe(label: string): void {
    if (this.eventsSubscribed) return
    const payload = encodeRuntimeEventSubscribeRequest({
      type: 'subscribe',
      types: [1, 2, 3, 4, 10],
    })
    if (this.bridge.sendData(label, payload)) {
      this.eventsSubscribed = true
    }
  }

  private resetEventsChannel(): void {
    this.eventsSubscribed = false
    this.eventsDataCleanup?.()
    this.eventsDataCleanup = null
    this.eventsCloseCleanup?.()
    this.eventsCloseCleanup = null
  }

  private parseRtcEvent(payload: Uint8Array): RtcEvent {
    return runtimeEventEnvelopeToRtcEvent(decodeRuntimeEventEnvelope(payload))
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    let nativeInfo: NativeConnectionInfo | null = null
    try {
      nativeInfo = await NativeConnection.getConnectionInfo({ machineId: this.machineId })
    } catch {
      nativeInfo = null
    }
    const type = nativeInfo?.type ?? (this.relayInUse ? 'relay' : 'unknown')
    return {
      path: this.path,
      connectionId: this.connectionId,
      machineId: this.machineId,
      relayInUse: this.relayInUse || nativeInfo?.relayInUse === true || type === 'relay',
      type,
      ...(nativeInfo?.localAddr ? { localAddr: nativeInfo.localAddr } : {}),
      ...(nativeInfo?.remoteAddr ? { remoteAddr: nativeInfo.remoteAddr } : {}),
      ...(nativeInfo?.candidateType ? { candidateType: nativeInfo.candidateType } : {}),
      ...(nativeInfo?.remoteCandidateType ? { remoteCandidateType: nativeInfo.remoteCandidateType } : {}),
      ...(typeof nativeInfo?.rtt === 'number' ? { rtt: nativeInfo.rtt } : {}),
    }
  }

  async getCapabilities(): Promise<ConnectionCapabilities> {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: this.relayInUse,
    }
  }

  async disconnect(): Promise<void> {
    this.closeBridgeOnly()
    await NativeConnection.release({ machineId: this.machineId })
  }

  closeBridgeOnly(): void {
    if (this.closed) return
    this.closeClientState(new Error('native bridge session closed'), { notifyDisconnect: true })
  }

  isAlive(): boolean {
    if (this.closed) return false
    const snapshot = getCachedNativeState(this.machineId)
    return !snapshot || nativeClientSessionCanStayOpen(snapshot.phase)
  }

  async handleAppResume(): Promise<boolean> {
    if (this.closed) return false
    try {
      await this.bridge.ensureConnected()
      this.sendSyncRequest()
    } catch {
      // Native may still publish state through the reconnect path below.
    }

    const initial = await NativeConnection.getSnapshot({ machineId: this.machineId })
      .then(cacheNativeState)
      .catch(() => null)
    if (!initial) return false
    if (initial?.phase === 'connected') return true
    if (initial?.phase === 'failed') return false

    const ac = new AbortController()
    const timer = setTimeout(() => ac.abort(new Error('native resume verification timed out')), NATIVE_RESUME_WAIT_TIMEOUT_MS)
    try {
      await this.waitUntilConnected(ac.signal)
      return true
    } catch {
      return false
    } finally {
      clearTimeout(timer)
    }
  }

  waitUntilConnected(signal?: AbortSignal): Promise<void> {
    if (this.closed) return Promise.reject(new Error('native bridge session is closed'))
    const snapshot = getCachedNativeState(this.machineId)
    if (!snapshot || snapshot.phase === 'connected') return Promise.resolve()
    if (snapshot.phase === 'failed') {
      return Promise.reject(new Error(snapshot.failReason ?? snapshot.statusText ?? 'connection failed'))
    }
    if (signal?.aborted) return Promise.reject(new Error('aborted'))
    return new Promise<void>((resolve, reject) => {
      const waiter = {
        resolve: () => {
          cleanup()
          resolve()
        },
        reject: (error: Error) => {
          cleanup()
          reject(error)
        },
        cleanup: undefined as (() => void) | undefined,
      }
      const onAbort = () => {
        this.connectedWaiters.delete(waiter)
        waiter.reject(new Error('aborted'))
      }
      const cleanup = () => {
        this.connectedWaiters.delete(waiter)
        if (signal) signal.removeEventListener('abort', onAbort)
      }
      waiter.cleanup = cleanup
      this.connectedWaiters.add(waiter)
      signal?.addEventListener('abort', onAbort, { once: true })
    })
  }

  private resolveConnectedWaiters(): void {
    for (const waiter of Array.from(this.connectedWaiters)) waiter.resolve()
  }

  private rejectConnectedWaiters(error: Error): void {
    for (const waiter of Array.from(this.connectedWaiters)) waiter.reject(error)
  }

  onDisconnect(handler: () => void): RtcSubscription {
    this.disconnectHandlers.add(handler)
    return { close: () => this.disconnectHandlers.delete(handler) }
  }
}

// ─── NativeRtcConnector ───────────────────────────────────────────────────────

export interface NativeRtcConnectInput {
  machineId: string
  connectOpts: Omit<NativeConnectOpts, 'machineId'>
}

export class NativeRtcConnector implements ManagedRtcConnector<{ machineId: string }> {
  private readonly connectOpts: Omit<NativeConnectOpts, 'machineId'>

  constructor(connectOpts: Omit<NativeConnectOpts, 'machineId'>) {
    this.connectOpts = connectOpts
  }

  async connect(input: { machineId: string }, options?: RtcConnectOptions): Promise<NativeRtcSession> {
    const { machineId } = input
    const signal = options?.signal

    try {
      const existing = await readReusableNativeConnection(machineId, options?.forceRelay)
      if (existing) {
        options?.onConnectionState?.(existing)
        await sharedNativeBridgeClient.ensureConnected(signal)
        sharedNativeBridgeClient.sendControl(FRAME_SYNC_REQUEST, new Uint8Array(0))
        return new NativeRtcSession(sharedNativeBridgeClient, machineId, existing.path ?? 'local', existing.relayInUse)
      }
    } catch {
      // Missing snapshots are expected before native has created a store.
    }

    options?.onStatus?.('Connecting through native runtime...')
    options?.onConnectionState?.({
      machineId,
      phase: 'connecting',
      statusText: 'Connecting through native runtime...',
      relayInUse: false,
    })
    const connected = waitForConnected(machineId, signal, CONNECT_TIMEOUT_MS, options)
    try {
      // Register the JS side waiter before starting native connect. Native can
      // reuse an already-connected store and emit no fresh state event.
      await NativeConnection.connect({
        machineId,
        ...this.connectOpts,
        ...(options?.forceRelay !== undefined ? { forceRelay: options.forceRelay } : {}),
      })
      const { path, relayInUse } = await connected.promise

      await sharedNativeBridgeClient.ensureConnected(signal)
      sharedNativeBridgeClient.sendControl(FRAME_SYNC_REQUEST, new Uint8Array(0))

      return new NativeRtcSession(sharedNativeBridgeClient, machineId, path, relayInUse)
    } catch (error) {
      connected.cancel()
      throw error
    }
  }
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

const sharedNativeBridgeClient = new NativeBridgeClient()
const nativeStateCache = new Map<string, RtcConnectionStateSnapshot>()

export function recoverNativeBridgeAfterResume(): void {
  sharedNativeBridgeClient.forceReconnect()
}

function cacheNativeState(data: NativeConnectionSnapshot | NativeStateChangeEvent): RtcConnectionStateSnapshot {
  const snapshot = normalizeNativeConnectionState(data)
  nativeStateCache.set(snapshot.machineId, snapshot)
  return snapshot
}

function getCachedNativeState(machineId: string): RtcConnectionStateSnapshot | null {
  return nativeStateCache.get(machineId) ?? null
}

function nativeClientSessionCanStayOpen(phase: RtcConnectionStateSnapshot['phase']): boolean {
  return phase === 'connected' ||
    phase === 'verifying' ||
    phase === 'reconnecting' ||
    phase === 'waiting_network' ||
    phase === 'probing' ||
    phase === 'connecting'
}

function isNativeStatePayload(data: unknown): data is NativeConnectionSnapshot | NativeStateChangeEvent {
  if (typeof data !== 'object' || data === null || Array.isArray(data)) return false
  const record = data as Record<string, unknown>
  return typeof record.machineId === 'string' &&
    typeof record.phase === 'string' &&
    typeof record.statusText === 'string'
}

function abortable<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise
  if (signal.aborted) return Promise.reject(new Error('aborted'))
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(new Error('aborted'))
    signal.addEventListener('abort', onAbort, { once: true })
    promise.then(
      (value) => {
        signal.removeEventListener('abort', onAbort)
        if (signal.aborted) reject(new Error('aborted'))
        else resolve(value)
      },
      (error) => {
        signal.removeEventListener('abort', onAbort)
        reject(error)
      },
    )
  })
}

async function readReusableNativeConnection(
  machineId: string,
  forceRelay: boolean | undefined,
): Promise<RtcConnectionStateSnapshot | null> {
  const snapshot = await NativeConnection.getSnapshot({ machineId })
  cacheNativeState(snapshot)
  if (snapshot.phase !== 'connected') return null
  if (forceRelay !== undefined && snapshot.forceRelay !== undefined && snapshot.forceRelay !== forceRelay) return null
  const normalized = normalizeNativeConnectionState(snapshot)
  if (forceRelay === true && !normalized.relayInUse) return null
  return normalized
}

interface ConnectedWait {
  promise: Promise<{ path: NativeRuntimePath; relayInUse: boolean }>
  cancel(): void
}

function waitForConnected(
  machineId: string,
  signal?: AbortSignal,
  timeoutMs = CONNECT_TIMEOUT_MS,
  options?: RtcConnectOptions,
): ConnectedWait {
  let settled = false
  let listenerHandle: { remove(): void } | null = null
  let timeoutHandle: ReturnType<typeof setTimeout> | null = null
  let pollHandle: ReturnType<typeof setInterval> | null = null
  let lastSnapshot: NativeConnectionSnapshot | NativeStateChangeEvent | null = null
  let onAbort: (() => void) | null = null

  const cleanup = () => {
    void listenerHandle?.remove()
    listenerHandle = null
    if (timeoutHandle) clearTimeout(timeoutHandle)
    timeoutHandle = null
    if (pollHandle) clearInterval(pollHandle)
    pollHandle = null
    if (onAbort) signal?.removeEventListener('abort', onAbort)
    onAbort = null
  }

  const promise = new Promise<{ path: NativeRuntimePath; relayInUse: boolean }>((resolve, reject) => {
    const finishResolve = (value: { path: NativeRuntimePath; relayInUse: boolean }) => {
      if (settled) return
      settled = true
      cleanup()
      resolve(value)
    }
    const finishReject = (error: Error) => {
      if (settled) return
      settled = true
      cleanup()
      reject(error)
    }
    const inspect = (data: NativeConnectionSnapshot | NativeStateChangeEvent) => {
      if (data.machineId !== machineId) return
      lastSnapshot = data
      const snapshot = cacheNativeState(data)
      options?.onConnectionState?.(snapshot)
      if (data.statusText) options?.onStatus?.(data.statusText)
      if (data.phase === 'connected') {
        finishResolve({
          path: normalizeNativePath(data.path),
          relayInUse: data.relayInUse,
        })
      } else if (data.phase === 'failed') {
        finishReject(new Error(data.failReason ?? data.statusText ?? 'connection failed'))
      }
    }
    const readSnapshot = async () => {
      try {
        inspect(await NativeConnection.getSnapshot({ machineId }))
      } catch {
        // The native store may not exist until connect() has been called.
      }
    }

    if (signal?.aborted) {
      finishReject(new Error('aborted'))
      return
    }

    onAbort = () => {
      finishReject(new Error('aborted'))
    }
    signal?.addEventListener('abort', onAbort)

    NativeConnection.addListener('stateChange', (data: NativeStateChangeEvent) => {
      inspect(data)
    }).then((handle) => {
      if (settled) {
        void handle.remove()
        return
      }
      listenerHandle = handle
      void readSnapshot()
    }).catch((error: unknown) => {
      finishReject(error instanceof Error ? error : new Error(String(error)))
    })

    pollHandle = setInterval(() => {
      void readSnapshot()
    }, 1000)
    timeoutHandle = setTimeout(() => {
      void readSnapshot().finally(() => {
        const detail = lastSnapshot?.statusText ? `: ${lastSnapshot.statusText}` : ''
        finishReject(new Error(`native runtime connection timed out${detail}`))
      })
    }, timeoutMs)
  })

  return {
    promise,
    cancel() {
      if (settled) return
      settled = true
      cleanup()
    },
  }
}

function normalizeNativePath(path: string | null | undefined): NativeRuntimePath {
  if (path === 'hub') return 'hub'
  return 'local'
}

function normalizeNativeConnectionState(data: NativeConnectionSnapshot | NativeStateChangeEvent): RtcConnectionStateSnapshot {
  return {
    machineId: data.machineId,
    phase: normalizeNativePhase(data.phase),
    ...(data.path ? { path: normalizeNativePath(data.path) } : {}),
    statusText: data.statusText || normalizeNativePhaseText(data.phase),
    relayInUse: data.relayInUse === true,
    ...(data.failReason ? { failReason: data.failReason } : {}),
  }
}

function normalizeNativePhase(phase: string): RtcConnectionStateSnapshot['phase'] {
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

function normalizeNativePhaseText(phase: string): string {
  if (phase === 'probing') return 'Probing connection paths...'
  if (phase === 'connecting') return 'Connecting...'
  if (phase === 'connected') return 'Connected'
  if (phase === 'verifying') return 'Verifying connection...'
  if (phase === 'reconnecting') return 'Reconnecting...'
  if (phase === 'waiting_network') return 'Waiting for network...'
  if (phase === 'failed') return 'Connection failed'
  return 'Ready'
}

function bridgeNow(): number {
  return typeof performance !== 'undefined' && typeof performance.now === 'function'
    ? performance.now()
    : Date.now()
}

function nativeBridgeLog(event: string, details: Record<string, unknown> = {}): void {
  try {
    const level = details.level === 'error' || details.level === 'warn' || details.level === 'info' || details.level === 'debug'
      ? details.level
      : 'debug'
    const { level: _level, ...metadata } = details
    const method = level === 'error' ? 'error' : level === 'warn' ? 'warn' : level === 'info' ? 'info' : 'debug'
    console[method](`[termx:native-bridge] ${event}`, metadata)
    writeNativeDebugLog(level, 'NativeBridgeJS', `${event} ${JSON.stringify(metadata)}`)
  } catch {
    // Diagnostics must not affect bridge behavior.
  }
}

function estimateJSONBytes(value: unknown): number {
  try {
    return new TextEncoder().encode(JSON.stringify(value)).byteLength
  } catch {
    return 0
  }
}

function parseApiChunk(bytes: Uint8Array): { id: string; payload: Uint8Array; last: boolean } {
  if (bytes.length < 3 || bytes[0] !== API_CHUNK_MAGIC) {
    throw new Error('invalid native api response chunk')
  }
  const flags = bytes[1] ?? 0
  const idLength = bytes[2] ?? 0
  const idStart = 3
  const idEnd = idStart + idLength
  if (idLength <= 0 || idEnd > bytes.length) {
    throw new Error('invalid native api response chunk')
  }
  return {
    id: new TextDecoder().decode(bytes.slice(idStart, idEnd)),
    payload: bytes.slice(idEnd),
    last: (flags & API_CHUNK_LAST) !== 0,
  }
}

function concatChunks(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0)
  const out = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    out.set(chunk, offset)
    offset += chunk.length
  }
  return out
}

function rejectOldestApiRequest(pending: Map<string, PendingApiRequest>, error: Error): void {
  const first = pending.entries().next()
  if (first.done) return
  const [id, request] = first.value
  pending.delete(id)
  clearTimeout(request.timer)
  request.reject(error)
}

function normalizeApiRequest(method: string, params: unknown): { method: string; path: string; body?: unknown } {
  if (typeof params !== 'object' || params === null || Array.isArray(params)) {
    return { method, path: method }
  }
  const record = params as Record<string, unknown>
  if (typeof record.path !== 'string') {
    return {
      method,
      path: method,
      body: normalizeApiBody(record),
    }
  }
  const body = normalizeApiBody(record.params)
  return {
    method: normalizeApiMethod(method, record.path),
    path: record.path,
    ...(body !== undefined ? { body } : {}),
  }
}

function normalizeApiMethod(method: string, path: string): string {
  if ((path === '/files/list' || path === '/files/stat') && method === 'GET') return 'POST'
  return method
}

function normalizeApiBody(params: unknown): unknown {
  if (typeof params !== 'object' || params === null || Array.isArray(params)) return params
  const record = params as Record<string, unknown>
  if (typeof record.path === 'string') {
    return { ...record, path: record.path }
  }
  return params
}

function apiResponseErrorMessage(body: unknown, status: number): string {
  if (typeof body === 'object' && body !== null && !Array.isArray(body)) {
    const record = body as Record<string, unknown>
    if (typeof record.error === 'string' && record.error) return record.error
    if (typeof record.message === 'string' && record.message) return record.message
  }
  return `native api failed: ${status}`
}

async function openAuthenticatedWebSocket(url: string, token: string, signal?: AbortSignal): Promise<WebSocket> {
  const ws = await openWebSocket(url, signal)
  try {
    await authenticateBridgeSocket(ws, token, signal)
    return ws
  } catch (error) {
    try {
      ws.close()
    } catch { /* ignore */ }
    throw error
  }
}

function authenticateBridgeSocket(ws: WebSocket, token: string, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    let settled = false
    const finishResolve = () => {
      if (settled) return
      settled = true
      cleanup()
      resolve()
    }
    const finishReject = (error: Error) => {
      if (settled) return
      settled = true
      cleanup()
      reject(error)
    }
    const cleanup = () => {
      clearTimeout(timer)
      ws.removeEventListener('message', onMessage)
      ws.removeEventListener('close', onClose)
      ws.removeEventListener('error', onError)
      signal?.removeEventListener('abort', onAbort)
    }
    const onAbort = () => {
      finishReject(new Error('aborted'))
    }
    const onClose = () => {
      finishReject(new Error('native bridge WebSocket closed before authentication'))
    }
    const onError = () => {
      finishReject(new Error('native bridge WebSocket authentication failed'))
    }
    const onMessage = (ev: MessageEvent) => {
      if (!(ev.data instanceof ArrayBuffer)) return
      const frame = parseBridgeFrame(new Uint8Array(ev.data))
      if (!frame) return
      if (frame.frameType === FRAME_AUTH_OK) {
        finishResolve()
        return
      }
      if (frame.frameType === FRAME_CHAN_ERROR) {
        const message = new TextDecoder().decode(frame.payload) || 'native bridge authentication rejected'
        finishReject(new Error(message))
      }
    }
    const timer = setTimeout(() => {
      finishReject(new Error('native bridge authentication timed out'))
    }, 5000)

    if (signal?.aborted) {
      finishReject(new Error('aborted'))
      return
    }
    ws.addEventListener('message', onMessage)
    ws.addEventListener('close', onClose)
    ws.addEventListener('error', onError)
    signal?.addEventListener('abort', onAbort, { once: true })
    sendBridgeFrame(ws, FRAME_AUTH, CHAN_CONTROL, new TextEncoder().encode(token))
  })
}

function parseBridgeFrame(buf: Uint8Array): { frameType: number; channelId: number; payload: Uint8Array } | null {
  if (buf.length < HEADER_SIZE) return null
  const view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength)
  const frameType = view.getUint8(0)
  const channelId = view.getUint16(1, false)
  const payloadLen = view.getUint32(3, false)
  if (payloadLen > buf.length - HEADER_SIZE) return null
  return {
    frameType,
    channelId,
    payload: buf.slice(HEADER_SIZE, HEADER_SIZE + payloadLen),
  }
}

function sendBridgeFrame(ws: WebSocket, frameType: number, channelId: number, payload: Uint8Array): void {
  const buf = new ArrayBuffer(HEADER_SIZE + payload.length)
  const view = new DataView(buf)
  view.setUint8(0, frameType)
  view.setUint16(1, channelId, false)
  view.setUint32(3, payload.length, false)
  new Uint8Array(buf, HEADER_SIZE).set(payload)
  ws.send(buf)
}

function openWebSocket(url: string, signal?: AbortSignal): Promise<WebSocket> {
  return new Promise((resolve, reject) => {
    let settled = false
    const finishResolve = (ws: WebSocket) => {
      if (settled) return
      settled = true
      signal?.removeEventListener('abort', onAbort)
      resolve(ws)
    }
    const finishReject = (error: Error) => {
      if (settled) return
      settled = true
      signal?.removeEventListener('abort', onAbort)
      reject(error)
    }
    const onAbort = () => {
      ws.close()
      finishReject(new Error('aborted'))
    }
    if (signal?.aborted) {
      reject(new Error('aborted'))
      return
    }
    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    signal?.addEventListener('abort', onAbort)
    ws.addEventListener('open', () => {
      finishResolve(ws)
    })
    ws.addEventListener('error', () => {
      finishReject(new Error('WebSocket connection failed'))
    })
    ws.addEventListener('close', () => {
      finishReject(new Error('WebSocket connection closed before opening'))
    })
  })
}
