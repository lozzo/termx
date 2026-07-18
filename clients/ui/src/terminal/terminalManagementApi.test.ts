import { create } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import { AcknowledgeResultSchema } from '../generated/apipb/application_pb'
import {
  TerminalCreateResultSchema,
  TerminalGetResultSchema,
  TerminalInfoSchema,
  TerminalRefSchema,
} from '../generated/apipb/terminal_pb'
import { MockProtoSession, protoResult } from '../test/mockProtoSession'
import { createTerminalManagementApi } from './terminalManagementApi'

describe('terminal management generated Proto API', () => {
  it('emits typed create, metadata, restart, remove, and get commands', async () => {
    const session = new MockProtoSession('machine-local', (command) => {
      if (command.command.case === 'terminalCreate') {
        return protoResult('terminalCreate', create(TerminalCreateResultSchema, {
          terminal: create(TerminalInfoSchema, { ref: create(TerminalRefSchema, { endpointId: 'machine-local', terminalId: 'terminal-3' }) }),
        }))
      }
      if (command.command.case === 'terminalGet') {
        return protoResult('terminalGet', create(TerminalGetResultSchema, {
          terminal: create(TerminalInfoSchema, {
            ref: create(TerminalRefSchema, { endpointId: 'machine-local', terminalId: 'terminal-1' }),
            cwd: '/srv/configured',
            liveCwd: '/srv/live',
          }),
        }))
      }
      return protoResult('acknowledge', create(AcknowledgeResultSchema))
    })
    const api = createTerminalManagementApi(session, 'machine-local')

    await expect(api.createTerminal({
      name: 'ops shell', command: ['/bin/zsh', '-l'], cwd: '/srv/app', environment: 'prod', sizeLockMode: 'lock',
    })).resolves.toEqual({ terminalId: 'terminal-3' })
    await api.updateTerminal({ terminalId: 'terminal-1', name: 'renamed', sizeLockMode: 'off' })
    await api.restartTerminal('terminal-1')
    await api.deleteTerminal('terminal-1')
    await expect(api.getTerminalDirectory('terminal-1')).resolves.toEqual({ path: '/srv/live', source: 'live' })

    expect(session.commands.map((command) => command.command.case)).toEqual([
      'terminalCreate', 'terminalSetMetadata', 'terminalRestart', 'terminalRemove', 'terminalGet',
    ])
    expect(session.commands[0]?.command.value).toMatchObject({
      terminal: { command: ['/bin/zsh', '-l'], cwd: '/srv/app', env: ['prod'], tags: { 'termx.size_lock': 'lock', cwd: '/srv/app', environment: 'prod' } },
    })
    expect(session.commands[1]?.command.value).toMatchObject({ tags: { 'termx.size_lock': 'off' } })
  })

  it('rejects endpoint mismatches before dispatch', () => {
    const session = new MockProtoSession('machine-other')
    expect(() => createTerminalManagementApi(session, 'machine-local')).toThrow(/machine-other.*machine-local/)
    expect(session.commands).toEqual([])
  })
})
