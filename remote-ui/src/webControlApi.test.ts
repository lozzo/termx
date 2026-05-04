import { describe, expect, it } from 'vitest'
import {
  createWebControlApi,
  type WebControlFetch,
} from './webControlApi'
import source from './webControlApi.ts?raw'

describe('WebControlApi', () => {
  it('implements authenticated public_p2p rendezvous using Web Control HTTP endpoints', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(201, {
        channel_id: 'rv_1',
        channel_secret: 'secret-1',
        path: 'public_p2p',
        public_stun_servers: ['stun:one.termx.test:3478'],
      }),
      jsonResponse(202, {}),
      jsonResponse(200, {
        messages: [{
          type: 'answer',
          from: 'machine-1',
          payload: {
            answer: { session_id: 'rtc-1', sdp: 'answer-sdp' },
            signature: { value: 'answer-sig' },
          },
        }],
      }),
      jsonResponse(202, {}),
    ])
    const api = createWebControlApi({
      baseUrl: 'https://control.termx.test/root/',
      accessToken: 'access-token-1',
      fetch: fetch.fetch,
    })

    const channel = await api.createChannel({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      ttlSeconds: 600,
    })
    await api.postOffer({
      channelId: channel.channelId,
      channelSecret: channel.channelSecret,
      from: 'app-device-1',
      appPublicKey: 'app-public-1',
      appCertificate: { payload: { app_public_key: 'app-public-1' } },
      offer: { session_id: 'rtc-1', sdp: 'offer-sdp', ice_candidates: [] },
      signature: { value: 'offer-sig' },
    })
    const messages = await api.pollEvents({
      channelId: channel.channelId,
      channelSecret: channel.channelSecret,
    })
    await api.postCandidate({
      channelId: channel.channelId,
      channelSecret: channel.channelSecret,
      appPublicKey: 'app-public-1',
      candidate: { candidate: 'candidate:1 1 udp 1 192.0.2.1 1 typ host', mid: '0', mline_index: 0 },
    })

    expect(channel).toEqual({
      channelId: 'rv_1',
      channelSecret: 'secret-1',
      publicStunServers: ['stun:one.termx.test:3478'],
    })
    expect(JSON.stringify(channel)).not.toMatch(/turn:|credential|username/i)
    expect(messages).toEqual([{
      type: 'answer',
      from: 'machine-1',
      payload: {
        answer: { session_id: 'rtc-1', sdp: 'answer-sdp' },
        signature: { value: 'answer-sig' },
      },
    }])
    expect(fetch.requests.map((request) => [request.method, request.url])).toEqual([
      ['POST', 'https://control.termx.test/root/api/v1/public-p2p/channels'],
      ['POST', 'https://control.termx.test/root/api/v1/public-p2p/channels/rv_1/offer'],
      ['GET', 'https://control.termx.test/root/api/v1/public-p2p/channels/rv_1/events'],
      ['POST', 'https://control.termx.test/root/api/v1/public-p2p/channels/rv_1/candidate'],
    ])
    const createRequest = fetch.requests[0]
    const offerRequest = fetch.requests[1]
    const eventsRequest = fetch.requests[2]
    const candidateRequest = fetch.requests[3]
    expect(createRequest).toBeDefined()
    expect(offerRequest).toBeDefined()
    expect(eventsRequest).toBeDefined()
    expect(candidateRequest).toBeDefined()
    expect(createRequest!.headers).toMatchObject({
      authorization: 'Bearer access-token-1',
      'content-type': 'application/json',
    })
    expect(createRequest!.body).toEqual({
      machine_id: 'machine-1',
      terminal_id: 'terminal-1',
      machine_public_key_fingerprint: 'sha256:machine',
      ttl_seconds: 600,
    })
    expect(offerRequest!.headers).not.toHaveProperty('authorization')
    expect(offerRequest!.body).toEqual({
      channel_secret: 'secret-1',
      app_certificate: { payload: { app_public_key: 'app-public-1' } },
      offer: { session_id: 'rtc-1', sdp: 'offer-sdp', ice_candidates: [] },
      signature: { value: 'offer-sig' },
    })
    expect(JSON.stringify(offerRequest!.body)).not.toMatch(/ice_servers/)
    expect(eventsRequest!.headers).toMatchObject({
      authorization: 'Rendezvous rv_1:secret-1',
    })
    expect(candidateRequest!.body).toEqual({
      channel_secret: 'secret-1',
      app_public_key: 'app-public-1',
      candidate: { candidate: 'candidate:1 1 udp 1 192.0.2.1 1 typ host', mid: '0', mline_index: 0 },
    })
  })

  it('rejects public_p2p channel responses that include TURN credentials', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(201, {
        channel_id: 'rv_turn',
        channel_secret: 'secret-1',
        public_stun_servers: ['stun:one.termx.test:3478', 'turn:turn.termx.test:3478'],
      }),
    ])
    const api = createWebControlApi({
      baseUrl: 'https://control.termx.test',
      accessToken: 'access-token-1',
      fetch: fetch.fetch,
    })

    await expect(api.createChannel({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      ttlSeconds: 600,
    })).rejects.toThrow(/turn.*public_p2p/i)
  })

  it('requires terminal_id before posting public_p2p channel creation', async () => {
    const fetch = new RecordingFetch([])
    const api = createWebControlApi({
      baseUrl: 'https://control.termx.test',
      accessToken: 'access-token-1',
      fetch: fetch.fetch,
    })

    await expect(api.createChannel({
      machineId: 'machine-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      ttlSeconds: 600,
    } as never)).rejects.toThrow(/terminal.*required/i)

    expect(fetch.requests).toEqual([])
  })

  it('creates managed connect tickets without modeling relay as a client path', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(201, {
        id: 'ticket-1',
        path: 'managed',
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        allow_relay: true,
        relay_in_use: false,
        relay_bytes_remaining: 1024,
        relay_throttled: false,
      }),
    ])
    const api = createWebControlApi({
      baseUrl: 'https://control.termx.test',
      accessToken: 'access-token-1',
      fetch: fetch.fetch,
    })

    const ticket = await api.createManagedConnectTicket({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      ttlSeconds: 60,
    })

    expect(ticket).toEqual({
      id: 'ticket-1',
      path: 'managed',
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      allowRelay: true,
      relayInUse: false,
      relayBytesRemaining: 1024,
      relayThrottled: false,
    })
    expect(fetch.requests).toHaveLength(1)
    expect(fetch.requests[0]).toMatchObject({
      method: 'POST',
      url: 'https://control.termx.test/api/v1/managed/connect-tickets',
      headers: {
        authorization: 'Bearer access-token-1',
        'content-type': 'application/json',
      },
      body: {
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        ttl_seconds: 60,
      },
    })
  })

  it('fails closed when managed ticket response uses relay as a path', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(201, {
        id: 'ticket-1',
        path: 'relay',
        machine_id: 'machine-1',
      }),
    ])
    const api = createWebControlApi({
      baseUrl: 'https://control.termx.test',
      accessToken: 'access-token-1',
      fetch: fetch.fetch,
    })

    await expect(api.createManagedConnectTicket({
      machineId: 'machine-1',
    })).rejects.toThrow(/managed.*path/i)
  })

  it('surfaces HTTP error envelopes without leaking runtime transport concepts', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(403, {
        error: {
          code: 'create_rendezvous_failed',
          message: 'machine is not owned by user',
        },
      }),
    ])
    const api = createWebControlApi({
      baseUrl: 'https://control.termx.test',
      accessToken: 'access-token-1',
      fetch: fetch.fetch,
    })

    await expect(api.createChannel({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      ttlSeconds: 600,
    })).rejects.toThrow(/create_rendezvous_failed.*machine is not owned/i)
  })

  it('requires an access token before making authenticated Web Control requests', async () => {
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

  it('logs in and lists Web Control machines for remote QR selection', async () => {
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
          machine_public_key_fingerprint: 'sha256:machine',
          preferred_path: 'public_p2p',
          control_url: 'http://114.66.58.243:12306',
          hub_id: 'termx-hub-1',
          hub_http_url: 'http://114.66.58.243:8447',
          hub_status: 'online',
          terminal_count: 1,
          terminal_ids: ['terminal-1'],
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
      machinePublicKeyFingerprint: 'sha256:machine',
      preferredPath: 'public_p2p',
      controlUrl: 'http://114.66.58.243:12306',
      hubId: 'termx-hub-1',
      hubHttpUrl: 'http://114.66.58.243:8447',
      hubStatus: 'online',
      terminalCount: 1,
      terminalIds: ['terminal-1'],
      lastSeen: '2026-05-04T03:08:00.000Z',
    }])
    expect(fetch.requests.map((request) => [request.method, request.url])).toEqual([
      ['POST', 'http://114.66.58.243:12306/api/v1/auth/login'],
      ['GET', 'http://114.66.58.243:12306/api/v1/auth/me'],
      ['GET', 'http://114.66.58.243:12306/api/v1/machines'],
    ])
  })

  it('keeps Web Control API as signaling/control, not runtime transport', () => {
    expect(source).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket/)
    expect(source).not.toMatch(/openTerminal\(|openApi\(|openFileTransfer\(|subscribeEvents\(/)
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
