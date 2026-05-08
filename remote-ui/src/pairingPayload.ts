import type { ConnectionPath } from './transport'

export interface PairingPayload {
  schemaVersion: 3
  machine: PairingPayloadMachine
  addresses: PairingPayloadAddresses
  endpoints: PairingPayloadEndpoints
  pairing: PairingPayloadPairing
  bootstrap: PairingPayloadBootstrap
  preferredPath: ConnectionPath
}

export interface PairingPayloadMachine {
  id: string
  name: string
  hostname?: string | undefined
}

export interface PairingPayloadAddresses {
  local: string[]
  lan: string[]
  public: string[]
}

export interface PairingPayloadEndpoints {
  webControl?: string | undefined
}

export interface PairingPayloadPairing {
  sessionId: string
  secret: string
  answerProofSecret?: string | undefined
  expiresAt?: string | undefined
}

export interface PairingPayloadBootstrap {
}

const allowedPaths: readonly ConnectionPath[] = ['local', 'public_p2p', 'managed']

export function parsePairingPayload(input: string): PairingPayload {
  const raw = parsePairingInput(input)
  rejectPrivateKeyMaterial(raw)
  const data = record(raw, 'pairing payload')
  const type = optionalString(data.type)
  if (type !== 'termx_pair') {
    throw new Error(`unsupported pairing payload type ${type}`)
  }
  const version = numberField(data, 'schema_version')
  if (version !== 3) {
    throw new Error(`unsupported pairing payload schema_version ${version}`)
  }
  return normalizeV3Payload(data)
}

function parsePairingInput(input: string): unknown {
  const trimmed = input.trim()
  if (trimmed === '') {
    throw new Error('pairing payload is required')
  }
  if (trimmed.startsWith('{')) {
    return JSON.parse(trimmed)
  }
  const url = new URL(trimmed)
  if (url.protocol !== 'termx:' || url.hostname !== 'pair') {
    throw new Error('pairing QR must use termx://pair')
  }
  const encoded = url.searchParams.get('payload')
  if (!encoded) {
    throw new Error('pairing QR payload parameter is required')
  }
  return JSON.parse(decodeBase64url(encoded))
}

function normalizeV3Payload(data: Record<string, unknown>): PairingPayload {
  const machine = record(data.machine, 'pairing payload machine')
  const addresses = optionalRecord(data.addresses)
  const endpoints = optionalRecord(data.endpoints)
  const pairing = record(data.pairing, 'pairing payload pairing')
  return {
    schemaVersion: 3,
    machine: {
      id: stringField(machine, 'id'),
      name: stringField(machine, 'name'),
      ...(optionalString(machine.hostname) ? { hostname: optionalString(machine.hostname) } : {}),
    },
    addresses: {
      local: stringArrayField(addresses, 'local'),
      lan: stringArrayField(addresses, 'lan'),
      public: stringArrayField(addresses, 'public'),
    },
    endpoints: endpointsFromRecord(endpoints),
    pairing: {
      sessionId: stringField(pairing, 'session_id'),
      secret: stringField(pairing, 'secret'),
      ...(optionalString(pairing.answer_proof_secret) ? { answerProofSecret: optionalString(pairing.answer_proof_secret) } : {}),
      ...(optionalString(pairing.expires_at) ? { expiresAt: optionalString(pairing.expires_at) } : {}),
    },
    bootstrap: {},
    preferredPath: connectionPath(optionalString(data.preferred_path) ?? 'local'),
  }
}

function endpointsFromRecord(data: Record<string, unknown>): PairingPayloadEndpoints {
  return {
    ...(optionalString(data.web_control) ? { webControl: optionalString(data.web_control) } : {}),
  }
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
      throw new Error('pairing payload must not include private key material')
    }
    for (const [key, child] of Object.entries(current)) {
      const normalized = key.replace(/[-_\s]/g, '').toLowerCase()
      if (normalized.includes('machineprivatekey')) {
        throw new Error('pairing payload must not include machine private key material')
      }
      if (normalized.includes('appprivatekey')) {
        throw new Error('pairing payload must not include app private key material')
      }
      if (normalized === 'privatekey' || normalized.endsWith('privatekey') || normalized.includes('privatekey')) {
        throw new Error('pairing payload must not include private key material')
      }
      if (typeof child === 'string' && /BEGIN [A-Z ]*PRIVATE KEY/i.test(child)) {
        throw new Error('pairing payload must not include private key material')
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

function connectionPath(value: string): ConnectionPath {
  if ((allowedPaths as readonly string[]).includes(value)) return value as ConnectionPath
  throw new Error(`unsupported client-visible connection path ${value}`)
}

function decodeBase64url(value: string): string {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = normalized.padEnd(normalized.length + ((4 - (normalized.length % 4)) % 4), '=')
  if (typeof atob === 'function') {
    const binary = atob(padded)
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0))
    return new TextDecoder().decode(bytes)
  }
  throw new Error('base64url decoding is not available')
}

function record(value: unknown, label: string): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${label} must be an object`)
  }
  return value as Record<string, unknown>
}

function optionalRecord(value: unknown): Record<string, unknown> {
  if (value === undefined || value === null) return {}
  return record(value, 'pairing payload field')
}

function stringField(data: Record<string, unknown>, key: string): string {
  const value = optionalString(data[key])
  if (!value) {
    throw new Error(`pairing payload ${key} is required`)
  }
  return value
}

function numberField(data: Record<string, unknown>, key: string): number {
  const value = data[key]
  if (typeof value !== 'number' || !Number.isInteger(value)) {
    throw new Error(`pairing payload ${key} must be an integer`)
  }
  return value
}

function stringArrayField(data: Record<string, unknown>, key: string): string[] {
  const value = data[key]
  if (value === undefined || value === null) return []
  if (!Array.isArray(value)) {
    throw new Error(`pairing payload ${key} must be an array`)
  }
  return compactStrings(value.map((item) => optionalString(item)))
}

function optionalString(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed === '' ? undefined : trimmed
}

function compactStrings(values: Array<string | undefined>): string[] {
  return values.filter((value): value is string => typeof value === 'string' && value.trim() !== '')
}
