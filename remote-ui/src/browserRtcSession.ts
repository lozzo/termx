import { TERMX_FRAME_TYPES, decodeTermxFrame } from './termxProtocol'
import type { LocalOfferSignature } from './localAppIdentity'
import type {
  ConnectionCapabilities,
  ConnectionInfo,
  ConnectionPath,
  RtcOfferSigningInput,
  RtcSessionCapabilityUpdater,
  LocalInventoryRTCAnswer,
  LocalInventoryRTCOffer,
  RtcBinaryChannel,
  RtcEvent,
  RtcJsonRpcChannel,
  RtcSession,
  RtcSessionAnswerTarget,
  RtcSessionAnswerer,
  RtcSessionDescription,
  RtcSessionNegotiator,
  RtcSessionNegotiationTarget,
  RtcSubscription,
  TerminalInventoryEvent,
  TerminalInventoryEvents,
  TerminalInventorySubscription,
} from './transport'

export interface BrowserRtcSessionOptions {
  machineId: string
  terminalId?: string | undefined
  path?: 'local' | 'public_p2p' | 'managed' | undefined
  peerConnectionFactory?: ((configuration?: RTCConfiguration) => RTCPeerConnectionLike) | undefined
  sessionIdGenerator?: (() => string) | undefined
  iceGatheringTimeoutMs?: number | undefined
  dataChannelOpenTimeoutMs?: number | undefined
  heartbeatIntervalMs?: number | undefined
}

export interface BrowserRtcInventoryEventsOptions {
  machineId: string
  appCertificate: string
  peerConnectionFactory?: (() => RTCPeerConnectionLike) | undefined
  createAnswer(input: LocalInventoryRTCOffer): Promise<LocalInventoryRTCAnswer>
  signOffer(input: RtcOfferSigningInput): Promise<LocalOfferSignature>
  sessionIdGenerator?: (() => string) | undefined
  iceGatheringTimeoutMs?: number | undefined
  dataChannelOpenTimeoutMs?: number | undefined
}

export interface BrowserRtcSessionLifecycle {
  onDisconnect(handler: () => void): RtcSubscription
  isAlive(): boolean
  handleAppResume(): Promise<boolean>
}

export type BrowserRtcConnectedSession = RtcSession & RtcSessionNegotiator & RtcSessionAnswerer & RtcSessionCapabilityUpdater & BrowserRtcSessionLifecycle

export interface RTCPeerConnectionLike {
  localDescription: RTCSessionDescriptionInit | null
  readonly iceGatheringState?: RTCIceGatheringState
  readonly connectionState?: RTCPeerConnectionState
  createDataChannel(label: string, options?: RTCDataChannelInit): RTCDataChannelLike
  createOffer(): Promise<RTCSessionDescriptionInit>
  createAnswer(): Promise<RTCSessionDescriptionInit>
  setLocalDescription(description: RTCSessionDescriptionInit): Promise<void>
  setRemoteDescription(description: RTCSessionDescriptionInit): Promise<void>
  close(): void | Promise<void>
  addEventListener?(type: 'icegatheringstatechange' | 'datachannel' | 'connectionstatechange', listener: EventListener): void
  removeEventListener?(type: 'icegatheringstatechange' | 'datachannel' | 'connectionstatechange', listener: EventListener): void
}

export interface RTCDataChannelLike extends EventTarget {
  readonly label: string
  readyState: RTCDataChannelState
  binaryType?: BinaryType
  send(data: string | ArrayBuffer | Blob | ArrayBufferView): void
  close(): void
}

declare global {
  interface Navigator {
    connection?: NetworkInformationLike
  }
}

interface NetworkInformationLike {
  type?: string | undefined
  effectiveType?: string | undefined
  addEventListener?(type: 'change', listener: EventListener): void
  removeEventListener?(type: 'change', listener: EventListener): void
}

export function createBrowserRtcSession(options: BrowserRtcSessionOptions): BrowserRtcConnectedSession {
  return new BrowserRtcSession(options)
}

