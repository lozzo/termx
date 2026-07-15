import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, useImperativeHandle } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConnectionInfoDialog, MachineWorkspace, type MachineWorkspaceConnector } from './MachineWorkspace'
import { createMachineSessionStore } from '../state/localAppIdentity'
import type { TerminalModifierState } from '../terminal/mobileTerminalInput'
import type { TerminalResizeControl } from '../terminal/terminalClient'
import type { LocalPairingApi, LocalStatus, MachineConnectionStateEvents, RtcConnectionStateSnapshot, RtcSession, TerminalInventoryEvents } from '../core/transport'
import { createMockFileSession } from '../test/mockFileSession'
import type { TerminalHandle } from '../terminal/Terminal'
import type { Terminal } from '../core/model'
import { DEFAULT_TERMINAL_SETTINGS } from '../terminal/terminalSettings'
import { dispatchNativeKeyboardEvent } from '../platform/nativeKeyboard'

const terminalReattachMock = vi.fn()
const terminalHandleMocks = vi.hoisted(() => ({
  handles: new Map<string, {
    sendInput: ReturnType<typeof vi.fn>
    pasteText: ReturnType<typeof vi.fn>
    fit: ReturnType<typeof vi.fn>
    focus: ReturnType<typeof vi.fn>
    adjustInputPosition: ReturnType<typeof vi.fn>
    emitBufferChange: (isAlternate: boolean) => void
  }>(),
  allHandles: [] as Array<{
    terminalId: string
    sendInput: ReturnType<typeof vi.fn>
    pasteText: ReturnType<typeof vi.fn>
    fit: ReturnType<typeof vi.fn>
    focus: ReturnType<typeof vi.fn>
    adjustInputPosition: ReturnType<typeof vi.fn>
    emitBufferChange: (isAlternate: boolean) => void
  }>,
}))

async function clickTerminalMenuAction(name: RegExp): Promise<void> {
  if (!screen.queryByTestId('termx-terminal-menu-sheet')) {
    await userEvent.click(screen.getByRole('button', { name: /open terminal menu/i }))
  }
  await userEvent.click(within(screen.getByTestId('termx-terminal-menu-sheet')).getByRole('button', { name }))
}

