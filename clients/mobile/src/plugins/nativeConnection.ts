import { registerPlugin } from '@capacitor/core'
import type { Plugin, PluginListenerHandle } from '@capacitor/core'

export interface NativeBridgeEndpoint {
  port: number
  token: string
}

export interface NativeConnectionPlugin extends Plugin {
  addListener(eventName: 'generationChanging' | 'generationChanged' | 'generationChangeFailed', listener: (event: { reason: string; epoch: number }) => void): Promise<PluginListenerHandle>
  handleForegroundResume(): Promise<void>
  resetLocalPairings(): Promise<void>
  getBridgeEndpoint(): Promise<NativeBridgeEndpoint>
}

export const NativeConnection = registerPlugin<NativeConnectionPlugin>('NativeConnection')
