import type { TerminalResizePolicy, TerminalTransport, TerminalTransportEvent } from './terminalClient'
import { createLocalTerminalProtocolTransport } from './localTerminalProtocolTransport'
import { TERMX_FRAME_TYPES, TERMX_PROTOCOL_VERSION, decodeTermxFrame, encodeTermxFrame } from './termxProtocol'
import type {
  BinaryChannel,
  ConnectTarget,
  ConnectionInfo,
  ConnectionMode,
  JsonRpcChannel,
  LocalInventoryRTCAnswer,
  LocalInventoryRTCOffer,
  LocalRTCAnswer,
  LocalRTCOffer,
  PeerTransport,
  TerminalInventoryEvent,
  TerminalInventoryEvents,
  TerminalInventorySubscription,
} from './transport'

export interface LocalOfferSignature {
  signature: string
  nonce: string
  timestamp: string
}

export interface LocalWebRtcPeerTransportOptions {
  machineId: string
  terminalId: string
  mode?: ConnectionMode | undefined
  appCertificate: string
  peerConnectionFactory?: (() => RTCPeerConnectionLike) | undefined
  createAnswer(input: LocalRTCOffer): Promise<LocalRTCAnswer>
  signOffer(input: { sessionId: string; machineId: string; terminalId: string; sdp: string }): Promise<LocalOfferSignature>
  sessionIdGenerator?: (() => string) | undefined
  iceGatheringTimeoutMs?: number | undefined
  dataChannelOpenTimeoutMs?: number | undefined
  terminalResizePolicy?: TerminalResizePolicy | undefined
}

export interface LocalWebRtcInventoryEventsOptions {
  machineId: string
  appCertificate: string
  peerConnectionFactory?: (() => RTCPeerConnectionLike) | undefined
  createAnswer(input: LocalInventoryRTCOffer): Promise<LocalInventoryRTCAnswer>
  signOffer(input: { sessionId: string; machineId: string; terminalId: string; sdp: string }): Promise<LocalOfferSignature>
  sessionIdGenerator?: (() => string) | undefined
  iceGatheringTimeoutMs?: number | undefined
  dataChannelOpenTimeoutMs?: number | undefined
}

export interface RTCPeerConnectionLike {
  localDescription: RTCSessionDescriptionInit | null
  readonly iceGatheringState?: RTCIceGatheringState
  createDataChannel(label: string, options?: RTCDataChannelInit): RTCDataChannelLike
  createOffer(): Promise<RTCSessionDescriptionInit>
  setLocalDescription(description: RTCSessionDescriptionInit): Promise<void>
  setRemoteDescription(description: RTCSessionDescriptionInit): Promise<void>
  close(): void | Promise<void>
  addEventListener?(type: 'icegatheringstatechange', listener: EventListener): void
  removeEventListener?(type: 'icegatheringstatechange', listener: EventListener): void
}

export interface RTCDataChannelLike extends EventTarget {
  readonly label: string
  readyState: RTCDataChannelState
  binaryType?: BinaryType
  send(data: string | ArrayBuffer | Blob | ArrayBufferView): void
  close(): void
}

export function createLocalWebRtcPeerTransport(options: LocalWebRtcPeerTransportOptions): PeerTransport & TerminalTransport {
  return new LocalWebRtcPeerTransport(options)
}

export function createLocalWebRtcInventoryEvents(options: LocalWebRtcInventoryEventsOptions): TerminalInventoryEvents {
  return new LocalWebRtcInventoryEventsConnection(options)
}

class LocalWebRtcPeerTransport implements PeerTransport, TerminalTransport {
  private pc: RTCPeerConnectionLike | null = null
  private connectionId = ''
  private apiChannel: RTCDataChannelLike | null = null
  private terminalChannels = new Map<string, RTCDataChannelLike>()
  private terminalProtocols = new Map<string, TerminalTransport>()
  private terminalSubscribers = new Map<string, Set<(event: TerminalTransportEvent) => void>>()
  private fileChannels = new Map<string, RTCDataChannelLike>()

