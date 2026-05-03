import type {
  RtcConnectionTarget,
  RtcConnector,
  RtcConnectOptions,
  RtcOfferSigningInput,
  RtcSession,
  RtcSessionDescription,
  RtcSessionNegotiator,
} from './transport'

export interface PublicP2pConnectInput extends RtcConnectionTarget {
  terminalId: string
  machinePublicKeyFingerprint: string
  ttlSeconds?: number | undefined
}

export interface PublicP2pRendezvousChannel {
  channelId: string
  channelSecret: string
  publicStunServers: string[]
}

export interface PublicP2pRendezvousMessage {
  type: 'offer' | 'answer' | 'candidate' | string
  from?: string | undefined
  appPublicKey?: string | undefined
  payload?: unknown
}

export interface PublicP2pRendezvousAdapter {
  createChannel(input: {
    machineId: string
    terminalId: string
    machinePublicKeyFingerprint: string
    ttlSeconds: number
  }, options?: RtcConnectOptions): Promise<PublicP2pRendezvousChannel>
  postOffer(input: {
    channelId: string
    channelSecret: string
    from: string
    appPublicKey: string
    appCertificate: unknown
    offer: unknown
    signature: unknown
  }, options?: RtcConnectOptions): Promise<void>
  pollEvents(input: {
    channelId: string
    channelSecret: string
  }, options?: RtcConnectOptions): Promise<PublicP2pRendezvousMessage[]>
  closeChannel?(input: {
    channelId: string
    channelSecret: string
  }, options?: RtcConnectOptions): Promise<void>
}

export interface PublicP2pRtcConnectorOptions<TSession extends RtcSession & RtcSessionNegotiator = RtcSession & RtcSessionNegotiator> {
  rendezvous: PublicP2pRendezvousAdapter
  createSession(input: RtcConnectionTarget): TSession
  appIdentity: {
    from: string
    appPublicKey: string
    appCertificate: unknown
  }
  signOffer(input: {
    sessionId: RtcOfferSigningInput['sessionId']
    machineId: RtcOfferSigningInput['machineId']
    terminalId?: RtcOfferSigningInput['terminalId'] | undefined
    sdp: RtcOfferSigningInput['sdp']
    candidates?: RtcOfferSigningInput['candidates']
    channelId: string
  }, options?: RtcConnectOptions): Promise<unknown>
  verifyAnswer?(input: {
    from?: string | undefined
    sessionId: string
    sdp: string
    signature?: unknown
    machineId: string
    machinePublicKeyFingerprint: string
  }, options?: RtcConnectOptions): Promise<boolean>
  maxAnswerPolls?: number | undefined
}

export function createPublicP2pRtcConnector(
  options: PublicP2pRtcConnectorOptions,
): RtcConnector<PublicP2pConnectInput> {
  return new PublicP2pRtcConnector(options)
}

class PublicP2pRtcConnector implements RtcConnector<PublicP2pConnectInput> {
  constructor(private readonly options: PublicP2pRtcConnectorOptions) {}

  async connect(input: PublicP2pConnectInput, options: RtcConnectOptions = {}): Promise<RtcSession> {
    let channel: PublicP2pRendezvousChannel | null = null
    let session: (RtcSession & RtcSessionNegotiator) | null = null
    try {
      throwIfAborted(options.signal)
      const terminalId = requiredTerminalId(input.terminalId)
      channel = await this.options.rendezvous.createChannel({
        machineId: input.machineId,
        terminalId,
        machinePublicKeyFingerprint: input.machinePublicKeyFingerprint,
        ttlSeconds: input.ttlSeconds ?? 600,
      }, options)
      throwIfAborted(options.signal)
      const iceServers = publicP2pIceServers(channel.publicStunServers)
      session = this.options.createSession({
        machineId: input.machineId,
        terminalId,
      })
      const offer = await session.createOffer({
        machineId: input.machineId,
        terminalId,
        path: 'public_p2p',
        iceServers,
      }, options)
      throwIfAborted(options.signal)
      const signature = await this.options.signOffer({
        sessionId: offer.sessionId,
        machineId: input.machineId,
        terminalId,
        sdp: offer.description.sdp,
        candidates: [],
        channelId: channel.channelId,
      }, options)
      throwIfAborted(options.signal)
      await this.options.rendezvous.postOffer({
        channelId: channel.channelId,
        channelSecret: channel.channelSecret,
        from: this.options.appIdentity.from,
        appPublicKey: this.options.appIdentity.appPublicKey,
        appCertificate: this.options.appIdentity.appCertificate,
        offer: {
          session_id: offer.sessionId,
          sdp: offer.description.sdp,
          ice_candidates: [],
        },
        signature: webControlSignatureEnvelope(signature),
      }, options)
      throwIfAborted(options.signal)
      const answer = await this.pollForAnswer({
        channel,
        offerSessionId: offer.sessionId,
        input,
        options,
      })
      throwIfAborted(options.signal)
      await session.acceptAnswer(answer.description, options)
      return session
    } catch (err) {
      if (session) await session.disconnect()
      if (channel) {
        await this.options.rendezvous.closeChannel?.({
          channelId: channel.channelId,
          channelSecret: channel.channelSecret,
        }, options).catch(() => {})
      }
      throw err
    }
  }

