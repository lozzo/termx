import { describe, expect, it } from 'vitest'
import mobileAppSource from './MuxviaApp.tsx?raw'
import remoteControlSource from '../../ui/src/app/RemoteControlApp.tsx?raw'
import nativeConnectionSource from '../android/app/src/main/java/com/muxvia/app/NativeConnectionPlugin.kt?raw'

describe('mobile product shell', () => {
  it('keeps the terminal client accountless and scoped to saved services', () => {
    expect(remoteControlSource).not.toContain('CloudAccountAdapter')
    expect(mobileAppSource).not.toContain('cloudAccountAdapter=')
    expect(mobileAppSource).not.toContain('NativeConnection.getCloudAccount')
    expect(mobileAppSource).not.toContain('NativeConnection.cloudListDevices')
    expect(mobileAppSource).not.toContain('NativeConnection.cloudLogout')
  })

  it('does not expose staging IP addresses in the official App shell', () => {
    expect(mobileAppSource).not.toContain('114.66.58.243')
    expect(mobileAppSource).not.toContain('VITE_CONTROL_URL')
    expect(remoteControlSource).not.toContain('workspace.connection.unavailableReason.cloud_unavailable')
  })

  it('removes the retired Cloud account bridge from the native plugin', () => {
    expect(nativeConnectionSource).not.toMatch(/fun cloud(?:Begin|Claim|Await|Cancel|List|Logout)/)
    expect(nativeConnectionSource).not.toContain('getCloudAccount')
    expect(nativeConnectionSource).not.toContain('ManagedCloudAssembly')
  })
})
