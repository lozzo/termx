import { cleanup, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

describe('local web entry shell', () => {
  afterEach(() => {
    cleanup()
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('mounts the shared local remote app with browser local adapters and no forbidden public model text', async () => {
    const entry = await import('./localWebEntry')
    document.body.innerHTML = '<div id="root"></div>'
    const fetch = vi.fn(async (input: RequestInfo | URL) => {
      const path = new URL(String(input), 'http://127.0.0.1:18888').pathname
      if (path === '/api/local/status') {
        return jsonResponse({
          machine_id: 'machine-local',
          machine_name: 'Local Mac',
          remote_enabled: true,
          local_rtc: {
            http_url: 'http://127.0.0.1:18888',
            ice_tcp_enabled: true,
            ice_tcp_port: 18889,
          },
          updated_at: '2026-05-01T04:00:00Z',
        })
      }
      if (path === '/api/local/terminals') {
        return jsonResponse({
          terminals: [{
            terminal_id: 'terminal-1',
            name: 'zsh',
            command: ['/bin/zsh', '-l'],
            cols: 120,
            rows: 36,
            state: 'running',
          }],
        })
      }
      return jsonResponse({ error: { message: 'not found' } }, 404)
    })
    vi.stubGlobal('fetch', fetch)

    entry.mountLocalWebApp({
      createTransport: () => ({
        async connect() {},
        async disconnect() {},
        async openTerminal() {
          return {
            label: 'terminal:terminal-1',
            readyState: 'open' as const,
            send() {},
            close() {},
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
          }
        },
        async getConnectionInfo() {
          return {
            mode: 'local' as const,
            connectionId: 'local-test',
            machineId: 'machine-local',
            terminalId: 'terminal-1',
            relayInUse: false,
          }
        },
        subscribeTerminal() {
          return () => {}
        },
        closeTerminalChannel() {},
      }),
    })

    await waitFor(() => expect(screen.getByTestId('termx-local-web-shell')).toBeTruthy())
    await waitFor(() => expect(screen.getByTestId('termx-terminal-list')).toBeTruthy())
    expect(screen.getByTestId('termx-terminal-list').getAttribute('data-machine-id')).toBe('machine-local')
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|session/i)
    expect(JSON.stringify(fetch.mock.calls)).not.toMatch(/turn|credential|machine_private_key|privateKey/i)
  })
})

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}
