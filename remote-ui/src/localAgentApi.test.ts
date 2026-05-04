import { afterEach, describe, expect, it, vi } from 'vitest'
import { createLocalAgentApi } from './localAgentApi'

describe('createLocalAgentApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

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
          size_locked: true,
          size_lock_mode: 'lock',
          cwd: '/Users/lozzow/project',
          environment: 'dev',
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
        sizeLocked: true,
        sizeLockMode: 'lock',
        cwd: '/Users/lozzow/project',
        environment: 'dev',
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

  it('claims pairing through an absolute QR pairing endpoint', async () => {
    const calls: Array<{ url: string; init: RequestInit | undefined }> = []
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return jsonResponse({
        machine_id: 'machine-local',
        app_certificate: {
          payload: { machine_id: 'machine-local', app_public_key: 'app-public-key' },
          signature: 'base64-signature',
        },
        expires_at: '2027-05-01T04:00:00Z',
      })
    })

    const api = createLocalAgentApi({
      baseUrl: 'http://127.0.0.1:5173',
      fetch,
      pairUrl: 'http://127.0.0.1:18888/api/local/pair',
    })

    await expect(api.pair({
      pairSessionId: 'pair_1',
      pairSecret: 'pair-secret',
      appDeviceId: 'appdev_1',
      appName: 'TermX Remote App',
      appPublicKey: 'app-public-key',
      requestedCapabilities: ['terminal'],
    })).resolves.toMatchObject({
      machineId: 'machine-local',
      expiresAt: '2027-05-01T04:00:00Z',
    })

    expect(calls[0]?.url).toBe('http://127.0.0.1:18888/api/local/pair')
  })

  it('uses the browser fetch binding when no custom fetch is injected', async () => {
    const calls: Array<{ thisValue: unknown; input: string }> = []
    const boundFetch = function (this: unknown, input: RequestInfo | URL) {
      calls.push({ thisValue: this, input: String(input) })
      if (this !== globalThis) {
        throw new TypeError("Failed to execute 'fetch' on 'Window': Illegal invocation")
      }
      return Promise.resolve(jsonResponse({
        machine_id: 'machine-local',
        machine_name: 'Local Mac',
        remote_enabled: true,
        local_rtc: {
          http_url: 'http://127.0.0.1:18888',
        },
      }))
    }
    vi.stubGlobal('fetch', boundFetch)
    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888' })

    await expect(api.getStatus()).resolves.toMatchObject({
      machine: { machineId: 'machine-local' },
    })
    expect(calls).toEqual([{
      thisValue: globalThis,
      input: 'http://127.0.0.1:18888/api/local/status',
    }])
  })

  it('submits local RTC offers without TURN credentials or private keys', async () => {
    const calls: Array<{ url: string; init: RequestInit | undefined }> = []
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return jsonResponse({
        answer: { session_id: 'rtc-local-1', sdp: 'answer-sdp', ice_candidates: [] },
        ice_tcp_enabled: true,
        data_channels: ['api', 'terminal:{terminal_id}', 'file:{transfer_id}'],
        capabilities: {
          terminal_allowed: true,
          api_allowed: true,
          events_allowed: false,
          file_transfer_allowed: true,
          terminal_management_allowed: true,
          relay_in_use: false,
        },
      })
    })

    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888', fetch })
    await expect(api.createRTCAnswer({
      sessionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sdp: 'offer-sdp',
      iceCandidates: ['candidate:host-a'],
      appCertificate: '{"payload":{}}',
      appSignature: 'signature',
      nonce: 'nonce-1',
      timestamp: '1770000000',
    })).resolves.toEqual({
      sessionId: 'rtc-local-1',
      answer: { type: 'answer', sdp: 'answer-sdp' },
      iceTCP: { enabled: true },
      dataChannels: ['api', 'terminal:{terminal_id}', 'file:{transfer_id}'],
      capabilities: {
        terminalAllowed: true,
        apiAllowed: true,
        eventsAllowed: false,
        fileTransferAllowed: true,
        terminalManagementAllowed: true,
        relayInUse: false,
      },
    })

    const body = JSON.parse(String(calls[0]?.init?.body))
    expect(body.offer).toMatchObject({
      session_id: 'rtc-local-1',
      machine_id: 'machine-local',
      terminal_id: 'terminal-1',
      sdp: 'offer-sdp',
      ice_candidates: ['candidate:host-a'],
    })
    expect(body.signature).toEqual({
      algorithm: 'ed25519',
      nonce: 'nonce-1',
      timestamp: 1770000000,
      value: 'signature',
    })
    expect(body.client).toEqual({ type: 'browser', purpose: 'runtime' })
    expect(JSON.stringify(body)).not.toMatch(/turn|credential|machine_private_key|privateKey/i)
  })

  it('submits machine-level inventory RTC offers without terminal scoping', async () => {
    const calls: Array<{ url: string; init: RequestInit | undefined }> = []
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return jsonResponse({
        answer: { session_id: 'rtc-inventory-1', sdp: 'answer-sdp', ice_candidates: [] },
        data_channels: ['events'],
      })
    })

    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888', fetch })
    await expect(api.createInventoryRTCAnswer({
      sessionId: 'rtc-inventory-1',
      machineId: 'machine-local',
      sdp: 'offer-sdp',
      appCertificate: '{"payload":{}}',
      appSignature: 'signature',
      nonce: 'nonce-1',
      timestamp: '1770000000',
    })).resolves.toEqual({
      sessionId: 'rtc-inventory-1',
      answer: { type: 'answer', sdp: 'answer-sdp' },
    })

    const body = JSON.parse(String(calls[0]?.init?.body))
    expect(body.offer).toMatchObject({
      session_id: 'rtc-inventory-1',
      machine_id: 'machine-local',
      terminal_id: '',
      sdp: 'offer-sdp',
    })
    expect(body.client).toEqual({ type: 'browser', purpose: 'inventory_events' })
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

  it('creates, updates, and deletes local terminals through machine-level local endpoints', async () => {
    const calls: Array<{ url: string; init: RequestInit | undefined }> = []
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      const path = new URL(String(input)).pathname
      if (path === '/api/local/terminals' && init?.method === 'POST') {
        return jsonResponse({
          terminal_id: 'terminal-3',
          machine_id: 'machine-local',
          name: 'ops shell',
          command: ['/bin/zsh', '-l'],
          cols: 100,
          rows: 30,
          state: 'running',
          cwd: '/srv/app',
          environment: 'prod',
          size_locked: true,
          size_lock_mode: 'lock',
        })
      }
      if (path === '/api/local/terminals/terminal-3' && init?.method === 'PATCH') {
        return jsonResponse({
          terminal_id: 'terminal-3',
          machine_id: 'machine-local',
          name: 'ops shell renamed',
          command: ['/bin/zsh', '-l'],
          cols: 100,
          rows: 30,
          state: 'running',
          cwd: '/srv/app-next',
          environment: 'staging',
          size_locked: false,
          size_lock_mode: 'off',
        })
      }
      if (path === '/api/local/terminals/terminal-3' && init?.method === 'DELETE') {
        return new Response(null, { status: 204 })
      }
      return jsonResponse({ error: { message: 'not found' } }, 404)
    })

    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888', fetch, machineId: 'machine-local' })
    await expect(api.createTerminal({
      name: 'ops shell',
      command: ['/bin/zsh', '-l'],
      cols: 100,
      rows: 30,
      cwd: '/srv/app',
      environment: 'prod',
      sizeLockMode: 'lock',
    })).resolves.toEqual(expect.objectContaining({
      terminalId: 'terminal-3',
      title: 'ops shell',
      cwd: '/srv/app',
      environment: 'prod',
      sizeLocked: true,
      sizeLockMode: 'lock',
    }))

    await expect(api.updateTerminal({
      terminalId: 'terminal-3',
      name: 'ops shell renamed',
      cwd: '/srv/app-next',
      environment: 'staging',
      sizeLockMode: 'off',
    })).resolves.toEqual(expect.objectContaining({
      terminalId: 'terminal-3',
      title: 'ops shell renamed',
      cwd: '/srv/app-next',
      environment: 'staging',
      sizeLockMode: 'off',
    }))

    await expect(api.deleteTerminal('terminal-3')).resolves.toBeUndefined()

    expect(calls.map((call) => [new URL(call.url).pathname, call.init?.method ?? 'GET'])).toEqual([
      ['/api/local/terminals', 'POST'],
      ['/api/local/terminals/terminal-3', 'PATCH'],
      ['/api/local/terminals/terminal-3', 'DELETE'],
    ])
    expect(JSON.parse(String(calls[0]?.init?.body))).toEqual({
      name: 'ops shell',
      command: ['/bin/zsh', '-l'],
      cols: 100,
      rows: 30,
      cwd: '/srv/app',
      environment: 'prod',
      size_lock_mode: 'lock',
    })
    expect(JSON.parse(String(calls[1]?.init?.body))).toEqual({
      name: 'ops shell renamed',
      cwd: '/srv/app-next',
      environment: 'staging',
      size_lock_mode: 'off',
    })
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
