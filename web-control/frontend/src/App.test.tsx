import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'

const originalFetch = globalThis.fetch
const originalLocalStorageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')

function jsonResponse(body: unknown, init: ResponseInit = {}) {
  return new Response(JSON.stringify(body), {
    status: init.status ?? 200,
    headers: {
      'Content-Type': 'application/json',
      ...init.headers,
    },
  })
}

describe('App', () => {
  beforeEach(() => {
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: createMemoryStorage(),
    })
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
    if (originalLocalStorageDescriptor) {
      Object.defineProperty(globalThis, 'localStorage', originalLocalStorageDescriptor)
    }
  })

  it('renders a control-plane shell instead of a marketing landing page', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: 'TermX Control' })).toBeInTheDocument()
    expect(screen.getByText('Control Plane')).toBeInTheDocument()
    expect(screen.getByText('SQLite dev database')).toBeInTheDocument()
    expect(screen.getByText('WebRTC DataChannel runtime only')).toBeInTheDocument()
  })

  it('shows only supported client connection paths', () => {
    render(<App />)

    expect(screen.getByText('local')).toBeInTheDocument()
    expect(screen.getByText('public_p2p')).toBeInTheDocument()
    expect(screen.getByText('managed')).toBeInTheDocument()
    expect(screen.queryByText('paid_relay')).not.toBeInTheDocument()
    expect(screen.queryByText('anonymous_p2p')).not.toBeInTheDocument()
  })

  it('logs in through the real auth API shape and loads devices and terminals', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/health') {
        return jsonResponse({
          service: 'termx-web-control',
          status: 'ok',
          transport: 'signaling-control-only',
        })
      }
      if (path === '/api/v1/auth/login') {
        expect(init?.method).toBe('POST')
        expect(JSON.parse(String(init?.body))).toEqual({
          email: 'dev@example.com',
          password: 'valid password',
        })
        return jsonResponse({
          user: { id: 'user_1', email: 'dev@example.com', role: 'user' },
          plan: {
            id: 'registered_free',
            name: 'Registered Free',
            allow_public_p2p: true,
            allow_relay: false,
            monthly_relay_bytes: 0,
            relay_session_limit: 0,
            relay_transfer_files: false,
          },
          access_token: 'access-token',
          refresh_token: 'refresh-token',
        })
      }
      if (path === '/api/v1/auth/me') {
        expect(init?.headers).toMatchObject({ Authorization: 'Bearer access-token' })
        return jsonResponse({
          user: { id: 'user_1', email: 'dev@example.com', role: 'user' },
          plan: {
            id: 'registered_free',
            name: 'Registered Free',
            allow_public_p2p: true,
            allow_relay: false,
            monthly_relay_bytes: 0,
            relay_session_limit: 0,
            relay_transfer_files: false,
          },
        })
      }
      if (path === '/api/devices') {
        expect(init?.headers).toMatchObject({ Authorization: 'Bearer access-token' })
        return jsonResponse({
          devices: [
            {
              id: 'device-0fbc2e86970eb988',
              display_name: 'external-smoke-agent',
              hostname: 'al',
              platform: 'linux',
            },
          ],
        })
      }
      if (path === '/api/terminals') {
        expect(init?.headers).toMatchObject({ Authorization: 'Bearer access-token' })
        return jsonResponse({
          terminals: [
            {
              id: '1',
              machine_id: 'device-0fbc2e86970eb988',
              name: 'bash',
              state: 'running',
            },
          ],
        })
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    globalThis.fetch = fetchMock as typeof fetch

    render(<App />)

    fireEvent.change(screen.getByLabelText('Email'), {
      target: { value: 'dev@example.com' },
    })
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'valid password' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Submit login' }))

    expect((await screen.findAllByText('dev@example.com')).length).toBeGreaterThan(0)
    expect((await screen.findAllByText('device-0fbc2e86970eb988')).length).toBeGreaterThan(0)
    expect(screen.getByText('Terminal 1')).toBeInTheDocument()
    expect(screen.getByText('Managed relay unavailable on this plan')).toBeInTheDocument()
    expect(localStorage.getItem('termx.webControl.accessToken')).toBe('access-token')
  })

  it('registers through the real auth API shape and then loads inventory', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/health') {
        return jsonResponse({ service: 'termx-web-control', status: 'ok' })
      }
      if (path === '/api/v1/auth/register') {
        expect(init?.method).toBe('POST')
        expect(JSON.parse(String(init?.body))).toEqual({
          email: 'new@example.com',
          password: 'valid password',
        })
        return jsonResponse(
          {
            user: { id: 'user_2', email: 'new@example.com', role: 'user' },
            plan: {
              id: 'registered_free',
              name: 'Registered Free',
              allow_public_p2p: true,
              allow_relay: false,
              monthly_relay_bytes: 0,
              relay_session_limit: 0,
              relay_transfer_files: false,
            },
            access_token: 'new-access-token',
            refresh_token: 'new-refresh-token',
          },
          { status: 201 },
        )
      }
      if (path === '/api/v1/auth/me') {
        return jsonResponse({
          user: { id: 'user_2', email: 'new@example.com', role: 'user' },
          plan: {
            id: 'registered_free',
            name: 'Registered Free',
            allow_public_p2p: true,
            allow_relay: false,
            monthly_relay_bytes: 0,
            relay_session_limit: 0,
            relay_transfer_files: false,
          },
        })
      }
      if (path === '/api/devices') {
        expect(init?.headers).toMatchObject({ Authorization: 'Bearer new-access-token' })
        return jsonResponse({ devices: [] })
      }
      if (path === '/api/terminals') {
        expect(init?.headers).toMatchObject({ Authorization: 'Bearer new-access-token' })
        return jsonResponse({ terminals: [] })
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    globalThis.fetch = fetchMock as typeof fetch

    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Register' }))
    fireEvent.change(screen.getByLabelText('Email'), {
      target: { value: 'new@example.com' },
    })
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'valid password' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }))

    expect((await screen.findAllByText('new@example.com')).length).toBeGreaterThan(0)
    expect(localStorage.getItem('termx.webControl.accessToken')).toBe('new-access-token')
  })

  it('creates a managed connect ticket as control-plane state without adding relay as a client path', async () => {
    localStorage.setItem('termx.webControl.accessToken', 'access-token')
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/health') {
        return jsonResponse({ service: 'termx-web-control', status: 'ok' })
      }
      if (path === '/api/v1/auth/me') {
        return jsonResponse({
          user: { id: 'user_1', email: 'dev@example.com', role: 'user' },
          plan: {
            id: 'registered_free',
            name: 'Registered Free',
            allow_public_p2p: true,
            allow_relay: false,
            monthly_relay_bytes: 0,
            relay_session_limit: 0,
            relay_transfer_files: false,
          },
        })
      }
      if (path === '/api/devices') {
        return jsonResponse({
          devices: [{ id: 'machine-a', display_name: 'al daemon' }],
        })
      }
      if (path === '/api/terminals') {
        return jsonResponse({
          terminals: [{ id: '1', machine_id: 'machine-a', state: 'running' }],
        })
      }
      if (path === '/api/v1/managed/connect-tickets') {
        expect(init?.method).toBe('POST')
        expect(init?.headers).toMatchObject({ Authorization: 'Bearer access-token' })
        expect(JSON.parse(String(init?.body))).toEqual({
          machine_id: 'machine-a',
          terminal_id: '1',
          ttl_seconds: 300,
        })
        return jsonResponse(
          {
            id: 'ct_123',
            machine_id: 'machine-a',
            terminal_id: '1',
            path: 'managed',
            allow_relay: false,
            relay_in_use: false,
            relay_bytes_remaining: 0,
            relay_throttled: false,
            expires_at: '2026-05-03T06:00:00Z',
          },
          { status: 201 },
        )
      }
      throw new Error(`unexpected fetch ${path}`)
    })
    globalThis.fetch = fetchMock as typeof fetch

    render(<App />)

    expect(await screen.findByText('Terminal 1')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create managed ticket for Terminal 1' }))

    expect(await screen.findByText('ct_123')).toBeInTheDocument()
    expect(screen.getAllByText('managed').length).toBeGreaterThan(0)
    expect(screen.queryByText('relay')).not.toBeInTheDocument()
    expect(screen.queryByText('paid_relay')).not.toBeInTheDocument()
  })

  it('handles unavailable localStorage without losing the API-backed console', async () => {
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: {
        getItem: () => {
          throw new Error('storage unavailable')
        },
        setItem: () => {
          throw new Error('storage unavailable')
        },
        removeItem: () => undefined,
        clear: () => undefined,
      },
    })
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === '/api/health') {
        return jsonResponse({ service: 'termx-web-control', status: 'ok' })
      }
      throw new Error(`unexpected fetch ${String(input)}`)
    }) as typeof fetch

    render(<App />)

    expect(await screen.findByText('termx-web-control')).toBeInTheDocument()
    expect(screen.getByLabelText('Email')).toBeInTheDocument()
  })
})

function createMemoryStorage(): Storage {
  const values = new Map<string, string>()
  return {
    get length() {
      return values.size
    },
    clear() {
      values.clear()
    },
    getItem(key: string) {
      return values.get(key) ?? null
    },
    key(index: number) {
      return Array.from(values.keys())[index] ?? null
    },
    removeItem(key: string) {
      values.delete(key)
    },
    setItem(key: string, value: string) {
      values.set(key, value)
    },
  }
}
