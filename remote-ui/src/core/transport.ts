import type { Machine, Terminal } from './model'

export type ConnectionPath = 'local' | 'hub'

export const CONNECTION_PATHS = ['local', 'hub'] as const satisfies readonly ConnectionPath[]

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

export interface RtcConnectOptions {
  signal?: AbortSignal
  forceRelay?: boolean | undefined
  onStatus?: ((status: string) => void) | undefined
  onConnectionState?: ((snapshot: RtcConnectionStateSnapshot) => void) | undefined
}

export interface RtcSubscription {
  close(): void
}

export type RtcConnectionPhase = 'idle' | 'probing' | 'connecting' | 'connected' | 'verifying' | 'reconnecting' | 'waiting_network' | 'failed'

export interface RtcConnectionStateSnapshot {
  machineId: string
  phase: RtcConnectionPhase
  path?: ConnectionPath | undefined
  statusText: string
  relayInUse: boolean
  failReason?: string | undefined
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
  type?: 'p2p' | 'relay' | 'unknown' | undefined
  localAddr?: string | undefined
  remoteAddr?: string | undefined
  candidateType?: string | undefined
  remoteCandidateType?: string | undefined
  rtt?: number | undefined
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

export interface RtcSessionLiveness {
  isAlive(): boolean
}

export interface RtcSessionConnectionStateEvents {
  subscribeConnectionState(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription
}

export interface MachineConnectionStateEvents {
  subscribe(machineId: string, handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription
}

export interface RtcTerminalDataChannelController {
  closeTerminalDataChannel(terminalId: string): void
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
  requestedCapabilities: string[]
}

export interface LocalPairResult {
  machineId: string
  sessionToken: string
  expiresAt: string
}

export interface LocalPairingApi {
  pair(input: LocalPairInput): Promise<LocalPairResult>
}

export interface TerminalInventorySubscription {
  close(): void
}

export interface TerminalInventoryEvent {
  type: 'inventory_changed'
  payload?: unknown
}

export interface TerminalInventoryEvents {
  subscribe(machineId: string, handler: (event: TerminalInventoryEvent) => void): TerminalInventorySubscription
}

export interface LocalAgentApi {
  getStatus(): Promise<LocalStatus>
}

export interface LocalCreateTerminalInput {
  name?: string | undefined
  command?: string[] | undefined
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
