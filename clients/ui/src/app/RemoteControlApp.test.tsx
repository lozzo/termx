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

  it('uses service QR pairing without exposing account login when account access is disabled', async () => {
    renderApp({ cloudAccount: null, accountAccessEnabled: false, scanPairingCode: vi.fn(async () => null) })

    expect(await screen.findByTestId('muxvia-first-use')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Scan service QR' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Sign in to Muxvia Cloud' })).toBeNull()

    await userEvent.click(screen.getByRole('button', { name: 'Open settings' }))
    expect(screen.queryByRole('heading', { name: 'Account' })).toBeNull()
    expect(screen.queryByText('Current plan')).toBeNull()
    expect(screen.getByText('Device access')).toBeTruthy()
  })

  it('does not query the Cloud directory or show an error before sign-in', async () => {
    const listMachines = vi.fn(async () => {
      throw Object.assign(new Error('login required'), { code: 'login_required' })
    })
    renderApp({ cloudAccount: null, listMachines })

    expect(await screen.findByTestId('muxvia-first-use')).toBeTruthy()
    expect(listMachines).not.toHaveBeenCalled()
    expect(screen.queryByText('Sign in to Muxvia Cloud before continuing.')).toBeNull()
  })

  it('places Cloud activation before terminal appearance settings', async () => {
    renderApp({ cloudAccount: null, scanPairingCode: vi.fn(async () => null) })

    await userEvent.click(await screen.findByRole('button', { name: 'Sign in to Muxvia Cloud' }))

    const accountHeading = screen.getByRole('heading', { name: 'Account' })
    const terminalHeading = screen.getByRole('heading', { name: 'Terminal' })
    expect(accountHeading.compareDocumentPosition(terminalHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByLabelText('Login code')).toBeTruthy()
  })

  it('shows the current Cloud subscription in settings', async () => {
    renderApp({ cloudAccount: { accountId: 'account-1', accountLabel: 'ada@example.com' } })

    await userEvent.click(await screen.findByRole('button', { name: 'Open settings' }))

    expect(screen.getByText('Current plan')).toBeTruthy()
    expect(screen.getByText('Muxvia Pro')).toBeTruthy()
    expect(screen.getByText('Subscription status')).toBeTruthy()
    expect(screen.getByText('Active')).toBeTruthy()
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

  it('does not claim a new Cloud activation while an account session is already active', async () => {
    const claimActivation = vi.fn(async () => ({ userCode: 'MXA-TEST', expiresAtUnix: Date.now() / 1000 + 600 }))
    renderApp({
      cloudAccount: { accountId: 'account-1', accountLabel: 'ada@example.com' },
      claimActivation,
      scanPairingCode: vi.fn(async () => 'muxvia-cloud-activate:v1:MXA-0000-0000-0000-0000-0000-000000'),
    })

    await userEvent.click(await screen.findByRole('button', { name: 'Add local device' }))
    await userEvent.click(screen.getByRole('button', { name: 'Scan QR with camera' }))

    expect(await screen.findByText('You are already signed in as ada@example.com. Sign out before signing in to another account.')).toBeTruthy()
    expect(claimActivation).not.toHaveBeenCalled()
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
  accountAccessEnabled = true,
  cloudAccount,
  machines = [],
  listMachines,
  claimActivation,
  pairingImport,
  scanPairingCode,
}: {
  accountAccessEnabled?: boolean
  cloudAccount: { accountId: string; accountLabel: string } | null
  machines?: Awaited<ReturnType<CloudAccountAdapter['listMachines']>>
  listMachines?: CloudAccountAdapter['listMachines'] | undefined
  claimActivation?: CloudAccountAdapter['claimActivation'] | undefined
  pairingImport?: ExternalPairingAdapter['import'] | undefined
  scanPairingCode?: (() => Promise<string | null>) | undefined
}) {
  const storage = new MemoryStorage()
  const account = cloudAccount ? {
    ...cloudAccount,
    planId: 'pro',
    planName: 'Muxvia Pro',
    subscriptionStatus: 'SUBSCRIPTION_STATUS_ACTIVE',
    subscriptionRevision: 2,
  } : null
  const networkRuntime: RemoteNetworkRuntime = {
    storage,
    queryParam: () => null,
    fetch: async () => new Response('{}', { status: 200 }),
  }
  const cloudAccountAdapter: CloudAccountAdapter = {
    current: async () => account,
    beginActivation: async () => ({ userCode: 'MXA-TEST', expiresAtUnix: Date.now() / 1000 + 600 }),
    claimActivation: claimActivation ?? (async () => ({ userCode: 'MXA-TEST', expiresAtUnix: Date.now() / 1000 + 600 })),
    awaitActivation: async () => account ?? {
      accountId: 'account-1', accountLabel: 'ada@example.com', planId: 'managed-free', planName: 'Managed Free',
      subscriptionStatus: 'SUBSCRIPTION_STATUS_ACTIVE', subscriptionRevision: 1,
    },
    cancelActivation: async () => {},
    listMachines: listMachines ?? (async () => machines),
    logout: async () => {},
  }
  const externalPairingAdapter: ExternalPairingAdapter = {
    import: pairingImport ?? (async () => null),
    isAuthorized: () => true,
    forget: () => {},
  }
  render(
    <RemoteControlApp
      accountAccessEnabled={accountAccessEnabled}
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
