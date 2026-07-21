import type { CapacitorConfig } from '@capacitor/cli'

const config: CapacitorConfig = {
  appId: 'com.muxvia.app',
  appName: 'muxvia',
  webDir: 'dist',
  plugins: {
    Keyboard: {
      resize: 'none',
    },
    StatusBar: {
      backgroundColor: '#000000',
      style: 'DARK',
    },
  },
  android: {
    allowMixedContent: true,
  },
}

export default config
