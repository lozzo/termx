import { describe, expect, it, vi } from 'vitest'
import { createLocalRtcConnector } from './localRtcConnector'
import appSource from './LocalRemoteApp.tsx?raw'
import browserSource from './browserRtcSession.ts?raw'
import type { LocalRTCAnswer, RtcBinaryChannel, RtcJsonRpcChannel, RtcConnectOptions, RtcSession, RtcSessionDescription, RtcSessionNegotiationTarget } from './transport'

describe('LocalRtcConnector', () => {
  it('owns local HTTP signaling and returns an already connected RtcSession', async () => {
    const session = new MockOfferSession()
    const createRTCAnswer = vi.fn(async (offer): Promise<LocalRTCAnswer> => ({
      sessionId: offer.sessionId,
      answer: { type: 'answer', sdp: 'answer-sdp' },
    }))
    const signOffer = vi.fn(async () => ({
      signature: 'signature',
      nonce: 'nonce-1',
      timestamp: '1770000000',
    }))
    const connector = createLocalRtcConnector({
      api: { createRTCAnswer },
      getAppCertificate: () => 'app-cert',
      createSession: () => session,
      signOffer,
    })

    const connected = await connector.connect({ machineId: 'machine-local', terminalId: 'terminal-1' })

    expect(connected).toBe(session)
    expect(session.createdOffers).toEqual([{ machineId: 'machine-local', terminalId: 'terminal-1', path: 'local' }])
    expect(signOffer).toHaveBeenCalledWith({
      sessionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sdp: 'offer-sdp',
      candidates: [],
    })
    expect(createRTCAnswer).toHaveBeenCalledWith({
      sessionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sdp: 'offer-sdp',
      iceCandidates: [],
      appCertificate: 'app-cert',
      appSignature: 'signature',
      nonce: 'nonce-1',
      timestamp: '1770000000',
    }, {})
    expect(session.acceptedAnswers).toEqual([{ type: 'answer', sdp: 'answer-sdp' }])
    await expect(connected.getConnectionInfo()).resolves.toMatchObject({ path: 'local' })
  })

  it('keeps connector responsibilities out of the UI and browser runtime adapter source', () => {
    expect(appSource).not.toMatch(/createConnectedSession|LocalRemoteTransport|createTransport/)
    const sessionSource = browserSource.slice(
      browserSource.indexOf('class BrowserRtcSession'),
      browserSource.indexOf('class BrowserRtcInventoryEventsConnection'),
    )
    expect(sessionSource).not.toMatch(/createRTCAnswer|signOffer|appCertificate|LocalRTCOffer|LocalRTCAnswer/)
    expect(sessionSource).not.toMatch(/\bconnect\s*\(/)
  })

  it('rejects stale local answers whose session id does not match the offer and disconnects the session', async () => {
    const session = new MockOfferSession()
    const connector = createLocalRtcConnector({
      api: {
        createRTCAnswer: async (): Promise<LocalRTCAnswer> => ({
          sessionId: 'rtc-stale',
          answer: { type: 'answer', sdp: 'stale-answer' },
        }),
      },
      getAppCertificate: () => 'app-cert',
      createSession: () => session,
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })

    await expect(connector.connect({ machineId: 'machine-local', terminalId: 'terminal-1' }))
      .rejects.toThrow(/session.*mismatch/i)
    expect(session.acceptedAnswers).toEqual([])
    expect(session.disconnectCalls).toBe(1)
  })

  it('passes abort options to local signaling and disconnects the session if the connection is aborted', async () => {
    const session = new MockOfferSession()
    const controller = new AbortController()
    const seenOptions: RtcConnectOptions[] = []
    const connector = createLocalRtcConnector({
      api: {
        async createRTCAnswer(_offer, options?: RtcConnectOptions): Promise<LocalRTCAnswer> {
          if (options) seenOptions.push(options)
          controller.abort()
          return {
            sessionId: 'rtc-local-1',
            answer: { type: 'answer', sdp: 'answer-sdp' },
          }
        },
      },
      getAppCertificate: () => 'app-cert',
      createSession: () => session,
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })

    await expect(connector.connect(
      { machineId: 'machine-local', terminalId: 'terminal-1' },
      { signal: controller.signal },
    )).rejects.toThrow(/aborted/i)
    expect(seenOptions).toEqual([{ signal: controller.signal }])
    expect(session.acceptedAnswers).toEqual([])
    expect(session.disconnectCalls).toBe(1)
  })

  it('supports machine-scoped api sessions for creating the first terminal without an existing terminal id', async () => {
    const session = new MockOfferSession()
    const createRTCAnswer = vi.fn(async (offer): Promise<LocalRTCAnswer> => ({
      sessionId: offer.sessionId,
      answer: { type: 'answer', sdp: 'answer-sdp' },
    }))
    const signOffer = vi.fn(async () => ({
      signature: 'signature',
      nonce: 'nonce-machine',
      timestamp: '1770000000',
    }))
    const connector = createLocalRtcConnector({
      api: { createRTCAnswer },
      getAppCertificate: () => 'app-cert',
      createSession: () => session,
      signOffer,
    })

    const connected = await connector.connect({ machineId: 'machine-local' })

    expect(connected).toBe(session)
    expect(session.createdOffers).toEqual([{ machineId: 'machine-local', path: 'local' }])
    expect(signOffer).toHaveBeenCalledWith({
      sessionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: '',
      sdp: 'offer-sdp',
      candidates: [],
    })
    expect(createRTCAnswer).toHaveBeenCalledWith(expect.objectContaining({
      sessionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: '',
      sdp: 'offer-sdp',
      iceCandidates: [],
    }), {})
  })

  it('applies server-negotiated data channel capabilities to the connected session', async () => {
    const session = new MockOfferSession()
    const connector = createLocalRtcConnector({
      api: {
        createRTCAnswer: async (offer): Promise<LocalRTCAnswer> => ({
          sessionId: offer.sessionId,
          answer: { type: 'answer', sdp: 'answer-sdp' },
          dataChannels: ['api'],
          capabilities: {
            terminalAllowed: false,
            apiAllowed: true,
            eventsAllowed: false,
            fileTransferAllowed: false,
            terminalManagementAllowed: true,
            relayInUse: false,
          },
        }),
      },
      getAppCertificate: () => 'app-cert',
      createSession: () => session,
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })

    await connector.connect({ machineId: 'machine-local' })

    await expect(session.getCapabilities()).resolves.toMatchObject({
      terminalAllowed: false,
      apiAllowed: true,
      eventsAllowed: false,
      fileTransferAllowed: false,
      terminalManagementAllowed: true,
    })
  })

  it('does not infer terminal management permission from api channel labels alone', async () => {
    const session = new MockOfferSession()
    const connector = createLocalRtcConnector({
      api: {
        createRTCAnswer: async (offer): Promise<LocalRTCAnswer> => ({
          sessionId: offer.sessionId,
          answer: { type: 'answer', sdp: 'answer-sdp' },
          dataChannels: ['api', 'terminal:{terminal_id}', 'file:{transfer_id}'],
          capabilities: {
            terminalAllowed: true,
            apiAllowed: true,
            eventsAllowed: false,
            fileTransferAllowed: true,
            terminalManagementAllowed: false,
            relayInUse: false,
          },
        }),
      },
      getAppCertificate: () => 'app-cert',
      createSession: () => session,
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })

    await connector.connect({ machineId: 'machine-local', terminalId: 'terminal-1' })

    await expect(session.getCapabilities()).resolves.toMatchObject({
      apiAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: false,
    })
  })
})

class MockOfferSession implements RtcSession {
  readonly createdOffers: RtcSessionNegotiationTarget[] = []
  readonly acceptedAnswers: RtcSessionDescription[] = []
  disconnectCalls = 0
  capabilities = {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    terminalManagementAllowed: true,
    relayInUse: false,
  }

  async createOffer(input: RtcSessionNegotiationTarget): Promise<{ sessionId: string; description: RtcSessionDescription }> {
    if (input.path !== 'local') throw new Error(`unexpected path ${input.path}`)
    this.createdOffers.push(input)
    return {
      sessionId: 'rtc-local-1',
      description: { type: 'offer', sdp: 'offer-sdp' },
    }
  }

  async acceptAnswer(answer: RtcSessionDescription): Promise<void> {
    this.acceptedAnswers.push(answer)
  }

  async disconnect(): Promise<void> {
    this.disconnectCalls += 1
  }

  async openTerminal(): Promise<RtcBinaryChannel> {
    throw new Error('terminal channel is not used by local connector tests')
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    throw new Error('api channel is not used by local connector tests')
  }

  async openFileTransfer(): Promise<RtcBinaryChannel> {
    throw new Error('file channel is not used by local connector tests')
  }

  subscribeEvents() {
    return { close() {} }
  }

  async getConnectionInfo() {
    return {
      path: 'local' as const,
      connectionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      relayInUse: false,
    }
  }

  async getCapabilities() {
    return this.capabilities
  }

  updateConnectionCapabilities(capabilities: typeof this.capabilities): void {
    this.capabilities = capabilities
  }
}
