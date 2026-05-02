import { describe, expect, it } from 'vitest'
import { createManagedRtcConnector, type ManagedRtcAnswerTarget, type ManagedSignalingOffer } from './managedRtcConnector'
import source from './managedRtcConnector.ts?raw'
import type { ConnectionCapabilities, RtcBinaryChannel, RtcJsonRpcChannel, RtcSession } from './transport'

describe('ManagedRtcConnector', () => {
  it('accepts a hub signaling offer and returns one managed RtcSession regardless of relay policy', async () => {
    const session = new MockManagedSession()
    const signaling = new MockManagedSignaling()
    const connector = createManagedRtcConnector({
      signaling,
      createSession: () => session,
    })

    const connected = await connector.connect({
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      hubSessionId: 'hub-session-1',
      deviceId: 'device-1',
    })

    expect(connected).toBe(session)
    expect(signaling.polledOffers).toEqual([{
      hubSessionId: 'hub-session-1',
      deviceId: 'device-1',
    }])
    expect(session.acceptedOffers).toEqual([{
      sessionId: 'managed-rtc-1',
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      path: 'managed',
      description: { type: 'offer', sdp: 'managed-offer-sdp' },
      iceServers: [{ urls: ['turn:turn.example:3478'], username: 'u', credential: 'p' }],
      relayPolicy: { allowRelay: true, allowRelayTransfer: false },
      relayInUse: true,
    }])
    expect(signaling.submittedAnswers).toEqual([{
      hubSessionId: 'hub-session-1',
      deviceId: 'device-1',
      answer: { sessionId: 'managed-rtc-1', sdp: 'managed-answer-sdp' },
    }])
    await expect(connected.getConnectionInfo()).resolves.toMatchObject({
      path: 'managed',
      relayInUse: true,
    })
    await expect(connected.getCapabilities()).resolves.toMatchObject({
      relayInUse: true,
      fileTransferAllowed: false,
    })
  })

  it('does not create relay as a client transport type when managed policy uses relay', async () => {
    const session = new MockManagedSession()
    const connector = createManagedRtcConnector({
      signaling: new MockManagedSignaling(),
      createSession: () => session,
    })

    await connector.connect({
      machineId: 'machine-managed',
      hubSessionId: 'hub-session-1',
      deviceId: 'device-1',
    })

    await expect(session.getConnectionInfo()).resolves.toMatchObject({ path: 'managed' })
    expect(JSON.stringify(session.acceptedOffers)).not.toMatch(/paid_relay|relayTransport|path":"relay/)
  })

  it('rejects foreign managed offers before creating an answer', async () => {
    const session = new MockManagedSession()
    const signaling = new MockManagedSignaling({
      offer: {
        sessionId: 'managed-rtc-foreign',
        machineId: 'other-machine',
        terminalId: 'terminal-1',
        sdp: 'managed-offer-sdp',
        iceServers: [],
        relayPolicy: { allowRelay: false, allowRelayTransfer: false },
      },
    })
    const connector = createManagedRtcConnector({
      signaling,
      createSession: () => session,
    })

    await expect(connector.connect({
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      hubSessionId: 'hub-session-1',
      deviceId: 'device-1',
    })).rejects.toThrow(/managed offer machine mismatch/i)

    expect(session.acceptedOffers).toEqual([])
    expect(signaling.submittedAnswers).toEqual([])
    expect(signaling.rejectedOffers).toEqual([{
      hubSessionId: 'hub-session-1',
      deviceId: 'device-1',
      sessionId: 'managed-rtc-foreign',
      reason: expect.stringMatching(/machine mismatch/),
    }])
  })

  it('rejects contradictory relay policy before creating an answer', async () => {
    const session = new MockManagedSession()
    const signaling = new MockManagedSignaling({
      offer: {
        sessionId: 'managed-rtc-1',
        machineId: 'machine-managed',
        terminalId: 'terminal-1',
        sdp: 'managed-offer-sdp',
        iceServers: [],
        relayPolicy: { allowRelay: false, allowRelayTransfer: false },
        relayInUse: true,
      },
    })
    const connector = createManagedRtcConnector({
      signaling,
      createSession: () => session,
    })

    await expect(connector.connect({
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      hubSessionId: 'hub-session-1',
      deviceId: 'device-1',
    })).rejects.toThrow(/relay.*policy/i)

    expect(session.acceptedOffers).toEqual([])
    expect(signaling.submittedAnswers).toEqual([])
    expect(signaling.rejectedOffers).toEqual([{
      hubSessionId: 'hub-session-1',
      deviceId: 'device-1',
      sessionId: 'managed-rtc-1',
      reason: expect.stringMatching(/relay.*policy/i),
    }])
  })

  it('passes abort options through to the managed answerer', async () => {
    const controller = new AbortController()
    const session = new MockManagedSession({
      onAcceptOffer: (_offer, options) => {
        if (options?.signal !== controller.signal) {
          throw new Error('signal was not forwarded to managed answerer')
        }
      },
    })
    const signaling = new MockManagedSignaling()
    const connector = createManagedRtcConnector({
      signaling,
      createSession: () => session,
    })

    await connector.connect({
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      hubSessionId: 'hub-session-1',
      deviceId: 'device-1',
    }, { signal: controller.signal })

    expect(session.acceptedOffers).toHaveLength(1)
  })

  it('keeps managed signaling separate from runtime transport taxonomy', () => {
    expect(source).toMatch(/RtcConnector<ManagedRtcConnectInput>/)
    expect(source).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket|paid_relay|managed_p2p|anonymous_p2p|relayTransport/)
    expect(source).not.toMatch(/openTerminal\(|openApi\(|openFileTransfer\(|subscribeEvents\(/)
  })
})

class MockManagedSignaling {
  readonly polledOffers: unknown[] = []
  readonly submittedAnswers: unknown[] = []
  readonly rejectedOffers: unknown[] = []
  private readonly offer: ManagedSignalingOffer

  constructor(options: { offer?: ManagedSignalingOffer } = {}) {
    this.offer = options.offer ?? {
      sessionId: 'managed-rtc-1',
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      sdp: 'managed-offer-sdp',
      iceServers: [{ urls: ['turn:turn.example:3478'], username: 'u', credential: 'p' }],
      relayPolicy: { allowRelay: true, allowRelayTransfer: false },
      relayInUse: true,
    }
  }

  async pollOffer(input: unknown): Promise<ManagedSignalingOffer> {
    this.polledOffers.push(input)
    return this.offer
  }

  async submitAnswer(input: unknown): Promise<void> {
    this.submittedAnswers.push(input)
  }

  async rejectOffer(input: unknown): Promise<void> {
    this.rejectedOffers.push(input)
  }
}

class MockManagedSession implements RtcSession {
  readonly acceptedOffers: unknown[] = []
  disconnectCalls = 0
  private relayInUse = false
  private readonly onAcceptOffer: ((input: unknown, options?: { signal?: AbortSignal }) => void) | undefined
  private capabilities: ConnectionCapabilities = {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    terminalManagementAllowed: true,
    relayInUse: false,
  }

  constructor(options: { onAcceptOffer?: (input: unknown, options?: { signal?: AbortSignal }) => void } = {}) {
    this.onAcceptOffer = options.onAcceptOffer
  }

  async acceptOffer(input: ManagedRtcAnswerTarget, options?: { signal?: AbortSignal }) {
    this.onAcceptOffer?.(input, options)
    this.acceptedOffers.push(input)
    this.relayInUse = input.relayInUse
    this.capabilities = {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: input.relayPolicy.allowRelayTransfer,
      terminalManagementAllowed: true,
      relayInUse: input.relayInUse,
    }
    return { sessionId: 'managed-rtc-1', description: { type: 'answer' as const, sdp: 'managed-answer-sdp' } }
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
      path: 'managed' as const,
      connectionId: 'managed-rtc-1',
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      relayInUse: this.relayInUse,
    }
  }

  async getCapabilities() {
    return this.capabilities
  }

  async disconnect(): Promise<void> {
    this.disconnectCalls += 1
  }
}
