import { describe, expect, it } from 'vitest'
import mobileAppSource from './MuxviaApp.tsx?raw'
import remoteControlSource from '../../ui/src/app/RemoteControlApp.tsx?raw'

describe('mobile product shell', () => {
  it('does not expose staging IP addresses in the official App shell', () => {
    expect(mobileAppSource).not.toContain('114.66.58.243')
    expect(mobileAppSource).toContain("import.meta.env.VITE_CONTROL_URL || ''")
    expect(mobileAppSource).toContain('role="status"')
    expect(remoteControlSource).toMatch(/!nativeCloudLogin[\s\S]*settings\.connection/)
  })
})
