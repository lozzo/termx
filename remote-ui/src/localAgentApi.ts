import { normalizeMachine, normalizeTerminal, type Machine, type Terminal } from './model'
import type { LocalAgentApi, LocalPairInput, LocalPairResult, LocalRTCAnswer, LocalRTCOffer, LocalStatus } from './transport'

export interface LocalAgentApiOptions {
  baseUrl?: string | undefined
  fetch?: typeof globalThis.fetch | undefined
  machineId?: string | undefined
}

export function createLocalAgentApi(options: LocalAgentApiOptions = {}): LocalAgentApi {
  const fetchImpl = options.fetch ?? globalThis.fetch
  if (!fetchImpl) {
    throw new Error('fetch is required for local agent api')
  }
  const baseUrl = normalizeBaseUrl(options.baseUrl ?? '')
  let knownMachineId = options.machineId
  let knownStatus: LocalStatus | null = null

  async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await fetchImpl(joinURL(baseUrl, path), {
      ...init,
      headers: {
        accept: 'application/json',
        ...(init?.body ? { 'content-type': 'application/json' } : {}),
        ...init?.headers,
      },
    })
    const body = await readJSON(response)
    if (!response.ok) {
      throw new Error(localErrorMessage(body, response.status))
    }
    return body as T
  }

  return {
    async getStatus() {
      const raw = await requestJSON<Record<string, unknown>>('/api/local/status')
      const status = normalizeLocalStatus(raw, baseUrl)
      knownMachineId = status.machine.machineId
      knownStatus = status
      return status
    },
    async listTerminals() {
      const raw = await requestJSON<Record<string, unknown>>('/api/local/terminals')
      const machineId = knownMachineId ?? knownStatus?.machine.machineId
      if (!machineId) {
        throw new Error('machineId is required before listing local terminals')
      }
      return normalizeLocalTerminals(raw, machineId)
    },
    async pair(input: LocalPairInput): Promise<LocalPairResult> {
      const raw = await requestJSON<Record<string, unknown>>('/api/local/pair', {
        method: 'POST',
        body: JSON.stringify({
          pair_session_id: input.pairSessionId,
          pair_secret: input.pairSecret,
          app_device_id: input.appDeviceId,
          app_name: input.appName,
          app_public_key: input.appPublicKey,
          requested_capabilities: input.requestedCapabilities,
        }),
      })
      const machineId = requiredString(raw, 'machine_id')
      const appCertificate = raw.app_certificate
      if (typeof appCertificate !== 'object' || appCertificate === null || Array.isArray(appCertificate)) {
        throw new Error('app_certificate must be an object')
      }
      return {
        machineId,
        appCertificate: JSON.stringify(appCertificate),
        expiresAt: requiredString(raw, 'expires_at'),
      }
    },
    async createRTCAnswer(input: LocalRTCOffer): Promise<LocalRTCAnswer> {
      const raw = await requestJSON<Record<string, unknown>>('/api/local/rtc/offer', {
        method: 'POST',
        body: JSON.stringify({
          app_certificate: parseCertificate(input.appCertificate),
          offer: {
            session_id: input.sessionId,
            machine_id: input.machineId,
            terminal_id: input.terminalId,
            sdp: input.sdp,
            ice_candidates: [],
          },
          signature: {
            algorithm: 'ed25519',
            nonce: input.nonce,
            timestamp: Number.parseInt(input.timestamp, 10),
            value: input.appSignature,
          },
          client: {
            type: 'browser',
            transport: 'local',
          },
        }),
      })
      return normalizeRTCAnswer(raw)
    },
  }
}

function normalizeLocalStatus(raw: Record<string, unknown>, baseUrl: string): LocalStatus {
  const machineId = requiredString(raw, 'machine_id')
  const httpUrl = normalizeBaseUrl(getNestedString(raw.local_rtc, 'http_url') ?? baseUrl)
  const rtcOfferUrl = joinURL(httpUrl, '/api/local/rtc/offer')
  const machine: Machine = normalizeMachine({
    machine_id: machineId,
    name: optionalString(raw, 'machine_name') ?? machineId,
    state: raw.remote_enabled === false ? 'offline' : 'online',
    last_seen_at: optionalString(raw, 'updated_at'),
    local_rtc: cleanOptional({
      signaling_url: rtcOfferUrl,
      ice_tcp_url: iceTCPUrl(raw.local_rtc, httpUrl),
    }),
  })
  return {
    machine,
    localWeb: {
      httpUrl,
      rtcOfferUrl,
    },
  }
}

