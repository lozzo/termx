import { normalizeMachine, type Machine } from '../core/model'
import type { LocalAgentApi, LocalStatus, RemoteRuntimeFetch } from '../core/transport'

export interface LocalAgentApiOptions {
  baseUrl?: string | undefined
  fetch: RemoteRuntimeFetch
}

export function createLocalAgentApi(options: LocalAgentApiOptions): LocalAgentApi {
  const fetchImpl = options.fetch
  const baseUrl = normalizeBaseUrl(options.baseUrl ?? '')

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
      return normalizeLocalStatus(raw, baseUrl)
    },
  }
}

function normalizeLocalStatus(raw: Record<string, unknown>, baseUrl: string): LocalStatus {
  const machineId = requiredString(raw, 'machine_id')
  const httpUrl = normalizeBaseUrl(getNestedString(raw.local_rtc, 'http_url') ?? baseUrl)
  const machine: Machine = normalizeMachine({
    machine_id: machineId,
    name: optionalString(raw, 'machine_name') ?? machineId,
    state: raw.remote_enabled === false ? 'offline' : 'online',
    last_seen_at: optionalString(raw, 'updated_at'),
    local_rtc: cleanOptional({
      signaling_url: httpUrl,
      ice_tcp_url: iceTCPUrl(raw.local_rtc, httpUrl),
    }),
  })
  return {
    machine,
    localWeb: {
      httpUrl,
      rtcOfferUrl: httpUrl,
    },
  }
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

function iceTCPUrl(localRTC: unknown, httpUrl: string): string | undefined {
  const enabled = getNestedBoolean(localRTC, 'ice_tcp_enabled')
  const port = getNestedNumber(localRTC, 'ice_tcp_port')
  if (!enabled || port === undefined) return undefined
  return `tcp://${new URL(httpUrl).hostname}:${port}`
}

function normalizeBaseUrl(value: string): string {
  return value.replace(/\/+$/, '')
}

function joinURL(baseUrl: string, path: string): string {
  if (/^https?:\/\//i.test(path)) return path
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
