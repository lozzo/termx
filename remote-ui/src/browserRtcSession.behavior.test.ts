import { describe, expect, it, vi } from 'vitest'
import { createBrowserRtcSession } from './browserRtcSession'
import { TERMX_FRAME_TYPES, encodeTermxFrame } from './termxProtocol'

describe('BrowserRtcSession', () => {
  it('creates raw terminal and api DataChannels during browser WebRTC negotiation', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })

    const offer = await session.createOffer({ machineId: 'machine-local', terminalId: 'terminal-1', path: 'local' })
    await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
    const terminal = await session.openTerminal('terminal-1')
    const api = await session.openApi()

    expect(offer).toEqual({
      sessionId: 'rtc-local-1',
      description: { type: 'offer', sdp: 'offer-sdp' },
    })
    expect(factory.labelsAtCreateOffer()).toEqual(['terminal:terminal-1', 'api'])
    expect(factory.lastConnection()?.remoteDescription).toEqual({ type: 'answer', sdp: 'answer-sdp' })
    expect(terminal.label).toBe('terminal:terminal-1')
    expect(factory.channel('terminal:terminal-1').binaryType).toBe('arraybuffer')
    await expect(session.getConnectionInfo()).resolves.toEqual({
      path: 'local',
      connectionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      relayInUse: false,
    })

    const apiChannel = factory.channel('api')
    const apiPromise = api.request('POST', { path: '/files/list', params: { path: '/' } })
    apiChannel.emitMessage(apiResponseChunk('req_1', {
      id: 'req_1',
      status: 200,
      body: { path: '/', parent: '', total: 0, entries: [] },
    }))

    await expect(apiPromise).resolves.toEqual({ path: '/', parent: '', total: 0, entries: [] })
    expect(JSON.parse(apiChannel.sentText()[0] ?? '{}')).toEqual({
      id: 'req_1',
      method: 'POST',
      path: '/files/list',
      body: { path: '/' },
    })
  })

  it('sends direct runtime api protocol methods without requiring file-style path params', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })
    await connectBrowserSession(session)
    const api = await session.openApi()
    const apiChannel = factory.channel('api')

    const request = api.request('create', {
      command: ['/bin/zsh', '-l'],
      name: 'ops shell',
    })
    apiChannel.emitMessage(apiResponseChunk('req_1', {
      id: 'req_1',
      status: 200,
      body: { terminal_id: 'terminal-3' },
    }))

    await expect(request).resolves.toEqual({ terminal_id: 'terminal-3' })
    expect(JSON.parse(apiChannel.sentText()[0] ?? '{}')).toEqual({
      id: 'req_1',
      method: 'create',
      path: 'create',
      body: {
        command: ['/bin/zsh', '-l'],
        name: 'ops shell',
      },
    })
  })

  it('reports the negotiated connection path from createOffer even when options do not duplicate it', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-remote',
      terminalId: 'terminal-1',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-p2p-1',
    })

    await session.createOffer({ machineId: 'machine-remote', terminalId: 'terminal-1', path: 'public_p2p' })

    await expect(session.getConnectionInfo()).resolves.toMatchObject({
      path: 'public_p2p',
      connectionId: 'rtc-p2p-1',
    })
  })

  it('passes signaling ICE server configuration into the browser peer connection', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-remote',
      terminalId: 'terminal-1',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-public-1',
    })

    await session.createOffer({
      machineId: 'machine-remote',
      terminalId: 'terminal-1',
      path: 'public_p2p',
      iceServers: [{ urls: ['stun:one.example:3478'] }],
    })

    expect(factory.lastConnection()?.configuration).toEqual({
      iceServers: [{ urls: ['stun:one.example:3478'] }],
    })
  })

  it('reports server-negotiated channel capabilities instead of hard-coded browser defaults', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-api',
    })
    await session.createOffer({ machineId: 'machine-local', path: 'local' })
    session.updateConnectionCapabilities({
      terminalAllowed: false,
      apiAllowed: true,
      eventsAllowed: false,
      fileTransferAllowed: false,
      terminalManagementAllowed: true,
      relayInUse: false,
    })

    await expect(session.getCapabilities()).resolves.toEqual({
      terminalAllowed: false,
      apiAllowed: true,
      eventsAllowed: false,
      fileTransferAllowed: false,
      terminalManagementAllowed: true,
      relayInUse: false,
    })
  })

  it('keeps one machine-scoped browser session alive for api, multiple terminals, and files', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      path: 'managed',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-machine-1',
      heartbeatIntervalMs: 60000,
    })

    await session.createOffer({ machineId: 'machine-local', path: 'managed' })
    await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
    const terminalOne = await session.openTerminal('terminal-1')
    const terminalTwo = await session.openTerminal('terminal-2')
    const file = await session.openFileTransfer('transfer-1')
    const api = await session.openApi()

    expect(factory.labelsAtCreateOffer()).toEqual(['api'])
    expect(factory.createdLabels()).toEqual(['api', 'terminal:terminal-1', 'terminal:terminal-2', 'file:transfer-1'])
    expect(terminalOne.label).toBe('terminal:terminal-1')
    expect(terminalTwo.label).toBe('terminal:terminal-2')
    expect(file.label).toBe('file:transfer-1')
    await expect(session.getConnectionInfo()).resolves.toEqual({
      path: 'managed',
      connectionId: 'rtc-machine-1',
      machineId: 'machine-local',
      relayInUse: false,
    })

    const apiChannel = factory.channel('api')
    const request = api.request('list', {})
    apiChannel.emitMessage(apiResponseChunk('req_1', {
      id: 'req_1',
      status: 200,
      body: { terminals: [] },
    }))
    await expect(request).resolves.toEqual({ terminals: [] })
  })

  it('runs runtime ping when peer connection state reaches connected', async () => {
    vi.useFakeTimers()
    try {
      const factory = createMockPeerConnectionFactory()
      const session = createBrowserRtcSession({
        machineId: 'machine-local',
        path: 'managed',
        peerConnectionFactory: factory,
        sessionIdGenerator: () => 'rtc-machine-1',
        heartbeatIntervalMs: 20,
      })
      await session.createOffer({ machineId: 'machine-local', path: 'managed' })
      await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
      factory.lastConnection()?.setConnectionState('connected')
      vi.advanceTimersByTime(20)
      await flushMicrotasks()

      const apiChannel = factory.channel('api')
      const heartbeatRequest = JSON.parse(apiChannel.sentText().at(-1) ?? '{}')
      expect(heartbeatRequest).toMatchObject({
        id: 'req_1',
        method: 'ping',
        path: 'ping',
        body: {},
      })
      apiChannel.emitMessage(apiResponseChunk('req_1', {
        id: 'req_1',
        status: 200,
        body: { ok: true },
      }))
      await flushMicrotasks()
    } finally {
      vi.useRealTimers()
    }
  })

  it('notifies disconnect handlers when the api channel closes unexpectedly', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      path: 'managed',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-machine-1',
    })
    const onDisconnect = vi.fn()
    session.onDisconnect(onDisconnect)

    await session.createOffer({ machineId: 'machine-local', path: 'managed' })
    await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
    factory.channel('api').close()
    await flushMicrotasks()

    expect(onDisconnect).toHaveBeenCalledTimes(1)
    expect(session.isAlive()).toBe(false)
  })

  it('answers managed hub offers with TURN config while expressing relay as connection capability only', async () => {
    const factory = createMockPeerConnectionFactory({
      initialIceGatheringState: 'gathering',
      gatheredLocalSDP: 'managed-answer-sdp-with-candidates',
    })
    const session = createBrowserRtcSession({
      machineId: 'machine-managed',
      peerConnectionFactory: factory,
    })

    const answerPromise = session.acceptOffer({
      sessionId: 'managed-rtc-1',
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      path: 'managed',
      description: { type: 'offer', sdp: 'managed-offer-sdp' },
      iceServers: [{ urls: ['turn:turn.example:3478'], username: 'u', credential: 'p' }],
      relayPolicy: { allowRelay: true, allowRelayTransfer: false },
      relayInUse: true,
    })
    await flushMicrotasks()
    factory.lastConnection()?.completeIceGathering()

    await expect(answerPromise).resolves.toEqual({
      sessionId: 'managed-rtc-1',
      description: { type: 'answer', sdp: 'managed-answer-sdp-with-candidates' },
    })
    expect(factory.lastConnection()?.remoteDescription).toEqual({ type: 'offer', sdp: 'managed-offer-sdp' })
    expect(factory.lastConnection()?.configuration).toEqual({
      iceServers: [{ urls: ['turn:turn.example:3478'], username: 'u', credential: 'p' }],
    })
    await expect(session.getConnectionInfo()).resolves.toEqual({
      path: 'managed',
      connectionId: 'managed-rtc-1',
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      relayInUse: true,
    })
    await expect(session.getCapabilities()).resolves.toMatchObject({
      relayInUse: true,
      fileTransferAllowed: false,
    })
    expect(factory.createdLabels()).toEqual([])
  })

  it('uses incoming managed DataChannels instead of creating browser-local runtime channels', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-managed',
      peerConnectionFactory: factory,
    })
    await session.acceptOffer({
      sessionId: 'managed-rtc-1',
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      path: 'managed',
      description: { type: 'offer', sdp: 'managed-offer-sdp' },
      iceServers: [],
      relayPolicy: { allowRelay: true, allowRelayTransfer: true },
      relayInUse: false,
    })

    const terminalPromise = session.openTerminal('terminal-1')
    await flushMicrotasks()
    expect(factory.createdLabels()).toEqual([])
    factory.lastConnection()?.emitIncomingDataChannel('terminal:terminal-1')
    const terminal = await terminalPromise

    expect(terminal.label).toBe('terminal:terminal-1')
    expect(factory.channel('terminal:terminal-1').binaryType).toBe('arraybuffer')
  })

  it('does not infer incoming channel ownership from managed path when the browser created the offer', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-managed',
      terminalId: 'terminal-1',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'managed-offerer-1',
    })

    await session.createOffer({ machineId: 'machine-managed', terminalId: 'terminal-1', path: 'managed' })
    await session.acceptAnswer({ type: 'answer', sdp: 'managed-answer-sdp' })
    const terminal = await session.openTerminal('terminal-1')

    expect(terminal.label).toBe('terminal:terminal-1')
    expect(factory.createdLabels()).toEqual(['terminal:terminal-1', 'api'])
  })

  it('subscribes to runtime events through the unified session events DataChannel', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })
    await connectBrowserSession(session)
    const events: unknown[] = []

    const subscription = session.subscribeEvents((event) => events.push(event))
    const eventsChannel = factory.channel('events')
    await flushMicrotasks()
    expect(JSON.parse(eventsChannel.sentText()[0] ?? '{}')).toEqual({
      type: 'subscribe',
      types: [1, 2, 3, 4, 10],
    })
    eventsChannel.emitMessage(encodeJSON({
      type: 'terminal_changed',
      payload: { terminal_id: 'terminal-1' },
    }))

    expect(events).toEqual([{
      type: 'terminal_changed',
      payload: { terminal_id: 'terminal-1' },
    }])
    subscription.close()
    eventsChannel.emitMessage(encodeJSON({ type: 'ignored_after_unsubscribe' }))
    expect(events).toHaveLength(1)
  })

  it('subscribes to managed answerer events from incoming events DataChannel', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-managed',
      peerConnectionFactory: factory,
    })
    await session.acceptOffer({
      sessionId: 'managed-rtc-1',
      machineId: 'machine-managed',
      path: 'managed',
      description: { type: 'offer', sdp: 'managed-offer-sdp' },
      iceServers: [],
      relayPolicy: { allowRelay: true, allowRelayTransfer: true },
      relayInUse: false,
    })
    const events: unknown[] = []

    const subscription = session.subscribeEvents((event) => events.push(event))
    await flushMicrotasks()
    expect(factory.createdLabels()).toEqual([])
    const eventsChannel = factory.lastConnection()?.emitIncomingDataChannel('events')
    await flushMicrotasks()
    expect(JSON.parse(eventsChannel?.sentText()[0] ?? '{}')).toEqual({
      type: 'subscribe',
      types: [1, 2, 3, 4, 10],
    })
    eventsChannel?.emitMessage(encodeJSON({ type: 'inventory_changed' }))

    expect(events).toEqual([{ type: 'inventory_changed' }])
    subscription.close()
  })

  it('rejects file transfer opening when managed relay policy denies it', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-managed',
      peerConnectionFactory: factory,
    })
    await session.acceptOffer({
      sessionId: 'managed-rtc-1',
      machineId: 'machine-managed',
      path: 'managed',
      description: { type: 'offer', sdp: 'managed-offer-sdp' },
      iceServers: [],
      relayPolicy: { allowRelay: true, allowRelayTransfer: false },
      relayInUse: true,
    })

    await expect(session.openFileTransfer('upload-1')).rejects.toThrow(/file transfer|relay policy/i)
    expect(factory.createdLabels()).toEqual([])
  })

  it('rejects wrong terminal targets before creating browser data channels', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })

    await expect(session.createOffer({ machineId: 'machine-local', terminalId: 'terminal-2', path: 'local' }))
      .rejects.toThrow(/terminal-2.*terminal-1/)
    await expect(session.openTerminal('terminal-2')).rejects.toThrow(/terminal-2.*terminal-1/)
    expect(factory.createdLabels()).toEqual([])
  })

  it('passes raw binary terminal frames without implementing the terminal protocol itself', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })
    await connectBrowserSession(session)
    const terminal = await session.openTerminal('terminal-1')
    const terminalChannel = factory.channel('terminal:terminal-1')
    const received: Uint8Array[] = []
    terminal.onMessage?.((data) => received.push(data))

    const output = encodeTermxFrame(7, TERMX_FRAME_TYPES.output, new TextEncoder().encode('hello'))
    terminalChannel.emitMessage(output)
    terminal.send(new Uint8Array([1, 2, 3]))

    expect(received).toEqual([output])
    expect(terminalChannel.sent).toEqual([new Uint8Array([1, 2, 3])])
  })

  it('waits for the api data channel to open before accepting the answer', async () => {
    const factory = createMockPeerConnectionFactory({ initialReadyState: 'connecting' })
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })
    await session.createOffer({ machineId: 'machine-local', terminalId: 'terminal-1', path: 'local' })
    let accepted = false
    const acceptedPromise = session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' }).then(() => {
      accepted = true
    })
    await flushMicrotasks()

    expect(accepted).toBe(false)
    factory.channel('api').open()
    await acceptedPromise
    expect(accepted).toBe(true)
  })

  it('waits for browser ICE gathering before returning the offer', async () => {
    const factory = createMockPeerConnectionFactory({
      initialIceGatheringState: 'gathering',
      gatheredLocalSDP: 'offer-sdp-with-local-candidates',
    })
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })
    const offerPromise = session.createOffer({ machineId: 'machine-local', terminalId: 'terminal-1', path: 'local' })
    await flushMicrotasks()

    factory.lastConnection()?.completeIceGathering()

    await expect(offerPromise).resolves.toMatchObject({
      description: { sdp: 'offer-sdp-with-local-candidates' },
    })
  })

  it('rejects pending api requests when the data channel closes without a response', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })
    await connectBrowserSession(session)
    const api = await session.openApi()
    const apiChannel = factory.channel('api')

    const request = api.request('POST', { path: '/files/list', params: { path: '/' } })
    apiChannel.close()

    await expect(request).rejects.toThrow(/api.*closed|closed.*api/i)
  })

  it('rejects pending api requests when the response chunk is invalid', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })
    await connectBrowserSession(session)
    const api = await session.openApi()
    const apiChannel = factory.channel('api')

    const request = api.request('POST', { path: '/files/list', params: { path: '/' } })
    apiChannel.emitMessage(new Uint8Array([0x01, 0x02, 0x03]))

    await expect(request).rejects.toThrow(/invalid api response chunk/i)
  })

  it('waits for file transfer data channels to open before resolving them', async () => {
    const factory = createMockPeerConnectionFactory({ initialReadyState: 'connecting' })
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })
    const offer = session.createOffer({ machineId: 'machine-local', terminalId: 'terminal-1', path: 'local' })
    await flushMicrotasks()
    factory.channel('api').open()
    await offer
    await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
    let resolved = false
    const fileChannelPromise = session.openFileTransfer('upload-1').then((channel) => {
      resolved = true
      return channel
    })
    const rtcChannel = factory.channel('file:upload-1')
    await Promise.resolve()

    expect(resolved).toBe(false)
    rtcChannel.open()
    const fileChannel = await fileChannelPromise
    fileChannel.send(new Uint8Array([1, 2, 3]))

    expect(fileChannel.label).toBe('file:upload-1')
    expect(rtcChannel.sent[0]).toEqual(new Uint8Array([1, 2, 3]))
  })

  it('subscribes to runtime events over a dedicated events data channel', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-events-1',
    })
    const events: unknown[] = []

    const offer = session.createOffer({ machineId: 'machine-local', path: 'local' })
    await flushMicrotasks()
    factory.channel('api').open()
    await offer
    await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
    const subscription = session.subscribeEvents((event) => events.push(event))

    await flushMicrotasks()
    expect(factory.labelsAtCreateOffer()).toEqual(['api'])
    expect(factory.createdLabels()).toContain('events')
    const eventsChannel = factory.channel('events')
    const subscribeRequest = JSON.parse(eventsChannel.sentText()[0] ?? '{}')
    expect(subscribeRequest).toEqual({
      type: 'subscribe',
      types: [1, 2, 3, 4, 10],
    })
    eventsChannel.emitMessage(encodeJSON({
      type: 'inventory_changed',
      payload: { terminalId: 'terminal-2' },
    }))

    expect(events).toContainEqual({ type: 'inventory_changed', payload: { terminalId: 'terminal-2' } })
    subscription.close()
  })
})