export function createBrowserRtcInventoryEvents(options: BrowserRtcInventoryEventsOptions): TerminalInventoryEvents {
  return new BrowserRtcInventoryEventsConnection(options)
}

class BrowserRtcSession implements BrowserRtcConnectedSession {
  private pc: RTCPeerConnectionLike | null = null
  private connectionId = ''
  private path: ConnectionPath | undefined
  private terminalId: string | undefined
  private dataChannelRole: 'offerer' | 'answerer' | null = null
  private relayInUse = false
  private apiChannel: RTCDataChannelLike | null = null
  private apiJsonChannel: RtcJsonRpcChannel | null = null
  private eventsChannel: RTCDataChannelLike | null = null
  private eventsListener: EventListener | null = null
  private eventsSubscribed = false
  private readonly eventHandlers = new Set<(event: RtcEvent) => void>()
  private readonly disconnectHandlers = new Set<() => void>()
  private terminalChannels = new Map<string, RTCDataChannelLike>()
  private fileChannels = new Map<string, RTCDataChannelLike>()
  private connected = false
  private connectedAt = 0
  private disconnecting = false
  private disconnectTimer: ReturnType<typeof setTimeout> | null = null
  private heartbeatTimer: ReturnType<typeof setTimeout> | null = null
  private heartbeatFailCount = 0
  private lastRttMs = 0
  private networkChangeCleanup: (() => void) | null = null
  private readonly incomingWaiters = new Map<string, Array<{
    resolve: (channel: RTCDataChannelLike) => void
    reject: (err: Error) => void
  }>>()
  private capabilities: ConnectionCapabilities = {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    terminalManagementAllowed: true,
    relayInUse: false,
  }

  constructor(private readonly options: BrowserRtcSessionOptions) {}

  async createOffer(input: RtcSessionNegotiationTarget): Promise<{ sessionId: string; description: RtcSessionDescription }> {
    this.assertTarget(input.machineId, input.terminalId)
    const pc = (this.options.peerConnectionFactory ?? ((configuration?: RTCConfiguration) => new RTCPeerConnection(configuration)))(
      peerConnectionConfiguration(input.iceServers),
    )
    const sessionId = this.options.sessionIdGenerator?.() ?? crypto.randomUUID()
    this.pc = pc
    this.setupConnectionStateHandlers(pc)
    this.connectionId = sessionId
    this.path = input.path
    this.terminalId = input.terminalId ?? this.options.terminalId
    this.dataChannelRole = 'offerer'
    if (input.terminalId) {
      await this.ensureTerminalChannel(input.terminalId)
    }
    await this.ensureAPIChannel()
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    await waitForICEGatheringComplete(pc, this.options.iceGatheringTimeoutMs)
    return {
      sessionId,
      description: {
        type: 'offer',
        sdp: pc.localDescription?.sdp ?? offer.sdp ?? '',
      },
    }
  }

  async acceptAnswer(answer: RtcSessionDescription): Promise<void> {
    if (!this.pc) throw new Error('browser WebRTC session has no pending offer')
    await this.pc.setRemoteDescription(answer)
    await waitChannelOpenWithTimeout(await this.ensureAPIChannel(), this.options.dataChannelOpenTimeoutMs)
    this.markReadyForTraffic()
  }

  async acceptOffer(input: RtcSessionAnswerTarget): Promise<{ sessionId: string; description: RtcSessionDescription }> {
    this.assertTarget(input.machineId, input.terminalId)
    const pc = (this.options.peerConnectionFactory ?? ((configuration?: RTCConfiguration) => new RTCPeerConnection(configuration)))(
      peerConnectionConfiguration(input.iceServers),
    )
    this.pc = pc
    this.setupConnectionStateHandlers(pc)
    this.connectionId = input.sessionId
    this.path = input.path
    this.terminalId = input.terminalId ?? this.options.terminalId
    this.dataChannelRole = 'answerer'
    this.relayInUse = input.relayInUse === true
    this.updateConnectionCapabilities(capabilitiesForAnsweredOffer(input))
    pc.addEventListener?.('datachannel', (event) => {
      const channel = (event as RTCDataChannelEvent).channel as RTCDataChannelLike | undefined
      if (channel) this.registerIncomingChannel(channel)
    })
    await pc.setRemoteDescription(input.description)
    const answer = await pc.createAnswer()
    await pc.setLocalDescription(answer)
    await waitForICEGatheringComplete(pc, this.options.iceGatheringTimeoutMs)
    return {
      sessionId: input.sessionId,
      description: {
        type: 'answer',
        sdp: pc.localDescription?.sdp ?? answer.sdp ?? '',
      },
    }
  }

