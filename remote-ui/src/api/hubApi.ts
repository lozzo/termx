import type { RemoteRuntimeFetch, RtcConnectOptions, RtcSessionDescription } from '../core/transport'
import { serviceFetchError } from './networkErrors'
import { normalizeHubBaseUrlCandidate } from './hubUrl'

export interface HubRelayPolicy {
  allowRelay: boolean
  allowRelayTransfer: boolean
}

export interface HubIceServer {
  urls: string[]
  username?: string | undefined
  credential?: string | undefined
}

export type HubFetch = RemoteRuntimeFetch
export type HubSessionPath = 'hub' | 'local'

export interface HubApiOptions {
  baseUrl: string
  accessToken?: string | undefined
  fetch: HubFetch
}

export interface HubApi {
  getSessionIce(input: HubSessionIceInput, options?: RtcConnectOptions): Promise<HubSessionIceConfig>
  createSession(input: CreateHubSessionInput, options?: RtcConnectOptions): Promise<HubCreateSessionResult>
  pollSessionAnswer(input: PollHubSessionAnswerInput, options?: RtcConnectOptions): Promise<HubSession>
  pair(input: HubPairInput, options?: RtcConnectOptions): Promise<HubPairResult>
}

export interface HubSessionIceInput {
  machineId: string
  terminalId?: string | undefined
  sessionToken: string
}

export interface HubSessionIceConfig {
  path: HubSessionPath
  machineId: string
  terminalId?: string | undefined
  iceServers: HubIceServer[]
  relayPolicy: HubRelayPolicy
}

export interface CreateHubSessionInput {
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

export interface HubSession {
  sessionId: string
  path: HubSessionPath
  machineId: string
  terminalId?: string | undefined
  answer: RtcSessionDescription
  iceCandidates: string[]
  iceServers: HubIceServer[]
  relayPolicy: HubRelayPolicy
  relayInUse: boolean
  answerProof?: string | undefined
}

export interface HubPendingSession {
  sessionId: string
  path: HubSessionPath
  machineId: string
  terminalId?: string | undefined
  pending: true
}

export type HubCreateSessionResult = HubSession | HubPendingSession

export interface PollHubSessionAnswerInput {
  sessionId: string
  machineId: string
}

export interface HubPairInput {
  machineId: string
  pairSessionId: string
  pairSecret: string
  appDeviceId: string
  appName: string
  requestedCapabilities: string[]
}

export interface HubPairResult {
  claimId: string
  machineId: string
  machineName?: string | undefined
  sessionToken: string
  expiresAt: string
}

export function createHubApi(options: HubApiOptions): HubApi {
  return new HubHttpApi(options)
}

class HubHttpApi implements HubApi {
  private readonly baseUrl: string
  private readonly accessToken: string | undefined
  private readonly fetchImpl: HubFetch

  constructor(options: HubApiOptions) {
    this.baseUrl = normalizeBaseUrl(options.baseUrl)
    if (options.accessToken !== undefined) {
      const token = options.accessToken.trim()
      if (token === '') {
        throw new Error('Hub access token must be non-empty when provided')
      }
      this.accessToken = token
    }
    this.fetchImpl = options.fetch
  }

  async getSessionIce(input: HubSessionIceInput, options: RtcConnectOptions = {}): Promise<HubSessionIceConfig> {
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
    const path = hubSessionPath(response)
    return {
      path,
      machineId: stringField(response, 'machine_id'),
      ...(optionalStringField(response, 'terminal_id') ? { terminalId: optionalStringField(response, 'terminal_id') } : {}),
      iceServers: iceServersField(response, 'ice_servers'),
      relayPolicy: relayPolicy(response),
    }
  }

  async createSession(input: CreateHubSessionInput, options: RtcConnectOptions = {}): Promise<HubCreateSessionResult> {
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
    const path = hubSessionPath(response)
    if (optionalBooleanField(response, 'pending') === true) {
      return {
        sessionId: stringField(response, 'session_id'),
        path,
        machineId: stringField(response, 'machine_id'),
        ...(optionalStringField(response, 'terminal_id') ? { terminalId: optionalStringField(response, 'terminal_id') } : {}),
        pending: true,
      }
    }
    return hubSessionFromResponse(response)
  }

  async pollSessionAnswer(input: PollHubSessionAnswerInput, options: RtcConnectOptions = {}): Promise<HubSession> {
    const sessionId = requiredString(input.sessionId, 'session_id')
    const machineId = requiredString(input.machineId, 'machine_id')
    const response = await this.requestJSON('POST', `/api/v1/sessions/${encodeURIComponent(sessionId)}/answer`, {
      signal: options.signal,
      body: {
        machine_id: machineId,
      },
    })
    return hubSessionFromResponse(response)
  }

