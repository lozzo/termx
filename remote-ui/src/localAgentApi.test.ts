import { describe, expect, it, vi } from 'vitest'
import { createLocalAgentApi } from './localAgentApi'

describe('createLocalAgentApi', () => {
  it('normalizes local status and terminal inventory into machine/terminal models', async () => {
    const fetch = vi.fn(async (input: RequestInfo | URL) => responseFor(String(input), {
      '/api/local/status': {
        machine_id: 'machine-local',
        machine_name: 'Local Mac',
        remote_enabled: true,
        local_rtc: {
          http_url: 'http://127.0.0.1:18888',
          ice_tcp_enabled: true,
          ice_tcp_port: 18889,
        },
        updated_at: '2026-05-01T04:00:00Z',
      },
      '/api/local/terminals': {
        terminals: [{
          terminal_id: 'terminal-1',
          name: 'zsh',
          command: ['/bin/zsh', '-l'],
          cols: 120,
          rows: 36,
          state: 'running',
          last_active_at: '2026-05-01T04:01:00Z',
        }],
      },
    }))

    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888', fetch })
    await expect(api.getStatus()).resolves.toMatchObject({
      machine: {
        machineId: 'machine-local',
        name: 'Local Mac',
        state: 'online',
        localRTC: {
          signalingUrl: 'http://127.0.0.1:18888/api/local/rtc/offer',
          iceTCPUrl: 'tcp://127.0.0.1:18889',
        },
      },
      localWeb: {
        httpUrl: 'http://127.0.0.1:18888',
        rtcOfferUrl: 'http://127.0.0.1:18888/api/local/rtc/offer',
      },
    })
    await expect(api.listTerminals()).resolves.toEqual([
      expect.objectContaining({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        title: 'zsh',
        command: '/bin/zsh -l',
        cols: 120,
        rows: 36,
      }),
    ])
  })

  it('claims local pairing with snake_case request fields and no machine private key material', async () => {
    const calls: Array<{ url: string; init: RequestInit | undefined }> = []
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return jsonResponse({
        machine_id: 'machine-local',
        app_certificate: {
          payload: { machine_id: 'machine-local', capabilities: ['terminal', 'file_manager'] },
          signature: 'base64-signature',
        },
        expires_at: '2027-05-01T04:00:00Z',
      })
    })

    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888/', fetch })
    await expect(api.pair({
      pairSessionId: 'pair_1',
      pairSecret: 'pair-secret',
      appDeviceId: 'appdev_1',
      appName: 'TermX Local Web',
      appPublicKey: 'app-public-key',
      requestedCapabilities: ['terminal', 'file_manager'],
    })).resolves.toEqual({
      machineId: 'machine-local',
      appCertificate: JSON.stringify({
        payload: { machine_id: 'machine-local', capabilities: ['terminal', 'file_manager'] },
        signature: 'base64-signature',
      }),
      expiresAt: '2027-05-01T04:00:00Z',
    })

    expect(calls[0]?.url).toBe('http://127.0.0.1:18888/api/local/pair')
    expect(calls[0]?.init?.method).toBe('POST')
    expect(JSON.parse(String(calls[0]?.init?.body))).toEqual({
      pair_session_id: 'pair_1',
      pair_secret: 'pair-secret',
      app_device_id: 'appdev_1',
      app_name: 'TermX Local Web',
      app_public_key: 'app-public-key',
      requested_capabilities: ['terminal', 'file_manager'],
    })
    expect(JSON.stringify(calls)).not.toMatch(/machine_private_key|privateKey|turn|credential/i)
  })

  it('submits local RTC offers without TURN credentials or private keys', async () => {
    const calls: Array<{ url: string; init: RequestInit | undefined }> = []
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return jsonResponse({
        answer: { session_id: 'rtc-local-1', sdp: 'answer-sdp', ice_candidates: [] },
        ice_tcp_enabled: true,
        data_channels: ['api', 'terminal:{terminal_id}', 'file:{transfer_id}'],
      })
    })

    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888', fetch })
    await expect(api.createRTCAnswer({
      sessionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sdp: 'offer-sdp',
      appCertificate: '{"payload":{}}',
      appSignature: 'signature',
      nonce: 'nonce-1',
      timestamp: '1770000000',
    })).resolves.toEqual({
      sessionId: 'rtc-local-1',
      answer: { type: 'answer', sdp: 'answer-sdp' },
      iceTCP: { enabled: true },
    })

    const body = JSON.parse(String(calls[0]?.init?.body))
    expect(body.offer).toMatchObject({
      session_id: 'rtc-local-1',
      machine_id: 'machine-local',
      terminal_id: 'terminal-1',
      sdp: 'offer-sdp',
    })
    expect(body.signature).toEqual({
      algorithm: 'ed25519',
      nonce: 'nonce-1',
      timestamp: 1770000000,
      value: 'signature',
    })
    expect(body.client).toEqual({ type: 'browser', transport: 'local' })
    expect(JSON.stringify(body)).not.toMatch(/turn|credential|machine_private_key|privateKey/i)
  })

  it('rejects local API payloads carrying workspace or pane-shaped public data', async () => {
    const fetch = vi.fn(async () => jsonResponse({
      terminals: [{
        terminal_id: 'terminal-1',
        machine_id: 'machine-local',
        pane_id: 'pane-1',
      }],
    }))

    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888', fetch, machineId: 'machine-local' })
    await expect(api.listTerminals()).rejects.toThrow(/pane_id/)
  })

  it('rejects local terminal records that explicitly belong to another machine', async () => {
    const fetch = vi.fn(async () => jsonResponse({
      terminals: [{
        terminal_id: 'terminal-1',
        machine_id: 'machine-b',
      }],
    }))

    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888', fetch, machineId: 'machine-local' })
    await expect(api.listTerminals()).rejects.toThrow(/machine-b.*machine-local/)
  })
})

function responseFor(url: string, routes: Record<string, unknown>): Response {
  const path = new URL(url).pathname
  const body = routes[path]
  if (body === undefined) return jsonResponse({ error: { message: 'not found' } }, 404)
  return jsonResponse(body)
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}
