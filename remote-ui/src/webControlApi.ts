import type { RtcConnectOptions } from './transport'

export type WebControlFetch = (input: string, init?: RequestInit) => Promise<Response>

export interface WebControlApiOptions {
  baseUrl: string
  accessToken?: string | undefined
  fetch?: WebControlFetch | undefined
}

export interface WebControlApi {
  login(input: WebControlLoginInput, options?: RtcConnectOptions): Promise<WebControlAuthResult>
  me(options?: RtcConnectOptions): Promise<WebControlUser>
  listMachines(options?: RtcConnectOptions): Promise<WebControlMachine[]>
  createConnectTicket(input: WebControlConnectTicketInput, options?: RtcConnectOptions): Promise<WebControlConnectTicket>
}

export interface WebControlLoginInput {
  login: string
  password: string
}

export interface WebControlUser {
  id: string
  username: string
  email: string
  role?: string | undefined
}

export interface WebControlAuthResult {
  tokenType: 'Bearer'
  accessToken: string
  refreshToken?: string | undefined
  user: WebControlUser
}

export interface WebControlMachine {
  id: string
  name: string
  hostname?: string | undefined
  osInfo?: string | undefined
  online: boolean
  paired: boolean
  source: 'cloud'
  publicKey?: string | undefined
  machinePublicKeyFingerprint?: string | undefined
  controlUrl?: string | undefined
  hubId?: string | undefined
  hubHttpUrl?: string | undefined
  hubStatus?: string | undefined
  lastSeen?: string | undefined
  createdAt?: string | undefined
}

export interface WebControlConnectTicketInput {
  machineId: string
  terminalId?: string | undefined
  ttlSeconds?: number | undefined
}

export interface WebControlConnectTicket {
  connectTicket: string
  machineId: string
  terminalId?: string | undefined
  path: 'cloud'
  allowRelay: boolean
  relayInUse: boolean
  relayThrottled: boolean
  hubHttpUrl?: string | undefined
  expiresAt: string
}

export function createWebControlApi(options: WebControlApiOptions): WebControlApi {
  return new WebControlHttpApi(options)
}

class WebControlHttpApi implements WebControlApi {
  private readonly baseUrl: string
  private readonly accessToken: string | undefined
  private readonly fetchImpl: WebControlFetch

  constructor(options: WebControlApiOptions) {
    this.baseUrl = normalizeBaseUrl(options.baseUrl)
    if (options.accessToken !== undefined) {
      const accessToken = options.accessToken.trim()
      if (accessToken === '') {
        throw new Error('Web Control access token must be non-empty when provided')
      }
      this.accessToken = accessToken
    }
    this.fetchImpl = options.fetch ?? ((input, init) => globalThis.fetch(input, init))
  }

  async login(input: WebControlLoginInput, options: RtcConnectOptions = {}): Promise<WebControlAuthResult> {
    const login = input.login.trim()
    if (login === '') {
      throw new Error('Web Control login is required')
    }
    if (input.password === '') {
      throw new Error('Web Control password is required')
    }
    const response = await this.requestJSON('POST', '/api/v1/auth/login', {
      signal: options.signal,
      body: {
        email: login,
        password: input.password,
      },
    })
    return authResult(response)
  }

  async me(options: RtcConnectOptions = {}): Promise<WebControlUser> {
    const response = await this.requestJSON('GET', '/api/v1/auth/me', {
      auth: 'bearer',
      signal: options.signal,
    })
    return userRecord(record(response.user, 'Web Control user'))
  }

  async listMachines(options: RtcConnectOptions = {}): Promise<WebControlMachine[]> {
    const response = await this.requestJSON('GET', '/api/v1/machines', {
      auth: 'bearer',
      signal: options.signal,
    })
    const machines = response.machines
    if (!Array.isArray(machines)) {
      throw new Error('Web Control machines response machines is required')
    }
    return machines.map((machine) => machineRecord(machine))
  }

