import { registerPlugin } from '@capacitor/core'
import type { Plugin, PluginListenerHandle } from '@capacitor/core'
import type { RouteSelectionReason } from '@termx/ui'

export type NativeRelayMode = 'auto' | 'direct' | 'relay_only' | 'smart_route'

export interface NativeConnectOpts {
  endpointId: string
  targetDeviceId: string
  deviceFingerprint: string
  grantRef: string
  relayMode: NativeRelayMode
}

export interface NativeConnectionSnapshot {
  endpointId: string
  targetDeviceId: string
  phase: 'idle' | 'resolving' | 'signaling' | 'connecting' | 'authorizing' | 'connected' | 'verifying' | 'reconnecting' | 'waiting_network' | 'failed'
  path: 'hub' | null
  observedPath: 'direct' | 'single_relay' | 'relay_mesh' | null
  routeSelectionReason?: RouteSelectionReason
  statusText: string
  relayInUse: boolean
  relayMode: NativeRelayMode
  version?: number
  failReason?: string
}

export interface NativeConnectionInfo {
  type: 'p2p' | 'relay' | 'unknown'
  localAddr?: string
  remoteAddr?: string
  candidateType?: string
  remoteCandidateType?: string
  rtt?: number
  relayInUse?: boolean
  routeSelectionReason?: RouteSelectionReason
}

export interface NativeTransferSnapshotItem {
  id: string
  machineId?: string
  storeKey?: string
  name: string
  direction: 'download' | 'upload'
  totalSize: number
  transferredSize: number
  status: string
  startedAt: number
  updatedAt?: number
  bytesPerSecond?: number
  filePath?: string
  localUri?: string | undefined
  targetDir?: string | undefined
  savedPath?: string | undefined
  savedUri?: string | undefined
  error?: string
}

export interface NativeDebugLogExport {
  path: string
  name: string
  bytes: number
}

export interface NativeTransferSnapshot {
  transfers: NativeTransferSnapshotItem[]
}

export interface NativeBridgeEndpoint {
  port: number
  token: string
}

/** NativeManagedPairingImport 只含可写入 WebView endpoint registry 的非秘密 metadata。 */
export interface NativeManagedPairingImport {
  endpointId: string
  label: string
  targetDeviceId: string
  deviceFingerprint: string
  grantRef: string
  expiresAt: string
}

export interface NativeStateChangeEvent {
  endpointId: string
  targetDeviceId: string
  phase: string
  path: string | null
  observedPath: 'direct' | 'single_relay' | 'relay_mesh' | null
  routeSelectionReason?: RouteSelectionReason
  statusText: string
  relayInUse: boolean
  relayMode: NativeRelayMode
  version?: number
  failReason?: string
}

export interface NativeConnectionPlugin extends Plugin {
  connect(opts: NativeConnectOpts): Promise<void>
  importManagedPairing(opts: { payload: string; expectedEndpointId?: string }): Promise<NativeManagedPairingImport>
  deleteManagedGrant(opts: { grantRef: string }): Promise<void>
  retry(opts: { endpointId: string }): Promise<void>
  release(opts: { endpointId: string }): Promise<void>
  releaseAll(): Promise<void>
  handleForegroundResume(opts?: { backgroundDurationMs?: number }): Promise<void>
  getBridgeEndpoint(): Promise<NativeBridgeEndpoint>
  getSnapshot(opts: { endpointId: string }): Promise<NativeConnectionSnapshot>
  getConnectionInfo(opts?: { endpointId?: string }): Promise<NativeConnectionInfo>
  getDownloadResumeOffset(opts: { machineId: string; filePath: string; fileSize: number }): Promise<{ offset: number }>
  getTransferSnapshot(): Promise<NativeTransferSnapshot>
  clearTransfer(opts: { transferId: string }): Promise<void>
  resumeAllTransfers(opts?: { machineId?: string }): Promise<void>
  exportDebugLogs(): Promise<NativeDebugLogExport>
  writeDebugLog(opts: { level?: 'debug' | 'info' | 'warn' | 'error'; tag?: string; message: string }): Promise<void>

  addListener(
    event: 'stateChange',
    handler: (data: NativeStateChangeEvent) => void,
  ): Promise<PluginListenerHandle>
}

export const NativeConnection = registerPlugin<NativeConnectionPlugin>('NativeConnection')
