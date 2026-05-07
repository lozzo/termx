import type { ManagedHubApi, ManagedHubSession } from './managedHubApi'
import type {
  ConnectionCapabilities,
  ConnectionPath,
  RtcConnectionTarget,
  RtcConnectOptions,
  RtcSession,
  RtcSessionCapabilityUpdater,
  RtcSessionNegotiator,
} from './transport'
import type { ConnectionLogger } from './connectionLogger'
import { logConnectionEvent } from './connectionLogger'

export interface ManagedHubRtcConnectInput extends RtcConnectionTarget {
  sessionToken: string
  answerProofSecret?: string | undefined
  path?: ConnectionPath | undefined
}

export interface ManagedHubRtcConnectorOptions<TSession extends RtcSession & RtcSessionNegotiator = RtcSession & RtcSessionNegotiator> {
  api: Pick<ManagedHubApi, 'getSessionIce' | 'createSession' | 'pollSessionAnswer'>
  createSession(input: RtcConnectionTarget): TSession
  maxAnswerPolls?: number | undefined
  answerPollDelayMs?: number | undefined
  hubUrl?: string | undefined
  logger?: ConnectionLogger | undefined
}

export function createManagedHubRtcConnector(options: ManagedHubRtcConnectorOptions) {
  return new ManagedHubRtcConnector(options)
}

class ManagedHubRtcConnector {
  constructor(private readonly options: ManagedHubRtcConnectorOptions) {}

  async connect(input: ManagedHubRtcConnectInput, options: RtcConnectOptions = {}): Promise<RtcSession> {
    const terminalId = input.terminalId?.trim() ?? ''
    const session = this.options.createSession({
      machineId: input.machineId,
      ...(terminalId ? { terminalId } : {}),
    })
    const path = input.path ?? 'managed'
    try {
      this.log('connect_start', {
        level: 'info',
        machineId: input.machineId,
        terminalId,
        path,
        details: {
          hasAnswerProofSecret: Boolean(input.answerProofSecret),
        },
      })
      throwIfAborted(options.signal)
      const iceConfig = await this.options.api.getSessionIce({
        machineId: input.machineId,
        terminalId,
        sessionToken: input.sessionToken,
      }, options)
      this.log('ice_config_received', {
        machineId: input.machineId,
        terminalId,
        path,
        details: {
          iceServerCount: iceConfig.iceServers.length,
          allowRelay: iceConfig.relayPolicy.allowRelay,
        },
      })
      throwIfAborted(options.signal)
      const offer = await session.createOffer({
        machineId: input.machineId,
        path,
        iceServers: iceConfig.iceServers,
        ...(terminalId ? { terminalId } : {}),
      })
      this.log('offer_created', {
        machineId: input.machineId,
        terminalId,
        path,
        sessionId: offer.sessionId,
        details: {
          sdpBytes: offer.description.sdp.length,
        },
      })
      const sdp = offer.description.sdp
      if (!sdp) throw new Error('managed Hub WebRTC offer SDP is required')
      throwIfAborted(options.signal)
      const answerProofChallenge = input.answerProofSecret ? randomProofChallenge() : undefined
      this.log('hub_session_create_start', {
        machineId: input.machineId,
        terminalId,
        path,
        sessionId: offer.sessionId,
      })
      const result = await this.options.api.createSession({
        machineId: input.machineId,
        terminalId,
        sessionToken: input.sessionToken,
        ...(answerProofChallenge ? { answerProofChallenge } : {}),
        offer: {
          sessionId: offer.sessionId,
          sdp,
          iceCandidates: [],
        },
      }, options)
      this.log('hub_session_create_result', {
        machineId: input.machineId,
        terminalId,
        path,
        sessionId: offer.sessionId,
        details: {
          pending: 'pending' in result && result.pending === true,
        },
      })
      const answer: ManagedHubSession = 'pending' in result && result.pending
        ? await this.pollAnswer({
          sessionId: result.sessionId,
          machineId: input.machineId,
          terminalId,
          path,
        }, options)
        : result as ManagedHubSession
      if (answer.sessionId !== offer.sessionId) {
        throw new Error(`managed Hub RTC answer session mismatch: ${answer.sessionId} != ${offer.sessionId}`)
      }
      this.log('answer_received', {
        machineId: input.machineId,
        terminalId,
        path,
        sessionId: offer.sessionId,
        details: {
          relayInUse: answer.relayInUse,
          answerBytes: answer.answer.sdp.length,
        },
      })
      await verifyAnswerProof({
        answer,
        offerSessionId: offer.sessionId,
        pairSessionToken: input.sessionToken,
        answerProofSecret: input.answerProofSecret,
        answerProofChallenge,
      })
      this.log('answer_proof_verified', {
        machineId: input.machineId,
        terminalId,
        path,
        sessionId: offer.sessionId,
        details: {
          required: Boolean(input.answerProofSecret),
        },
      })
      applySessionCapabilities(session, answer)
      this.log('answer_accept_start', {
        machineId: input.machineId,
        terminalId,
        path,
        sessionId: offer.sessionId,
      })
      await session.acceptAnswer(answer.answer)
      this.log('connect_success', {
        level: 'info',
        machineId: input.machineId,
        terminalId,
        path,
        sessionId: offer.sessionId,
        details: {
          relayInUse: answer.relayInUse,
        },
      })
      return session
    } catch (err) {
      if (isAbortError(err, options.signal)) {
        this.log('connect_cancelled', {
          level: 'debug',
          machineId: input.machineId,
          terminalId,
          path,
          message: errorMessage(err),
        })
      } else {
        this.log('connect_failed', {
          level: 'error',
          machineId: input.machineId,
          terminalId,
          path,
          message: errorMessage(err),
        })
      }
      await session.disconnect()
      throw err
    }
  }

