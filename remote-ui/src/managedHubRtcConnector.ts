import type { ManagedHubApi, ManagedHubSession } from './managedHubApi'
import type {
  ConnectionCapabilities,
  ConnectionPath,
  RtcConnectionPhase,
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
      emitConnectionState(options, input, 'probing', 'Fetching ICE servers...', path)
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
      emitConnectionState(options, input, 'probing', 'Creating WebRTC offer...', path)
      const offer = await session.createOffer({
        machineId: input.machineId,
        path,
        iceServers: iceConfig.iceServers,
        ...(terminalId ? { terminalId } : {}),
      }, options)
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
      emitConnectionState(options, input, 'connecting', 'Exchanging signals with hub...', path)
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
      emitConnectionState(options, input, 'connecting', 'Opening data channels...', path, answer.relayInUse)
      await session.acceptAnswer(answer.answer)
      emitConnectionState(options, input, 'connected', 'Connected', path, answer.relayInUse)
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
        emitConnectionState(options, input, 'failed', errorMessage(err), path)
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
        emitConnectionState(options, input, 'connecting', `Waiting for machine response (${attempt + 1}/${maxPolls})...`, input.path)
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
  if (cryptoImpl?.subtle) {
    const key = await cryptoImpl.subtle.importKey('raw', new TextEncoder().encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
    const signature = await cryptoImpl.subtle.sign('HMAC', key, data)
    return base64url(new Uint8Array(signature))
  }
  return base64url(hmacSHA256(new TextEncoder().encode(secret), data))
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

function hmacSHA256(key: Uint8Array, data: Uint8Array): Uint8Array {
  const blockSize = 64
  let normalizedKey = key
  if (normalizedKey.length > blockSize) {
    normalizedKey = sha256(normalizedKey)
  }
  const keyBlock = new Uint8Array(blockSize)
  keyBlock.set(normalizedKey)
  const outer = new Uint8Array(blockSize)
  const inner = new Uint8Array(blockSize)
  for (let index = 0; index < blockSize; index += 1) {
    const value = keyBlock[index] ?? 0
    outer[index] = value ^ 0x5c
    inner[index] = value ^ 0x36
  }
  return sha256(concatBytes(outer, sha256(concatBytes(inner, data))))
}

function sha256(input: Uint8Array): Uint8Array {
  const bytes = new Uint8Array(input.length + 1 + 8 + ((64 - ((input.length + 1 + 8) % 64)) % 64))
  bytes.set(input)
  bytes[input.length] = 0x80
  const bitLength = input.length * 8
  const view = new DataView(bytes.buffer)
  view.setUint32(bytes.length - 8, Math.floor(bitLength / 0x100000000), false)
  view.setUint32(bytes.length - 4, bitLength >>> 0, false)

  let h0 = 0x6a09e667
  let h1 = 0xbb67ae85
  let h2 = 0x3c6ef372
  let h3 = 0xa54ff53a
  let h4 = 0x510e527f
  let h5 = 0x9b05688c
  let h6 = 0x1f83d9ab
  let h7 = 0x5be0cd19
  const words = new Uint32Array(64)

  for (let offset = 0; offset < bytes.length; offset += 64) {
    for (let index = 0; index < 16; index += 1) {
      words[index] = view.getUint32(offset + index * 4, false)
    }
    for (let index = 16; index < 64; index += 1) {
      words[index] = add32(
        smallSigma1(words[index - 2] ?? 0),
        words[index - 7] ?? 0,
        smallSigma0(words[index - 15] ?? 0),
        words[index - 16] ?? 0,
      )
    }

    let a = h0
    let b = h1
    let c = h2
    let d = h3
    let e = h4
    let f = h5
    let g = h6
    let h = h7

    for (let index = 0; index < 64; index += 1) {
      const t1 = add32(h, bigSigma1(e), choose(e, f, g), sha256Constants[index] ?? 0, words[index] ?? 0)
      const t2 = add32(bigSigma0(a), majority(a, b, c))
      h = g
      g = f
      f = e
      e = add32(d, t1)
      d = c
      c = b
      b = a
      a = add32(t1, t2)
    }

    h0 = add32(h0, a)
    h1 = add32(h1, b)
    h2 = add32(h2, c)
    h3 = add32(h3, d)
    h4 = add32(h4, e)
    h5 = add32(h5, f)
    h6 = add32(h6, g)
    h7 = add32(h7, h)
  }

  const out = new Uint8Array(32)
  const outView = new DataView(out.buffer)
  ;[h0, h1, h2, h3, h4, h5, h6, h7].forEach((value, index) => outView.setUint32(index * 4, value, false))
  return out
}

function concatBytes(a: Uint8Array, b: Uint8Array): Uint8Array {
  const out = new Uint8Array(a.length + b.length)
  out.set(a)
  out.set(b, a.length)
  return out
}

function rotateRight(value: number, bits: number): number {
  return (value >>> bits) | (value << (32 - bits))
}

function add32(...values: number[]): number {
  return values.reduce((sum, value) => (sum + value) >>> 0, 0)
}

function choose(x: number, y: number, z: number): number {
  return (x & y) ^ (~x & z)
}

function majority(x: number, y: number, z: number): number {
  return (x & y) ^ (x & z) ^ (y & z)
}

function bigSigma0(x: number): number {
  return rotateRight(x, 2) ^ rotateRight(x, 13) ^ rotateRight(x, 22)
}

function bigSigma1(x: number): number {
  return rotateRight(x, 6) ^ rotateRight(x, 11) ^ rotateRight(x, 25)
}

function smallSigma0(x: number): number {
  return rotateRight(x, 7) ^ rotateRight(x, 18) ^ (x >>> 3)
}

function smallSigma1(x: number): number {
  return rotateRight(x, 17) ^ rotateRight(x, 19) ^ (x >>> 10)
}

const sha256Constants = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])

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
    fileTransferAllowed: true,
    terminalManagementAllowed: true,
    relayInUse,
  }
}

function emitConnectionState(
  options: RtcConnectOptions,
  input: RtcConnectionTarget,
  phase: RtcConnectionPhase,
  statusText: string,
  path: ConnectionPath | undefined,
  relayInUse = false,
): void {
  options.onStatus?.(statusText)
  options.onConnectionState?.({
    machineId: input.machineId,
    phase,
    ...(path ? { path } : {}),
    statusText,
    relayInUse,
    ...(phase === 'failed' ? { failReason: statusText } : {}),
  })
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