  constructor(private readonly options: LocalWebRtcPeerTransportOptions) {}

  async connect(input: ConnectTarget & { mode: ConnectionMode }): Promise<void> {
    try {
      this.assertTarget(input.machineId, input.terminalId)
      const pc = (this.options.peerConnectionFactory ?? (() => new RTCPeerConnection()))()
      const sessionId = this.options.sessionIdGenerator?.() ?? crypto.randomUUID()
      this.pc = pc
      this.connectionId = sessionId
      this.ensureTerminalChannel(this.options.terminalId)
      this.ensureAPIChannel()

      const offer = await pc.createOffer()
      await pc.setLocalDescription(offer)
      await waitForICEGatheringComplete(pc, this.options.iceGatheringTimeoutMs)
      const sdp = pc.localDescription?.sdp ?? offer.sdp
      if (!sdp) throw new Error('local WebRTC offer SDP is required')
      const signed = await this.options.signOffer({
        sessionId,
        machineId: this.options.machineId,
        terminalId: this.options.terminalId,
        sdp,
      })
      const answer = await this.options.createAnswer({
        sessionId,
        machineId: this.options.machineId,
        terminalId: this.options.terminalId,
        sdp,
        appCertificate: this.options.appCertificate,
        appSignature: signed.signature,
        nonce: signed.nonce,
        timestamp: signed.timestamp,
      })
      await pc.setRemoteDescription(answer.answer)
      await waitChannelOpenWithTimeout(this.ensureAPIChannel(), this.options.dataChannelOpenTimeoutMs)
    } catch (err) {
      await this.disconnect()
      throw err
    }
  }

  async disconnect(): Promise<void> {
    for (const channel of this.terminalChannels.values()) channel.close()
    for (const channel of this.fileChannels.values()) channel.close()
    this.apiChannel?.close()
    await this.pc?.close()
    this.pc = null
    this.apiChannel = null
    this.terminalChannels.clear()
    this.terminalProtocols.clear()
    this.fileChannels.clear()
  }

  async openTerminal(terminalId: string): Promise<BinaryChannel> {
    this.assertTarget(this.options.machineId, terminalId)
    return this.ensureTerminalProtocol(terminalId).openTerminal(terminalId)
  }

  async openApi(): Promise<JsonRpcChannel> {
    return new LocalApiChannel(this.ensureAPIChannel())
  }

  async openFileTransfer(transferId: string): Promise<BinaryChannel> {
    const channel = this.openRTCChannel(`file:${transferId}`)
    this.fileChannels.set(transferId, channel)
    await waitChannelOpen(channel)
    return toBinaryChannel(channel)
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    return {
      mode: this.options.mode ?? 'local',
      connectionId: this.connectionId || 'local-webrtc',
      machineId: this.options.machineId,
      terminalId: this.options.terminalId,
      relayInUse: false,
    }
  }

  subscribeTerminal(terminalId: string, handler: (event: TerminalTransportEvent) => void): () => void {
    this.assertTarget(this.options.machineId, terminalId)
    let handlers = this.terminalSubscribers.get(terminalId)
    if (!handlers) {
      handlers = new Set()
      this.terminalSubscribers.set(terminalId, handlers)
    }
    handlers.add(handler)
    return () => {
      handlers?.delete(handler)
    }
  }

  closeTerminalChannel(terminalId: string): void {
    this.assertTarget(this.options.machineId, terminalId)
    const protocol = this.terminalProtocols.get(terminalId)
    if (protocol) {
      protocol.closeTerminalChannel(terminalId)
    } else {
      this.terminalChannels.get(terminalId)?.close()
    }
    this.terminalProtocols.delete(terminalId)
    this.terminalChannels.delete(terminalId)
  }

  private openRTCChannel(label: string): RTCDataChannelLike {
    if (!this.pc) throw new Error('local WebRTC transport is not connected')
    return this.pc.createDataChannel(label, { ordered: true })
  }