  async createConnectTicket(input: WebControlConnectTicketInput, options: RtcConnectOptions = {}): Promise<WebControlConnectTicket> {
    const machineId = requiredTrimmed(input.machineId, 'machine_id')
    const response = await this.requestJSON('POST', `/api/v1/machines/${encodeURIComponent(machineId)}/connect-tickets`, {
      auth: 'bearer',
      signal: options.signal,
      body: {
        terminal_id: input.terminalId?.trim() ?? '',
        ...(input.ttlSeconds !== undefined ? { ttl_seconds: input.ttlSeconds } : {}),
      },
    })
    return connectTicketRecord(response)
  }

  private async requestJSON(
    method: string,
    path: string,
    options: {
      auth?: 'bearer' | undefined
      signal?: AbortSignal | undefined
      body?: unknown
    } = {},
  ): Promise<Record<string, unknown>> {
    const headers: Record<string, string> = {}
    if (options.auth === 'bearer') {
      if (!this.accessToken) {
        throw new Error('Web Control access token is required')
      }
      headers.authorization = `Bearer ${this.accessToken}`
    }
    const init: RequestInit = {
      method,
      headers,
    }
    if (options.signal) {
      init.signal = options.signal
    }
    if (options.body !== undefined) {
      headers['content-type'] = 'application/json'
      init.body = JSON.stringify(options.body)
    }
    const response = await this.fetchImpl(this.url(path), init)
    if (!response.ok) {
      throw new Error(await errorMessage(response))
    }
    if (response.status === 204) {
      return {}
    }
    const text = await response.text()
    if (text.trim() === '') {
      return {}
    }
    return record(JSON.parse(text), 'Web Control response')
  }

  private url(path: string): string {
    return `${this.baseUrl}${path.replace(/^\//, '')}`
  }
}

function authResult(response: Record<string, unknown>): WebControlAuthResult {
  const tokenType = stringField(response, 'token_type')
  if (tokenType !== 'Bearer') {
    throw new Error(`Web Control token_type must be Bearer, got ${tokenType}`)
  }
  const refreshToken = optionalTrimmedStringField(response, 'refresh_token')
  return {
    tokenType: 'Bearer',
    accessToken: stringField(response, 'access_token'),
    ...(refreshToken ? { refreshToken } : {}),
    user: userRecord(record(response.user, 'Web Control auth user')),
  }
}

function userRecord(response: Record<string, unknown>): WebControlUser {
  return {
    id: stringField(response, 'id'),
    username: stringField(response, 'username'),
    email: stringField(response, 'email'),
    ...(optionalStringField(response, 'role') ? { role: optionalStringField(response, 'role') } : {}),
  }
}

function machineRecord(value: unknown): WebControlMachine {
  const response = record(value, 'Web Control machine')
  const source = optionalStringField(response, 'source') ?? 'cloud'
  if (source !== 'cloud') {
    throw new Error(`Web Control machine source must be cloud, got ${source}`)
  }
  return {
    id: stringField(response, 'id'),
    name: stringField(response, 'name'),
    ...(optionalStringField(response, 'hostname') ? { hostname: optionalStringField(response, 'hostname') } : {}),
    ...(optionalStringField(response, 'os_info') ? { osInfo: optionalStringField(response, 'os_info') } : {}),
    online: booleanField(response, 'online'),
    paired: booleanField(response, 'paired'),
    source: 'cloud',
    ...(optionalStringField(response, 'public_key') ? { publicKey: optionalStringField(response, 'public_key') } : {}),
    ...(optionalStringField(response, 'machine_public_key_fingerprint')
      ? { machinePublicKeyFingerprint: optionalStringField(response, 'machine_public_key_fingerprint') }
      : {}),
    ...(optionalStringField(response, 'control_url') ? { controlUrl: optionalStringField(response, 'control_url') } : {}),
    ...(optionalStringField(response, 'hub_id') ? { hubId: optionalStringField(response, 'hub_id') } : {}),
    ...(optionalStringField(response, 'hub_http_url') ? { hubHttpUrl: optionalStringField(response, 'hub_http_url') } : {}),
    ...(optionalStringField(response, 'hub_status') ? { hubStatus: optionalStringField(response, 'hub_status') } : {}),
    ...(optionalStringField(response, 'last_seen') ? { lastSeen: optionalStringField(response, 'last_seen') } : {}),
    ...(optionalStringField(response, 'created_at') ? { createdAt: optionalStringField(response, 'created_at') } : {}),
  }
}

