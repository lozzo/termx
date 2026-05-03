import type {
  ConnectionCapabilities,
  ConnectionInfo,
  ConnectionPath,
  RtcBinaryChannel,
  RtcConnectOptions,
  RtcEvent,
  RtcJsonRpcChannel,
  RtcSession,
  RtcSessionAnswerer,
  RtcSessionAnswerTarget,
  RtcSessionCapabilityUpdater,
  RtcSessionDescription,
  RtcSessionNegotiator,
  RtcSessionNegotiationTarget,
  RtcSubscription,
} from './transport'

export type BridgeRtcConnectedSession = RtcSession & RtcSessionNegotiator & RtcSessionAnswerer & RtcSessionCapabilityUpdater

export interface BridgeRtcSessionAdapter {
  createOffer(target: RtcSessionNegotiationTarget, options?: RtcConnectOptions): Promise<{
    sessionId: string
    description: RtcSessionDescription
  }>
  acceptAnswer(input: {
    sessionId: string
    description: RtcSessionDescription
  }, options?: RtcConnectOptions): Promise<void>
  acceptOffer(target: RtcSessionAnswerTarget, options?: RtcConnectOptions): Promise<{
    sessionId: string
    description: RtcSessionDescription
  }>
  openBinaryChannel(label: string): Promise<RtcBinaryChannel>
  openJsonRpcChannel(label: 'api'): Promise<RtcJsonRpcChannel>
  subscribeEvents(handler: (event: RtcEvent) => void): RtcSubscription
  getConnectionInfo(): Promise<ConnectionInfo>
  getCapabilities(): Promise<ConnectionCapabilities>
  updateConnectionCapabilities(capabilities: ConnectionCapabilities): void
  disconnect(): Promise<void>
}

export interface BridgeRtcSessionOptions {
  machineId: string
  terminalId?: string | undefined
  adapter: BridgeRtcSessionAdapter
}

export function createBridgeRtcSession(options: BridgeRtcSessionOptions): BridgeRtcConnectedSession {
  return new BridgeRtcSession(options)
}

class BridgeRtcSession implements BridgeRtcConnectedSession {
  private pendingOfferSessionId: string | null = null
  private connectionInfo: ConnectionInfo | null = null
  private capabilities: ConnectionCapabilities = {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    terminalManagementAllowed: true,
    relayInUse: false,
  }

  constructor(private readonly options: BridgeRtcSessionOptions) {}

  async createOffer(target: RtcSessionNegotiationTarget, options?: RtcConnectOptions): Promise<{
    sessionId: string
    description: RtcSessionDescription
  }> {
    this.assertTarget(target.machineId, target.terminalId)
    assertConnectionPath(target.path)
    const offer = await this.options.adapter.createOffer(target, options)
    this.pendingOfferSessionId = offer.sessionId
    this.connectionInfo = {
      path: target.path,
      connectionId: offer.sessionId,
      machineId: target.machineId,
      ...(target.terminalId ? { terminalId: target.terminalId } : {}),
      relayInUse: false,
    }
    return offer
  }

  async acceptAnswer(answer: RtcSessionDescription, options?: RtcConnectOptions): Promise<void> {
    if (!this.pendingOfferSessionId) {
      throw new Error('bridge RTC session has no pending offer')
    }
    await this.options.adapter.acceptAnswer({
      sessionId: this.pendingOfferSessionId,
      description: answer,
    }, options)
    this.connectionInfo = await this.readConnectionInfo()
  }

  async acceptOffer(target: RtcSessionAnswerTarget, options?: RtcConnectOptions): Promise<{
    sessionId: string
    description: RtcSessionDescription
  }> {
    this.assertTarget(target.machineId, target.terminalId)
    assertConnectionPath(target.path)
    if (target.path !== 'managed' && target.relayInUse === true) {
      throw new Error('relay may only be reported for managed bridge RTC connections')
    }
    if (target.relayInUse === true && target.relayPolicy?.allowRelay !== true) {
      throw new Error('bridge RTC managed relay result violates relay policy')
    }
    const answer = await this.options.adapter.acceptOffer(target, options)
    if (answer.sessionId !== target.sessionId) {
      throw new Error(`bridge RTC answer session mismatch: ${answer.sessionId} != ${target.sessionId}`)
    }
    this.connectionInfo = await this.readConnectionInfo()
    this.updateConnectionCapabilities(capabilitiesForAcceptedTarget(target))
    return answer
  }

