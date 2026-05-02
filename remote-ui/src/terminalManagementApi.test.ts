import { describe, expect, it } from 'vitest'
import { createTerminalManagementApi } from './terminalManagementApi'
import type { ConnectionCapabilities, RtcJsonRpcChannel, RtcSession } from './transport'

describe('terminal management API over RtcSession', () => {
  it('creates, updates, and deletes terminals through the unified session api channel', async () => {
    const session = new MockManagementSession()
    const api = createTerminalManagementApi(session, 'machine-local')

    await api.createTerminal({
      name: 'ops shell',
      command: ['/bin/zsh', '-l'],
      cwd: '/srv/app',
      environment: 'prod',
      sizeLockMode: 'lock',
    })
    await api.updateTerminal({
      terminalId: 'terminal-1',
      name: 'renamed shell',
      cwd: '/srv/app-next',
      environment: 'staging',
      sizeLockMode: 'warn',
    })
    await api.deleteTerminal('terminal-1')

    expect(session.requests).toEqual([
      ['create', {
        command: ['/bin/zsh', '-l'],
        name: 'ops shell',
        dir: '/srv/app',
        env: ['prod'],
        tags: { 'termx.size_lock': 'lock', cwd: '/srv/app', environment: 'prod' },
      }],
      ['set_metadata', {
        terminal_id: 'terminal-1',
        name: 'renamed shell',
        tags: { 'termx.size_lock': 'warn', cwd: '/srv/app-next', environment: 'staging' },
      }],
      ['remove', { terminal_id: 'terminal-1' }],
    ])
  })

  it('checks session capabilities before opening the api channel', async () => {
    const session = new MockManagementSession()
    session.capabilities = {
      ...session.capabilities,
      apiAllowed: false,
      terminalManagementAllowed: false,
      denialReason: 'anonymous devices cannot manage terminals',
    }
    const api = createTerminalManagementApi(session, 'machine-local')

    await expect(api.deleteTerminal('terminal-1')).rejects.toThrow(/anonymous devices/)
    expect(session.openApiCount).toBe(0)
    expect(session.requests).toEqual([])
  })

  it('rejects management through a session connected to another machine before opening api', async () => {
    const session = new MockManagementSession()
    session.machineId = 'machine-other'
    const api = createTerminalManagementApi(session, 'machine-local')

    await expect(api.createTerminal({ command: ['/bin/zsh', '-l'] })).rejects.toThrow(/machine-other.*machine-local/)
    expect(session.openApiCount).toBe(0)
    expect(session.requests).toEqual([])
  })

  it('rejects malformed create responses instead of returning an empty terminal id', async () => {
    const session = new MockManagementSession()
    session.createResponse = { state: 'running' }
    const api = createTerminalManagementApi(session, 'machine-local')

    await expect(api.createTerminal({ command: ['/bin/zsh', '-l'] })).rejects.toThrow(/terminal_id.*required/i)
  })
})

class MockManagementSession implements Pick<RtcSession, 'openApi' | 'getCapabilities' | 'getConnectionInfo'> {
  readonly requests: Array<[string, unknown]> = []
  openApiCount = 0
  machineId = 'machine-local'
  createResponse: unknown = { terminal_id: 'terminal-3', state: 'running' }
  capabilities: ConnectionCapabilities = {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    relayInUse: false,
    terminalManagementAllowed: true,
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    this.openApiCount += 1
    return {
      request: async <TResponse>(method: string, params?: unknown) => {
        this.requests.push([method, params])
        if (method === 'create') {
          return this.createResponse as TResponse
        }
        return undefined as TResponse
      },
      close() {},
    }
  }

  async getCapabilities() {
    return this.capabilities
  }

  async getConnectionInfo() {
    return {
      path: 'local' as const,
      connectionId: 'management-connection',
      machineId: this.machineId,
      terminalId: 'terminal-1',
      relayInUse: false,
    }
  }
}
