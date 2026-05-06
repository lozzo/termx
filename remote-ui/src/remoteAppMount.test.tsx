import { cleanup, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('./WebControlRemoteApp', () => ({
  WebControlRemoteApp: ({ defaultControlUrl }: { defaultControlUrl?: string | undefined }) => (
    <output data-testid="control-url">{defaultControlUrl ?? ''}</output>
  ),
}))

vi.mock('./browserNetworkRuntime', () => ({
  createBrowserRemoteNetworkRuntime: () => ({
    fetch: vi.fn(),
    storage: undefined,
    queryParam: vi.fn(() => null),
  }),
}))

vi.mock('./browserRtcSession', () => ({
  createBrowserRtcSession: vi.fn(),
}))

describe('mountRemoteApp', () => {
  afterEach(() => {
    cleanup()
    document.body.innerHTML = ''
    vi.resetModules()
    vi.unstubAllEnvs()
  })

  it('passes VITE_CONTROL_URL to the Web Control app default URL', async () => {
    vi.stubEnv('VITE_CONTROL_URL', 'http://control.example.test:3000')
    const { mountRemoteApp } = await import('./remoteAppMount')

    document.body.innerHTML = '<div id="root"></div>'
    const root = mountRemoteApp()

    await waitFor(() => expect(screen.getByTestId('control-url').textContent).toBe('http://control.example.test:3000'))
    root.unmount()
  })
})
