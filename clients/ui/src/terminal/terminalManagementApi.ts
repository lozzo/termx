import { create } from '@bufbuild/protobuf'
import type { ProtoClientSession } from '../core/protoClientSession'
import type { LocalCreateTerminalInput, LocalUpdateTerminalInput } from '../core/transport'
import { CommandEnvelopeSchema } from '../generated/apipb/application_pb'
import {
  TerminalCreateCommandSchema,
  TerminalCreateSpecSchema,
  TerminalGetCommandSchema,
  TerminalRefSchema,
  TerminalRemoveCommandSchema,
  TerminalRestartCommandSchema,
  TerminalSetMetadataCommandSchema,
} from '../generated/apipb/terminal_pb'

export interface TerminalManagementApi {
  createTerminal(input: LocalCreateTerminalInput): Promise<{ terminalId: string }>
  updateTerminal(input: LocalUpdateTerminalInput): Promise<void>
  restartTerminal(terminalId: string): Promise<void>
  deleteTerminal(terminalId: string): Promise<void>
  getTerminalDirectory(terminalId: string): Promise<{ path: string; source?: string | undefined }>
}

export function createTerminalManagementApi(
  session: ProtoClientSession,
  machineId: string,
): TerminalManagementApi {
  return createProtoTerminalManagementApi(session, machineId)
}

function createProtoTerminalManagementApi(session: ProtoClientSession, machineId: string): TerminalManagementApi {
  if (session.stamp.endpointId !== machineId) {
    throw new Error(`terminal management session machine mismatch: connected to ${session.stamp.endpointId}, expected ${machineId}`)
  }
  const terminalRef = (terminalId: string) => create(TerminalRefSchema, { endpointId: machineId, terminalId })
  const execute = (caseName: Parameters<typeof protoCommand>[0], value: Parameters<typeof protoCommand>[1]) =>
    session.execute(protoCommand(caseName, value))
  return {
    async createTerminal(input) {
      const terminal = create(TerminalCreateSpecSchema, {
        name: input.name ?? '',
        command: input.command ?? [],
        cwd: input.cwd ?? '',
        env: input.environment ? [input.environment] : [],
        scrollbackRows: finiteInt(input.scrollbackSize),
        scrollbackMaxBytes: BigInt(finiteInt(input.scrollbackMaxBytes)),
        scrollbackMaxAgeSeconds: BigInt(finiteInt(input.scrollbackMaxAgeSeconds)),
        tags: terminalTags(input),
      })
      const result = await execute('terminalCreate', create(TerminalCreateCommandSchema, { terminal }))
      if (result.result.case !== 'terminalCreate' || !result.result.value.terminal?.ref?.terminalId) {
        throw new Error('terminal create returned no terminal reference')
      }
      return { terminalId: result.result.value.terminal.ref.terminalId }
    },
    async updateTerminal(input) {
      const result = await execute('terminalSetMetadata', create(TerminalSetMetadataCommandSchema, {
        terminal: terminalRef(input.terminalId),
        name: input.name ?? '',
        tags: terminalTags(input),
      }))
      assertAcknowledge(result, 'terminal metadata update')
    },
    async restartTerminal(terminalId) {
      assertAcknowledge(await execute('terminalRestart', create(TerminalRestartCommandSchema, { terminal: terminalRef(terminalId) })), 'terminal restart')
    },
    async deleteTerminal(terminalId) {
      assertAcknowledge(await execute('terminalRemove', create(TerminalRemoveCommandSchema, { terminal: terminalRef(terminalId) })), 'terminal remove')
    },
    async getTerminalDirectory(terminalId) {
      const result = await execute('terminalGet', create(TerminalGetCommandSchema, { terminal: terminalRef(terminalId) }))
      if (result.result.case !== 'terminalGet' || !result.result.value.terminal) throw new Error('terminal get returned no terminal')
      const terminal = result.result.value.terminal
      return {
        path: terminal.liveCwd || terminal.cwd || '',
        source: terminal.liveCwd ? 'live' : terminal.cwd ? 'metadata' : undefined,
      }
    },
  }
}

function protoCommand(caseName: string, value: object) {
  return create(CommandEnvelopeSchema, { command: { case: caseName, value } } as never)
}

function assertAcknowledge(result: Awaited<ReturnType<ProtoClientSession['execute']>>, operation: string): void {
  if (result.result.case !== 'acknowledge') throw new Error(`${operation} was not acknowledged`)
}

function finiteInt(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) ? Math.max(0, Math.floor(value)) : 0
}

function terminalTags(input: {
  cwd?: string | undefined
  environment?: string | undefined
  sizeLockMode?: LocalCreateTerminalInput['sizeLockMode'] | LocalUpdateTerminalInput['sizeLockMode']
}): Record<string, string> {
  const tags: Record<string, string> = {}
  if (input.sizeLockMode) {
    tags['muxvia.size_lock'] = input.sizeLockMode
  }
  if (input.cwd) {
    tags.cwd = input.cwd
  }
  if (input.environment) {
    tags.environment = input.environment
  }
  return tags
}
