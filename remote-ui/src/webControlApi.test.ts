import { afterEach, describe, expect, it, vi } from 'vitest'
import { createBrowserRemoteNetworkRuntime } from './browserNetworkRuntime'
import {
  createWebControlApi,
  type WebControlFetch,
} from './webControlApi'
import source from './webControlApi.ts?raw'

describe('WebControlApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('logs in and lists Web Control machines without terminal inventory', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        token_type: 'Bearer',
        access_token: 'access-token-1',
        refresh_token: '',
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
          role: 'user',
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
          os_info: 'linux/amd64',
          online: true,
          paired: false,
          source: 'cloud',
          public_key: 'machine-public-key',
          machine_public_key_fingerprint: 'sha256:machine',
          control_url: 'http://114.66.58.243:12306',
          hub_id: 'termx-hub-1',
          hub_http_url: 'http://114.66.58.243:8447',
          hub_status: 'online',
          last_seen: '2026-05-04T03:08:00.000Z',
        }],
      }),
    ])
    const loginApi = createWebControlApi({
      baseUrl: 'http://114.66.58.243:12306',
      fetch: fetch.fetch,
    })

    const auth = await loginApi.login({
      login: 'lozzow@example.test',
      password: 'secret',
    })
    const authedApi = createWebControlApi({
      baseUrl: 'http://114.66.58.243:12306',
      accessToken: auth.accessToken,
      fetch: fetch.fetch,
    })
    const me = await authedApi.me()
    const machines = await authedApi.listMachines()

    expect(auth.accessToken).toBe('access-token-1')
    expect(me.email).toBe('lozzow@example.test')
    expect(machines).toEqual([{
      id: 'device-1',
      name: 'RedmiBook',
      hostname: 'redmibook',
      osInfo: 'linux/amd64',
      online: true,
      paired: false,
      source: 'cloud',
      publicKey: 'machine-public-key',
      machinePublicKeyFingerprint: 'sha256:machine',
      controlUrl: 'http://114.66.58.243:12306',
      hubId: 'termx-hub-1',
      hubHttpUrl: 'http://114.66.58.243:8447',
      hubStatus: 'online',
      lastSeen: '2026-05-04T03:08:00.000Z',
    }])
    expect(fetch.requests.map((request) => [request.method, request.url])).toEqual([
      ['POST', 'http://114.66.58.243:12306/api/v1/auth/login'],
      ['GET', 'http://114.66.58.243:12306/api/v1/auth/me'],
      ['GET', 'http://114.66.58.243:12306/api/v1/machines'],
    ])
  })

  it('creates machine-scoped cloud connection tickets for Hub WebRTC sessions', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        connect_ticket: 'ticket-1',
        path: 'cloud',
        machine_id: 'device-1',
        terminal_id: '',
        allow_relay: false,
        relay_in_use: false,
        relay_throttled: false,
        hub_http_url: 'http://114.66.58.243:8447',
        expires_at: '2026-05-04T12:30:00Z',
      }),
    ])
    const api = createWebControlApi({
      baseUrl: 'http://114.66.58.243:12306',
      accessToken: 'access-token-1',
      fetch: fetch.fetch,
    })

    await expect(api.createConnectTicket({
      machineId: 'device-1',
    })).resolves.toEqual({
      connectTicket: 'ticket-1',
      machineId: 'device-1',
      path: 'cloud',
      allowRelay: false,
      relayInUse: false,
      relayThrottled: false,
      hubHttpUrl: 'http://114.66.58.243:8447',
      expiresAt: '2026-05-04T12:30:00Z',
    })
    expect(fetch.requests).toEqual([{
      method: 'POST',
      url: 'http://114.66.58.243:12306/api/v1/machines/device-1/connect-tickets',
      headers: {
        authorization: 'Bearer access-token-1',
        'content-type': 'application/json',
      },
      body: {
        terminal_id: '',
      },
    }])
  })

  it('requires an access token before authenticated Web Control requests', async () => {
    const fetch = new RecordingFetch([])
    expect(() => createWebControlApi({
      baseUrl: 'https://control.termx.test',
      accessToken: '   ',
      fetch: fetch.fetch,
    })).toThrow(/access token.*non-empty/i)

    const api = createWebControlApi({
      baseUrl: 'https://control.termx.test',
      fetch: fetch.fetch,
    })
    await expect(api.me()).rejects.toThrow(/access token.*required/i)

    expect(fetch.requests).toEqual([])
  })

  it('surfaces HTTP error envelopes', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(403, {
        error: {
          code: 'machine_access_denied',
          message: 'machine is not owned by user',
        },
      }),
    ])
    const api = createWebControlApi({
      baseUrl: 'https://control.termx.test',
      accessToken: 'access-token-1',
      fetch: fetch.fetch,
    })

    await expect(api.listMachines()).rejects.toThrow(/machine_access_denied.*machine is not owned/i)
  })

  it('uses the browser runtime fetch binding when injected from the browser adapter', async () => {
    const calls: Array<{ thisValue: unknown; input: string }> = []
    const boundFetch = function (this: unknown, input: RequestInfo | URL) {
      calls.push({ thisValue: this, input: String(input) })
      if (this !== globalThis) {
        throw new TypeError("Failed to execute 'fetch' on 'Window': Illegal invocation")
      }
      return Promise.resolve(jsonResponse(200, {
        token_type: 'Bearer',
        access_token: 'access-token-1',
        user: {
          id: 'user-1',
          username: 'lozzow',
          email: 'lozzow@example.test',
        },
      }))
    }
    vi.stubGlobal('fetch', boundFetch)
    const runtime = createBrowserRemoteNetworkRuntime()
    const api = createWebControlApi({ baseUrl: 'https://control.termx.test', fetch: runtime.fetch })

    await expect(api.login({ login: 'lozzow@example.test', password: 'secret' })).resolves.toMatchObject({
      accessToken: 'access-token-1',
    })

    expect(calls).toEqual([{
      thisValue: globalThis,
      input: 'https://control.termx.test/api/v1/auth/login',
    }])
  })

  it('keeps Web Control API as account and node management only', () => {
    expect(source).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket/)
    expect(source).not.toMatch(/openTerminal\(|openApi\(|openFileTransfer\(|subscribeEvents\(/)
    expect(source).not.toMatch(/public-p2p/)
    expect(source).not.toMatch(/paid_relay|anonymous_p2p|managed_p2p|relayTransport|path:\s*['"]relay['"]/)
  })
})

interface RecordedRequest {
  url: string
  method: string
  headers: Record<string, string>
  body: unknown
}

class RecordingFetch {
  readonly requests: RecordedRequest[] = []
  private readonly responses: Response[]

  constructor(responses: Response[]) {
    this.responses = [...responses]
  }

  readonly fetch: WebControlFetch = async (input, init = {}) => {
    const headers = lowerHeaders(init.headers)
    this.requests.push({
      url: String(input),
      method: init.method ?? 'GET',
      headers,
      body: init.body ? JSON.parse(String(init.body)) : undefined,
    })
    const response = this.responses.shift()
    if (!response) {
      throw new Error(`unexpected request to ${String(input)}`)
    }
    return response
  }
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

function lowerHeaders(headers: HeadersInit | undefined): Record<string, string> {
  const result: Record<string, string> = {}
  const normalized = new Headers(headers)
  normalized.forEach((value, key) => {
    result[key] = value
  })
  return result
}
