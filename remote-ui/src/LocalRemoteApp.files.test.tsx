import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, useImperativeHandle } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LocalRemoteApp, type LocalRemoteTransportFactory } from './LocalRemoteApp'
import type { TerminalModifierState } from './mobileTerminalInput'
import type { LocalAgentApi } from './transport'
import { createMockFilePeerTransport } from './test/mockFileTransport'
import type { TerminalHandle } from './Terminal'
import type { TerminalTransport, TerminalTransportEvent } from './terminalClient'

vi.mock('./Terminal', () => ({
  Terminal: forwardRef<TerminalHandle, { machineId: string; terminalId: string; modifierState?: TerminalModifierState }>(function MockTerminal(
    { machineId, terminalId, modifierState },
    ref,
  ) {
    useImperativeHandle(ref, () => ({
      sendInput: vi.fn(),
      sendResize: vi.fn(),
      reattach: vi.fn(),
      focus: vi.fn(),
      blur: vi.fn(),
      fit: vi.fn(),
      pasteText: vi.fn(),
      getCursorInfo: vi.fn(() => null),
      adjustInputPosition: vi.fn(),
      getBufferType: vi.fn(() => 'normal' as const),
    }))
    return (
      <section
        data-machine-id={machineId}
        data-modifier-state={`${modifierState?.ctrl ?? 'off'}:${modifierState?.alt ?? 'off'}`}
        data-terminal-id={terminalId}
        data-testid="termx-terminal"
      />
    )
  }),
}))

describe('LocalRemoteApp real file manager flow', () => {
  afterEach(() => {
    cleanup()
  })

  it('preserves file navigation across list and terminal pages for the same terminal, then resets for a new terminal context', async () => {
    const api = createMockLocalAgentApi()
    const transports: Array<ReturnType<typeof createMockLocalRemoteTransport>> = []
    const createTransport = vi.fn<LocalRemoteTransportFactory>(({ machineId, terminalId }) =>
      trackTransport(transports, createMockLocalRemoteTransport(machineId, terminalId)),
    )

    render(<LocalRemoteApp api={api} createTransport={createTransport} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())

    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /open tmp/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open tmp/i }))
    await waitFor(() => expect(screen.getByText('log.txt')).toBeTruthy())

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').style.visibility).toBe('hidden'))

    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByText('log.txt')).toBeTruthy())

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').style.visibility).toBe('hidden'))

    await userEvent.click(screen.getByRole('button', { name: /open worker/i }))
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByText('cache')).toBeTruthy())
    expect(screen.queryByText('log.txt')).toBeNull()
    expect(createTransport).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-local',
      terminalId: 'terminal-2',
    }))
  })
})

function createMockLocalRemoteTransport(
  machineId: string,
  terminalId: string,
): ReturnType<typeof createMockFilePeerTransport> & TerminalTransport & {
  connectCalls: Array<{ machineId: string; terminalId?: string; mode: string }>
  disconnectCalls: number
} {
  const transport = createMockFilePeerTransport({
    '/files/list': ({ path }: { path?: string }) => {
      if (terminalId === 'terminal-2') {
        if (path === '/srv/worker/tmp') {
          return {
            path: '/srv/worker/tmp',
            parent: '/srv/worker',
            total: 1,
            entries: [{ name: 'cache.log', type: 'file', size: 7 }],
          }
        }
        return {
          path: path ?? '/srv/worker',
          parent: '',
          total: 1,
          entries: [{ name: 'cache', type: 'dir', size: 0 }],
        }
      }
      if (path === '/Users/lozzow/project/tmp') {
        return {
          path: '/Users/lozzow/project/tmp',
          parent: '/Users/lozzow/project',
          total: 1,
          entries: [{ name: 'log.txt', type: 'file', size: 42 }],
        }
      }
      return {
        path: path ?? '/Users/lozzow/project',
        parent: '',
        total: 1,
        entries: [{ name: 'tmp', type: 'dir', size: 0 }],
      }
    },
  }, {}, { machineId, terminalId })

  return Object.assign(transport, {
    connectCalls: [] as Array<{ machineId: string; terminalId?: string; mode: string }>,
    disconnectCalls: 0,
    async connect(input: { machineId: string; terminalId?: string; mode: string }) {
      this.connectCalls.push(input)
    },
    async disconnect() {
      this.disconnectCalls += 1
    },
    async openTerminal() {
      return {
        label: `terminal:${terminalId}`,
        readyState: 'open' as const,
        send() {},
        close() {},
      }
    },
    subscribeTerminal(_id: string, _handler: (event: TerminalTransportEvent) => void) {
      return () => {}
    },
    closeTerminalChannel() {},
  })
}

function trackTransport<T extends ReturnType<typeof createMockLocalRemoteTransport>>(transports: T[], transport: T): T {
  transports.push(transport)
  return transport
}

function createMockLocalAgentApi(): LocalAgentApi {
  return {
    async getStatus() {
      return {
        machine: {
          machineId: 'machine-local',
          name: 'Local Mac',
          state: 'online',
          terminalCount: 2,
          localRTC: { signalingUrl: 'http://127.0.0.1:18888/api/local/rtc/offer' },
        },
        localWeb: {
          httpUrl: 'http://127.0.0.1:18888',
          rtcOfferUrl: 'http://127.0.0.1:18888/api/local/rtc/offer',
        },
      }
    },
    async listTerminals() {
      return [{
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        title: 'zsh',
        state: 'running',
        command: '/bin/zsh',
        cols: 120,
        rows: 36,
        cwd: '/Users/lozzow/project',
        sizeLocked: true,
        sizeLockMode: 'lock',
        environment: 'dev',
      }, {
        machineId: 'machine-local',
        terminalId: 'terminal-2',
        title: 'worker',
        state: 'running',
        command: '/usr/bin/env bash',
        cols: 90,
        rows: 28,
        cwd: '/srv/worker',
        sizeLocked: false,
        sizeLockMode: 'off',
        environment: 'prod',
      }]
    },
    async pair() {
      throw new Error('pair is not used by LocalRemoteApp files tests')
    },
    async createRTCAnswer() {
      throw new Error('createRTCAnswer is not used by LocalRemoteApp files tests')
    },
    async createInventoryRTCAnswer() {
      throw new Error('createInventoryRTCAnswer is not used by LocalRemoteApp files tests')
    },
    async createTerminal() {
      throw new Error('createTerminal is not used by LocalRemoteApp files tests')
    },
    async updateTerminal() {
      throw new Error('updateTerminal is not used by LocalRemoteApp files tests')
    },
    async deleteTerminal() {
      throw new Error('deleteTerminal is not used by LocalRemoteApp files tests')
    },
  }
}
