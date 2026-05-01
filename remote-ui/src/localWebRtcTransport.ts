import type { TerminalTransport, TerminalTransportEvent } from './terminalClient'
import type {
  BinaryChannel,
  ConnectTarget,
  ConnectionInfo,
  ConnectionMode,
  JsonRpcChannel,
  LocalRTCAnswer,
  LocalRTCOffer,
  PeerTransport,
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
}

export interface RTCPeerConnectionLike {
  localDescription: RTCSessionDescriptionInit | null
  createDataChannel(label: string, options?: RTCDataChannelInit): RTCDataChannelLike
  createOffer(): Promise<RTCSessionDescriptionInit>
  setLocalDescription(description: RTCSessionDescriptionInit): Promise<void>
  setRemoteDescription(description: RTCSessionDescriptionInit): Promise<void>
  close(): void | Promise<void>
}

export interface RTCDataChannelLike extends EventTarget {
  readonly label: string
  readyState: RTCDataChannelState
  send(data: string | ArrayBuffer | Blob | ArrayBufferView): void
  close(): void
}

export function createLocalWebRtcPeerTransport(options: LocalWebRtcPeerTransportOptions): PeerTransport & TerminalTransport {
  return new LocalWebRtcPeerTransport(options)
}

class LocalWebRtcPeerTransport implements PeerTransport, TerminalTransport {
  private pc: RTCPeerConnectionLike | null = null
  private connectionId = ''
  private apiChannel: RTCDataChannelLike | null = null
  private terminalChannels = new Map<string, RTCDataChannelLike>()
  private fileChannels = new Map<string, RTCDataChannelLike>()
  private terminalSubscribers = new Map<string, Set<(event: TerminalTransportEvent) => void>>()

  constructor(private readonly options: LocalWebRtcPeerTransportOptions) {}

  async connect(input: ConnectTarget & { mode: ConnectionMode }): Promise<void> {
    this.assertTarget(input.machineId, input.terminalId)
    const pc = (this.options.peerConnectionFactory ?? (() => new RTCPeerConnection()))()
    const sessionId = this.options.sessionIdGenerator?.() ?? crypto.randomUUID()
    this.pc = pc
    this.connectionId = sessionId
    this.ensureTerminalChannel(this.options.terminalId)
    this.ensureAPIChannel()

    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
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
  }

  async disconnect(): Promise<void> {
    for (const channel of this.terminalChannels.values()) channel.close()
    for (const channel of this.fileChannels.values()) channel.close()
    this.apiChannel?.close()
    await this.pc?.close()
    this.pc = null
    this.apiChannel = null
    this.terminalChannels.clear()
    this.fileChannels.clear()
  }

  async openTerminal(terminalId: string): Promise<BinaryChannel> {
    this.assertTarget(this.options.machineId, terminalId)
    const channel = this.ensureTerminalChannel(terminalId)
    return toBinaryChannel(channel)
  }

  async openApi(): Promise<JsonRpcChannel> {
    return new LocalApiChannel(this.ensureAPIChannel())
  }

  async openFileTransfer(transferId: string): Promise<BinaryChannel> {
    const channel = this.openRTCChannel(`file:${transferId}`)
    this.fileChannels.set(transferId, channel)
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
    this.terminalChannels.get(terminalId)?.close()
  }

  private openRTCChannel(label: string): RTCDataChannelLike {
    if (!this.pc) throw new Error('local WebRTC transport is not connected')
    return this.pc.createDataChannel(label, { ordered: true })
  }

  private ensureTerminalChannel(terminalId: string): RTCDataChannelLike {
    const existing = this.terminalChannels.get(terminalId)
    if (existing) return existing
    const channel = this.openRTCChannel(`terminal:${terminalId}`)
    this.terminalChannels.set(terminalId, channel)
    channel.addEventListener('message', (event) => {
      this.emitTerminal(terminalId, { type: 'output', data: messageBytes((event as MessageEvent).data) })
    })
    channel.addEventListener('close', () => {
      this.emitTerminal(terminalId, { type: 'closed' })
    })
    return channel
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

  private emitTerminal(terminalId: string, event: TerminalTransportEvent): void {
    for (const handler of this.terminalSubscribers.get(terminalId) ?? []) {
      handler(event)
    }
  }
}

class LocalApiChannel implements JsonRpcChannel {
  private nextID = 1
  private readonly waiters = new Map<string, {
    chunks: Uint8Array[]
    resolve: (value: unknown) => void
    reject: (err: Error) => void
  }>()

  constructor(private readonly channel: RTCDataChannelLike) {
    channel.addEventListener('message', (event) => this.handleMessage((event as MessageEvent).data))
  }

  request<TResponse>(method: string, params?: unknown): Promise<TResponse> {
    const payload = normalizeAPIRequest(method, params)
    const id = `req_${this.nextID++}`
    return new Promise<TResponse>((resolve, reject) => {
      this.waiters.set(id, {
        chunks: [],
        resolve: (value) => resolve(value as TResponse),
        reject,
      })
      this.channel.send(JSON.stringify({
        id,
        method: payload.method,
        path: payload.path,
        body: payload.body,
      }))
    })
  }

  close(): void {
    this.channel.close()
  }

  private handleMessage(data: unknown): void {
    const frame = parseAPIChunk(messageBytes(data))
    const waiter = this.waiters.get(frame.id)
    if (!waiter) return
    waiter.chunks.push(frame.payload)
    if (!frame.last) return
    this.waiters.delete(frame.id)
    const response = JSON.parse(new TextDecoder().decode(concatChunks(waiter.chunks))) as {
      status: number
      body: unknown
    }
    if (response.status >= 400) {
      const error = response.body as { error?: string; message?: string }
      waiter.reject(new Error(error.error ?? error.message ?? `local api failed: ${response.status}`))
      return
    }
    waiter.resolve(response.body)
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

function parseAPIChunk(bytes: Uint8Array): { id: string; payload: Uint8Array; last: boolean } {
  if (bytes[0] !== 0xc0) {
    throw new Error('invalid api response chunk')
  }
  const flags = bytes[1] ?? 0
  const idLength = bytes[2] ?? 0
  const idStart = 3
  const idEnd = idStart + idLength
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

function messageBytes(data: unknown): Uint8Array {
  if (data instanceof Uint8Array) return data
  if (data instanceof ArrayBuffer) return new Uint8Array(data)
  if (typeof data === 'string') return new TextEncoder().encode(data)
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
  }
  throw new Error('unsupported data channel message')
}
