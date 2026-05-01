import { describe, expect, it, vi } from 'vitest'
import { createLocalWebRtcPeerTransport } from './localWebRtcTransport'
import { TERMX_FRAME_TYPES, decodeTermxFrame, encodeTermxFrame } from './termxProtocol'

describe('createLocalWebRtcPeerTransport', () => {
  it('opens local terminal and api channels through browser WebRTC primitives', async () => {
    const factory = createMockPeerConnectionFactory()
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({
        sessionId: offer.sessionId,
        answer: { type: 'answer', sdp: 'answer-sdp' },
      }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })

    await transport.connect({ machineId: 'machine-local', terminalId: 'terminal-1', mode: 'local' })
    expect(factory.labelsAtCreateOffer()).toEqual(['terminal:terminal-1', 'api'])
    const terminalPromise = transport.openTerminal('terminal-1')
    const api = await transport.openApi()

    await expect(transport.getConnectionInfo()).resolves.toEqual({
      mode: 'local',
      connectionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      relayInUse: false,
    })
    expect(factory.createdLabels()).toEqual(['terminal:terminal-1', 'api'])
    const terminalChannel = factory.channel('terminal:terminal-1')
    const hello = decodeBinarySentFrame(terminalChannel, 0)
    expect(hello.type).toBe(TERMX_FRAME_TYPES.hello)
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attach = decodeBinarySentFrame(terminalChannel, 1)
    const attachRequest = JSON.parse(new TextDecoder().decode(attach.payload))
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))
    const terminal = await terminalPromise
    expect(terminal.label).toBe('terminal:terminal-1')
    expect((await transport.openTerminal('terminal-1')).label).toBe('terminal:terminal-1')
    expect(factory.createdLabels()).toEqual(['terminal:terminal-1', 'api'])

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

  it('rejects wrong terminal targets before creating data channels', async () => {
    const factory = createMockPeerConnectionFactory()
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({ sessionId: offer.sessionId, answer: { type: 'answer', sdp: 'answer-sdp' } }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })

    await expect(transport.connect({ machineId: 'machine-local', terminalId: 'terminal-2', mode: 'local' }))
      .rejects.toThrow(/terminal-2.*terminal-1/)
    await expect(transport.openTerminal('terminal-2')).rejects.toThrow(/terminal-2.*terminal-1/)
    expect(factory.createdLabels()).toEqual([])
  })

  it('routes Go binary protocol terminal output without exposing browser primitives to subscribers', async () => {
    const factory = createMockPeerConnectionFactory()
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({ sessionId: offer.sessionId, answer: { type: 'answer', sdp: 'answer-sdp' } }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })
    const events: unknown[] = []
    transport.subscribeTerminal('terminal-1', (event) => events.push(event))

    await transport.connect({ machineId: 'machine-local', terminalId: 'terminal-1', mode: 'local' })
    const terminalPromise = transport.openTerminal('terminal-1')
    const terminalChannel = factory.channel('terminal:terminal-1')
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeBinarySentFrame(terminalChannel, 1).payload))
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))
    const terminal = await terminalPromise
    terminalChannel.emitMessage(encodeTermxFrame(7, TERMX_FRAME_TYPES.output, new TextEncoder().encode('hello')))
    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'input', data: 'echo hi\n' })))

    expect(events).toHaveLength(1)
    expect(events[0]).toMatchObject({ type: 'output' })
    expect(Array.from((events[0] as { data: Uint8Array }).data)).toEqual(Array.from(new TextEncoder().encode('hello')))
    const inputFrame = decodeBinarySentFrame(terminalChannel, 3)
    expect(inputFrame).toMatchObject({ channel: 7, type: TERMX_FRAME_TYPES.input })
    expect(new TextDecoder().decode(inputFrame.payload)).toBe('echo hi\n')
    expect(JSON.stringify(events)).not.toMatch(/RTCPeerConnection|RTCDataChannel|nativePlugin|turn|credential/i)
  })

  it('forwards subscribers registered after the protocol bridge is created', async () => {
    const factory = createMockPeerConnectionFactory()
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({ sessionId: offer.sessionId, answer: { type: 'answer', sdp: 'answer-sdp' } }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })
    await transport.connect({ machineId: 'machine-local', terminalId: 'terminal-1', mode: 'local' })
    const terminalPromise = transport.openTerminal('terminal-1')
    const terminalChannel = factory.channel('terminal:terminal-1')
    const events: unknown[] = []
    transport.subscribeTerminal('terminal-1', (event) => events.push(event))

    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeBinarySentFrame(terminalChannel, 1).payload))
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))
    await terminalPromise
    terminalChannel.emitMessage(encodeTermxFrame(7, TERMX_FRAME_TYPES.output, new TextEncoder().encode('late-sub')))

    expect(new TextDecoder().decode((events[0] as { data: Uint8Array }).data)).toBe('late-sub')
  })

  it('forwards raw RTC terminal channel close as a terminal closed event', async () => {
    const factory = createMockPeerConnectionFactory()
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({ sessionId: offer.sessionId, answer: { type: 'answer', sdp: 'answer-sdp' } }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })
    const events: unknown[] = []
    transport.subscribeTerminal('terminal-1', (event) => events.push(event))
    await transport.connect({ machineId: 'machine-local', terminalId: 'terminal-1', mode: 'local' })
    const terminalPromise = transport.openTerminal('terminal-1')
    const terminalChannel = factory.channel('terminal:terminal-1')
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeBinarySentFrame(terminalChannel, 1).payload))
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))
    await terminalPromise

    terminalChannel.close()

    expect(events).toContainEqual({ type: 'closed' })
  })

  it('sets terminal channel binaryType and accepts browser Blob binary messages', async () => {
    const factory = createMockPeerConnectionFactory()
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({ sessionId: offer.sessionId, answer: { type: 'answer', sdp: 'answer-sdp' } }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })
    const events: unknown[] = []
    transport.subscribeTerminal('terminal-1', (event) => events.push(event))
    await transport.connect({ machineId: 'machine-local', terminalId: 'terminal-1', mode: 'local' })
    const terminalPromise = transport.openTerminal('terminal-1')
    const terminalChannel = factory.channel('terminal:terminal-1')

    expect(terminalChannel.binaryType).toBe('arraybuffer')
    terminalChannel.emitMessage(new Blob([
      blobPart(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' }))),
    ]))
    const attachRequest = JSON.parse(new TextDecoder().decode((await waitForBinarySentFrame(terminalChannel, 1)).payload))
    terminalChannel.emitMessage(new Blob([
      blobPart(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
        id: attachRequest.id,
        result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
      }))),
    ]))
    await terminalPromise
    terminalChannel.emitMessage(new Blob([
      blobPart(encodeTermxFrame(7, TERMX_FRAME_TYPES.output, new TextEncoder().encode('blob-output'))),
    ]))
    const output = await waitForTerminalEvent<{ data: Uint8Array }>(events)

    expect(new TextDecoder().decode(output.data)).toBe('blob-output')
  })

  it('waits for the terminal data channel to open before sending hello', async () => {
    const factory = createMockPeerConnectionFactory({ initialReadyState: 'connecting' })
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({ sessionId: offer.sessionId, answer: { type: 'answer', sdp: 'answer-sdp' } }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })
    await transport.connect({ machineId: 'machine-local', terminalId: 'terminal-1', mode: 'local' })
    const terminalPromise = transport.openTerminal('terminal-1')
    const terminalChannel = factory.channel('terminal:terminal-1')

    expect(terminalChannel.sent).toEqual([])
    terminalChannel.open()
    expect((await waitForBinarySentFrame(terminalChannel, 0)).type).toBe(TERMX_FRAME_TYPES.hello)
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeBinarySentFrame(terminalChannel, 1).payload))
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))

    await expect(terminalPromise).resolves.toMatchObject({ label: 'terminal:terminal-1' })
  })

  it('creates a fresh terminal channel after raw RTC close before retrying open', async () => {
    const factory = createMockPeerConnectionFactory()
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({ sessionId: offer.sessionId, answer: { type: 'answer', sdp: 'answer-sdp' } }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })
    await transport.connect({ machineId: 'machine-local', terminalId: 'terminal-1', mode: 'local' })
    const first = transport.openTerminal('terminal-1')
    const firstChannel = factory.channel('terminal:terminal-1')
    firstChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const firstAttach = JSON.parse(new TextDecoder().decode(decodeBinarySentFrame(firstChannel, 1).payload))
    firstChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: firstAttach.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))
    await first
    firstChannel.close()

    const second = transport.openTerminal('terminal-1')
    const secondChannel = factory.channel('terminal:terminal-1')
    expect(secondChannel).not.toBe(firstChannel)
    expect(decodeBinarySentFrame(secondChannel, 0).type).toBe(TERMX_FRAME_TYPES.hello)
    secondChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const secondAttach = JSON.parse(new TextDecoder().decode(decodeBinarySentFrame(secondChannel, 1).payload))
    secondChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: secondAttach.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 8 }),
    })))

    await expect(second).resolves.toMatchObject({ label: 'terminal:terminal-1' })
  })

  it('drops a pre-open raw closed terminal channel before opening terminal', async () => {
    const factory = createMockPeerConnectionFactory()
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({ sessionId: offer.sessionId, answer: { type: 'answer', sdp: 'answer-sdp' } }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })
    await transport.connect({ machineId: 'machine-local', terminalId: 'terminal-1', mode: 'local' })
    const preOpenChannel = factory.channel('terminal:terminal-1')
    preOpenChannel.close()

    const terminalPromise = transport.openTerminal('terminal-1')
    const terminalChannel = factory.channel('terminal:terminal-1')
    expect(terminalChannel).not.toBe(preOpenChannel)
    expect(decodeBinarySentFrame(terminalChannel, 0).type).toBe(TERMX_FRAME_TYPES.hello)
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeBinarySentFrame(terminalChannel, 1).payload))
    terminalChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))

    await expect(terminalPromise).resolves.toMatchObject({ label: 'terminal:terminal-1' })
  })

  it('creates a fresh terminal protocol after close and reopen', async () => {
    const factory = createMockPeerConnectionFactory()
    const transport = createLocalWebRtcPeerTransport({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      mode: 'local',
      peerConnectionFactory: factory,
      createAnswer: async (offer) => ({ sessionId: offer.sessionId, answer: { type: 'answer', sdp: 'answer-sdp' } }),
      sessionIdGenerator: () => 'rtc-local-1',
      appCertificate: '{"payload":{"machine_id":"machine-local"}}',
      signOffer: async () => ({ signature: 'signature', nonce: 'nonce-1', timestamp: '1770000000' }),
    })
    await transport.connect({ machineId: 'machine-local', terminalId: 'terminal-1', mode: 'local' })

    const first = transport.openTerminal('terminal-1')
    const firstChannel = factory.channel('terminal:terminal-1')
    firstChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const firstAttach = JSON.parse(new TextDecoder().decode(decodeBinarySentFrame(firstChannel, 1).payload))
    firstChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: firstAttach.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))
    await first
    transport.closeTerminalChannel('terminal-1')

    const second = transport.openTerminal('terminal-1')
    const secondChannel = factory.channel('terminal:terminal-1')
    expect(secondChannel).not.toBe(firstChannel)
    expect(decodeBinarySentFrame(secondChannel, 0).type).toBe(TERMX_FRAME_TYPES.hello)
    secondChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const secondAttach = JSON.parse(new TextDecoder().decode(decodeBinarySentFrame(secondChannel, 1).payload))
    secondChannel.emitMessage(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: secondAttach.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 8 }),
    })))
    const reopened = await second
    expect(reopened.label).toBe('terminal:terminal-1')
  })
})

