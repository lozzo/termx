import type {
  ConnectionPath,
  RtcConnectionPhase,
  RtcConnectionStateSnapshot,
  RtcSubscription,
} from '../core/transport'

export interface ConnectionStatePublisher {
  subscribe(handler: (snapshot: RtcConnectionStateSnapshot) => void): RtcSubscription
  publish(snapshot: RtcConnectionStateSnapshot): void
}

export function createConnectionStatePublisher(): ConnectionStatePublisher {
  const handlers = new Set<(snapshot: RtcConnectionStateSnapshot) => void>()
  return {
    subscribe(handler) {
      handlers.add(handler)
      return { close: () => handlers.delete(handler) }
    },
    publish(snapshot) {
      for (const handler of handlers) handler(snapshot)
    },
  }
}

export function connectionStatusIsSettled(phase: RtcConnectionPhase | null | undefined): boolean {
  return phase === 'connected' || phase === 'idle'
}

export function connectionPhaseLabel(phase: RtcConnectionPhase | null | undefined): string {
  if (phase === 'probing') return 'Probing connection paths...'
  if (phase === 'connecting') return 'Connecting...'
  if (phase === 'connected') return 'Connected'
  if (phase === 'verifying') return 'Verifying connection...'
  if (phase === 'reconnecting') return 'Reconnecting...'
  if (phase === 'waiting_network') return 'Waiting for network...'
  if (phase === 'failed') return 'Connection failed'
  return 'Ready'
}

export function connectionPathLabel(path: ConnectionPath | undefined): string {
  if (path === 'public_p2p') return 'P2P'
  if (path === 'managed') return 'Managed'
  if (path === 'local') return 'Local'
  return 'Runtime'
}

export function connectionSnapshotFromStatus(input: {
  machineId: string
  statusText: string
  phase?: RtcConnectionPhase | undefined
  path?: ConnectionPath | undefined
  relayInUse?: boolean | undefined
}): RtcConnectionStateSnapshot {
  const status = input.statusText.trim()
  return {
    machineId: input.machineId,
    phase: input.phase ?? inferConnectionPhase(status),
    ...(input.path ? { path: input.path } : {}),
    statusText: status || connectionPhaseLabel(input.phase),
    relayInUse: input.relayInUse === true,
  }
}

export function inferConnectionPhase(statusText: string): RtcConnectionPhase {
  const text = statusText.toLowerCase()
  if (text.includes('connected')) return 'connected'
  if (text.includes('verify') || text.includes('verifying')) return 'verifying'
  if (text.includes('reconnect') || text.includes('switching') || text.includes('trying p2p')) return 'reconnecting'
  if (text.includes('waiting') && text.includes('network')) return 'waiting_network'
  if (text.includes('probe') || text.includes('racing') || text.includes('checking')) return 'probing'
  if (text.includes('fail') || text.includes('error')) return 'failed'
  return 'connecting'
}

export function connectionStateFromAttempt(input: {
  machineId: string
  stage: string
  message: string
  path?: ConnectionPath | undefined
  relayInUse?: boolean | undefined
}): RtcConnectionStateSnapshot {
  return {
    machineId: input.machineId,
    phase: phaseFromAttemptStage(input.stage),
    ...(input.path ? { path: input.path } : {}),
    statusText: input.message,
    relayInUse: input.relayInUse === true,
  }
}

function phaseFromAttemptStage(stage: string): RtcConnectionPhase {
  if (stage === 'connected') return 'connected'
  if (stage === 'failed') return 'failed'
  if (stage === 'trying_local' || stage === 'trying_public_p2p' || stage === 'trying_managed') return 'probing'
  return 'connecting'
}
