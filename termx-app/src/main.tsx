import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Capacitor } from '@capacitor/core'
import { setHapticImpactHandler } from '@termx/remote-ui'
import './index.css'
import { TermxApp } from './TermxApp'
import NativeHaptic from './plugins/nativeHaptic'

if (Capacitor.isNativePlatform()) {
  setHapticImpactHandler(() => NativeHaptic.impact())
}

const root = document.getElementById('root')
if (!root) throw new Error('root element not found')

createRoot(root).render(
  <StrictMode>
    <TermxApp />
  </StrictMode>,
)
