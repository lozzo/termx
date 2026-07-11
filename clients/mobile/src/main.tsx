import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Capacitor } from '@capacitor/core'
import { setHapticImpactHandler } from '@termx/ui'
import './index.css'
import { TermxApp } from './TermxApp'
import NativeHaptic from './plugins/nativeHaptic'
import { installNativeDebugLogCapture } from './nativeDebugLog'

if (Capacitor.isNativePlatform()) {
  installNativeDebugLogCapture()
  setHapticImpactHandler((pattern) => NativeHaptic.impact({ pattern }))
}

const root = document.getElementById('root')
if (!root) throw new Error('root element not found')

createRoot(root).render(
  <StrictMode>
    <TermxApp />
  </StrictMode>,
)
