import { registerPlugin } from '@capacitor/core'
import type { HapticPattern } from '@termx/remote-ui'

interface NativeHapticPlugin {
  impact(options?: { pattern?: HapticPattern }): Promise<void>
}

const NativeHaptic = registerPlugin<NativeHapticPlugin>('NativeHaptic')

export default NativeHaptic