  private async pollForAnswer(input: {
    channel: PublicP2pRendezvousChannel
    offerSessionId: string
    input: PublicP2pConnectInput
    options: RtcConnectOptions
  }): Promise<{
    sessionId: string
    description: RtcSessionDescription
  }> {
    const maxPolls = this.options.maxAnswerPolls ?? 10
    let sawStaleAnswer = false
    for (let index = 0; index < maxPolls; index += 1) {
      throwIfAborted(input.options.signal)
      const messages = await this.options.rendezvous.pollEvents({
        channelId: input.channel.channelId,
        channelSecret: input.channel.channelSecret,
      }, input.options)
      for (const answer of answersFromMessages(messages)) {
        if (answer.sessionId !== input.offerSessionId) {
          sawStaleAnswer = true
          continue
        }
        const trusted = this.options.verifyAnswer
          ? await this.options.verifyAnswer({
            from: answer.from,
            sessionId: answer.sessionId,
            sdp: answer.description.sdp,
            signature: answer.signature,
            machineId: input.input.machineId,
            machinePublicKeyFingerprint: input.input.machinePublicKeyFingerprint,
          }, input.options)
          : true
        if (trusted) return answer
      }
    }
    if (sawStaleAnswer) {
      throw new Error(`public P2P answer session mismatch: no answer matched ${input.offerSessionId}`)
    }
    throw new Error('public P2P rendezvous answer is required')
  }
}

function answersFromMessages(messages: PublicP2pRendezvousMessage[]): Array<{
  from?: string | undefined
  sessionId: string
  description: RtcSessionDescription
  signature?: unknown
}> {
  return messages
    .filter((candidate) => candidate.type === 'answer')
    .map((message) => answerFromMessage(message))
}

function publicP2pIceServers(urls: string[]): Array<{ urls: string[] }> {
  if (!Array.isArray(urls) || urls.length === 0) {
    throw new Error('public_p2p rendezvous must return at least one STUN server')
  }
  return urls.map((url) => {
    if (typeof url !== 'string' || url.trim() === '') {
      throw new Error('public_p2p rendezvous STUN server must be a non-empty string')
    }
    const normalized = url.trim()
    if (!isPublicP2pStunURL(normalized)) {
      throw new Error(`public_p2p rendezvous may only return STUN servers, got ${normalized}`)
    }
    return { urls: [normalized] }
  })
}

function isPublicP2pStunURL(url: string): boolean {
  const lower = url.toLowerCase()
  return lower.startsWith('stun:') || lower.startsWith('stuns:')
}

function answerFromMessage(message: PublicP2pRendezvousMessage): {
  from?: string | undefined
  sessionId: string
  description: RtcSessionDescription
  signature?: unknown
} {
  const payload = record(message.payload)
  const answer = record(payload.answer ?? payload)
  const sessionId = stringField(answer, 'session_id')
  const sdp = stringField(answer, 'sdp')
  return {
    from: message.from,
    sessionId,
    description: { type: 'answer', sdp },
    signature: payload.signature,
  }
}

function record(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('public P2P rendezvous payload must be an object')
  }
  return value as Record<string, unknown>
}

function stringField(value: Record<string, unknown>, key: string): string {
  const field = value[key]
  if (typeof field !== 'string' || field.trim() === '') {
    throw new Error(`public P2P rendezvous answer ${key} is required`)
  }
  return field
}

function webControlSignatureEnvelope(value: unknown): {
  algorithm: string
  nonce: string
  timestamp: number
  value: string
} {
  const signature = record(value)
  const nonce = signatureStringField(signature, 'nonce')
  const signatureValue = optionalSignatureStringField(signature, 'value')
    ?? optionalSignatureStringField(signature, 'signature')
  if (!signatureValue) {
    throw new Error('public P2P offer signature value is required')
  }
  return {
    algorithm: optionalSignatureStringField(signature, 'algorithm') ?? 'ed25519',
    nonce,
    timestamp: signatureTimestamp(signature.timestamp),
    value: signatureValue,
  }
}

function signatureStringField(value: Record<string, unknown>, key: string): string {
  const field = optionalSignatureStringField(value, key)
  if (!field) {
    throw new Error(`public P2P offer signature ${key} is required`)
  }
  return field
}

function optionalSignatureStringField(value: Record<string, unknown>, key: string): string | undefined {
  const field = value[key]
  if (field === undefined) return undefined
  if (typeof field !== 'string' || field.trim() === '') {
    throw new Error(`public P2P offer signature ${key} must be a non-empty string`)
  }
  return field
}

function signatureTimestamp(value: unknown): number {
  const timestamp = typeof value === 'number'
    ? value
    : typeof value === 'string'
      ? Number(value)
      : Number.NaN
  if (!Number.isInteger(timestamp) || !Number.isFinite(timestamp)) {
    throw new Error('public P2P offer signature timestamp must be an integer')
  }
  return timestamp
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (!signal?.aborted) return
  throw signal.reason instanceof Error ? signal.reason : new Error('public P2P connection aborted')
}

function requiredTerminalId(value: unknown): string {
  const trimmed = typeof value === 'string' ? value.trim() : ''
  if (trimmed === '') {
    throw new Error('public_p2p terminalId is required')
  }
  return trimmed
}
