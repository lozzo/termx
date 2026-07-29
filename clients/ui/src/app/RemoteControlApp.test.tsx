import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { anyttyI18n } from '../i18n'
import type { RemoteNetworkRuntime, RemoteRuntimeStorage } from '../core/transport'
import { RemoteControlApp, type ExternalPairingAdapter } from './RemoteControlApp'

describe('RemoteControlApp accountless product shell', () => {
  beforeEach(async () => {
    await anyttyI18n.changeLanguage('en')
  })

  afterEach(() => cleanup())

  it('starts with service pairing and no retired account action', async () => {
    renderApp()

    expect(await screen.findByTestId('anytty-first-use')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Scan service QR' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Sign in to AnyTTY Cloud' })).toBeNull()
  })

  it('does not invent a static Cloud availability state in settings', async () => {
    renderApp()

    await userEvent.click(await screen.findByRole('button', { name: 'Open settings' }))
    expect(screen.getByText('Device access')).toBeTruthy()
    expect(screen.queryByText('Cloud unavailable')).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Account' })).toBeNull()
  })

  it('keeps manual pairing available when camera permission is denied', async () => {
    renderApp({
      scanPairingCode: vi.fn(async () => {
        throw new Error('Error getting userMedia, error = NotAllowedError: Permission denied')
      }),
    })

    await userEvent.click(await screen.findByRole('button', { name: 'Scan service QR' }))
    await userEvent.click(screen.getByRole('button', { name: 'Scan QR with camera' }))

    expect(await screen.findByText('This device cannot scan a QR code. Enter the same pairing code below.')).toBeTruthy()
    expect(screen.getByLabelText('Pairing code or share link')).toBeTruthy()
  })

  it('ends pairing and allows retry when the device connection is unavailable', async () => {
    const failure = Object.assign(new Error('sanitized connection failure'), { code: 'unavailable' })
    renderApp({ pairingImport: async () => { throw failure } })

    await userEvent.click(await screen.findByRole('button', { name: 'Scan service QR' }))
    const sheet = screen.getByTestId('anytty-pair-sheet')
    await userEvent.type(within(sheet).getByLabelText('Pairing code or share link'), 'MXP2-TEST')
    await userEvent.click(within(sheet).getByRole('button', { name: 'Add device' }))

    expect(await screen.findByText('Could not connect to this device. Check both devices\' networks and try again.')).toBeTruthy()
    expect((within(sheet).getByRole('button', { name: 'Add device' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('refreshes the native device projection from the home toolbar', async () => {
    const onRefreshMachines = vi.fn(async () => undefined)
    renderApp({ onRefreshMachines })

    await userEvent.click(await screen.findByRole('button', { name: 'Refresh devices' }))

    expect(onRefreshMachines).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('Device status updated')).toBeTruthy()
  })
})

function renderApp({
  pairingImport,
  scanPairingCode,
  onRefreshMachines,
}: {
  pairingImport?: ExternalPairingAdapter['import'] | undefined
  scanPairingCode?: (() => Promise<string | null>) | undefined
  onRefreshMachines?: (() => Promise<void>) | undefined
} = {}) {
  const storage = new MemoryStorage()
  const networkRuntime: RemoteNetworkRuntime = {
    storage,
    queryParam: () => null,
    fetch: async () => new Response('{}', { status: 200 }),
  }
  const externalPairingAdapter: ExternalPairingAdapter = {
    import: pairingImport ?? (async () => null),
    isAuthorized: () => true,
    forget: () => {},
  }
  render(
    <RemoteControlApp
      externalPairingAdapter={externalPairingAdapter}
      networkRuntime={networkRuntime}
      onRefreshMachines={onRefreshMachines}
      scanPairingCode={scanPairingCode}
    />,
  )
}

class MemoryStorage implements RemoteRuntimeStorage {
  private readonly values = new Map<string, string>()
  getItem(key: string): string | null { return this.values.get(key) ?? null }
  removeItem(key: string): void { this.values.delete(key) }
  setItem(key: string, value: string): void { this.values.set(key, value) }
}
