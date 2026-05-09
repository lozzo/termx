import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, useImperativeHandle } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LocalRemoteApp, type LocalRemoteSessionConnector } from './LocalRemoteApp'
import { createMachineSessionStore } from './localAppIdentity'
import type { TerminalModifierState } from './mobileTerminalInput'
import type { TerminalResizeControl } from './terminalClient'
import type { LocalPairingApi, LocalStatus, MachineConnectionStateEvents, RtcConnectionStateSnapshot, RtcSession, TerminalInventoryEvents } from './transport'
import { createMockFileSession } from './test/mockFileSession'
import type { TerminalHandle } from './Terminal'
import type { Terminal } from './model'

const terminalReattachMock = vi.fn()

vi.mock('./Terminal', () => ({
  Terminal: forwardRef<TerminalHandle, {
    machineId: string
    terminalId: string
    modifierState?: TerminalModifierState
    onResizeControl?: (control: TerminalResizeControl) => void
    selectionMode?: boolean
  }>(function MockTerminal(
    { machineId, terminalId, modifierState, onResizeControl, selectionMode },
    ref,
  ) {
    const fit = vi.fn()
    const focus = vi.fn()
    useImperativeHandle(ref, () => ({
      sendInput: vi.fn(),
      sendResize: vi.fn(),
      reattach: terminalReattachMock,
      focus,
      blur: vi.fn(),
      fit,
      pasteText: vi.fn(),
      selectAll: vi.fn(),
      selectVisible: vi.fn(),
      getSelection: vi.fn(() => ''),
      hasSelection: vi.fn(() => false),
      clearSelection: vi.fn(),
      getCursorInfo: vi.fn(() => null),
      adjustInputPosition: vi.fn(),
      getBufferType: vi.fn(() => 'normal' as const),
      updateOptions: vi.fn(),
    }))
    return (
      <section
        data-machine-id={machineId}
        data-modifier-state={`${modifierState?.ctrl ?? 'off'}:${modifierState?.alt ?? 'off'}`}
        data-selection-mode={selectionMode ? 'true' : 'false'}
        data-terminal-id={terminalId}
        data-testid="termx-terminal"
      >
        <button
          type="button"
          onClick={() => onResizeControl?.({ canResize: false, reason: 'size_locked', sizeLocked: true })}
        >
          Emit size lock
        </button>
      </section>
    )
  }),
}))

