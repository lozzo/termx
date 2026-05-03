import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RemoteAppShell } from './RemoteAppShell'
import type { AppMachineRecord } from './appMachine'

describe('RemoteAppShell', () => {
  afterEach(() => {
    cleanup()
  })

  it('defaults to the APP machine list instead of a terminal surface', () => {
    render(
      <RemoteAppShell
        machines={[machine({ machineId: 'machine-local', name: 'Local Mac', terminalCount: 2 })]}
        onAddMachine={vi.fn()}
        onScanMachine={vi.fn()}
        onStartConnection={vi.fn()}
      />,
    )

    expect(screen.getByTestId('termx-remote-app-shell')).toBeTruthy()
    expect(screen.getByTestId('termx-machine-list')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Machines' })).toBeTruthy()
    expect(screen.queryByTestId('termx-terminal')).toBeNull()
    expect(screen.queryByTestId('termx-terminal-list')).toBeNull()
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|tmux|session/i)
  })

  it('enters a connection flow when a machine is clicked and does not open terminal immediately', async () => {
    const onStartConnection = vi.fn(async () => ({
      stage: 'trying_public_p2p' as const,
      message: 'Local unavailable. Trying public P2P.',
    }))

    render(
      <RemoteAppShell
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
    expect(screen.getByTestId('termx-connection-flow')).toBeTruthy()
    expect(screen.getByText('Trying local')).toBeTruthy()
    expect(screen.queryByTestId('termx-terminal')).toBeNull()
    expect(screen.queryByTestId('termx-terminal-list')).toBeNull()

    await waitFor(() => expect(screen.getByText('Trying public P2P')).toBeTruthy())
    expect(screen.getByText('Local unavailable. Trying public P2P.')).toBeTruthy()
  })

  it('surfaces managed relay as connection info without exposing relay as a path', async () => {
    const onStartConnection = vi.fn(async () => ({
      stage: 'trying_managed' as const,
      path: 'managed' as const,
      relayInUse: true,
      message: 'Using managed connection.',
    }))

    render(
      <RemoteAppShell
        machines={[machine({ machineId: 'machine-managed', name: 'Office Linux', preferredPath: 'managed' })]}
        onAddMachine={vi.fn()}
        onScanMachine={vi.fn()}
        onStartConnection={onStartConnection}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /connect to office linux/i }))

    await waitFor(() => expect(screen.getByText('Trying managed')).toBeTruthy())
    expect(screen.getByText('Relay active')).toBeTruthy()
    expect(screen.getByTestId('termx-connection-flow').textContent).not.toMatch(/\brelay path\b|\bpaid relay\b|\banonymous p2p\b|\bmanaged p2p\b/i)
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
