import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, useImperativeHandle } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LocalRemoteApp, type LocalRemoteTransportFactory } from './LocalRemoteApp'
import { createLocalAppIdentityStore, type LocalAppCrypto } from './localAppIdentity'
import type { TerminalModifierState } from './mobileTerminalInput'
import type { LocalAgentApi } from './transport'
import type { TerminalInventoryEvents } from './transport'
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
    const transports: ReturnType<typeof createMockLocalRemoteTransport>[] = []
    const createTransport = vi.fn<LocalRemoteTransportFactory>(({ machineId, terminalId }) =>
      trackTransport(transports, createMockLocalRemoteTransport({
          '/files/list': { path: '/', parent: '', total: 0, entries: [] },
        }, machineId, terminalId)),
    )

    render(<LocalRemoteApp api={api} createTransport={createTransport} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(screen.queryByTestId('termx-terminal')).toBeNull()
    expect(createTransport).not.toHaveBeenCalled()
    expect(screen.getByText('120 × 36')).toBeTruthy()
    expect(screen.getByTestId('termx-terminal-list').textContent).toMatch(/120 × 36/)
    expect(screen.getByText('/Users/lozzow/project')).toBeTruthy()
    expect(screen.getByText('dev')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay')).toBeTruthy())
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('terminal-1')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-initial-path')).toBe('/Users/lozzow/project')
    expect(screen.queryByTestId('termx-terminal')).toBeNull()
    await waitFor(() => expect(createTransport).toHaveBeenCalledTimes(1))
    await userEvent.click(screen.getByRole('button', { name: 'Open tmp' }))
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/tmp')
    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').style.visibility).toBe('hidden'))
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
    expect(createTransport).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    }))
    await waitFor(() => expect(transports[1]?.connectCalls).toEqual([{
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
    }]))
    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').style.visibility).toBe('hidden'))
    expect(screen.getByTestId('termx-terminal')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|session/i)
  })

  it('preserves file state for the same terminal but resets when the file context terminal changes', async () => {
    const api = createMockLocalAgentApi()
    const transports: ReturnType<typeof createMockLocalRemoteTransport>[] = []
    const createTransport = vi.fn<LocalRemoteTransportFactory>(({ machineId, terminalId }) =>
      trackTransport(transports, createMockLocalRemoteTransport({
        '/files/list': { path: '/', parent: '', total: 0, entries: [] },
      }, machineId, terminalId)),
    )

    render(<LocalRemoteApp api={api} createTransport={createTransport} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: 'Open tmp' }))
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/tmp')

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').style.visibility).toBe('hidden'))
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/tmp'))

    await userEvent.click(screen.getByRole('button', { name: /close files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').style.visibility).toBe('hidden'))
    await userEvent.click(screen.getByRole('button', { name: /open worker/i }))
    await userEvent.click(screen.getByRole('button', { name: /open files/i }))
    await waitFor(() => expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('terminal-2'))
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-initial-path')).toBe('/srv/worker')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-current-path')).toBe('/srv/worker')
  })

  it('uses a terminal-first mobile shell with sheets instead of a Config sidebar', async () => {
    const api = createMockLocalAgentApi()
    const transports: ReturnType<typeof createMockLocalRemoteTransport>[] = []
    const createTransport = vi.fn<LocalRemoteTransportFactory>(({ machineId, terminalId }) =>
      trackTransport(transports, createMockLocalRemoteTransport({
        '/files/list': { path: '/', parent: '', total: 0, entries: [] },
      }, machineId, terminalId)),
    )
    const pairApi = createMockLocalAgentApi()
    pairApi.pair = vi.fn(async () => ({
      machineId: 'machine-local',
      appCertificate: '{"payload":{"machine_id":"machine-local","app_public_key":"AQIDBA=="},"signature":"machine-sig"}',
      expiresAt: '2026-05-01T07:00:00Z',
    }))

    render(
      <LocalRemoteApp
        api={api}
        createTransport={createTransport}
        pair={{
          api: pairApi,
          storage: createLocalAppIdentityStore(new MemoryStorage()),
          crypto: createMockAppCrypto(),
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(screen.queryByText('Config')).toBeNull()
    expect(screen.getByText('Local Mac')).toBeTruthy()
    expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy()
    expect(screen.queryByTestId('termx-mobile-keybar')).toBeNull()
    expect(createTransport).not.toHaveBeenCalled()

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
    await waitFor(() => expect(screen.getByTestId('termx-machine-files-overlay').style.visibility).toBe('hidden'))
    expect(screen.getByTestId('termx-terminal')).toBeTruthy()
    expect(screen.getByTestId('termx-mobile-keybar').className.split(/\s+/)).not.toContain('hidden')

    await userEvent.click(screen.getByRole('button', { name: /show terminal list/i }))
    expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy()
    expect(screen.getByTestId('termx-terminal-list-page').textContent).not.toMatch(/workspace|tab|window|pane|session/i)
    await waitFor(() => expect(transports[0]?.disconnectCalls).toBe(1))

    await userEvent.click(screen.getAllByRole('button', { name: /pair device/i })[0]!)
    expect(screen.getByTestId('termx-pair-sheet')).toBeTruthy()
    expect(screen.getByTestId('termx-pair-sheet').textContent).not.toMatch(/workspace|tab|window|pane|session/i)

    await userEvent.type(screen.getByLabelText('Pair ID'), 'pair-1')
    await userEvent.type(screen.getByLabelText('Pair secret'), 'secret-1')
    await userEvent.click(within(screen.getByTestId('termx-pair-sheet')).getByRole('button', { name: /^pair device$/i }))
    await waitFor(() => expect(screen.getByText('Paired with machine-local')).toBeTruthy())
    expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy()
    expect(screen.queryByTestId('termx-terminal')).toBeNull()
    expect(createTransport).toHaveBeenCalledTimes(2)
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

    render(<LocalRemoteApp api={api} createTransport={vi.fn()} inventoryEvents={inventoryEvents} />)

    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
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

  it('keeps the app shell driven by LocalAgentApi and transport interfaces only', () => {
    const createTransport = vi.fn<LocalRemoteTransportFactory>(() => createMockLocalRemoteTransport(
      {},
      'machine-local',
      'terminal-1',
    ))
    const props = {
      api: createMockLocalAgentApi(),
      createTransport,
    }

    expect(Object.keys(props)).not.toContain('rtcPeerConnection')
    expect(Object.keys(props)).not.toContain('nativePlugin')
    expect(Object.keys(props)).not.toContain('relayCredentials')
  })

  it('renders local transport setup errors instead of crashing the embedded shell', async () => {
    const createTransport = vi.fn<LocalRemoteTransportFactory>(() => {
      throw new Error('local app certificate is required before opening a terminal')
    })

    render(<LocalRemoteApp api={createMockLocalAgentApi()} createTransport={createTransport} />)

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(createTransport).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('local app certificate is required'))
    expect(createTransport).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane/i)
  })

  it('keeps the local pair harness reachable through app-level interfaces', async () => {
    const createTransport = vi.fn<LocalRemoteTransportFactory>(() => createMockLocalRemoteTransport(
      {},
      'machine-local',
      'terminal-1',
    ))

    render(
      <LocalRemoteApp
        api={createMockLocalAgentApi()}
        createTransport={createTransport}
        pair={{
          api: createMockLocalAgentApi(),
          storage: createLocalAppIdentityStore(new MemoryStorage()),
          crypto: createMockAppCrypto(),
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /pair device/i })).toBeTruthy())
    await userEvent.click(screen.getAllByRole('button', { name: /pair device/i })[0]!)
    await waitFor(() => expect(screen.getByTestId('termx-local-pair-panel')).toBeTruthy())
    expect(screen.getByLabelText('Pair ID')).toBeTruthy()
    expect(screen.getByLabelText('Pair secret')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|session/i)
  })

  it('keeps pair reachable when first-run terminal connect needs a certificate and retries after pairing', async () => {
    const storage = createLocalAppIdentityStore(new MemoryStorage())
    let paired = false
    const createTransport = vi.fn<LocalRemoteTransportFactory>(() => {
      if (!paired) throw new Error('local app certificate is required before opening a terminal')
      return createMockLocalRemoteTransport({}, 'machine-local', 'terminal-1')
    })
    const pairApi = createMockLocalAgentApi()
    pairApi.pair = vi.fn(async () => {
      paired = true
      return {
        machineId: 'machine-local',
        appCertificate: '{"payload":{"machine_id":"machine-local","app_public_key":"AQIDBA=="},"signature":"machine-sig"}',
        expiresAt: '2026-05-01T07:00:00Z',
      }
    })

    render(
      <LocalRemoteApp
        api={createMockLocalAgentApi()}
        createTransport={createTransport}
        pair={{
          api: pairApi,
          storage,
          crypto: createMockAppCrypto(),
          appName: 'TermX Local Web',
        }}
      />,
    )

    await waitFor(() => expect(screen.getByTestId('termx-terminal-list-page')).toBeTruthy())
    expect(createTransport).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('local app certificate is required'))
    await waitFor(() => expect(screen.getByTestId('termx-pair-sheet')).toBeTruthy())
    expect(screen.getByTestId('termx-local-pair-panel')).toBeTruthy()

    await userEvent.type(screen.getByLabelText('Pair ID'), 'pair-1')
    await userEvent.type(screen.getByLabelText('Pair secret'), 'secret-1')
    await userEvent.click(within(screen.getByTestId('termx-pair-sheet')).getByRole('button', { name: /^pair device$/i }))

    await waitFor(() => expect(screen.getByText('Paired with machine-local')).toBeTruthy())
    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    expect(createTransport).toHaveBeenCalledTimes(2)
    expect(storage.loadCertificate()).toContain('machine-local')
  })
})

