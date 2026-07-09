import { describe, expect, it } from 'vitest'
import { createTerminalManagementApi } from '../terminal/terminalManagementApi'
import { createTerminalProtocolClient } from '../terminal/terminalProtocolClient'
import { normalizeTerminalInventory } from '../terminal/terminalInventory'
import { MockRtcTerminalSession } from '../test/mockRtcTerminalSession'
import type { ConnectionPath, RtcJsonRpcChannel } from '../core/transport'

describe('App terminal runtime contract', () => {
  it('routes terminal inventory, management, and live surface through one core-v2 session', async () => {
    const session = new MockCoreV2AppSession('machine-local', 'local', {
      list: {
        terminals: [{
          terminal_id: 'terminal-1',
          machine_id: 'machine-local',
          name: 'zsh',
          state: 'running',
          command: ['/bin/zsh', '-l'],
          cols: 120,
          rows: 36,
        }],
      },
      create: { terminal_id: 'terminal-2', state: 'running' },
      restart: {},
      remove: {},
    })
    session.emitResizeControl('terminal-1', { canResize: true, reason: 'owner' })
    session.setTerminalSnapshot('terminal-1', { text: 'ready', cols: 120, rows: 36 })

    const terminals = await listTerminalsFromCoreSession(session, 'machine-local')
    const management = createTerminalManagementApi(session, 'machine-local')
    const created = await management.createTerminal({
      name: 'ops',
      command: ['/bin/zsh', '-l'],
      cwd: '/srv/app',
      environment: 'prod',
      sizeLockMode: 'off',
    })
    await management.restartTerminal(created.terminalId)
    await management.deleteTerminal(created.terminalId)

    const rawChannel = await session.openTerminal('terminal-1')
    const protocol = createTerminalProtocolClient({
      channel: rawChannel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: await session.getConnectionInfo(),
      resizePolicy: 'owner',
      surfaceId: 'app:machine-local:terminal:terminal-1',
    })
    const terminal = await protocol.openTerminal('terminal-1')
    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'input', data: 'echo ok\n' })))
    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'resize', cols: 100, rows: 30 })))

    expect(terminals).toEqual([
      expect.objectContaining({
        terminalId: 'terminal-1',
        machineId: 'machine-local',
        title: 'zsh',
        command: '/bin/zsh -l',
      }),
    ])
    expect(session.apiRequests).toEqual([
      { method: 'list', params: {} },
      {
        method: 'create',
        params: {
          command: ['/bin/zsh', '-l'],
          name: 'ops',
          dir: '/srv/app',
          env: ['prod'],
          tags: { 'termx.size_lock': 'off', cwd: '/srv/app', environment: 'prod' },
        },
      },
      { method: 'restart', params: { terminal_id: 'terminal-2' } },
      { method: 'remove', params: { terminal_id: 'terminal-2' } },
    ])
    expect(session.openedTerminalIds).toEqual(['terminal-1'])
    expect(terminal.label).toBe('terminal:terminal-1')
    expect(session.sentText('terminal-1')).toBe('echo ok\n')
    expect(session.sentResize('terminal-1')).toEqual({ cols: 100, rows: 30 })
  })
})

async function listTerminalsFromCoreSession(session: MockCoreV2AppSession, machineId: string) {
  const channel = await session.openApi()
  try {
    const response = await channel.request<{ terminals?: Record<string, unknown>[] }>('list', {})
    return normalizeTerminalInventory({
      machine_id: machineId,
      terminals: response.terminals ?? [],
    }).terminals
  } finally {
    channel.close()
  }
}

type RuntimeResponder = unknown | ((params: unknown) => unknown | Promise<unknown>)

class MockCoreV2AppSession extends MockRtcTerminalSession {
  readonly apiRequests: Array<{ method: string; params: unknown }> = []

  constructor(
    machineId: string,
    path: ConnectionPath,
    private readonly responders: Record<string, RuntimeResponder>,
  ) {
    super(machineId, path)
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    return {
      request: async <TResponse>(method: string, params?: unknown): Promise<TResponse> => {
        const requestParams = params ?? {}
        this.apiRequests.push({ method, params: requestParams })
        const responder = this.responders[method]
        if (typeof responder === 'function') {
          return await responder(requestParams) as TResponse
        }
        if (responder !== undefined) {
          return responder as TResponse
        }
        throw new Error(`unhandled core-v2 runtime api method ${method}`)
      },
      close() {},
    }
  }
}