  async pair(input: HubPairInput, options: RtcConnectOptions = {}): Promise<HubPairResult> {
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
      throw new Error('Hub pairing is pending; make sure the TermX agent is online, connected to this hub, and then try Pair Device again')
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
      throw serviceFetchError('Hub', url, err)
    }
    if (!response.ok) {
      throw new Error(await errorMessage(response))
    }
    const text = await response.text()
    if (text.trim() === '') {
      return {}
    }
    return parseJSONRecord(text, `Hub ${method} ${url}`, response.status)
  }

  private url(path: string): string {
    return `${this.baseUrl}${path.replace(/^\//, '')}`
  }
}

function parseJSONRecord(text: string, label: string, status: number): Record<string, unknown> {
  try {
    return record(JSON.parse(text), 'Hub response')
  } catch {
    throw new Error(`${label} returned invalid JSON with HTTP ${status}: ${previewBody(text)}`)
  }
}

function previewBody(text: string): string {
  const trimmed = text.trim().replace(/\s+/g, ' ')
  if (trimmed.length <= 160) return trimmed
  return `${trimmed.slice(0, 160)}...`
}

function hubSessionFromResponse(response: Record<string, unknown>): HubSession {
  const path = hubSessionPath(response)
  if (optionalBooleanField(response, 'pending') === true) {
    throw new Error('Hub answer response is still pending')
  }
  const answer = record(response.answer, 'Hub session answer')
  return {
    sessionId: stringField(response, 'session_id'),
    path,
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

function hubSessionPath(response: Record<string, unknown>): HubSessionPath {
  const path = stringField(response, 'path')
  if (path !== 'hub' && path !== 'local') {
    throw new Error(`Hub session path must be hub or local, got ${path}`)
  }
  return path
}

async function errorMessage(response: Response): Promise<string> {
  const text = await response.text()
  if (text.trim() === '') {
    return `Hub request failed with HTTP ${response.status}`
  }
  try {
    const data = record(JSON.parse(text), 'Hub error response')
    const err = record(data.error, 'Hub error')
    const code = optionalStringField(err, 'code') ?? `http_${response.status}`
    const message = optionalStringField(err, 'message') ?? response.statusText
    return `${code}: ${message}`
  } catch {
    return `Hub request failed with HTTP ${response.status}: ${text}`
  }
}

function normalizeBaseUrl(raw: string): string {
  const normalized = normalizeHubBaseUrlCandidate(raw)
  if (!normalized) {
    throw new Error('Hub baseUrl is required')
  }
  return `${normalized}/`
}

function requiredString(value: unknown, label: string): string {
  const trimmed = typeof value === 'string' ? value.trim() : ''
  if (trimmed === '') {
    throw new Error(`Hub ${label} is required`)
  }
  return trimmed
}

function requiredPayloadString(value: unknown, label: string): string {
  if (typeof value !== 'string' || value.trim() === '') {
    throw new Error(`Hub ${label} is required`)
  }
  return value
}

function optionalTrimmedString(value: unknown): string {
  if (value === undefined || value === null) return ''
  if (typeof value !== 'string') {
    throw new Error('Hub terminal_id must be a string')
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
    throw new Error(`Hub response ${key} is required`)
  }
  return field
}

function optionalStringField(value: Record<string, unknown>, key: string): string | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'string' || field.trim() === '') {
    throw new Error(`Hub response ${key} must be a string`)
  }
  return field
}

function optionalStringArrayField(value: Record<string, unknown>, key: string): string[] | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (!Array.isArray(field) || field.some((item) => typeof item !== 'string' || item.trim() === '')) {
    throw new Error(`Hub response ${key} must be a string array`)
  }
  return field
}

function booleanField(value: Record<string, unknown>, key: string): boolean {
  const field = value[key]
  if (typeof field !== 'boolean') {
    throw new Error(`Hub response ${key} must be a boolean`)
  }
  return field
}

function optionalBooleanField(value: Record<string, unknown>, key: string): boolean | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'boolean') {
    throw new Error(`Hub response ${key} must be a boolean`)
  }
  return field
}

function relayPolicy(response: Record<string, unknown>): HubRelayPolicy {
  const nested = response.relay_policy === undefined || response.relay_policy === null
    ? response
    : record(response.relay_policy, 'Hub relay_policy')
  return {
    allowRelay: booleanField(nested, 'allow_relay'),
    allowRelayTransfer: booleanField(nested, 'allow_relay_transfer'),
  }
}

function iceServersField(value: Record<string, unknown>, key: string): HubIceServer[] {
  const field = value[key]
  if (field === undefined || field === null) return []
  if (!Array.isArray(field)) {
    throw new Error(`Hub response ${key} must be an array`)
  }
  return field.map((item) => {
    const server = record(item, 'Hub ICE server')
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
    throw new Error(`Hub response ${key} is required`)
  }
  return field
}