  async disconnect(): Promise<void> {
    await this.closeInternal(new Error('browser WebRTC session disconnected'), false)
  }

  async openTerminal(terminalId: string): Promise<RtcBinaryChannel> {
    this.assertTarget(this.options.machineId, terminalId)
    return toProtocolBinaryChannel(await this.ensureTerminalChannel(terminalId))
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    if (!this.apiJsonChannel) {
      this.apiJsonChannel = new LocalApiChannel(await this.ensureAPIChannel())
    }
    return this.apiJsonChannel
  }

  async openFileTransfer(transferId: string): Promise<RtcBinaryChannel> {
    if (!this.capabilities.fileTransferAllowed) {
      throw new Error(this.capabilities.denialReason ?? 'file transfer is not allowed by connection policy')
    }
    const channel = this.dataChannelRole === 'answerer'
      ? await this.waitForIncomingChannel(`file:${transferId}`)
      : this.openRTCChannel(`file:${transferId}`)
    this.fileChannels.set(transferId, channel)
    await waitChannelOpen(channel)
    return toBinaryChannel(channel)
  }

  subscribeEvents(handler: (event: RtcEvent) => void): RtcSubscription {
    this.eventHandlers.add(handler)
    void this.ensureEventsChannel().then((channel) => {
      this.sendEventsSubscribe(channel)
    })
    return {
      close: () => {
        this.eventHandlers.delete(handler)
      },
    }
  }

  async getCapabilities(): Promise<ConnectionCapabilities> {
    return this.capabilities
  }

  onDisconnect(handler: () => void): RtcSubscription {
    this.disconnectHandlers.add(handler)
    return {
      close: () => this.disconnectHandlers.delete(handler),
    }
  }

  isAlive(): boolean {
    if (!this.pc) return false
    if (this.apiChannel?.readyState !== 'open') return false
    const state = this.pc.connectionState
    return state === undefined || state === 'new' || state === 'connecting' || state === 'connected'
  }

  async handleAppResume(): Promise<boolean> {
    if (!this.pc) return false
    try {
      await this.pingRuntime()
      this.markConnected()
      return true
    } catch {
      await this.failConnection(new Error('browser WebRTC session did not respond after app resume'))
      return false
    }
  }

  updateConnectionCapabilities(capabilities: ConnectionCapabilities): void {
    this.capabilities = capabilities
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    const info: ConnectionInfo = {
      path: this.path ?? this.options.path ?? 'local',
      connectionId: this.connectionId || 'local-webrtc',
      machineId: this.options.machineId,
      relayInUse: this.relayInUse || this.capabilities.relayInUse,
    }
    const terminalId = this.terminalId ?? this.options.terminalId
    if (terminalId) info.terminalId = terminalId
    return info
  }

  private openRTCChannel(label: string): RTCDataChannelLike {
    if (!this.pc) throw new Error('browser WebRTC session is not connected')
    return this.pc.createDataChannel(label, { ordered: true })
  }

  private async ensureTerminalChannel(terminalId: string): Promise<RTCDataChannelLike> {
    const existing = this.terminalChannels.get(terminalId)
    if (existing) return existing
    const channel = this.dataChannelRole === 'answerer'
      ? await this.waitForIncomingChannel(`terminal:${terminalId}`)
      : this.openRTCChannel(`terminal:${terminalId}`)
    return this.registerTerminalChannel(terminalId, channel)
  }