function encodeJSON(value: unknown): Uint8Array {
  return new TextEncoder().encode(JSON.stringify(value))
}

function blobPart(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
}

function decodeBinarySentFrame(channel: MockRTCDataChannel, index: number) {
  const data = channel.sent[index]
  if (data === undefined || typeof data === 'string') {
    throw new Error(`missing binary sent frame ${index}`)
  }
  return decodeTermxFrame(data instanceof Uint8Array ? data : new Uint8Array(data as ArrayBuffer))
}

async function waitForBinarySentFrame(channel: MockRTCDataChannel, index: number) {
  const started = Date.now()
  for (;;) {
    const data = channel.sent[index]
    if (data !== undefined && typeof data !== 'string') {
      return decodeTermxFrame(data instanceof Uint8Array ? data : new Uint8Array(data as ArrayBuffer))
    }
    if (Date.now() - started > 1000) {
      throw new Error(`missing binary sent frame ${index}`)
    }
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
}

async function waitForTerminalEvent<TEvent>(events: unknown[], index = 0): Promise<TEvent> {
  const started = Date.now()
  for (;;) {
    if (events[index] !== undefined) return events[index] as TEvent
    if (Date.now() - started > 1000) {
      throw new Error(`missing terminal event ${index}`)
    }
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
}

function apiResponseChunk(id: string, payload: unknown): Uint8Array {
  const idBytes = new TextEncoder().encode(id)
  const body = new TextEncoder().encode(JSON.stringify(payload))
  const out = new Uint8Array(3 + idBytes.length + body.length)
  out[0] = 0xc0
  out[1] = 0x01 | 0x02
  out[2] = idBytes.length
  out.set(idBytes, 3)
  out.set(body, 3 + idBytes.length)
  return out
}

function createMockPeerConnectionFactory(options: { initialReadyState?: RTCDataChannelState } = {}) {
  const channels = new Map<string, MockRTCDataChannel>()
  const createOfferLabels: string[][] = []
  const factory = vi.fn(() => new MockRTCPeerConnection(channels, createOfferLabels, options.initialReadyState ?? 'open'))
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
  })
}

class MockRTCPeerConnection {
  localDescription: RTCSessionDescriptionInit | null = null

  constructor(
    private readonly channels: Map<string, MockRTCDataChannel>,
    private readonly createOfferLabels: string[][],
    private readonly initialReadyState: RTCDataChannelState,
  ) {}

  createDataChannel(label: string): MockRTCDataChannel {
    const channel = new MockRTCDataChannel(label, this.initialReadyState)
    this.channels.set(label, channel)
    return channel
  }

  async createOffer(): Promise<RTCSessionDescriptionInit> {
    this.createOfferLabels.push(Array.from(this.channels.keys()))
    return { type: 'offer', sdp: 'offer-sdp' }
  }

  async setLocalDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.localDescription = description
  }

  async setRemoteDescription(): Promise<void> {}

  async close(): Promise<void> {}
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

  emitMessage(data: string | Uint8Array | Blob): void {
    this.dispatchEvent(new MessageEvent('message', { data }))
  }

  sentText(): string[] {
    return this.sent.map((item) => typeof item === 'string' ? item : new TextDecoder().decode(item as ArrayBuffer))
  }
}
