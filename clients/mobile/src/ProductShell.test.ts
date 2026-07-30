import { describe, expect, it } from 'vitest'
import mobileAppSource from './AnyTTYApp.tsx?raw'
import remoteControlSource from '../../ui/src/app/RemoteControlApp.tsx?raw'
import nativeConnectionSource from '../android/app/src/main/java/com/anytty/app/NativeConnectionPlugin.kt?raw'
import nativeRuntimeCoordinatorSource from '../android/app/src/main/java/com/anytty/app/NativeConnectionRuntimeCoordinator.kt?raw'

describe('mobile product shell', () => {
  it('does not expose staging IP addresses in the official App shell', () => {
    expect(mobileAppSource).not.toContain('114.66.58.243')
    expect(mobileAppSource).not.toContain('VITE_CONTROL_URL')
    expect(remoteControlSource).not.toContain('workspace.connection.unavailableReason.cloud_unavailable')
  })

  it('waits for the replacement native generation before reconnecting after network recovery', () => {
    expect(nativeConnectionSource).toContain('runtimeCoordinator.onNetworkAvailable(network)')
    expect(nativeRuntimeCoordinatorSource).toContain('NETWORK_RESTART_DELAY_MILLIS = 300L')
    expect(nativeRuntimeCoordinatorSource).toMatch(
      /fun onNetworkAvailable[\s\S]*generationChanging\("network_available"[\s\S]*scheduleNetworkRestart/,
    )
    expect(nativeRuntimeCoordinatorSource).toMatch(
      /fun finishNetworkAvailable[\s\S]*networkEpoch != epoch[\s\S]*restartRuntime\(\)[\s\S]*generationChanged\("network_available"/,
    )
    expect(mobileAppSource).toContain("addListener('generationChanging'")
    expect(mobileAppSource).toContain("addListener('generationChangeFailed'")
    expect(mobileAppSource).toContain('connectionReady={nativeConnectionRecovery.connectionReady}')
    expect(mobileAppSource).toContain('onRetryConnectionRecovery={nativeConnectionRecovery.retryConnectionRecovery}')
  })

  it('discards the loaded transfer store before native pairing and generation reset', () => {
    expect(mobileAppSource).toMatch(
      /const resetLocalPairings[\s\S]*await nativeAppRuntime\.discardLocalState\(\)[\s\S]*await NativeConnection\.resetLocalPairings\(\)[\s\S]*replaceNativeGeneration/,
    )
  })
})
