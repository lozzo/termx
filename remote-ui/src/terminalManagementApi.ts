import type { LocalCreateTerminalInput, LocalUpdateTerminalInput, RtcSession } from './transport'
import { requireConnectionCapability } from './connectionPolicy'

export interface TerminalManagementApi {
  createTerminal(input: LocalCreateTerminalInput): Promise<{ terminalId: string }>
  updateTerminal(input: LocalUpdateTerminalInput): Promise<void>
  deleteTerminal(terminalId: string): Promise<void>
}

export function createTerminalManagementApi(
  session: Pick<RtcSession, 'openApi' | 'getConnectionInfo' | 'getCapabilities'>,
  machineId: string,
): TerminalManagementApi {
  const api = async () => {
    const [info, capabilities] = await Promise.all([
      session.getConnectionInfo(),
      session.getCapabilities(),
    ])
    if (info.machineId !== machineId) {
      throw new Error(`terminal management session machine mismatch: connected to ${info.machineId}, expected ${machineId}`)
    }
    requireConnectionCapability(info, capabilities, 'terminal_management')
    return session.openApi()
  }

  return {
    async createTerminal(input) {
      const channel = await api()
      const response = await channel.request<{ terminal_id?: string; terminalId?: string }>('create', {
        command: input.command,
        ...(input.name ? { name: input.name } : {}),
        ...(input.cwd ? { dir: input.cwd } : {}),
        ...(input.environment ? { env: [input.environment] } : {}),
        tags: terminalTags({
          cwd: input.cwd,
          environment: input.environment,
          sizeLockMode: input.sizeLockMode,
        }),
      })
      const terminalId = response.terminal_id ?? response.terminalId
      if (!terminalId) throw new Error('terminal_id is required in terminal management create response')
      return { terminalId }
    },
    async updateTerminal(input) {
      const channel = await api()
      await channel.request('set_metadata', {
        terminal_id: input.terminalId,
        ...(input.name ? { name: input.name } : {}),
        tags: terminalTags({
          cwd: input.cwd,
          environment: input.environment,
          sizeLockMode: input.sizeLockMode,
        }),
      })
    },
    async deleteTerminal(terminalId) {
      const channel = await api()
      await channel.request('remove', { terminal_id: terminalId })
    },
  }
}

function terminalTags(input: {
  cwd?: string | undefined
  environment?: string | undefined
  sizeLockMode?: LocalCreateTerminalInput['sizeLockMode'] | LocalUpdateTerminalInput['sizeLockMode']
}): Record<string, string> {
  const tags: Record<string, string> = {}
  if (input.sizeLockMode && input.sizeLockMode !== 'off') {
    tags['termx.size_lock'] = input.sizeLockMode
  }
  if (input.cwd) {
    tags.cwd = input.cwd
  }
  if (input.environment) {
    tags.environment = input.environment
  }
  return tags
}
