import { afterEach, describe, expect, it, vi } from 'vitest'
import { createBrowserRtcSession } from './browserRtcSession'
import type { ConnectionLogEvent } from '../connection/connectionLogger'
import { TERMX_FRAME_TYPES, encodeTermxFrame } from '../terminal/termxProtocol'

describe('BrowserRtcSession', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

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
      type: 'unknown',
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

  it('publishes browser transport connection state during negotiation and peer reconnects', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-state-1',
      heartbeatIntervalMs: 60000,
    })
    const states: Array<{ phase: string; statusText: string; path?: string }> = []
    const subscription = session.subscribeConnectionState((snapshot) => {
      const state: { phase: string; statusText: string; path?: string } = {
        phase: snapshot.phase,
        statusText: snapshot.statusText,
      }
      if (snapshot.path) state.path = snapshot.path
      states.push(state)
    })

    await connectBrowserSession(session)
    factory.lastConnection()?.setConnectionState('disconnected')
    await flushMicrotasks()

    expect(states).toEqual(expect.arrayContaining([
      { phase: 'probing', statusText: 'Creating WebRTC session...', path: 'local' },
      { phase: 'probing', statusText: 'Opening browser data channels...', path: 'local' },
      { phase: 'connecting', statusText: 'Applying WebRTC answer...', path: 'local' },
      { phase: 'connected', statusText: 'Connected', path: 'local' },
      { phase: 'reconnecting', statusText: 'WebRTC disconnected, probing connection...', path: 'local' },
    ]))
    subscription.close()
    await session.disconnect()
  })

  it('emits browser transport state through connect options during direct negotiation', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-options-state-1',
      heartbeatIntervalMs: 60000,
    })
    const states: string[] = []
    const options = {
      onConnectionState: (snapshot: { statusText: string }) => states.push(snapshot.statusText),
    }

    await session.createOffer({ machineId: 'machine-local', terminalId: 'terminal-1', path: 'local' }, options)
    await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' }, options)

    expect(states).toEqual(expect.arrayContaining([
      'Creating WebRTC session...',
      'Opening browser data channels...',
      'Gathering ICE candidates...',
      'Applying WebRTC answer...',
      'Opening data channels...',
      'Connected',
    ]))
    await session.disconnect()
  })

  it('falls back to getRandomValues when crypto.randomUUID is unavailable', async () => {
    vi.stubGlobal('crypto', {
      getRandomValues(bytes: Uint8Array) {
        bytes.set(Array.from({ length: bytes.length }, (_, index) => index + 1))
        return bytes
      },
    })
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-remote',
      terminalId: 'terminal-1',
      peerConnectionFactory: factory,
    })

    const offer = await session.createOffer({ machineId: 'machine-remote', terminalId: 'terminal-1', path: 'managed' })

    expect(offer.sessionId).toMatch(/^rtc_/)
    expect(offer.sessionId).not.toContain('undefined')
    await expect(session.getConnectionInfo()).resolves.toMatchObject({
      connectionId: offer.sessionId,
      path: 'managed',
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

  it('logs browser WebRTC negotiation, ICE, answer, and data channel stages', async () => {
    const factory = createMockPeerConnectionFactory()
    const logs: ConnectionLogEvent[] = []
    const session = createBrowserRtcSession({
      machineId: 'machine-remote',
      terminalId: 'terminal-1',
      path: 'managed',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-managed-logs',
      logger: { log: (event) => logs.push(event) },
    })

    await session.createOffer({
      machineId: 'machine-remote',
      terminalId: 'terminal-1',
      path: 'managed',
      iceServers: [{ urls: ['stun:one.example:3478'] }],
    })
    await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
    factory.lastConnection()?.setConnectionState('connected')

    expect(logs).toEqual(expect.arrayContaining([
      expect.objectContaining({ scope: 'browser_webrtc', event: 'offer_start', path: 'managed' }),
      expect.objectContaining({ scope: 'browser_webrtc', event: 'local_offer_created', path: 'managed', sessionId: 'rtc-managed-logs' }),
      expect.objectContaining({ scope: 'browser_webrtc', event: 'ice_gathering_complete', path: 'managed', sessionId: 'rtc-managed-logs' }),
      expect.objectContaining({ scope: 'browser_webrtc', event: 'answer_accept_start', path: 'managed', sessionId: 'rtc-managed-logs' }),
      expect.objectContaining({ scope: 'browser_webrtc', event: 'api_channel_open', path: 'managed', sessionId: 'rtc-managed-logs' }),
      expect.objectContaining({ scope: 'browser_webrtc', event: 'peer_connection_state', path: 'managed', sessionId: 'rtc-managed-logs', details: { state: 'connected' } }),
    ]))
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
      type: 'unknown',
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

  it('opens a fresh terminal data channel when the cached one is already closing', async () => {
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
    await session.openTerminal('terminal-1')
    const firstRawChannel = factory.channel('terminal:terminal-1')
    firstRawChannel.readyState = 'closing'
    const second = await session.openTerminal('terminal-1')

    expect(second.label).toBe('terminal:terminal-1')
    expect(factory.channel('terminal:terminal-1')).not.toBe(firstRawChannel)
  })

  it('does not let a delayed old terminal close event remove a fresh terminal data channel', async () => {
    vi.useFakeTimers()
    try {
      const factory = createMockPeerConnectionFactory({ closeEventDelayMs: 25 })
      const session = createBrowserRtcSession({
        machineId: 'machine-local',
        path: 'managed',
        peerConnectionFactory: factory,
        sessionIdGenerator: () => 'rtc-machine-1',
        heartbeatIntervalMs: 60000,
      })

      await session.createOffer({ machineId: 'machine-local', path: 'managed' })
      await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
      const first = await session.openTerminal('terminal-1')
      const firstRawChannel = factory.channel('terminal:terminal-1')

      session.closeTerminalDataChannel('terminal-1')
      const second = await session.openTerminal('terminal-1')
      const secondRawChannel = factory.channel('terminal:terminal-1')
      vi.advanceTimersByTime(25)

      expect(first.readyState).toBe('closed')
      expect(second.readyState).toBe('open')
      expect(secondRawChannel).not.toBe(firstRawChannel)
      await expect(session.openTerminal('terminal-1')).resolves.toBeTruthy()
      expect(factory.channel('terminal:terminal-1')).toBe(secondRawChannel)
    } finally {
      vi.useRealTimers()
    }
  })

  it('force closes and recreates an open terminal data channel on resume reattach', async () => {
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
    const first = await session.openTerminal('terminal-1')
    const firstRawChannel = factory.channel('terminal:terminal-1')

    session.closeTerminalDataChannel('terminal-1')
    const second = await session.openTerminal('terminal-1')
    const secondRawChannel = factory.channel('terminal:terminal-1')

    expect(first.readyState).toBe('closed')
    expect(second.label).toBe('terminal:terminal-1')
    expect(second.readyState).toBe('open')
    expect(secondRawChannel).not.toBe(firstRawChannel)
  })

  it('runs runtime status check when peer connection state reaches connected', async () => {
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
        method: 'GET',
        path: '/status',
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

  it('logs WebRTC state and selected candidate details when the api channel never opens', async () => {
    vi.useFakeTimers()
    try {
      const factory = createMockPeerConnectionFactory({
        initialReadyState: 'connecting',
        gatheredLocalSDP: [
          'v=0',
          'm=application 9 UDP/DTLS/SCTP webrtc-datachannel',
          'a=candidate:local1 1 udp 2122260223 10.0.0.2 54231 typ host',
          'a=candidate:local2 1 udp 2122260223 203.0.113.10 50000 typ srflx raddr 10.0.0.2 rport 54231',
          '',
        ].join('\r\n'),
        iceConnectionState: 'checking',
        signalingState: 'stable',
        sctpState: 'connecting',
        stats: new Map<string, Record<string, unknown>>([
          ['local-candidate-1', {
            id: 'local-candidate-1',
            type: 'local-candidate',
            candidateType: 'host',
            protocol: 'udp',
            address: '10.0.0.2',
            port: 54231,
          }],
          ['remote-candidate-1', {
            id: 'remote-candidate-1',
            type: 'remote-candidate',
            candidateType: 'srflx',
            protocol: 'udp',
            address: '114.66.58.243',
            port: 8447,
          }],
          ['pair-1', {
            id: 'pair-1',
            type: 'candidate-pair',
            state: 'succeeded',
            selected: true,
            nominated: true,
            localCandidateId: 'local-candidate-1',
            remoteCandidateId: 'remote-candidate-1',
            currentRoundTripTime: 0.12,
            requestsSent: 3,
            responsesReceived: 3,
          }],
          ['pair-2', {
            id: 'pair-2',
            type: 'candidate-pair',
            state: 'in-progress',
            localCandidateId: 'local-candidate-1',
            remoteCandidateId: 'remote-candidate-1',
          }],
        ]),
      })
      const logs: ConnectionLogEvent[] = []
      const session = createBrowserRtcSession({
        machineId: 'machine-local',
        path: 'managed',
        peerConnectionFactory: factory,
        sessionIdGenerator: () => 'rtc-timeout-1',
        dataChannelOpenTimeoutMs: 50,
        logger: { log: (event) => logs.push(event) },
      })

      await session.createOffer({
        machineId: 'machine-local',
        path: 'managed',
        iceServers: [{
          urls: ['turn:114.66.58.243:3478?transport=udp'],
          username: 'user',
          credential: 'secret',
        }],
      })
      const acceptPromise = session.acceptAnswer({
        type: 'answer',
        sdp: [
          'v=0',
          'a=fingerprint:sha-256 AA:BB',
          'm=application 9 UDP/DTLS/SCTP webrtc-datachannel',
          'a=ice-ufrag:iceuser',
          'a=ice-pwd:icepassword',
          'a=setup:active',
          'a=mid:0',
          'a=sctp-port:5000',
          'a=candidate:remote1 1 udp 2122260223 114.66.58.243 3478 typ relay raddr 10.0.0.3 rport 50000',
          '',
        ].join('\r\n'),
      })
      const rejection = expect(acceptPromise).rejects.toThrow(/timed out opening data channel api/)
      await vi.advanceTimersByTimeAsync(50)

      await rejection
      expect(logs).toEqual(expect.arrayContaining([
        expect.objectContaining({
          scope: 'browser_webrtc',
          event: 'data_channel_open_timeout',
          level: 'error',
          sessionId: 'rtc-timeout-1',
          message: 'timed out opening data channel api',
          details: expect.objectContaining({
            label: 'api',
            channelReadyState: 'connecting',
            peerConnectionState: 'new',
            iceConnectionState: 'checking',
            signalingState: 'stable',
            sctpState: 'connecting',
            iceServers: [{
              urls: ['turn:114.66.58.243:3478?transport=udp'],
              hasUsername: true,
              hasCredential: true,
            }],
            localDescriptionCandidates: expect.objectContaining({
              count: 2,
              byType: { host: 1, srflx: 1 },
            }),
            remoteDescriptionCandidates: expect.objectContaining({
              count: 0,
            }),
            rawAnswerCandidates: expect.objectContaining({
              count: 1,
              byType: { relay: 1 },
            }),
            addedRemoteCandidates: expect.objectContaining({
              count: 1,
              byType: { relay: 1 },
            }),
            selectedCandidatePair: expect.objectContaining({
              id: 'pair-1',
              state: 'succeeded',
              nominated: true,
              requestsSent: 3,
              responsesReceived: 3,
              local: expect.objectContaining({ candidateType: 'host', address: '10.0.0.2' }),
              remote: expect.objectContaining({ candidateType: 'srflx', address: '114.66.58.243' }),
            }),
            candidatePairs: expect.arrayContaining([
              expect.objectContaining({ id: 'pair-1', state: 'succeeded' }),
              expect.objectContaining({ id: 'pair-2', state: 'in-progress' }),
            ]),
            localCandidates: expect.arrayContaining([
              expect.objectContaining({ id: 'local-candidate-1', candidateType: 'host' }),
            ]),
            remoteCandidates: expect.arrayContaining([
              expect.objectContaining({ id: 'remote-candidate-1', candidateType: 'srflx' }),
            ]),
          }),
        }),
      ]))
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not delay the api channel open timeout while collecting slow WebRTC stats', async () => {
    vi.useFakeTimers()
    try {
      const factory = createMockPeerConnectionFactory({
        initialReadyState: 'connecting',
        statsPromise: new Promise<RTCStatsReport>(() => {}),
      })
      const logs: ConnectionLogEvent[] = []
      const session = createBrowserRtcSession({
        machineId: 'machine-local',
        path: 'managed',
        peerConnectionFactory: factory,
        sessionIdGenerator: () => 'rtc-slow-stats-1',
        dataChannelOpenTimeoutMs: 50,
        logger: { log: (event) => logs.push(event) },
      })
      await session.createOffer({ machineId: 'machine-local', path: 'managed' })
      const acceptPromise = session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })
      const rejection = expect(acceptPromise).rejects.toThrow(/timed out opening data channel api/)

      await vi.advanceTimersByTimeAsync(50)
      await rejection
      expect(logs.some((event) => event.event === 'data_channel_open_timeout')).toBe(false)

      await vi.advanceTimersByTimeAsync(500)
      await flushMicrotasks()
      expect(logs).toEqual(expect.arrayContaining([
        expect.objectContaining({
          event: 'data_channel_open_timeout',
          details: expect.objectContaining({
            selectedCandidatePair: expect.objectContaining({
              error: 'timed out reading browser WebRTC stats',
            }),
          }),
        }),
      ]))
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not report a second session failure when active disconnect closes the api channel', async () => {
    vi.useFakeTimers()
    try {
      const factory = createMockPeerConnectionFactory({ closeEventDelayMs: 1 })
      const logs: ConnectionLogEvent[] = []
      const session = createBrowserRtcSession({
        machineId: 'machine-local',
        path: 'managed',
        peerConnectionFactory: factory,
        sessionIdGenerator: () => 'rtc-disconnect-1',
        logger: { log: (event) => logs.push(event) },
      })
      await session.createOffer({ machineId: 'machine-local', path: 'managed' })
      await session.acceptAnswer({ type: 'answer', sdp: 'answer-sdp' })

      await session.disconnect()
      await vi.advanceTimersByTimeAsync(1)
      await flushMicrotasks()

      expect(logs.filter((event) => event.event === 'session_failed')).toEqual([])
      expect(logs).toEqual(expect.arrayContaining([
        expect.objectContaining({ scope: 'browser_webrtc', event: 'session_closing', message: 'browser WebRTC session disconnected' }),
        expect.objectContaining({ scope: 'browser_webrtc', event: 'data_channel_close', details: { label: 'api' } }),
      ]))
    } finally {
      vi.useRealTimers()
    }
  })

  it('normalizes Pion-only answer SDP lines before accepting an answer', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-local-1',
    })
    await session.createOffer({ machineId: 'machine-local', terminalId: 'terminal-1', path: 'local' })

    await session.acceptAnswer({
      type: 'answer',
      sdp: [
        'v=0',
        's=-',
        'a=fingerprint:sha-256 AA:BB',
        'm=application 9 UDP/DTLS/SCTP webrtc-datachannel',
        'a=setup:active',
        'a=mid:0',
        'a=sendrecv',
        'a=sctp-port:5000',
        'a=max-message-size:1073741823',
        'a=ice-ufrag:iceuser',
        'a=ice-pwd:icepassword',
        'a=candidate:375738803 1 tcp 1671430143 127.0.0.1 18888 typ host tcptype passive ufrag abc123',
        'a=candidate:375738803 2 tcp 1671430143 127.0.0.1 18888 typ host tcptype passive ufrag abc123',
        'a=end-of-candidates',
        '',
      ].join('\r\n'),
    })

    expect(factory.lastConnection()?.remoteDescription?.sdp).toBe([
      'v=0',
      's=-',
      'm=application 9 UDP/DTLS/SCTP webrtc-datachannel',
      'a=ice-ufrag:iceuser',
      'a=ice-pwd:icepassword',
      'a=ice-options:trickle',
      'a=fingerprint:sha-256 AA:BB',
      'a=setup:active',
      'a=mid:0',
      'a=sctp-port:5000',
      'a=max-message-size:262144',
      '',
    ].join('\r\n'))
    expect(factory.lastConnection()?.addedCandidates).toEqual([{
      candidate: 'candidate:375738803 1 tcp 1671430143 127.0.0.1 18888 typ host tcptype passive',
      sdpMid: '0',
      sdpMLineIndex: 0,
    }])
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

  it('normalizes core protocol terminal events for inventory refresh handlers', async () => {
    const factory = createMockPeerConnectionFactory()
    const session = createBrowserRtcSession({
      machineId: 'machine-local',
      path: 'local',
      peerConnectionFactory: factory,
      sessionIdGenerator: () => 'rtc-events-protocol-1',
    })
    const events: unknown[] = []

    await connectBrowserSession(session)
    const subscription = session.subscribeEvents((event) => events.push(event))
    await flushMicrotasks()
    factory.channel('events').emitMessage(encodeJSON({
      type: 1,
      terminal_id: 'terminal-3',
      timestamp: '2026-05-08T10:00:00Z',
    }))

    expect(events).toContainEqual({
      type: 'inventory_changed',
      payload: {
        eventType: 'terminal_created',
        protocolType: 1,
        terminalId: 'terminal-3',
        timestamp: '2026-05-08T10:00:00Z',
      },
    })
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
  iceConnectionState?: RTCIceConnectionState
  signalingState?: RTCSignalingState
  sctpState?: string
  stats?: Map<string, Record<string, unknown>>
  statsPromise?: Promise<RTCStatsReport>
  closeEventDelayMs?: number
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
      iceConnectionState: options.iceConnectionState,
      signalingState: options.signalingState,
      sctpState: options.sctpState,
      stats: options.stats,
      statsPromise: options.statsPromise,
      closeEventDelayMs: options.closeEventDelayMs,
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
  addedCandidates: RTCIceCandidateInit[] = []
  iceGatheringState: RTCIceGatheringState
  iceConnectionState: RTCIceConnectionState
  signalingState: RTCSignalingState
  connectionState: RTCPeerConnectionState = 'new'
  sctp: { state?: string } | null
  closed = false
  private readonly channels: Map<string, MockRTCDataChannel>
  private readonly createOfferLabels: string[][]
  private readonly initialReadyState: RTCDataChannelState
  private readonly gatheredLocalSDP: string | undefined
  private readonly stats: Map<string, Record<string, unknown>>
  private readonly statsPromise: Promise<RTCStatsReport> | undefined
  private readonly closeEventDelayMs: number | undefined

  constructor(options: {
    configuration?: RTCConfiguration | undefined
    channels: Map<string, MockRTCDataChannel>
    createOfferLabels: string[][]
    initialReadyState: RTCDataChannelState
    initialIceGatheringState: RTCIceGatheringState
    gatheredLocalSDP?: string | undefined
    iceConnectionState?: RTCIceConnectionState | undefined
    signalingState?: RTCSignalingState | undefined
    sctpState?: string | undefined
    stats?: Map<string, Record<string, unknown>> | undefined
    statsPromise?: Promise<RTCStatsReport> | undefined
    closeEventDelayMs?: number | undefined
  }) {
    super()
    this.configuration = options.configuration
    this.channels = options.channels
    this.createOfferLabels = options.createOfferLabels
    this.initialReadyState = options.initialReadyState
    this.iceGatheringState = options.initialIceGatheringState
    this.gatheredLocalSDP = options.gatheredLocalSDP
    this.iceConnectionState = options.iceConnectionState ?? 'new'
    this.signalingState = options.signalingState ?? 'stable'
    this.sctp = options.sctpState ? { state: options.sctpState } : null
    this.stats = options.stats ?? new Map()
    this.statsPromise = options.statsPromise
    this.closeEventDelayMs = options.closeEventDelayMs
  }

  readonly configuration: RTCConfiguration | undefined

  createDataChannel(label: string): MockRTCDataChannel {
    const channel = new MockRTCDataChannel(label, this.initialReadyState, this.closeEventDelayMs)
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
    this.localDescription = this.iceGatheringState === 'complete' && this.gatheredLocalSDP
      ? { ...description, sdp: this.gatheredLocalSDP }
      : description
  }

  async setRemoteDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.remoteDescription = description
  }

  async addIceCandidate(candidate: RTCIceCandidateInit): Promise<void> {
    this.addedCandidates.push(candidate)
  }

  async getStats(): Promise<RTCStatsReport> {
    if (this.statsPromise) return this.statsPromise
    const stats = this.stats
    return {
      forEach(callbackfn: (value: unknown, key: string, parent: RTCStatsReport) => void, thisArg?: unknown) {
        for (const [key, value] of stats) {
          callbackfn.call(thisArg, value, key, this as RTCStatsReport)
        }
      },
    } as RTCStatsReport
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
    const channel = new MockRTCDataChannel(label, this.initialReadyState, this.closeEventDelayMs)
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

  constructor(readonly label: string, initialReadyState: RTCDataChannelState, private readonly closeEventDelayMs?: number | undefined) {
    super()
    this.readyState = initialReadyState
  }

  send(data: string | ArrayBuffer | Blob | ArrayBufferView): void {
    if (this.readyState !== 'open') throw new Error(`data channel ${this.label} is not open`)
    this.sent.push(data)
  }

  close(): void {
    this.readyState = 'closed'
    if (this.closeEventDelayMs === undefined) {
      this.dispatchEvent(new Event('close'))
      return
    }
    setTimeout(() => this.dispatchEvent(new Event('close')), this.closeEventDelayMs)
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
