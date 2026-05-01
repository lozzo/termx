import type { Machine, Terminal } from './model'

export type ConnectionMode = 'local' | 'anonymous_p2p' | 'managed_p2p' | 'paid_relay'

export interface ConnectTarget {
  machineId: string
  terminalId?: string
}

export interface ConnectOptions {
  mode?: ConnectionMode
  signal?: AbortSignal
}

export interface ConnectResult {
  mode: ConnectionMode
  connectionId: string
}

export interface TransportStatus {
  phase: 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'failed' | 'released'
  mode?: ConnectionMode
}

export interface ConnectionInfo {
  mode: ConnectionMode
  connectionId: string
  machineId: string
  terminalId?: string
  relayInUse: boolean
}

export interface BinaryChannel {
  readonly label: string
  readonly readyState: 'connecting' | 'open' | 'closing' | 'closed'
  send(data: Uint8Array): void
  close(): void
}

export interface JsonRpcChannel {
  request<TResponse>(method: string, params?: unknown): Promise<TResponse>
  close(): void
}

export interface LocalStatus {
  machine: Machine
  localWeb: {
    httpUrl: string
    rtcOfferUrl: string
  }
}

export interface LocalPairInput {
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
  terminalId: string
  sdp: string
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
}

export interface RemoteTransport {
  connect(target: ConnectTarget, options?: ConnectOptions): Promise<ConnectResult>
  disconnect(): Promise<void>
  status(): TransportStatus
  openTerminal(terminalId: string): Promise<BinaryChannel>
  openApi(): Promise<JsonRpcChannel>
  openFileTransfer(transferId: string): Promise<BinaryChannel>
  getConnectionInfo(): Promise<ConnectionInfo>
}

export interface PeerTransport {
  connect(input: ConnectTarget & { mode: ConnectionMode }): Promise<void>
  disconnect(): Promise<void>
  openTerminal(terminalId: string): Promise<BinaryChannel>
  openApi(): Promise<JsonRpcChannel>
  openFileTransfer(transferId: string): Promise<BinaryChannel>
  getConnectionInfo(): Promise<ConnectionInfo>
}

export interface LocalAgentApi {
  getStatus(): Promise<LocalStatus>
  listTerminals(): Promise<Terminal[]>
  pair(input: LocalPairInput): Promise<LocalPairResult>
  createRTCAnswer(input: LocalRTCOffer): Promise<LocalRTCAnswer>
}
