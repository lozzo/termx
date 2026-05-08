import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createManagedHubRtcConnector } from './managedHubRtcConnector'
import type { ConnectionLogEvent } from './connectionLogger'
import source from './managedHubRtcConnector.ts?raw'
import type { ManagedHubApi } from './managedHubApi'
import type { ConnectionCapabilities, RtcBinaryChannel, RtcJsonRpcChannel, RtcSession } from './transport'

describe('ManagedHubRtcConnector', () => {
  beforeEach(() => {
    vi.stubGlobal('crypto', fixedCrypto())
  })

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
      iceServers: [
        { urls: ['stun:hub.termx.test:3478'] },
        { urls: ['turn:hub.termx.test:3478?transport=udp'], username: 'turn-user', credential: 'turn-pass' },
      ],
    }])
    expect(api.preflightSessions).toEqual([{
      machineId: 'machine-1',
      terminalId: '',
      sessionToken: 'session-token-1',
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
      fileTransferAllowed: true,
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
      iceServers: [
        { urls: ['stun:hub.termx.test:3478'] },
        { urls: ['turn:hub.termx.test:3478?transport=udp'], username: 'turn-user', credential: 'turn-pass' },
      ],
    }])
    expect(api.createdSessions[0]).toMatchObject({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    })
  })

  it('logs Hub signaling and answer stages with the hub URL and session id', async () => {
    const api = new MockManagedHubApi({ pending: true })
    const session = new MockOffererSession()
    const logs: ConnectionLogEvent[] = []
    const connector = createManagedHubRtcConnector({
      api,
      createSession: () => session,
      answerPollDelayMs: 0,
      hubUrl: 'https://hub-1.termx.test',
      logger: { log: (event) => logs.push(event) },
    })

    await connector.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
    })

    expect(logs).toEqual(expect.arrayContaining([
      expect.objectContaining({ scope: 'managed_hub', event: 'offer_created', hubUrl: 'https://hub-1.termx.test', sessionId: 'rtc-managed-1' }),
      expect.objectContaining({ scope: 'managed_hub', event: 'hub_session_create_result', hubUrl: 'https://hub-1.termx.test', sessionId: 'rtc-managed-1' }),
      expect.objectContaining({ scope: 'managed_hub', event: 'answer_poll_start', hubUrl: 'https://hub-1.termx.test', sessionId: 'rtc-managed-1' }),
      expect.objectContaining({ scope: 'managed_hub', event: 'answer_received', hubUrl: 'https://hub-1.termx.test', sessionId: 'rtc-managed-1' }),
      expect.objectContaining({ scope: 'managed_hub', event: 'connect_success', level: 'info', hubUrl: 'https://hub-1.termx.test', sessionId: 'rtc-managed-1' }),
    ]))
  })

  it('disconnects the browser session if Hub signaling fails', async () => {
    const session = new MockOffererSession()
    const connector = createManagedHubRtcConnector({
      api: {
        async getSessionIce() {
          return {
            path: 'managed' as const,
            machineId: 'machine-1',
            iceServers: [],
            relayPolicy: { allowRelay: false, allowRelayTransfer: false },
          }
        },
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

  it('requires a valid agent answer proof when an answer proof secret is provided', async () => {
    const api = new MockManagedHubApi({ answerProofSecret: 'proof-secret', pairSessionId: 'pair-1' })
    const session = new MockOffererSession()
    const connector = createManagedHubRtcConnector({
      api,
      createSession: () => session,
    })

    await connector.connect({
      machineId: 'machine-1',
      sessionToken: sessionTokenWithID('pair-1'),
      answerProofSecret: 'proof-secret',
    })

    expect(api.createdSessions[0]).toMatchObject({
      answerProofChallenge: 'AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE',
    })
    expect(session.acceptedAnswers).toEqual([{ type: 'answer', sdp: 'answer-sdp' }])
  })

  it('rejects forged agent answers when the proof does not match the local pairing secret', async () => {
    const api = new MockManagedHubApi({ answerProof: 'forged-proof' })
    const session = new MockOffererSession()
    const connector = createManagedHubRtcConnector({
      api,
      createSession: () => session,
    })

    await expect(connector.connect({
      machineId: 'machine-1',
      sessionToken: sessionTokenWithID('pair-1'),
      answerProofSecret: 'proof-secret',
    })).rejects.toThrow(/answer proof mismatch/i)
    expect(session.disconnectCalls).toBe(1)
  })

  it('keeps managed Hub signaling separate from runtime transport taxonomy', () => {
    expect(source).not.toMatch(/WebSocket|paid_relay|managed_p2p|anonymous_p2p|relayTransport|path:\s*['"]relay['"]/)
  })
})

class MockManagedHubApi implements ManagedHubApi {
  readonly preflightSessions: unknown[] = []
  readonly createdSessions: unknown[] = []
  readonly polledAnswers: unknown[] = []
  constructor(private readonly options: {
    pending?: boolean
    answerProof?: string
    answerProofSecret?: string
    pairSessionId?: string
  } = {}) {}

  async getSessionIce(input: Parameters<ManagedHubApi['getSessionIce']>[0]) {
    this.preflightSessions.push(input)
    return {
      path: 'managed' as const,
      machineId: input.machineId,
      terminalId: input.terminalId || undefined,
      iceServers: [
        { urls: ['stun:hub.termx.test:3478'] },
        { urls: ['turn:hub.termx.test:3478?transport=udp'], username: 'turn-user', credential: 'turn-pass' },
      ],
      relayPolicy: { allowRelay: true, allowRelayTransfer: false },
    }
  }

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
      ...(this.answerProofFor(input) ? { answerProof: this.answerProofFor(input) } : {}),
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
      ...(this.answerProofFor(input) ? { answerProof: this.answerProofFor(input) } : {}),
    }
  }

  async pair(): ReturnType<ManagedHubApi['pair']> {
    throw new Error('not used')
  }

  private answerProofFor(input: { sessionId?: string | undefined; answerProofChallenge?: string | undefined }): string | undefined {
    if (this.options.answerProof !== undefined) return this.options.answerProof
    if (!this.options.answerProofSecret || !this.options.pairSessionId || !input.answerProofChallenge) return undefined
    return expectedAnswerProof(this.options.answerProofSecret, this.options.pairSessionId, input.sessionId ?? 'rtc-managed-1', input.answerProofChallenge)
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

function fixedCrypto(): Crypto {
  return {
    getRandomValues<T extends ArrayBufferView | null>(array: T): T {
      if (array instanceof Uint8Array) array.fill(1)
      return array
    },
    subtle: {
      async importKey() {
        return {} as CryptoKey
      },
      async sign(_algorithm: AlgorithmIdentifier | HmacImportParams, _key: CryptoKey, data: BufferSource) {
        const bytes = new Uint8Array(data as ArrayBuffer)
        const text = new TextDecoder().decode(bytes)
        return new TextEncoder().encode(`sig:${text}`).buffer
      },
    } as unknown as SubtleCrypto,
  } as Crypto
}

function expectedAnswerProof(secret: string, pairSessionId: string, offerSessionId: string, challenge: string): string {
  void secret
  const text = `sig:termx-answer-proof-v1:${pairSessionId}:${offerSessionId}:${challenge}`
  return base64url(new TextEncoder().encode(text))
}

function sessionTokenWithID(sessionId: string): string {
  return `${base64url(new TextEncoder().encode(JSON.stringify({ sid: sessionId })))}.mac`
}

function base64url(bytes: Uint8Array): string {
  let binary = ''
  for (const value of bytes) binary += String.fromCharCode(value)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}