  private async ensureAPIChannel(): Promise<RTCDataChannelLike> {
    if (this.apiChannel && this.apiChannel.readyState !== 'closed') return this.apiChannel
    this.apiChannel = null
    this.apiJsonChannel = null
    this.apiChannel = this.dataChannelRole === 'answerer'
      ? await this.waitForIncomingChannel('api')
      : this.openRTCChannel('api')
    this.attachAPIChannelLifecycle(this.apiChannel)
    return this.apiChannel
  }

  private async ensureEventsChannel(): Promise<RTCDataChannelLike> {
    if (this.eventsChannel) return this.eventsChannel
    this.eventsChannel = this.dataChannelRole === 'answerer'
      ? await this.waitForIncomingChannel('events')
      : this.openRTCChannel('events')
    this.eventsChannel.binaryType = 'arraybuffer'
    this.attachEventsChannelListener(this.eventsChannel)
    return this.eventsChannel
  }

  private attachEventsChannelListener(channel: RTCDataChannelLike): void {
    if (this.eventsListener) return
    this.eventsListener = (event: Event) => {
      const bytes = messageBytes((event as MessageEvent).data)
      const handleBytes = (resolved: Uint8Array) => {
        const parsed = parseRtcEvent(resolved)
        for (const handler of this.eventHandlers) handler(parsed)
      }
      if (bytes instanceof Uint8Array) {
        handleBytes(bytes)
        return
      }
      void bytes.then(handleBytes)
    }
    channel.addEventListener('message', this.eventsListener)
  }

  private sendEventsSubscribe(channel: RTCDataChannelLike): void {
    if (this.eventsSubscribed) return
    const send = () => {
      if (this.eventsSubscribed) return
      try {
        channel.send(JSON.stringify({
          type: 'subscribe',
          types: [1, 2, 3, 4, 10],
        }))
        this.eventsSubscribed = true
      } catch {
        this.eventsSubscribed = false
      }
    }
    if (channel.readyState === 'open') {
      send()
      return
    }
    channel.addEventListener('open', send, { once: true })
  }

  private registerIncomingChannel(channel: RTCDataChannelLike): void {
    if (channel.label === 'api') {
      this.apiChannel = channel
      this.apiJsonChannel = null
      this.attachAPIChannelLifecycle(channel)
    } else if (channel.label === 'events') {
      channel.binaryType = 'arraybuffer'
      this.eventsChannel = channel
      this.attachEventsChannelListener(channel)
      if (this.eventHandlers.size > 0) this.sendEventsSubscribe(channel)
    } else if (channel.label.startsWith('terminal:')) {
      this.registerTerminalChannel(channel.label.slice('terminal:'.length), channel)
    } else if (channel.label.startsWith('file:')) {
      this.fileChannels.set(channel.label.slice('file:'.length), channel)
    }
    const waiters = this.incomingWaiters.get(channel.label)
    if (!waiters) return
    this.incomingWaiters.delete(channel.label)
    for (const waiter of waiters) waiter.resolve(channel)
  }

  private registerTerminalChannel(terminalId: string, channel: RTCDataChannelLike): RTCDataChannelLike {
    channel.binaryType = 'arraybuffer'
    this.terminalChannels.set(terminalId, channel)
    channel.addEventListener('close', () => {
      this.terminalChannels.delete(terminalId)
    })
    return channel
  }

  private waitForIncomingChannel(label: string): Promise<RTCDataChannelLike> {
    if (!this.pc) throw new Error('browser WebRTC session is not connected')
    if (label === 'api' && this.apiChannel) return Promise.resolve(this.apiChannel)
    if (label === 'events' && this.eventsChannel) return Promise.resolve(this.eventsChannel)
    if (label.startsWith('terminal:')) {
      const channel = this.terminalChannels.get(label.slice('terminal:'.length))
      if (channel) return Promise.resolve(channel)
    }
    if (label.startsWith('file:')) {
      const channel = this.fileChannels.get(label.slice('file:'.length))
      if (channel) return Promise.resolve(channel)
    }
    return new Promise((resolve, reject) => {
      const waiters = this.incomingWaiters.get(label) ?? []
      waiters.push({ resolve, reject })
      this.incomingWaiters.set(label, waiters)
    })
  }

