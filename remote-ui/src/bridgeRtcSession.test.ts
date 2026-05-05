import { describe, expect, it, vi } from 'vitest'
import {
  createBridgeRtcSession,
  type BridgeRtcSessionAdapter,
} from './bridgeRtcSession'
import indexSource from './index.ts?raw'
import bridgeSource from './bridgeRtcSession.ts?raw'
import orchestratorSource from './connectionOrchestrator.ts?raw'
import terminalSource from './Terminal.tsx?raw'
import useTerminalSource from './useTerminalSession.tsx?raw'
import fileManagerSource from './useFileManager.tsx?raw'
import type {
  ConnectionCapabilities,
  ConnectionInfo,
  RtcBinaryChannel,
  RtcConnectOptions,
  RtcEvent,
  RtcJsonRpcChannel,
  RtcSessionAnswerTarget,
  RtcSessionDescription,
  RtcSessionNegotiationTarget,
  RtcSubscription,
} from './transport'

describe('BridgeRtcSession seam', () => {
  it('lets bridge-backed local sessions create offers and accept answers without browser or platform runtime types', async () => {
    const bridge = new MockBridgeAdapter()
    const session = createBridgeRtcSession({
      machineId: 'machine-bridge',
      terminalId: 'terminal-1',
      adapter: bridge,
    })

    const offer = await session.createOffer({ machineId: 'machine-bridge', terminalId: 'terminal-1', path: 'local' })
    await session.acceptAnswer({ type: 'answer', sdp: 'local-answer-sdp' })

    expect(offer).toEqual({
      sessionId: 'bridge-offer-1',
      description: { type: 'offer', sdp: 'bridge-offer-sdp' },
    })
    expect(bridge.createdOffers).toEqual([{
      machineId: 'machine-bridge',
      terminalId: 'terminal-1',
      path: 'local',
    }])
    expect(bridge.acceptedAnswers).toEqual([{
      sessionId: 'bridge-offer-1',
      description: { type: 'answer', sdp: 'local-answer-sdp' },
    }])
    await expect(session.getConnectionInfo()).resolves.toMatchObject({
      path: 'local',
      connectionId: 'bridge-offer-1',
      machineId: 'machine-bridge',
      terminalId: 'terminal-1',
      relayInUse: false,
    })
  })

  it('answers managed offers while keeping relay as managed capability information', async () => {
    const bridge = new MockBridgeAdapter()
    const session = createBridgeRtcSession({
      machineId: 'machine-bridge',
      terminalId: 'terminal-1',
      adapter: bridge,
    })

    const answer = await session.acceptOffer({
      sessionId: 'managed-bridge-1',
      machineId: 'machine-bridge',
      terminalId: 'terminal-1',
      path: 'managed',
      description: { type: 'offer', sdp: 'managed-offer-sdp' },
      iceServers: [{ urls: ['turn:turn.example:3478'], username: 'u', credential: 'p' }],
      relayPolicy: { allowRelay: true, allowRelayTransfer: false },
      relayInUse: true,
    })

    expect(answer).toEqual({
      sessionId: 'managed-bridge-1',
      description: { type: 'answer', sdp: 'bridge-answer-sdp' },
    })
    expect(bridge.acceptedOffers).toEqual([{
      sessionId: 'managed-bridge-1',
      machineId: 'machine-bridge',
      terminalId: 'terminal-1',
      path: 'managed',
      description: { type: 'offer', sdp: 'managed-offer-sdp' },
      iceServers: [{ urls: ['turn:turn.example:3478'], username: 'u', credential: 'p' }],
      relayPolicy: { allowRelay: true, allowRelayTransfer: false },
      relayInUse: true,
    }])
    await expect(session.getConnectionInfo()).resolves.toEqual({
      path: 'managed',
      connectionId: 'managed-bridge-1',
      machineId: 'machine-bridge',
      terminalId: 'terminal-1',
      relayInUse: true,
    })
    await expect(session.getCapabilities()).resolves.toMatchObject({
      relayInUse: true,
      fileTransferAllowed: false,
    })
    await expect(session.openFileTransfer('upload-1')).rejects.toThrow(/file transfer|relay policy/i)
  })

  it('fails closed on relay outside managed and on bridge-reported relay path', async () => {
    const bridge = new MockBridgeAdapter()
    const session = createBridgeRtcSession({
      machineId: 'machine-bridge',
      adapter: bridge,
    })

    await expect(session.createOffer({
      machineId: 'machine-bridge',
      path: 'relay',
    } as never)).rejects.toThrow(/connection path/i)
    await expect(session.acceptOffer({
      sessionId: 'public-bridge-1',
      machineId: 'machine-bridge',
      path: 'public_p2p',
      description: { type: 'offer', sdp: 'public-offer-sdp' },
      relayPolicy: { allowRelay: true, allowRelayTransfer: true },
      relayInUse: true,
    } as RtcSessionAnswerTarget)).rejects.toThrow(/relay.*managed/i)

    bridge.connectionInfo = {
      path: 'relay',
      connectionId: 'bad-bridge-1',
      machineId: 'machine-bridge',
      relayInUse: true,
    } as never
    await expect(session.getConnectionInfo()).rejects.toThrow(/connection path/i)
  })

  it('delegates runtime channels through neutral channel interfaces and accepts capability updates', async () => {
    const bridge = new MockBridgeAdapter()
    const session = createBridgeRtcSession({
      machineId: 'machine-bridge',
      terminalId: 'terminal-1',
      adapter: bridge,
    })
    await session.createOffer({
      machineId: 'machine-bridge',
      terminalId: 'terminal-1',
      path: 'local',
    })

    session.updateConnectionCapabilities({
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: false,
      terminalManagementAllowed: false,
      relayInUse: false,
      denialReason: 'policy blocks file transfer',
    })
    const terminal = await session.openTerminal('terminal-1')
    const api = await session.openApi()
    const events: RtcEvent[] = []
    const subscription = session.subscribeEvents((event) => events.push(event))

    expect(terminal.label).toBe('terminal:terminal-1')
    await expect(api.request('status')).resolves.toEqual({ ok: true })
    await expect(session.openFileTransfer('upload-1')).rejects.toThrow(/policy blocks file transfer/i)
    bridge.emit({ type: 'heartbeat', payload: { ok: true } })
    expect(events).toEqual([{ type: 'heartbeat', payload: { ok: true } }])
    expect(bridge.openedBinaryLabels).toEqual(['terminal:terminal-1'])
    expect(bridge.openedJsonLabels).toEqual(['api'])
    expect(bridge.capabilities.fileTransferAllowed).toBe(false)
    subscription.close()
  })

  it('keeps bridge adapter details out of common business modules and package exports', () => {
    const commonSources = [
      orchestratorSource,
      terminalSource,
      useTerminalSource,
      fileManagerSource,
    ].join('\n')
    expect(indexSource).not.toMatch(/createBridgeRtcSession|BridgeRtcSession|NativeRtc|Android|Swift|Kotlin|WebView/)
    expect(bridgeSource).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket|WKWebView|Swift|Kotlin|Android|nativePlugin|NativeWebRTC/)
    expect(bridgeSource).not.toMatch(/paid_relay|managed_p2p|anonymous_p2p|relayTransport|path:\s*['"]relay['"]/)
    expect(commonSources).not.toMatch(/BridgeRtc|NativeRtc|RTCPeerConnection|RTCDataChannel|WKWebView|Swift|Kotlin|Android|WebView/)
  })
})

class MockBridgeAdapter implements BridgeRtcSessionAdapter {
  readonly createdOffers: RtcSessionNegotiationTarget[] = []
  readonly acceptedAnswers: Array<{ sessionId: string; description: RtcSessionDescription }> = []
  readonly acceptedOffers: RtcSessionAnswerTarget[] = []
  readonly openedBinaryLabels: string[] = []
  readonly openedJsonLabels: string[] = []
  private readonly eventHandlers = new Set<(event: RtcEvent) => void>()
  connectionInfo: ConnectionInfo | undefined
  capabilities: ConnectionCapabilities = {
    terminalAllowed: true,
    apiAllowed: true,
    eventsAllowed: true,
    fileTransferAllowed: true,
    terminalManagementAllowed: true,
    relayInUse: false,
  }
  disconnectCalls = 0

  async createOffer(target: RtcSessionNegotiationTarget): Promise<{ sessionId: string; description: RtcSessionDescription }> {
    this.createdOffers.push(target)
    this.connectionInfo = {
      path: target.path,
      connectionId: 'bridge-offer-1',
      machineId: target.machineId,
      ...(target.terminalId ? { terminalId: target.terminalId } : {}),
      relayInUse: false,
    }
    return {
      sessionId: 'bridge-offer-1',
      description: { type: 'offer', sdp: 'bridge-offer-sdp' },
    }
  }

  async acceptAnswer(input: { sessionId: string; description: RtcSessionDescription }): Promise<void> {
    this.acceptedAnswers.push(input)
  }

  async acceptOffer(target: RtcSessionAnswerTarget): Promise<{ sessionId: string; description: RtcSessionDescription }> {
    this.acceptedOffers.push(target)
    this.connectionInfo = {
      path: target.path,
      connectionId: target.sessionId,
      machineId: target.machineId,
      ...(target.terminalId ? { terminalId: target.terminalId } : {}),
      relayInUse: target.relayInUse === true,
    }
    return {
      sessionId: target.sessionId,
      description: { type: 'answer', sdp: 'bridge-answer-sdp' },
    }
  }

  async openBinaryChannel(label: string): Promise<RtcBinaryChannel> {
    this.openedBinaryLabels.push(label)
    return new MockBinaryChannel(label)
  }

  async openJsonRpcChannel(label: 'api'): Promise<RtcJsonRpcChannel> {
    this.openedJsonLabels.push(label)
    return new MockJsonRpcChannel()
  }

  subscribeEvents(handler: (event: RtcEvent) => void): RtcSubscription {
    this.eventHandlers.add(handler)
    return {
      close: () => {
        this.eventHandlers.delete(handler)
      },
    }
  }

  emit(event: RtcEvent): void {
    for (const handler of this.eventHandlers) handler(event)
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    if (!this.connectionInfo) throw new Error('bridge is not connected')
    return this.connectionInfo
  }

  async getCapabilities(): Promise<ConnectionCapabilities> {
    return this.capabilities
  }

  updateConnectionCapabilities(capabilities: ConnectionCapabilities): void {
    this.capabilities = capabilities
  }

  async disconnect(): Promise<void> {
    this.disconnectCalls += 1
  }
}

class MockBinaryChannel implements RtcBinaryChannel {
  readyState = 'open' as const

  constructor(readonly label: string) {}

  send(): void {}

  close(): void {}

  onMessage(): RtcSubscription {
    return { close() {} }
  }

  onClose(): RtcSubscription {
    return { close() {} }
  }

  async waitOpen(): Promise<void> {}
}

class MockJsonRpcChannel implements RtcJsonRpcChannel {
  async request<TResponse>(): Promise<TResponse> {
    return { ok: true } as TResponse
  }

  close(): void {}
}
