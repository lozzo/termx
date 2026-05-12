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
            savedPath: 'Downloads/TermX/server.log',
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
    expect(screen.getByText('/var/log/server.log -> Downloads/TermX/server.log')).toBeTruthy()
    expect(screen.getByText('To Office Mac')).toBeTruthy()
    expect(screen.getByText('/srv/reports / report.txt')).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: /close data transfer center/i }))
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
