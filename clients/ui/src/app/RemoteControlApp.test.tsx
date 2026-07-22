import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { muxviaI18n } from '../i18n'
import type { RemoteNetworkRuntime, RemoteRuntimeStorage } from '../core/transport'
import { RemoteControlApp, type CloudAccountAdapter, type ExternalPairingAdapter } from './RemoteControlApp'

describe('RemoteControlApp first-use experience', () => {
  beforeEach(async () => {
    await muxviaI18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('offers Cloud sign-in and local device setup before any device exists', async () => {
    renderApp({ cloudAccount: null })

    expect(await screen.findByTestId('muxvia-first-use')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Sign in to Muxvia Cloud' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Add local device' })).toBeTruthy()
  })

  it('keeps camera scan and manual pairing code on the same screen', async () => {
    renderApp({ cloudAccount: null, scanPairingCode: vi.fn(async () => null) })

    await userEvent.click(await screen.findByRole('button', { name: 'Add local device' }))

    expect(screen.getByRole('button', { name: 'Scan QR with camera' })).toBeTruthy()
    expect(screen.getByLabelText('Pairing code or share link')).toBeTruthy()
    expect(screen.getByPlaceholderText(/MXP1 code/i)).toBeTruthy()
  })

  it('keeps manual pairing available when camera permission is denied', async () => {
    renderApp({
      cloudAccount: null,
      scanPairingCode: vi.fn(async () => {
        throw new Error('Error getting userMedia, error = NotAllowedError: Permission denied')
      }),
    })

    await userEvent.click(await screen.findByRole('button', { name: 'Add local device' }))
    await userEvent.click(screen.getByRole('button', { name: 'Scan QR with camera' }))

    expect(await screen.findByText('This device cannot scan a QR code. Enter the same pairing code below.')).toBeTruthy()
    expect(screen.getByLabelText('Pairing code or share link')).toBeTruthy()
  })

  it('keeps technical device identity in details instead of the main row', async () => {
    renderApp({
      cloudAccount: { accountId: 'account-1', accountLabel: 'ada@example.com' },
      machines: [{
        id: 'daemon-technical-id',
        name: 'Studio Mac',
        hostname: 'studio.local',
        osInfo: 'darwin/arm64',
        online: true,
        source: 'hub',
        hubId: 'hub-us',
        hubUrls: [],
      }],
    })

    expect(await screen.findByText('Studio Mac')).toBeTruthy()
    expect(screen.queryByText('daemon-technical-id')).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'More actions for Studio Mac' }))
    await userEvent.click(screen.getByRole('button', { name: 'Device details' }))
    expect(screen.getByText('daemon-technical-id')).toBeTruthy()
    expect(screen.getByText('darwin/arm64')).toBeTruthy()
  })
})

function renderApp({
  cloudAccount,
  machines = [],
  scanPairingCode,
}: {
  cloudAccount: { accountId: string; accountLabel: string } | null
  machines?: Awaited<ReturnType<CloudAccountAdapter['listMachines']>>
  scanPairingCode?: (() => Promise<string | null>) | undefined
}) {
  const storage = new MemoryStorage()
  const networkRuntime: RemoteNetworkRuntime = {
    storage,
    queryParam: () => null,
    fetch: async () => new Response('{}', { status: 200 }),
  }
  const cloudAccountAdapter: CloudAccountAdapter = {
    current: async () => cloudAccount,
    beginActivation: async () => ({ userCode: 'MXA-TEST', expiresAtUnix: Date.now() / 1000 + 600 }),
    claimActivation: async () => ({ userCode: 'MXA-TEST', expiresAtUnix: Date.now() / 1000 + 600 }),
    awaitActivation: async () => cloudAccount ?? { accountId: 'account-1', accountLabel: 'ada@example.com' },
    cancelActivation: async () => {},
    listMachines: async () => machines,
    logout: async () => {},
  }
  const externalPairingAdapter: ExternalPairingAdapter = {
    import: async () => null,
    isAuthorized: () => true,
    forget: () => {},
  }
  render(
    <RemoteControlApp
      cloudAccountAdapter={cloudAccountAdapter}
      externalPairingAdapter={externalPairingAdapter}
      networkRuntime={networkRuntime}
      scanPairingCode={scanPairingCode}
    />,
  )
  return waitFor(() => expect(screen.getByTestId('muxvia-app-home')).toBeTruthy())
}

class MemoryStorage implements RemoteRuntimeStorage {
  private readonly values = new Map<string, string>()
  getItem(key: string): string | null { return this.values.get(key) ?? null }
  removeItem(key: string): void { this.values.delete(key) }
  setItem(key: string, value: string): void { this.values.set(key, value) }
}
