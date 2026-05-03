import type { LocalOfferSignature } from './localAppIdentity'
import type {
  LocalAgentApi,
  LocalRTCAnswer,
  RtcOfferSigningInput,
  RtcConnector,
  RtcConnectionTarget,
  RtcConnectOptions,
  RtcSessionCapabilityUpdater,
  RtcSession,
  RtcSessionDescription,
  RtcSessionNegotiator,
} from './transport'

export interface LocalRtcConnectorOptions<TSession extends RtcSession & RtcSessionNegotiator = RtcSession & RtcSessionNegotiator> {
  api: Pick<LocalAgentApi, 'createRTCAnswer'>
  getAppCertificate(): string | null | undefined
  createSession(input: RtcConnectionTarget): TSession
  signOffer(input: RtcOfferSigningInput): Promise<LocalOfferSignature>
}

export function createLocalRtcConnector(
  options: LocalRtcConnectorOptions,
): RtcConnector<{ machineId: string; terminalId?: string | undefined }> {
  return new LocalRtcConnector(options)
}

class LocalRtcConnector implements RtcConnector<{ machineId: string; terminalId?: string | undefined }> {
  constructor(private readonly options: LocalRtcConnectorOptions) {}

  async connect(input: { machineId: string; terminalId?: string | undefined }, options: RtcConnectOptions = {}): Promise<RtcSession> {
    const appCertificate = this.options.getAppCertificate()
    if (!appCertificate) {
      throw new Error('local app certificate is required before opening a terminal')
    }
    const terminalId = input.terminalId ?? ''
    const session = this.options.createSession(input)
    try {
      throwIfAborted(options.signal)
      const offer = await session.createOffer({
        machineId: input.machineId,
        path: 'local',
        ...(input.terminalId ? { terminalId: input.terminalId } : {}),
      })
      throwIfAborted(options.signal)
      const sdp = offer.description.sdp
      if (!sdp) throw new Error('local WebRTC offer SDP is required')
      const signed = await this.options.signOffer({
        sessionId: offer.sessionId,
        machineId: input.machineId,
        terminalId,
        sdp,
        candidates: [],
      })
      throwIfAborted(options.signal)
      const answer = await this.options.api.createRTCAnswer({
        sessionId: offer.sessionId,
        machineId: input.machineId,
        terminalId,
        sdp,
        iceCandidates: [],
        appCertificate,
        appSignature: signed.signature,
        nonce: signed.nonce,
        timestamp: signed.timestamp,
      }, options)
      throwIfAborted(options.signal)
      if (answer.sessionId !== offer.sessionId) {
        throw new Error(`local RTC answer session mismatch: ${answer.sessionId} != ${offer.sessionId}`)
      }
      applyAnswerCapabilities(session, answer)
      await session.acceptAnswer(toSessionDescription(answer))
      return session
    } catch (err) {
      await session.disconnect()
      throw err
    }
  }
}

function applyAnswerCapabilities(session: RtcSession, answer: LocalRTCAnswer): void {
  if (!answer.capabilities && !answer.dataChannels) return
  const updater = session as RtcSession & Partial<RtcSessionCapabilityUpdater>
  if (typeof updater.updateConnectionCapabilities !== 'function') return
  updater.updateConnectionCapabilities(answer.capabilities ?? capabilitiesFromDataChannels(answer.dataChannels ?? []))
}

function capabilitiesFromDataChannels(dataChannels: string[]) {
  const hasAPI = dataChannels.includes('api')
  const hasTerminal = dataChannels.some((label) => label === 'terminal:{terminal_id}' || label.startsWith('terminal:'))
  const hasEvents = dataChannels.includes('events')
  const hasFile = dataChannels.some((label) => label === 'file:{transfer_id}' || label.startsWith('file:'))
  return {
    terminalAllowed: hasTerminal,
    apiAllowed: hasAPI,
    eventsAllowed: hasEvents,
    fileTransferAllowed: hasFile,
    terminalManagementAllowed: false,
    relayInUse: false,
  }
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (!signal?.aborted) return
  throw signal.reason instanceof Error ? signal.reason : new Error('local RTC connection aborted')
}

function toSessionDescription(answer: LocalRTCAnswer): RtcSessionDescription {
  return {
    type: answer.answer.type,
    sdp: answer.answer.sdp,
  }
}
