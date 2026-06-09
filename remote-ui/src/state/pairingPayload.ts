export interface PairingPayload {
  schemaVersion: 4
  machine: PairingPayloadMachine
  local: PairingPayloadLocal
  pairing: PairingPayloadPairing
}

export interface PairingPayloadMachine {
  id: string
  name: string
  hostname?: string | undefined
}

export interface PairingPayloadLocal {
  hubUrls: string[]
}

export interface PairingPayloadPairing {
  sessionId: string
  secret: string
  answerProofSecret?: string | undefined
  expiresAt?: string | undefined
}

export function parsePairingPayload(input: string): PairingPayload {
  const raw = parsePairingInput(input)
  rejectPrivateKeyMaterial(raw)
  const data = record(unwrapPairingPayload(raw), 'pairing payload')
  rejectPrivateKeyMaterial(data)
  const type = optionalString(data.type)
  if (type !== 'termx_pair') {
    throw new Error(`unsupported pairing payload type ${type}`)
  }
  const version = numberField(data, 'schema_version')
  if (version !== 4) {
    throw new Error(`unsupported pairing payload schema_version ${version}`)
  }
  return normalizeV4Payload(data)
}

function parsePairingInput(input: string): unknown {
  const trimmed = input.trim()
  if (trimmed === '') {
    throw new Error('pairing payload is required')
  }
  if (trimmed.startsWith('{')) {
    return JSON.parse(trimmed)
  }
  const payloadParam = trimmed.match(/^payload=([A-Za-z0-9_-]+)$/i)
  if (payloadParam?.[1]) {
    return JSON.parse(decodeBase64url(payloadParam[1]))
  }
  return parsePairingURI(extractPairingURI(trimmed) ?? trimmed)
}

function unwrapPairingPayload(value: unknown): unknown {
  const data = record(value, 'pairing payload')
  if (optionalString(data.type) === 'termx_pair') return data

  const payload = data.payload
  if (typeof payload === 'string') return parsePairingInput(payload)
  if (payload !== undefined && payload !== null) return payload

  const uri = optionalString(data.uri)
  if (uri) return parsePairingInput(uri)

  return data
}

function extractPairingURI(input: string): string | undefined {
  const start = input.search(/termx:\/\/pair\?payload=/i)
  if (start === -1) return undefined
  const lines = input.slice(start).split(/\r?\n/)
  const firstLine = lines[0] ?? ''
  const firstMatch = firstLine.match(/^termx:\/\/pair\?payload=([A-Za-z0-9_-]+)/i)
  if (!firstMatch?.[1]) return undefined

  let encoded = firstMatch[1]
  for (const line of lines.slice(1)) {
    const trimmed = line.trim()
    if (trimmed === '') break
    if (/^[A-Za-z_][A-Za-z0-9_-]*:\s*/.test(trimmed)) break
    const chunk = trimmed.replace(/[ \t]/g, '')
    if (!/^[A-Za-z0-9_-]+$/.test(chunk)) break
    encoded += chunk
  }
  return `termx://pair?payload=${encoded}`
}

function parsePairingURI(input: string): unknown {
  const url = new URL(input)
  if (url.protocol !== 'termx:' || url.hostname !== 'pair') {
    throw new Error('pairing QR must use termx://pair')
  }
  const encoded = url.searchParams.get('payload')
  if (!encoded) {
    throw new Error('pairing QR payload parameter is required')
  }
  return JSON.parse(decodeBase64url(encoded))
}

function normalizeV4Payload(data: Record<string, unknown>): PairingPayload {
  rejectUnknownFields(data, ['type', 'schema_version', 'machine', 'local', 'pairing'], 'pairing payload')
  const machine = record(data.machine, 'pairing payload machine')
  rejectUnknownFields(machine, ['id', 'name', 'hostname'], 'pairing payload machine')
  const local = optionalRecord(data.local)
  rejectUnknownFields(local, ['hub_urls'], 'pairing payload local')
  const pairing = record(data.pairing, 'pairing payload pairing')
  rejectUnknownFields(pairing, ['session_id', 'secret', 'answer_proof_secret', 'expires_at'], 'pairing payload pairing')
  return {
    schemaVersion: 4,
    machine: {
      id: stringField(machine, 'id'),
      name: stringField(machine, 'name'),
      ...(optionalString(machine.hostname) ? { hostname: optionalString(machine.hostname) } : {}),
    },
    local: {
      hubUrls: stringArrayField(local, 'hub_urls'),
    },
    pairing: {
      sessionId: stringField(pairing, 'session_id'),
      secret: stringField(pairing, 'secret'),
      ...(optionalString(pairing.answer_proof_secret) ? { answerProofSecret: optionalString(pairing.answer_proof_secret) } : {}),
      ...(optionalString(pairing.expires_at) ? { expiresAt: optionalString(pairing.expires_at) } : {}),
    },
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

function rejectUnknownFields(data: Record<string, unknown>, allowed: readonly string[], label: string): void {
  const allowedSet = new Set(allowed)
  const unknown = Object.keys(data).filter((key) => !allowedSet.has(key))
  if (unknown.length > 0) {
    throw new Error(`${label} contains unsupported field ${unknown[0]}`)
  }
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