  private rejectIncomingWaiters(err: Error): void {
    for (const waiters of this.incomingWaiters.values()) {
      for (const waiter of waiters) waiter.reject(err)
    }
    this.incomingWaiters.clear()
  }

  private setupConnectionStateHandlers(pc: RTCPeerConnectionLike): void {
    pc.addEventListener?.('connectionstatechange', () => {
      const state = pc.connectionState
      if (state === 'connected') {
        this.markConnected()
        return
      }
      if (state === 'disconnected') {
        this.stopHeartbeat()
        const sinceConnected = this.connectedAt > 0 ? Date.now() - this.connectedAt : 0
        this.startDisconnectGrace(sinceConnected < 15000 ? 12000 : 5000)
        void this.probeConnection(true)
        return
      }
      if (state === 'failed' || state === 'closed') {
        void this.failConnection(new Error(`browser WebRTC peer connection ${state}`))
      }
    })
  }

  private attachAPIChannelLifecycle(channel: RTCDataChannelLike): void {
    channel.addEventListener('close', () => {
      if (this.apiChannel === channel) {
        this.apiChannel = null
        this.apiJsonChannel = null
      }
      if (!this.disconnecting) {
        void this.failConnection(new Error('browser WebRTC api channel closed'))
      }
    })
    channel.addEventListener('error', () => {
      if (!this.disconnecting) {
        void this.failConnection(new Error('browser WebRTC api channel failed'))
      }
    })
  }

  private markConnected(): void {
    if (this.disconnecting) return
    this.connected = true
    this.connectedAt = Date.now()
    this.clearDisconnectGrace()
    this.startHeartbeat()
  }

  private markReadyForTraffic(): void {
    if (this.disconnecting) return
    this.connected = true
    this.connectedAt = Date.now()
    this.clearDisconnectGrace()
    const state = this.pc?.connectionState
    if (state === undefined || state === 'connected') {
      this.startHeartbeat()
    }
  }

  private startDisconnectGrace(delayMs: number): void {
    if (this.disconnectTimer) return
    this.disconnectTimer = setTimeout(() => {
      this.disconnectTimer = null
      if (!this.connected) return
      if (this.pc?.connectionState === 'connected') return
      void this.failConnection(new Error('browser WebRTC disconnected grace period expired'))
    }, delayMs)
  }

  private clearDisconnectGrace(): void {
    if (!this.disconnectTimer) return
    clearTimeout(this.disconnectTimer)
    this.disconnectTimer = null
  }

  private async probeConnection(aggressiveFirst: boolean): Promise<void> {
    try {
      await this.pingRuntime()
      this.clearDisconnectGrace()
      this.startHeartbeat()
    } catch {
      if (aggressiveFirst && this.pc?.connectionState !== 'connected') {
        await this.failConnection(new Error('browser WebRTC probe failed'))
      }
    }
  }

  private startHeartbeat(): void {
    this.stopHeartbeat()
    this.heartbeatFailCount = 0
    this.scheduleHeartbeat(this.options.heartbeatIntervalMs ?? 5000)
    this.listenNetworkChange()
  }

