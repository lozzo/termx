import { describe, expect, it } from 'vitest'
import mobileAppSource from './AnyTTYApp.tsx?raw'
import remoteControlSource from '../../ui/src/app/RemoteControlApp.tsx?raw'
import nativeConnectionSource from '../android/app/src/main/java/com/anytty/app/NativeConnectionPlugin.kt?raw'

describe('mobile product shell', () => {
  it('does not expose staging IP addresses in the official App shell', () => {
    expect(mobileAppSource).not.toContain('114.66.58.243')
    expect(mobileAppSource).not.toContain('VITE_CONTROL_URL')
    expect(remoteControlSource).not.toContain('workspace.connection.unavailableReason.cloud_unavailable')
  })

  it('waits for the replacement native generation before reconnecting after network recovery', () => {
    expect(nativeConnectionSource).toMatch(
      /onAvailable[\s\S]*notifyListeners\("generationChanging"[\s\S]*delay\(300\)[\s\S]*notifyListeners\("generationChanged"/,
    )
    expect(mobileAppSource).toContain("addListener('generationChanging'")
    expect(mobileAppSource).toContain("addListener('generationChangeFailed'")
    expect(mobileAppSource).toContain('connectionReady={nativeConnectionRecovery.connectionReady}')
    expect(mobileAppSource).toContain('onRetryConnectionRecovery={nativeConnectionRecovery.retryConnectionRecovery}')
  })
})
