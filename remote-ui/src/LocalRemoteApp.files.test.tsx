import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, useImperativeHandle } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LocalRemoteApp, type LocalRemoteSessionConnector } from './LocalRemoteApp'
import type { TerminalModifierState } from './mobileTerminalInput'
import type { LocalStatus, RtcSession } from './transport'
import { createMockFileSession } from './test/mockFileSession'
import type { TerminalHandle } from './Terminal'
import type { Terminal } from './model'

vi.mock('./Terminal', () => ({
  Terminal: forwardRef<TerminalHandle, { machineId: string; terminalId: string; modifierState?: TerminalModifierState; selectionMode?: boolean }>(function MockTerminal(
    { machineId, terminalId, modifierState, selectionMode },
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
      selectAll: vi.fn(),
      selectVisible: vi.fn(),
      getSelection: vi.fn(() => ''),
      hasSelection: vi.fn(() => false),
      clearSelection: vi.fn(),
      getCursorInfo: vi.fn(() => null),
      adjustInputPosition: vi.fn(),
      getBufferType: vi.fn(() => 'normal' as const),
    }))
    return (
      <section
        data-machine-id={machineId}
        data-modifier-state={`${modifierState?.ctrl ?? 'off'}:${modifierState?.alt ?? 'off'}`}
        data-selection-mode={selectionMode ? 'true' : 'false'}
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
    const sessions: Array<ReturnType<typeof createMockLocalRemoteSession>> = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession(machineId))),
    )
    const connector = { connect } satisfies LocalRemoteSessionConnector

    render(<LocalRemoteApp api={api} connector={connector} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())

    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /open tmp/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open tmp/i }))
    await waitFor(() => expect(screen.getByText('log.txt')).toBeTruthy())

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))

    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByText('live.txt')).toBeTruthy())

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))

    await userEvent.click(screen.getByRole('button', { name: /open worker/i }))
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByText('cache')).toBeTruthy())
    expect(screen.queryByText('log.txt')).toBeNull()
    expect(connect).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-local',
    }))
    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('opens files from the terminal-reported live directory when available', async () => {
    const api = createMockLocalAgentApi()
    const sessions: Array<ReturnType<typeof createMockLocalRemoteSession>> = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession(machineId))),
    )

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))

    await waitFor(() => expect(screen.getByText('live.txt')).toBeTruthy())
    expect(sessions[0]?.requests).toEqual(expect.arrayContaining([
      { method: 'get_directory', path: 'get_directory', params: { terminal_id: 'terminal-1' } },
      { method: 'GET', path: '/files/list', params: { path: '/Users/lozzow/project', offset: 0, limit: 500 } },
    ]))
  })
})

function createMockLocalRemoteSession(
  machineId: string,
): ReturnType<typeof createMockFileSession> & RtcSession & {
  disconnectCalls: number
} {
  const session = createMockFileSession({
    get_directory: ({ terminal_id }: { terminal_id?: string }) => ({
      path: terminal_id === 'terminal-1' ? '/Users/lozzow/project' : '',
      source: 'live',
    }),
    '/files/list': ({ path }: { path?: string }) => {
      if (path === '/srv/worker/tmp') {
        return {
          path: '/srv/worker/tmp',
          parent: '/srv/worker',
          total: 1,
          entries: [{ name: 'cache.log', type: 'file', size: 7 }],
        }
      }
      if (path === '/srv/worker') {
        return {
          path: '/srv/worker',
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
      if (path === '/Users/lozzow/project') {
        return {
          path: '/Users/lozzow/project',
          parent: '',
          total: 2,
          entries: [{ name: 'tmp', type: 'dir', size: 0 }, { name: 'live.txt', type: 'file', size: 1 }],
        }
      }
      return {
        path: path ?? '/Users/lozzow/project',
        parent: '',
        total: 0,
        entries: [],
      }
    },
  }, {}, { machineId })

  return Object.assign(session, {
    disconnectCalls: 0,
    async disconnect() {
      this.disconnectCalls += 1
    },
    async openTerminal(terminalId: string) {
      return {
        label: `terminal:${terminalId}`,
        readyState: 'open' as const,
        send() {},
        close() {},
        onMessage() { return { close() {} } },
        onClose() { return { close() {} } },
        async waitOpen() {},
      }
    },
  })
}

function trackSession<T extends ReturnType<typeof createMockLocalRemoteSession>>(sessions: T[], session: T): T {
  sessions.push(session)
  return session
}

function createMockLocalAgentApi() {
  return {
    async getStatus(): Promise<LocalStatus> {
      return {
        machine: {
          machineId: 'machine-local',
          name: 'Local Mac',
          state: 'online',
          terminalCount: 2,
          localRTC: { signalingUrl: 'http://127.0.0.1:18888' },
        },
        localWeb: {
          httpUrl: 'http://127.0.0.1:18888',
          rtcOfferUrl: 'http://127.0.0.1:18888',
        },
      }
    },
    async listTerminals(): Promise<Terminal[]> {
      return [{
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        title: 'zsh',
        state: 'running' as const,
        command: '/bin/zsh',
        cols: 120,
        rows: 36,
        cwd: '/Users/lozzow/project',
        sizeLocked: true,
        sizeLockMode: 'lock' as const,
        environment: 'dev',
      }, {
        machineId: 'machine-local',
        terminalId: 'terminal-2',
        title: 'worker',
        state: 'running' as const,
        command: '/usr/bin/env bash',
        cols: 90,
        rows: 28,
        cwd: '/srv/worker',
        sizeLocked: false,
        sizeLockMode: 'off' as const,
        environment: 'prod',
      }]
    },
  }
}
