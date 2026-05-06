import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { LocalAppCrypto } from './localAppIdentity'
import type { LocalPairingApi } from './transport'

vi.mock('./Terminal', () => ({
  Terminal: ({ machineId, terminalId }: { machineId: string; terminalId: string }) => (
    <section data-machine-id={machineId} data-terminal-id={terminalId} data-testid="termx-terminal" />
  ),
}))

describe('local web entry shell', () => {
  afterEach(() => {
    cleanup()
    document.body.innerHTML = ''
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('mounts the shared local remote app with browser local adapters and no forbidden public model text', async () => {
    const entry = await import('./localWebEntry')
    document.body.innerHTML = '<div id="root"></div>'
    const fetch = vi.fn(async () => jsonResponse({ error: { message: 'unexpected fetch' } }, 500))
    vi.stubGlobal('fetch', fetch)
    const session = {
      async disconnect() {},
      async openTerminal() {
        return {
          label: 'terminal:terminal-1',
          readyState: 'open' as const,
          send() {},
          close() {},
          onMessage() { return { close() {} } },
          onClose() { return { close() {} } },
          async waitOpen() {},
        }
      },
      async openApi() {
        return {
          async request<TResponse>() {
            return { path: '/', parent: '', total: 0, entries: [] } as TResponse
          },
          close() {},
        }
      },
      async openFileTransfer() {
        return {
          label: 'file:test',
          readyState: 'open' as const,
          send() {},
          close() {},
          onMessage() { return { close() {} } },
          onClose() { return { close() {} } },
          async waitOpen() {},
        }
      },
      async getConnectionInfo() {
        return {
          path: 'local' as const,
          connectionId: 'local-test',
          machineId: 'machine-local',
          terminalId: 'terminal-1',
          relayInUse: false,
        }
      },
      async getCapabilities() {
        return {
          terminalAllowed: true,
          apiAllowed: true,
          eventsAllowed: true,
          fileTransferAllowed: true,
          terminalManagementAllowed: true,
          relayInUse: false,
        }
      },
      subscribeEvents() {
        return { close() {} }
      },
    }

    entry.mountLocalWebApp({
      api: {
        async getStatus() {
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
        async listTerminals() {
          return [{
            machineId: 'machine-local',
            terminalId: 'terminal-1',
            title: 'zsh',
            state: 'running' as const,
            command: '/bin/zsh -l',
            cols: 120,
            rows: 36,
          }]
        },
      },
      connector: {
        async connect() {
          return session
        },
      },
    })

    await waitFor(() => expect(screen.getByTestId('termx-local-web-shell')).toBeTruthy())
    await waitFor(() => expect(screen.getByTestId('termx-terminal-list')).toBeTruthy())
    expect(screen.getByLabelText('Local hub URL')).toHaveProperty('value', 'http://127.0.0.1:18888')
    const appShell = screen.getByTestId('termx-local-web-shell').firstElementChild as HTMLElement
    expect(appShell.className).toContain('flex')
    expect(appShell.className).not.toMatch(/\bgrid\b|gap-4|p-4|md:grid-cols/)
    expect(screen.getByTestId('termx-terminal-list').getAttribute('data-machine-id')).toBe('machine-local')
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|session/i)
    expect(JSON.stringify(fetch.mock.calls)).not.toMatch(/turn|credential|machine_private_key|privateKey/i)
  })

  it('lets users enter and persist a local embedded hub URL', async () => {
    const entry = await import('./localWebEntry')
    document.body.innerHTML = '<div id="root"></div>'
    const storage = new MemoryStorage()

    entry.mountLocalWebApp({
      api: {
        async getStatus() {
          return {
            machine: {
              machineId: 'machine-local',
              name: 'Local Mac',
              state: 'online' as const,
              terminalCount: 0,
            },
            localWeb: {
              httpUrl: 'http://127.0.0.1:18888',
              rtcOfferUrl: 'http://127.0.0.1:18888',
            },
          }
        },
        async listTerminals() {
          return []
        },
      },
      networkRuntime: {
        fetch: async () => jsonResponse({}),
        storage,
        queryParam: () => null,
      },
    })

    const input = await screen.findByLabelText('Local hub URL')
    await userEvent.clear(input)
    await userEvent.type(input, '192.168.1.100:18888')
    await userEvent.click(screen.getByRole('button', { name: 'Use' }))

    await waitFor(() => expect(input).toHaveProperty('value', 'http://192.168.1.100:18888'))
    expect(storage.getItem('termx.local.hubUrl')).toBe('http://192.168.1.100:18888')
  })

  it('shows the pairing gate before a first-run browser local RTC inventory session exists', async () => {
    const entry = await import('./localWebEntry')
    document.body.innerHTML = '<div id="root"></div>'
    const storage = new MemoryStorage()
    const fetch = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).endsWith('/api/v1/agents/online')) {
        return jsonResponse({
          agents: [{
            machine_id: 'device-real-local',
            machine_name: 'Local Mac',
            status: 'online',
            terminals: [],
          }],
        })
      }
      return jsonResponse({})
    })
    const pair = vi.fn(async (input: Parameters<LocalPairingApi['pair']>[0]) => ({
      machineId: input.machineId ?? 'missing-machine',
      appCertificate: '{"payload":{"machine_id":"device-real-local","app_public_key":"AQIDBA=="},"signature":"machine-sig"}',
      expiresAt: '2026-05-06T00:00:00Z',
    }))

    entry.mountLocalWebApp({
      networkRuntime: {
        fetch,
        storage,
        queryParam: () => null,
      },
      pairApi: {
        pair,
      },
      pairCrypto: createMockAppCrypto(),
    })

    await waitFor(() => expect(screen.getByTestId('termx-verification-gate')).toBeTruthy())
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByText('No active terminals')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /verify device/i }))
    fireEvent.change(screen.getByLabelText(/pair id/i), { target: { value: 'pair-1' } })
    fireEvent.change(screen.getByLabelText(/pair secret/i), { target: { value: 'secret-1' } })
    const pairButton = within(screen.getByTestId('termx-local-pair-panel')).getByRole('button', { name: /^pair device$/i })
    await waitFor(() => expect(pairButton).not.toHaveProperty('disabled', true))
    fireEvent.click(pairButton)

    await waitFor(() => expect(pair).toHaveBeenCalledWith(expect.objectContaining({ machineId: 'device-real-local' })))
    expect(screen.getByTestId('termx-local-web-shell').textContent).toContain('Local Mac')
    expect(fetch).toHaveBeenCalledWith('http://127.0.0.1:18888/api/v1/agents/online', expect.any(Object))
  })

  it('generates a fresh opaque local connect ticket for each embedded Hub offer', async () => {
    const entry = await import('./localWebEntry')

    const first = entry.createLocalConnectTicket('machine local', () => 'nonce-1')
    const second = entry.createLocalConnectTicket('machine local', () => 'nonce-2')

    expect(first).toBe('local:machine%20local:nonce-1')
    expect(second).toBe('local:machine%20local:nonce-2')
    expect(first).not.toBe(second)
  })

  it('does not require browser crypto or local storage until a terminal session is created', async () => {
    const entry = await import('./localWebEntry')
    document.body.innerHTML = '<div id="root"></div>'
    vi.stubGlobal('localStorage', undefined)

    entry.mountLocalWebApp({
      api: {
        async getStatus() {
          return {
            machine: {
              machineId: 'machine-local',
              name: 'Local Mac',
              state: 'online',
              terminalCount: 0,
            },
            localWeb: {
              httpUrl: 'http://127.0.0.1:18888',
              rtcOfferUrl: 'http://127.0.0.1:18888',
            },
          }
        },
        async listTerminals() {
          return []
        },
      },
    })

    await waitFor(() => expect(screen.getByTestId('termx-local-web-shell')).toBeTruthy())
    await waitFor(() => expect(screen.getByText('No active terminals')).toBeTruthy())
  })

  it('keeps the local shell mounted when pair crypto is unavailable', async () => {
    const entry = await import('./localWebEntry')
    document.body.innerHTML = '<div id="root"></div>'
    vi.stubGlobal('crypto', undefined)

    entry.mountLocalWebApp({
      api: {
        async getStatus() {
          return {
            machine: {
              machineId: 'machine-local',
              name: 'Local Mac',
              state: 'online',
              terminalCount: 0,
            },
            localWeb: {
              httpUrl: 'http://127.0.0.1:18888',
              rtcOfferUrl: 'http://127.0.0.1:18888',
            },
          }
        },
        async listTerminals() {
          return []
        },
      },
    })

    await waitFor(() => expect(screen.getByTestId('termx-local-web-shell')).toBeTruthy())
    await waitFor(() => expect(screen.getByText('No active terminals')).toBeTruthy())
    expect(screen.queryByTestId('termx-local-pair-panel')).toBeNull()
  })
})

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
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
