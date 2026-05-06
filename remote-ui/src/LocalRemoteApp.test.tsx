import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, useImperativeHandle } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LocalRemoteApp, type LocalRemoteSessionConnector } from './LocalRemoteApp'
import { createMachineSessionStore } from './localAppIdentity'
import type { TerminalModifierState } from './mobileTerminalInput'
import type { LocalPairingApi, LocalStatus, RtcSession, TerminalInventoryEvents } from './transport'
import { createMockFileSession } from './test/mockFileSession'
import type { TerminalHandle } from './Terminal'
import type { Terminal } from './model'

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

vi.mock('./FileManager', async () => {
  const React = await import('react')
  return {
    FileManager: ({ machineId, terminalId, initialPath }: { machineId: string; terminalId: string; initialPath?: string }) => {
      const [currentPath, setCurrentPath] = React.useState(initialPath ?? '')
      return (
        <section
          data-current-path={currentPath}
          data-initial-path={initialPath ?? ''}
          data-machine-id={machineId}
          data-terminal-id={terminalId}
          data-testid="termx-file-manager"
        >
          <button type="button" onClick={() => setCurrentPath('/tmp')}>Open tmp</button>
        </section>
      )
    },
  }
})

describe('LocalRemoteApp', () => {
  afterEach(() => {
    cleanup()
  })

  it('loads local machine terminals and composes shared terminal/file manager components', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId, terminalId }: { machineId: string; terminalId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({
          '/files/list': { path: '/', parent: '', total: 0, entries: [] },
        }, machineId, terminalId))),
    )
    const connector = { connect } satisfies LocalRemoteSessionConnector

    render(<LocalRemoteApp api={api} connector={connector} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(screen.queryByTestId('termx-terminal')).toBeNull()
    expect(connect).not.toHaveBeenCalled()
    expect(screen.getByText('120 × 36')).toBeTruthy()
    expect(screen.getByTestId('termx-terminal-list').textContent).toMatch(/120 × 36/)
    expect(screen.getByText('/Users/lozzow/project')).toBeTruthy()
    expect(screen.getByText('dev')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay')).toBeTruthy())
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('terminal-1')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-initial-path')).toBe('/Users/lozzow/project')
    expect(screen.queryByTestId('termx-terminal')).toBeNull()
    await waitFor(() => expect(connect).toHaveBeenCalledTimes(1))
    await userEvent.click(screen.getByRole('button', { name: 'Open tmp' }))
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/tmp')
    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))
    expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))

    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    expect(screen.queryByTestId('termx-terminal-list-page')).toBeNull()
    expect(screen.getByTestId('termx-terminal-list').getAttribute('data-machine-id')).toBe('machine-local')
    expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1')
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager')).toBeTruthy())
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('terminal-1')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-initial-path')).toBe('/Users/lozzow/project')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/tmp')
    expect(connect).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    }))
    expect(sessions[1]).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))
    expect(screen.getByTestId('termx-terminal')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|session/i)
  })

  it('preserves file state for the same terminal but resets when the file context terminal changes', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId, terminalId }: { machineId: string; terminalId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({
        '/files/list': { path: '/', parent: '', total: 0, entries: [] },
      }, machineId, terminalId))),
    )
    const connector = { connect } satisfies LocalRemoteSessionConnector

    render(<LocalRemoteApp api={api} connector={connector} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: 'Open tmp' }))
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/tmp')

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/tmp'))

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))
    await userEvent.click(screen.getByRole('button', { name: /open worker/i }))
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('terminal-2'))
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-initial-path')).toBe('/srv/worker')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/srv/worker')
  })

  it('uses a terminal-first mobile shell with sheets instead of a Config sidebar', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId, terminalId }: { machineId: string; terminalId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({
        '/files/list': { path: '/', parent: '', total: 0, entries: [] },
      }, machineId, terminalId))),
    )
    const connector = { connect } satisfies LocalRemoteSessionConnector
    const pairApi = createMockPairApi()
    pairApi.pair = vi.fn(async () => ({
      machineId: 'machine-local',
      sessionToken: 'session-token-local',
      expiresAt: '2026-05-01T07:00:00Z',
    }))
    const storage = new MemoryStorage()

    render(
      <LocalRemoteApp
        api={api}
        connector={connector}
        pair={{
          api: pairApi,
          sessionStore: createMachineSessionStore(storage),
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(screen.queryByText('Config')).toBeNull()
    expect(screen.getByText('Local Mac')).toBeTruthy()
    expect(screen.getByTestId('termx-verification-gate')).toBeTruthy()
    expect(screen.queryByTestId('termx-mobile-keybar')).toBeNull()
    expect(connect).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: /verify device/i }))
    await waitFor(() => expect(screen.getByTestId('termx-pair-sheet')).toBeTruthy())
    await userEvent.type(screen.getByLabelText('Pair ID'), 'pair-1')
    await userEvent.type(screen.getByLabelText('Pair secret'), 'secret-1')
    await userEvent.click(within(screen.getByTestId('termx-pair-sheet')).getByRole('button', { name: /^pair device$/i }))
    await waitFor(() => expect(screen.getByText('Paired with machine-local')).toBeTruthy())

    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    expect(screen.getByTestId('termx-mobile-keybar')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: 'Ctrl' }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-modifier-state')).toBe('once:off'))

    expect(screen.getByRole('button', { name: /back to terminal list/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /show terminal list/i })).toBeTruthy()
    expect(screen.getByTestId('termx-terminal-title').textContent).toContain('zsh')
    expect(screen.queryByRole('navigation', { name: /mobile terminal navigation/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /^console$/i })).toBeNull()

    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay')).toBeTruthy())
    expect(screen.getByTestId('termx-file-manager')).toBeTruthy()
    expect(screen.getByTestId('termx-terminal')).toBeTruthy()
    expect(screen.getByTestId('termx-terminal-panel').className).toContain('flex-1')
    expect(screen.getByTestId('termx-machine-files-overlay').className).toContain('absolute')
    expect(screen.getByTestId('termx-mobile-keybar').className.split(/\s+/)).not.toContain('hidden')

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))
    expect(screen.getByTestId('termx-terminal')).toBeTruthy()
    expect(screen.getByTestId('termx-mobile-keybar').className.split(/\s+/)).not.toContain('hidden')

    await userEvent.click(screen.getByRole('button', { name: /show terminal list/i }))
    expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy()
    expect(screen.getByTestId('termx-terminal-list-page').textContent).not.toMatch(/workspace|tab|window|pane|session/i)
    await waitFor(() => expect(sessions[0]?.disconnectCalls).toBe(1))

    expect(connect).toHaveBeenCalledTimes(2)
  })

  it('refreshes the terminal list when a machine-level inventory event arrives', async () => {
    let terminals: Array<{
      machineId: string
      terminalId: string
      title: string
      state: 'running'
      command: string
      cols: number
      rows: number
      cwd: string
      sizeLocked: boolean
      sizeLockMode: 'lock' | 'off'
      environment: string
    }> = [{
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
    }]
    const api = createMockLocalAgentApi()
    api.listTerminals = vi.fn(async () => terminals)

    let handler: ((event: { type: 'inventory_changed' }) => void) | null = null
    const inventoryEvents: TerminalInventoryEvents = {
      subscribe(machineId, next) {
        expect(machineId).toBe('machine-local')
        handler = next
        return {
          close() {
            handler = null
          },
        }
      },
    }

    render(<LocalRemoteApp api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} inventoryEvents={inventoryEvents} />)

    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
    await waitFor(() => expect(handler).toBeTruthy())
    expect(screen.queryByText('worker')).toBeNull()

    terminals = [
      ...terminals,
      {
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
      },
    ]
    const fireInventoryChanged = () => {
      if (!handler) throw new Error('inventory event handler was not installed')
      handler({ type: 'inventory_changed' })
    }
    fireInventoryChanged()

    await waitFor(() => expect(screen.getByText('worker')).toBeTruthy())
    expect(screen.getByText('/srv/worker')).toBeTruthy()
    expect(screen.getByText('prod')).toBeTruthy()
  })

  it('opens a terminal management sheet from a long-press style terminal-list gesture and can edit metadata', async () => {
    const api = createMockLocalAgentApi()
    const listTerminals = vi.fn(async () => [{
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
    }])
    api.listTerminals = listTerminals
    const managementSession = createMockLocalRemoteSession({
      set_metadata: {},
    }, 'machine-local', 'terminal-1')
    const connect = vi.fn(async () => managementSession)

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    fireEvent.contextMenu(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-actions-sheet')).toBeTruthy())

    await userEvent.click(within(screen.getByTestId('termx-terminal-actions-sheet')).getByRole('button', { name: /edit terminal/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-editor-sheet')).toBeTruthy())
    const editor = screen.getByTestId('termx-terminal-editor-sheet')
    const nameInput = within(editor).getByLabelText('Name')
    await userEvent.clear(nameInput)
    await userEvent.type(nameInput, 'renamed shell')
    const cwdInput = within(editor).getByLabelText('Working directory')
    await userEvent.clear(cwdInput)
    await userEvent.type(cwdInput, '/srv/app-next')
    const environmentInput = within(editor).getByLabelText('Environment')
    await userEvent.clear(environmentInput)
    await userEvent.type(environmentInput, 'staging')
    await userEvent.click(within(editor).getByRole('button', { name: /save changes/i }))

    await waitFor(() => expect(managementSession.requests).toContainEqual({
      method: 'set_metadata',
      path: 'set_metadata',
      params: {
        terminal_id: 'terminal-1',
        name: 'renamed shell',
        tags: {
          'termx.size_lock': 'lock',
          cwd: '/srv/app-next',
          environment: 'staging',
        },
      },
    }))
  })

  it('creates and deletes terminals from the list page management controls', async () => {
    const api = createMockLocalAgentApi()
    const managementSession = createMockLocalRemoteSession({
      create: { terminal_id: 'terminal-3', state: 'running' },
      remove: {},
    }, 'machine-local', 'terminal-1')
    const connect = vi.fn(async () => managementSession)

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /create terminal/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /create terminal/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-editor-sheet')).toBeTruthy())

    const editor = screen.getByTestId('termx-terminal-editor-sheet')
    await userEvent.clear(within(editor).getByLabelText('Name'))
    await userEvent.type(within(editor).getByLabelText('Name'), 'ops shell')
    await userEvent.clear(within(editor).getByLabelText('Command'))
    await userEvent.type(within(editor).getByLabelText('Command'), '/bin/zsh -l')
    await userEvent.clear(within(editor).getByLabelText('Working directory'))
    await userEvent.type(within(editor).getByLabelText('Working directory'), '/srv/app')
    await userEvent.clear(within(editor).getByLabelText('Environment'))
    await userEvent.type(within(editor).getByLabelText('Environment'), 'prod')
    await userEvent.click(within(editor).getByRole('button', { name: /create terminal/i }))

    await waitFor(() => expect(managementSession.requests).toContainEqual({
      method: 'create',
      path: 'create',
      params: {
        command: ['/bin/zsh', '-l'],
        name: 'ops shell',
        dir: '/srv/app',
        env: ['prod'],
        tags: {
          cwd: '/srv/app',
          environment: 'prod',
        },
      },
    }))

    fireEvent.contextMenu(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-actions-sheet')).toBeTruthy())
    await userEvent.click(within(screen.getByTestId('termx-terminal-actions-sheet')).getByRole('button', { name: /delete terminal/i }))
    await waitFor(() => expect(managementSession.requests).toContainEqual({
      method: 'remove',
      path: 'remove',
      params: { terminal_id: 'terminal-1' },
    }))
  })

  it('creates the first terminal through a machine-scoped runtime api session', async () => {
    const api = createMockLocalAgentApi()
    api.listTerminals = vi.fn(async () => [])
    const managementSession = createMockLocalRemoteSession({
      create: { terminal_id: 'terminal-1', state: 'running' },
    }, 'machine-local')
    const connect = vi.fn(async () => managementSession)

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /create terminal/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /create terminal/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-editor-sheet')).toBeTruthy())
    await userEvent.click(within(screen.getByTestId('termx-terminal-editor-sheet')).getByRole('button', { name: /create terminal/i }))

    await waitFor(() => expect(managementSession.requests).toContainEqual({
      method: 'create',
      path: 'create',
      params: {
        command: ['/bin/zsh', '-l'],
        tags: {},
      },
    }))
    expect(connect).toHaveBeenCalledWith({ machineId: 'machine-local' })
  })

  it('uses policy rather than local HTTP management methods to expose terminal management controls', async () => {
    const baseApi = createMockLocalAgentApi()
    const api = {
      getStatus: baseApi.getStatus,
      listTerminals: baseApi.listTerminals,
    }
    const managementSession = createMockLocalRemoteSession({
      create: { terminal_id: 'terminal-3', state: 'running' },
    }, 'machine-local', 'terminal-1')
    const connect = vi.fn(async () => managementSession)

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /create terminal/i })).toBeTruthy())
    fireEvent.contextMenu(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-actions-sheet')).toBeTruthy())
  })

  it('hides terminal management controls when policy denies them', async () => {
    const baseApi = createMockLocalAgentApi()
    const api = {
      getStatus: baseApi.getStatus,
      listTerminals: baseApi.listTerminals,
    }

    render(
      <LocalRemoteApp
        api={api}
        connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }}
        managementPolicy={{ terminalManagementAllowed: false, denialReason: 'upgrade required' }}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    expect(screen.queryByRole('button', { name: /create terminal/i })).toBeNull()
    fireEvent.contextMenu(screen.getByRole('button', { name: /open zsh/i }))
    expect(screen.queryByTestId('termx-terminal-actions-sheet')).toBeNull()
  })

  it('refreshes terminals from the list header action', async () => {
    const api = createMockLocalAgentApi()
    const getStatus = vi.fn(api.getStatus)
    const listTerminals = vi.fn(api.listTerminals)
    api.getStatus = getStatus
    api.listTerminals = listTerminals

    render(<LocalRemoteApp api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    const initialListCalls = listTerminals.mock.calls.length
    await userEvent.click(screen.getByRole('button', { name: /refresh terminals/i }))
    await waitFor(() => expect(listTerminals.mock.calls.length).toBeGreaterThan(initialListCalls))
  })

  it('keeps the app shell driven by LocalAgentApi and session interfaces only', () => {
    const connect = vi.fn(() => Promise.resolve(createMockLocalRemoteSession(
      {},
      'machine-local',
      'terminal-1',
    )))
    const connector = { connect } satisfies LocalRemoteSessionConnector
    const props = {
      api: createMockLocalAgentApi(),
      connector,
    }

    expect(Object.keys(props)).not.toContain('rtcPeerConnection')
    expect(Object.keys(props)).not.toContain('nativePlugin')
    expect(Object.keys(props)).not.toContain('relayCredentials')
  })

  it('renders local session setup errors instead of crashing the embedded shell', async () => {
    const connect = vi.fn(() => Promise.reject(new Error('session token is required before opening a terminal')))
    const connector = { connect } satisfies LocalRemoteSessionConnector

    render(<LocalRemoteApp api={createMockLocalAgentApi()} connector={connector} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(connect).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('session token is required'))
    expect(connect).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane/i)
  })

  it('keeps the local pair harness reachable through app-level interfaces', async () => {
    const connect = vi.fn(() => Promise.resolve(createMockLocalRemoteSession(
      {},
      'machine-local',
      'terminal-1',
    )))
    const connector = { connect } satisfies LocalRemoteSessionConnector

    render(
      <LocalRemoteApp
        api={createMockLocalAgentApi()}
        connector={connector}
        pair={{
          api: createMockPairApi(),
          sessionStore: createMachineSessionStore(new MemoryStorage()),
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(screen.getByTestId('termx-verification-gate')).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: /verify device/i }))
    await waitFor(() => expect(screen.getByTestId('termx-local-pair-panel')).toBeTruthy())
    expect(screen.getByLabelText('Pair ID')).toBeTruthy()
    expect(screen.getByLabelText('Pair secret')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|session/i)
  })

  it('keeps pair reachable when first-run terminal connect needs a token and retries after pairing', async () => {
    const sessionStore = createMachineSessionStore(new MemoryStorage())
    let paired = false
    const connect = vi.fn(() => {
      if (!paired) return Promise.reject(new Error('session token is required before opening a terminal'))
      return Promise.resolve(createMockLocalRemoteSession({}, 'machine-local', 'terminal-1'))
    })
    const connector = { connect } satisfies LocalRemoteSessionConnector
    const pairApi = createMockPairApi()
    pairApi.pair = vi.fn(async () => {
      paired = true
      return {
        machineId: 'machine-local',
        sessionToken: 'session-token-local',
        expiresAt: '2026-05-01T07:00:00Z',
      }
    })

    render(
      <LocalRemoteApp
        api={createMockLocalAgentApi()}
        connector={connector}
        pair={{
          api: pairApi,
          sessionStore,
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(connect).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-pair-sheet')).toBeTruthy())
    expect(screen.getByTestId('termx-local-pair-panel')).toBeTruthy()

    await userEvent.type(screen.getByLabelText('Pair ID'), 'pair-1')
    await userEvent.type(screen.getByLabelText('Pair secret'), 'secret-1')
    await userEvent.click(within(screen.getByTestId('termx-pair-sheet')).getByRole('button', { name: /^pair device$/i }))

    await waitFor(() => expect(screen.getByText('Paired with machine-local')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    expect(connect).toHaveBeenCalledTimes(1)
    expect(sessionStore.getSessionToken('machine-local')).toBe('session-token-local')
  })
})

function trackSession<T extends ReturnType<typeof createMockLocalRemoteSession>>(sessions: T[], session: T): T {
  sessions.push(session)
  return session
}

function createMockLocalRemoteSession(
  responders: Parameters<typeof createMockFileSession>[0],
  machineId: string,
  terminalId?: string,
): ReturnType<typeof createMockFileSession> & RtcSession & {
  disconnectCalls: number
} {
  const session = createMockFileSession(
    responders,
    {},
    terminalId === undefined ? { machineId } : { machineId, terminalId },
  )
  return Object.assign(session, {
    disconnectCalls: 0,
    async disconnect() {
      this.disconnectCalls += 1
    },
    async openTerminal() {
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

function createMockLocalAgentApi() {
  return {
    async getStatus(): Promise<LocalStatus> {
      return {
        machine: {
          machineId: 'machine-local',
          name: 'Local Mac',
          state: 'online',
          terminalCount: 1,
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

function createMockPairApi(): LocalPairingApi {
  return {
    async pair() {
      throw new Error('pair is not used by this test')
    },
  }
}

class MemoryStorage implements Storage {
  private readonly values = new Map<string, string>()
  length = 0

  clear(): void {
    this.values.clear()
    this.length = 0
  }

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  key(index: number): string | null {
    return Array.from(this.values.keys())[index] ?? null
  }

  removeItem(key: string): void {
    this.values.delete(key)
    this.length = this.values.size
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
    this.length = this.values.size
  }
}
