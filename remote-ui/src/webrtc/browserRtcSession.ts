import type {
  ConnectionCapabilities,
  ConnectionInfo,
  ConnectionPath,
  RtcSessionCapabilityUpdater,
  RtcBinaryChannel,
  RtcEvent,
  RtcJsonRpcChannel,
  RtcConnectOptions,
  RtcConnectionPhase,
  RtcConnectionStateSnapshot,
  RtcSession,
  RtcSessionDescription,
  RtcSessionConnectionStateEvents,
  RtcSessionNegotiator,
  RtcSessionNegotiationTarget,
  RtcTerminalDataChannelController,
  RtcSubscription,
} from '../core/transport'
import { createConnectionStatePublisher } from '../connection/connectionState'
import type { ConnectionLogger } from '../connection/connectionLogger'
import { logConnectionEvent } from '../connection/connectionLogger'
import {
  LocalApiChannel,
  messageBytes,
  waitChannelOpen,
  waitChannelOpenWithTimeout,
  withTimeout,
} from './rtcApiChannel'
import {
  sdpCandidateSummary,
  iceCandidateInitSummary,
  normalizeRemoteDescription,
} from './rtcSdpUtils'

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

export type BrowserRtcConnectedSession = RtcSession & RtcSessionNegotiator & RtcSessionCapabilityUpdater & BrowserRtcSessionLifecycle & RtcTerminalDataChannelController & RtcSessionConnectionStateEvents

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
  private readonly connectionState = createConnectionStatePublisher()
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
  private negotiationConnectionStateHandler: ((snapshot: RtcConnectionStateSnapshot) => void) | null = null
  private capabilities: ConnectionCapabilities = {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    terminalManagementAllowed: true,
    relayInUse: false,
  }

  constructor(private readonly options: BrowserRtcSessionOptions) {}

  async createOffer(input: RtcSessionNegotiationTarget, options: RtcConnectOptions = {}): Promise<{ sessionId: string; description: RtcSessionDescription }> {
    this.assertTarget(input.machineId, input.terminalId)
    this.negotiationConnectionStateHandler = options.onConnectionState ?? null
    this.publishConnectionState('probing', 'Creating WebRTC session...', input.path)
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
      peerConnectionConfiguration(input.iceServers, options.forceRelay),
    )
    const sessionId = this.options.sessionIdGenerator?.() ?? createRtcSessionId()
    this.pc = pc
    this.setupConnectionStateHandlers(pc)
    this.connectionId = sessionId
    this.path = input.path
    this.terminalId = input.terminalId ?? this.options.terminalId
    this.publishConnectionState('probing', 'Opening browser data channels...', input.path)
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
    this.publishConnectionState('probing', 'Gathering ICE candidates...', input.path)
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

  async acceptAnswer(answer: RtcSessionDescription, options: RtcConnectOptions = {}): Promise<void> {
    if (!this.pc) throw new Error('browser WebRTC session has no pending offer')
    if (options.onConnectionState) this.negotiationConnectionStateHandler = options.onConnectionState
    this.publishConnectionState('connecting', 'Applying WebRTC answer...')
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
      this.publishConnectionState('connecting', 'Opening data channels...')
      await waitChannelOpenWithTimeout(apiChannel, this.options.dataChannelOpenTimeoutMs)
    } catch (err) {
      void this.logDataChannelOpenTimeout(apiChannel, err)
      throw err
    }
    this.log('api_channel_open', {
      sessionId: this.connectionId,
    })
    this.markReadyForTraffic()
    this.negotiationConnectionStateHandler = null
  }

  async disconnect(): Promise<void> {
    await this.closeInternal(new Error('browser WebRTC session disconnected'), false)
  }

  async openTerminal(terminalId: string): Promise<RtcBinaryChannel> {
    this.assertTarget(this.options.machineId, terminalId)
    return toProtocolBinaryChannel(await this.ensureTerminalChannel(terminalId))
  }

  closeTerminalDataChannel(terminalId: string): void {
    const channel = this.terminalChannels.get(terminalId)
    if (!channel) return
    this.terminalChannels.delete(terminalId)
    this.suppressedFailureChannels.add(channel)
    channel.close()
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    if (!this.apiJsonChannel) {
      this.apiJsonChannel = new LocalApiChannel(await this.ensureAPIChannel())
    }
    return this.apiJsonChannel
  }

  async openFileTransfer(transferId: string): Promise<RtcBinaryChannel> {
    const channel = this.openRTCChannel(`file:${transferId}`)
    this.fileChannels.set(transferId, channel)
    channel.addEventListener('close', () => {
      if (this.fileChannels.get(transferId) === channel) this.fileChannels.delete(transferId)
    })
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

  subscribeConnectionState(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription {
    const subscription = this.connectionState.subscribe(handler)
    const phase = this.browserPhaseFromPeerState(this.pc?.connectionState)
    handler(this.connectionSnapshot(phase, this.connected ? 'Connected' : browserPhaseMessage(phase)))
    return subscription
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
    const statsInfo = await rtcConnectionInfo(this.pc)
    if (statsInfo.type) info.type = statsInfo.type
    if (statsInfo.type === 'relay') info.relayInUse = true
    if (statsInfo.localAddr) info.localAddr = statsInfo.localAddr
    if (statsInfo.remoteAddr) info.remoteAddr = statsInfo.remoteAddr
    if (statsInfo.candidateType) info.candidateType = statsInfo.candidateType
    if (statsInfo.remoteCandidateType) info.remoteCandidateType = statsInfo.remoteCandidateType
    if (statsInfo.rtt !== undefined) info.rtt = statsInfo.rtt
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
    if (existing && existing.readyState !== 'closing' && existing.readyState !== 'closed') return existing
    if (existing) this.terminalChannels.delete(terminalId)
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
      if (this.terminalChannels.get(terminalId) === channel) {
        this.terminalChannels.delete(terminalId)
      }
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
        this.publishConnectionState('reconnecting', 'WebRTC disconnected, probing connection...')
        this.disconnectGuard = true
        this.stopHeartbeat()
        const sinceConnected = this.connectedAt > 0 ? Date.now() - this.connectedAt : 0
        this.startDisconnectGrace(sinceConnected < 15000 ? 12000 : 5000)
        void this.probeConnection(true)
        return
      }
      if (state === 'failed' || state === 'closed') {
        this.publishConnectionState('failed', `WebRTC connection ${state}`)
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
      const phase = this.browserPhaseFromIceState(state)
      if (phase) this.publishConnectionState(phase, browserPhaseMessage(phase))
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
    this.publishConnectionState('connected', 'Connected')
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
    this.publishConnectionState('connected', 'Connected')
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
      this.publishConnectionState('reconnecting', 'Checking WebRTC connection...')
      await this.pingRuntime()
      this.clearDisconnectGrace()
      this.publishConnectionState('connected', 'Connected')
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
        this.publishConnectionState('connected', 'Connected')
        this.scheduleHeartbeat(this.options.heartbeatIntervalMs ?? 5000)
      }, () => {
        this.heartbeatFailCount += 1
        if (this.heartbeatFailCount >= 4) {
          void this.failConnection(new Error('browser WebRTC heartbeat failed'))
          return
        }
        this.publishConnectionState('reconnecting', `WebRTC heartbeat retry ${this.heartbeatFailCount}/4...`)
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
      this.publishConnectionState('waiting_network', 'Network changed, checking WebRTC connection...')
      this.scheduleHeartbeat(200)
    }
    connection.addEventListener('change', onChange)
    this.networkChangeCleanup = () => connection.removeEventListener?.('change', onChange)
  }

  private async pingRuntime(): Promise<void> {
    const timeoutMs = this.lastRttMs ? Math.min(Math.max(this.lastRttMs * 3, 500), 3000) : 1500
    const channel = await this.openApi()
    await withTimeout(
      channel.request('GET', { path: '/status' }),
      timeoutMs,
      () => new Error('browser WebRTC heartbeat timeout'),
    )
  }

  private async failConnection(err: Error): Promise<void> {
    if (this.disconnecting) return
    this.publishConnectionState('failed', err.message)
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
    if (!notify) this.publishConnectionState('idle', 'Ready')
    this.negotiationConnectionStateHandler = null
    if (notify) {
      for (const handler of Array.from(this.disconnectHandlers)) handler()
    }
  }

  private publishConnectionState(phase: RtcConnectionPhase, statusText: string, path: ConnectionPath | undefined = this.path ?? this.options.path): void {
    const snapshot = this.connectionSnapshot(phase, statusText, path)
    this.connectionState.publish(snapshot)
    this.negotiationConnectionStateHandler?.(snapshot)
  }

  private connectionSnapshot(phase: RtcConnectionPhase, statusText: string, path: ConnectionPath | undefined = this.path ?? this.options.path): RtcConnectionStateSnapshot {
    return {
      machineId: this.options.machineId,
      phase,
      ...(path ? { path } : {}),
      statusText,
      relayInUse: this.capabilities.relayInUse,
      ...(phase === 'failed' ? { failReason: statusText } : {}),
    }
  }

  private browserPhaseFromPeerState(state: RTCPeerConnectionState | undefined): RtcConnectionPhase {
    if (!this.pc) return 'idle'
    if (state === 'connected') return 'connected'
    if (state === 'disconnected') return 'reconnecting'
    if (state === 'failed') return 'failed'
    if (state === 'closed') return 'idle'
    return this.connected ? 'connected' : 'connecting'
  }

  private browserPhaseFromIceState(state: RTCIceConnectionState | undefined): RtcConnectionPhase | null {
    if (state === 'checking') return this.connected ? 'reconnecting' : 'connecting'
    if (state === 'connected' || state === 'completed') return 'connected'
    if (state === 'disconnected') return 'reconnecting'
    if (state === 'failed') return 'failed'
    if (state === 'closed') return this.disconnecting ? 'idle' : 'failed'
    return null
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

function peerConnectionConfiguration(iceServers: RtcSessionNegotiationTarget['iceServers'], forceRelay?: boolean | undefined): RTCConfiguration | undefined {
  if ((!iceServers || iceServers.length === 0) && !forceRelay) return undefined
  return {
    ...(forceRelay ? { iceTransportPolicy: 'relay' as RTCIceTransportPolicy } : {}),
    iceServers: (iceServers ?? []).map((server) => ({
      urls: server.urls,
      ...(server.username !== undefined ? { username: server.username } : {}),
      ...(server.credential !== undefined ? { credential: server.credential } : {}),
    })),
  }
}

function browserPhaseMessage(phase: RtcConnectionPhase): string {
  if (phase === 'idle') return 'Ready'
  if (phase === 'probing') return 'Probing WebRTC connection...'
  if (phase === 'connecting') return 'Connecting WebRTC...'
  if (phase === 'connected') return 'Connected'
  if (phase === 'verifying') return 'Verifying WebRTC...'
  if (phase === 'reconnecting') return 'Reconnecting WebRTC...'
  if (phase === 'waiting_network') return 'Waiting for network...'
  return 'WebRTC connection failed'
}

async function rtcConnectionInfo(pc: RTCPeerConnectionLike | null): Promise<Partial<ConnectionInfo> & { type?: 'p2p' | 'relay' | 'unknown' }> {
  if (!pc?.getStats) return { type: 'unknown' }
  try {
    const stats = await withTimeout(
      pc.getStats(),
      700,
      () => new Error('timed out reading browser WebRTC stats'),
    )
    const pairs: Array<Record<string, unknown>> = []
    const candidates = new Map<string, Record<string, unknown>>()
    stats.forEach((raw) => {
      const item = raw as Record<string, unknown>
      if (typeof item.id === 'string' && (item.type === 'local-candidate' || item.type === 'remote-candidate' || item.type === 'candidate')) {
        candidates.set(item.id, item)
      }
      if (item.type === 'candidate-pair') pairs.push(item)
    })
    const pair = pairs.find((item) => item.selected === true) ??
      pairs.find((item) => item.nominated === true) ??
      pairs.find((item) => item.state === 'succeeded') ??
      null
    if (!pair) return { type: 'unknown' }
    const local = candidates.get(String(pair.localCandidateId ?? ''))
    const remote = candidates.get(String(pair.remoteCandidateId ?? ''))
    const localType = stringValue(local?.candidateType)
    const remoteType = stringValue(remote?.candidateType)
    const rttSeconds = numberValue(pair.currentRoundTripTime)
    const type = localType === 'relay' || remoteType === 'relay' ? 'relay' : 'p2p'
    return {
      type,
      localAddr: candidateAddr(local),
      remoteAddr: candidateAddr(remote),
      candidateType: localType,
      remoteCandidateType: remoteType,
      ...(rttSeconds !== undefined ? { rtt: Math.round(rttSeconds * 1000) } : {}),
    }
  } catch {
    return { type: 'unknown' }
  }
}

function candidateAddr(candidate: Record<string, unknown> | undefined): string | undefined {
  if (!candidate) return undefined
  const address = stringValue(candidate.address) ?? stringValue(candidate.ip)
  const port = numberValue(candidate.port)
  if (!address || port === undefined) return undefined
  return `${address}:${port}`
}

function stringValue(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? value : undefined
}

function numberValue(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
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
  if (typeof record.type === 'number') {
    return normalizeProtocolRtcEvent(record)
  }
  if (typeof record.type !== 'string' || record.type.length === 0) {
    throw new Error('rtc event type is required')
  }
  return {
    type: record.type,
    ...(Object.hasOwn(record, 'payload') ? { payload: record.payload } : {}),
  }
}

const terminalInventoryProtocolEventNames = new Map<number, string>([
  [1, 'terminal_created'],
  [2, 'terminal_state_changed'],
  [3, 'terminal_resized'],
  [4, 'terminal_removed'],
  [10, 'terminal_metadata_changed'],
])

function normalizeProtocolRtcEvent(record: Record<string, unknown>): RtcEvent {
  const protocolType = typeof record.type === 'number' ? record.type : Number.NaN
  const eventType = terminalInventoryProtocolEventNames.get(protocolType)
  if (eventType) {
    const payload: Record<string, unknown> = {
      eventType,
      protocolType,
    }
    if (typeof record.terminal_id === 'string' && record.terminal_id.trim()) {
      payload.terminalId = record.terminal_id.trim()
    }
    if (typeof record.timestamp === 'string' && record.timestamp.trim()) {
      payload.timestamp = record.timestamp.trim()
    }
    return {
      type: 'inventory_changed',
      payload,
    }
  }
  return {
    type: `protocol_event_${protocolType}`,
    payload: record,
  }
}

function createRtcSessionId(): string {
  const cryptoImpl = globalThis.crypto
  if (cryptoImpl?.randomUUID) {
    return `rtc_${cryptoImpl.randomUUID()}`
  }
  const bytes = new Uint8Array(16)
  cryptoImpl?.getRandomValues?.(bytes)
  if (bytes.some((value) => value !== 0)) {
    return `rtc_${base64url(bytes)}`
  }
  return `rtc_${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}`
}

function base64url(bytes: Uint8Array): string {
  let binary = ''
  for (const value of bytes) {
    binary += String.fromCharCode(value)
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
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
