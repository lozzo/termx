import { describe, expect, it } from 'vitest'
import {
  createConnectionOrchestrator,
  type ConnectionAttemptSnapshot,
} from './connectionOrchestrator'
import source from './connectionOrchestrator.ts?raw'
import { ManualLocalHubUrlProvider } from './localHubUrlProvider'
import type { ManagedHubApi } from './managedHubApi'
import type {
  ConnectionCapabilities,
  ConnectionInfo,
  RtcBinaryChannel,
  RtcJsonRpcChannel,
  RtcSession,
} from './transport'

describe('ConnectionOrchestrator', () => {
  it('tries local Hub first and returns before racing online hubs when local succeeds', async () => {
    const localSession = new MockRtcSession({
      path: 'local',
      connectionId: 'local-rtc-1',
      machineId: 'machine-1',
      relayInUse: false,
    })
    const localConnector = new RecordingManagedHubConnector(localSession)
    const onlineConnector = new RecordingManagedHubConnector(new Error('online should not run'))
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({
      localHubUrlProvider: new ManualLocalHubUrlProvider('http://127.0.0.1:18888'),
      managedHubApiFactory: (hubUrl) => new MockManagedHubApi(hubUrl),
      managedHubRtcConnectorFactory: ({ hubUrl }) => hubUrl.includes('127.0.0.1') ? localConnector : onlineConnector,
    })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      hubUrls: ['https://hub-1.termx.test'],
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })

    expect(result.session).toBe(localSession)
    expect(result.path).toBe('local')
    expect(localConnector.calls).toEqual([{
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      path: 'local',
    }])
    expect(onlineConnector.calls).toEqual([])
    expect(snapshots.map((snapshot) => snapshot.stage)).toEqual(['trying_local', 'connected'])
  })

  it('races all configured hub URLs after local fails and returns the first managed success', async () => {
    const slowFailure = new RecordingManagedHubConnector(new Error('hub 1 unavailable'))
    const winnerSession = new MockRtcSession({
      path: 'managed',
      connectionId: 'managed-rtc-2',
      machineId: 'machine-1',
      relayInUse: true,
    })
    const winner = new RecordingManagedHubConnector(winnerSession)
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({
      localHubUrlProvider: new ManualLocalHubUrlProvider('http://127.0.0.1:18888'),
      managedHubApiFactory: (hubUrl) => new MockManagedHubApi(hubUrl),
      managedHubRtcConnectorFactory: ({ hubUrl }) => {
        if (hubUrl.includes('127.0.0.1')) return new RecordingManagedHubConnector(new Error('local unreachable'))
        return hubUrl.includes('hub-1') ? slowFailure : winner
      },
    })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      hubUrls: ['https://hub-1.termx.test', 'https://hub-2.termx.test'],
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })

    expect(result.session).toBe(winnerSession)
    expect(result.path).toBe('managed')
    expect(result.relayInUse).toBe(true)
    expect(slowFailure.calls).toHaveLength(1)
    expect(winner.calls).toEqual([{
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      path: 'managed',
    }])
    expect(snapshots).toEqual([
      { stage: 'trying_local', path: 'local', message: 'Trying local connection' },
      { stage: 'trying_managed', path: 'managed', message: 'Racing 2 hub(s)' },
      { stage: 'connected', path: 'managed', relayInUse: true, message: 'Connected' },
    ])
  })

  it('fails when no hub URLs are configured after local is unavailable', async () => {
    const orchestrator = createConnectionOrchestrator({
      localHubUrlProvider: new ManualLocalHubUrlProvider('http://127.0.0.1:18888'),
      managedHubApiFactory: (hubUrl) => new MockManagedHubApi(hubUrl),
      managedHubRtcConnectorFactory: () => new RecordingManagedHubConnector(new Error('local unreachable')),
    })

    await expect(orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      hubUrls: [],
    })).rejects.toThrow(/no hub URLs configured/i)
  })

  it('stops on abort instead of continuing to hub racing', async () => {
    const controller = new AbortController()
    const orchestrator = createConnectionOrchestrator({
      localHubUrlProvider: new ManualLocalHubUrlProvider('http://127.0.0.1:18888'),
      managedHubApiFactory: (hubUrl) => new MockManagedHubApi(hubUrl),
      managedHubRtcConnectorFactory: () => new RecordingManagedHubConnector(async () => {
        controller.abort(new Error('user canceled connection'))
        return new MockRtcSession({
          path: 'local',
          connectionId: 'local-rtc-1',
          machineId: 'machine-1',
          relayInUse: false,
        })
      }),
    })

    await expect(orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      hubUrls: ['https://hub-1.termx.test'],
    }, { signal: controller.signal })).rejects.toThrow(/user canceled connection/i)
  })

  it('keeps orchestration above signaling/runtime adapters and old offer signing', () => {
    expect(source).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket|fetch\(|XMLHttpRequest/)
    expect(source).not.toMatch(/appCertificate|app_certificate|signOffer|ed25519|offer_signature/)
    expect(source).not.toMatch(/paid_relay|managed_p2p|anonymous_p2p|"relay"|'relay'/)
    expect(source).not.toMatch(/openTerminal\(|openApi\(|openFileTransfer\(|subscribeEvents\(/)
  })
})

class RecordingManagedHubConnector {
  readonly calls: unknown[] = []

  constructor(private readonly result: RtcSession | Error | (() => Promise<RtcSession>)) {}

  async connect(input: unknown): Promise<RtcSession> {
    this.calls.push(input)
    if (this.result instanceof Error) throw this.result
    if (typeof this.result === 'function') return this.result()
    return this.result
  }
}

class MockManagedHubApi implements ManagedHubApi {
  constructor(readonly hubUrl: string) {}

  async createSession(): ReturnType<ManagedHubApi['createSession']> {
    throw new Error('createSession is not used by orchestrator tests')
  }

  async pollSessionAnswer(): ReturnType<ManagedHubApi['pollSessionAnswer']> {
    throw new Error('pollSessionAnswer is not used by orchestrator tests')
  }

  async pair(): ReturnType<ManagedHubApi['pair']> {
    throw new Error('pair is not used by orchestrator tests')
  }
}

class MockRtcSession implements RtcSession {
  disconnectCalls = 0

  constructor(private readonly info: ConnectionInfo) {}

  async openTerminal(): Promise<RtcBinaryChannel> {
    throw new Error('terminal is not used by orchestrator tests')
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    throw new Error('api is not used by orchestrator tests')
  }

  async openFileTransfer(): Promise<RtcBinaryChannel> {
    throw new Error('file transfer is not used by orchestrator tests')
  }

  subscribeEvents() {
    return { close() {} }
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    return this.info
  }

  async getCapabilities(): Promise<ConnectionCapabilities> {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: this.info.relayInUse,
    }
  }

  async disconnect(): Promise<void> {
    this.disconnectCalls += 1
  }
}
