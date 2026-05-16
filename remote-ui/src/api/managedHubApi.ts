import type { RemoteRuntimeFetch, RtcConnectOptions, RtcSessionDescription } from '../core/transport'
import { serviceFetchError } from './networkErrors'
import { normalizeHubBaseUrlCandidate } from './hubUrl'

export interface ManagedRelayPolicy {
  allowRelay: boolean
  allowRelayTransfer: boolean
}

export interface ManagedIceServer {
  urls: string[]
  username?: string | undefined
  credential?: string | undefined
}

export type ManagedHubFetch = RemoteRuntimeFetch

export interface ManagedHubApiOptions {
  baseUrl: string
  accessToken?: string | undefined
  fetch: ManagedHubFetch
}

export interface ManagedHubApi {
  getSessionIce(input: ManagedHubSessionIceInput, options?: RtcConnectOptions): Promise<ManagedHubSessionIceConfig>
  createSession(input: CreateManagedHubSessionInput, options?: RtcConnectOptions): Promise<ManagedHubCreateSessionResult>
  pollSessionAnswer(input: PollManagedHubSessionAnswerInput, options?: RtcConnectOptions): Promise<ManagedHubSession>
  pair(input: ManagedHubPairInput, options?: RtcConnectOptions): Promise<ManagedHubPairResult>
}

export interface ManagedHubSessionIceInput {
  machineId: string
  terminalId?: string | undefined
  sessionToken: string
}

export interface ManagedHubSessionIceConfig {
  path: 'managed'
  machineId: string
  terminalId?: string | undefined
  iceServers: ManagedIceServer[]
  relayPolicy: ManagedRelayPolicy
}

export interface CreateManagedHubSessionInput {
  machineId: string
  terminalId?: string | undefined
  sessionToken: string
  answerProofChallenge?: string | undefined
  offer: {
    sessionId: string
    sdp: string
    iceCandidates?: string[] | undefined
  }
}

export interface ManagedHubSession {
  sessionId: string
  path: 'managed'
  machineId: string
  terminalId?: string | undefined
  answer: RtcSessionDescription
  iceCandidates: string[]
  iceServers: ManagedIceServer[]
  relayPolicy: ManagedRelayPolicy
  relayInUse: boolean
  answerProof?: string | undefined
}

export interface ManagedHubPendingSession {
  sessionId: string
  path: 'managed'
  machineId: string
  terminalId?: string | undefined
  pending: true
}

export type ManagedHubCreateSessionResult = ManagedHubSession | ManagedHubPendingSession

export interface PollManagedHubSessionAnswerInput {
  sessionId: string
  machineId: string
}

export interface ManagedHubPairInput {
  machineId: string
  pairSessionId: string
  pairSecret: string
  appDeviceId: string
  appName: string
  requestedCapabilities: string[]
}

export interface ManagedHubPairResult {
  claimId: string
  machineId: string
  machineName?: string | undefined
  sessionToken: string
  expiresAt: string
}

export function createManagedHubApi(options: ManagedHubApiOptions): ManagedHubApi {
  return new ManagedHubHttpApi(options)
}

class ManagedHubHttpApi implements ManagedHubApi {
  private readonly baseUrl: string
  private readonly accessToken: string | undefined
  private readonly fetchImpl: ManagedHubFetch

  constructor(options: ManagedHubApiOptions) {
    this.baseUrl = normalizeBaseUrl(options.baseUrl)
    if (options.accessToken !== undefined) {
      const token = options.accessToken.trim()
      if (token === '') {
        throw new Error('Managed Hub access token must be non-empty when provided')
      }
      this.accessToken = token
    }
    this.fetchImpl = options.fetch
  }

  async getSessionIce(input: ManagedHubSessionIceInput, options: RtcConnectOptions = {}): Promise<ManagedHubSessionIceConfig> {
    const machineId = requiredString(input.machineId, 'machine_id')
    const terminalId = optionalTrimmedString(input.terminalId)
    const response = await this.requestJSON('POST', '/api/v1/sessions/ice', {
      signal: options.signal,
      body: {
        machine_id: machineId,
        terminal_id: terminalId,
        session_token: requiredString(input.sessionToken, 'session_token'),
      },
    })
    assertManagedPath(response)
    return {
      path: 'managed',
      machineId: stringField(response, 'machine_id'),
      ...(optionalStringField(response, 'terminal_id') ? { terminalId: optionalStringField(response, 'terminal_id') } : {}),
      iceServers: iceServersField(response, 'ice_servers'),
      relayPolicy: relayPolicy(response),
    }
  }

