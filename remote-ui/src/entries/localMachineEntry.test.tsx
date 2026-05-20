import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { LocalPairingApi } from '../core/transport'

vi.mock('./terminal/Terminal', () => ({
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
    const entry = await import('./localMachineEntry')
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
    const entry = await import('./localMachineEntry')
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

  it('reuses the browser local inventory session instead of disconnecting after list', async () => {
    const entry = await import('./localMachineEntry')
    document.body.innerHTML = '<div id="root"></div>'
    const storage = new MemoryStorage()
    storage.setItem('termx.session.device-real-local.token', 'session-token-local')
    storage.setItem('termx.session.device-real-local.exp', '2099-05-09T00:00:00.000Z')
    const fetch = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).endsWith('/api/v1/agents/online')) {
        return jsonResponse({
          agents: [{
            machine_id: 'device-real-local',
            machine_name: 'Local Mac',
            status: 'online',
            terminals: [{
              terminal_id: 'terminal-1',
              title: 'zsh',
              state: 'running',
            }],
          }],
        })
      }
      return jsonResponse({})
    })
    const disconnect = vi.fn()
    const connector = {
      connect: vi.fn(async () => ({
        async disconnect() { disconnect() },
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
            async request<TResponse>(method: string) {
              if (method === 'list') return { terminals: [] } as TResponse
              return { path: '/', parent: '', total: 0, entries: [] } as TResponse
            },
            close() {},
          }
        },
        async openFileTransfer() {
          throw new Error('not used')
        },
        subscribeEvents() {
          return { close() {} }
        },
        async getConnectionInfo() {
          return {
            path: 'local' as const,
            connectionId: 'local-test',
            machineId: 'device-real-local',
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
      })),
    }

    entry.mountLocalWebApp({
      connector,
      networkRuntime: {
        fetch,
        storage,
        queryParam: () => null,
      },
    })

    await waitFor(() => expect(connector.connect).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getByTestId('termx-terminal-list')).toBeTruthy())
    expect(disconnect).not.toHaveBeenCalled()
  })

  it('shows the pairing gate before a first-run browser local RTC inventory session exists', async () => {
    const entry = await import('./localMachineEntry')
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
      sessionToken: 'session-token-local',
      expiresAt: '2099-05-06T00:00:00Z',
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
    expect(storage.getItem('termx.session.device-real-local.token')).toBe('session-token-local')
    expect(screen.getByTestId('termx-local-web-shell').textContent).toContain('Local Mac')
    expect(fetch).toHaveBeenCalledWith('http://127.0.0.1:18888/api/v1/agents/online', expect.any(Object))
  })

  it('passes saved answer proof secrets into browser local RTC connections', async () => {
    const entry = await import('./localMachineEntry')
    document.body.innerHTML = '<div id="root"></div>'
    const storage = new MemoryStorage()
    storage.setItem('termx.session.device-real-local.token', sessionTokenWithID('pair-session-local'))
    storage.setItem('termx.session.device-real-local.exp', '2099-05-09T00:00:00.000Z')
    storage.setItem('termx.session.device-real-local.answerProofSecret', 'answer-proof-secret')
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/api/v1/agents/online')) {
        return jsonResponse({
          agents: [{
            machine_id: 'device-real-local',
            machine_name: 'Local Mac',
            status: 'online',
            terminals: [{
              terminal_id: 'terminal-1',
              title: 'zsh',
              state: 'running',
            }],
          }],
        })
      }
      if (url.endsWith('/api/v1/sessions/ice')) {
        return jsonResponse({
          path: 'local',
          machine_id: 'device-real-local',
          ice_servers: [],
          relay_policy: { allow_relay: false, allow_relay_transfer: false },
        })
      }
      if (url.endsWith('/api/v1/sessions')) {
        const request = JSON.parse(String(init?.body ?? '{}')) as { offer?: { session_id?: string }; answer_proof_challenge?: string }
        return jsonResponse({
          session_id: request.offer?.session_id ?? 'rtc-local-1',
          path: 'local',
          machine_id: 'device-real-local',
          answer: {
            type: 'answer',
            sdp: minimalAnswerSDP(),
          },
          ice_candidates: [],
          ice_servers: [],
          relay_policy: { allow_relay: false, allow_relay_transfer: false },
          relay_in_use: false,
          answer_proof: await answerProofForTest(
            'answer-proof-secret',
            'pair-session-local',
            request.offer?.session_id ?? 'rtc-local-1',
            request.answer_proof_challenge ?? '',
          ),
        })
      }
      return jsonResponse({})
    })
    vi.stubGlobal('RTCPeerConnection', FakePeerConnection)

    entry.mountLocalWebApp({
      networkRuntime: {
        fetch,
        storage,
        queryParam: () => null,
      },
    })

    await waitFor(() => expect(fetch).toHaveBeenCalledWith('http://127.0.0.1:18888/api/v1/sessions', expect.any(Object)))
    const sessionRequest = fetch.mock.calls.find(([input]) => String(input).endsWith('/api/v1/sessions'))?.[1] as RequestInit | undefined
    const body = JSON.parse(String(sessionRequest?.body ?? '{}')) as { answer_proof_challenge?: string }
    expect(body.answer_proof_challenge).toBeTruthy()
    await waitFor(() => expect(screen.getByTestId('termx-terminal-list')).toBeTruthy())
    expect(screen.queryByText(/server sent answerProof/i)).toBeNull()
  })

  it('does not require browser crypto or local storage until a terminal session is created', async () => {
    const entry = await import('./localMachineEntry')
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

  it('keeps the local shell mounted when browser crypto is unavailable', async () => {
    const entry = await import('./localMachineEntry')
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
  })
})

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

