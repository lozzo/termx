import type { ConnectionCapabilities, ConnectionInfo } from '../core/transport'

export type ConnectionCapabilityName =
  | 'terminal'
  | 'api'
  | 'events'
  | 'file_transfer'
  | 'terminal_management'

export interface ConnectionCapabilityDecision {
  allowed: boolean
  relayInUse: boolean
  reason?: string
}

export interface ConnectionPolicySnapshot {
  path: ConnectionInfo['path']
  relayInUse: boolean
  capabilities: Record<ConnectionCapabilityName, ConnectionCapabilityDecision>
}

const capabilityNames: ConnectionCapabilityName[] = [
  'terminal',
  'api',
  'events',
  'file_transfer',
  'terminal_management',
]

export function createConnectionPolicySnapshot(
  info: ConnectionInfo,
  capabilities: ConnectionCapabilities,
): ConnectionPolicySnapshot {
  return {
    path: info.path,
    relayInUse: info.relayInUse || capabilities.relayInUse,
    capabilities: Object.fromEntries(
      capabilityNames.map((name) => [name, evaluateConnectionCapability(info, capabilities, name)]),
    ) as Record<ConnectionCapabilityName, ConnectionCapabilityDecision>,
  }
}

export function evaluateConnectionCapability(
  info: ConnectionInfo,
  capabilities: ConnectionCapabilities,
  capability: ConnectionCapabilityName,
): ConnectionCapabilityDecision {
  const allowed = isCapabilityAllowed(capabilities, capability)
  const relayInUse = info.relayInUse || capabilities.relayInUse
  if (allowed) {
    return { allowed: true, relayInUse }
  }
  return {
    allowed: false,
    relayInUse,
    reason: capabilities.denialReason ?? defaultDenialReason(capability),
  }
}

export function requireConnectionCapability(
  info: ConnectionInfo,
  capabilities: ConnectionCapabilities,
  capability: ConnectionCapabilityName,
): void {
  const decision = evaluateConnectionCapability(info, capabilities, capability)
  if (decision.allowed) return
  throw new Error(decision.reason ?? defaultDenialReason(capability))
}

function isCapabilityAllowed(
  capabilities: ConnectionCapabilities,
  capability: ConnectionCapabilityName,
): boolean {
  switch (capability) {
    case 'terminal':
      return capabilities.terminalAllowed
    case 'api':
      return capabilities.apiAllowed
    case 'events':
      return capabilities.eventsAllowed
    case 'file_transfer':
      return capabilities.fileTransferAllowed
    case 'terminal_management':
      return capabilities.apiAllowed && capabilities.terminalManagementAllowed
  }
}

function defaultDenialReason(capability: ConnectionCapabilityName): string {
  return `${capability.replace(/_/g, ' ')} is not allowed by connection policy`
}
