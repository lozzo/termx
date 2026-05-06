import { describe, expect, it } from 'vitest'
import { createManagedHubRtcConnector } from './managedHubRtcConnector'
import source from './managedHubRtcConnector.ts?raw'
import type { ManagedHubApi } from './managedHubApi'
import type { ConnectionCapabilities, RtcBinaryChannel, RtcJsonRpcChannel, RtcSession } from './transport'

describe('ManagedHubRtcConnector', () => {
  it('creates a browser offer, posts it with the session token to Hub, and accepts the answer', async () => {
    const api = new MockManagedHubApi()
    const session = new MockOffererSession()
    const connector = createManagedHubRtcConnector({
      api,
      createSession: () => session,
    })

    const connected = await connector.connect({
      machineId: 'machine-1',
      sessionToken: 'session-token-1',
    })

    expect(connected).toBe(session)
    expect(session.createdOffers).toEqual([{
      machineId: 'machine-1',
      path: 'managed',
    }])
    expect(api.createdSessions).toEqual([{
      machineId: 'machine-1',
      terminalId: '',
      sessionToken: 'session-token-1',
      offer: {
        sessionId: 'rtc-managed-1',
        sdp: 'offer-sdp',
        iceCandidates: [],
      },
    }])
    expect(session.acceptedAnswers).toEqual([{ type: 'answer', sdp: 'answer-sdp' }])
    await expect(session.getCapabilities()).resolves.toMatchObject({
      terminalAllowed: true,
      eventsAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: false,
    })
  })

  it('polls accepted pending Hub sessions until an answer is available', async () => {
    const api = new MockManagedHubApi({ pending: true })
    const session = new MockOffererSession()
    const connector = createManagedHubRtcConnector({
      api,
      createSession: () => session,
      answerPollDelayMs: 0,
    })

    await connector.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
    })

    expect(api.polledAnswers).toEqual([{
      sessionId: 'rtc-managed-1',
      machineId: 'machine-1',
    }])
    expect(session.acceptedAnswers).toEqual([{ type: 'answer', sdp: 'answer-after-pending' }])
    await expect(session.getCapabilities()).resolves.toMatchObject({
      terminalAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: false,
      relayInUse: true,
    })
  })

  it('can run the same Hub signaling flow for a local embedded Hub path', async () => {
    const api = new MockManagedHubApi()
    const session = new MockOffererSession()
    const connector = createManagedHubRtcConnector({
      api,
      createSession: () => session,
    })

    await connector.connect({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-local',
      path: 'local',
    })

    expect(session.createdOffers).toEqual([{
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
    }])
    expect(api.createdSessions[0]).toMatchObject({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    })
  })

  it('disconnects the browser session if Hub signaling fails', async () => {
    const session = new MockOffererSession()
    const connector = createManagedHubRtcConnector({
      api: {
        async createSession() {
          throw new Error('session rejected')
        },
        async pollSessionAnswer() {
          throw new Error('not used')
        },
      },
      createSession: () => session,
    })

    await expect(connector.connect({
      machineId: 'machine-1',
      sessionToken: 'session-token-1',
    })).rejects.toThrow(/session rejected/i)
    expect(session.disconnectCalls).toBe(1)
  })

  it('keeps managed Hub signaling separate from runtime transport taxonomy', () => {
    expect(source).not.toMatch(/WebSocket|paid_relay|managed_p2p|anonymous_p2p|relayTransport|path:\s*['"]relay['"]/)
  })
})

class MockManagedHubApi implements ManagedHubApi {
  readonly createdSessions: unknown[] = []
  readonly polledAnswers: unknown[] = []
  constructor(private readonly options: { pending?: boolean } = {}) {}

  async createSession(input: Parameters<ManagedHubApi['createSession']>[0]) {
    this.createdSessions.push(input)
    if (this.options.pending) {
      return {
        sessionId: 'rtc-managed-1',
        path: 'managed' as const,
        machineId: 'machine-1',
        terminalId: input.terminalId || undefined,
        pending: true as const,
      }
    }
    return {
      sessionId: 'rtc-managed-1',
      path: 'managed' as const,
      machineId: 'machine-1',
      terminalId: input.terminalId || undefined,
      answer: { type: 'answer' as const, sdp: 'answer-sdp' },
      iceCandidates: [],
      iceServers: [],
      relayPolicy: { allowRelay: false, allowRelayTransfer: false },
      relayInUse: false,
    }
  }

  async pollSessionAnswer(input: Parameters<ManagedHubApi['pollSessionAnswer']>[0]) {
    this.polledAnswers.push(input)
    return {
      sessionId: 'rtc-managed-1',
      path: 'managed' as const,
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      answer: { type: 'answer' as const, sdp: 'answer-after-pending' },
      iceCandidates: [],
      iceServers: [],
      relayPolicy: { allowRelay: true, allowRelayTransfer: false },
      relayInUse: true,
    }
  }

  async pair(): ReturnType<ManagedHubApi['pair']> {
    throw new Error('not used')
  }
}

class MockOffererSession implements RtcSession {
  readonly createdOffers: unknown[] = []
  readonly acceptedAnswers: unknown[] = []
  disconnectCalls = 0
  private capabilities: ConnectionCapabilities = {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    terminalManagementAllowed: true,
    relayInUse: false,
  }

  async createOffer(input: unknown) {
    this.createdOffers.push(input)
    return {
      sessionId: 'rtc-managed-1',
      description: { type: 'offer' as const, sdp: 'offer-sdp' },
    }
  }

  async acceptAnswer(answer: unknown) {
    this.acceptedAnswers.push(answer)
  }

  updateConnectionCapabilities(capabilities: ConnectionCapabilities): void {
    this.capabilities = capabilities
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
      connectionId: 'rtc-managed-1',
      machineId: 'machine-1',
      relayInUse: this.capabilities.relayInUse,
    }
  }

  async getCapabilities() {
    return this.capabilities
  }

  async disconnect(): Promise<void> {
    this.disconnectCalls += 1
  }
}
