import { registerPlugin } from '@capacitor/core'
import type { Plugin, PluginListenerHandle } from '@capacitor/core'

export interface NativeDebugLogExport {
  path: string
  name: string
  bytes: number
}

export interface NativeBridgeEndpoint {
  port: number
  token: string
}

export interface NativeConnectionPlugin extends Plugin {
  addListener(eventName: 'generationChanged', listener: (event: { reason: string }) => void): Promise<PluginListenerHandle>
  handleForegroundResume(): Promise<void>
  getBridgeEndpoint(): Promise<NativeBridgeEndpoint>
  exportDebugLogs(): Promise<NativeDebugLogExport>
  writeDebugLog(opts: { level?: 'debug' | 'info' | 'warn' | 'error'; tag?: string; message: string }): Promise<void>
}

export const NativeConnection = registerPlugin<NativeConnectionPlugin>('NativeConnection')