  async createSession(input: CreateManagedHubSessionInput, options: RtcConnectOptions = {}): Promise<ManagedHubCreateSessionResult> {
    const machineId = requiredString(input.machineId, 'machine_id')
    const terminalId = optionalTrimmedString(input.terminalId)
    const sessionToken = requiredString(input.sessionToken, 'session_token')
    const sessionId = requiredString(input.offer.sessionId, 'offer session_id')
    const sdp = requiredPayloadString(input.offer.sdp, 'offer sdp')
    const response = await this.requestJSON('POST', '/api/v1/sessions', {
      signal: options.signal,
      body: {
        machine_id: machineId,
        terminal_id: terminalId,
        session_token: sessionToken,
        ...(optionalTrimmedString(input.answerProofChallenge) ? { answer_proof_challenge: optionalTrimmedString(input.answerProofChallenge) } : {}),
        offer: {
          session_id: sessionId,
          sdp,
          ice_candidates: input.offer.iceCandidates ?? [],
        },
      },
    })
    assertManagedPath(response)
    if (optionalBooleanField(response, 'pending') === true) {
      return {
        sessionId: stringField(response, 'session_id'),
        path: 'managed',
        machineId: stringField(response, 'machine_id'),
        ...(optionalStringField(response, 'terminal_id') ? { terminalId: optionalStringField(response, 'terminal_id') } : {}),
        pending: true,
      }
    }
    return managedSessionFromResponse(response)
  }

  async pollSessionAnswer(input: PollManagedHubSessionAnswerInput, options: RtcConnectOptions = {}): Promise<ManagedHubSession> {
    const sessionId = requiredString(input.sessionId, 'session_id')
    const machineId = requiredString(input.machineId, 'machine_id')
    const response = await this.requestJSON('POST', `/api/v1/sessions/${encodeURIComponent(sessionId)}/answer`, {
      signal: options.signal,
      body: {
        machine_id: machineId,
      },
    })
    return managedSessionFromResponse(response)
  }

  async pair(input: ManagedHubPairInput, options: RtcConnectOptions = {}): Promise<ManagedHubPairResult> {
    const machineId = requiredString(input.machineId, 'machine_id')
    const response = await this.requestJSON('POST', '/api/v1/pairing/claims', {
      signal: options.signal,
      body: {
        machine_id: machineId,
        pair_session_id: requiredString(input.pairSessionId, 'pair_session_id'),
        pair_secret: requiredString(input.pairSecret, 'pair_secret'),
        app_device_id: requiredString(input.appDeviceId, 'app_device_id'),
        app_name: requiredString(input.appName, 'app_name'),
        requested_capabilities: input.requestedCapabilities,
      },
    })
    if (optionalBooleanField(response, 'pending') === true) {
      throw new Error('Managed Hub pairing is pending; make sure the TermX agent is online, connected to this Hub, and then try Pair Device again')
    }
    const sessionToken = requiredString(response.session_token, 'session_token')
    return {
      claimId: stringField(response, 'claim_id'),
      machineId: stringField(response, 'machine_id'),
      ...(optionalStringField(response, 'machine_name') ? { machineName: optionalStringField(response, 'machine_name') } : {}),
      sessionToken,
      expiresAt: stringField(response, 'expires_at'),
    }
  }

  private async requestJSON(
    method: string,
    path: string,
    options: {
      signal?: AbortSignal | undefined
      body?: unknown
    },
  ): Promise<Record<string, unknown>> {
    const headers: Record<string, string> = {}
    if (this.accessToken) {
      headers.authorization = `Bearer ${this.accessToken}`
    }
    const init: RequestInit = { method, headers }
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
      throw serviceFetchError('Managed Hub', url, err)
    }
    if (!response.ok) {
      throw new Error(await errorMessage(response))
    }
    const text = await response.text()
    if (text.trim() === '') {
      return {}
    }
    return record(JSON.parse(text), 'Managed Hub response')
  }

  private url(path: string): string {
    return `${this.baseUrl}${path.replace(/^\//, '')}`
  }
}

function managedSessionFromResponse(response: Record<string, unknown>): ManagedHubSession {
  assertManagedPath(response)
  if (optionalBooleanField(response, 'pending') === true) {
    throw new Error('Managed Hub answer response is still pending')
  }
  const answer = record(response.answer, 'Managed Hub session answer')
  return {
    sessionId: stringField(response, 'session_id'),
    path: 'managed',
    machineId: stringField(response, 'machine_id'),
    ...(optionalStringField(response, 'terminal_id') ? { terminalId: optionalStringField(response, 'terminal_id') } : {}),
    answer: {
      type: 'answer',
      sdp: stringField(answer, 'sdp'),
    },
    iceCandidates: optionalStringArrayField(answer, 'ice_candidates') ?? [],
    iceServers: iceServersField(response, 'ice_servers'),
    relayPolicy: relayPolicy(response),
    relayInUse: optionalBooleanField(response, 'relay_in_use') ?? false,
    ...(optionalStringField(answer, 'answer_proof') ? { answerProof: optionalStringField(answer, 'answer_proof') } : {}),
  }
}

