import { afterEach, describe, expect, it, vi } from 'vitest'
import { createBrowserRemoteNetworkRuntime } from '../connection/browserNetworkRuntime'
import {
  createManagedHubApi,
  type ManagedHubFetch,
} from './managedHubApi'
import source from './managedHubApi.ts?raw'

describe('ManagedHubApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches ICE servers before creating a managed WebRTC offer', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        path: 'cloud',
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        ice_servers: [
          { urls: ['stun:hub.termx.test:3478'] },
          { urls: ['turn:hub.termx.test:3478?transport=udp'], username: 'u', credential: 'p' },
        ],
        relay_policy: {
          allow_relay: true,
          allow_relay_transfer: false,
        },
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test/root/',
      accessToken: 'hub-access-token',
      fetch: fetch.fetch,
    })

    await expect(api.getSessionIce({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
    })).resolves.toEqual({
      path: 'managed',
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      iceServers: [
        { urls: ['stun:hub.termx.test:3478'] },
        { urls: ['turn:hub.termx.test:3478?transport=udp'], username: 'u', credential: 'p' },
      ],
      relayPolicy: {
        allowRelay: true,
        allowRelayTransfer: false,
      },
    })
    expect(fetch.requests).toEqual([{
      method: 'POST',
      url: 'https://hub.termx.test/root/api/v1/sessions/ice',
      headers: {
        authorization: 'Bearer hub-access-token',
        'content-type': 'application/json',
      },
      body: {
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        session_token: 'session-token-1',
      },
    }])
  })

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
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      offer: {
        sessionId: 'rtc-managed-1',
        sdp: 'offer-sdp',
        iceCandidates: ['candidate:offer 1 udp 1 192.0.2.1 1 typ host'],
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
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        session_token: 'session-token-1',
        offer: {
          session_id: 'rtc-managed-1',
          sdp: 'offer-sdp',
          ice_candidates: ['candidate:offer 1 udp 1 192.0.2.1 1 typ host'],
        },
      },
    }])
  })

  it('preserves offer SDP exactly while sending the session token separately', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(202, {
        session_id: 'rtc-managed-1',
        path: 'cloud',
        machine_id: 'machine-1',
        pending: true,
        relay_policy: { allow_relay: false, allow_relay_transfer: false },
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch: fetch.fetch,
    })
    const signedSDP = 'v=0\r\ns=-\r\n'

    await api.createSession({
      machineId: 'machine-1',
      sessionToken: 'session-token-1',
      offer: {
        sessionId: 'rtc-managed-1',
        sdp: signedSDP,
        iceCandidates: [],
      },
    })

    expect(fetch.requests[0]?.body).toMatchObject({
      session_token: 'session-token-1',
      offer: {
        sdp: signedSDP,
      },
    })
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
        machine_id: 'machine-1',
      },
    })
  })

  it('accepts cloud path and empty terminal id for machine-scoped runtime API sessions', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        session_id: 'rtc-managed-list-1',
        path: 'cloud',
        machine_id: 'machine-1',
        answer: {
          sdp: 'machine-answer-sdp',
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

    await expect(api.createSession({
      ...validSessionInput(),
      terminalId: '',
      offer: {
        sessionId: 'rtc-managed-list-1',
        sdp: 'offer-sdp',
        iceCandidates: [],
      },
    })).resolves.toMatchObject({
      sessionId: 'rtc-managed-list-1',
      path: 'managed',
      machineId: 'machine-1',
      answer: {
        type: 'answer',
        sdp: 'machine-answer-sdp',
      },
    })
    expect(fetch.requests[0]?.body).toMatchObject({
      machine_id: 'machine-1',
      terminal_id: '',
    })
  })

  it('requires a session token before making a Hub request', async () => {
    const fetch = new RecordingFetch([])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch: fetch.fetch,
    })

    await expect(api.createSession({
      ...validSessionInput(),
      sessionToken: ' ',
    })).rejects.toThrow(/session_token.*required/i)
    expect(fetch.requests).toEqual([])
  })

  it('surfaces Hub error envelopes without exposing runtime transport concepts', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(403, {
        error: {
          code: 'managed_session_rejected',
          message: 'managed session expired',
        },
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch: fetch.fetch,
    })

    await expect(api.createSession(validSessionInput())).rejects.toThrow(/managed_session_rejected.*expired/i)
  })

  it('translates browser fetch failures into actionable Hub reachability errors', async () => {
    const fetch: ManagedHubFetch = async () => {
      throw new TypeError('Failed to fetch')
    }
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch,
    })

    await expect(api.createSession(validSessionInput())).rejects.toThrow(/Cannot reach Managed Hub at https:\/\/hub\.termx\.test.*CORS/i)
    await expect(api.createSession(validSessionInput())).rejects.not.toThrow(/^Failed to fetch$/)
  })

  it('claims a pairing code through the Hub while leaving secret validation to the agent', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        claim_id: 'claim-1',
        machine_id: 'machine-1',
        machine_name: 'RedmiBook',
        session_token: 'session-token-pair',
        expires_at: '2027-05-04T10:30:00Z',
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch: fetch.fetch,
    })

    await expect(api.pair({
      machineId: 'machine-1',
      pairSessionId: 'pair-session-1',
      pairSecret: 'pair-secret-1',
      appDeviceId: 'appweb-1',
      appName: 'TermX Remote App',
      requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
    })).resolves.toEqual({
      claimId: 'claim-1',
      machineId: 'machine-1',
      machineName: 'RedmiBook',
      sessionToken: 'session-token-pair',
      expiresAt: '2027-05-04T10:30:00Z',
    })
    expect(fetch.requests).toEqual([{
      method: 'POST',
      url: 'https://hub.termx.test/api/v1/pairing/claims',
      headers: {
        'content-type': 'application/json',
      },
      body: {
        machine_id: 'machine-1',
        pair_session_id: 'pair-session-1',
        pair_secret: 'pair-secret-1',
        app_device_id: 'appweb-1',
        app_name: 'TermX Remote App',
        requested_capabilities: ['terminal', 'file_manager', 'terminal_management'],
      },
    }])
  })

  it('normalizes a pairing claim endpoint base URL without duplicating the path', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(200, {
        claim_id: 'claim-1',
        machine_id: 'machine-1',
        session_token: 'session-token-pair',
        expires_at: '2027-05-04T10:30:00Z',
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'http://127.0.0.1:18888/api/v1/pairing/claims',
      fetch: fetch.fetch,
    })

    await api.pair({
      machineId: 'machine-1',
      pairSessionId: 'pair-session-1',
      pairSecret: 'pair-secret-1',
      appDeviceId: 'appweb-1',
      appName: 'TermX Remote App',
      requestedCapabilities: ['terminal'],
    })

    expect(fetch.requests[0]?.url).toBe('http://127.0.0.1:18888/api/v1/pairing/claims')
  })

  it('surfaces pending pairing claims as an actionable agent-not-ready error', async () => {
    const fetch = new RecordingFetch([
      jsonResponse(202, {
        claim_id: 'claim-1',
        machine_id: 'machine-1',
        pending: true,
      }),
    ])
    const api = createManagedHubApi({
      baseUrl: 'https://hub.termx.test',
      fetch: fetch.fetch,
    })

    await expect(api.pair({
      machineId: 'machine-1',
      pairSessionId: 'pair-session-1',
      pairSecret: 'pair-secret-1',
      appDeviceId: 'appweb-1',
      appName: 'TermX Remote App',
      requestedCapabilities: ['terminal'],
    })).rejects.toThrow(/pairing is pending.*agent is online/i)
  })

  it('uses the browser runtime fetch binding when injected from the browser adapter', async () => {
    const calls: Array<{ thisValue: unknown; input: string }> = []
    const boundFetch = function (this: unknown, input: RequestInfo | URL) {
      calls.push({ thisValue: this, input: String(input) })
      if (this !== globalThis) {
        throw new TypeError("Failed to execute 'fetch' on 'Window': Illegal invocation")
      }
      return Promise.resolve(jsonResponse(202, {
        session_id: 'rtc-managed-1',
        path: 'managed',
        machine_id: 'machine-1',
        terminal_id: 'terminal-1',
        pending: true,
      }))
    }
    vi.stubGlobal('fetch', boundFetch)
    const runtime = createBrowserRemoteNetworkRuntime()
    const api = createManagedHubApi({ baseUrl: 'https://hub.termx.test', fetch: runtime.fetch })

    await expect(api.createSession(validSessionInput())).resolves.toMatchObject({
      sessionId: 'rtc-managed-1',
      pending: true,
    })
    expect(calls).toEqual([{
      thisValue: globalThis,
      input: 'https://hub.termx.test/api/v1/sessions',
    }])
  })

  it('keeps managed Hub API as signaling/control, not runtime transport', () => {
    expect(source).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket/)
    expect(source).not.toMatch(/openTerminal\(|openApi\(|openFileTransfer\(|subscribeEvents\(/)
    expect(source).not.toMatch(/paid_relay|anonymous_p2p|managed_p2p|relayTransport|path:\s*['"]relay['"]/)
  })
})

function validSessionInput() {
  return {
    machineId: 'machine-1',
    terminalId: 'terminal-1',
    sessionToken: 'session-token-1',
    offer: {
      sessionId: 'rtc-managed-1',
      sdp: 'offer-sdp',
      iceCandidates: [],
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