function trackTransport<T extends ReturnType<typeof createMockLocalRemoteTransport>>(transports: T[], transport: T): T {
  transports.push(transport)
  return transport
}

function createMockLocalRemoteTransport(
  responders: Parameters<typeof createMockFilePeerTransport>[0],
  machineId: string,
  terminalId: string,
): ReturnType<typeof createMockFilePeerTransport> & TerminalTransport & {
  connectCalls: Array<{ machineId: string; terminalId?: string; mode: string }>
  disconnectCalls: number
} {
  const transport = createMockFilePeerTransport(responders, {}, { machineId, terminalId })
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

function createMockLocalAgentApi(): LocalAgentApi {
  return {
    async getStatus() {
      return {
        machine: {
          machineId: 'machine-local',
          name: 'Local Mac',
          state: 'online',
          terminalCount: 1,
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
      throw new Error('pair is not used by LocalRemoteApp tests')
    },
    async createRTCAnswer() {
      throw new Error('createRTCAnswer is not used by LocalRemoteApp tests')
    },
    async createInventoryRTCAnswer() {
      throw new Error('createInventoryRTCAnswer is not used by LocalRemoteApp tests')
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

function createMockAppCrypto(): LocalAppCrypto {
  return {
    async generateKeyPair() {
      return {
        publicKey: { raw: new Uint8Array([1, 2, 3, 4]) },
        privateKey: { keyId: 'generated-app-key' },
      }
    },
    async savePrivateKey() {},
    async loadPrivateKey() {
      return { keyId: 'generated-app-key' }
    },
    async sign() {
      return new TextEncoder().encode('signed-by-app-key')
    },
    async randomBytes(length: number) {
      return new Uint8Array(length)
    },
    async sha256() {
      return new Uint8Array(32)
    },
  }
}