function connectTicketRecord(response: Record<string, unknown>): WebControlConnectTicket {
  const path = stringField(response, 'path')
  if (path !== 'cloud') {
    throw new Error(`Web Control connect ticket path must be cloud, got ${path}`)
  }
  return {
    connectTicket: stringField(response, 'connect_ticket', 'id'),
    machineId: stringField(response, 'machine_id'),
    ...(optionalTrimmedStringField(response, 'terminal_id') ? { terminalId: optionalTrimmedStringField(response, 'terminal_id') } : {}),
    path: 'cloud',
    allowRelay: optionalBooleanField(response, 'allow_relay') ?? false,
    relayInUse: optionalBooleanField(response, 'relay_in_use') ?? false,
    relayThrottled: optionalBooleanField(response, 'relay_throttled') ?? false,
    ...(optionalStringField(response, 'hub_http_url') ? { hubHttpUrl: optionalStringField(response, 'hub_http_url') } : {}),
    expiresAt: stringField(response, 'expires_at'),
  }
}

async function errorMessage(response: Response): Promise<string> {
  const text = await response.text()
  if (text.trim() === '') {
    return `Web Control request failed with HTTP ${response.status}`
  }
  try {
    const data = record(JSON.parse(text), 'Web Control error response')
    const err = record(data.error, 'Web Control error')
    const code = optionalStringField(err, 'code') ?? `http_${response.status}`
    const message = optionalStringField(err, 'message') ?? response.statusText
    return `${code}: ${message}`
  } catch {
    return `Web Control request failed with HTTP ${response.status}: ${text}`
  }
}

function normalizeBaseUrl(raw: string): string {
  const trimmed = raw.trim()
  if (trimmed === '') {
    throw new Error('Web Control baseUrl is required')
  }
  return trimmed.endsWith('/') ? trimmed : `${trimmed}/`
}

function record(value: unknown, label: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`${label} must be an object`)
  }
  return value as Record<string, unknown>
}

function stringField(value: Record<string, unknown>, key: string, alias?: string): string {
  const field = value[key] ?? (alias ? value[alias] : undefined)
  if (typeof field !== 'string' || field.trim() === '') {
    throw new Error(`Web Control response ${key} is required`)
  }
  return field
}

function requiredTrimmed(value: unknown, label: string): string {
  const trimmed = typeof value === 'string' ? value.trim() : ''
  if (!trimmed) throw new Error(`Web Control ${label} is required`)
  return trimmed
}

function optionalStringField(value: Record<string, unknown>, key: string): string | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'string' || field.trim() === '') {
    throw new Error(`Web Control response ${key} must be a string`)
  }
  return field
}

function optionalBooleanField(value: Record<string, unknown>, key: string): boolean | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'boolean') {
    throw new Error(`Web Control ${key} must be a boolean`)
  }
  return field
}

function optionalTrimmedStringField(value: Record<string, unknown>, key: string): string | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'string') {
    throw new Error(`Web Control response ${key} must be a string`)
  }
  const trimmed = field.trim()
  return trimmed === '' ? undefined : trimmed
}

function booleanField(value: Record<string, unknown>, key: string): boolean {
  const field = value[key]
  if (typeof field !== 'boolean') {
    throw new Error(`Web Control response ${key} must be a boolean`)
  }
  return field
}
