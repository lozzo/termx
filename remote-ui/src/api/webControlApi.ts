import type { RemoteRuntimeFetch, RtcConnectOptions } from '../core/transport'
import { serviceFetchError } from './networkErrors'

export type WebControlFetch = RemoteRuntimeFetch

export interface WebControlApiOptions {
  baseUrl: string
  accessToken?: string | undefined
  fetch: WebControlFetch
}

export interface WebControlApi {
  login(input: WebControlLoginInput, options?: RtcConnectOptions): Promise<WebControlAuthResult>
  me(options?: RtcConnectOptions): Promise<WebControlUser>
  listMachines(options?: RtcConnectOptions): Promise<WebControlMachine[]>
  pairMachine(input: WebControlPairMachineInput, options?: RtcConnectOptions): Promise<WebControlPairMachineResult>
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
  source: 'cloud' | 'local'
  controlUrl?: string | undefined
  hubId?: string | undefined
  currentHubUrl?: string | undefined
  hubUrls: string[]
  localHubUrls?: string[] | undefined
  localFallbackHubUrls?: string[] | undefined
  hubStatus?: string | undefined
  lastSeen?: string | undefined
  createdAt?: string | undefined
}

export interface WebControlPairMachineInput {
  machineId: string
  pairSessionId: string
  pairSecret: string
  appDeviceId: string
  appName: string
  requestedCapabilities: string[]
}

export interface WebControlPairMachineResult {
  claimId: string
  machineId: string
  machineName?: string | undefined
  sessionToken: string
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
    this.fetchImpl = options.fetch
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

  async pairMachine(input: WebControlPairMachineInput, options: RtcConnectOptions = {}): Promise<WebControlPairMachineResult> {
    const machineId = requiredString(input.machineId, 'machine_id')
    const response = await this.requestJSON('POST', `/api/v1/machines/${encodeURIComponent(machineId)}/pairing/claims`, {
      auth: 'bearer',
      signal: options.signal,
      body: {
        pair_session_id: requiredString(input.pairSessionId, 'pair_session_id'),
        pair_secret: requiredString(input.pairSecret, 'pair_secret'),
        app_device_id: requiredString(input.appDeviceId, 'app_device_id'),
        app_name: requiredString(input.appName, 'app_name'),
        requested_capabilities: input.requestedCapabilities,
      },
    })
    if (optionalBooleanField(response, 'pending') === true) {
      throw new Error('Web Control pairing is pending; make sure the TermX agent is online and try Pair Device again')
    }
    return {
      claimId: stringField(response, 'claim_id'),
      machineId: stringField(response, 'machine_id'),
      ...(optionalStringField(response, 'machine_name') ? { machineName: optionalStringField(response, 'machine_name') } : {}),
      sessionToken: stringField(response, 'session_token'),
      expiresAt: stringField(response, 'expires_at'),
    }
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
    const url = this.url(path)
    let response: Response
    try {
      response = await this.fetchImpl(url, init)
    } catch (err) {
      throw serviceFetchError('Web Control', url, err)
    }
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
  const currentHubUrl = currentHubUrlField(response)
  return {
    id: stringField(response, 'id'),
    name: stringField(response, 'name'),
    ...(optionalStringField(response, 'hostname') ? { hostname: optionalStringField(response, 'hostname') } : {}),
    ...(optionalStringField(response, 'os_info') ? { osInfo: optionalStringField(response, 'os_info') } : {}),
    online: booleanField(response, 'online'),
    paired: booleanField(response, 'paired'),
    source: 'cloud',
    ...(optionalStringField(response, 'control_url') ? { controlUrl: optionalStringField(response, 'control_url') } : {}),
    ...(optionalStringField(response, 'hub_id') ? { hubId: optionalStringField(response, 'hub_id') } : {}),
    ...(currentHubUrl ? { currentHubUrl } : {}),
    hubUrls: hubUrlsField(response),
    ...(optionalStringField(response, 'hub_status') ? { hubStatus: optionalStringField(response, 'hub_status') } : {}),
    ...(optionalStringField(response, 'last_seen') ? { lastSeen: optionalStringField(response, 'last_seen') } : {}),
    ...(optionalStringField(response, 'created_at') ? { createdAt: optionalStringField(response, 'created_at') } : {}),
  }
}

function currentHubUrlField(response: Record<string, unknown>): string | undefined {
  return optionalStringField(response, 'current_hub_url') ?? optionalStringField(response, 'hub_url')
}

function hubUrlsField(response: Record<string, unknown>): string[] {
  const currentHubUrl = currentHubUrlField(response)
  if (currentHubUrl) return [currentHubUrl]
  if (Array.isArray(response.hub_urls)) {
    return response.hub_urls.filter((value): value is string => typeof value === 'string' && value.trim() !== '')
  }
  const legacy = optionalStringField(response, 'hub_http_url')
  return legacy ? [legacy] : []
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

function optionalStringField(value: Record<string, unknown>, key: string): string | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'string' || field.trim() === '') {
    throw new Error(`Web Control response ${key} must be a string`)
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

function optionalBooleanField(value: Record<string, unknown>, key: string): boolean | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'boolean') {
    throw new Error(`Web Control response ${key} must be a boolean`)
  }
  return field
}

function requiredString(value: unknown, label: string): string {
  if (typeof value !== 'string' || value.trim() === '') {
    throw new Error(`Web Control ${label} is required`)
  }
  return value.trim()
}