  private scheduleHeartbeat(delayMs: number): void {
    this.heartbeatTimer = setTimeout(() => {
      this.heartbeatTimer = null
      if (!this.connected || this.disconnecting) return
      if (!this.apiChannel || this.apiChannel.readyState !== 'open') {
        void this.failConnection(new Error('browser WebRTC api channel is not open'))
        return
      }
      const start = Date.now()
      this.pingRuntime().then(() => {
        this.lastRttMs = Date.now() - start
        this.heartbeatFailCount = 0
        this.scheduleHeartbeat(this.options.heartbeatIntervalMs ?? 5000)
      }, () => {
        this.heartbeatFailCount += 1
        if (this.heartbeatFailCount >= 4) {
          void this.failConnection(new Error('browser WebRTC heartbeat failed'))
          return
        }
        this.scheduleHeartbeat(this.heartbeatFailCount * 1000)
      })
    }, delayMs)
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearTimeout(this.heartbeatTimer)
      this.heartbeatTimer = null
    }
    this.networkChangeCleanup?.()
    this.networkChangeCleanup = null
  }

  private listenNetworkChange(): void {
    this.networkChangeCleanup?.()
    this.networkChangeCleanup = null
    const connection = globalThis.navigator?.connection
    if (!connection?.addEventListener || !connection.removeEventListener) return
    let lastType = connection.effectiveType ?? connection.type ?? ''
    const onChange = () => {
      const nextType = connection.effectiveType ?? connection.type ?? ''
      if (nextType === lastType) return
      lastType = nextType
      if (this.heartbeatTimer) {
        clearTimeout(this.heartbeatTimer)
        this.heartbeatTimer = null
      }
      this.heartbeatFailCount = 0
      this.scheduleHeartbeat(200)
    }
    connection.addEventListener('change', onChange)
    this.networkChangeCleanup = () => connection.removeEventListener?.('change', onChange)
  }

  private async pingRuntime(): Promise<void> {
    const timeoutMs = this.lastRttMs ? Math.min(Math.max(this.lastRttMs * 3, 500), 3000) : 1500
    const channel = await this.openApi()
    await withTimeout(
      channel.request('ping', {}),
      timeoutMs,
      () => new Error('browser WebRTC heartbeat timeout'),
    )
  }

  private async failConnection(err: Error): Promise<void> {
    await this.closeInternal(err, true)
  }

  private async closeInternal(err: Error, notify: boolean): Promise<void> {
    if (this.disconnecting) return
    this.disconnecting = true
    const terminalChannels = Array.from(this.terminalChannels.values())
    const fileChannels = Array.from(this.fileChannels.values())
    const apiChannel = this.apiChannel
    const eventsChannel = this.eventsChannel
    const pc = this.pc
    this.connected = false
    this.clearDisconnectGrace()
    this.stopHeartbeat()
    this.pc = null
    this.dataChannelRole = null
    this.apiChannel = null
    this.apiJsonChannel = null
    this.eventsChannel = null
    this.eventsListener = null
    this.eventsSubscribed = false
    this.terminalChannels.clear()
    this.fileChannels.clear()
    for (const channel of terminalChannels) channel.close()
    for (const channel of fileChannels) channel.close()
    apiChannel?.close()
    eventsChannel?.close()
    await pc?.close()
    this.eventHandlers.clear()
    this.rejectIncomingWaiters(err)
    this.disconnecting = false
    if (notify) {
      for (const handler of Array.from(this.disconnectHandlers)) handler()
    }
  }

  private assertTarget(machineId: string, terminalId?: string): void {
    if (machineId !== this.options.machineId) {
      throw new Error(`local WebRTC machine mismatch: ${machineId} != ${this.options.machineId}`)
    }
    if (terminalId !== undefined && this.options.terminalId !== undefined && terminalId !== this.options.terminalId) {
      throw new Error(`local WebRTC terminal mismatch: ${terminalId} != ${this.options.terminalId}`)
    }
  }

}

function peerConnectionConfiguration(iceServers: RtcSessionNegotiationTarget['iceServers']): RTCConfiguration | undefined {
  if (!iceServers || iceServers.length === 0) return undefined
  return {
    iceServers: iceServers.map((server) => ({
      urls: server.urls,
      ...(server.username !== undefined ? { username: server.username } : {}),
      ...(server.credential !== undefined ? { credential: server.credential } : {}),
    })),
  }
}

function capabilitiesForAnsweredOffer(input: RtcSessionAnswerTarget): ConnectionCapabilities {
  const relayInUse = input.relayInUse === true
  return {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: relayInUse
      ? input.relayPolicy?.allowRelayTransfer === true
      : true,
    terminalManagementAllowed: true,
    relayInUse,
    ...relayInUse && input.relayPolicy?.allowRelayTransfer === false
      ? { denialReason: 'managed relay policy blocks file transfer' }
      : {},
  }
}