async function connectBrowserSession(session: ReturnType<typeof createBrowserRtcSession>): Promise<void> {
  await session.createOffer({ machineId: 'machine-local', terminalId: 'terminal-1', path: 'local' })
  await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
}

function encodeJSON(value: unknown): Uint8Array {
  return new TextEncoder().encode(JSON.stringify(value))
}

function apiResponseChunk(id: string, payload: unknown, options: { last?: boolean } = {}): Uint8Array {
  const idBytes = new TextEncoder().encode(id)
  const body = new TextEncoder().encode(JSON.stringify(payload))
  const out = new Uint8Array(3 + idBytes.length + body.length)
  out[0] = 0xc0
  out[1] = 0x01 | (options.last === false ? 0 : 0x02)
  out[2] = idBytes.length
  out.set(idBytes, 3)
  out.set(body, 3 + idBytes.length)
  return out
}

async function flushMicrotasks(): Promise<void> {
  for (let i = 0; i < 10; i++) await Promise.resolve()
}

function createMockPeerConnectionFactory(options: {
  initialReadyState?: RTCDataChannelState
  initialIceGatheringState?: RTCIceGatheringState
  gatheredLocalSDP?: string
} = {}) {
  const channels = new Map<string, MockRTCDataChannel>()
  const createOfferLabels: string[][] = []
  let lastConnection: MockRTCPeerConnection | null = null
  const factory = vi.fn((configuration?: RTCConfiguration) => {
    lastConnection = new MockRTCPeerConnection({
      configuration,
      channels,
      createOfferLabels,
      initialReadyState: options.initialReadyState ?? 'open',
      initialIceGatheringState: options.initialIceGatheringState ?? 'complete',
      gatheredLocalSDP: options.gatheredLocalSDP,
    })
    return lastConnection
  })
  return Object.assign(factory, {
    channel(label: string) {
      const channel = channels.get(label)
      if (!channel) throw new Error(`missing channel ${label}`)
      return channel
    },
    createdLabels() {
      return Array.from(channels.keys())
    },
    labelsAtCreateOffer() {
      return createOfferLabels.at(-1) ?? []
    },
    lastConnection() {
      return lastConnection
    },
  })
}

