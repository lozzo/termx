import type { AppMachineRecord, AppMachineState, AppMachineSource } from './appMachine'
import type { PairingPayload } from './pairingPayload'
import type { ConnectionPath } from '../core/transport'

export interface StoredMachineRecord extends AppMachineRecord {
  addresses: StoredMachineAddresses
  endpoints: StoredMachineEndpoints
  pairing?: StoredMachinePairing | undefined
  appBootstrap?: StoredAppBootstrap | undefined
  addedAt: string
  updatedAt: string
}

export interface StoredMachineAddresses {
  local: string[]
  lan: string[]
  public: string[]
}

export interface StoredMachineEndpoints {
  webControl?: string | undefined
  hub?: string | undefined
}

export interface StoredMachinePairing {
  sessionId: string
  secret: string
  expiresAt?: string | undefined
}

export interface StoredAppBootstrap {
}

export interface MachineStore {
  listMachines(): StoredMachineRecord[]
  getMachine(machineId: string): StoredMachineRecord | null
  saveMachine(record: StoredMachineRecord): StoredMachineRecord
  saveFromPairingPayload(payload: PairingPayload): StoredMachineRecord
  forgetMachine(machineId: string): void
}

export interface MachineStoreOptions {
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
  now?: (() => Date) | undefined
}

const storeKey = 'termx.app.machines.v1'

export function createMachineStore(options: MachineStoreOptions): MachineStore {
  const now = () => (options.now?.() ?? new Date()).toISOString()
  return {
    listMachines() {
      return readMachines(options.storage)
    },
    getMachine(machineId) {
      return readMachines(options.storage).find((record) => record.machineId === machineId) ?? null
    },
    saveMachine(record) {
      rejectPrivateKeyMaterial(record)
      const clean = normalizeStoredMachine(record)
      const machines = upsertMachine(readMachines(options.storage), clean)
      writeMachines(options.storage, machines)
      return clean
    },
    saveFromPairingPayload(payload) {
      rejectPrivateKeyMaterial(payload)
      const machines = readMachines(options.storage)
      const existing = machines.find((record) => record.machineId === payload.machine.id)
      const timestamp = now()
      const next: StoredMachineRecord = normalizeStoredMachine({
        machineId: payload.machine.id,
        name: payload.machine.name,
        ...(payload.machine.hostname ? { hostname: payload.machine.hostname } : {}),
        state: existing?.state ?? 'unknown',
        terminalCount: existing?.terminalCount ?? 0,
        ...(existing?.lastSeenAt ? { lastSeenAt: existing.lastSeenAt } : {}),
        ...(existing?.lastConnectionPath ? { lastConnectionPath: existing.lastConnectionPath } : {}),
        preferredPath: payload.preferredPath,
        ...(existing?.relayInUse !== undefined ? { relayInUse: existing.relayInUse } : {}),
        source: existing?.source ?? 'local',
        addresses: {
          local: payload.addresses.local,
          lan: payload.addresses.lan,
          public: payload.addresses.public,
        },
        endpoints: payload.endpoints,
        pairing: payload.pairing,
        ...(isEmptyBootstrap(payload.bootstrap) ? {} : { appBootstrap: payload.bootstrap }),
        addedAt: existing?.addedAt ?? timestamp,
        updatedAt: timestamp,
      })
      writeMachines(options.storage, upsertMachine(machines, next))
      return next
    },
    forgetMachine(machineId) {
      const machines = readMachines(options.storage).filter((record) => record.machineId !== machineId)
      if (machines.length === 0) {
        options.storage.removeItem(storeKey)
        return
      }
      writeMachines(options.storage, machines)
    },
  }
}

function readMachines(storage: Pick<Storage, 'getItem'>): StoredMachineRecord[] {
  const raw = storage.getItem(storeKey)
  if (!raw) return []
  const parsed = JSON.parse(raw)
  rejectPrivateKeyMaterial(parsed)
  if (!Array.isArray(parsed)) {
    throw new Error('stored machines must be an array')
  }
  return parsed.map((record) => normalizeStoredMachine(recordValue(record, 'stored machine')))
}

function writeMachines(storage: Pick<Storage, 'setItem'>, machines: StoredMachineRecord[]): void {
  rejectPrivateKeyMaterial(machines)
  storage.setItem(storeKey, JSON.stringify(machines))
}

function upsertMachine(machines: StoredMachineRecord[], record: StoredMachineRecord): StoredMachineRecord[] {
  const index = machines.findIndex((item) => item.machineId === record.machineId)
  if (index === -1) {
    return [...machines, record]
  }
  const next = [...machines]
  next[index] = record
  return next
}

