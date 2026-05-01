import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { FileManager, type FileManagerProps } from './FileManager'
import { createMockFilePeerTransport } from './test/mockFileTransport'

describe('FileManager', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders directory entries and navigates directories through the injected transport', async () => {
    const transport = createMockFilePeerTransport({
      '/files/list': ({ path }: { path?: string }) => ({
        path,
        parent: path === '/' ? '' : '/',
        total: 1,
        entries: [{ name: path === '/' ? 'tmp' : 'log.txt', type: path === '/' ? 'dir' : 'file', size: 42 }],
      }),
    }, {}, { terminalId: 'terminal-1' })

    render(
      <FileManager
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
        initialPath="/"
      />,
    )

    await waitFor(() => expect(screen.getByText('tmp')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open tmp/i }))
    await waitFor(() => expect(screen.getByText('log.txt')).toBeTruthy())
    expect(screen.getByTestId('termx-file-manager').textContent).not.toMatch(/workspace|tab|window|pane|session/i)
  })

  it('keeps props interface-based and free of browser/native transport implementations', () => {
    const propKeys = Object.keys({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      transport: createMockFilePeerTransport(),
    } satisfies FileManagerProps)

    expect(propKeys).not.toContain('webrtcTransport')
    expect(propKeys).not.toContain('rtcPeerConnection')
    expect(propKeys).not.toContain('nativePlugin')
    expect(propKeys).not.toContain('relayCredentials')
  })

  it('requires terminalId in the public file manager props contract', () => {
    const props = {
      machineId: 'machine-local',
      transport: createMockFilePeerTransport(),
    }

    // @ts-expect-error file manager is terminal-scoped and must not be opened for machine-only access.
    const _withoutTerminal: FileManagerProps = props

    expect(Object.keys(props)).not.toContain('terminalId')
  })
})