  private ensureTerminalChannel(terminalId: string): RTCDataChannelLike {
    const existing = this.terminalChannels.get(terminalId)
    if (existing) return existing
    const channel = this.openRTCChannel(`terminal:${terminalId}`)
    channel.binaryType = 'arraybuffer'
    this.terminalChannels.set(terminalId, channel)
    channel.addEventListener('close', () => {
      this.terminalProtocols.delete(terminalId)
      this.terminalChannels.delete(terminalId)
    })
    return channel
  }

  private ensureTerminalProtocol(terminalId: string): TerminalTransport {
    this.assertTarget(this.options.machineId, terminalId)
    const existing = this.terminalProtocols.get(terminalId)
    if (existing) return existing
    const channel = this.ensureTerminalChannel(terminalId)
    const protocol = createLocalTerminalProtocolTransport({
      channel: toProtocolBinaryChannel(channel),
      machineId: this.options.machineId,
      terminalId,
      connectionInfo: {
        mode: this.options.mode ?? 'local',
        connectionId: this.connectionId || 'local-webrtc',
        machineId: this.options.machineId,
        terminalId,
        relayInUse: false,
      },
      resizePolicy: this.options.terminalResizePolicy ?? 'follower',
    })
    protocol.subscribeTerminal(terminalId, (event) => {
      this.emitTerminalEvent(terminalId, event)
    })
    this.terminalProtocols.set(terminalId, protocol)
    return protocol
  }

  private emitTerminalEvent(terminalId: string, event: TerminalTransportEvent): void {
    for (const handler of this.terminalSubscribers.get(terminalId) ?? []) {
      handler(event)
    }
  }

  private ensureAPIChannel(): RTCDataChannelLike {
    if (this.apiChannel) return this.apiChannel
    this.apiChannel = this.openRTCChannel('api')
    return this.apiChannel
  }

  private assertTarget(machineId: string, terminalId?: string): void {
    if (machineId !== this.options.machineId) {
      throw new Error(`local WebRTC machine mismatch: ${machineId} != ${this.options.machineId}`)
    }
    if (terminalId !== undefined && terminalId !== this.options.terminalId) {
      throw new Error(`local WebRTC terminal mismatch: ${terminalId} != ${this.options.terminalId}`)
    }
  }

}

class LocalWebRtcInventoryEventsConnection implements TerminalInventoryEvents {
  private pc: RTCPeerConnectionLike | null = null
  private connectionId = ''
  private eventsChannel: RTCDataChannelLike | null = null
  private readonly subscribers = new Set<(event: TerminalInventoryEvent) => void>()
  private readonly pending = new Map<number, {
    resolve: (value: unknown) => void
    reject: (err: Error) => void
  }>()
  private nextRequestID = 1
  private helloDone: Promise<void> | null = null
  private subscribeDone: Promise<void> | null = null
  private connectPromise: Promise<void> | null = null
  private disconnecting = false

  constructor(private readonly options: LocalWebRtcInventoryEventsOptions) {}

  subscribe(machineId: string, handler: (event: TerminalInventoryEvent) => void): TerminalInventorySubscription {
    if (machineId !== this.options.machineId) {
      throw new Error(`local inventory events machine mismatch: ${machineId} != ${this.options.machineId}`)
    }
    this.subscribers.add(handler)
    if (this.subscribers.size === 1) {
      void this.ensureConnected().catch(() => {})
    }
    return {
      close: () => {
        this.subscribers.delete(handler)
        if (this.subscribers.size === 0) {
          void this.disconnect()
        }
      },
    }
  }

  private ensureConnected(): Promise<void> {
    if (this.connectPromise) return this.connectPromise
    this.connectPromise = this.connectInternal().catch(async (err) => {
      await this.disconnectInternal()
      throw err
    })
    return this.connectPromise
  }

