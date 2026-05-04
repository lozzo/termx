import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { WebControlRemoteApp } from './WebControlRemoteApp'
import type { WebControlFetch } from './webControlApi'

describe('WebControlRemoteApp', () => {
  afterEach(() => cleanup())

  it('defaults to the public Web Control URL instead of the local remote-ui origin', () => {
    const storage = new MemoryStorage()
    storage.setItem('termx.remote.controlUrl', 'http://127.0.0.1:5174')

    render(<WebControlRemoteApp storage={storage as unknown as Storage} />)

    expect((screen.getByLabelText(/web control/i) as HTMLInputElement).value).toBe('http://114.66.58.243:12306')
    expect(screen.getByText('Sign in to view your devices.')).toBeTruthy()
  })

  it('logs into Web Control, lists account machines, and imports a TermX pairing code', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri({
      type: 'termx_pair_v2',
      schema_version: 2,
      machine: {
        id: 'device-1',
        name: 'RedmiBook',
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
    })
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
          preferred_path: 'public_p2p',
          control_url: 'http://114.66.58.243:12306',
          hub_http_url: 'http://114.66.58.243:8447',
          terminal_count: 1,
          terminal_ids: ['terminal-1'],
        }],
      }),
    ])
    const originalFetch = globalThis.fetch
    globalThis.fetch = fetch.fetch as typeof globalThis.fetch
    try {
      render(<WebControlRemoteApp defaultControlUrl="http://114.66.58.243:12306" storage={storage as unknown as Storage} />)

      await userEvent.type(screen.getByLabelText(/email or username/i), 'lozzow@example.test')
      await userEvent.type(screen.getByLabelText(/password/i), 'secret')
      await userEvent.click(screen.getByRole('button', { name: /sign in/i }))

      await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))
      expect(screen.getAllByText('Scan QR').length).toBeGreaterThan(0)
      expect(screen.getByText('Public P2P')).toBeTruthy()
      expect(screen.getByRole('heading', { name: /pair device/i })).toBeTruthy()

      await userEvent.type(screen.getByLabelText(/termx qr content/i), pairUri)
      await userEvent.click(screen.getByRole('button', { name: /import pairing code/i }))

      expect(screen.getByText('Paired RedmiBook')).toBeTruthy()
      expect(screen.getAllByText('Ready').length).toBeGreaterThan(0)
      const stored = JSON.parse(storage.getItem('termx.app.machines.v1') ?? '[]') as Array<Record<string, unknown>>
      expect(stored).toHaveLength(1)
      expect(stored[0]?.machineId).toBe('device-1')
      expect(stored[0]?.preferredPath).toBe('public_p2p')
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('rejects pairing codes that do not match a Web Control machine in the signed-in account', async () => {
    const storage = new MemoryStorage()
    const pairUri = termxPairUri({
      type: 'termx_pair_v2',
      schema_version: 2,
      machine: {
        id: 'local-machine-1',
        name: 'Local Debug Machine',
        hostname: 'debug-host',
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
    })
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
          preferred_path: 'public_p2p',
          terminal_count: 0,
          terminal_ids: [],
        }],
      }),
    ])
    const originalFetch = globalThis.fetch
    globalThis.fetch = fetch.fetch as typeof globalThis.fetch
    try {
      render(<WebControlRemoteApp defaultControlUrl="http://114.66.58.243:12306" storage={storage as unknown as Storage} />)

      await userEvent.type(screen.getByLabelText(/email or username/i), 'lozzow@example.test')
      await userEvent.type(screen.getByLabelText(/password/i), 'secret')
      await userEvent.click(screen.getByRole('button', { name: /sign in/i }))
      await waitFor(() => expect(screen.getAllByText('RedmiBook').length).toBeGreaterThan(0))

      await userEvent.type(screen.getByLabelText(/termx qr content/i), pairUri)
      await userEvent.click(screen.getByRole('button', { name: /import pairing code/i }))

      expect(screen.getByText('This pairing code does not match a Web Control device in this account')).toBeTruthy()
      expect(storage.getItem('termx.app.machines.v1')).toBeNull()
    } finally {
      globalThis.fetch = originalFetch
    }
  })
})

interface RecordedRequest {
  url: string
  method: string
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
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

function termxPairUri(payload: Record<string, unknown>): string {
  const bytes = new TextEncoder().encode(JSON.stringify(payload))
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return `termx://pair?payload=${btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')}`
}
