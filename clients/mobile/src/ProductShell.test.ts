import { describe, expect, it } from 'vitest'
import mobileAppSource from './MuxviaApp.tsx?raw'
import remoteControlSource from '../../ui/src/app/RemoteControlApp.tsx?raw'
import nativeConnectionSource from '../android/app/src/main/java/com/muxvia/app/NativeConnectionPlugin.kt?raw'

describe('mobile product shell', () => {
  it('does not expose staging IP addresses in the official App shell', () => {
    expect(mobileAppSource).not.toContain('114.66.58.243')
    expect(mobileAppSource).toContain("import.meta.env.VITE_CONTROL_URL || ''")
    expect(mobileAppSource).toContain('role="status"')
    expect(remoteControlSource).toMatch(/!nativeCloudLogin[\s\S]*settings\.connection/)
  })

  it('keeps Hub directory synchronization from owning the Cloud account session', () => {
    const listDevices = nativeConnectionSource.match(/fun cloudListDevices[\s\S]*?fun cloudLogout/)?.[0] ?? ''
    expect(listDevices).not.toContain('cloudAdapter.logout()')
    expect(listDevices).toContain('"temporary"')
  })
})
