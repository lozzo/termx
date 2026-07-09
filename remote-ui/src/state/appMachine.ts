import type { ConnectionPath } from '../core/transport'

export type AppMachineState = 'online' | 'offline' | 'stale' | 'unknown' | 'connecting'
export type AppMachineSource = 'local' | 'hub' | 'manual'

export interface AppMachineRecord {
  machineId: string
  name: string
  hostname?: string | undefined
  state: AppMachineState
  terminalCount: number
  lastSeenAt?: string | undefined
  lastConnectionPath?: ConnectionPath | undefined
  preferredPath?: ConnectionPath | undefined
  relayInUse?: boolean | undefined
  source: AppMachineSource
}

export type ConnectionFlowStage =
  | 'idle'
  | 'trying_local'
  | 'trying_hub'
  | 'connected'
  | 'failed'

export interface ConnectionFlowSnapshot {
  stage: ConnectionFlowStage
  path?: ConnectionPath | undefined
  relayInUse?: boolean | undefined
  message?: string | undefined
}

export function formatMachineState(state: AppMachineState): string {
  if (state === 'online') return 'Online'
  if (state === 'offline') return 'Offline'
  if (state === 'stale') return 'Stale'
  if (state === 'connecting') return 'Connecting'
  return 'Unknown'
}

export function formatConnectionPath(path: ConnectionPath): string {
  if (path === 'hub') return 'Hub'
  return 'Local'
}

export function formatConnectionStage(stage: ConnectionFlowStage): string {
  if (stage === 'trying_local') return 'Trying local'
  if (stage === 'trying_hub') return 'Trying hub'
  if (stage === 'connected') return 'Connected'
  if (stage === 'failed') return 'Failed'
  return 'Idle'
}

export function formatTerminalCount(count: number): string {
  return `${count} ${count === 1 ? 'terminal' : 'terminals'}`
}

export function formatLastSeen(value: string | undefined): string {
  if (!value) return 'Never online'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const diffMs = Date.now() - date.getTime()
  if (diffMs < 60_000) return 'Just now'
  const diffMinutes = Math.floor(diffMs / 60_000)
  if (diffMinutes < 60) return `${diffMinutes} min ago`
  const diffHours = Math.floor(diffMinutes / 60)
  if (diffHours < 24) return `${diffHours} hr ago`
  const diffDays = Math.floor(diffHours / 24)
  if (diffDays < 7) return `${diffDays} day${diffDays === 1 ? '' : 's'} ago`
  return date.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
  })
}