  private async pollAnswer(input: {
    sessionId: string
    machineId: string
    terminalId: string
    path: ConnectionPath
  }, options: RtcConnectOptions): Promise<ManagedHubSession> {
    const maxPolls = this.options.maxAnswerPolls ?? 20
    const delayMs = this.options.answerPollDelayMs ?? 500
    let lastError: unknown
    for (let attempt = 0; attempt < maxPolls; attempt += 1) {
      throwIfAborted(options.signal)
      if (attempt > 0) {
        await delay(delayMs, options.signal)
      }
      try {
        this.log('answer_poll_start', {
          machineId: input.machineId,
          terminalId: input.terminalId,
          path: input.path,
          sessionId: input.sessionId,
          details: { attempt: attempt + 1, maxPolls },
        })
        return await this.options.api.pollSessionAnswer({
          sessionId: input.sessionId,
          machineId: input.machineId,
        }, options)
      } catch (err) {
        lastError = err
        this.log('answer_poll_failed', {
          level: isPendingAnswerError(err) ? 'debug' : 'warn',
          machineId: input.machineId,
          terminalId: input.terminalId,
          path: input.path,
          sessionId: input.sessionId,
          message: errorMessage(err),
          details: { attempt: attempt + 1, maxPolls },
        })
        if (!isPendingAnswerError(err)) throw err
      }
    }
    throw lastError instanceof Error ? lastError : new Error('managed Hub answer did not become ready')
  }

  private log(event: string, input: {
    level?: 'debug' | 'info' | 'warn' | 'error'
    machineId?: string | undefined
    terminalId?: string | undefined
    path?: ConnectionPath | undefined
    sessionId?: string | undefined
    message?: string | undefined
    details?: Record<string, unknown> | undefined
  }): void {
    logConnectionEvent(this.options.logger, {
      scope: 'managed_hub',
      event,
      hubUrl: this.options.hubUrl,
      ...input,
    })
  }
}

