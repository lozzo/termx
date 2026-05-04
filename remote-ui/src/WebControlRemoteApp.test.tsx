import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { WebControlRemoteApp } from './WebControlRemoteApp'
import type { RtcSession } from './transport'
import type { WebControlFetch } from './webControlApi'

describe('WebControlRemoteApp', () => {
  afterEach(() => cleanup())

  it('defaults to the App-first machine home and keeps Web Control URL in settings', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.controlUrl', 'http://127.0.0.1:5174')

    render(<WebControlRemoteApp storage={storage as unknown as Storage} />)

    expect(screen.getByTestId('termx-app-home')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Machines' })).toBeTruthy()
    expect(screen.getByText('Sign in to view your devices.')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))

    expect(screen.getByTestId('termx-app-settings')).toBeTruthy()
    expect((screen.getByLabelText(/web control/i) as HTMLInputElement).value).toBe('http://114.66.58.243:12306')
  })

  it('logs into Web Control, lists account machines, and claims a TermX pairing code through the machine Hub', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({ machineId: 'device-1', name: 'RedmiBook' }))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        token_type: 'Bearer',
        access_token: 'access-token-1',
        refresh_token: '',
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          paired: false,
          source: 'cloud',
          machine_public_key_fingerprint: 'sha256:machine',
          control_url: 'http://114.66.58.243:12306',
          hub_http_url: 'http://114.66.58.243:8447',
          hub_status: 'online',
        }],
      }),
      jsonResponse(200, {
        claim_id: 'claim-1',
        machine_id: 'device-1',
        machine_name: 'RedmiBook',
        machine_public_key: 'machine-public-key',
        app_certificate: {
          payload: {
            machine_id: 'device-1',
            app_public_key: 'AQIDBA==',
          },
          signature: 'machine-sig',
        },
        expires_at: '2026-05-05T10:30:00Z',
      }),
    ])
    const listTerminals = vi.fn(async () => [{
      terminalId: 'terminal-1',
      machineId: 'device-1',
      title: 'zsh',
      state: 'running' as const,
      command: '/bin/zsh -l',
      cols: 100,
      rows: 30,
    }])
    const connect = vi.fn(async () => fakeRtcSession())
    const crypto = createMockCrypto()
    const originalFetch = globalThis.fetch
    globalThis.fetch = fetch.fetch as typeof globalThis.fetch
    try {
      render(
        <WebControlRemoteApp
          defaultControlUrl="http://114.66.58.243:12306"
          machineRuntimeFactory={({ machine }) => ({
            api: {
              async getStatus() {
                return {
                  machine: {
                    machineId: machine.id,
                    name: machine.name,
                    state: 'online',
                  },
                  localWeb: {
                    httpUrl: '',
                    rtcOfferUrl: machine.hubHttpUrl ?? '',
                  },
                }
              },
              listTerminals,
            },
            connector: { connect },
          })}
          pairCrypto={crypto}
          storage={storage as unknown as Storage}
        />,
      )

      await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
      await userEvent.type(screen.getByLabelText(/email or username/i), 'lozzow@example.test')
      await userEvent.type(screen.getByLabelText(/password/i), 'secret')
      await userEvent.click(screen.getByRole('button', { name: /sign in/i }))

      await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
      expect(screen.getAllByText('Scan QR').length).toBeGreaterThan(0)
      expect(screen.getByText('Hub online')).toBeTruthy()
      expect(screen.queryByText('Ready')).toBeNull()

      await userEvent.click(screen.getByRole('button', { name: /pair redmibook/i }))
      expect(screen.getByTestId('termx-pair-sheet')).toBeTruthy()
      fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: pairUri } })
      await userEvent.click(screen.getByRole('button', { name: /^pair device$/i }))

      await waitFor(() => expect(screen.getByText('Paired RedmiBook')).toBeTruthy())
      expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy()
      expect(screen.getByRole('heading', { name: 'Terminals' })).toBeTruthy()
      expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0)
      await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())
      expect(screen.queryByText('Terminal runtime is not connected yet.')).toBeNull()
      await userEvent.click(screen.getByRole('button', { name: /back to machines/i }))
      expect(screen.getAllByText('Ready').length).toBeGreaterThan(0)
      await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
      expect(screen.getByTestId('termx-machine-terminal-list')).toBeTruthy()
      await waitFor(() => expect(listTerminals).toHaveBeenCalledTimes(2))
      const hubPairRequest = fetch.requests.find((request) => request.url === 'http://114.66.58.243:8447/api/v1/pairing/claims')
      expect(hubPairRequest?.method).toBe('POST')
      expect(hubPairRequest?.body).toMatchObject({
        machine_id: 'device-1',
        pair_session_id: 'pair-session-1',
        pair_secret: 'pair-secret-1',
        app_device_id: expect.stringMatching(/^appweb_/),
        app_name: 'TermX Remote App',
        app_public_key: 'AQIDBA==',
        requested_capabilities: ['terminal', 'file_manager', 'terminal_management'],
      })
      const stored = JSON.parse(storage.getItem('termx.app.machines.v1') ?? '[]') as Array<Record<string, unknown>>
      expect(stored).toHaveLength(1)
      expect(stored[0]?.machineId).toBe('device-1')
      expect(stored[0]?.source).toBe('cloud')
      expect(stored[0]?.preferredPath).toBe('public_p2p')
      expect(storage.dump()).toHaveProperty('termx.local.user%3Auser-1%3Amachine%3Adevice-1.appCertificate')
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('rejects pairing codes that do not match a Web Control machine in the signed-in account', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri(pairPayload({ machineId: 'local-machine-1', name: 'Local Debug Machine' }))
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        token_type: 'Bearer',
        access_token: 'access-token-1',
        refresh_token: '',
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }),
      jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          online: true,
          paired: false,
          source: 'cloud',
          hub_status: 'online',
        }],
      }),
    ])
    const originalFetch = globalThis.fetch
    globalThis.fetch = fetch.fetch as typeof globalThis.fetch
    try {
      render(
        <WebControlRemoteApp
          defaultControlUrl="http://114.66.58.243:12306"
          pairCrypto={createMockCrypto()}
          storage={storage as unknown as Storage}
        />,
      )

      await userEvent.click(screen.getByRole('button', { name: /open settings/i }))
      await userEvent.type(screen.getByLabelText(/email or username/i), 'lozzow@example.test')
      await userEvent.type(screen.getByLabelText(/password/i), 'secret')
      await userEvent.click(screen.getByRole('button', { name: /sign in/i }))
      await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))

      await userEvent.click(screen.getByRole('button', { name: /scan pairing qr/i }))
      fireEvent.change(screen.getByLabelText(/termx qr content/i), { target: { value: pairUri } })
      await userEvent.click(screen.getByRole('button', { name: /^pair device$/i }))

      await waitFor(() => expect(screen.getByText('This pairing code does not match a Web Control device in this account')).toBeTruthy())
      expect(fetch.requests.some((request) => request.url.includes('/api/v1/pairing/claims'))).toBe(false)
      expect(storage.getItem('termx.app.machines.v1')).toBeNull()
    } finally {
      globalThis.fetch = originalFetch
    }
  })
})