  private async connectInternal(): Promise<void> {
    const pc = (this.options.peerConnectionFactory ?? (() => new RTCPeerConnection()))()
    const sessionId = this.options.sessionIdGenerator?.() ?? crypto.randomUUID()
    this.pc = pc
    this.connectionId = sessionId
    const eventsChannel = pc.createDataChannel('events', { ordered: true })
    eventsChannel.binaryType = 'arraybuffer'
    this.eventsChannel = eventsChannel
    eventsChannel.addEventListener('message', (event) => this.handleMessage((event as MessageEvent).data))
    eventsChannel.addEventListener('close', () => {
      void this.disconnect()
    })
    eventsChannel.addEventListener('error', () => {
      void this.disconnect()
    })

    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    await waitForICEGatheringComplete(pc, this.options.iceGatheringTimeoutMs)
    const sdp = pc.localDescription?.sdp ?? offer.sdp
    if (!sdp) throw new Error('local WebRTC offer SDP is required')
    const signed = await this.options.signOffer({
      sessionId,
      machineId: this.options.machineId,
      terminalId: '',
      sdp,
    })
    const answer = await this.options.createAnswer({
      sessionId,
      machineId: this.options.machineId,
      sdp,
      appCertificate: this.options.appCertificate,
      appSignature: signed.signature,
      nonce: signed.nonce,
      timestamp: signed.timestamp,
    })
    await pc.setRemoteDescription(answer.answer)
    await waitChannelOpenWithTimeout(eventsChannel, this.options.dataChannelOpenTimeoutMs)
    await this.hello()
    await this.subscribeInventoryEvents()
  }

  private async disconnect(): Promise<void> {
    await this.disconnectInternal()
  }

  private async disconnectInternal(): Promise<void> {
    if (this.disconnecting) return
    this.disconnecting = true
    this.rejectPending(new Error('inventory events channel closed'))
    const eventsChannel = this.eventsChannel
    this.eventsChannel = null
    eventsChannel?.close()
    await this.pc?.close()
    this.pc = null
    this.connectPromise = null
    this.helloDone = null
    this.subscribeDone = null
    this.disconnecting = false
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
          client: 'termx-local-web-events',
          capabilities: ['events'],
        })
      })
    }
    return this.helloDone
  }

  private subscribeInventoryEvents(): Promise<void> {
    if (!this.subscribeDone) {
      this.subscribeDone = this.request('events', {
        types: [1, 2, 3, 4, 10],
      }).then(() => {})
    }
    return this.subscribeDone
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

  private sendFrame(channel: number, type: number, payload: unknown): void {
    if (!this.eventsChannel) throw new Error('inventory events channel is not connected')
    const bytes = payload instanceof Uint8Array
      ? payload
      : new TextEncoder().encode(JSON.stringify(payload))
    this.eventsChannel.send(encodeTermxFrame(channel, type, bytes))
  }

  private handleMessage(data: unknown): void {
    try {
      const bytes = messageBytes(data)
      if (bytes instanceof Uint8Array) {
        this.handleMessageBytes(bytes)
        return
      }
      void bytes.then((resolved) => this.handleMessageBytes(resolved))
    } catch {
      void this.disconnect()
    }
  }

  private handleMessageBytes(data: Uint8Array): void {
    const frame = decodeTermxFrame(data)
    if (frame.channel !== 0) return
    switch (frame.type) {
      case TERMX_FRAME_TYPES.hello:
        this.pending.get(0)?.resolve(undefined)
        this.pending.delete(0)
        return
      case TERMX_FRAME_TYPES.response: {
        const response = JSON.parse(new TextDecoder().decode(frame.payload)) as { id: number; result?: unknown }
        const pending = this.pending.get(response.id)
        if (!pending) return
        this.pending.delete(response.id)
        pending.resolve(response.result)
        return
      }
      case TERMX_FRAME_TYPES.error: {
        const response = JSON.parse(new TextDecoder().decode(frame.payload)) as { id: number; error?: { message?: string } }
        const pending = this.pending.get(response.id)
        if (!pending) return
        this.pending.delete(response.id)
        pending.reject(new Error(response.error?.message ?? 'termx protocol error'))
        return
      }
      case TERMX_FRAME_TYPES.event:
        for (const handler of this.subscribers) {
          handler({ type: 'inventory_changed' })
        }
        return
    }
  }

  private rejectPending(err: Error): void {
    for (const [id, pending] of Array.from(this.pending.entries())) {
      this.pending.delete(id)
      pending.reject(err)
    }
  }
}