function sessionTokenWithID(sessionId: string): string {
  return `${base64url(new TextEncoder().encode(JSON.stringify({ sid: sessionId })))}.mac`
}

async function answerProofForTest(secret: string, pairSessionId: string, offerSessionId: string, challenge: string): Promise<string> {
  const data = new TextEncoder().encode(`termx-answer-proof-v1:${pairSessionId}:${offerSessionId}:${challenge}`)
  const key = await crypto.subtle.importKey('raw', new TextEncoder().encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  return base64url(new Uint8Array(await crypto.subtle.sign('HMAC', key, data)))
}

function base64url(bytes: Uint8Array): string {
  let binary = ''
  for (const value of bytes) binary += String.fromCharCode(value)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function minimalAnswerSDP(): string {
  return [
    'v=0',
    'o=- 0 0 IN IP4 127.0.0.1',
    's=-',
    't=0 0',
    'm=application 9 UDP/DTLS/SCTP webrtc-datachannel',
    'c=IN IP4 0.0.0.0',
    'a=ice-ufrag:test',
    'a=ice-pwd:testtesttesttesttesttest',
    'a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00',
    'a=setup:active',
    'a=mid:0',
    'a=sctp-port:5000',
    '',
  ].join('\r\n')
}

class FakePeerConnection extends EventTarget {
  localDescription: RTCSessionDescriptionInit | null = null
  remoteDescription: RTCSessionDescriptionInit | null = null
  iceGatheringState: RTCIceGatheringState = 'complete'
  connectionState: RTCPeerConnectionState = 'connected'

  constructor(_configuration?: RTCConfiguration) {
    super()
  }

  createDataChannel(label: string): RTCDataChannel {
    return new FakeRTCDataChannel(label) as unknown as RTCDataChannel
  }

  async createOffer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'offer', sdp: minimalAnswerSDP() }
  }

  async setLocalDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.localDescription = description
  }

  async setRemoteDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.remoteDescription = description
  }

  async getStats(): Promise<RTCStatsReport> {
    return new Map() as unknown as RTCStatsReport
  }

  close(): void {
    this.connectionState = 'closed'
  }
}

class FakeRTCDataChannel extends EventTarget {
  readyState: RTCDataChannelState = 'open'
  binaryType: BinaryType = 'arraybuffer'

  constructor(readonly label: string) {
    super()
  }

  send(_data: string | Blob | ArrayBuffer | ArrayBufferView): void {}

  close(): void {
    this.readyState = 'closed'
    this.dispatchEvent(new Event('close'))
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
