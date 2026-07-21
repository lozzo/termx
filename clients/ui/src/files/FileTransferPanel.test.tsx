import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { FileTransferPanel } from './FileTransferPanel'
import type { TransferInfo } from './fileApi'

describe('FileTransferPanel', () => {
  afterEach(() => {
    cleanup()
  })

  it('shows source machine and transfer target details for downloads and uploads', async () => {
    render(
      <FileTransferPanel
        transfers={[
          transfer({
            id: 'download-1',
            machineId: 'machine-a',
            name: 'server.log',
            direction: 'download',
            totalSize: 1024,
            transferredSize: 1024,
            status: 'completed',
            filePath: '/var/log/server.log',
            savedPath: 'Downloads/Muxvia/server.log',
          }),
          transfer({
            id: 'upload-1',
            machineId: 'machine-b',
            name: 'report.txt',
            direction: 'upload',
            totalSize: 512,
            transferredSize: 120,
            status: 'transferring',
            targetDir: '/srv/reports',
          }),
        ]}
        hasActiveTransfers
        resolveMachineLabel={(machineId) => machineId === 'machine-a' ? 'Build Runner' : machineId === 'machine-b' ? 'Office Mac' : machineId}
        onCancel={vi.fn()}
        onDismiss={vi.fn()}
        open
      />,
    )

    expect(screen.getByText('From Build Runner')).toBeTruthy()
    expect(screen.getByText('/var/log/server.log -> Downloads/Muxvia/server.log')).toBeTruthy()
    expect(screen.getByText('To Office Mac')).toBeTruthy()
    expect(screen.getByText('/srv/reports / report.txt')).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: /close data transfer center/i }))
  })

  it('sorts newest transfer tasks first by added time', () => {
    render(
      <FileTransferPanel
        transfers={[
          transfer({ id: 'old', name: 'old.log', startedAt: 1000, status: 'completed', transferredSize: 1 }),
          transfer({ id: 'new', name: 'new.log', startedAt: 3000, status: 'completed', transferredSize: 1 }),
          transfer({ id: 'middle', name: 'middle.log', startedAt: 2000, status: 'completed', transferredSize: 1 }),
        ]}
        hasActiveTransfers={false}
        onCancel={vi.fn()}
        onDismiss={vi.fn()}
        open
      />,
    )

    const items = screen.getAllByRole('listitem')
    expect(items).toHaveLength(3)
    const [first, second, third] = items
    expect(first?.textContent).toContain('new.log')
    expect(second?.textContent).toContain('middle.log')
    expect(third?.textContent).toContain('old.log')
  })

  it('runs bulk controls for selected, completed, and failed transfer tasks', async () => {
    const onPause = vi.fn()
    const onResume = vi.fn()
    const onDismiss = vi.fn()

    render(
      <FileTransferPanel
        transfers={[
          transfer({ id: 'active-1', name: 'active.bin', status: 'transferring', startedAt: 5000, transferredSize: 4, totalSize: 10 }),
          transfer({ id: 'paused-1', name: 'paused.bin', status: 'paused', startedAt: 4000, transferredSize: 4, totalSize: 10 }),
          transfer({ id: 'failed-1', name: 'failed.bin', status: 'failed', startedAt: 3000, error: 'network lost' }),
          transfer({ id: 'missing-1', name: 'missing.bin', status: 'missing', startedAt: 2000 }),
          transfer({ id: 'done-1', name: 'done.bin', status: 'completed', startedAt: 1000, transferredSize: 1 }),
        ]}
        hasActiveTransfers
        onCancel={vi.fn()}
        onDismiss={onDismiss}
        onPause={onPause}
        onResume={onResume}
        open
      />,
    )

    expect(screen.queryByRole('button', { name: /select all transfers/i })).toBeNull()

    await userEvent.click(screen.getByRole('button', { name: /select transfers/i }))
    await userEvent.click(screen.getByRole('button', { name: /select all transfers/i }))
    await userEvent.click(screen.getByRole('button', { name: /pause selected transfers/i }))
    await userEvent.click(screen.getByRole('button', { name: /start selected transfers/i }))
    await userEvent.click(screen.getByRole('button', { name: /^cancel$/i }))
    await userEvent.click(screen.getByRole('button', { name: /delete all completed transfers/i }))
    await userEvent.click(screen.getByRole('button', { name: /delete all failed transfers/i }))

    expect(onPause).toHaveBeenCalledTimes(1)
    expect(onPause).toHaveBeenCalledWith('active-1')
    expect(onResume).toHaveBeenCalledTimes(3)
    expect(onResume).toHaveBeenCalledWith('paused-1')
    expect(onResume).toHaveBeenCalledWith('failed-1')
    expect(onResume).toHaveBeenCalledWith('missing-1')
    expect(onDismiss).toHaveBeenCalledTimes(3)
    expect(onDismiss).toHaveBeenCalledWith('done-1')
    expect(onDismiss).toHaveBeenCalledWith('failed-1')
    expect(onDismiss).toHaveBeenCalledWith('missing-1')
  })
})

function transfer(overrides: Partial<TransferInfo>): TransferInfo {
  return {
    id: overrides.id ?? 'transfer-1',
    name: overrides.name ?? 'file.txt',
    direction: overrides.direction ?? 'download',
    totalSize: overrides.totalSize ?? 1,
    transferredSize: overrides.transferredSize ?? 0,
    status: overrides.status ?? 'pending',
    startedAt: overrides.startedAt ?? Date.now(),
    ...(overrides.machineId !== undefined ? { machineId: overrides.machineId } : {}),
    ...(overrides.updatedAt !== undefined ? { updatedAt: overrides.updatedAt } : { updatedAt: Date.now() }),
    ...(overrides.bytesPerSecond !== undefined ? { bytesPerSecond: overrides.bytesPerSecond } : {}),
    ...(overrides.error !== undefined ? { error: overrides.error } : {}),
    ...(overrides.filePath !== undefined ? { filePath: overrides.filePath } : {}),
    ...(overrides.localUri !== undefined ? { localUri: overrides.localUri } : {}),
    ...(overrides.targetDir !== undefined ? { targetDir: overrides.targetDir } : {}),
    ...(overrides.savedPath !== undefined ? { savedPath: overrides.savedPath } : {}),
    ...(overrides.savedUri !== undefined ? { savedUri: overrides.savedUri } : {}),
  }
}