class LocalApiChannel implements JsonRpcChannel {
  private static readonly openTimeoutMs = 10000
  private static readonly responseTimeoutMs = 10000
  private nextID = 1
  private readonly openPromise: Promise<void>
  private readonly waiters = new Map<string, {
    chunks: Uint8Array[]
    timeout: ReturnType<typeof setTimeout> | null
    resolve: (value: unknown) => void
    reject: (err: Error) => void
  }>()

  constructor(private readonly channel: RTCDataChannelLike) {
    this.openPromise = waitChannelOpen(channel)
    channel.addEventListener('message', (event) => this.handleMessage((event as MessageEvent).data))
    channel.addEventListener('close', () => this.rejectPending(new Error(`api data channel ${channel.label} closed`)))
    channel.addEventListener('error', () => this.rejectPending(new Error(`api data channel ${channel.label} failed`)))
  }

  request<TResponse>(method: string, params?: unknown): Promise<TResponse> {
    const payload = normalizeAPIRequest(method, params)
    const id = `req_${this.nextID++}`
    return new Promise<TResponse>((resolve, reject) => {
      this.waiters.set(id, {
        chunks: [],
        timeout: null,
        resolve: (value) => {
          this.clearWaiterTimeout(id)
          resolve(value as TResponse)
        },
        reject: (err) => {
          this.clearWaiterTimeout(id)
          reject(err)
        },
      })
      const rejectAndDelete = (err: unknown) => {
        this.rejectWaiter(id, err instanceof Error ? err : new Error(String(err)))
      }
      const sendRequest = () => {
        try {
          this.startResponseTimeout(id)
          this.channel.send(JSON.stringify({
            id,
            method: payload.method,
            path: payload.path,
            body: payload.body,
          }))
        } catch (err) {
          rejectAndDelete(err)
        }
      }
      if (this.channel.readyState === 'open') {
        sendRequest()
        return
      }
      void withTimeout(
        this.openPromise,
        LocalApiChannel.openTimeoutMs,
        () => new Error(`timed out opening data channel ${this.channel.label}`),
      ).then(sendRequest, (err: unknown) => rejectAndDelete(err))
    })
  }

  close(): void {
    this.rejectPending(new Error(`api data channel ${this.channel.label} closed`))
    this.channel.close()
  }

  private handleMessage(data: unknown): void {
    try {
      const bytes = messageBytes(data)
      if (bytes instanceof Uint8Array) {
        this.handleMessageBytes(bytes)
        return
      }
      void bytes.then(
        (resolved) => this.handleMessageBytes(resolved),
        (err: unknown) => this.rejectOldestPending(err instanceof Error ? err : new Error(String(err))),
      )
    } catch (err) {
      this.rejectOldestPending(err instanceof Error ? err : new Error(String(err)))
    }
  }

  private handleMessageBytes(data: Uint8Array): void {
    let frame: { id: string; payload: Uint8Array; last: boolean }
    try {
      frame = parseAPIChunk(data)
    } catch (err) {
      this.rejectOldestPending(err instanceof Error ? err : new Error(String(err)))
      return
    }
    const waiter = this.waiters.get(frame.id)
    if (!waiter) return
    waiter.chunks.push(frame.payload)
    if (!frame.last) return
    let response: {
      status: number
      body: unknown
    }
    try {
      response = JSON.parse(new TextDecoder().decode(concatChunks(waiter.chunks))) as {
        status: number
        body: unknown
      }
    } catch (err) {
      this.rejectWaiter(frame.id, err instanceof Error ? err : new Error(String(err)))
      return
    }
    if (response.status >= 400) {
      const error = response.body as { error?: string; message?: string }
      this.rejectWaiter(frame.id, new Error(error.error ?? error.message ?? `local api failed: ${response.status}`))
      return
    }
    this.resolveWaiter(frame.id, response.body)
  }

