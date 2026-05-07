import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { WebControlRemoteApp } from './WebControlRemoteApp'
import type { RemoteNetworkRuntime, RemoteRuntimeStorage, RtcSession } from './transport'
import type { WebControlFetch } from './webControlApi'

describe('WebControlRemoteApp', () => {
  afterEach(() => cleanup())

  it('defaults to the App-first machine home and keeps Web Control URL in settings', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.controlUrl', 'http://127.0.0.1:5174')

    render(
      <WebControlRemoteApp
        managedRtcSessionFactory={fakeManagedRtcSessionFactory}
        networkRuntime={testNetworkRuntime(fetchNoRequests)}
        storage={storage}
      />,
    )

    expect(screen.getByTestId('termx-app-home')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Machines' })).toBeTruthy()
    expect(screen.getByText('Sign in to view your devices.')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /open settings/i }))

    expect(screen.getByTestId('termx-app-settings')).toBeTruthy()
    expect((screen.getByLabelText(/web control/i) as HTMLInputElement).value).toBe('')
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
          control_url: 'http://114.66.58.243:12306',
          hub_urls: ['http://114.66.58.243:8447'],
          hub_status: 'online',
        }],
      }),
      jsonResponse(200, {
        claim_id: 'claim-1',
        machine_id: 'device-1',
        machine_name: 'RedmiBook',
        session_token: 'session-token-device-1',
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
                  rtcOfferUrl: machine.hubUrls[0] ?? '',
                },
              }
            },
            listTerminals,
          },
          connector: { connect },
        })}
        managedRtcSessionFactory={fakeManagedRtcSessionFactory}
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
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
      requested_capabilities: ['terminal', 'file_manager', 'terminal_management'],
    })
    const stored = JSON.parse(storage.getItem('termx.app.machines.v1') ?? '[]') as Array<Record<string, unknown>>
    expect(stored).toHaveLength(1)
    expect(stored[0]?.machineId).toBe('device-1')
    expect(stored[0]?.source).toBe('cloud')
    expect(stored[0]?.preferredPath).toBe('public_p2p')
    expect(storage.getItem('termx.session.device-1.token')).toBe('session-token-device-1')
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
    render(
      <WebControlRemoteApp
        defaultControlUrl="http://114.66.58.243:12306"
        managedRtcSessionFactory={fakeManagedRtcSessionFactory}
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
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
  })

  it('opens paired Web Control machines by racing all hub_urls instead of only the first', async () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.accessToken', 'access-token-1')
    storage.setItem('termx.session.device-1.token', 'session-token-device-1')
    storage.setItem('termx.app.machines.v1', JSON.stringify([{
      machineId: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      state: 'online',
      terminalCount: 0,
      source: 'cloud',
      addresses: {
        local: [],
        lan: [],
        public: ['https://hub-1.termx.test', 'https://hub-2.termx.test'],
      },
      endpoints: {
        webControl: 'http://114.66.58.243:12306',
        hub: 'https://hub-1.termx.test',
      },
      schemaVersion: 2,
      addedAt: '2026-05-05T10:00:00.000Z',
      updatedAt: '2026-05-05T10:00:00.000Z',
    }]))
    const fetch = new WebControlHubFallbackFetch()

    render(
      <WebControlRemoteApp
        defaultControlUrl="http://114.66.58.243:12306"
        managedRtcSessionFactory={fakeManagedRtcSessionFactory}
        networkRuntime={testNetworkRuntime(fetch.fetch, storage)}
        storage={storage}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
    await userEvent.click(screen.getByRole('button', { name: /open redmibook/i }))
    await waitFor(() => expect(screen.getByText('zsh')).toBeTruthy())

    const sessionRequests = fetch.requests.filter((request) => request.url.endsWith('/api/v1/sessions'))
    expect(sessionRequests).toHaveLength(2)
    expect(new Set(sessionRequests.map((request) => request.url))).toEqual(new Set([
      'https://hub-1.termx.test/api/v1/sessions',
      'https://hub-2.termx.test/api/v1/sessions',
    ]))
    expect(sessionRequests.map((request) => request.body)).toEqual(expect.arrayContaining([
      expect.objectContaining({
        machine_id: 'device-1',
        session_token: 'session-token-device-1',
      }),
      expect.objectContaining({
        machine_id: 'device-1',
        session_token: 'session-token-device-1',
      }),
    ]))
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

let nextFakeManagedSessionId = 0

const fakeManagedRtcSessionFactory = (target?: { machineId?: string | undefined; terminalId?: string | undefined }) => ({
  async createOffer() {
    nextFakeManagedSessionId += 1
    return {
      sessionId: `rtc-web-control-${nextFakeManagedSessionId}`,
      description: { type: 'offer' as const, sdp: `offer-sdp-${nextFakeManagedSessionId}` },
    }
  },
  async acceptAnswer() {},
  async openTerminal() {
    throw new Error('terminal channel is not used by Web Control app tests')
  },
  async openApi() {
    return {
      async request<TResponse>(method: string): Promise<TResponse> {
        if (method !== 'list') throw new Error(`unexpected api request ${method}`)
        return {
          terminals: [{
            terminal_id: 'terminal-1',
            title: 'zsh',
            state: 'running',
            command: '/bin/zsh -l',
            cols: 100,
            rows: 30,
          }],
        } as TResponse
      },
      close() {},
    }
  },
  async openFileTransfer() {
    throw new Error('file transfer is not used by Web Control app tests')
  },
  subscribeEvents() {
    return { close() {} }
  },
  async getConnectionInfo() {
    return {
      path: 'managed' as const,
      connectionId: 'test-session',
      machineId: target?.machineId ?? 'device-1',
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
}) satisfies RtcSession & {
  createOffer(): Promise<{ sessionId: string; description: { type: 'offer'; sdp: string } }>
  acceptAnswer(): Promise<void>
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

class WebControlHubFallbackFetch {
  readonly requests: RecordedRequest[] = []

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const url = String(input)
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined
    this.requests.push({
      url,
      method: init.method ?? 'GET',
      ...(body !== undefined ? { body } : {}),
    })
    if (url === 'http://114.66.58.243:12306/api/v1/auth/me') {
      return jsonResponse(200, {
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      })
    }
    if (url === 'http://114.66.58.243:12306/api/v1/machines') {
      return jsonResponse(200, {
        machines: [{
          id: 'device-1',
          name: 'RedmiBook',
          hostname: 'redmibook',
          online: true,
          paired: true,
          source: 'cloud',
          control_url: 'http://114.66.58.243:12306',
          hub_urls: ['https://hub-1.termx.test', 'https://hub-2.termx.test'],
          hub_status: 'online',
        }],
      })
    }
    if (url === 'https://hub-1.termx.test/api/v1/sessions/ice') {
      return jsonResponse(200, {
        path: 'managed',
        machine_id: 'device-1',
        ice_servers: [],
        relay_policy: { allow_relay: true, allow_relay_transfer: false },
      })
    }
    if (url === 'https://hub-1.termx.test/api/v1/sessions') {
      return jsonResponse(503, {
        error: {
          code: 'hub_unavailable',
          message: 'first hub unavailable',
        },
      })
    }
    if (url === 'https://hub-2.termx.test/api/v1/sessions/ice') {
      const request = body as {
        machine_id: string
        terminal_id?: string | undefined
      }
      return jsonResponse(200, {
        path: 'managed',
        machine_id: request.machine_id,
        ...(request.terminal_id ? { terminal_id: request.terminal_id } : {}),
        ice_servers: [],
        relay_policy: { allow_relay: true, allow_relay_transfer: false },
      })
    }
    if (url === 'https://hub-2.termx.test/api/v1/sessions') {
      const request = body as {
        machine_id: string
        terminal_id?: string | undefined
        offer: { session_id: string }
      }
      return jsonResponse(200, {
        session_id: request.offer.session_id,
        path: 'managed',
        machine_id: request.machine_id,
        ...(request.terminal_id ? { terminal_id: request.terminal_id } : {}),
        answer: { sdp: 'answer-sdp', ice_candidates: [] },
        ice_servers: [],
        relay_policy: { allow_relay: true, allow_relay_transfer: true },
        relay_in_use: true,
      })
    }
    throw new Error(`unexpected request to ${url}`)
  }
}

class MemoryStorage implements RemoteRuntimeStorage {
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

const fetchNoRequests: WebControlFetch = async (input) => {
  throw new Error(`unexpected request to ${String(input)}`)
}

function testNetworkRuntime(fetch: WebControlFetch, storage?: RemoteRuntimeStorage | undefined): RemoteNetworkRuntime {
  return {
    fetch,
    ...(storage ? { storage } : {}),
    queryParam() {
      return null
    },
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
      local_pairing: 'http://127.0.0.1:18888/api/v1/pairing/claims',
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
