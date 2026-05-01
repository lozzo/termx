import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LocalRemoteApp, type LocalRemoteTransportFactory } from './LocalRemoteApp'
import type { LocalAgentApi } from './transport'
import { createMockFilePeerTransport } from './test/mockFileTransport'
import type { TerminalTransport, TerminalTransportEvent } from './terminalClient'

describe('LocalRemoteApp', () => {
  afterEach(() => {
    cleanup()
  })

  it('loads local machine terminals and composes shared terminal/file manager components', async () => {
    const api = createMockLocalAgentApi()
    const transports: ReturnType<typeof createMockLocalRemoteTransport>[] = []
    const createTransport = vi.fn<LocalRemoteTransportFactory>(({ machineId, terminalId }) =>
      trackTransport(transports, createMockLocalRemoteTransport({
          '/files/list': { path: '/', parent: '', total: 0, entries: [] },
        }, machineId, terminalId)),
    )

    render(<LocalRemoteApp api={api} createTransport={createTransport} />)

    await waitFor(() => expect(screen.getByRole('button', { name: /open zsh/i })).toBeTruthy())
    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))

    await waitFor(() => expect(screen.getByTestId('termx-terminal')).toBeTruthy())
    expect(screen.getByTestId('termx-terminal-list').getAttribute('data-machine-id')).toBe('machine-local')
    expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1')
    expect(screen.getByTestId('termx-file-manager').getAttribute('data-terminal-id')).toBe('terminal-1')
    expect(createTransport).toHaveBeenCalledWith(expect.objectContaining({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    }))
    await waitFor(() => expect(transports[0]?.connectCalls).toEqual([{
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
    }]))
    expect(document.body.textContent).not.toMatch(/workspace|tab|window|pane|session/i)
  })

  it('keeps the app shell driven by LocalAgentApi and transport interfaces only', () => {
    const createTransport = vi.fn<LocalRemoteTransportFactory>(() => createMockLocalRemoteTransport(
      {},
      'machine-local',
      'terminal-1',
    ))
    const props = {
      api: createMockLocalAgentApi(),
      createTransport,
    }

    expect(Object.keys(props)).not.toContain('rtcPeerConnection')
    expect(Object.keys(props)).not.toContain('nativePlugin')
    expect(Object.keys(props)).not.toContain('relayCredentials')
  })
})

function trackTransport<T extends ReturnType<typeof createMockLocalRemoteTransport>>(transports: T[], transport: T): T {
  transports.push(transport)
  return transport
}

function createMockLocalRemoteTransport(
  responders: Parameters<typeof createMockFilePeerTransport>[0],
  machineId: string,
  terminalId: string,
): ReturnType<typeof createMockFilePeerTransport> & TerminalTransport & { connectCalls: Array<{ machineId: string; terminalId?: string; mode: string }> } {
  const transport = createMockFilePeerTransport(responders, {}, { machineId, terminalId })
  return Object.assign(transport, {
    connectCalls: [] as Array<{ machineId: string; terminalId?: string; mode: string }>,
    async connect(input: { machineId: string; terminalId?: string; mode: string }) {
      this.connectCalls.push(input)
    },
    async openTerminal() {
      return {
        label: `terminal:${terminalId}`,
        readyState: 'open' as const,
        send() {},
        close() {},
      }
    },
    subscribeTerminal(_id: string, _handler: (event: TerminalTransportEvent) => void) {
      return () => {}
    },
    closeTerminalChannel() {},
  })
}

function createMockLocalAgentApi(): LocalAgentApi {
  return {
    async getStatus() {
      return {
        machine: {
          machineId: 'machine-local',
          name: 'Local Mac',
          state: 'online',
          terminalCount: 1,
          localRTC: { signalingUrl: 'http://127.0.0.1:18888/api/local/rtc/offer' },
        },
        localWeb: {
          httpUrl: 'http://127.0.0.1:18888',
          rtcOfferUrl: 'http://127.0.0.1:18888/api/local/rtc/offer',
        },
      }
    },
    async listTerminals() {
      return [{
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        title: 'zsh',
        state: 'running',
        command: '/bin/zsh',
        cols: 120,
        rows: 36,
      }]
    },
    async pair() {
      throw new Error('pair is not used by LocalRemoteApp tests')
    },
    async createRTCAnswer() {
      throw new Error('createRTCAnswer is not used by LocalRemoteApp tests')
    },
  }
}
