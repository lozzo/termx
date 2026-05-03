import type {
  PublicP2pRendezvousAdapter,
  PublicP2pRendezvousChannel,
  PublicP2pRendezvousMessage,
} from './publicP2pRtcConnector'
import type { RtcConnectOptions } from './transport'

export type WebControlFetch = (input: string, init?: RequestInit) => Promise<Response>

export interface WebControlApiOptions {
  baseUrl: string
  accessToken: string
  fetch?: WebControlFetch | undefined
}

export interface WebControlApi extends PublicP2pRendezvousAdapter {
  createManagedConnectTicket(input: CreateManagedConnectTicketInput, options?: RtcConnectOptions): Promise<ManagedConnectTicket>
  postCandidate(input: PublicP2pCandidateInput, options?: RtcConnectOptions): Promise<void>
}

export interface CreateManagedConnectTicketInput {
  machineId: string
  terminalId?: string | undefined
  ttlSeconds?: number | undefined
}

export interface ManagedConnectTicket {
  id: string
  path: 'managed'
  machineId: string
  terminalId?: string | undefined
  allowRelay: boolean
  relayInUse: boolean
  relayBytesRemaining?: number | undefined
  relayThrottled?: boolean | undefined
}

export interface PublicP2pCandidateInput {
  channelId: string
  channelSecret: string
  appPublicKey: string
  candidate: unknown
}

export function createWebControlApi(options: WebControlApiOptions): WebControlApi {
  return new WebControlHttpApi(options)
}

class WebControlHttpApi implements WebControlApi {
  private readonly baseUrl: string
  private readonly accessToken: string
  private readonly fetchImpl: WebControlFetch

  constructor(options: WebControlApiOptions) {
    this.baseUrl = normalizeBaseUrl(options.baseUrl)
    this.accessToken = options.accessToken.trim()
    if (this.accessToken === '') {
      throw new Error('Web Control access token is required')
    }
    this.fetchImpl = options.fetch ?? fetch
  }

  async createChannel(input: {
    machineId: string
    terminalId: string
    machinePublicKeyFingerprint: string
    ttlSeconds: number
  }, options: RtcConnectOptions = {}): Promise<PublicP2pRendezvousChannel> {
    const terminalId = requiredTerminalId(input.terminalId)
    const response = await this.requestJSON('POST', '/api/v1/public-p2p/channels', {
      auth: 'bearer',
      signal: options.signal,
      body: {
        machine_id: input.machineId,
        terminal_id: terminalId,
        machine_public_key_fingerprint: input.machinePublicKeyFingerprint,
        ttl_seconds: input.ttlSeconds,
      },
    })
    const channelId = stringField(response, 'channel_id', 'id')
    const channelSecret = stringField(response, 'channel_secret', 'secret')
    const publicStunServers = stringArrayField(response, 'public_stun_servers')
    if (publicStunServers.some((url) => /^turns?:/i.test(url.trim()))) {
      throw new Error('TURN credentials are not allowed in public_p2p rendezvous response')
    }
    return {
      channelId,
      channelSecret,
      publicStunServers,
    }
  }

  async postOffer(input: {
    channelId: string
    channelSecret: string
    from: string
    appPublicKey: string
    appCertificate: unknown
    offer: unknown
    signature: unknown
  }, options: RtcConnectOptions = {}): Promise<void> {
    await this.requestJSON('POST', `/api/v1/public-p2p/channels/${encodeURIComponent(input.channelId)}/offer`, {
      signal: options.signal,
      body: {
        channel_secret: input.channelSecret,
        app_certificate: input.appCertificate,
        offer: input.offer,
        signature: input.signature,
      },
    })
  }

  async pollEvents(input: {
    channelId: string
    channelSecret: string
  }, options: RtcConnectOptions = {}): Promise<PublicP2pRendezvousMessage[]> {
    const response = await this.requestJSON('GET', `/api/v1/public-p2p/channels/${encodeURIComponent(input.channelId)}/events`, {
      rendezvousAuth: input,
      signal: options.signal,
    })
    const messages = response.messages
    if (!Array.isArray(messages)) {
      throw new Error('Web Control rendezvous events response messages is required')
    }
    return messages.map((message) => rendezvousMessage(message))
  }