function normalizeLocalTerminals(raw: Record<string, unknown>, machineId: string): Terminal[] {
  const terminals = raw.terminals
  if (!Array.isArray(terminals)) {
    throw new Error('terminals must be an array')
  }
  return terminals.map((terminal) => {
    if (typeof terminal !== 'object' || terminal === null || Array.isArray(terminal)) {
      throw new Error('terminal must be an object')
    }
    const record = terminal as Record<string, unknown>
    const explicitMachineID = optionalString(record, 'machine_id')
    if (explicitMachineID !== undefined && explicitMachineID !== machineId) {
      throw new Error(`local terminal machine mismatch: ${explicitMachineID} != ${machineId}`)
    }
    return normalizeTerminal({
      ...record,
      machine_id: explicitMachineID ?? machineId,
      title: optionalString(record, 'title') ?? optionalString(record, 'name'),
      command: commandToString(record.command),
      last_active_at: optionalString(record, 'last_active_at'),
      size_locked: optionalBoolean(record, 'size_locked'),
      size_lock_mode: optionalString(record, 'size_lock_mode'),
      cwd: optionalString(record, 'cwd'),
      environment: optionalString(record, 'environment'),
    })
  })
}

function normalizeRTCAnswer(raw: Record<string, unknown>): LocalRTCAnswer {
  const answer = raw.answer
  if (typeof answer !== 'object' || answer === null || Array.isArray(answer)) {
    throw new Error('answer must be an object')
  }
  const record = answer as Record<string, unknown>
  const out: LocalRTCAnswer = {
    sessionId: requiredString(record, 'session_id'),
    answer: {
      type: 'answer',
      sdp: requiredString(record, 'sdp'),
    },
  }
  if (typeof raw.ice_tcp_enabled === 'boolean') {
    out.iceTCP = { enabled: raw.ice_tcp_enabled }
  }
  return out
}

async function readJSON(response: Response): Promise<unknown> {
  const text = await response.text()
  if (!text) return null
  try {
    return JSON.parse(text) as unknown
  } catch {
    throw new Error('local api returned invalid JSON')
  }
}

function localErrorMessage(body: unknown, status: number): string {
  if (typeof body === 'object' && body !== null && !Array.isArray(body)) {
    const record = body as Record<string, unknown>
    const error = record.error
    if (typeof error === 'object' && error !== null && !Array.isArray(error)) {
      const message = (error as Record<string, unknown>).message
      if (typeof message === 'string') return message
    }
  }
  return `local api failed: ${status}`
}

function parseCertificate(value: string): unknown {
  try {
    return JSON.parse(value) as unknown
  } catch {
    throw new Error('appCertificate must be JSON')
  }
}

function iceTCPUrl(localRTC: unknown, httpUrl: string): string | undefined {
  const enabled = getNestedBoolean(localRTC, 'ice_tcp_enabled')
  const port = getNestedNumber(localRTC, 'ice_tcp_port')
  if (!enabled || port === undefined) return undefined
  return `tcp://${new URL(httpUrl).hostname}:${port}`
}

function commandToString(value: unknown): string | undefined {
  if (value === undefined || value === null) return undefined
  if (typeof value === 'string') return value
  if (Array.isArray(value) && value.every((part) => typeof part === 'string')) {
    return value.join(' ')
  }
  throw new Error('command must be a string or string array')
}

function normalizeBaseUrl(value: string): string {
  return value.replace(/\/+$/, '')
}

function joinURL(baseUrl: string, path: string): string {
  if (!baseUrl) return path
  return `${baseUrl}${path.startsWith('/') ? path : `/${path}`}`
}

function requiredString(record: Record<string, unknown>, key: string): string {
  const value = optionalString(record, key)
  if (!value) throw new Error(`${key} is required`)
  return value
}

function optionalString(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key]
  if (value === undefined || value === null) return undefined
  if (typeof value !== 'string') throw new Error(`${key} must be a string`)
  return value
}

function optionalBoolean(record: Record<string, unknown>, key: string): boolean | undefined {
  const value = record[key]
  if (value === undefined || value === null) return undefined
  if (typeof value !== 'boolean') throw new Error(`${key} must be a boolean`)
  return value
}

function getNestedString(value: unknown, key: string): string | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined
  return optionalString(value as Record<string, unknown>, key)
}

function getNestedNumber(value: unknown, key: string): number | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined
  const nested = (value as Record<string, unknown>)[key]
  if (nested === undefined || nested === null) return undefined
  if (typeof nested !== 'number' || !Number.isFinite(nested)) throw new Error(`${key} must be a finite number`)
  return nested
}

function getNestedBoolean(value: unknown, key: string): boolean | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined
  const nested = (value as Record<string, unknown>)[key]
  if (nested === undefined || nested === null) return undefined
  if (typeof nested !== 'boolean') throw new Error(`${key} must be a boolean`)
  return nested
}

function cleanOptional<T extends object>(record: T): T {
  for (const key of Object.keys(record) as (keyof T)[]) {
    if (record[key] === undefined) delete record[key]
  }
  return record
}