vi.mock('./FileManager', async () => {
  const React = await import('react')
  return {
    FileManager: ({ machineId, terminalId, initialPath }: { machineId: string; terminalId?: string; initialPath?: string }) => {
      const [currentPath, setCurrentPath] = React.useState(initialPath ?? '')
      return (
        <section
          data-current-path={currentPath}
          data-initial-path={initialPath ?? ''}
          data-machine-id={machineId}
          data-terminal-id={terminalId ?? ''}
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
    terminalReattachMock.mockReset()
    window.localStorage?.clear?.()
    vi.unstubAllGlobals()
    cleanup()
  })

  it('loads local machine terminals and composes shared terminal/file manager components', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({
          '/files/list': { path: '/', parent: '', total: 0, entries: [] },
        }, machineId))),
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
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/Users/lozzow/project')
    expect(connect).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-local',
    }), expect.objectContaining({ forceRelay: false }))
    expect(connect).toHaveBeenCalledTimes(1)
    expect(sessions[0]).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))
    expect(screen.getByTestId('termx-terminal')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|session/i)
  })

  it('opens terminal files from the terminal cwd and resets when the file context terminal changes', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({
        '/files/list': { path: '/', parent: '', total: 0, entries: [] },
      }, machineId))),
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
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/tmp'))

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/Users/lozzow/project'))

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))
    await userEvent.click(screen.getByRole('button', { name: /open worker/i }))
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('terminal-2'))
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-initial-path')).toBe('/srv/worker')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/srv/worker')
  })

  it('opens machine files from root when no terminal is available', async () => {
    const api = createMockLocalAgentApi({ terminals: [] })
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({
        '/files/list': { path: '/', parent: '', total: 0, entries: [] },
      }, machineId))),
    )

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))

    await waitFor(() => expect(screen.getByTestId('termx-file-manager')).toBeTruthy())
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-initial-path')).toBe('/')
    expect(screen.queryByText('No terminal is available for local file access')).toBeNull()
    expect(connect).toHaveBeenCalledWith(expect.objectContaining({ machineId: 'machine-local' }), expect.objectContaining({ forceRelay: false }))
  })

  it('passes the saved relay preference into initial terminal inventory loading', async () => {
    vi.stubGlobal('localStorage', createMemoryStorage({
      'termx.forceRelay.machine-local': '1',
    }))
    const api = createMockLocalAgentApi()
    const listTerminals = vi.fn(api.listTerminals)
    api.listTerminals = listTerminals

    render(<LocalRemoteApp api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(listTerminals).toHaveBeenCalledWith(expect.objectContaining({ forceRelay: true }))
  })

  it('uses a terminal-first mobile shell with sheets instead of a Config sidebar', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({
        '/files/list': { path: '/', parent: '', total: 0, entries: [] },
      }, machineId))),
    )
    const connector = { connect } satisfies LocalRemoteSessionConnector
    const pairApi = createMockPairApi()
    pairApi.pair = vi.fn(async () => ({
      machineId: 'machine-local',
      sessionToken: 'session-token-local',
      expiresAt: '2099-05-01T07:00:00Z',
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
    await waitFor(() => expect(sessions[0]?.disconnectCalls).toBe(0))

    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('keeps the terminal RTC session when returning to the list and reuses it for the same terminal', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }, _options?: unknown) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({}, machineId))),
    )

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1'))
    expect(connect).toHaveBeenCalledTimes(1)

    await userEvent.click(screen.getByRole('button', { name: /show terminal list/i }))
    expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy()
    expect(sessions[0]?.disconnectCalls).toBe(0)

    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1'))
    expect(connect).toHaveBeenCalledTimes(1)
    expect(sessions[0]?.disconnectCalls).toBe(0)
  })

  it('keeps the machine RTC session when switching to a different terminal', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }, _options?: unknown) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({}, machineId))),
    )

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1'))

    await userEvent.click(screen.getByRole('button', { name: /open worker/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-2'))

    expect(connect).toHaveBeenCalledTimes(1)
    expect(sessions[0]?.disconnectCalls).toBe(0)
  })

  it('toggles forced relay back to an explicit P2P connection attempt', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }, _options) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({}, machineId))),
    )

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    const p2pSession = sessions[0]
    expect(p2pSession).toBeTruthy()
    if (!p2pSession) throw new Error('p2p session was not created')
    p2pSession.getConnectionInfo = vi.fn(async () => ({
      path: 'public_p2p' as const,
      connectionId: 'p2p-connection',
      machineId: 'machine-local',
      relayInUse: false,
      type: 'p2p' as const,
    }))
    await userEvent.click(screen.getByRole('button', { name: /connection info/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /use relay/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /use relay/i }))

    await waitFor(() => expect(connect).toHaveBeenCalledTimes(2))
    expect(connect.mock.calls[1]?.[1]).toEqual(expect.objectContaining({ forceRelay: true }))
    const relaySession = sessions[1]
    expect(relaySession).toBeTruthy()
    if (!relaySession) throw new Error('relay session was not created')
    relaySession.getConnectionInfo = vi.fn(async () => ({
      path: 'managed' as const,
      connectionId: 'relay-connection',
      machineId: 'machine-local',
      relayInUse: true,
      type: 'relay' as const,
    }))

    await userEvent.click(screen.getByRole('button', { name: /connection info/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /try p2p/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /try p2p/i }))

    await waitFor(() => expect(connect).toHaveBeenCalledTimes(3))
    expect(connect.mock.calls[2]?.[1]).toEqual(expect.objectContaining({ forceRelay: false }))
  })

  it('shows a single non-blocking reconnect banner while connection state is active', async () => {
    const api = createMockLocalAgentApi()
    let capturedStatus: ((status: string) => void) | undefined
    const connect = vi.fn(({ machineId }: { machineId: string }, options?: { onStatus?: (status: string) => void }) => {
      capturedStatus = options?.onStatus
      return Promise.resolve(createMockLocalRemoteSession({}, machineId))
    })

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    capturedStatus?.('Reconnecting...')

    await waitFor(() => expect(screen.getByTestId('termx-connection-progress-banner')).toBeTruthy())
    expect(screen.getByTestId('termx-connection-progress-banner').textContent).toContain('Reconnecting')
    expect(screen.queryByText('Connecting terminal...')).toBeNull()
  })

  it('reacquires and reattaches a terminal lease when the app-level connection reconnects', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({}, machineId))),
    )
    const connectionHandlerRef: { current: ((snapshot: RtcConnectionStateSnapshot) => void) | null } = { current: null }
    const connectionStateEvents: MachineConnectionStateEvents = {
      subscribe(machineId, handler) {
        if (machineId === 'machine-local') connectionHandlerRef.current = handler
        return {
          close() {
            if (connectionHandlerRef.current === handler) connectionHandlerRef.current = null
          },
        }
      },
    }

    render(<LocalRemoteApp api={api} connector={{ connect }} connectionStateEvents={connectionStateEvents} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    expect(connect).toHaveBeenCalledTimes(1)
    const emitConnectionState = connectionHandlerRef.current
    if (!emitConnectionState) throw new Error('connection state handler was not installed')

    await sessions[0]?.disconnect()
    emitConnectionState({
      machineId: 'machine-local',
      phase: 'connected',
      path: 'managed',
      statusText: 'Connected',
      relayInUse: false,
    })

    await waitFor(() => expect(connect).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(terminalReattachMock).toHaveBeenCalledWith(sessions[1]))
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('reattaches the active terminal when a preserved app-level connection recovers', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({}, machineId))),
    )
    const connectionHandlerRef: { current: ((snapshot: RtcConnectionStateSnapshot) => void) | null } = { current: null }
    const connectionStateEvents: MachineConnectionStateEvents = {
      subscribe(machineId, handler) {
        if (machineId === 'machine-local') connectionHandlerRef.current = handler
        return {
          close() {
            if (connectionHandlerRef.current === handler) connectionHandlerRef.current = null
          },
        }
      },
    }

    render(<LocalRemoteApp api={api} connector={{ connect }} connectionStateEvents={connectionStateEvents} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    expect(connect).toHaveBeenCalledTimes(1)
    const emitConnectionState = connectionHandlerRef.current
    if (!emitConnectionState) throw new Error('connection state handler was not installed')

    emitConnectionState({
      machineId: 'machine-local',
      phase: 'verifying',
      path: 'managed',
      statusText: 'App resumed, verifying connection...',
      relayInUse: false,
    })
    await waitFor(() => expect(screen.getByTestId('termx-connection-progress-banner').textContent).toContain('verifying'))

    emitConnectionState({
      machineId: 'machine-local',
      phase: 'connected',
      path: 'managed',
      statusText: 'Connected',
      relayInUse: false,
    })

    await waitFor(() => expect(terminalReattachMock).toHaveBeenCalledWith(sessions[0]))
    expect(connect).toHaveBeenCalledTimes(1)
    expect(sessions[0]?.disconnectCalls).toBe(0)
  })

  it('reconnects from connection info without changing the current transport mode', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }, _options?: unknown) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({}, machineId))),
    )

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    await userEvent.click(screen.getByRole('button', { name: /connection info/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /^reconnect$/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /^reconnect$/i }))

    await waitFor(() => expect(connect).toHaveBeenCalledTimes(2))
    expect(connect.mock.calls[1]?.[1]).toEqual(expect.objectContaining({ forceRelay: false }))
    expect(sessions[0]?.disconnectCalls).toBe(1)
  })

  it('opens a split terminal view from the existing machine RTC session', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockLocalRemoteSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockLocalRemoteSession({}, machineId))),
    )

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1'))

    await userEvent.click(screen.getByRole('button', { name: /split terminal/i }))
    await waitFor(() => expect(screen.getByTestId('termx-split-terminal-sheet')).toBeTruthy())
    await userEvent.click(within(screen.getByTestId('termx-split-terminal-sheet')).getByRole('button', { name: /open worker/i }))

    await waitFor(() => expect(screen.getAllByTestId('termx-terminal')).toHaveLength(2))
    expect(screen.getAllByTestId('termx-terminal').map((node) => node.getAttribute('data-terminal-id'))).toEqual([
      'terminal-1',
      'terminal-2',
    ])
    expect(screen.getByTestId('termx-terminal-panel').getAttribute('data-active-slot')).toBe('false')
    expect(screen.getByTestId('termx-split-terminal-panel').getAttribute('data-active-slot')).toBe('true')
    expect(connect).toHaveBeenCalledTimes(1)
    expect(sessions[0]?.disconnectCalls).toBe(0)

    await userEvent.click(screen.getByRole('button', { name: /enable synchronized input/i }))
    expect(screen.getByRole('button', { name: /disable synchronized input/i })).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /close split terminal/i }))
    await waitFor(() => expect(screen.queryByTestId('termx-split-terminal-panel')).toBeNull())
    expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1')
    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('shows a resize unlock action when attach reports a size lock and clears the lock through management api', async () => {
    const api = createMockLocalAgentApi()
    const listTerminals = vi.fn(api.listTerminals)
    api.listTerminals = listTerminals
    const managementSession = createMockLocalRemoteSession({
      set_metadata: {},
    }, 'machine-local')
    const connect = vi.fn(async () => managementSession)

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1'))

    await userEvent.click(within(screen.getByTestId('termx-terminal')).getByRole('button', { name: /emit size lock/i }))
    const unlock = await screen.findByRole('button', { name: /unlock terminal resize/i })
    await userEvent.click(unlock)

    await waitFor(() => expect(managementSession.requests).toContainEqual({
      method: 'set_metadata',
      path: 'set_metadata',
      params: {
        terminal_id: 'terminal-1',
        tags: { 'termx.size_lock': 'off' },
      },
    }))
    expect(listTerminals.mock.calls.length).toBeGreaterThan(1)
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

  it('refreshes the terminal list when a runtime inventory event arrives', async () => {
    let terminals = [terminalFixture({
      terminalId: 'terminal-1',
      title: 'zsh',
      command: '/bin/zsh',
      cols: 120,
      rows: 36,
      cwd: '/Users/lozzow/project',
    })]
    const api = createMockLocalAgentApi()
    api.listTerminals = vi.fn(async () => terminals)
    let runtimeHandler: ((event: { type: string; payload?: unknown }) => void) | null = null
    const runtimeSession = createMockLocalRemoteSession({}, 'machine-local')
    runtimeSession.subscribeEvents = vi.fn((handler) => {
      runtimeHandler = handler
      return {
        close() {
          runtimeHandler = null
        },
      }
    })
    const connect = vi.fn(async () => runtimeSession)

    render(<LocalRemoteApp api={api} connector={{ connect }} subscribeRuntimeInventoryEvents />)

    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
    await waitFor(() => expect(runtimeHandler).toBeTruthy())
    expect(connect).toHaveBeenCalledTimes(1)

    terminals = [
      ...terminals,
      terminalFixture({
        terminalId: 'terminal-3',
        title: 'ci shell',
        command: '/usr/bin/env bash',
        cwd: '/srv/ci',
      }),
    ]
    runtimeHandler!({ type: 'inventory_changed', payload: { terminalId: 'terminal-3' } })

    await waitFor(() => expect(screen.getByText('ci shell')).toBeTruthy())
    expect(screen.getByText('/srv/ci')).toBeTruthy()
  })

  it('ignores stale terminal list refreshes that finish after a newer refresh', async () => {
    const api = createMockLocalAgentApi()
    const resolvers: Array<(terminals: Terminal[]) => void> = []
    api.listTerminals = vi.fn(() => new Promise<Terminal[]>((resolve) => {
      resolvers.push(resolve)
    }))

    let handler: ((event: { type: 'inventory_changed' }) => void) | null = null
    const inventoryEvents: TerminalInventoryEvents = {
      subscribe(_machineId, next) {
        handler = next
        return {
          close() {
            handler = null
          },
        }
      },
    }

    render(<LocalRemoteApp api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} inventoryEvents={inventoryEvents} />)

    await waitFor(() => expect(resolvers).toHaveLength(1))
    resolvers[0]!([terminalFixture({ terminalId: '1', title: 'first' })])
    await waitFor(() => expect(screen.getByText('first')).toBeTruthy())
    await waitFor(() => expect(handler).toBeTruthy())

    handler!({ type: 'inventory_changed' })
    await waitFor(() => expect(resolvers).toHaveLength(2))
    handler!({ type: 'inventory_changed' })
    await waitFor(() => expect(resolvers).toHaveLength(3))

    resolvers[2]!([terminalFixture({ terminalId: '2', title: 'newest' })])
    await waitFor(() => expect(screen.getByText('newest')).toBeTruthy())
    resolvers[1]!([terminalFixture({ terminalId: '1', title: 'stale' })])

    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(screen.getByText('newest')).toBeTruthy()
    expect(screen.queryByText('stale')).toBeNull()
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
    }, 'machine-local')
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
    }, 'machine-local')
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
          'termx.size_lock': 'off',
          cwd: '/srv/app',
          environment: 'prod',
        },
      },
    }))

    await userEvent.click(screen.getByRole('button', { name: /manage zsh/i }))
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
        tags: { 'termx.size_lock': 'off' },
      },
    }))
    expect(connect).toHaveBeenCalledWith({ machineId: 'machine-local' }, expect.objectContaining({ forceRelay: false }))
  })

  it('uses policy rather than local HTTP management methods to expose terminal management controls', async () => {
    const baseApi = createMockLocalAgentApi()
    const api = {
      getStatus: baseApi.getStatus,
      listTerminals: baseApi.listTerminals,
    }
    const managementSession = createMockLocalRemoteSession({
      create: { terminal_id: 'terminal-3', state: 'running' },
    }, 'machine-local')
    const connect = vi.fn(async () => managementSession)

    render(<LocalRemoteApp api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /create terminal/i })).toBeTruthy())
    fireEvent.contextMenu(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-actions-sheet')).toBeTruthy())
  })

  it('shows terminal management controls without a local capability policy', async () => {
    const baseApi = createMockLocalAgentApi()
    const api = {
      getStatus: baseApi.getStatus,
      listTerminals: baseApi.listTerminals,
    }

    render(
      <LocalRemoteApp
        api={api}
        connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    expect(screen.getByRole('button', { name: /create terminal/i })).toBeTruthy()
    fireEvent.contextMenu(screen.getByRole('button', { name: /open zsh/i }))
    expect(screen.getByTestId('termx-terminal-actions-sheet')).toBeTruthy()
  })

  it('does not expose manual terminal refresh from the list header', async () => {
    const api = createMockLocalAgentApi()
    const listTerminals = vi.fn(api.listTerminals)
    api.listTerminals = listTerminals

    render(<LocalRemoteApp api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await waitFor(() => expect(listTerminals).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('button', { name: /refresh terminals/i })).toBeNull()
  })

  it('keeps the app shell driven by LocalAgentApi and session interfaces only', () => {
    const connect = vi.fn(() => Promise.resolve(createMockLocalRemoteSession(
      {},
      'machine-local',
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
      return Promise.resolve(createMockLocalRemoteSession({}, 'machine-local'))
    })
    const connector = { connect } satisfies LocalRemoteSessionConnector
    const pairApi = createMockPairApi()
    pairApi.pair = vi.fn(async () => {
      paired = true
      return {
        machineId: 'machine-local',
        sessionToken: 'session-token-local',
        expiresAt: '2099-05-01T07:00:00Z',
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

function createMemoryStorage(initial: Record<string, string> = {}): Storage {
  const values = new Map(Object.entries(initial))
  return {
    get length() {
      return values.size
    },
    clear() {
      values.clear()
    },
    getItem(key: string) {
      return values.get(key) ?? null
    },
    key(index: number) {
      return Array.from(values.keys())[index] ?? null
    },
    removeItem(key: string) {
      values.delete(key)
    },
    setItem(key: string, value: string) {
      values.set(key, value)
    },
  }
}

function createMockLocalRemoteSession(
  responders: Parameters<typeof createMockFileSession>[0],
  machineId: string,
  terminalId?: string,
): ReturnType<typeof createMockFileSession> & RtcSession & {
  disconnectCalls: number
  isAlive(): boolean
} {
  const session = createMockFileSession(
    responders,
    {},
    terminalId === undefined ? { machineId } : { machineId, terminalId },
  )
  let alive = true
  return Object.assign(session, {
    disconnectCalls: 0,
    isAlive() {
      return alive
    },
    async disconnect() {
      alive = false
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

function createMockLocalAgentApi(options: { terminals?: Terminal[] } = {}) {
  const terminals = options.terminals ?? [{
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
  return {
    async getStatus(): Promise<LocalStatus> {
      return {
        machine: {
          machineId: 'machine-local',
          name: 'Local Mac',
          state: 'online',
          terminalCount: terminals.length,
          localRTC: { signalingUrl: 'http://127.0.0.1:18888' },
        },
        localWeb: {
          httpUrl: 'http://127.0.0.1:18888',
          rtcOfferUrl: 'http://127.0.0.1:18888',
        },
      }
    },
    async listTerminals(): Promise<Terminal[]> {
      return terminals
    },
  }
}

function terminalFixture(overrides: Partial<Terminal>): Terminal {
  return {
    machineId: overrides.machineId ?? 'machine-local',
    terminalId: overrides.terminalId ?? 'terminal-1',
    title: overrides.title ?? 'zsh',
    state: overrides.state ?? 'running',
    command: overrides.command,
    cols: overrides.cols,
    rows: overrides.rows,
    cwd: overrides.cwd,
    sizeLocked: overrides.sizeLocked,
    sizeLockMode: overrides.sizeLockMode,
    environment: overrides.environment,
    lastActiveAt: overrides.lastActiveAt,
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