function fakeRtcSession(): RtcSession {
  return {
    async openTerminal() {
      throw new Error('terminal channel is not used by Web Control app tests')
    },
    async openApi() {
      throw new Error('api channel is not used by Web Control app tests')
    },
    async openFileTransfer() {
      throw new Error('file transfer is not used by Web Control app tests')
    },
    subscribeEvents() {
      return { close() {} }
    },
    async getConnectionInfo() {
      return {
        path: 'managed',
        connectionId: 'test-session',
        machineId: 'device-1',
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
    async disconnect() {},
  }
}

interface RecordedRequest {
  url: string
  method: string
  body?: unknown
}

class RecordingFetch {
  readonly requests: RecordedRequest[] = []
  private readonly responses: Response[]

  constructor(responses: Response[]) {
    this.responses = [...responses]
  }

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    this.requests.push({
      url: String(input),
      method: init.method ?? 'GET',
      ...(typeof init.body === 'string' ? { body: JSON.parse(init.body) } : {}),
    })
    const response = this.responses.shift()
    if (!response) {
      throw new Error(`unexpected request to ${String(input)}`)
    }
    return response
  }
}

class MemoryStorage implements Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> {
  private readonly values = new Map<string, string>()

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
  }

  removeItem(key: string): void {
    this.values.delete(key)
  }

  dump(): Record<string, string> {
    return Object.fromEntries(this.values)
  }
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

function pairPayload({ machineId, name }: { machineId: string; name: string }): Record<string, unknown> {
  return {
    type: 'termx_pair_v2',
    schema_version: 2,
    machine: {
      id: machineId,
      name,
      hostname: 'redmibook',
    },
    addresses: {
      local: ['http://127.0.0.1:18888'],
      lan: [],
      public: ['http://114.66.58.243:8447'],
    },
    endpoints: {
      web_control: 'http://114.66.58.243:12306',
      hub: 'http://114.66.58.243:8447',
      local_pairing: 'http://127.0.0.1:18888/api/local/pair',
    },
    pairing: {
      session_id: 'pair-session-1',
      secret: 'pair-secret-1',
    },
    bootstrap: {},
    preferred_path: 'public_p2p',
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

function createMockCrypto() {
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
      return new Uint8Array(Array.from({ length }, (_, index) => index + 1))
    },
    async sha256() {
      return new Uint8Array(32)
    },
  }
}
