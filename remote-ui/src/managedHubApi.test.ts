import { describe, expect, it } from 'vitest'
import {
  createManagedHubApi,
  type ManagedHubFetch,
} from './managedHubApi'
import source from './managedHubApi.ts?raw'

describe('ManagedHubApi', () => {
  it('submits a managed WebRTC offer to the Hub session endpoint and returns the answer', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        session_id: 'rtc-managed-1',
        path: 'managed',
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        answer: {
          sdp: 'answer-sdp',
          ice_candidates: ['candidate:answer 1 udp 1 192.0.2.10 1 typ host'],
        },
        ice_servers: [
          { urls: ['stun:hub.termx.test:3478'] },
          { urls: ['turn:hub.termx.test:3478?transport=udp'], username: 'u', credential: 'p' },
        ],
        relay_policy: {
          allow_relay: true,
          allow_relay_transfer: false,
        },
        relay_in_use: true,
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test/root/',
      accessToken: 'hub-access-token',
      fetch: fetch.fetch,
    })

    const session = await api.createSession({
      connectTicket: 'ticket-1',
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      appCertificate: { payload: { machine_id: 'machine-1' }, signature: 'cert-sig' },
      offer: {
        sessionId: 'rtc-managed-1',
        sdp: 'offer-sdp',
        iceCandidates: ['candidate:offer 1 udp 1 192.0.2.1 1 typ host'],
      },
      signature: {
        algorithm: 'ed25519',
        nonce: 'nonce-1',
        timestamp: 1770000000,
        value: 'offer-sig',
      },
    })

    expect(session).toEqual({
      sessionId: 'rtc-managed-1',
      path: 'managed',
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      answer: {
        type: 'answer',
        sdp: 'answer-sdp',
      },
      iceCandidates: ['candidate:answer 1 udp 1 192.0.2.10 1 typ host'],
      iceServers: [
        { urls: ['stun:hub.termx.test:3478'] },
        { urls: ['turn:hub.termx.test:3478?transport=udp'], username: 'u', credential: 'p' },
      ],
      relayPolicy: {
        allowRelay: true,
        allowRelayTransfer: false,
      },
      relayInUse: true,
    })
    expect(fetch.requests).toEqual([{
      method: 'POST',
      url: 'https://hub.termx.test/root/api/v1/sessions',
      headers: {
        authorization: 'Bearer hub-access-token',
        'content-type': 'application/json',
      },
      body: {
        connect_ticket: 'ticket-1',
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        app_certificate: { payload: { machine_id: 'machine-1' }, signature: 'cert-sig' },
        offer: {
          session_id: 'rtc-managed-1',
          sdp: 'offer-sdp',
          ice_candidates: ['candidate:offer 1 udp 1 192.0.2.1 1 typ host'],
        },
        signature: {
          algorithm: 'ed25519',
          nonce: 'nonce-1',
          timestamp: 1770000000,
          value: 'offer-sig',
        },
      },
    }])
  })

  it('fails closed when Hub returns relay as a client path', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        session_id: 'rtc-managed-1',
        path: 'relay',
        machine_id: 'machine-1',
        answer: { sdp: 'answer-sdp' },
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch: fetch.fetch,
    })

    await expect(api.createSession(validSessionInput())).rejects.toThrow(/path.*managed/i)
  })

  it('returns a recoverable pending result when Hub accepted the offer but answer is not ready', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(202, {
        session_id: 'rtc-managed-1',
        path: 'managed',
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        pending: true,
      }),
      jsonResponse(200, {
        session_id: 'rtc-managed-1',
        path: 'managed',
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        answer: {
          sdp: 'answer-after-pending',
          ice_candidates: [],
        },
        relay_policy: {
          allow_relay: false,
          allow_relay_transfer: false,
        },
        relay_in_use: false,
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch: fetch.fetch,
    })

    await expect(api.createSession(validSessionInput())).resolves.toEqual({
      sessionId: 'rtc-managed-1',
      path: 'managed',
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      pending: true,
    })
    await expect(api.pollSessionAnswer({
      sessionId: 'rtc-managed-1',
      connectTicket: 'ticket-1',
      machineId: 'machine-1',
    })).resolves.toMatchObject({
      sessionId: 'rtc-managed-1',
      path: 'managed',
      machineId: 'machine-1',
      answer: { type: 'answer', sdp: 'answer-after-pending' },
      relayPolicy: { allowRelay: false, allowRelayTransfer: false },
      relayInUse: false,
    })
    expect(fetch.requests[1]).toMatchObject({
      method: 'POST',
      url: 'https://hub.termx.test/api/v1/sessions/rtc-managed-1/answer',
      body: {
        connect_ticket: 'ticket-1',
        machine_id: 'machine-1',
      },
    })
  })

  it('requires a connect ticket and terminal id before making a Hub request', async () => {
    const fetch = new RecordingFetch([])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch: fetch.fetch,
    })

    await expect(api.createSession({
      ...validSessionInput(),
      connectTicket: ' ',
    })).rejects.toThrow(/connect ticket.*required/i)
    await expect(api.createSession({
      ...validSessionInput(),
      terminalId: ' ',
    })).rejects.toThrow(/terminal.*required/i)
    expect(fetch.requests).toEqual([])
  })

  it('surfaces Hub error envelopes without exposing runtime transport concepts', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(403, {
        error: {
          code: 'managed_ticket_rejected',
          message: 'managed ticket expired',
        },
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch: fetch.fetch,
    })

    await expect(api.createSession(validSessionInput())).rejects.toThrow(/managed_ticket_rejected.*expired/i)
  })

  it('keeps managed Hub API as signaling/control, not runtime transport', () => {
    expect(source).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket/)
    expect(source).not.toMatch(/openTerminal\(|openApi\(|openFileTransfer\(|subscribeEvents\(/)
    expect(source).not.toMatch(/paid_relay|anonymous_p2p|managed_p2p|relayTransport|path:\s*['"]relay['"]/)
  })
})

function validSessionInput() {
  return {
    connectTicket: 'ticket-1',
    machineId: 'machine-1',
    terminalId: 'terminal-1',
    appCertificate: { payload: { machine_id: 'machine-1' }, signature: 'cert-sig' },
    offer: {
      sessionId: 'rtc-managed-1',
      sdp: 'offer-sdp',
      iceCandidates: [],
    },
    signature: {
      algorithm: 'ed25519',
      nonce: 'nonce-1',
      timestamp: 1770000000,
      value: 'offer-sig',
    },
  }
}

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

  readonly fetch: ManagedHubFetch = async (input, init = {}) => {
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