  private rejectPending(err: Error): void {
    for (const id of Array.from(this.waiters.keys())) {
      this.rejectWaiter(id, err)
    }
  }

  private rejectOldestPending(err: Error): void {
    const first = this.waiters.entries().next()
    if (first.done) return
    this.rejectWaiter(first.value[0], err)
  }

  private startResponseTimeout(id: string): void {
    const waiter = this.waiters.get(id)
    if (!waiter || waiter.timeout) return
    waiter.timeout = setTimeout(() => {
      this.rejectWaiter(id, new Error(`timed out waiting for api response ${id}`))
    }, LocalApiChannel.responseTimeoutMs)
  }

  private resolveWaiter(id: string, value: unknown): void {
    const waiter = this.waiters.get(id)
    if (!waiter) return
    this.waiters.delete(id)
    if (waiter.timeout) clearTimeout(waiter.timeout)
    waiter.resolve(value)
  }

  private rejectWaiter(id: string, err: Error): void {
    const waiter = this.waiters.get(id)
    if (!waiter) return
    this.waiters.delete(id)
    if (waiter.timeout) clearTimeout(waiter.timeout)
    waiter.reject(err)
  }

  private clearWaiterTimeout(id: string): void {
    const waiter = this.waiters.get(id)
    if (!waiter?.timeout) return
    clearTimeout(waiter.timeout)
    waiter.timeout = null
  }
}

function normalizeAPIRequest(method: string, params: unknown): { method: string; path: string; body?: unknown } {
  if (typeof params !== 'object' || params === null || Array.isArray(params)) {
    return { method, path: method }
  }
  const record = params as Record<string, unknown>
  if (typeof record.path !== 'string') {
    throw new Error('api request path is required')
  }
  const body = normalizeAPIBody(record.params)
  return {
    method: normalizeAPIMethod(method, record.path),
    path: record.path,
    ...(body !== undefined ? { body } : {}),
  }
}

function normalizeAPIMethod(method: string, path: string): string {
  if ((path === '/files/list' || path === '/files/stat') && method === 'GET') return 'POST'
  return method
}

function normalizeAPIBody(params: unknown): unknown {
  if (typeof params !== 'object' || params === null || Array.isArray(params)) return params
  const record = params as Record<string, unknown>
  if (typeof record.path === 'string') {
    const body: Record<string, unknown> = { path: record.path }
    if (typeof record.offset === 'number') body.offset = record.offset
    if (typeof record.limit === 'number') body.limit = record.limit
    return body
  }
  return params
}

function toBinaryChannel(channel: RTCDataChannelLike): BinaryChannel {
  return {
    label: channel.label,
    get readyState() {
      return channel.readyState
    },
    send(data: Uint8Array) {
      channel.send(data)
    },
    close() {
      channel.close()
    },
  }
}

function toProtocolBinaryChannel(channel: RTCDataChannelLike): BinaryChannel & {
  onMessage(handler: (data: Uint8Array) => void): void
  onClose(handler: () => void): void
  waitOpen(): Promise<void>
} {
  return {
    label: channel.label,
    get readyState() {
      return channel.readyState
    },
    send(data: Uint8Array) {
      channel.send(data)
    },
    close() {
      channel.close()
    },
    onMessage(handler: (data: Uint8Array) => void) {
      channel.addEventListener('message', (event) => {
        const bytes = messageBytes((event as MessageEvent).data)
        if (bytes instanceof Uint8Array) {
          handler(bytes)
          return
        }
        void bytes.then(handler)
      })
    },
    onClose(handler: () => void) {
      channel.addEventListener('close', () => handler())
    },
    waitOpen() {
      return waitChannelOpen(channel)
    },
  }
}

