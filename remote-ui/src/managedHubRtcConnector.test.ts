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
    const states: unknown[] = []
    const connector = createManagedHubRtcConnector({
      api,
      createSession: () => session,
    })

    const connected = await connector.connect({
      machineId: 'machine-1',
      sessionToken: 'session-token-1',
    }, {
      onConnectionState: (snapshot) => states.push(snapshot),
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
    expect(states).toEqual([
      expect.objectContaining({ machineId: 'machine-1', phase: 'probing', path: 'managed', statusText: 'Fetching ICE servers...' }),
      expect.objectContaining({ machineId: 'machine-1', phase: 'probing', path: 'managed', statusText: 'Creating WebRTC offer...' }),
      expect.objectContaining({ machineId: 'machine-1', phase: 'connecting', path: 'managed', statusText: 'Exchanging signals with hub...' }),
      expect.objectContaining({ machineId: 'machine-1', phase: 'connecting', path: 'managed', statusText: 'Opening data channels...' }),
      expect.objectContaining({ machineId: 'machine-1', phase: 'connected', path: 'managed', statusText: 'Connected', relayInUse: false }),
    ])
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
    const statuses: string[] = []
    const connector = createManagedHubRtcConnector({
      api,
      createSession: () => session,
      answerPollDelayMs: 0,
    })

    await connector.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
    }, {
      onStatus: (status) => statuses.push(status),
    })

    expect(api.polledAnswers).toEqual([{
      sessionId: 'rtc-managed-1',
      machineId: 'machine-1',
    }])
    expect(session.acceptedAnswers).toEqual([{ type: 'answer', sdp: 'answer-after-pending' }])
    expect(statuses).toContain('Waiting for machine response (1/20)...')
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

  it('verifies agent answer proof without WebCrypto subtle support', async () => {
    vi.stubGlobal('crypto', {
      getRandomValues<T extends ArrayBufferView | null>(array: T): T {
        if (array instanceof Uint8Array) array.fill(1)
        return array
      },
    })
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

    expect(session.acceptedAnswers).toEqual([{ type: 'answer', sdp: 'answer-sdp' }])
  })

  it('matches the answer proof HMAC-SHA256 base64url test vector', async () => {
    vi.stubGlobal('crypto', {
      getRandomValues<T extends ArrayBufferView | null>(array: T): T {
        if (array instanceof Uint8Array) array.fill(1)
        return array
      },
    })
    const api = new MockManagedHubApi({
      answerProof: 'M9ll7Zw26PQINgjHyoQxowGFMRj_3MNOSeBPUdo6yI4',
    })
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
      async importKey(_format: KeyFormat, keyData: BufferSource) {
        return { keyData } as unknown as CryptoKey
      },
      async sign(_algorithm: AlgorithmIdentifier | HmacImportParams, key: CryptoKey, data: BufferSource) {
        const keyData = (key as unknown as { keyData: BufferSource }).keyData
        const imported = new Uint8Array(keyData as ArrayBuffer)
        const bytes = new Uint8Array(data as ArrayBuffer)
        return hmacSHA256ForTest(imported, bytes).buffer
      },
    } as unknown as SubtleCrypto,
  } as Crypto
}

function expectedAnswerProof(secret: string, pairSessionId: string, offerSessionId: string, challenge: string): string {
  const text = `termx-answer-proof-v1:${pairSessionId}:${offerSessionId}:${challenge}`
  return base64url(hmacSHA256ForTest(new TextEncoder().encode(secret), new TextEncoder().encode(text)))
}

function sessionTokenWithID(sessionId: string): string {
  return `${base64url(new TextEncoder().encode(JSON.stringify({ sid: sessionId })))}.mac`
}

function base64url(bytes: Uint8Array): string {
  let binary = ''
  for (const value of bytes) binary += String.fromCharCode(value)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function hmacSHA256ForTest(key: Uint8Array, data: Uint8Array): Uint8Array {
  const blockSize = 64
  let normalizedKey = key
  if (normalizedKey.length > blockSize) {
    normalizedKey = sha256ForTest(normalizedKey)
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
  return sha256ForTest(concatBytesForTest(outer, sha256ForTest(concatBytesForTest(inner, data))))
}

function sha256ForTest(input: Uint8Array): Uint8Array {
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
    for (let index = 0; index < 16; index += 1) words[index] = view.getUint32(offset + index * 4, false)
    for (let index = 16; index < 64; index += 1) {
      words[index] = add32ForTest(
        smallSigma1ForTest(words[index - 2] ?? 0),
        words[index - 7] ?? 0,
        smallSigma0ForTest(words[index - 15] ?? 0),
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
      const t1 = add32ForTest(h, bigSigma1ForTest(e), chooseForTest(e, f, g), sha256ConstantsForTest[index] ?? 0, words[index] ?? 0)
      const t2 = add32ForTest(bigSigma0ForTest(a), majorityForTest(a, b, c))
      h = g
      g = f
      f = e
      e = add32ForTest(d, t1)
      d = c
      c = b
      b = a
      a = add32ForTest(t1, t2)
    }
    h0 = add32ForTest(h0, a)
    h1 = add32ForTest(h1, b)
    h2 = add32ForTest(h2, c)
    h3 = add32ForTest(h3, d)
    h4 = add32ForTest(h4, e)
    h5 = add32ForTest(h5, f)
    h6 = add32ForTest(h6, g)
    h7 = add32ForTest(h7, h)
  }
  const out = new Uint8Array(32)
  const outView = new DataView(out.buffer)
  ;[h0, h1, h2, h3, h4, h5, h6, h7].forEach((value, index) => outView.setUint32(index * 4, value, false))
  return out
}

function concatBytesForTest(a: Uint8Array, b: Uint8Array): Uint8Array {
  const out = new Uint8Array(a.length + b.length)
  out.set(a)
  out.set(b, a.length)
  return out
}

function rotateRightForTest(value: number, bits: number): number {
  return (value >>> bits) | (value << (32 - bits))
}

function add32ForTest(...values: number[]): number {
  return values.reduce((sum, value) => (sum + value) >>> 0, 0)
}

function chooseForTest(x: number, y: number, z: number): number {
  return (x & y) ^ (~x & z)
}

function majorityForTest(x: number, y: number, z: number): number {
  return (x & y) ^ (x & z) ^ (y & z)
}

function bigSigma0ForTest(x: number): number {
  return rotateRightForTest(x, 2) ^ rotateRightForTest(x, 13) ^ rotateRightForTest(x, 22)
}

function bigSigma1ForTest(x: number): number {
  return rotateRightForTest(x, 6) ^ rotateRightForTest(x, 11) ^ rotateRightForTest(x, 25)
}

function smallSigma0ForTest(x: number): number {
  return rotateRightForTest(x, 7) ^ rotateRightForTest(x, 18) ^ (x >>> 3)
}

function smallSigma1ForTest(x: number): number {
  return rotateRightForTest(x, 17) ^ rotateRightForTest(x, 19) ^ (x >>> 10)
}

const sha256ConstantsForTest = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])