function assertManagedPath(response: Record<string, unknown>): void {
  const path = stringField(response, 'path')
  if (path !== 'managed' && path !== 'cloud') {
    throw new Error(`managed Hub session path must be managed or cloud, got ${path}`)
  }
}

async function errorMessage(response: Response): Promise<string> {
  const text = await response.text()
  if (text.trim() === '') {
    return `Managed Hub request failed with HTTP ${response.status}`
  }
  try {
    const data = record(JSON.parse(text), 'Managed Hub error response')
    const err = record(data.error, 'Managed Hub error')
    const code = optionalStringField(err, 'code') ?? `http_${response.status}`
    const message = optionalStringField(err, 'message') ?? response.statusText
    return `${code}: ${message}`
  } catch {
    return `Managed Hub request failed with HTTP ${response.status}: ${text}`
  }
}

function normalizeBaseUrl(raw: string): string {
  const normalized = normalizeHubBaseUrlCandidate(raw)
  if (!normalized) {
    throw new Error('Managed Hub baseUrl is required')
  }
  return `${normalized}/`
}

function requiredString(value: unknown, label: string): string {
  const trimmed = typeof value === 'string' ? value.trim() : ''
  if (trimmed === '') {
    throw new Error(`Managed Hub ${label} is required`)
  }
  return trimmed
}

function requiredPayloadString(value: unknown, label: string): string {
  if (typeof value !== 'string' || value.trim() === '') {
    throw new Error(`Managed Hub ${label} is required`)
  }
  return value
}

function optionalTrimmedString(value: unknown): string {
  if (value === undefined || value === null) return ''
  if (typeof value !== 'string') {
    throw new Error('Managed Hub terminal_id must be a string')
  }
  return value.trim()
}

function record(value: unknown, label: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`${label} must be an object`)
  }
  return value as Record<string, unknown>
}

function stringField(value: Record<string, unknown>, key: string): string {
  const field = value[key]
  if (typeof field !== 'string' || field.trim() === '') {
    throw new Error(`Managed Hub response ${key} is required`)
  }
  return field
}

function optionalStringField(value: Record<string, unknown>, key: string): string | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'string' || field.trim() === '') {
    throw new Error(`Managed Hub response ${key} must be a string`)
  }
  return field
}

function optionalStringArrayField(value: Record<string, unknown>, key: string): string[] | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (!Array.isArray(field) || field.some((item) => typeof item !== 'string' || item.trim() === '')) {
    throw new Error(`Managed Hub response ${key} must be a string array`)
  }
  return field
}

function booleanField(value: Record<string, unknown>, key: string): boolean {
  const field = value[key]
  if (typeof field !== 'boolean') {
    throw new Error(`Managed Hub response ${key} must be a boolean`)
  }
  return field
}

function optionalBooleanField(value: Record<string, unknown>, key: string): boolean | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'boolean') {
    throw new Error(`Managed Hub response ${key} must be a boolean`)
  }
  return field
}

function relayPolicy(response: Record<string, unknown>): ManagedRelayPolicy {
  const nested = response.relay_policy === undefined || response.relay_policy === null
    ? response
    : record(response.relay_policy, 'Managed Hub relay_policy')
  return {
    allowRelay: booleanField(nested, 'allow_relay'),
    allowRelayTransfer: booleanField(nested, 'allow_relay_transfer'),
  }
}

function iceServersField(value: Record<string, unknown>, key: string): ManagedIceServer[] {
  const field = value[key]
  if (field === undefined || field === null) return []
  if (!Array.isArray(field)) {
    throw new Error(`Managed Hub response ${key} must be an array`)
  }
  return field.map((item) => {
    const server = record(item, 'Managed Hub ICE server')
    return {
      urls: requiredStringArrayField(server, 'urls'),
      ...(optionalStringField(server, 'username') ? { username: optionalStringField(server, 'username') } : {}),
      ...(optionalStringField(server, 'credential') ? { credential: optionalStringField(server, 'credential') } : {}),
    }
  })
}

function requiredStringArrayField(value: Record<string, unknown>, key: string): string[] {
  const field = optionalStringArrayField(value, key)
  if (!field) {
    throw new Error(`Managed Hub response ${key} is required`)
  }
  return field
}