class MockRTCPeerConnection extends EventTarget {
  localDescription: RTCSessionDescriptionInit | null = null
  remoteDescription: RTCSessionDescriptionInit | null = null
  iceGatheringState: RTCIceGatheringState
  connectionState: RTCPeerConnectionState = 'new'
  closed = false
  private readonly channels: Map<string, MockRTCDataChannel>
  private readonly createOfferLabels: string[][]
  private readonly initialReadyState: RTCDataChannelState
  private readonly gatheredLocalSDP: string | undefined

  constructor(options: {
    configuration?: RTCConfiguration | undefined
    channels: Map<string, MockRTCDataChannel>
    createOfferLabels: string[][]
    initialReadyState: RTCDataChannelState
    initialIceGatheringState: RTCIceGatheringState
    gatheredLocalSDP?: string | undefined
  }) {
    super()
    this.configuration = options.configuration
    this.channels = options.channels
    this.createOfferLabels = options.createOfferLabels
    this.initialReadyState = options.initialReadyState
    this.iceGatheringState = options.initialIceGatheringState
    this.gatheredLocalSDP = options.gatheredLocalSDP
  }

  readonly configuration: RTCConfiguration | undefined

  createDataChannel(label: string): MockRTCDataChannel {
    const channel = new MockRTCDataChannel(label, this.initialReadyState)
    this.channels.set(label, channel)
    return channel
  }