function waitChannelOpen(channel: RTCDataChannelLike): Promise<void> {
  if (channel.readyState === 'open') return Promise.resolve()
  if (channel.readyState === 'closed') return Promise.reject(new Error(`data channel ${channel.label} is closed`))
  return new Promise((resolve, reject) => {
    const onOpen = () => {
      cleanup()
      resolve()
    }
    const onClose = () => {
      cleanup()
      reject(new Error(`data channel ${channel.label} closed before opening`))
    }
    const onError = () => {
      cleanup()
      reject(new Error(`data channel ${channel.label} failed before opening`))
    }
    const cleanup = () => {
      channel.removeEventListener('open', onOpen)
      channel.removeEventListener('close', onClose)
      channel.removeEventListener('error', onError)
    }
    channel.addEventListener('open', onOpen)
    channel.addEventListener('close', onClose)
    channel.addEventListener('error', onError)
  })
}

function waitChannelOpenWithTimeout(channel: RTCDataChannelLike, timeoutMs = 10000): Promise<void> {
  return withTimeout(
    waitChannelOpen(channel),
    timeoutMs,
    () => new Error(`timed out opening data channel ${channel.label}`),
  )
}

function waitForICEGatheringComplete(pc: RTCPeerConnectionLike, timeoutMs = 10000): Promise<void> {
  if (pc.iceGatheringState === undefined || pc.iceGatheringState === 'complete') {
    return Promise.resolve()
  }
  if (!pc.addEventListener || !pc.removeEventListener) {
    return Promise.resolve()
  }
  return new Promise<void>((resolve, reject) => {
    const cleanup = () => {
      clearTimeout(timer)
      pc.removeEventListener?.('icegatheringstatechange', onStateChange)
    }
    const onStateChange = () => {
      if (pc.iceGatheringState !== 'complete') return
      cleanup()
      resolve()
    }
    const timer = setTimeout(() => {
      cleanup()
      reject(new Error('timed out waiting for local ICE candidates'))
    }, timeoutMs)
    pc.addEventListener?.('icegatheringstatechange', onStateChange)
    onStateChange()
  })
}

function withTimeout<T>(promise: Promise<T>, timeoutMs: number, createError: () => Error): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(createError()), timeoutMs)
    promise.then(
      (value) => {
        clearTimeout(timer)
        resolve(value)
      },
      (err: unknown) => {
        clearTimeout(timer)
        reject(err)
      },
    )
  })
}

function parseAPIChunk(bytes: Uint8Array): { id: string; payload: Uint8Array; last: boolean } {
  if (bytes.length < 3 || bytes[0] !== 0xc0) {
    throw new Error('invalid api response chunk')
  }
  const flags = bytes[1] ?? 0
  const idLength = bytes[2] ?? 0
  const idStart = 3
  const idEnd = idStart + idLength
  if (idLength <= 0 || idEnd > bytes.length) {
    throw new Error('invalid api response chunk')
  }
  return {
    id: new TextDecoder().decode(bytes.slice(idStart, idEnd)),
    payload: bytes.slice(idEnd),
    last: (flags & 0x02) !== 0,
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

function messageBytes(data: unknown): Uint8Array | Promise<Uint8Array> {
  if (data instanceof Uint8Array) return data
  if (data instanceof ArrayBuffer) return new Uint8Array(data)
  if (typeof Blob !== 'undefined' && data instanceof Blob) {
    if (typeof data.arrayBuffer === 'function') {
      return data.arrayBuffer().then((buffer) => new Uint8Array(buffer))
    }
    return blobBytesWithFileReader(data)
  }
  if (typeof data === 'string') return new TextEncoder().encode(data)
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
  }
  throw new Error('unsupported data channel message')
}

function blobBytesWithFileReader(blob: Blob): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error ?? new Error('failed to read data channel Blob'))
    reader.onload = () => {
      const result = reader.result
      if (!(result instanceof ArrayBuffer)) {
        reject(new Error('failed to read data channel Blob as ArrayBuffer'))
        return
      }
      resolve(new Uint8Array(result))
    }
    reader.readAsArrayBuffer(blob)
  })
}
