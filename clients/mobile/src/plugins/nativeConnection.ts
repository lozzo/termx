import { registerPlugin } from '@capacitor/core'
import type { Plugin } from '@capacitor/core'

export type NativeRelayMode = 'auto' | 'direct' | 'relay_only' | 'smart_route'

export interface NativeDebugLogExport {
  path: string
  name: string
  bytes: number
}

export interface NativeBridgeEndpoint {
  port: number
  token: string
}

/** NativeCloudAccount 是 Android Keystore Session 的非秘密账号摘要。 */
export interface NativeCloudAccount {
  accountId?: string
  accountLabel?: string
  expiresAtUnix?: number
}

/** NativeCloudActivation 只包含 WebView 可展示的短码与期限；高熵 flow ID 留在 Android 原生层。 */
export interface NativeCloudActivation {
  userCode: string
  expiresAtUnix: number
}

/** NativeCloudDevice 只含同账号目录 metadata；authorization 仍由 pairing secure store 决定。 */
export interface NativeCloudDevice {
  deviceId: string
  displayName: string
  platform: string
  kind: 'client' | 'daemon'
  online: boolean
  revoked: boolean
}

export interface NativeConnectionPlugin extends Plugin {
  cloudBeginActivation(): Promise<NativeCloudActivation>
  cloudClaimActivation(opts: { payload: string }): Promise<NativeCloudActivation>
  cloudAwaitActivation(): Promise<NativeCloudAccount>
  cloudCancelActivation(): Promise<void>
  getCloudAccount(): Promise<NativeCloudAccount>
  cloudListDevices(): Promise<{ devices: NativeCloudDevice[] }>
  cloudLogout(): Promise<void>
  handleForegroundResume(): Promise<void>
  getBridgeEndpoint(): Promise<NativeBridgeEndpoint>
  exportDebugLogs(): Promise<NativeDebugLogExport>
  writeDebugLog(opts: { level?: 'debug' | 'info' | 'warn' | 'error'; tag?: string; message: string }): Promise<void>
}

export const NativeConnection = registerPlugin<NativeConnectionPlugin>('NativeConnection')