class BrowserRtcInventoryEventsConnection implements TerminalInventoryEvents {
  private pc: RTCPeerConnectionLike | null = null
  private connectionId = ''
  private eventsChannel: RTCDataChannelLike | null = null
  private readonly subscribers = new Set<(event: TerminalInventoryEvent) => void>()
  private subscribeDone: Promise<void> | null = null
  private connectPromise: Promise<void> | null = null
  private disconnecting = false

  constructor(private readonly options: BrowserRtcInventoryEventsOptions) {}

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
      candidates: [],
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
    await this.subscribeInventoryEvents()
  }

  private async disconnect(): Promise<void> {
    await this.disconnectInternal()
  }

  private async disconnectInternal(): Promise<void> {
    if (this.disconnecting) return
    this.disconnecting = true
    const eventsChannel = this.eventsChannel
    this.eventsChannel = null
    eventsChannel?.close()
    await this.pc?.close()
    this.pc = null
    this.connectPromise = null
    this.subscribeDone = null
    this.disconnecting = false
  }

  private subscribeInventoryEvents(): Promise<void> {
    if (!this.subscribeDone) {
      this.subscribeDone = Promise.resolve().then(() => {
        this.eventsChannel?.send(JSON.stringify({
          type: 'subscribe',
          types: [1, 2, 3, 4, 10],
        }))
      })
    }
    return this.subscribeDone
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
    try {
      const event = JSON.parse(new TextDecoder().decode(data)) as { type?: unknown }
      if (event.type) {
        for (const handler of this.subscribers) {
          handler({ type: 'inventory_changed' })
        }
        return
      }
    } catch {
      const frame = decodeTermxFrame(data)
      if (frame.channel !== 0 || frame.type !== TERMX_FRAME_TYPES.event) return
      for (const handler of this.subscribers) {
        handler({ type: 'inventory_changed' })
      }
    }
  }

}

class LocalApiChannel implements RtcJsonRpcChannel {
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
    return {
      method,
      path: method,
      body: normalizeAPIBody(record),
    }
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

function toBinaryChannel(channel: RTCDataChannelLike): RtcBinaryChannel {
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
      const listener = (event: Event) => {
        const bytes = messageBytes((event as MessageEvent).data)
        if (bytes instanceof Uint8Array) {
          handler(bytes)
          return
        }
        void bytes.then(handler)
      }
      channel.addEventListener('message', listener)
      return { close: () => channel.removeEventListener('message', listener) }
    },
    onClose(handler: () => void) {
      const listener = () => handler()
      channel.addEventListener('close', listener)
      return { close: () => channel.removeEventListener('close', listener) }
    },
    waitOpen() {
      return waitChannelOpen(channel)
    },
  }
}

function toProtocolBinaryChannel(channel: RTCDataChannelLike): RtcBinaryChannel {
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
      const listener = (event: Event) => {
        const bytes = messageBytes((event as MessageEvent).data)
        if (bytes instanceof Uint8Array) {
          handler(bytes)
          return
        }
        void bytes.then(handler)
      }
      channel.addEventListener('message', listener)
      return { close: () => channel.removeEventListener('message', listener) }
    },
    onClose(handler: () => void) {
      const listener = () => handler()
      channel.addEventListener('close', listener)
      return { close: () => channel.removeEventListener('close', listener) }
    },
    waitOpen() {
      return waitChannelOpen(channel)
    },
  }
}

function parseRtcEvent(bytes: Uint8Array): RtcEvent {
  const value = JSON.parse(new TextDecoder().decode(bytes)) as unknown
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('rtc event must be an object')
  }
  const record = value as Record<string, unknown>
  if (typeof record.type !== 'string' || record.type.length === 0) {
    throw new Error('rtc event type is required')
  }
  return {
    type: record.type,
    ...(Object.hasOwn(record, 'payload') ? { payload: record.payload } : {}),
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