  async openTerminal(terminalId: string): Promise<RtcBinaryChannel> {
    this.assertTarget(this.options.machineId, terminalId)
    return this.options.adapter.openBinaryChannel(`terminal:${terminalId}`)
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    return this.options.adapter.openJsonRpcChannel('api')
  }

  async openFileTransfer(transferId: string): Promise<RtcBinaryChannel> {
    const capabilities = await this.getCapabilities()
    if (!capabilities.fileTransferAllowed) {
      throw new Error(capabilities.denialReason ?? 'file transfer is not allowed by bridge RTC connection policy')
    }
    return this.options.adapter.openBinaryChannel(`file:${transferId}`)
  }

  subscribeEvents(handler: (event: RtcEvent) => void): RtcSubscription {
    return this.options.adapter.subscribeEvents(handler)
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    return this.readConnectionInfo()
  }

  async getCapabilities(): Promise<ConnectionCapabilities> {
    const adapterCapabilities = await this.options.adapter.getCapabilities()
    this.capabilities = normalizeCapabilities(adapterCapabilities)
    return this.capabilities
  }

  updateConnectionCapabilities(capabilities: ConnectionCapabilities): void {
    this.capabilities = normalizeCapabilities(capabilities)
    this.options.adapter.updateConnectionCapabilities(this.capabilities)
  }

  async disconnect(): Promise<void> {
    this.pendingOfferSessionId = null
    this.connectionInfo = null
    await this.options.adapter.disconnect()
  }

  private async readConnectionInfo(): Promise<ConnectionInfo> {
    const info = this.connectionInfo ?? await this.options.adapter.getConnectionInfo()
    assertConnectionPath(info.path)
    if (info.machineId !== this.options.machineId) {
      throw new Error(`bridge RTC machine mismatch: ${info.machineId} != ${this.options.machineId}`)
    }
    if (this.options.terminalId !== undefined && info.terminalId !== undefined && info.terminalId !== this.options.terminalId) {
      throw new Error(`bridge RTC terminal mismatch: ${info.terminalId} != ${this.options.terminalId}`)
    }
    if (info.path !== 'managed' && info.relayInUse === true) {
      throw new Error(`${info.path} bridge RTC connection must not report relay usage`)
    }
    this.connectionInfo = info
    return info
  }

  private assertTarget(machineId: string, terminalId?: string): void {
    if (machineId !== this.options.machineId) {
      throw new Error(`bridge RTC machine mismatch: ${machineId} != ${this.options.machineId}`)
    }
    if (terminalId !== undefined && this.options.terminalId !== undefined && terminalId !== this.options.terminalId) {
      throw new Error(`bridge RTC terminal mismatch: ${terminalId} != ${this.options.terminalId}`)
    }
  }
}

function capabilitiesForAcceptedTarget(target: RtcSessionAnswerTarget): ConnectionCapabilities {
  const relayInUse = target.relayInUse === true
  return normalizeCapabilities({
    terminalAllowed: target.terminalId !== undefined,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: relayInUse
      ? target.relayPolicy?.allowRelayTransfer === true
      : true,
    terminalManagementAllowed: true,
    relayInUse,
    ...relayInUse && target.relayPolicy?.allowRelayTransfer === false
      ? { denialReason: 'managed relay policy blocks file transfer' }
      : {},
  })
}

function normalizeCapabilities(capabilities: ConnectionCapabilities): ConnectionCapabilities {
  return {
    ...capabilities,
    relayInUse: capabilities.relayInUse === true,
  }
}

function assertConnectionPath(path: ConnectionPath): void {
  if (path !== 'local' && path !== 'public_p2p' && path !== 'managed') {
    throw new Error(`bridge RTC connection path must be local, public_p2p, or managed, got ${String(path)}`)
  }
}