function normalizeStoredMachine(value: Record<string, unknown> | StoredMachineRecord): StoredMachineRecord {
  const record = value as Record<string, unknown>
  const machineId = stringField(record, 'machineId')
  const name = stringField(record, 'name')
  const state = machineState(record.state)
  const terminalCount = numberField(record, 'terminalCount')
  const source = machineSource(record.source)
  const addresses = addressesField(record.addresses)
  const endpoints = endpointsField(record.endpoints)
  return {
    machineId,
    name,
    ...(optionalString(record.hostname) ? { hostname: optionalString(record.hostname) } : {}),
    state,
    terminalCount,
    ...(optionalString(record.lastSeenAt) ? { lastSeenAt: optionalString(record.lastSeenAt) } : {}),
    ...(connectionPathOrUndefined(record.lastConnectionPath)
      ? { lastConnectionPath: connectionPathOrUndefined(record.lastConnectionPath) }
      : {}),
    ...(connectionPathOrUndefined(record.preferredPath)
      ? { preferredPath: connectionPathOrUndefined(record.preferredPath) }
      : {}),
    ...(typeof record.relayInUse === 'boolean' ? { relayInUse: record.relayInUse } : {}),
    source,
    addresses,
    endpoints,
    ...(record.pairing !== undefined ? { pairing: pairingField(record.pairing) } : {}),
    ...(record.appBootstrap !== undefined ? { appBootstrap: appBootstrapField(record.appBootstrap) } : {}),
    addedAt: stringField(record, 'addedAt'),
    updatedAt: stringField(record, 'updatedAt'),
  }
}

function addressesField(value: unknown): StoredMachineAddresses {
  const record = recordValue(value, 'stored machine addresses')
  return {
    local: stringArray(record.local),
    lan: stringArray(record.lan),
    public: stringArray(record.public),
  }
}

function endpointsField(value: unknown): StoredMachineEndpoints {
  const record = recordValue(value, 'stored machine endpoints')
  return {
    ...(optionalString(record.webControl) ? { webControl: optionalString(record.webControl) } : {}),
    ...(optionalString(record.hub) ? { hub: optionalString(record.hub) } : {}),
  }
}

function pairingField(value: unknown): StoredMachinePairing {
  const record = recordValue(value, 'stored machine pairing')
  return {
    sessionId: stringField(record, 'sessionId'),
    secret: stringField(record, 'secret'),
    ...(optionalString(record.expiresAt) ? { expiresAt: optionalString(record.expiresAt) } : {}),
  }
}

function appBootstrapField(value: unknown): StoredAppBootstrap {
  recordValue(value, 'stored app bootstrap')
  return {}
}

function rejectPrivateKeyMaterial(value: unknown): void {
  const stack: unknown[] = [value]
  while (stack.length > 0) {
    const current = stack.pop()
    if (Array.isArray(current)) {
      stack.push(...current)
      continue
    }
    if (!current || typeof current !== 'object') continue
    if (looksLikePrivateJwk(current)) {
      throw new Error('private key material must not be stored in MachineStore')
    }
    for (const [key, child] of Object.entries(current)) {
      const normalized = key.replace(/[-_\s]/g, '').toLowerCase()
      if (normalized.includes('machineprivatekey')) {
        throw new Error('machine private key material must not be stored in MachineStore')
      }
      if (normalized.includes('appprivatekey')) {
        throw new Error('app private key material must not be stored in MachineStore')
      }
      if (normalized === 'privatekey' || normalized.endsWith('privatekey')) {
        throw new Error('private key material must not be stored in MachineStore')
      }
      if (typeof child === 'string' && /BEGIN [A-Z ]*PRIVATE KEY/i.test(child)) {
        throw new Error('private key material must not be stored in MachineStore')
      }
      stack.push(child)
    }
  }
}

function looksLikePrivateJwk(value: object): boolean {
  const record = value as Record<string, unknown>
  if (typeof record.d !== 'string') return false
  return typeof record.kty === 'string' ||
    typeof record.crv === 'string' ||
    typeof record.x === 'string' ||
    typeof record.n === 'string'
}

function isEmptyBootstrap(value: StoredAppBootstrap): boolean {
  return Object.keys(value).length === 0
}

function machineState(value: unknown): AppMachineState {
  if (value === 'online' || value === 'offline' || value === 'stale' || value === 'unknown' || value === 'connecting') {
    return value
  }
  throw new Error(`invalid machine state ${String(value)}`)
}

function machineSource(value: unknown): AppMachineSource {
  if (value === 'local' || value === 'cloud' || value === 'manual') return value
  throw new Error(`invalid machine source ${String(value)}`)
}

function connectionPathOrUndefined(value: unknown): ConnectionPath | undefined {
  if (value === undefined || value === null) return undefined
  if (value === 'local' || value === 'public_p2p' || value === 'managed') return value
  throw new Error(`invalid connection path ${String(value)}`)
}

function recordValue(value: unknown, label: string): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${label} must be an object`)
  }
  return value as Record<string, unknown>
}

function stringField(record: Record<string, unknown>, key: string): string {
  const value = optionalString(record[key])
  if (!value) {
    throw new Error(`stored machine ${key} is required`)
  }
  return value
}

function numberField(record: Record<string, unknown>, key: string): number {
  const value = record[key]
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error(`stored machine ${key} must be a number`)
  }
  return value
}

function optionalString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed === '' ? undefined : trimmed
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string' && item.trim() !== '')
    .map((item) => item.trim())
}
