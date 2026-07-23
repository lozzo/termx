import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
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

  it('places Cloud activation before terminal appearance settings', async () => {
    renderApp({ cloudAccount: null, scanPairingCode: vi.fn(async () => null) })

    await userEvent.click(await screen.findByRole('button', { name: 'Sign in to Muxvia Cloud' }))

    const accountHeading = screen.getByRole('heading', { name: 'Account' })
    const terminalHeading = screen.getByRole('heading', { name: 'Terminal' })
    expect(accountHeading.compareDocumentPosition(terminalHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByLabelText('Login code')).toBeTruthy()
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

  it('ends pairing and shows an actionable message when the connection is unavailable', async () => {
    const failure = Object.assign(new Error('sanitized connection failure'), { code: 'unavailable' })
    renderApp({ cloudAccount: null, pairingImport: async () => { throw failure } })

    await userEvent.click(await screen.findByRole('button', { name: 'Add local device' }))
    const sheet = screen.getByTestId('muxvia-pair-sheet')
    await userEvent.type(within(sheet).getByLabelText('Pairing code or share link'), 'MXP1-TEST')
    await userEvent.click(within(sheet).getByRole('button', { name: 'Add device' }))

    expect(await screen.findByText('Could not connect to this device. Check both devices\' networks and try again.')).toBeTruthy()
    expect((within(sheet).getByRole('button', { name: 'Add device' }) as HTMLButtonElement).disabled).toBe(false)
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

  it('does not use a daemon device id as its visible name', async () => {
    renderApp({
      cloudAccount: { accountId: 'account-1', accountLabel: 'ada@example.com' },
      machines: [{
        id: 'device-technical-id',
        name: 'device-technical-id',
        online: true,
        source: 'hub',
        hubUrls: [],
      }],
    })

    expect((await screen.findAllByText('Muxvia daemon')).length).toBeGreaterThan(0)
    expect(screen.queryByText('device-technical-id')).toBeNull()
  })

  it('keeps a user-provided name that starts with device', async () => {
    renderApp({
      cloudAccount: { accountId: 'account-1', accountLabel: 'ada@example.com' },
      machines: [{
        id: 'daemon-1',
        name: 'device-lab',
        online: true,
        source: 'hub',
        hubUrls: [],
      }],
    })

    expect(await screen.findByText('device-lab')).toBeTruthy()
  })
})

function renderApp({
  cloudAccount,
  machines = [],
  pairingImport,
  scanPairingCode,
}: {
  cloudAccount: { accountId: string; accountLabel: string } | null
  machines?: Awaited<ReturnType<CloudAccountAdapter['listMachines']>>
  pairingImport?: ExternalPairingAdapter['import'] | undefined
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
    import: pairingImport ?? (async () => null),
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
