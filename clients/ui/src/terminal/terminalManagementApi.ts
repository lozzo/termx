import type { LocalCreateTerminalInput, LocalUpdateTerminalInput, RtcSession } from '../core/transport'

export interface TerminalManagementApi {
  createTerminal(input: LocalCreateTerminalInput): Promise<{ terminalId: string }>
  updateTerminal(input: LocalUpdateTerminalInput): Promise<void>
  restartTerminal(terminalId: string): Promise<void>
  deleteTerminal(terminalId: string): Promise<void>
  getTerminalDirectory(terminalId: string): Promise<{ path: string; source?: string | undefined }>
}

export function createTerminalManagementApi(
  session: Pick<RtcSession, 'openApi' | 'getConnectionInfo'>,
  machineId: string,
): TerminalManagementApi {
  const api = async () => {
    const info = await session.getConnectionInfo()
    if (info.machineId !== machineId) {
      throw new Error(`terminal management session machine mismatch: connected to ${info.machineId}, expected ${machineId}`)
    }
    return session.openApi()
  }

  return {
    async createTerminal(input) {
      const channel = await api()
      const response = await channel.request<{ terminal_id?: string; terminalId?: string }>('create', {
        ...(input.command && input.command.length > 0 ? { command: input.command } : {}),
        ...(input.name ? { name: input.name } : {}),
        ...(input.cwd ? { dir: input.cwd } : {}),
        ...(input.environment ? { env: [input.environment] } : {}),
        ...(typeof input.scrollbackSize === 'number' && Number.isFinite(input.scrollbackSize) ? { scrollback_size: Math.floor(input.scrollbackSize) } : {}),
        ...(typeof input.scrollbackMaxBytes === 'number' && Number.isFinite(input.scrollbackMaxBytes) ? { scrollback_max_bytes: Math.floor(input.scrollbackMaxBytes) } : {}),
        ...(typeof input.scrollbackMaxAgeSeconds === 'number' && Number.isFinite(input.scrollbackMaxAgeSeconds) ? { scrollback_max_age_seconds: Math.floor(input.scrollbackMaxAgeSeconds) } : {}),
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
    async restartTerminal(terminalId) {
      const channel = await api()
      await channel.request('restart', { terminal_id: terminalId })
    },
    async deleteTerminal(terminalId) {
      const channel = await api()
      await channel.request('remove', { terminal_id: terminalId })
    },
    async getTerminalDirectory(terminalId) {
      const channel = await api()
      const response = await channel.request<{ cwd?: string; live_cwd?: string }>('get', { terminal_id: terminalId })
      return {
        path: response.live_cwd || response.cwd || '',
        source: response.live_cwd ? 'live' : response.cwd ? 'metadata' : undefined,
      }
    },
  }
}

function terminalTags(input: {
  cwd?: string | undefined
  environment?: string | undefined
  sizeLockMode?: LocalCreateTerminalInput['sizeLockMode'] | LocalUpdateTerminalInput['sizeLockMode']
}): Record<string, string> {
  const tags: Record<string, string> = {}
  if (input.sizeLockMode) {
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