vi.mock('../terminal/Terminal', () => ({
  Terminal: forwardRef<TerminalHandle, {
    machineId: string
    terminalId: string
    onInput?: (data: string) => void
    onBufferChange?: (isAlternate: boolean) => void
    modifierState?: TerminalModifierState
    onResizeControl?: (control: TerminalResizeControl) => void
    selectionMode?: boolean
  }>(function MockTerminal(
    { machineId, terminalId, onInput, onBufferChange, modifierState, onResizeControl, selectionMode },
    ref,
  ) {
    const fit = vi.fn()
    const focus = vi.fn()
    const adjustInputPosition = vi.fn()
    const sendInput = vi.fn()
    const pasteText = vi.fn()
    const getBufferType = vi.fn<() => 'normal' | 'alternate'>(() => 'normal')
    const emitBufferChange = (isAlternate: boolean) => {
      getBufferType.mockReturnValue(isAlternate ? 'alternate' : 'normal')
      onBufferChange?.(isAlternate)
    }
    const handle = { terminalId, sendInput, pasteText, fit, focus, adjustInputPosition, emitBufferChange }
    terminalHandleMocks.handles.set(terminalId, handle)
    terminalHandleMocks.allHandles.push(handle)
    useImperativeHandle(ref, () => ({
      sendInput,
      sendResize: vi.fn(),
      requestResizeOwner: vi.fn(async () => ({ canResize: true, reason: 'owner' as const })),
      releaseResizeOwner: vi.fn(async () => ({ canResize: false, reason: 'follower' as const })),
      reattach: terminalReattachMock,
      focus,
      blur: vi.fn(),
      fit,
      pasteText,
      selectAll: vi.fn(),
      selectVisible: vi.fn(),
      getSelection: vi.fn(() => ''),
      hasSelection: vi.fn(() => false),
      clearSelection: vi.fn(),
      getCursorInfo: vi.fn(() => null),
      adjustInputPosition,
      getBufferType,
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
        <button type="button" onClick={() => onInput?.('typed\n')}>Type through xterm</button>
      </section>
    )
  }),
}))

vi.mock('../files/FileManager', async () => {
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

describe('MachineWorkspace', () => {
  it('allows an explicit relay attempt after direct connection info is unavailable', async () => {
    const onToggleMode = vi.fn()
    render(
      <ConnectionInfoDialog
        info={null}
        loading={false}
        error="route_unavailable"
        forceRelayActive={false}
        onClose={vi.fn()}
        onRefresh={vi.fn()}
        onReconnect={vi.fn()}
        onToggleMode={onToggleMode}
      />,
    )

    const useRelay = screen.getByRole('button', { name: /use relay/i })
    expect(useRelay.hasAttribute('disabled')).toBe(false)
    await userEvent.click(useRelay)
    expect(onToggleMode).toHaveBeenCalledTimes(1)
  })

  afterEach(() => {
    terminalReattachMock.mockReset()
    terminalHandleMocks.handles.clear()
    terminalHandleMocks.allHandles.length = 0
    window.localStorage?.clear?.()
    vi.unstubAllGlobals()
    cleanup()
  })

  it('loads local machine terminals and composes shared terminal/file manager components', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({
          'file.list': { path: '/', entries: [], next_cursor: '' },
        }, machineId))),
    )
    const connector = { connect } satisfies MachineWorkspaceConnector

    render(<MachineWorkspace api={api} connector={connector} />)

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
    await clickTerminalMenuAction(/files/i)
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
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({
        'file.list': { path: '/', entries: [], next_cursor: '' },
      }, machineId))),
    )
    const connector = { connect } satisfies MachineWorkspaceConnector

    render(<MachineWorkspace api={api} connector={connector} />)

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
    await clickTerminalMenuAction(/files/i)
    await waitFor(() => expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/Users/lozzow/project'))

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').className).toMatch(/invisible/))
    await userEvent.click(screen.getByRole('button', { name: /open worker/i }))
    await clickTerminalMenuAction(/files/i)
    await waitFor(() => expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('terminal-2'))
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-initial-path')).toBe('/srv/worker')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/srv/worker')
  })

  it('opens machine files from root when no terminal is available', async () => {
    const api = createMockLocalAgentApi({ terminals: [] })
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({
        'file.list': { path: '/', entries: [], next_cursor: '' },
      }, machineId))),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))

    await waitFor(() => expect(screen.getByTestId('termx-file-manager')).toBeTruthy())
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-initial-path')).toBe('/')
    expect(screen.queryByText('No terminal is available for local file access')).toBeNull()
    expect(connect).toHaveBeenCalledWith(expect.objectContaining({ machineId: 'machine-local' }), expect.objectContaining({ forceRelay: false }))
  })

  it('ignores the removed force-relay preference and starts managed endpoints in auto mode', async () => {
    vi.stubGlobal('localStorage', createMemoryStorage({
      'termx.forceRelay.machine-local': '1',
    }))
    const api = createMockLocalAgentApi()
    const listTerminals = vi.fn(api.listTerminals)
    api.listTerminals = listTerminals

    render(<MachineWorkspace api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(listTerminals).toHaveBeenCalledWith(expect.objectContaining({ forceRelay: false }))
  })

  it('uses a terminal-first mobile shell with sheets instead of a Config sidebar', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({
        'file.list': { path: '/', entries: [], next_cursor: '' },
      }, machineId))),
    )
    const connector = { connect } satisfies MachineWorkspaceConnector
    const pairApi = createMockPairApi()
    pairApi.pair = vi.fn(async () => ({
      machineId: 'machine-local',
      sessionToken: 'session-token-local',
      expiresAt: '2099-05-01T07:00:00Z',
    }))
    const storage = new MemoryStorage()

    render(
      <MachineWorkspace
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
    await waitFor(() => expect(screen.getAllByTestId('termx-pair-sheet').length).toBeGreaterThan(0))
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

    await clickTerminalMenuAction(/files/i)
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

  it('does not force a viewport height when embedded inside another flex shell', async () => {
    const api = createMockLocalAgentApi()
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(createMockMachineWorkspaceSession({}, machineId)),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} className="min-h-0 flex-1" />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    const shell = screen.getByTestId('termx-terminal-page').parentElement
    expect(shell?.className).toContain('h-full')
    expect(shell?.className).toContain('min-h-0')
    expect(shell?.className).not.toContain('h-[100dvh]')
  })

  it('lays out mobile terminal chrome as header, terminal body, and keybar siblings', async () => {
    const api = createMockLocalAgentApi()
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(createMockMachineWorkspaceSession({}, machineId)),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    const page = screen.getByTestId('termx-terminal-page')
    const header = screen.getByTestId('termx-terminal-header')
    const body = screen.getByTestId('termx-terminal-body')
    const keybar = screen.getByTestId('termx-mobile-keybar')

    expect(header.parentElement).toBe(page)
    expect(body.parentElement).toBe(page)
    expect(keybar.parentElement).toBe(page)
    expect(Array.from(page.children).slice(0, 3)).toEqual([header, body, keybar])
    expect(page.className).toContain('grid-rows-[auto_minmax(0,1fr)_auto]')
    expect(page.className).toContain('min-h-0')
    expect(page.className).toContain('max-w-full')
    expect(header.className).toContain('shrink-0')
    expect(header.className).toContain('overflow-hidden')
    expect(header.className).toContain('row-start-1')
    // 移动端保留 header/body/keybar 三行；桌面端 header/keybar 隐藏后只保留 terminal 的 1fr 行。
    expect(body.className).toContain('row-start-2')
    expect(page.className).toContain('md:grid-rows-[minmax(0,1fr)]')
    expect(body.className).toContain('md:row-start-1')
    expect(body.className).toContain('h-full')
    expect(body.className).toContain('flex-1')
    expect(body.className).toContain('min-w-0')
    expect(keybar.className).toContain('row-start-3')
    expect(keybar.className).toContain('shrink-0')
    expect(keybar.className).toContain('max-w-full')
  })

  it('keeps terminal actions reachable from the touch-sized mobile tools sheet', async () => {
    const api = createMockLocalAgentApi()
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(createMockMachineWorkspaceSession({}, machineId)),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await userEvent.click(screen.getByRole('button', { name: /open terminal menu/i }))

    const sheet = screen.getByTestId('termx-terminal-menu-sheet')
    expect(sheet).toBeTruthy()
    expect(within(sheet).getByRole('button', { name: /split terminal/i })).toBeTruthy()
    expect(within(sheet).getByRole('button', { name: /control resize/i })).toBeTruthy()
    expect(within(sheet).getByRole('button', { name: /^terminal tools$/i })).toBeTruthy()
    expect(within(sheet).getByRole('button', { name: /connection/i })).toBeTruthy()
    expect(within(sheet).getByRole('button', { name: /files/i })).toBeTruthy()
  })

  it('keeps the terminal RTC session when returning to the list and reuses it for the same terminal', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }, _options?: unknown) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({}, machineId))),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

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
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }, _options?: unknown) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({}, machineId))),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1'))

    await userEvent.click(screen.getByRole('button', { name: /open worker/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-2'))

    expect(connect).toHaveBeenCalledTimes(1)
    expect(sessions[0]?.disconnectCalls).toBe(0)
  })

  it('offers a current-session relay fallback when a one-shot P2P probe fails', async () => {
    const storage = createMemoryStorage()
    vi.stubGlobal('localStorage', storage)
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }, _options) =>
      connect.mock.calls.length === 3
        ? Promise.reject(new Error('route_unavailable'))
        : Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({}, machineId))),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    const p2pSession = sessions[0]
    expect(p2pSession).toBeTruthy()
    if (!p2pSession) throw new Error('p2p session was not created')
    p2pSession.getConnectionInfo = vi.fn(async () => ({
      path: 'hub' as const,
      connectionId: 'p2p-connection',
      machineId: 'machine-local',
      relayInUse: false,
      type: 'p2p' as const,
    }))
    await clickTerminalMenuAction(/connection/i)
    await waitFor(() => expect(screen.getByRole('button', { name: /use relay/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /use relay/i }))

    await waitFor(() => expect(connect).toHaveBeenCalledTimes(2))
    expect(connect.mock.calls[1]?.[1]).toEqual(expect.objectContaining({ forceRelay: true }))
    const relaySession = sessions[1]
    expect(relaySession).toBeTruthy()
    if (!relaySession) throw new Error('relay session was not created')
    relaySession.getConnectionInfo = vi.fn(async () => ({
      path: 'hub' as const,
      connectionId: 'relay-connection',
      machineId: 'machine-local',
      relayInUse: true,
      type: 'relay' as const,
    }))

    await clickTerminalMenuAction(/connection/i)
    await waitFor(() => expect(screen.getByRole('button', { name: /try p2p/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /try p2p/i }))

    await waitFor(() => expect(connect).toHaveBeenCalledTimes(3))
    expect(connect.mock.calls[2]?.[1]).toEqual(expect.objectContaining({ forceRelay: false }))
    await waitFor(() => expect(screen.getByRole('dialog', { name: /p2p unavailable/i })).toBeTruthy())
    expect(storage.getItem('termx.forceRelay.machine-local')).toBeNull()

    await userEvent.click(within(screen.getByRole('dialog', { name: /p2p unavailable/i })).getByRole('button', { name: /use relay/i }))
    await waitFor(() => expect(connect).toHaveBeenCalledTimes(4))
    expect(connect.mock.calls[3]?.[1]).toEqual(expect.objectContaining({ forceRelay: true }))
    expect(storage.getItem('termx.forceRelay.machine-local')).toBeNull()
  })

  it('shows a single non-blocking machine network overlay while connection state is active', async () => {
    const api = createMockLocalAgentApi()
    let capturedStatus: ((status: string) => void) | undefined
    const connect = vi.fn(({ machineId }: { machineId: string }, options?: { onStatus?: (status: string) => void }) => {
      capturedStatus = options?.onStatus
      return Promise.resolve(createMockMachineWorkspaceSession({}, machineId))
    })

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    capturedStatus?.('Reconnecting...')

    await waitFor(() => expect(screen.getByTestId('termx-machine-network-overlay')).toBeTruthy())
    expect(screen.getByTestId('termx-machine-network-overlay').textContent).toContain('Reconnecting')
    expect(screen.queryByText('Connecting terminal...')).toBeNull()

    await userEvent.click(screen.getByRole('button', { name: /back to terminal list/i }))

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    const overlays = screen.getAllByTestId('termx-machine-network-overlay')
    expect(overlays).toHaveLength(1)
    expect(overlays[0]?.textContent).toContain('Reconnecting')
  })

  it('reacquires and reattaches a terminal lease when the app-level connection reconnects', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({}, machineId))),
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

    render(<MachineWorkspace api={api} connector={{ connect }} connectionStateEvents={connectionStateEvents} />)

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
      path: 'hub',
      statusText: 'Connected',
      relayInUse: false,
    })

    await waitFor(() => expect(connect).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(terminalReattachMock).toHaveBeenCalledWith(sessions[1], { forceTerminalChannel: true }))
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('reattaches the active terminal when a preserved app-level connection recovers', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({}, machineId))),
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

    render(<MachineWorkspace api={api} connector={{ connect }} connectionStateEvents={connectionStateEvents} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    expect(connect).toHaveBeenCalledTimes(1)
    const emitConnectionState = connectionHandlerRef.current
    if (!emitConnectionState) throw new Error('connection state handler was not installed')

    emitConnectionState({
      machineId: 'machine-local',
      phase: 'verifying',
      path: 'hub',
      statusText: 'App resumed, verifying connection...',
      relayInUse: false,
    })
    await waitFor(() => expect(screen.getByTestId('termx-machine-network-overlay').textContent).toContain('verifying'))

    emitConnectionState({
      machineId: 'machine-local',
      phase: 'connected',
      path: 'hub',
      statusText: 'Connected',
      relayInUse: false,
    })

    await waitFor(() => expect(terminalReattachMock).toHaveBeenCalledWith(sessions[0], { forceTerminalChannel: true }))
    expect(connect).toHaveBeenCalledTimes(1)
    expect(sessions[0]?.disconnectCalls).toBe(0)
  })

  it('reattaches the active terminal on native app resume without reconnecting WebRTC', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({}, machineId))),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    expect(connect).toHaveBeenCalledTimes(1)

    fireEvent(document, new Event('termx:resume'))

    await waitFor(() => expect(terminalReattachMock).toHaveBeenCalledWith(sessions[0], { forceTerminalChannel: true }))
    expect(connect).toHaveBeenCalledTimes(1)
    expect(sessions[0]?.disconnectCalls).toBe(0)
  })

  it('clears mobile keyboard layout offsets on native app resume', async () => {
    vi.stubGlobal('innerHeight', 800)
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({}, machineId))),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} terminalSettings={{ ...DEFAULT_TERMINAL_SETTINGS, keyboardMode: 'shift' }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    const shell = screen.getByTestId('termx-terminal-page').parentElement as HTMLElement
    const wrapper = screen.getByTestId('termx-terminal-body').querySelector('[data-testid="termx-terminal-panel"]')?.parentElement as HTMLElement
    Object.defineProperty(screen.getByTestId('termx-terminal-body'), 'clientHeight', { configurable: true, value: 640 })

    act(() => {
      dispatchNativeKeyboardEvent({ visible: true, keyboardHeight: 260 })
      shell.style.height = '540px'
      wrapper.style.height = '640px'
      wrapper.style.transform = 'translateY(-120px)'
    })
    const terminalHandleBeforeResume = terminalHandleMocks.handles.get('terminal-1')

    fireEvent(document, new Event('termx:resume'))

    await waitFor(() => expect(shell.style.height).toBe(''))
    expect(wrapper.style.height).toBe('')
    expect(wrapper.style.transform).toBe('')
    expect(terminalHandleBeforeResume?.adjustInputPosition).toHaveBeenCalledWith(0)
  })

  it('highlights the keyboard button immediately after requesting the system keyboard', async () => {
    const api = createMockLocalAgentApi()
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(createMockMachineWorkspaceSession({}, machineId)),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    const keyboardButton = screen.getByRole('button', { name: /toggle system keyboard/i })
    expect(keyboardButton.getAttribute('aria-pressed')).toBe('false')
    expect(keyboardButton.className).not.toContain('bg-[var(--termx-accent)]')
    const terminalHandleBeforeFocus = terminalHandleMocks.handles.get('terminal-1')

    await userEvent.click(keyboardButton)

    await waitFor(() => expect(keyboardButton.getAttribute('aria-pressed')).toBe('true'))
    expect(keyboardButton.className).toContain('bg-[var(--termx-accent)]')
    expect(keyboardButton.className).toContain('text-[var(--termx-accent-text)]')
    expect(terminalHandleBeforeFocus?.focus).toHaveBeenCalled()

    act(() => {
      dispatchNativeKeyboardEvent({ visible: false })
    })

    await waitFor(() => expect(keyboardButton.getAttribute('aria-pressed')).toBe('false'))
    expect(keyboardButton.className).not.toContain('bg-[var(--termx-accent)]')
  })

  it('uses shift mode for normal-buffer auto keyboard layout', async () => {
    vi.stubGlobal('innerHeight', 800)
    const api = createMockLocalAgentApi()
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(createMockMachineWorkspaceSession({}, machineId)),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} terminalSettings={{ ...DEFAULT_TERMINAL_SETTINGS, keyboardMode: 'auto' }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    const shell = screen.getByTestId('termx-terminal-page').parentElement as HTMLElement
    const body = screen.getByTestId('termx-terminal-body')
    const wrapper = body.querySelector('[data-testid="termx-terminal-panel"]')?.parentElement as HTMLElement
    Object.defineProperty(body, 'clientHeight', { configurable: true, value: 640 })

    act(() => {
      dispatchNativeKeyboardEvent({ visible: true, keyboardHeight: 260 })
    })

    await waitFor(() => expect(shell.style.height).toBe('540px'))
    expect(wrapper.style.height).toBe('640px')
    expect(wrapper.style.transform).toBe('translateY(0px)')
  })

  it('uses resize mode for alternate-buffer auto keyboard layout', async () => {
    vi.stubGlobal('innerHeight', 800)
    const api = createMockLocalAgentApi()
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(createMockMachineWorkspaceSession({}, machineId)),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} terminalSettings={{ ...DEFAULT_TERMINAL_SETTINGS, keyboardMode: 'auto' }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    const shell = screen.getByTestId('termx-terminal-page').parentElement as HTMLElement
    const body = screen.getByTestId('termx-terminal-body')
    const wrapper = body.querySelector('[data-testid="termx-terminal-panel"]')?.parentElement as HTMLElement
    Object.defineProperty(body, 'clientHeight', { configurable: true, value: 640 })
    const terminalHandle = terminalHandleMocks.handles.get('terminal-1')
    await waitFor(() => expect(totalTerminalHandleCalls('fit')).toBeGreaterThan(0))
    for (const handle of terminalHandleMocks.allHandles) handle.fit.mockClear()

    act(() => {
      terminalHandle?.emitBufferChange(true)
      dispatchNativeKeyboardEvent({ visible: true, keyboardHeight: 260 })
    })

    await waitFor(() => expect(shell.style.height).toBe('540px'))
    expect(wrapper.style.height).toBe('')
    expect(wrapper.style.transform).toBe('')
    await waitFor(() => expect(totalTerminalHandleCalls('fit')).toBeGreaterThan(0))
  })

  it('does not focus the terminal or show the keyboard when toggling resize control', async () => {
    const api = createMockLocalAgentApi()
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(createMockMachineWorkspaceSession({}, machineId)),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    for (const handle of terminalHandleMocks.allHandles) {
      handle.focus.mockClear()
      handle.fit.mockClear()
    }
    const keyboardButton = screen.getByRole('button', { name: /toggle system keyboard/i })

    await clickTerminalMenuAction(/control resize/i)

    await waitFor(() => expect(totalTerminalHandleCalls('fit')).toBeGreaterThan(0))
    expect(totalTerminalHandleCalls('focus')).toBe(0)
    expect(keyboardButton.getAttribute('aria-pressed')).toBe('false')
  })

  it('pastes the latest daemon clipboard history entry from terminal tools', async () => {
    vi.stubGlobal('innerWidth', 390)
    const api = createMockLocalAgentApi()
    const session = createMockMachineWorkspaceSession({
      '/storage/list': {
        entries: [{
          app_id: 'termx.clipboard',
          scope: 'public',
          key: 'history/clip-1',
          value: new TextEncoder().encode(JSON.stringify({
            schema_version: 1,
            id: 'clip-1',
            text: 'daemon clipboard',
            preview: 'daemon clipboard',
            created_at: '2026-05-16T08:00:00Z',
          })),
          version: 1,
        }],
      },
    }, 'machine-local')
    const connect = vi.fn(async () => session)

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    await clickTerminalMenuAction(/^terminal tools$/i)
    await userEvent.click(screen.getByRole('button', { name: '粘贴' }))

    await waitFor(() => expect(
      terminalHandleMocks.allHandles.flatMap((handle) => handle.pasteText.mock.calls.map(([text]) => text)),
    ).toContain('daemon clipboard'))
    expect(session.requests).toContainEqual({
      method: 'POST',
      path: '/storage/list',
      params: {
        app_id: 'termx.clipboard',
        scope: 'public',
        prefix: 'history/',
      },
    })
  })

  it('opens daemon clipboard history and deletes an entry', async () => {
    vi.stubGlobal('innerWidth', 390)
    const api = createMockLocalAgentApi()
    const session = createMockMachineWorkspaceSession({
      '/storage/list': {
        entries: [{
          app_id: 'termx.clipboard',
          scope: 'public',
          key: 'history/clip-1',
          value: new TextEncoder().encode(JSON.stringify({
            schema_version: 1,
            id: 'clip-1',
            text: 'history text',
            preview: 'history text',
            created_at: '2026-05-16T08:00:00Z',
          })),
          version: 1,
        }],
      },
      '/storage/delete': { deleted: true, version: 2 },
    }, 'machine-local')
    const connect = vi.fn(async () => session)

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    await clickTerminalMenuAction(/^terminal tools$/i)
    await userEvent.click(screen.getByRole('button', { name: '剪贴板' }))

    const sheet = await screen.findByTestId('termx-clipboard-history-sheet')
    await waitFor(() => expect(within(sheet).getByText('history text')).toBeTruthy())
    await userEvent.click(within(sheet).getByRole('button', { name: /delete/i }))

    await waitFor(() => expect(session.requests).toContainEqual({
      method: 'POST',
      path: '/storage/delete',
      params: {
        app_id: 'termx.clipboard',
        scope: 'public',
        key: 'history/clip-1',
      },
    }))
  })

  it('reconnects from connection info without changing the current transport mode', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }, _options?: unknown) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({}, machineId))),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())

    await clickTerminalMenuAction(/connection/i)
    await waitFor(() => expect(screen.getByRole('button', { name: /^reconnect$/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /^reconnect$/i }))

    await waitFor(() => expect(connect).toHaveBeenCalledTimes(2))
    expect(connect.mock.calls[1]?.[1]).toEqual(expect.objectContaining({ forceRelay: false }))
    expect(sessions[0]?.disconnectCalls).toBe(1)
  })

  it('opens a split terminal view from the existing machine RTC session', async () => {
    const api = createMockLocalAgentApi()
    const sessions: ReturnType<typeof createMockMachineWorkspaceSession>[] = []
    const connect = vi.fn(({ machineId }: { machineId: string }) =>
      Promise.resolve(trackSession(sessions, createMockMachineWorkspaceSession({}, machineId))),
    )

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1'))

    await clickTerminalMenuAction(/split terminal/i)
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

    await clickTerminalMenuAction(/sync input/i)

    await userEvent.click(within(screen.getAllByTestId('termx-terminal')[1]!).getByRole('button', { name: /type through xterm/i }))
    expect(terminalHandleMocks.handles.get('terminal-1')?.sendInput).toHaveBeenCalledWith('typed\n')
    expect(terminalHandleMocks.handles.get('terminal-2')?.sendInput).toHaveBeenCalledWith('typed\n')

    await clickTerminalMenuAction(/close split/i)
    await waitFor(() => expect(screen.queryByTestId('termx-split-terminal-panel')).toBeNull())
    expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1')
    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('shows a resize unlock action when attach reports a size lock and clears the lock through management api', async () => {
    const api = createMockLocalAgentApi()
    const listTerminals = vi.fn(api.listTerminals)
    api.listTerminals = listTerminals
    const managementSession = createMockMachineWorkspaceSession({
      set_metadata: {},
    }, 'machine-local')
    const connect = vi.fn(async () => managementSession)

    render(<MachineWorkspace api={api} connector={{ connect }} />)

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

    render(<MachineWorkspace api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} inventoryEvents={inventoryEvents} />)

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
    const runtimeSession = createMockMachineWorkspaceSession({}, 'machine-local')
    runtimeSession.subscribeEvents = vi.fn((handler) => {
      runtimeHandler = handler
      return {
        close() {
          runtimeHandler = null
        },
      }
    })
    const connect = vi.fn(async () => runtimeSession)

    render(<MachineWorkspace api={api} connector={{ connect }} subscribeRuntimeInventoryEvents />)

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

  it('updates resize ownership from a pushed runtime terminal event without reloading the terminal list', async () => {
    const api = createMockLocalAgentApi({
      terminals: [terminalFixture({
        terminalId: 'terminal-1',
        title: 'zsh',
        command: '/bin/zsh',
        cols: 120,
        rows: 36,
        cwd: '/Users/lozzow/project',
      })],
    })
    const listTerminals = vi.fn(api.listTerminals)
    api.listTerminals = listTerminals
    let runtimeHandler: ((event: { type: string; payload?: unknown }) => void) | null = null
    const runtimeSession = createMockMachineWorkspaceSession({}, 'machine-local')
    runtimeSession.subscribeEvents = vi.fn((handler) => {
      runtimeHandler = handler
      return {
        close() {
          runtimeHandler = null
        },
      }
    })

    render(<MachineWorkspace api={api} connector={{ connect: vi.fn(async () => runtimeSession) }} subscribeRuntimeInventoryEvents />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(runtimeHandler).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open terminal menu/i }))
    expect(within(screen.getByTestId('termx-terminal-menu-sheet')).getByRole('button', { name: /control resize/i })).toBeTruthy()

    runtimeHandler!({
      type: 'terminal_metadata_changed',
      payload: {
        terminalId: 'terminal-1',
        terminal: {
          terminal_id: 'terminal-1',
          machine_id: 'machine-local',
          name: 'zsh',
          state: 'running',
          command: ['/bin/zsh'],
          cols: 120,
          rows: 36,
          cwd: '/Users/lozzow/project',
          resize_ownership: {
            owner_surface_id: 'app:machine-local:terminal:terminal-1',
          },
          resize_owner_attachment_count: 1,
        },
      },
    })

    await waitFor(() => expect(within(screen.getByTestId('termx-terminal-menu-sheet')).getByRole('button', { name: /release resize/i })).toBeTruthy())
    expect(listTerminals.mock.calls.length).toBeGreaterThanOrEqual(1)
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

    render(<MachineWorkspace api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} inventoryEvents={inventoryEvents} />)

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

  it('keeps the rendered terminal list visible while a refresh is in flight', async () => {
    let terminals = [terminalFixture({ terminalId: 'terminal-1', title: 'zsh' })]
    let resolveRefresh: ((value: Terminal[]) => void) | null = null
    const api = createMockLocalAgentApi()
    api.listTerminals = vi.fn(() => {
      if (resolveRefresh) {
        return Promise.resolve(terminals)
      }
      return new Promise<Terminal[]>((resolve) => {
        resolveRefresh = resolve
      })
    })

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

    render(<MachineWorkspace api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} inventoryEvents={inventoryEvents} />)

    await waitFor(() => expect(resolveRefresh).toBeTruthy())
    const resolveInitialRefresh = resolveRefresh!
    resolveInitialRefresh(terminals)
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
    await waitFor(() => expect(handler).toBeTruthy())

    terminals = [terminalFixture({ terminalId: 'terminal-2', title: 'worker' })]
    const pendingRefresh = new Promise<Terminal[]>((resolve) => {
      resolveRefresh = resolve
    })
    api.listTerminals = vi.fn(() => pendingRefresh)
    handler!({ type: 'inventory_changed' })

    expect(screen.getByText('zsh')).toBeTruthy()
    expect(screen.queryByText('Loading terminals...')).toBeNull()

    const resolvePendingRefresh = resolveRefresh!
    resolvePendingRefresh(terminals)
    await waitFor(() => expect(screen.getByText('worker')).toBeTruthy())
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
    const managementSession = createMockMachineWorkspaceSession({
      set_metadata: {},
    }, 'machine-local')
    const connect = vi.fn(async () => managementSession)

    render(<MachineWorkspace api={api} connector={{ connect }} />)

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
    const managementSession = createMockMachineWorkspaceSession({
      create: { terminal_id: 'terminal-3', state: 'running' },
      remove: {},
    }, 'machine-local')
    const connect = vi.fn(async () => managementSession)

    render(<MachineWorkspace api={api} connector={{ connect }} />)

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

  it('restarts and deletes an exited terminal from management controls', async () => {
    const api = createMockLocalAgentApi({
      terminals: [
        terminalFixture({
          terminalId: 'terminal-1',
          title: 'finished job',
          state: 'exited',
          command: '/bin/zsh',
          cwd: '/Users/lozzow/project',
        }),
      ],
    })
    const managementSession = createMockMachineWorkspaceSession({
      restart: {},
      remove: {},
    }, 'machine-local')
    const closeTerminalDataChannel = vi.fn()
    Object.assign(managementSession, { closeTerminalDataChannel })
    const connect = vi.fn(async () => managementSession)

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /manage finished job/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /manage finished job/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-actions-sheet')).toBeTruthy())

    const sheet = screen.getByTestId('termx-terminal-actions-sheet')
    await userEvent.click(within(sheet).getByRole('button', { name: /restart terminal/i }))

    await waitFor(() => expect(managementSession.requests).toContainEqual({
      method: 'restart',
      path: 'restart',
      params: { terminal_id: 'terminal-1' },
    }))
    expect(closeTerminalDataChannel).toHaveBeenCalledWith('terminal-1')

    await userEvent.click(screen.getByRole('button', { name: /manage finished job/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-actions-sheet')).toBeTruthy())
    await userEvent.click(within(screen.getByTestId('termx-terminal-actions-sheet')).getByRole('button', { name: /delete terminal/i }))

    await waitFor(() => expect(managementSession.requests).toContainEqual({
      method: 'remove',
      path: 'remove',
      params: { terminal_id: 'terminal-1' },
    }))
  })

  it('chooses a new terminal working directory from the visual picker', async () => {
    const api = createMockLocalAgentApi()
    const managementSession = createMockMachineWorkspaceSession({
      'file.list': ({ path }: { path?: string } = {}) => {
        if (path === '/Users/lozzow/project/app') {
          return {
            path: '/Users/lozzow/project/app',
            parent: '/Users/lozzow/project',
            total: 0,
            entries: [],
          }
        }
        return {
          path: '/Users/lozzow/project',
          parent: '/Users/lozzow',
          total: 2,
          entries: [
            { name: 'app', type: 'dir', size: 0 },
            { name: 'README.md', type: 'file', size: 42 },
          ],
        }
      },
      create: { terminal_id: 'terminal-3', state: 'running' },
    }, 'machine-local')
    const connect = vi.fn(async () => managementSession)

    render(<MachineWorkspace api={api} connector={{ connect }} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /create terminal/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /create terminal/i }))
    const editor = await screen.findByTestId('termx-terminal-editor-sheet')
    await userEvent.click(within(editor).getByRole('button', { name: /browse/i }))

    const picker = await screen.findByTestId('termx-terminal-path-picker-sheet')
    await waitFor(() => expect(within(picker).getByRole('button', { name: /^app$/i })).toBeTruthy())
    await userEvent.click(within(picker).getByRole('button', { name: /^app$/i }))
    await waitFor(() => expect(within(picker).getByText('/Users/lozzow/project/app')).toBeTruthy())
    expect(within(picker).getByTestId('termx-terminal-path-picker-list').className).toContain('h-80')
    expect(within(picker).getByText('Empty')).toBeTruthy()
    await userEvent.click(within(picker).getByRole('button', { name: /use this path/i }))

    const reopenedEditor = await screen.findByTestId('termx-terminal-editor-sheet')
    expect((within(reopenedEditor).getByLabelText('Working directory') as HTMLInputElement).value).toBe('/Users/lozzow/project/app')
    await userEvent.click(within(reopenedEditor).getByRole('button', { name: /create terminal/i }))

    await waitFor(() => expect(managementSession.requests).toContainEqual({
      method: 'create',
      path: 'create',
      params: {
        dir: '/Users/lozzow/project/app',
        tags: {
          'termx.size_lock': 'off',
          cwd: '/Users/lozzow/project/app',
        },
      },
    }))
  })

  it('creates the first terminal through a machine-scoped runtime api session', async () => {
    const api = createMockLocalAgentApi()
    api.listTerminals = vi.fn(async () => [])
    const managementSession = createMockMachineWorkspaceSession({
      create: { terminal_id: 'terminal-1', state: 'running' },
    }, 'machine-local')
    const connect = vi.fn(async () => managementSession)

    render(<MachineWorkspace api={api} connector={{ connect }} />)

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
    const managementSession = createMockMachineWorkspaceSession({
      create: { terminal_id: 'terminal-3', state: 'running' },
    }, 'machine-local')
    const connect = vi.fn(async () => managementSession)

    render(<MachineWorkspace api={api} connector={{ connect }} />)

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
      <MachineWorkspace
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

    render(<MachineWorkspace api={api} connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await waitFor(() => expect(listTerminals.mock.calls.length).toBeGreaterThanOrEqual(1))
    expect(screen.queryByRole('button', { name: /refresh terminals/i })).toBeNull()
  })

  it('keeps the app shell driven by LocalAgentApi and session interfaces only', () => {
    const connect = vi.fn(() => Promise.resolve(createMockMachineWorkspaceSession(
      {},
      'machine-local',
    )))
    const connector = { connect } satisfies MachineWorkspaceConnector
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
    const connector = { connect } satisfies MachineWorkspaceConnector

    render(<MachineWorkspace api={createMockLocalAgentApi()} connector={connector} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(connect).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-network-overlay').textContent).toContain('session token is required'))
    expect(connect).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('alert')).toBeNull()
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane/i)
  })

  it('keeps the local pair harness reachable through app-level interfaces', async () => {
    const connect = vi.fn(() => Promise.resolve(createMockMachineWorkspaceSession(
      {},
      'machine-local',
    )))
    const connector = { connect } satisfies MachineWorkspaceConnector

    render(
      <MachineWorkspace
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

  it('pairs local devices from a termx QR payload without manually splitting credentials', async () => {
    const sessionStore = createMachineSessionStore(new MemoryStorage())
    const pairApi = createMockPairApi()
    pairApi.pair = vi.fn(async () => ({
      machineId: 'machine-local',
      sessionToken: 'session-token-local',
      expiresAt: '2099-05-01T07:00:00Z',
    }))

    render(
      <MachineWorkspace
        api={createMockLocalAgentApi()}
        connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }}
        pair={{
          api: pairApi,
          sessionStore,
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /verify device/i }))
    await waitFor(() => expect(screen.getByTestId('termx-local-pair-panel')).toBeTruthy())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), {
      target: { value: termxPairUri(localPairPayload()) },
    })
    await userEvent.click(within(screen.getByTestId('termx-pair-sheet')).getByRole('button', { name: /^pair device$/i }))

    await waitFor(() => expect(screen.getByText('Paired with machine-local')).toBeTruthy())
    expect(pairApi.pair).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-local',
      pairSessionId: 'pair-session-local',
      pairSecret: 'pair-secret-local',
    }))
    expect(sessionStore.getSessionToken('machine-local')).toBe('session-token-local')
  })

  it('refreshes terminal inventory after pairing a local device', async () => {
    const sessionStore = createMachineSessionStore(new MemoryStorage())
    let paired = false
    const api = createMockLocalAgentApi({ terminals: [] })
    const listTerminals = vi.fn(async () => (
      paired ? [terminalFixture({ title: 'zsh', terminalId: 'terminal-after-pair' })] : []
    ))
    api.listTerminals = listTerminals
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
      <MachineWorkspace
        api={api}
        connector={{ connect: vi.fn(async () => { throw new Error('unexpected connect') }) }}
        pair={{
          api: pairApi,
          sessionStore,
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(screen.queryByRole('button', { name: /open zsh/i })).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: /verify device/i }))
    await waitFor(() => expect(screen.getByTestId('termx-local-pair-panel')).toBeTruthy())
    fireEvent.change(screen.getByLabelText(/termx qr content/i), {
      target: { value: termxPairUri(localPairPayload()) },
    })
    await userEvent.click(within(screen.getByTestId('termx-pair-sheet')).getByRole('button', { name: /^pair device$/i }))

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    expect(listTerminals).toHaveBeenCalledTimes(2)
  })

  it('keeps pair reachable when first-run terminal connect needs a token and retries after pairing', async () => {
    const sessionStore = createMachineSessionStore(new MemoryStorage())
    let paired = false
    const connect = vi.fn(() => {
      if (!paired) return Promise.reject(new Error('session token is required before opening a terminal'))
      return Promise.resolve(createMockMachineWorkspaceSession({}, 'machine-local'))
    })
    const connector = { connect } satisfies MachineWorkspaceConnector
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
      <MachineWorkspace
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
    await waitFor(() => expect(screen.getAllByTestId('termx-pair-sheet').length).toBeGreaterThan(0))
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

  it('clears stale pair tokens and opens pairing on native auth failure', async () => {
    const sessionStore = createMachineSessionStore(new MemoryStorage())
    sessionStore.saveSessionToken('machine-local', 'stale-token', '2099-05-01T07:00:00Z')
    const connect = vi.fn(() => Promise.reject(new Error('auth')))

    render(
      <MachineWorkspace
        api={createMockLocalAgentApi()}
        connector={{ connect }}
        pair={{
          api: createMockPairApi(),
          sessionStore,
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))

    await waitFor(() => expect(screen.getAllByTestId('termx-pair-sheet').length).toBeGreaterThan(0))
    expect(screen.getByTestId('termx-machine-network-overlay').textContent).toContain('re-authorize')
    expect(sessionStore.getSessionToken('machine-local')).toBeNull()
  })

  it.each([
    'unauthenticated',
    'capability_invalid',
    'capability_expired',
    'device_identity_mismatch',
    'scope_invalid',
  ])('returns managed endpoint failure %s to the external reauthorization flow', async (failureCode) => {
    const onNeedsReauthorization = vi.fn()
    const connect = vi.fn(() => Promise.reject(new Error(failureCode)))

    render(
      <MachineWorkspace
        api={createMockLocalAgentApi()}
        connector={{ connect }}
        initialMachine={{ machineId: 'machine-local', name: 'Managed daemon', state: 'online' }}
        onNeedsReauthorization={onNeedsReauthorization}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))

    await waitFor(() => expect(onNeedsReauthorization).toHaveBeenCalledWith('machine-local'))
    expect(screen.getByTestId('termx-machine-network-overlay').textContent).toContain('re-authorize')
  })

  it('reopens pairing when a cached runtime session token cannot be parsed', async () => {
    const sessionStore = createMachineSessionStore(new MemoryStorage())
    sessionStore.saveSessionToken('machine-local', 'stale-token', '2099-05-01T07:00:00Z')
    const connect = vi.fn(() =>
      Promise.reject(new Error('Stored session token is invalid. Pair this machine again before opening the terminal.')),
    )

    render(
      <MachineWorkspace
        api={createMockLocalAgentApi()}
        connector={{ connect }}
        pair={{
          api: createMockPairApi(),
          sessionStore,
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))

    await waitFor(() => expect(screen.getAllByTestId('termx-pair-sheet').length).toBeGreaterThan(0))
    expect(screen.getByTestId('termx-machine-network-overlay').textContent).toContain('re-authorize')
    expect(screen.getAllByTestId('termx-local-pair-panel').length).toBeGreaterThan(0)
    expect(sessionStore.getSessionToken('machine-local')).toBeNull()
  })

  it('reopens pairing when Hub rejects a cached runtime session token', async () => {
    const sessionStore = createMachineSessionStore(new MemoryStorage())
    sessionStore.saveSessionToken('machine-local', 'stale-token', '2099-05-01T07:00:00Z')
    const connect = vi.fn(() =>
      Promise.reject(new Error('cloud_answer_error: invalid session token: invalid token format')),
    )

    render(
      <MachineWorkspace
        api={createMockLocalAgentApi()}
        connector={{ connect }}
        pair={{
          api: createMockPairApi(),
          sessionStore,
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))

    await waitFor(() => expect(screen.getAllByTestId('termx-pair-sheet').length).toBeGreaterThan(0))
    expect(screen.getByTestId('termx-machine-network-overlay').textContent).toContain('re-authorize')
    expect(sessionStore.getSessionToken('machine-local')).toBeNull()
  })
})

function trackSession<T extends ReturnType<typeof createMockMachineWorkspaceSession>>(sessions: T[], session: T): T {
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

function createMockMachineWorkspaceSession(
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

function totalTerminalHandleCalls(method: 'fit' | 'focus'): number {
  return terminalHandleMocks.allHandles.reduce((sum, handle) => sum + handle[method].mock.calls.length, 0)
}

function createMockPairApi(): LocalPairingApi {
  return {
    async pair() {
      throw new Error('pair is not used by this test')
    },
  }
}

function localPairPayload(): Record<string, unknown> {
  return {
    type: 'termx_pair',
    schema_version: 4,
    machine: {
      id: 'machine-local',
      name: 'Local Mac',
    },
    local: {
      hub_urls: ['http://127.0.0.1:18888'],
    },
    pairing: {
      session_id: 'pair-session-local',
      secret: 'pair-secret-local',
    },
  }
}

function termxPairUri(payload: Record<string, unknown>): string {
  const bytes = new TextEncoder().encode(JSON.stringify(payload))
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return `termx://pair?payload=${btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')}`
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
