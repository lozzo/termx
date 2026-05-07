import type {
  ConnectionCapabilities,
  ConnectionInfo,
  ConnectionPath,
  RtcSessionCapabilityUpdater,
  RtcBinaryChannel,
  RtcEvent,
  RtcJsonRpcChannel,
  RtcSession,
  RtcSessionDescription,
  RtcSessionNegotiator,
  RtcSessionNegotiationTarget,
  RtcSubscription,
} from './transport'
import type { ConnectionLogger } from './connectionLogger'
import { logConnectionEvent } from './connectionLogger'

export interface BrowserRtcSessionOptions {
  machineId: string
  terminalId?: string | undefined
  path?: 'local' | 'public_p2p' | 'managed' | undefined
  peerConnectionFactory?: ((configuration?: RTCConfiguration) => RTCPeerConnectionLike) | undefined
  sessionIdGenerator?: (() => string) | undefined
  iceGatheringTimeoutMs?: number | undefined
  dataChannelOpenTimeoutMs?: number | undefined
  heartbeatIntervalMs?: number | undefined
  logger?: ConnectionLogger | undefined
}

export interface BrowserRtcSessionLifecycle {
  onDisconnect(handler: () => void): RtcSubscription
  isAlive(): boolean
  handleAppResume(): Promise<boolean>
}

export type BrowserRtcConnectedSession = RtcSession & RtcSessionNegotiator & RtcSessionCapabilityUpdater & BrowserRtcSessionLifecycle