async function verifyAnswerProof(input: {
  answer: ManagedHubSession
  offerSessionId: string
  pairSessionToken: string
  answerProofSecret?: string | undefined
  answerProofChallenge?: string | undefined
}): Promise<void> {
  if (!input.answerProofSecret || !input.answerProofChallenge) {
    if (input.answer.answerProof) {
      throw new Error('server sent answerProof but client has no answerProofSecret')
    }
    return
  }
  if (!input.answer.answerProof) {
    throw new Error('managed Hub answer proof is required')
  }
  const pairSessionId = sessionIDFromToken(input.pairSessionToken)
  const expected = await answerProofHMAC(input.answerProofSecret, pairSessionId, input.offerSessionId, input.answerProofChallenge)
  if (input.answer.answerProof !== expected) {
    throw new Error('managed Hub answer proof mismatch')
  }
}

function randomProofChallenge(): string {
  const bytes = new Uint8Array(32)
  const cryptoImpl = globalThis.crypto
  cryptoImpl?.getRandomValues?.(bytes)
  if (bytes.some((value) => value !== 0)) {
    return base64url(bytes)
  }
  return `challenge_${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}`
}

async function answerProofHMAC(secret: string, pairSessionId: string, offerSessionId: string, challenge: string): Promise<string> {
  const data = new TextEncoder().encode(`termx-answer-proof-v1:${pairSessionId}:${offerSessionId}:${challenge}`)
  const cryptoImpl = globalThis.crypto
  if (!cryptoImpl?.subtle) {
    throw new Error('WebCrypto is required to verify managed Hub answer proof')
  }
  const key = await cryptoImpl.subtle.importKey('raw', new TextEncoder().encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  const signature = await cryptoImpl.subtle.sign('HMAC', key, data)
  return base64url(new Uint8Array(signature))
}

function sessionIDFromToken(token: string): string {
  const [payload] = token.split('.', 1)
  if (!payload) {
    throw new Error('session token payload is required for answer proof verification')
  }
  const decoded = JSON.parse(decodeBase64url(payload)) as { sid?: unknown }
  if (typeof decoded.sid !== 'string' || decoded.sid.trim() === '') {
    throw new Error('session token sid is required for answer proof verification')
  }
  return decoded.sid.trim()
}

function decodeBase64url(value: string): string {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = normalized.padEnd(normalized.length + ((4 - (normalized.length % 4)) % 4), '=')
  const binary = atob(padded)
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}

function base64url(bytes: Uint8Array): string {
  let binary = ''
  for (const value of bytes) {
    binary += String.fromCharCode(value)
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function applySessionCapabilities(session: RtcSession, answer: ManagedHubSession): void {
  const updater = session as RtcSession & Partial<RtcSessionCapabilityUpdater>
  if (typeof updater.updateConnectionCapabilities !== 'function') return
  updater.updateConnectionCapabilities(capabilitiesFromManagedSession(answer))
}

function capabilitiesFromManagedSession(answer: ManagedHubSession): ConnectionCapabilities {
  const relayInUse = answer.relayInUse === true
  return {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: relayInUse ? answer.relayPolicy.allowRelayTransfer : true,
    terminalManagementAllowed: true,
    relayInUse,
    ...relayInUse && answer.relayPolicy.allowRelayTransfer === false
      ? { denialReason: 'managed relay policy blocks file transfer' }
      : {},
  }
}

function isPendingAnswerError(err: unknown): boolean {
  return err instanceof Error && /pending|gateway timeout|http 504|deadline/i.test(err.message)
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (!signal?.aborted) return
  throw signal.reason instanceof Error ? signal.reason : new Error('managed Hub RTC connection aborted')
}

function isAbortError(err: unknown, signal: AbortSignal | undefined): boolean {
  return signal?.aborted === true && (err === signal.reason || errorMessage(err) === errorMessage(signal.reason))
}

function delay(ms: number, signal: AbortSignal | undefined): Promise<void> {
  if (!signal) return new Promise((resolve) => setTimeout(resolve, ms))
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      cleanup()
      resolve()
    }, ms)
    const onAbort = () => {
      cleanup()
      reject(signal.reason instanceof Error ? signal.reason : new Error('managed Hub RTC connection aborted'))
    }
    const cleanup = () => {
      clearTimeout(timer)
      signal.removeEventListener('abort', onAbort)
    }
    signal.addEventListener('abort', onAbort, { once: true })
  })
}
