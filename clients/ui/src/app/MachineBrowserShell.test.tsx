import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MachineBrowserShell } from './MachineBrowserShell'
import type { AppMachineRecord } from '../state/appMachine'

describe('MachineBrowserShell', () => {
  afterEach(() => {
    cleanup()
  })

  it('defaults to the APP machine list instead of a terminal surface', () => {
    render(
      <MachineBrowserShell
        machines={[machine({ machineId: 'machine-local', name: 'Local Mac', terminalCount: 2 })]}
        onAddMachine={vi.fn()}
        onScanMachine={vi.fn()}
        onStartConnection={vi.fn()}
      />,
    )

    expect(screen.getByTestId('anytty-remote-app-shell')).toBeTruthy()
    expect(screen.getByTestId('anytty-machine-list')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Devices' })).toBeTruthy()
    expect(screen.queryByTestId('anytty-terminal')).toBeNull()
    expect(screen.queryByTestId('anytty-terminal-list')).toBeNull()
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|tmux|session/i)
  })

  it('enters a connection flow when a machine is clicked and does not open terminal immediately', async () => {
    const onStartConnection = vi.fn(async () => ({
      stage: 'trying_hub' as const,
      message: 'Local unavailable. Trying Hub.',
    }))

    render(
      <MachineBrowserShell
        machines={[machine({ machineId: 'machine-local', name: 'Local Mac', terminalCount: 2 })]}
        onAddMachine={vi.fn()}
        onScanMachine={vi.fn()}
        onStartConnection={onStartConnection}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /connect to local mac/i }))

    expect(onStartConnection).toHaveBeenCalledWith(machine({
      machineId: 'machine-local',
      name: 'Local Mac',
      terminalCount: 2,
    }))
    expect(screen.getByTestId('anytty-connection-flow')).toBeTruthy()
    expect(screen.getByText('Connecting to device')).toBeTruthy()
    expect(screen.queryByTestId('anytty-terminal')).toBeNull()
    expect(screen.queryByTestId('anytty-terminal-list')).toBeNull()

    await waitFor(() => expect(screen.getByText('Connecting to device')).toBeTruthy())
    expect(screen.getByText('Finding the best available connection...')).toBeTruthy()
    expect(screen.queryByText('Local unavailable. Trying Hub.')).toBeNull()
  })

  it('surfaces hub relay as connection info without exposing relay as a path', async () => {
    const onStartConnection = vi.fn(async () => ({
      stage: 'trying_hub' as const,
      path: 'hub' as const,
      relayInUse: true,
      message: 'Using hub connection.',
    }))

    render(
      <MachineBrowserShell
        machines={[machine({ machineId: 'machine-hub', name: 'Office Linux', preferredPath: 'hub' })]}
        onAddMachine={vi.fn()}
        onScanMachine={vi.fn()}
        onStartConnection={onStartConnection}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /connect to office linux/i }))

    await waitFor(() => expect(screen.getByText('Connecting to device')).toBeTruthy())
    expect(screen.queryByText('Relay active')).toBeNull()
    expect(screen.getByTestId('anytty-connection-flow').textContent).not.toMatch(/\bhub\b|\brelay\b|\bp2p\b|\bice\b/i)
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
