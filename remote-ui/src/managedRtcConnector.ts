import type {
  RtcConnectionTarget,
  RtcConnector,
  RtcConnectOptions,
  RtcSessionAnswerer,
  RtcSessionAnswerTarget,
  RtcSession,
  RtcSessionDescription,
} from './transport'

export interface ManagedRtcConnectInput extends RtcConnectionTarget {
  hubSessionId: string
  deviceId: string
}

export interface ManagedRelayPolicy {
  allowRelay: boolean
  allowRelayTransfer: boolean
}

export interface ManagedIceServer {
  urls: string[]
  username?: string | undefined
  credential?: string | undefined
}

export interface ManagedSignalingOffer {
  sessionId: string
  machineId: string
  terminalId?: string | undefined
  sdp: string
  iceServers: ManagedIceServer[]
  relayPolicy: ManagedRelayPolicy
  relayInUse?: boolean | undefined
}

export interface ManagedSignalingAnswer {
  sessionId: string
  sdp: string
}

export interface ManagedSignalingAdapter {
  pollOffer(input: {
    hubSessionId: string
    deviceId: string
  }, options?: RtcConnectOptions): Promise<ManagedSignalingOffer>
  submitAnswer(input: {
    hubSessionId: string
    deviceId: string
    answer: ManagedSignalingAnswer
  }, options?: RtcConnectOptions): Promise<void>
  rejectOffer?(input: {
    hubSessionId: string
    deviceId: string
    sessionId: string
    reason: string
  }, options?: RtcConnectOptions): Promise<void>
}

export interface ManagedRtcAnswerTarget extends RtcSessionAnswerTarget {
  path: 'managed'
  relayPolicy: ManagedRelayPolicy
  relayInUse: boolean
}

export interface ManagedRtcAnswerer extends RtcSessionAnswerer {
  acceptOffer(input: ManagedRtcAnswerTarget, options?: RtcConnectOptions): Promise<{
    sessionId: string
    description: RtcSessionDescription
  }>
}

export interface ManagedRtcConnectorOptions<TSession extends RtcSession & ManagedRtcAnswerer = RtcSession & ManagedRtcAnswerer> {
  signaling: ManagedSignalingAdapter
  createSession(input: RtcConnectionTarget): TSession
}

export function createManagedRtcConnector(
  options: ManagedRtcConnectorOptions,
): RtcConnector<ManagedRtcConnectInput> {
  return new ManagedRtcConnector(options)
}

class ManagedRtcConnector implements RtcConnector<ManagedRtcConnectInput> {
  constructor(private readonly options: ManagedRtcConnectorOptions) {}

  async connect(input: ManagedRtcConnectInput, options: RtcConnectOptions = {}): Promise<RtcSession> {
    const session = this.options.createSession(input)
    let offer: ManagedSignalingOffer | null = null
    try {
      throwIfAborted(options.signal)
      offer = await this.options.signaling.pollOffer({
        hubSessionId: input.hubSessionId,
        deviceId: input.deviceId,
      }, options)
      throwIfAborted(options.signal)
      assertManagedOfferTarget(offer, input)
      assertManagedRelayPolicy(offer)
      const answer = await session.acceptOffer({
        sessionId: offer.sessionId,
        machineId: input.machineId,
        path: 'managed',
        description: { type: 'offer', sdp: offer.sdp },
        iceServers: offer.iceServers,
        relayPolicy: offer.relayPolicy,
        relayInUse: offer.relayInUse === true,
        ...(input.terminalId ?? offer.terminalId ? { terminalId: input.terminalId ?? offer.terminalId } : {}),
      }, options)
      throwIfAborted(options.signal)
      if (answer.sessionId !== offer.sessionId) {
        throw new Error(`managed RTC answer session mismatch: ${answer.sessionId} != ${offer.sessionId}`)
      }
      await this.options.signaling.submitAnswer({
        hubSessionId: input.hubSessionId,
        deviceId: input.deviceId,
        answer: {
          sessionId: answer.sessionId,
          sdp: answer.description.sdp,
        },
      }, options)
      return session
    } catch (err) {
      await session.disconnect()
      if (offer) {
        await this.options.signaling.rejectOffer?.({
          hubSessionId: input.hubSessionId,
          deviceId: input.deviceId,
          sessionId: offer.sessionId,
          reason: err instanceof Error ? err.message : String(err),
        }, options).catch(() => {})
      }
      throw err
    }
  }
}

function assertManagedOfferTarget(offer: ManagedSignalingOffer, input: ManagedRtcConnectInput): void {
  if (offer.machineId !== input.machineId) {
    throw new Error(`managed offer machine mismatch: ${offer.machineId} != ${input.machineId}`)
  }
  if (input.terminalId && offer.terminalId && offer.terminalId !== input.terminalId) {
    throw new Error(`managed offer terminal mismatch: ${offer.terminalId} != ${input.terminalId}`)
  }
}

function assertManagedRelayPolicy(offer: ManagedSignalingOffer): void {
  if (offer.relayInUse === true && !offer.relayPolicy.allowRelay) {
    throw new Error('managed relay result violates relay policy')
  }
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (!signal?.aborted) return
  throw signal.reason instanceof Error ? signal.reason : new Error('managed RTC connection aborted')
}