  async postCandidate(input: PublicP2pCandidateInput, options: RtcConnectOptions = {}): Promise<void> {
    await this.requestJSON('POST', `/api/v1/public-p2p/channels/${encodeURIComponent(input.channelId)}/candidate`, {
      signal: options.signal,
      body: {
        channel_secret: input.channelSecret,
        app_public_key: input.appPublicKey,
        candidate: input.candidate,
      },
    })
  }

  async createManagedConnectTicket(
    input: CreateManagedConnectTicketInput,
    options: RtcConnectOptions = {},
  ): Promise<ManagedConnectTicket> {
    const response = await this.requestJSON('POST', '/api/v1/managed/connect-tickets', {
      auth: 'bearer',
      signal: options.signal,
      body: {
        machine_id: input.machineId,
        ...(input.terminalId ? { terminal_id: input.terminalId } : {}),
        ...(input.ttlSeconds !== undefined ? { ttl_seconds: input.ttlSeconds } : {}),
      },
    })
    const path = stringField(response, 'path')
    if (path !== 'managed') {
      throw new Error(`managed connect ticket path must be managed, got ${path}`)
    }
    return {
      id: stringField(response, 'id'),
      path: 'managed',
      machineId: stringField(response, 'machine_id'),
      ...(optionalStringField(response, 'terminal_id') ? { terminalId: optionalStringField(response, 'terminal_id') } : {}),
      allowRelay: booleanField(response, 'allow_relay'),
      relayInUse: booleanField(response, 'relay_in_use'),
      ...(optionalNumberField(response, 'relay_bytes_remaining') !== undefined
        ? { relayBytesRemaining: optionalNumberField(response, 'relay_bytes_remaining') }
        : {}),
      ...(optionalBooleanField(response, 'relay_throttled') !== undefined
        ? { relayThrottled: optionalBooleanField(response, 'relay_throttled') }
        : {}),
    }
  }

  private async requestJSON(
    method: string,
    path: string,
    options: {
      auth?: 'bearer' | undefined
      rendezvousAuth?: { channelId: string; channelSecret: string } | undefined
      signal?: AbortSignal | undefined
      body?: unknown
    } = {},
  ): Promise<Record<string, unknown>> {
    const headers: Record<string, string> = {}
    if (options.auth === 'bearer') {
      headers.authorization = `Bearer ${this.accessToken}`
    }
    if (options.rendezvousAuth) {
      headers.authorization = `Rendezvous ${options.rendezvousAuth.channelId}:${options.rendezvousAuth.channelSecret}`
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

function requiredTerminalId(value: unknown): string {
  const trimmed = typeof value === 'string' ? value.trim() : ''
  if (trimmed === '') {
    throw new Error('public_p2p terminal_id is required')
  }
  return trimmed
}

function rendezvousMessage(value: unknown): PublicP2pRendezvousMessage {
  const data = record(value, 'Web Control rendezvous message')
  return {
    type: stringField(data, 'type'),
    ...(optionalStringField(data, 'from') ? { from: optionalStringField(data, 'from') } : {}),
    ...(data.payload !== undefined ? { payload: data.payload } : {}),
  }
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

function stringArrayField(value: Record<string, unknown>, key: string): string[] {
  const field = value[key]
  if (!Array.isArray(field) || field.some((item) => typeof item !== 'string' || item.trim() === '')) {
    throw new Error(`Web Control response ${key} must be a string array`)
  }
  return field
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

function optionalNumberField(value: Record<string, unknown>, key: string): number | undefined {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  if (typeof field !== 'number' || !Number.isFinite(field)) {
    throw new Error(`Web Control response ${key} must be a number`)
  }
  return field
}
