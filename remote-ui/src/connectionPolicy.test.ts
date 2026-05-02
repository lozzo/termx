import { describe, expect, it } from 'vitest'
import {
  createConnectionPolicySnapshot,
  evaluateConnectionCapability,
  requireConnectionCapability,
} from './connectionPolicy'
import { CONNECTION_PATHS, type ConnectionCapabilities, type ConnectionInfo } from './transport'
import policySource from './connectionPolicy.ts?raw'

describe('connection capability policy', () => {
  it('keeps path taxonomy separate from runtime capabilities', () => {
    const info = connectionInfo({ path: 'managed', relayInUse: true })
    const capabilities = connectionCapabilities({
      relayInUse: true,
      fileTransferAllowed: false,
      terminalManagementAllowed: false,
      denialReason: 'managed relay policy blocks file transfer for this plan',
    })

    const snapshot = createConnectionPolicySnapshot(info, capabilities)

    expect(CONNECTION_PATHS).toEqual(['local', 'public_p2p', 'managed'])
    expect(snapshot.path).toBe('managed')
    expect(snapshot.relayInUse).toBe(true)
    expect(snapshot.capabilities.file_transfer.allowed).toBe(false)
    expect(snapshot.capabilities.terminal_management.allowed).toBe(false)
    expect(JSON.stringify(snapshot)).not.toMatch(/"path":"relay"|paid_relay|managed_p2p|anonymous_p2p/)
  })

  it('returns the same capability shape for local, public_p2p, and managed sessions', () => {
    const capabilities = connectionCapabilities()

    const keys = CONNECTION_PATHS.map((path) => {
      const snapshot = createConnectionPolicySnapshot(connectionInfo({ path }), capabilities)
      return Object.keys(snapshot.capabilities).sort()
    })

    expect(keys[0]).toEqual(keys[1])
    expect(keys[1]).toEqual(keys[2])
    expect(keys[0]).toEqual(['api', 'events', 'file_transfer', 'terminal', 'terminal_management'])
  })

  it('denies capability use with policy reasons instead of transport mismatch errors', () => {
    const info = connectionInfo({ path: 'managed', relayInUse: true })
    const capabilities = connectionCapabilities({
      relayInUse: true,
      fileTransferAllowed: false,
      denialReason: 'upgrade required for file transfer over managed relay',
    })

    expect(evaluateConnectionCapability(info, capabilities, 'file_transfer')).toEqual({
      allowed: false,
      reason: 'upgrade required for file transfer over managed relay',
      relayInUse: true,
    })
    expect(() => requireConnectionCapability(info, capabilities, 'file_transfer')).toThrow(/upgrade required/)
    expect(() => requireConnectionCapability(info, capabilities, 'file_transfer')).not.toThrow(/transport/i)
  })

  it('does not define relay as a runtime connector or transport family', () => {
    expect(policySource).not.toMatch(/relayTransport|paid_relay|managed_p2p|anonymous_p2p/)
    expect(policySource).not.toMatch(/ConnectionPath\s*=.*relay/s)
  })
})

function connectionInfo(overrides: Partial<ConnectionInfo> = {}): ConnectionInfo {
  return {
    path: 'local',
    connectionId: 'connection-1',
    machineId: 'machine-local',
    terminalId: 'terminal-1',
    relayInUse: false,
    ...overrides,
  }
}

function connectionCapabilities(overrides: Partial<ConnectionCapabilities> = {}): ConnectionCapabilities {
  return {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    terminalManagementAllowed: true,
    relayInUse: false,
    ...overrides,
  }
}
