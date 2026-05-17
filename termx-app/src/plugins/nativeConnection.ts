import { registerPlugin } from '@capacitor/core'
import type { Plugin, PluginListenerHandle } from '@capacitor/core'

export interface NativeConnectOpts {
  machineId: string
  localAddresses: string[]
  hubUrls: string[]
  sessionToken: string
  answerProofSecret?: string
  preferredPath: 'local' | 'hub'
  forceRelay?: boolean
}

export interface NativeConnectionSnapshot {
  machineId: string
  phase: 'idle' | 'probing' | 'connecting' | 'connected' | 'verifying' | 'reconnecting' | 'waiting_network' | 'failed'
  path: 'local' | 'hub' | null
  statusText: string
  relayInUse: boolean
  forceRelay?: boolean
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

export interface NativeStateChangeEvent {
  machineId: string
  phase: string
  path: string | null
  statusText: string
  relayInUse: boolean
  forceRelay?: boolean
  version?: number
  failReason?: string
}

export interface NativeConnectionPlugin extends Plugin {
  connect(opts: NativeConnectOpts): Promise<void>
  retry(opts: { machineId: string; forceRelay?: boolean }): Promise<void>
  release(opts: { machineId: string }): Promise<void>
  releaseAll(): Promise<void>
  handleForegroundResume(opts?: { backgroundDurationMs?: number }): Promise<void>
  getBridgeEndpoint(): Promise<NativeBridgeEndpoint>
  getSnapshot(opts: { machineId: string }): Promise<NativeConnectionSnapshot>
  getConnectionInfo(opts?: { machineId?: string }): Promise<NativeConnectionInfo>
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
