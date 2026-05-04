import { describe, expect, it, vi } from 'vitest'
import { createManagedHubRtcConnector } from './managedHubRtcConnector'
import source from './managedHubRtcConnector.ts?raw'
import type { ManagedHubApi } from './managedHubApi'
import type { ConnectionCapabilities, RtcBinaryChannel, RtcJsonRpcChannel, RtcSession } from './transport'

describe('ManagedHubRtcConnector', () => {
  it('creates a browser offer, signs it with the Web Control ticket, posts it to Hub, and accepts the answer', async () => {
    const api = new MockManagedHubApi()
    const session = new MockOffererSession()
    const signOffer = vi.fn(async () => ({
      signature: 'offer-signature',
      nonce: 'nonce-1',
      timestamp: '1777808400',
    }))
    const connector = createManagedHubRtcConnector({
      api,
      createSession: () => session,
      signOffer,
    })

    const connected = await connector.connect({
      machineId: 'machine-1',
      connectTicket: 'ticket-1',
      appCertificate: { payload: { machine_id: 'machine-1' }, signature: 'cert-sig' },
    })

    expect(connected).toBe(session)
    expect(session.createdOffers).toEqual([{
      machineId: 'machine-1',
      path: 'managed',
    }])
    expect(signOffer).toHaveBeenCalledWith({
      sessionId: 'rtc-managed-1',
      ticketId: 'ticket-1',
      machineId: 'machine-1',
      terminalId: '',
      sdp: 'offer-sdp',
      candidates: [],
    })
    expect(api.createdSessions).toEqual([{
      connectTicket: 'ticket-1',
      machineId: 'machine-1',
      terminalId: '',
      appCertificate: { payload: { machine_id: 'machine-1' }, signature: 'cert-sig' },
      offer: {
        sessionId: 'rtc-managed-1',
        sdp: 'offer-sdp',
        iceCandidates: [],
      },
      signature: {
        algorithm: 'ed25519',
        nonce: 'nonce-1',
        timestamp: 1777808400,
        value: 'offer-signature',
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
      signOffer: async () => ({
        signature: 'offer-signature',
        nonce: 'nonce-1',
        timestamp: '1777808400',
      }),
      answerPollDelayMs: 0,
    })

    await connector.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      connectTicket: 'ticket-1',
      appCertificate: { payload: { machine_id: 'machine-1' } },
    })

    expect(api.polledAnswers).toEqual([{
      sessionId: 'rtc-managed-1',
      connectTicket: 'ticket-1',
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

  it('disconnects the browser session if Hub signaling fails', async () => {
    const session = new MockOffererSession()
    const connector = createManagedHubRtcConnector({
      api: {
        async createSession() {
          throw new Error('ticket rejected')
        },
        async pollSessionAnswer() {
          throw new Error('not used')
        },
      },
      createSession: () => session,
      signOffer: async () => ({
        signature: 'offer-signature',
        nonce: 'nonce-1',
        timestamp: '1777808400',
      }),
    })

    await expect(connector.connect({
      machineId: 'machine-1',
      connectTicket: 'ticket-1',
      appCertificate: {},
    })).rejects.toThrow(/ticket rejected/i)
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