export interface RTCPeerConnectionLike {
  localDescription: RTCSessionDescriptionInit | null
  remoteDescription?: RTCSessionDescriptionInit | null
  readonly iceGatheringState?: RTCIceGatheringState
  readonly connectionState?: RTCPeerConnectionState
  readonly iceConnectionState?: RTCIceConnectionState
  readonly signalingState?: RTCSignalingState
  readonly sctp?: { state?: string | undefined } | null
  createDataChannel(label: string, options?: RTCDataChannelInit): RTCDataChannelLike
  createOffer(): Promise<RTCSessionDescriptionInit>
  createAnswer(): Promise<RTCSessionDescriptionInit>
  setLocalDescription(description: RTCSessionDescriptionInit): Promise<void>
  setRemoteDescription(description: RTCSessionDescriptionInit): Promise<void>
  addIceCandidate?(candidate: RTCIceCandidateInit): Promise<void>
  getStats?(): Promise<RTCStatsReport>
  close(): void | Promise<void>
  addEventListener?(type: 'icegatheringstatechange' | 'iceconnectionstatechange' | 'signalingstatechange' | 'datachannel' | 'connectionstatechange', listener: EventListener): void
  removeEventListener?(type: 'icegatheringstatechange' | 'iceconnectionstatechange' | 'signalingstatechange' | 'datachannel' | 'connectionstatechange', listener: EventListener): void
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

class BrowserRtcSession implements BrowserRtcConnectedSession {
  private pc: RTCPeerConnectionLike | null = null
  private connectionId = ''
  private path: ConnectionPath | undefined
  private terminalId: string | undefined
  private iceServersSnapshot: unknown[] = []
  private normalizedAnswerCandidates: RTCIceCandidateInit[] = []
  private rawAnswerCandidateSummary: Record<string, unknown> = sdpCandidateSummary('')
  private apiChannel: RTCDataChannelLike | null = null
  private apiJsonChannel: RtcJsonRpcChannel | null = null
  private eventsChannel: RTCDataChannelLike | null = null
  private eventsListener: EventListener | null = null
  private eventsSubscribed = false
  private readonly eventHandlers = new Set<(event: RtcEvent) => void>()
  private readonly disconnectHandlers = new Set<() => void>()
  private readonly suppressedFailureChannels = new WeakSet<RTCDataChannelLike>()
  private terminalChannels = new Map<string, RTCDataChannelLike>()
  private fileChannels = new Map<string, RTCDataChannelLike>()
  private connected = false
  private connectedAt = 0
  private disconnecting = false
  private disconnectGuard = false
  private disconnectTimer: ReturnType<typeof setTimeout> | null = null
  private heartbeatTimer: ReturnType<typeof setTimeout> | null = null
  private heartbeatFailCount = 0
  private lastRttMs = 0
  private networkChangeCleanup: (() => void) | null = null
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
    this.log('offer_start', {
      level: 'info',
      machineId: input.machineId,
      terminalId: input.terminalId,
      path: input.path,
      details: {
        iceServerCount: input.iceServers?.length ?? 0,
        iceServers: sanitizeICEServers(input.iceServers),
      },
    })
    this.iceServersSnapshot = sanitizeICEServers(input.iceServers)
    this.normalizedAnswerCandidates = []
    this.rawAnswerCandidateSummary = sdpCandidateSummary('')
    const pc = (this.options.peerConnectionFactory ?? ((configuration?: RTCConfiguration) => new RTCPeerConnection(configuration)))(
      peerConnectionConfiguration(input.iceServers),
    )
    const sessionId = this.options.sessionIdGenerator?.() ?? crypto.randomUUID()
    this.pc = pc
    this.setupConnectionStateHandlers(pc)
    this.connectionId = sessionId
    this.path = input.path
    this.terminalId = input.terminalId ?? this.options.terminalId
    if (input.terminalId) {
      await this.ensureTerminalChannel(input.terminalId)
    }
    await this.ensureAPIChannel()
    const offer = await pc.createOffer()
    this.log('local_offer_created', {
      machineId: input.machineId,
      terminalId: input.terminalId,
      path: input.path,
      sessionId,
      details: {
        sdpBytes: offer.sdp?.length ?? 0,
      },
    })
    await pc.setLocalDescription(offer)
    this.log('local_description_set', {
      machineId: input.machineId,
      terminalId: input.terminalId,
      path: input.path,
      sessionId,
    })
    await waitForICEGatheringComplete(pc, this.options.iceGatheringTimeoutMs, (state) => {
      this.log('ice_gathering_state', {
        machineId: input.machineId,
        terminalId: input.terminalId,
        path: input.path,
        sessionId,
        details: { state },
      })
    })
    this.log('ice_gathering_complete', {
      machineId: input.machineId,
      terminalId: input.terminalId,
      path: input.path,
      sessionId,
      details: {
        sdpBytes: pc.localDescription?.sdp?.length ?? offer.sdp?.length ?? 0,
        candidates: sdpCandidateSummary(pc.localDescription?.sdp ?? offer.sdp ?? ''),
      },
    })
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
    this.log('answer_accept_start', {
      level: 'info',
      sessionId: this.connectionId,
      details: {
        answerBytes: answer.sdp.length,
      },
    })
    const normalized = normalizeRemoteDescription(answer)
    this.normalizedAnswerCandidates = normalized.candidates
    this.rawAnswerCandidateSummary = sdpCandidateSummary(answer.sdp)
    await this.pc.setRemoteDescription(normalized.description)
    this.log('remote_description_set', {
      sessionId: this.connectionId,
      details: {
        candidateCount: normalized.candidates.length,
        sdpCandidates: sdpCandidateSummary(answer.sdp),
        normalizedSDPCandidates: sdpCandidateSummary(normalized.description.sdp),
        addedCandidates: iceCandidateInitSummary(normalized.candidates),
      },
    })
    for (const candidate of normalized.candidates) {
      await this.pc.addIceCandidate?.(candidate)
    }
    if (normalized.candidates.length > 0) {
      this.log('remote_candidates_added', {
        sessionId: this.connectionId,
        details: {
          candidateCount: normalized.candidates.length,
        },
      })
    }
    const apiChannel = await this.ensureAPIChannel()
    try {
      await waitChannelOpenWithTimeout(apiChannel, this.options.dataChannelOpenTimeoutMs)
    } catch (err) {
      void this.logDataChannelOpenTimeout(apiChannel, err)
      throw err
    }
    this.log('api_channel_open', {
      sessionId: this.connectionId,
    })
    this.markReadyForTraffic()
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
    const channel = this.openRTCChannel(`file:${transferId}`)
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
      relayInUse: this.capabilities.relayInUse,
    }
    const terminalId = this.terminalId ?? this.options.terminalId
    if (terminalId) info.terminalId = terminalId
    return info
  }

  private openRTCChannel(label: string): RTCDataChannelLike {
    if (!this.pc) throw new Error('browser WebRTC session is not connected')
    const channel = this.pc.createDataChannel(label, { ordered: true })
    this.attachChannelDiagnostics(channel)
    this.log('data_channel_created', {
      sessionId: this.connectionId,
      details: {
        label,
        readyState: channel.readyState,
      },
    })
    return channel
  }

  private async ensureTerminalChannel(terminalId: string): Promise<RTCDataChannelLike> {
    const existing = this.terminalChannels.get(terminalId)
    if (existing) return existing
    const channel = this.openRTCChannel(`terminal:${terminalId}`)
    return this.registerTerminalChannel(terminalId, channel)
  }

  private clearApiChannel(): void {
    this.apiChannel = null
    this.apiJsonChannel = null
  }

  private async ensureAPIChannel(): Promise<RTCDataChannelLike> {
    if (this.apiChannel && this.apiChannel.readyState !== 'closed') return this.apiChannel
    this.clearApiChannel()
    this.apiChannel = this.openRTCChannel('api')
    this.attachAPIChannelLifecycle(this.apiChannel)
    return this.apiChannel
  }

  private async ensureEventsChannel(): Promise<RTCDataChannelLike> {
    if (this.eventsChannel) return this.eventsChannel
    this.eventsChannel = this.openRTCChannel('events')
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

  private registerTerminalChannel(terminalId: string, channel: RTCDataChannelLike): RTCDataChannelLike {
    channel.binaryType = 'arraybuffer'
    this.terminalChannels.set(terminalId, channel)
    channel.addEventListener('close', () => {
      this.terminalChannels.delete(terminalId)
    })
    return channel
  }

  private setupConnectionStateHandlers(pc: RTCPeerConnectionLike): void {
    pc.addEventListener?.('connectionstatechange', () => {
      const state = pc.connectionState
      this.log('peer_connection_state', {
        level: state === 'failed' || state === 'closed' ? 'warn' : 'debug',
        sessionId: this.connectionId,
        details: { state },
      })
      if (state === 'connected') {
        this.markConnected()
        return
      }
      if (state === 'disconnected') {
        this.disconnectGuard = true
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
    pc.addEventListener?.('iceconnectionstatechange', () => {
      const state = pc.iceConnectionState
      this.log('ice_connection_state', {
        level: state === 'failed' || state === 'closed' ? 'warn' : 'debug',
        sessionId: this.connectionId,
        details: { state },
      })
    })
    pc.addEventListener?.('signalingstatechange', () => {
      const state = pc.signalingState
      this.log('signaling_state', {
        level: state === 'closed' ? 'warn' : 'debug',
        sessionId: this.connectionId,
        details: { state },
      })
    })
  }

  private attachAPIChannelLifecycle(channel: RTCDataChannelLike): void {
    channel.addEventListener('close', () => {
      if (this.apiChannel === channel) {
        this.clearApiChannel()
      }
      if (!this.disconnecting && !this.suppressedFailureChannels.has(channel)) {
        if (this.disconnectGuard) return
        void this.failConnection(new Error('browser WebRTC api channel closed'))
      }
    })
    channel.addEventListener('error', () => {
      if (!this.disconnecting && !this.suppressedFailureChannels.has(channel)) {
        void this.failConnection(new Error('browser WebRTC api channel failed'))
      }
    })
  }

  private attachChannelDiagnostics(channel: RTCDataChannelLike): void {
    channel.addEventListener('open', () => {
      this.log('data_channel_open', {
        sessionId: this.connectionId,
        details: { label: channel.label },
      })
    })
    channel.addEventListener('close', () => {
      this.log('data_channel_close', {
        level: 'debug',
        sessionId: this.connectionId,
        details: { label: channel.label },
      })
    })
    channel.addEventListener('error', () => {
      this.log('data_channel_error', {
        level: 'warn',
        sessionId: this.connectionId,
        details: { label: channel.label },
      })
    })
  }

  private markConnected(): void {
    if (this.disconnecting) return
    this.connected = true
    this.connectedAt = Date.now()
    this.clearDisconnectGrace()
    this.log('session_connected', {
      level: 'info',
      sessionId: this.connectionId,
    })
    this.startHeartbeat()
  }

  private markReadyForTraffic(): void {
    if (this.disconnecting) return
    this.connected = true
    this.connectedAt = Date.now()
    this.clearDisconnectGrace()
    const state = this.pc?.connectionState
    this.log('session_ready_for_traffic', {
      level: 'info',
      sessionId: this.connectionId,
      details: {
        peerConnectionState: state,
      },
    })
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
    if (this.disconnecting) return
    this.log('session_failed', {
      level: 'error',
      sessionId: this.connectionId,
      message: err.message,
    })
    await this.closeInternal(err, true)
  }

  private async closeInternal(err: Error, notify: boolean): Promise<void> {
    if (this.disconnecting) return
    this.disconnecting = true
    this.log('session_closing', {
      level: notify ? 'warn' : 'debug',
      sessionId: this.connectionId,
      message: err.message,
      details: { notify },
    })
    const terminalChannels = Array.from(this.terminalChannels.values())
    const fileChannels = Array.from(this.fileChannels.values())
    const apiChannel = this.apiChannel
    const eventsChannel = this.eventsChannel
    const pc = this.pc
    for (const channel of terminalChannels) this.suppressedFailureChannels.add(channel)
    for (const channel of fileChannels) this.suppressedFailureChannels.add(channel)
    if (apiChannel) this.suppressedFailureChannels.add(apiChannel)
    if (eventsChannel) this.suppressedFailureChannels.add(eventsChannel)
    this.connected = false
    this.clearDisconnectGrace()
    this.stopHeartbeat()
    this.pc = null
    this.clearApiChannel()
    this.eventsChannel = null
    this.eventsListener = null
    this.eventsSubscribed = false
    this.normalizedAnswerCandidates = []
    this.terminalChannels.clear()
    this.fileChannels.clear()
    for (const channel of terminalChannels) channel.close()
    for (const channel of fileChannels) channel.close()
    apiChannel?.close()
    eventsChannel?.close()
    await pc?.close()
    this.eventHandlers.clear()
    this.disconnectGuard = false
    this.disconnecting = false
    if (notify) {
      for (const handler of Array.from(this.disconnectHandlers)) handler()
    }
  }

  private async logDataChannelOpenTimeout(channel: RTCDataChannelLike, err: unknown): Promise<void> {
    this.log('data_channel_open_timeout', {
      level: 'error',
      sessionId: this.connectionId,
      message: errorMessage(err),
      details: await this.connectionDebugSnapshot(channel),
    })
  }

  private async connectionDebugSnapshot(channel?: RTCDataChannelLike | null): Promise<Record<string, unknown>> {
    const pc = this.pc
    const details: Record<string, unknown> = {
      label: channel?.label,
      channelReadyState: channel?.readyState,
      peerConnectionState: pc?.connectionState,
      iceConnectionState: pc?.iceConnectionState,
      iceGatheringState: pc?.iceGatheringState,
      signalingState: pc?.signalingState,
      sctpState: pc?.sctp?.state,
      hasLocalDescription: Boolean(pc?.localDescription?.sdp),
      hasRemoteDescription: Boolean(pc?.remoteDescription?.sdp),
      iceServers: this.iceServersSnapshot,
      localDescriptionCandidates: sdpCandidateSummary(pc?.localDescription?.sdp ?? ''),
      rawAnswerCandidates: this.rawAnswerCandidateSummary,
      remoteDescriptionCandidates: sdpCandidateSummary(pc?.remoteDescription?.sdp ?? ''),
      addedRemoteCandidates: iceCandidateInitSummary(this.normalizedAnswerCandidates),
    }
    const statsSnapshot = await rtcStatsSnapshot(pc)
    if (statsSnapshot) {
      details.selectedCandidatePair = statsSnapshot.selectedCandidatePair
      details.candidatePairs = statsSnapshot.candidatePairs
      details.localCandidates = statsSnapshot.localCandidates
      details.remoteCandidates = statsSnapshot.remoteCandidates
    }
    return details
  }

  private assertTarget(machineId: string, terminalId?: string): void {
    if (machineId !== this.options.machineId) {
      throw new Error(`local WebRTC machine mismatch: ${machineId} != ${this.options.machineId}`)
    }
    if (terminalId !== undefined && this.options.terminalId !== undefined && terminalId !== this.options.terminalId) {
      throw new Error(`local WebRTC terminal mismatch: ${terminalId} != ${this.options.terminalId}`)
    }
  }

  private log(event: string, input: {
    level?: 'debug' | 'info' | 'warn' | 'error'
    machineId?: string | undefined
    terminalId?: string | undefined
    path?: ConnectionPath | undefined
    sessionId?: string | undefined
    message?: string | undefined
    details?: Record<string, unknown> | undefined
  }): void {
    logConnectionEvent(this.options.logger, {
      scope: 'browser_webrtc',
      event,
      machineId: input.machineId ?? this.options.machineId,
      terminalId: input.terminalId ?? this.terminalId ?? this.options.terminalId,
      path: input.path ?? this.path ?? this.options.path,
      ...input,
    })
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

function sanitizeICEServers(iceServers: RtcSessionNegotiationTarget['iceServers']): Array<Record<string, unknown>> {
  return (iceServers ?? []).map((server) => ({
    urls: server.urls,
    hasUsername: typeof server.username === 'string' && server.username.length > 0,
    hasCredential: typeof server.credential === 'string' && server.credential.length > 0,
  }))
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

interface RTCStatsDebugSnapshot {
  selectedCandidatePair?: Record<string, unknown> | null
  candidatePairs?: Array<Record<string, unknown>>
  localCandidates?: Array<Record<string, unknown>>
  remoteCandidates?: Array<Record<string, unknown>>
}

async function rtcStatsSnapshot(pc: RTCPeerConnectionLike | null): Promise<RTCStatsDebugSnapshot | null> {
  if (!pc?.getStats) return null
  try {
    const stats = await withTimeout(
      pc.getStats(),
      500,
      () => new Error('timed out reading browser WebRTC stats'),
    )
    const pairStats: Array<Record<string, unknown>> = []
    const candidates = new Map<string, Record<string, unknown>>()
    stats.forEach((raw) => {
      const item = raw as Record<string, unknown>
      if (typeof item.id === 'string' && (item.type === 'local-candidate' || item.type === 'remote-candidate' || item.type === 'candidate')) {
        candidates.set(item.id, item)
      }
      if (item.type !== 'candidate-pair') return
      pairStats.push(item)
    })
    const localCandidates = Array.from(candidates.values())
      .filter((item) => item.type === 'local-candidate' || item.type === 'candidate')
      .map(candidateSnapshot)
      .filter((item): item is Record<string, unknown> => Boolean(item))
    const remoteCandidates = Array.from(candidates.values())
      .filter((item) => item.type === 'remote-candidate')
      .map(candidateSnapshot)
      .filter((item): item is Record<string, unknown> => Boolean(item))
    const candidatePairs = pairStats.map((pair) => candidatePairSnapshot(pair, candidates))
    const selectedPair = pairStats.find((item) => item.selected === true) ??
      pairStats.find((item) => item.nominated === true) ??
      pairStats.find((item) => item.state === 'succeeded') ??
      null
    return {
      selectedCandidatePair: selectedPair ? candidatePairSnapshot(selectedPair, candidates) : null,
      candidatePairs,
      localCandidates,
      remoteCandidates,
    }
  } catch (err) {
    return { selectedCandidatePair: { error: errorMessage(err) } }
  }
}

function candidatePairSnapshot(pair: Record<string, unknown>, candidates: Map<string, Record<string, unknown>>): Record<string, unknown> {
  const localCandidate = candidates.get(String(pair.localCandidateId ?? ''))
  const remoteCandidate = candidates.get(String(pair.remoteCandidateId ?? ''))
  return {
    id: pair.id,
    state: pair.state,
    selected: pair.selected,
    nominated: pair.nominated,
    writable: pair.writable,
    bytesSent: pair.bytesSent,
    bytesReceived: pair.bytesReceived,
    requestsSent: pair.requestsSent,
    responsesReceived: pair.responsesReceived,
    currentRoundTripTime: pair.currentRoundTripTime,
    availableOutgoingBitrate: pair.availableOutgoingBitrate,
    local: candidateSnapshot(localCandidate),
    remote: candidateSnapshot(remoteCandidate),
  }
}

function candidateSnapshot(candidate: Record<string, unknown> | undefined): Record<string, unknown> | undefined {
  if (!candidate) return undefined
  return {
    id: candidate.id,
    candidateType: candidate.candidateType,
    protocol: candidate.protocol,
    address: candidate.address ?? candidate.ip,
    port: candidate.port,
    tcpType: candidate.tcpType,
    relayProtocol: candidate.relayProtocol,
    url: candidate.url,
  }
}

function sdpCandidateSummary(sdp: string): Record<string, unknown> {
  const candidates = parseSDPCandidates(sdp)
  return {
    count: candidates.length,
    byType: countBy(candidates.map((candidate) => candidate.candidateType ?? 'unknown')),
    byProtocol: countBy(candidates.map((candidate) => candidate.protocol ?? 'unknown')),
    candidates: candidates.slice(0, 12),
    truncated: candidates.length > 12,
  }
}

function iceCandidateInitSummary(candidates: RTCIceCandidateInit[]): Record<string, unknown> {
  const parsed = candidates
    .map((candidate) => parseCandidateLine(candidate.candidate))
    .filter((candidate): candidate is ParsedCandidate => Boolean(candidate))
  return {
    count: candidates.length,
    byType: countBy(parsed.map((candidate) => candidate.candidateType ?? 'unknown')),
    byProtocol: countBy(parsed.map((candidate) => candidate.protocol ?? 'unknown')),
    candidates: parsed.slice(0, 12),
    truncated: parsed.length > 12,
  }
}

interface ParsedCandidate {
  foundation?: string
  component?: string
  protocol?: string
  address?: string
  port?: number
  candidateType?: string
  tcpType?: string
  relayProtocol?: string
  relatedAddress?: string
}

function parseSDPCandidates(sdp: string): ParsedCandidate[] {
  if (!sdp) return []
  return sdp.split(/\r?\n/)
    .map((raw) => raw.trim())
    .filter((line) => line.startsWith('a=candidate:'))
    .map((line) => parseCandidateLine(line.slice('a='.length)))
    .filter((candidate): candidate is ParsedCandidate => Boolean(candidate))
}

function parseCandidateLine(candidateLine: string | undefined): ParsedCandidate | null {
  const line = candidateLine?.trim()
  if (!line) return null
  const parts = line.replace(/^a=/, '').split(/\s+/)
  if (!parts[0]?.startsWith('candidate:')) return null
  const typeIndex = parts.findIndex((part) => part === 'typ')
  const raddrIndex = parts.findIndex((part) => part === 'raddr')
  const tcpTypeIndex = parts.findIndex((part) => part === 'tcptype')
  const relayProtocolIndex = parts.findIndex((part) => part === 'relayProtocol')
  const out: ParsedCandidate = {
    foundation: parts[0].slice('candidate:'.length),
  }
  if (parts[1]) out.component = parts[1]
  if (parts[2]) out.protocol = parts[2].toLowerCase()
  if (parts[4]) out.address = parts[4]
  const port = Number(parts[5])
  if (port) out.port = port
  const candidateType = typeIndex >= 0 ? parts[typeIndex + 1] : undefined
  const tcpType = tcpTypeIndex >= 0 ? parts[tcpTypeIndex + 1] : undefined
  const relayProtocol = relayProtocolIndex >= 0 ? parts[relayProtocolIndex + 1] : undefined
  const relatedAddress = raddrIndex >= 0 ? parts[raddrIndex + 1] : undefined
  if (candidateType) out.candidateType = candidateType
  if (tcpType) out.tcpType = tcpType
  if (relayProtocol) out.relayProtocol = relayProtocol
  if (relatedAddress) out.relatedAddress = relatedAddress
  return out
}

function countBy(values: string[]): Record<string, number> {
  const counts: Record<string, number> = {}
  for (const value of values) counts[value] = (counts[value] ?? 0) + 1
  return counts
}

function normalizeRemoteDescription(description: RtcSessionDescription): { description: RtcSessionDescription; candidates: RTCIceCandidateInit[] } {
  const normalized = splitOutAnswerCandidates(description.sdp)
  return {
    description: {
      ...description,
      sdp: normalized.sdp,
    },
    candidates: normalized.candidates,
  }
}

function splitOutAnswerCandidates(sdp: string): { sdp: string; candidates: RTCIceCandidateInit[] } {
  const sessionLines: string[] = []
  const mediaLines: string[] = []
  const candidates: RTCIceCandidateInit[] = []
  let sessionFingerprint = ''
  let currentMid = ''
  let currentMLineIndex = -1
  let inMedia = false
  let normalized = false
  for (const rawLine of sdp.split(/\r?\n/)) {
    const line = rawLine.trim()
    if (line.startsWith('m=')) {
      currentMLineIndex += 1
      inMedia = true
    }
    if (line.startsWith('a=mid:')) currentMid = line.slice('a=mid:'.length)
    if (line === 'a=end-of-candidates') continue
    if (!inMedia && line.startsWith('a=fingerprint:')) {
      sessionFingerprint = line
      normalized = true
      continue
    }
    if (line.startsWith('a=candidate:')) {
      normalized = true
      if (/^a=candidate:\S+\s+2\s+/i.test(line)) continue
      candidates.push({
        candidate: line.slice('a='.length).replace(/\s+ufrag\s+\S+$/i, ''),
        ...(currentMid ? { sdpMid: currentMid } : {}),
        ...(currentMLineIndex >= 0 ? { sdpMLineIndex: currentMLineIndex } : {}),
      })
      continue
    }
    if (inMedia) {
      mediaLines.push(line)
    } else {
      sessionLines.push(line)
    }
  }
  const media = normalized ? normalizeApplicationMediaLines(mediaLines, sessionFingerprint) : mediaLines
  const normalizedSDP = [...sessionLines, ...media].join('\r\n')
  return { sdp: normalized ? `${normalizedSDP}\r\n` : normalizedSDP, candidates }
}

function normalizeApplicationMediaLines(lines: string[], fallbackFingerprint = ''): string[] {
  const mLine = lines.find((line) => line.startsWith('m=')) ?? ''
  if (!mLine.includes('webrtc-datachannel')) return lines.filter((line) => line !== 'a=sendrecv')
  const byPrefix = (prefix: string) => lines.find((line) => line.startsWith(prefix))
  return [
    mLine,
    byPrefix('c='),
    byPrefix('a=ice-ufrag:'),
    byPrefix('a=ice-pwd:'),
    byPrefix('a=ice-options:') ?? 'a=ice-options:trickle',
    byPrefix('a=fingerprint:') ?? fallbackFingerprint,
    byPrefix('a=setup:'),
    byPrefix('a=mid:'),
    byPrefix('a=sctp-port:'),
    'a=max-message-size:262144',
  ].filter((line): line is string => Boolean(line))
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

function waitForICEGatheringComplete(
  pc: RTCPeerConnectionLike,
  timeoutMs = 10000,
  reportState?: ((state: RTCIceGatheringState | undefined) => void) | undefined,
): Promise<void> {
  reportState?.(pc.iceGatheringState)
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
      reportState?.(pc.iceGatheringState)
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
