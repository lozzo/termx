import { describe, expect, it, vi } from 'vitest'
import { createLocalWebRtcPeerTransport } from './localWebRtcTransport'

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
    const terminal = await transport.openTerminal('terminal-1')
    const api = await transport.openApi()

    await expect(transport.getConnectionInfo()).resolves.toEqual({
      mode: 'local',
      connectionId: 'rtc-local-1',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      relayInUse: false,
    })
    expect(terminal.label).toBe('terminal:terminal-1')
    expect(factory.createdLabels()).toEqual(['terminal:terminal-1', 'api'])
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

  it('routes terminal output subscriptions without exposing browser primitives to subscribers', async () => {
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
    await transport.openTerminal('terminal-1')
    factory.channel('terminal:terminal-1').emitMessage(new TextEncoder().encode('hello'))

    expect(events).toHaveLength(1)
    expect(events[0]).toMatchObject({ type: 'output' })
    expect(Array.from((events[0] as { data: Uint8Array }).data)).toEqual(Array.from(new TextEncoder().encode('hello')))
    expect(JSON.stringify(events)).not.toMatch(/RTCPeerConnection|RTCDataChannel|nativePlugin|turn|credential/i)
  })
})

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

function createMockPeerConnectionFactory() {
  const channels = new Map<string, MockRTCDataChannel>()
  const factory = vi.fn(() => new MockRTCPeerConnection(channels))
  return Object.assign(factory, {
    channel(label: string) {
      const channel = channels.get(label)
      if (!channel) throw new Error(`missing channel ${label}`)
      return channel
    },
    createdLabels() {
      return Array.from(channels.keys())
    },
  })
}

class MockRTCPeerConnection {
  localDescription: RTCSessionDescriptionInit | null = null

  constructor(private readonly channels: Map<string, MockRTCDataChannel>) {}

  createDataChannel(label: string): MockRTCDataChannel {
    const channel = new MockRTCDataChannel(label)
    this.channels.set(label, channel)
    return channel
  }

  async createOffer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'offer', sdp: 'offer-sdp' }
  }

  async setLocalDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.localDescription = description
  }

  async setRemoteDescription(): Promise<void> {}

  async close(): Promise<void> {}
}

class MockRTCDataChannel extends EventTarget {
  readyState: RTCDataChannelState = 'open'
  readonly sent: unknown[] = []

  constructor(readonly label: string) {
    super()
  }

  send(data: string | ArrayBuffer | Blob | ArrayBufferView): void {
    this.sent.push(data)
  }

  close(): void {
    this.readyState = 'closed'
    this.dispatchEvent(new Event('close'))
  }

  emitMessage(data: string | Uint8Array): void {
    this.dispatchEvent(new MessageEvent('message', { data }))
  }

  sentText(): string[] {
    return this.sent.map((item) => typeof item === 'string' ? item : new TextDecoder().decode(item as ArrayBuffer))
  }
}
