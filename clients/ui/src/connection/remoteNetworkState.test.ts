import { afterEach, describe, expect, it, vi } from 'vitest'
import { RemoteNetworkStateManager, type NativeNetworkStatus, type NativeNetworkStatusPlugin } from './remoteNetworkState'

describe('RemoteNetworkStateManager', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('uses an explicitly injected native network plugin', async () => {
    vi.useFakeTimers()
    let networkStatusHandler: ((status: NativeNetworkStatus) => void) | undefined
    const remove = vi.fn()
    const plugin: NativeNetworkStatusPlugin = {
      getStatus: vi.fn(async () => ({ connected: false, connectionType: 'none' })),
      addListener: vi.fn(async (_eventName, handler) => {
        networkStatusHandler = handler
        return { remove }
      }),
    }
    const manager = new RemoteNetworkStateManager(plugin)

    manager.init()
    await Promise.resolve()
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(101)
    expect(manager.state.phoneOnline).toBe(false)
    expect(manager.state.connectionType).toBe('none')

    networkStatusHandler?.({ connected: true, connectionType: 'wifi' })
    await vi.advanceTimersByTimeAsync(101)
    expect(manager.state.phoneOnline).toBe(true)
    expect(manager.state.connectionType).toBe('wifi')

    manager.destroy()
    expect(remove).toHaveBeenCalledOnce()
  })
})
