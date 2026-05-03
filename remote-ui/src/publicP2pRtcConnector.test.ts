import { describe, expect, it, vi } from 'vitest'
import { createPublicP2pRtcConnector, type PublicP2pRendezvousMessage } from './publicP2pRtcConnector'
import source from './publicP2pRtcConnector.ts?raw'
import type { RtcBinaryChannel, RtcConnectOptions, RtcJsonRpcChannel, RtcSession, RtcSessionDescription, RtcSessionNegotiationTarget } from './transport'

describe('PublicP2pRtcConnector', () => {
  it('uses rendezvous HTTP only before returning a connected public_p2p RtcSession', async () => {
    const session = new MockNegotiatedSession()
    const rendezvous = new MockPublicP2pRendezvous()
    const connector = createPublicP2pRtcConnector({
      rendezvous,
      createSession: () => session,
      appIdentity: {
        from: 'app-device-1',
        appPublicKey: 'app-public-1',
        appCertificate: { payload: { app_public_key: 'app-public-1' } },
      },
      signOffer: async () => ({ signature: 'sig-offer', nonce: 'nonce-1', timestamp: '1770000000' }),
    })

    const connected = await connector.connect({
      machineId: 'machine-public',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
    })

    expect(connected).toBe(session)
    expect(session.createdOffers).toEqual([{
      machineId: 'machine-public',
      terminalId: 'terminal-1',
      path: 'public_p2p',
      iceServers: [{ urls: ['stun:one.example:3478'] }],
    }])
    expect(rendezvous.createdChannels).toEqual([{
      machineId: 'machine-public',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      ttlSeconds: 600,
    }])
    expect(rendezvous.postedOffers).toEqual([{
      channelId: 'rv_1',
      channelSecret: 'secret-1',
      from: 'app-device-1',
      appPublicKey: 'app-public-1',
      appCertificate: { payload: { app_public_key: 'app-public-1' } },
      offer: { session_id: 'rtc-public-1', sdp: 'offer-sdp', ice_candidates: [] },
      signature: { algorithm: 'ed25519', nonce: 'nonce-1', timestamp: 1770000000, value: 'sig-offer' },
    }])
    expect(rendezvous.eventPolls).toEqual([{ channelId: 'rv_1', channelSecret: 'secret-1' }])
    expect(session.acceptedAnswers).toEqual([{ type: 'answer', sdp: 'answer-sdp' }])
    await expect(connected.getConnectionInfo()).resolves.toMatchObject({ path: 'public_p2p' })
  })

  it('does not expose relay as a public_p2p transport result and rejects stale answers', async () => {
    const session = new MockNegotiatedSession()
    const rendezvous = new MockPublicP2pRendezvous({
      messages: [{
        type: 'answer',
        from: 'machine-public',
        payload: {
          answer: {
            session_id: 'rtc-stale',
            sdp: 'answer-sdp',
            relay: true,
          },
        },
      }],
    })
    const connector = createPublicP2pRtcConnector({
      rendezvous,
      createSession: () => session,
      appIdentity: {
        from: 'app-device-1',
        appPublicKey: 'app-public-1',
        appCertificate: {},
      },
      signOffer: async () => ({ algorithm: 'ed25519', nonce: 'nonce-1', timestamp: 1770000000, value: 'sig-offer' }),
    })

    await expect(connector.connect({
      machineId: 'machine-public',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
    })).rejects.toThrow(/session.*mismatch/i)

    expect(session.acceptedAnswers).toEqual([])
    expect(session.disconnectCalls).toBe(1)
    expect(JSON.stringify(rendezvous.postedOffers)).not.toMatch(/relay/i)
  })

  it('rejects non-STUN public rendezvous ICE servers before creating a public_p2p offer', async () => {
    const session = new MockNegotiatedSession()
    const createSession = vi.fn(() => session)
    const rendezvous = new MockPublicP2pRendezvous({
      publicStunServers: ['stun:one.example:3478', 'turn:turn.example:3478?transport=udp'],
    })
    const connector = createPublicP2pRtcConnector({
      rendezvous,
      createSession,
      appIdentity: {
        from: 'app-device-1',
        appPublicKey: 'app-public-1',
        appCertificate: {},
      },
      signOffer: async () => ({ algorithm: 'ed25519', nonce: 'nonce-1', timestamp: 1770000000, value: 'sig-offer' }),
    })

    await expect(connector.connect({
      machineId: 'machine-public',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
    })).rejects.toThrow(/public_p2p.*stun/i)

    expect(session.createdOffers).toEqual([])
    expect(createSession).not.toHaveBeenCalled()
    expect(rendezvous.postedOffers).toEqual([])
    expect(rendezvous.closedChannels).toEqual([{ channelId: 'rv_1', channelSecret: 'secret-1' }])
  })

  it('requires a terminal id before creating a public_p2p rendezvous channel', async () => {
    const session = new MockNegotiatedSession()
    const rendezvous = new MockPublicP2pRendezvous()
    const connector = createPublicP2pRtcConnector({
      rendezvous,
      createSession: () => session,
      appIdentity: {
        from: 'app-device-1',
        appPublicKey: 'app-public-1',
        appCertificate: {},
      },
      signOffer: async () => ({ algorithm: 'ed25519', nonce: 'nonce-1', timestamp: 1770000000, value: 'sig-offer' }),
    })

    await expect(connector.connect({
      machineId: 'machine-public',
      machinePublicKeyFingerprint: 'sha256:machine',
    } as never)).rejects.toThrow(/terminal.*required/i)

    expect(rendezvous.createdChannels).toEqual([])
    expect(session.createdOffers).toEqual([])
  })

  it('polls until it finds a verified matching answer and ignores stale or untrusted answers', async () => {
    const session = new MockNegotiatedSession()
    const rendezvous = new MockPublicP2pRendezvous({
      pollBatches: [
        [],
        [{
          type: 'answer',
          from: 'machine-public',
          payload: { answer: { session_id: 'rtc-stale', sdp: 'stale-answer-sdp' } },
        }],
        [{
          type: 'answer',
          from: 'unknown-machine',
          payload: { answer: { session_id: 'rtc-public-1', sdp: 'untrusted-answer-sdp' } },
        }, {
          type: 'answer',
          from: 'machine-public',
          payload: {
            answer: { session_id: 'rtc-public-1', sdp: 'answer-sdp' },
            signature: { value: 'answer-sig' },
          },
        }],
      ],
    })
    const verifiedAnswers: unknown[] = []
    const connector = createPublicP2pRtcConnector({
      rendezvous,
      createSession: () => session,
      appIdentity: {
        from: 'app-device-1',
        appPublicKey: 'app-public-1',
        appCertificate: {},
      },
      signOffer: async () => ({ algorithm: 'ed25519', nonce: 'nonce-1', timestamp: 1770000000, value: 'sig-offer' }),
      verifyAnswer: async (input) => {
        verifiedAnswers.push(input)
        return input.from === 'machine-public' && input.sessionId === 'rtc-public-1'
      },
      maxAnswerPolls: 3,
    })

    await connector.connect({
      machineId: 'machine-public',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
    })

    expect(rendezvous.eventPolls).toHaveLength(3)
    expect(verifiedAnswers).toEqual([{
      from: 'unknown-machine',
      sessionId: 'rtc-public-1',
      sdp: 'untrusted-answer-sdp',
      signature: undefined,
      machineId: 'machine-public',
      machinePublicKeyFingerprint: 'sha256:machine',
    }, {
      from: 'machine-public',
      sessionId: 'rtc-public-1',
      sdp: 'answer-sdp',
      signature: { value: 'answer-sig' },
      machineId: 'machine-public',
      machinePublicKeyFingerprint: 'sha256:machine',
    }])
    expect(session.acceptedAnswers).toEqual([{ type: 'answer', sdp: 'answer-sdp' }])
  })

  it('cleans up the rendezvous channel and passes abort options through signing and negotiation steps', async () => {
    const session = new MockNegotiatedSession()
    const controller = new AbortController()
    const rendezvous = new MockPublicP2pRendezvous({
      messages: [],
    })
    const seenSignals: unknown[] = []
    const connector = createPublicP2pRtcConnector({
      rendezvous,
      createSession: () => session,
      appIdentity: {
        from: 'app-device-1',
        appPublicKey: 'app-public-1',
        appCertificate: {},
      },
      signOffer: async (_input, options) => {
        seenSignals.push(options?.signal)
        controller.abort()
        return { algorithm: 'ed25519', nonce: 'nonce-1', timestamp: 1770000000, value: 'sig-offer' }
      },
      maxAnswerPolls: 1,
    })

    await expect(connector.connect({
      machineId: 'machine-public',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
    }, { signal: controller.signal })).rejects.toThrow(/aborted/i)

    expect(session.createOfferOptions).toEqual([{ signal: controller.signal }])
    expect(seenSignals).toEqual([controller.signal])
    expect(session.disconnectCalls).toBe(1)
    expect(rendezvous.closedChannels).toEqual([{ channelId: 'rv_1', channelSecret: 'secret-1' }])
    expect(rendezvous.postedOffers).toEqual([])
  })

  it('keeps public rendezvous as signaling, not a runtime transport implementation', () => {
    expect(source).toMatch(/RtcConnector<PublicP2pConnectInput>/)
    expect(source).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket|relayTransport|paid_relay|managed_p2p|anonymous_p2p/)
    expect(source).not.toMatch(/openTerminal\(|openApi\(|openFileTransfer\(|subscribeEvents\(/)
  })
})

class MockPublicP2pRendezvous {
  readonly createdChannels: unknown[] = []
  readonly postedOffers: unknown[] = []
  readonly eventPolls: unknown[] = []
  readonly closedChannels: unknown[] = []
  private readonly messages: PublicP2pRendezvousMessage[]
  private readonly pollBatches: PublicP2pRendezvousMessage[][]
  private readonly publicStunServers: string[]

  constructor(options: {
    messages?: PublicP2pRendezvousMessage[]
    pollBatches?: PublicP2pRendezvousMessage[][]
    publicStunServers?: string[]
  } = {}) {
    this.messages = options.messages ?? [{
      type: 'answer',
      from: 'machine-public',
      payload: {
        answer: {
          session_id: 'rtc-public-1',
          sdp: 'answer-sdp',
        },
      },
    }]
    this.pollBatches = options.pollBatches ?? []
    this.publicStunServers = options.publicStunServers ?? ['stun:one.example:3478']
  }

  async createChannel(input: unknown) {
    this.createdChannels.push(input)
    return {
      channelId: 'rv_1',
      channelSecret: 'secret-1',
      publicStunServers: this.publicStunServers,
    }
  }

  async postOffer(input: unknown) {
    this.postedOffers.push(input)
  }

  async pollEvents(input: unknown) {
    this.eventPolls.push(input)
    return this.pollBatches.shift() ?? this.messages
  }

  async closeChannel(input: unknown) {
    this.closedChannels.push(input)
  }
}

class MockNegotiatedSession implements RtcSession {
  readonly createdOffers: RtcSessionNegotiationTarget[] = []
  readonly createOfferOptions: Array<RtcConnectOptions | undefined> = []
  readonly acceptedAnswers: RtcSessionDescription[] = []
  disconnectCalls = 0

  async createOffer(input: RtcSessionNegotiationTarget, options?: RtcConnectOptions) {
    this.createdOffers.push(input)
    this.createOfferOptions.push(options)
    return {
      sessionId: 'rtc-public-1',
      description: { type: 'offer' as const, sdp: 'offer-sdp' },
    }
  }

  async acceptAnswer(answer: RtcSessionDescription, _options?: unknown): Promise<void> {
    this.acceptedAnswers.push(answer)
  }

  async openTerminal(): Promise<RtcBinaryChannel> {
    throw new Error('terminal is not used by connector tests')
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    throw new Error('api is not used by connector tests')
  }

  async openFileTransfer(): Promise<RtcBinaryChannel> {
    throw new Error('file transfer is not used by connector tests')
  }

  subscribeEvents() {
    return { close() {} }
  }

  async getConnectionInfo() {
    return {
      path: 'public_p2p' as const,
      connectionId: 'rtc-public-1',
      machineId: 'machine-public',
      relayInUse: false,
    }
  }

  async getCapabilities() {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: false,
    }
  }

  async disconnect(): Promise<void> {
    this.disconnectCalls += 1
  }
}