  async createOffer(): Promise<RTCSessionDescriptionInit> {
    this.createOfferLabels.push(Array.from(this.channels.keys()))
    return { type: 'offer', sdp: 'offer-sdp' }
  }

  async createAnswer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'answer', sdp: 'answer-sdp' }
  }

  async setLocalDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.localDescription = description
  }

  async setRemoteDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.remoteDescription = description
  }

  async close(): Promise<void> {
    this.closed = true
    this.connectionState = 'closed'
    this.dispatchEvent(new Event('connectionstatechange'))
  }

  completeIceGathering(): void {
    this.iceGatheringState = 'complete'
    if (this.localDescription && this.gatheredLocalSDP) {
      this.localDescription = {
        ...this.localDescription,
        sdp: this.gatheredLocalSDP,
      }
    }
    this.dispatchEvent(new Event('icegatheringstatechange'))
  }

  emitIncomingDataChannel(label: string): MockRTCDataChannel {
    const channel = new MockRTCDataChannel(label, this.initialReadyState)
    this.channels.set(label, channel)
    const event = new Event('datachannel') as Event & { channel: MockRTCDataChannel }
    event.channel = channel
    this.dispatchEvent(event)
    return channel
  }

  setConnectionState(state: RTCPeerConnectionState): void {
    this.connectionState = state
    this.dispatchEvent(new Event('connectionstatechange'))
  }
}

class MockRTCDataChannel extends EventTarget {
  readyState: RTCDataChannelState
  binaryType: BinaryType = 'blob'
  readonly sent: unknown[] = []

  constructor(readonly label: string, initialReadyState: RTCDataChannelState) {
    super()
    this.readyState = initialReadyState
  }

  send(data: string | ArrayBuffer | Blob | ArrayBufferView): void {
    if (this.readyState !== 'open') throw new Error(`data channel ${this.label} is not open`)
    this.sent.push(data)
  }

  close(): void {
    this.readyState = 'closed'
    this.dispatchEvent(new Event('close'))
  }

  open(): void {
    this.readyState = 'open'
    this.dispatchEvent(new Event('open'))
  }

  emitMessage(data: unknown): void {
    this.dispatchEvent(new MessageEvent('message', { data }))
  }

  sentText(): string[] {
    return this.sent.map((item) => typeof item === 'string' ? item : new TextDecoder().decode(item as ArrayBuffer))
  }
}
