import { cleanup, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('../app/RemoteControlApp', () => ({
  RemoteControlApp: ({ defaultControlUrl }: { defaultControlUrl?: string | undefined }) => (
    <output data-testid="control-url">{defaultControlUrl ?? ''}</output>
  ),
}))

vi.mock('../connection/browserNetworkRuntime', () => ({
  createBrowserRemoteNetworkRuntime: () => ({
    fetch: vi.fn(),
    storage: undefined,
    queryParam: vi.fn(() => null),
  }),
}))

vi.mock('../webrtc/browserRtcSession', () => ({
  createBrowserRtcSession: vi.fn(),
}))

describe('mountRemoteControlApp', () => {
  afterEach(() => {
    cleanup()
    document.body.innerHTML = ''
    vi.resetModules()
    vi.unstubAllEnvs()
  })

  it('passes VITE_CONTROL_URL to the Web Control app default URL', async () => {
    vi.stubEnv('VITE_CONTROL_URL', 'http://control.example.test:3000')
    const { mountRemoteControlApp } = await import('./mountRemoteControlApp')

    document.body.innerHTML = '<div id="root"></div>'
    const root = mountRemoteControlApp()

    await waitFor(() => expect(screen.getByTestId('control-url').textContent).toBe('http://control.example.test:3000'))
    root.unmount()
  })
})
