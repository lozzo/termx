import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MachineList, type MachineListProps } from './MachineList'
import type { AppMachineRecord } from '../state/appMachine'
import appMachineSource from '../state/appMachine.ts?raw'
import machineListSource from './MachineList.tsx?raw'

describe('MachineList', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders a dense APP-first machine list with add and scan actions', async () => {
    const onAddMachine = vi.fn()
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
        onAddMachine={onAddMachine}
        onScanMachine={onScanMachine}
        onSelectMachine={onSelectMachine}
      />,
    )

    expect(screen.getByTestId('termx-machine-list')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Machines' })).toBeTruthy()
    expect(screen.getByRole('button', { name: /add machine/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /scan pairing qr/i })).toBeTruthy()
    expect(screen.getByText('MacBook Pro')).toBeTruthy()
    expect(screen.getByText('mbp.local')).toBeTruthy()
    expect(screen.getByText('Online')).toBeTruthy()
    expect(screen.getByText('Tap to connect')).toBeTruthy()
    expect(screen.getByText('Local')).toBeTruthy()
    expect(screen.getByText('Build Runner')).toBeTruthy()
    expect(screen.getByText('Stale')).toBeTruthy()
    expect(screen.getByText(/Last online/i)).toBeTruthy()
    expect(screen.getByTestId('termx-machine-list').textContent).not.toMatch(/workspace|tab|window|pane|tmux|session/i)

    await userEvent.click(screen.getByRole('button', { name: /scan pairing qr/i }))
    await userEvent.click(screen.getByRole('button', { name: /add machine/i }))
    await userEvent.click(screen.getByRole('button', { name: /connect to macbook pro/i }))

    expect(onScanMachine).toHaveBeenCalledTimes(1)
    expect(onAddMachine).toHaveBeenCalledTimes(1)
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
        onAddMachine={vi.fn()}
        onScanMachine={vi.fn()}
        onSelectMachine={onSelectMachine}
      />,
    )

    screen.getByRole('button', { name: /connect to macbook pro/i }).dispatchEvent(new MouseEvent('contextmenu', { bubbles: true }))

    expect(await screen.findByTestId('termx-machine-detail-sheet')).toBeTruthy()
    expect(screen.getByText('Machine details')).toBeTruthy()
    expect(screen.getByText('machine-local')).toBeTruthy()
    expect(onSelectMachine).not.toHaveBeenCalled()
  })

  it('shows a compact empty state that only points to add, scan, or login', () => {
    render(
      <MachineList
        machines={[]}
        authState="anonymous"
        onAddMachine={vi.fn()}
        onScanMachine={vi.fn()}
        onSelectMachine={vi.fn()}
      />,
    )

    expect(screen.getByTestId('termx-machine-empty-state')).toBeTruthy()
    expect(screen.getByText('No machines yet')).toBeTruthy()
    expect(screen.getByText(/add or scan a termx qr/i)).toBeTruthy()
    expect(screen.getByRole('button', { name: /scan pairing qr/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /add machine/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /sign in/i })).toBeTruthy()
    expect(screen.getByTestId('termx-machine-list').textContent).not.toMatch(/terminal page|workspace|tab|window|pane|tmux|session/i)
  })

  it('keeps public props machine-focused and implementation free of old path taxonomy', () => {
    const props = {
      machines: [],
      onAddMachine: vi.fn(),
      onScanMachine: vi.fn(),
      onSelectMachine: vi.fn(),
    } satisfies MachineListProps

    render(<MachineList {...props} />)

    expect(screen.getByTestId('termx-machine-list')).toBeTruthy()
    expect(`${appMachineSource}\n${machineListSource}`).not.toMatch(/anonymous_p2p|managed_p2p|paid_relay|\brelayPath\b|\bpaidRelay\b|\bsessions\b|\bpanes\b|\bworkspace\b|\btmux\b/)
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
