import type { LocalOfferSignature } from './localAppIdentity'
import type { ManagedHubApi, ManagedHubSession } from './managedHubApi'
import type {
  ConnectionCapabilities,
  RtcConnectionTarget,
  RtcConnectOptions,
  RtcOfferSigningInput,
  RtcSession,
  RtcSessionCapabilityUpdater,
  RtcSessionNegotiator,
} from './transport'

export interface ManagedHubRtcConnectInput extends RtcConnectionTarget {
  connectTicket: string
  appCertificate: unknown
}

export interface ManagedHubRtcConnectorOptions<TSession extends RtcSession & RtcSessionNegotiator = RtcSession & RtcSessionNegotiator> {
  api: Pick<ManagedHubApi, 'createSession' | 'pollSessionAnswer'>
  createSession(input: RtcConnectionTarget): TSession
  signOffer(input: RtcOfferSigningInput): Promise<LocalOfferSignature>
  maxAnswerPolls?: number | undefined
  answerPollDelayMs?: number | undefined
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
    try {
      throwIfAborted(options.signal)
      const offer = await session.createOffer({
        machineId: input.machineId,
        path: 'managed',
        ...(terminalId ? { terminalId } : {}),
      })
      const sdp = offer.description.sdp
      if (!sdp) throw new Error('managed Hub WebRTC offer SDP is required')
      throwIfAborted(options.signal)
      const signed = await this.options.signOffer({
        sessionId: offer.sessionId,
        ticketId: input.connectTicket,
        machineId: input.machineId,
        terminalId,
        sdp,
        candidates: [],
      })
      throwIfAborted(options.signal)
      const result = await this.options.api.createSession({
        connectTicket: input.connectTicket,
        machineId: input.machineId,
        terminalId,
        appCertificate: input.appCertificate,
        offer: {
          sessionId: offer.sessionId,
          sdp,
          iceCandidates: [],
        },
        signature: {
          algorithm: 'ed25519',
          nonce: signed.nonce,
          timestamp: Number(signed.timestamp),
          value: signed.signature,
        },
      }, options)
      const answer: ManagedHubSession = 'pending' in result && result.pending
        ? await this.pollAnswer({
          sessionId: result.sessionId,
          connectTicket: input.connectTicket,
          machineId: input.machineId,
        }, options)
        : result as ManagedHubSession
      if (answer.sessionId !== offer.sessionId) {
        throw new Error(`managed Hub RTC answer session mismatch: ${answer.sessionId} != ${offer.sessionId}`)
      }
      applySessionCapabilities(session, answer)
      await session.acceptAnswer(answer.answer)
      return session
    } catch (err) {
      await session.disconnect()
      throw err
    }
  }

  private async pollAnswer(input: {
    sessionId: string
    connectTicket: string
    machineId: string
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
        return await this.options.api.pollSessionAnswer(input, options)
      } catch (err) {
        lastError = err
        if (!isPendingAnswerError(err)) throw err
      }
    }
    throw lastError instanceof Error ? lastError : new Error('managed Hub answer did not become ready')
  }
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

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (!signal?.aborted) return
  throw signal.reason instanceof Error ? signal.reason : new Error('managed Hub RTC connection aborted')
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
