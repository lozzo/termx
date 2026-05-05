import type { Machine, Terminal } from './model'

export type ConnectionPath = 'local' | 'public_p2p' | 'managed'

export const CONNECTION_PATHS = ['local', 'public_p2p', 'managed'] as const satisfies readonly ConnectionPath[]

export type RemoteRuntimeFetch = (input: string, init?: RequestInit) => Promise<Response>

export interface RemoteRuntimeStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

export interface RemoteNetworkRuntime {
  fetch: RemoteRuntimeFetch
  storage?: RemoteRuntimeStorage | undefined
  queryParam(name: string): string | null
}

export interface RtcConnectionTarget {
  machineId: string
  terminalId?: string | undefined
}

export interface RtcSessionDescription {
  type: 'offer' | 'answer'
  sdp: string
}

export interface RtcOfferSigningInput {
  sessionId: string
  ticketId?: string | undefined
  machineId: string
  terminalId: string
  sdp: string
  candidates?: string[] | undefined
}

export interface RtcSessionNegotiationTarget extends RtcConnectionTarget {
  path: ConnectionPath
  iceServers?: Array<{
    urls: string[]
    username?: string | undefined
    credential?: string | undefined
  }> | undefined
}

export interface RtcSessionNegotiator {
  createOffer(target: RtcSessionNegotiationTarget, options?: RtcConnectOptions): Promise<{
    sessionId: string
    description: RtcSessionDescription
  }>
  acceptAnswer(answer: RtcSessionDescription, options?: RtcConnectOptions): Promise<void>
}

export interface RtcSessionAnswerTarget extends RtcSessionNegotiationTarget {
  sessionId: string
  description: RtcSessionDescription
  relayPolicy?: {
    allowRelay: boolean
    allowRelayTransfer: boolean
  } | undefined
  relayInUse?: boolean | undefined
}

export interface RtcSessionAnswerer {
  acceptOffer(target: RtcSessionAnswerTarget, options?: RtcConnectOptions): Promise<{
    sessionId: string
    description: RtcSessionDescription
  }>
}

export interface RtcConnectOptions {
  signal?: AbortSignal
}

export interface RtcSubscription {
  close(): void
}

export interface RtcEvent {
  type: string
  payload?: unknown
}

export interface ConnectionInfo {
  path: ConnectionPath
  connectionId: string
  machineId: string
  terminalId?: string
  relayInUse: boolean
}

export interface ConnectionCapabilities {
  terminalAllowed: boolean
  apiAllowed: boolean
  eventsAllowed: boolean
  fileTransferAllowed: boolean
  terminalManagementAllowed: boolean
  relayInUse: boolean
  denialReason?: string
}

export interface RtcSessionCapabilityUpdater {
  updateConnectionCapabilities(capabilities: ConnectionCapabilities): void
}

export interface RtcBinaryChannel {
  readonly label: string
  readonly readyState: 'connecting' | 'open' | 'closing' | 'closed'
  send(data: Uint8Array): void
  close(): void
  onMessage(handler: (data: Uint8Array) => void): RtcSubscription
  onClose(handler: () => void): RtcSubscription
  waitOpen(): Promise<void>
}

export interface RtcJsonRpcChannel {
  request<TResponse>(method: string, params?: unknown): Promise<TResponse>
  close(): void
}

export interface RtcSession {
  openTerminal(terminalId: string): Promise<RtcBinaryChannel>
  openApi(): Promise<RtcJsonRpcChannel>
  openFileTransfer(transferId: string): Promise<RtcBinaryChannel>
  subscribeEvents(handler: (event: RtcEvent) => void): RtcSubscription
  getConnectionInfo(): Promise<ConnectionInfo>
  getCapabilities(): Promise<ConnectionCapabilities>
  disconnect(): Promise<void>
}

export interface RtcConnector<TInput extends RtcConnectionTarget = RtcConnectionTarget> {
  connect(input: TInput, options?: RtcConnectOptions): Promise<RtcSession>
}

export interface LocalStatus {
  machine: Machine
  localWeb: {
    httpUrl: string
    rtcOfferUrl: string
  }
}

export interface LocalPairInput {
  machineId?: string | undefined
  pairSessionId: string
  pairSecret: string
  appDeviceId: string
  appName: string
  appPublicKey: string
  requestedCapabilities: string[]
}

export interface LocalPairResult {
  machineId: string
  appCertificate: string
  expiresAt: string
}

export interface LocalRTCOffer {
  sessionId: string
  machineId: string
  terminalId?: string | undefined
  sdp: string
  iceCandidates?: string[] | undefined
  appCertificate: string
  appSignature: string
  nonce: string
  timestamp: string
}

export interface LocalRTCAnswer {
  sessionId: string
  answer: {
    type: 'answer'
    sdp: string
  }
  iceTCP?: {
    enabled: boolean
    endpoint?: string
  }
  dataChannels?: string[]
  capabilities?: ConnectionCapabilities
}

export interface TerminalInventorySubscription {
  close(): void
}

export interface TerminalInventoryEvent {
  type: 'inventory_changed'
}

export interface TerminalInventoryEvents {
  subscribe(machineId: string, handler: (event: TerminalInventoryEvent) => void): TerminalInventorySubscription
}

export interface LocalAgentApi {
  getStatus(): Promise<LocalStatus>
  listTerminals(): Promise<Terminal[]>
  pair(input: LocalPairInput): Promise<LocalPairResult>
  createRTCAnswer(input: LocalRTCOffer, options?: RtcConnectOptions): Promise<LocalRTCAnswer>
  createInventoryRTCAnswer(input: LocalInventoryRTCOffer): Promise<LocalInventoryRTCAnswer>
  createTerminal(input: LocalCreateTerminalInput): Promise<Terminal>
  updateTerminal(input: LocalUpdateTerminalInput): Promise<Terminal>
  deleteTerminal(terminalId: string): Promise<void>
}

export interface LocalInventoryRTCOffer {
  sessionId: string
  machineId: string
  sdp: string
  appCertificate: string
  appSignature: string
  nonce: string
  timestamp: string
}

export interface LocalInventoryRTCAnswer {
  sessionId: string
  answer: {
    type: 'answer'
    sdp: string
  }
}

export interface LocalCreateTerminalInput {
  name?: string | undefined
  command: string[]
  cols?: number | undefined
  rows?: number | undefined
  cwd?: string | undefined
  environment?: string | undefined
  sizeLockMode?: 'off' | 'warn' | 'lock' | undefined
}

export interface LocalUpdateTerminalInput {
  terminalId: string
  name?: string | undefined
  cwd?: string | undefined
  environment?: string | undefined
  sizeLockMode?: 'off' | 'warn' | 'lock' | undefined
}
