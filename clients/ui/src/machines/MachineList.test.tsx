import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MachineList, type MachineListProps } from './MachineList'
import type { AppMachineRecord } from '../state/appMachine'

describe('MachineList', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders a dense local machine list with a QR scan action', async () => {
    const onScanMachine = vi.fn()
    const onSelectMachine = vi.fn()

    render(
      <MachineList
        machines={[
          machine({
            machineId: 'machine-local',
            name: 'MacBook Pro',
            hostname: 'mbp.local',
            state: 'online',
            terminalCount: 3,
            lastSeenAt: '2026-05-03T07:22:00Z',
            lastConnectionPath: 'local',
            source: 'local',
          }),
          machine({
            machineId: 'machine-cloud',
            name: 'Build Runner',
            hostname: 'runner.internal',
            state: 'stale',
            terminalCount: 1,
            lastSeenAt: '2026-05-02T11:00:00Z',
            lastConnectionPath: 'hub',
            relayInUse: true,
            source: 'hub',
          }),
        ]}
        onScanMachine={onScanMachine}
        onSelectMachine={onSelectMachine}
      />,
    )

    expect(screen.getByTestId('anytty-machine-list')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Devices' })).toBeTruthy()
    expect(screen.getByRole('button', { name: /scan pairing qr/i })).toBeTruthy()
    expect(screen.getByText('MacBook Pro')).toBeTruthy()
    expect(screen.getByText('mbp.local')).toBeTruthy()
    expect(screen.getByText('Online')).toBeTruthy()
    expect(screen.getByText('Tap to connect')).toBeTruthy()
    expect(screen.getByText('Local')).toBeTruthy()
    expect(screen.getByText('Build Runner')).toBeTruthy()
    expect(screen.getByText('Needs attention')).toBeTruthy()
    expect(screen.getByText(/Last online/i)).toBeTruthy()
    expect(screen.getByTestId('anytty-machine-list').textContent).not.toMatch(/workspace|tab|window|pane|tmux|session/i)

    await userEvent.click(screen.getByRole('button', { name: /scan pairing qr/i }))
    await userEvent.click(screen.getByRole('button', { name: /connect to macbook pro/i }))

    expect(onScanMachine).toHaveBeenCalledTimes(1)
    expect(onSelectMachine).toHaveBeenCalledTimes(1)
    expect(onSelectMachine).toHaveBeenCalledWith(machine({
      machineId: 'machine-local',
      name: 'MacBook Pro',
      hostname: 'mbp.local',
      state: 'online',
      terminalCount: 3,
      lastSeenAt: '2026-05-03T07:22:00Z',
      lastConnectionPath: 'local',
      source: 'local',
    }))
  })

  it('opens machine details from a context-menu style gesture without connecting', async () => {
    const onSelectMachine = vi.fn()
    render(
      <MachineList
        machines={[
          machine({
            machineId: 'machine-local',
            name: 'MacBook Pro',
            hostname: 'mbp.local',
            state: 'online',
            terminalCount: 3,
            lastSeenAt: '2026-05-03T07:22:00Z',
            lastConnectionPath: 'local',
            source: 'local',
          }),
        ]}
        onScanMachine={vi.fn()}
        onSelectMachine={onSelectMachine}
      />,
    )

    const trigger = screen.getByRole('button', { name: /connect to macbook pro/i })
    trigger.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true }))

    expect(await screen.findByTestId('anytty-machine-detail-sheet')).toBeTruthy()
    const dialog = screen.getByRole('dialog', { name: 'MacBook Pro' })
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(screen.getByText('Device details')).toBeTruthy()
    expect(screen.getByText('machine-local')).toBeTruthy()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: /close device details/i }))
    expect(onSelectMachine).not.toHaveBeenCalled()

    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'MacBook Pro' })).toBeNull()
    expect(document.activeElement).toBe(trigger)
    expect(onSelectMachine).not.toHaveBeenCalled()
  })

  it('shows a QR-only empty state without account or manual-add actions', () => {
    render(
      <MachineList
        machines={[]}
        onScanMachine={vi.fn()}
        onSelectMachine={vi.fn()}
      />,
    )

    expect(screen.getByTestId('anytty-machine-empty-state')).toBeTruthy()
    expect(screen.getByText('No devices yet')).toBeTruthy()
    expect(screen.getByText(/scan the QR code shown by the AnyTTY service/i)).toBeTruthy()
    expect(screen.getByRole('button', { name: /scan pairing qr/i })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /add device/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /sign in/i })).toBeNull()
    expect(screen.getByTestId('anytty-machine-list').textContent).not.toMatch(/terminal page|workspace|tab|window|pane|tmux|session/i)
  })

  it('keeps public props QR-only and machine-focused', () => {
    const props = {
      machines: [],
      onScanMachine: vi.fn(),
      onSelectMachine: vi.fn(),
    } satisfies MachineListProps

    render(<MachineList {...props} />)

    expect(screen.getByTestId('anytty-machine-list')).toBeTruthy()
    expect(screen.getAllByRole('button')).toHaveLength(2)
    expect(screen.getAllByRole('button').every((button) => /scan|close/i.test(button.getAttribute('aria-label') ?? button.textContent ?? ''))).toBe(true)
  })
})

function machine(overrides: Partial<AppMachineRecord>): AppMachineRecord {
  return {
    machineId: overrides.machineId ?? 'machine-1',
    name: overrides.name ?? 'MacBook Pro',
    hostname: overrides.hostname ?? 'mbp.local',
    state: overrides.state ?? 'online',
    terminalCount: overrides.terminalCount ?? 1,
    lastSeenAt: overrides.lastSeenAt,
    lastConnectionPath: overrides.lastConnectionPath,
    preferredPath: overrides.preferredPath ?? 'local',
    relayInUse: overrides.relayInUse ?? false,
    source: overrides.source ?? 'local',
  }
}
