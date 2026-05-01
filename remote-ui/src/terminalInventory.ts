import {
  initialConnectionSnapshot,
  reduceConnectionMessage,
  type ConnectionSnapshot,
} from './connectionMessageReducer'
import { assertRemoteModelShape, normalizeTerminal, type Terminal } from './model'

export interface TerminalInventorySnapshot {
  machineId: string
  terminals: Terminal[]
  connection: ConnectionSnapshot
}

export interface TerminalInventoryInput {
  machine_id?: string
  machineId?: string
  terminals?: Record<string, unknown>[]
}

export function normalizeTerminalInventory(input: TerminalInventoryInput): TerminalInventorySnapshot {
  assertRemoteModelShape(input)
  assertNoTerminalListSessionConcepts(input)

  const machineId = input.machine_id ?? input.machineId
  if (!machineId) {
    throw new Error('machine_id is required')
  }
  if (!Array.isArray(input.terminals)) {
    throw new Error('terminals must be an array')
  }

  return createTerminalInventorySnapshot(
    machineId,
    input.terminals.map((terminal) => {
      assertNoTerminalListSessionConcepts(terminal)
      const normalized = normalizeTerminal({
        ...terminal,
        machine_id: terminal.machine_id ?? terminal.machineId ?? machineId,
        title: terminal.title ?? terminal.name ?? terminal.terminal_id ?? terminal.terminalId,
      })
      if (normalized.machineId !== machineId) {
        throw new Error(`terminal ${normalized.terminalId} belongs to ${normalized.machineId}, not ${machineId}`)
      }
      return normalized
    }),
  )
}

export function createTerminalInventorySnapshot(
  machineId: string,
  terminals: Terminal[],
): TerminalInventorySnapshot {
  return {
    machineId,
    terminals: terminals.map((terminal) => ({ ...terminal, machineId })),
    connection: reduceConnectionMessage(initialConnectionSnapshot(), {
      type: 'user.connectMachine',
      machineId,
    }),
  }
}

export function selectTerminal(
  snapshot: TerminalInventorySnapshot,
  terminalId: string,
): TerminalInventorySnapshot {
  return {
    ...snapshot,
    connection: reduceConnectionMessage(snapshot.connection, {
      type: 'user.openTerminal',
      machineId: snapshot.machineId,
      terminalId,
    }),
  }
}

function assertNoTerminalListSessionConcepts(value: unknown): void {
  const seen = new Set<unknown>()
  const blocked = new Set(['session', 'sessions', 'session_id', 'sessionId'])

  function walk(current: unknown): void {
    if (!current || typeof current !== 'object') return
    if (seen.has(current)) return
    seen.add(current)

    if (Array.isArray(current)) {
      for (const item of current) walk(item)
      return
    }

    for (const [key, nested] of Object.entries(current as Record<string, unknown>)) {
      if (blocked.has(key)) {
        throw new Error(`terminal inventory must not contain ${key}`)
      }
      walk(nested)
    }
  }

  walk(value)
}
