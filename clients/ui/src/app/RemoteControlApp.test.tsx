import { act, cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { anyttyI18n } from '../i18n'
import type { RemoteNetworkRuntime, RemoteRuntimeStorage } from '../core/transport'
import { createMachineStore } from '../state/machineStore'
import { dispatchNativeBack } from '../platform/nativeBack'
import { RemoteControlApp, type ExternalPairingAdapter } from './RemoteControlApp'

describe('RemoteControlApp accountless product shell', () => {
  beforeEach(async () => {
    await anyttyI18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
    document.querySelector('[data-testid="english-back-label-decoy"]')?.remove()
  })

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

  it('keeps pairing QR-only when camera permission is denied', async () => {
    renderApp({
      scanPairingCode: vi.fn(async () => {
        throw new Error('Error getting userMedia, error = NotAllowedError: Permission denied')
      }),
    })

    await userEvent.click(await screen.findByRole('button', { name: 'Scan service QR' }))
    await userEvent.click(screen.getByRole('button', { name: 'Scan QR with camera' }))

    expect(await screen.findByText(/QR scanning is unavailable on this device/)).toBeTruthy()
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('imports a scanned service QR and saves the resulting endpoint locally', async () => {
    const pairingImport = vi.fn(async () => ({
      machine: { id: 'device-1', name: 'Office Mac', hostname: 'office.local', accessClass: 'local' as const },
    }))
    const { storage } = renderApp({
      pairingImport,
      scanPairingCode: vi.fn(async () => 'MXP2-SCANNED'),
    })

    await userEvent.click(await screen.findByRole('button', { name: 'Scan service QR' }))
    await userEvent.click(screen.getByRole('button', { name: 'Scan QR with camera' }))

    expect(pairingImport).toHaveBeenCalledWith('MXP2-SCANNED', undefined)
    expect(createMachineStore({ storage }).listMachines()).toEqual([
      expect.objectContaining({ machineId: 'device-1', name: 'Office Mac', hostname: 'office.local' }),
    ])
  })

  it('ends a failed scanned pairing and allows camera retry', async () => {
    const failure = Object.assign(new Error('sanitized connection failure'), { code: 'unavailable' })
    const scanPairingCode = vi.fn(async () => 'MXP2-TEST')
    renderApp({ pairingImport: async () => { throw failure }, scanPairingCode })

    await userEvent.click(await screen.findByRole('button', { name: 'Scan service QR' }))
    const sheet = screen.getByTestId('anytty-pair-sheet')
    await userEvent.click(within(sheet).getByRole('button', { name: 'Scan QR with camera' }))

    expect(await screen.findByText('Could not connect to this device. Check both devices\' networks and try again.')).toBeTruthy()
    expect((within(sheet).getByRole('button', { name: 'Scan QR with camera' }) as HTMLButtonElement).disabled).toBe(false)

    await userEvent.click(within(sheet).getByRole('button', { name: 'Scan QR with camera' }))
    expect(scanPairingCode).toHaveBeenCalledTimes(2)
  })

  it('refreshes the native device projection from the home toolbar', async () => {
    const onRefreshMachines = vi.fn(async () => undefined)
    renderApp({ onRefreshMachines })

    await userEvent.click(await screen.findByRole('button', { name: 'Refresh devices' }))

    expect(onRefreshMachines).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('Device status updated')).toBeTruthy()
  })

  it('closes the Chinese pairing surface without querying English aria labels', async () => {
    await anyttyI18n.changeLanguage('zh-CN')
    const englishLabelDecoy = document.createElement('button')
    const decoyClick = vi.fn()
    englishLabelDecoy.dataset.testid = 'english-back-label-decoy'
    englishLabelDecoy.setAttribute('aria-label', 'Close pairing')
    englishLabelDecoy.onclick = decoyClick
    document.body.append(englishLabelDecoy)
    renderApp()

    await userEvent.click(await screen.findByRole('button', { name: anyttyI18n.t('machines.scanService') }))
    expect(screen.getByTestId('anytty-pair-sheet')).toBeTruthy()

    act(() => { expect(dispatchNativeBack()).toBe(true) })
    expect(screen.queryByTestId('anytty-pair-sheet')).toBeNull()
    expect(decoyClick).not.toHaveBeenCalled()
    expect(dispatchNativeBack()).toBe(false)
    expect(decoyClick).not.toHaveBeenCalled()
    englishLabelDecoy.remove()
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
  return { storage }
}

class MemoryStorage implements RemoteRuntimeStorage {
  private readonly values = new Map<string, string>()
  getItem(key: string): string | null { return this.values.get(key) ?? null }
  removeItem(key: string): void { this.values.delete(key) }
  setItem(key: string, value: string): void { this.values.set(key, value) }
}
