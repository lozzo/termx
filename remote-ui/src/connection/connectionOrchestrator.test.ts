import { describe, expect, it } from 'vitest'
import {
  createConnectionOrchestrator,
  type ConnectionAttemptSnapshot,
} from './connectionOrchestrator'
import type { ConnectionLogEvent } from './connectionLogger'
import source from './connectionOrchestrator.ts?raw'
import { ManualLocalHubUrlProvider } from './localHubUrlProvider'
import type { ManagedHubApi } from '../api/managedHubApi'
import type {
  ConnectionCapabilities,
  ConnectionInfo,
  RtcBinaryChannel,
  RtcJsonRpcChannel,
  RtcSession,
} from '../core/transport'

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

  it('races configured local Hub URLs and uses the fastest local address before online hubs', async () => {
    const slowLocal = new RecordingManagedHubConnector(async (_input, options) => {
      await neverUntilAbort(options?.signal)
      return new MockRtcSession({
        path: 'local',
        connectionId: 'local-rtc-slow',
        machineId: 'machine-1',
        relayInUse: false,
      })
    })
    const fastLocalSession = new MockRtcSession({
      path: 'local',
      connectionId: 'local-rtc-fast',
      machineId: 'machine-1',
      relayInUse: false,
    })
    const fastLocal = new RecordingManagedHubConnector(fastLocalSession)
    const onlineConnector = new RecordingManagedHubConnector(new Error('online should not run'))
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({
      managedHubApiFactory: (hubUrl) => new MockManagedHubApi(hubUrl),
      managedHubRtcConnectorFactory: ({ hubUrl }) => {
        if (hubUrl.includes('192.168.1.20')) return fastLocal
        if (hubUrl.includes('127.0.0.1')) return slowLocal
        return onlineConnector
      },
    })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      localHubUrls: ['http://127.0.0.1:18888', 'http://192.168.1.20:18888'],
      hubUrls: ['https://hub-1.termx.test'],
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })

    expect(result.session).toBe(fastLocalSession)
    expect(result.path).toBe('local')
    expect(fastLocal.calls).toEqual([{
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      path: 'local',
    }])
    expect(slowLocal.calls).toHaveLength(1)
    expect(onlineConnector.calls).toEqual([])
    expect(snapshots).toEqual([
      { stage: 'trying_local', path: 'local', message: 'Racing 2 local address(es)' },
      { stage: 'connected', path: 'local', relayInUse: false, message: 'Connected' },
    ])
  })

  it('tries public local addresses before managed hubs when inner local addresses fail', async () => {
    const innerLocal = new RecordingManagedHubConnector(new Error('inner LAN unreachable'))
    const publicLocalSession = new MockRtcSession({
      path: 'local',
      connectionId: 'local-rtc-frp',
      machineId: 'machine-1',
      relayInUse: false,
    })
    const publicLocal = new RecordingManagedHubConnector(publicLocalSession)
    const onlineConnector = new RecordingManagedHubConnector(new Error('online should not run'))
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({
      managedHubApiFactory: (hubUrl) => new MockManagedHubApi(hubUrl),
      managedHubRtcConnectorFactory: ({ hubUrl }) => {
        if (hubUrl.includes('192.168.1.20')) return innerLocal
        if (hubUrl.includes('frp.termx.test')) return publicLocal
        return onlineConnector
      },
    })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      localHubUrls: ['http://192.168.1.20:18888'],
      localFallbackHubUrls: ['https://frp.termx.test'],
      hubUrls: ['https://hub-1.termx.test'],
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })

    expect(result.session).toBe(publicLocalSession)
    expect(result.path).toBe('local')
    expect(onlineConnector.calls).toEqual([])
    expect(snapshots.map((snapshot) => snapshot.message)).toEqual([
      'Racing 1 local address(es)',
      'Racing 1 public local address(es)',
      'Connected',
    ])
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

  it('logs every attempted path and hub URL when all connection paths fail', async () => {
    const logs: ConnectionLogEvent[] = []
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({
      localHubUrlProvider: new ManualLocalHubUrlProvider('http://127.0.0.1:18888'),
      managedHubApiFactory: (hubUrl) => new MockManagedHubApi(hubUrl),
      managedHubRtcConnectorFactory: ({ hubUrl }) => new RecordingManagedHubConnector(new Error(`${hubUrl} rejected offer`)),
      logger: { log: (event) => logs.push(event) },
    })

    await expect(orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      hubUrls: ['https://hub-1.termx.test', 'https://hub-2.termx.test'],
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })).rejects.toThrow(/all connection paths failed/i)

    const failedSnapshot = snapshots.find((snapshot) => snapshot.stage === 'failed')
    expect(failedSnapshot?.errors).toEqual([
      {
        path: 'local',
        message: 'http://127.0.0.1:18888 rejected offer',
      },
      {
        path: 'managed',
        hubUrl: 'https://hub-1.termx.test',
        message: 'https://hub-1.termx.test rejected offer',
      },
      {
        path: 'managed',
        hubUrl: 'https://hub-2.termx.test',
        message: 'https://hub-2.termx.test rejected offer',
      },
    ])
    expect(logs).toEqual(expect.arrayContaining([
      expect.objectContaining({ scope: 'orchestrator', event: 'path_attempt_failed', path: 'local', message: 'http://127.0.0.1:18888 rejected offer' }),
      expect.objectContaining({ scope: 'orchestrator', event: 'managed_hub_attempt_failed', path: 'managed', hubUrl: 'https://hub-1.termx.test', message: 'https://hub-1.termx.test rejected offer' }),
      expect.objectContaining({ scope: 'orchestrator', event: 'managed_hub_attempt_failed', path: 'managed', hubUrl: 'https://hub-2.termx.test', message: 'https://hub-2.termx.test rejected offer' }),
      expect.objectContaining({ scope: 'orchestrator', event: 'connect_failed_all_paths', level: 'error' }),
    ]))
  })

  it('logs cancelled managed Hub race losers without reporting them as failed attempts', async () => {
    const logs: ConnectionLogEvent[] = []
    const winnerSession = new MockRtcSession({
      path: 'managed',
      connectionId: 'managed-rtc-2',
      machineId: 'machine-1',
      relayInUse: true,
    })
    const orchestrator = createConnectionOrchestrator({
      managedHubApiFactory: (hubUrl) => new MockManagedHubApi(hubUrl),
      managedHubRtcConnectorFactory: ({ hubUrl }) => hubUrl.includes('hub-1')
        ? new RecordingManagedHubConnector(async (_input, options) => {
          await neverUntilAbort(options?.signal)
          return winnerSession
        })
        : new RecordingManagedHubConnector(winnerSession),
      logger: { log: (event) => logs.push(event) },
    })

    await expect(orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      hubUrls: ['https://hub-1.termx.test', 'https://hub-2.termx.test'],
    })).resolves.toMatchObject({ path: 'managed', relayInUse: true })

    expect(logs).toEqual(expect.arrayContaining([
      expect.objectContaining({ scope: 'orchestrator', event: 'managed_hub_attempt_success', hubUrl: 'https://hub-2.termx.test' }),
      expect.objectContaining({ scope: 'orchestrator', event: 'managed_hub_attempt_cancelled', hubUrl: 'https://hub-1.termx.test', level: 'debug' }),
    ]))
    expect(logs).not.toEqual(expect.arrayContaining([
      expect.objectContaining({ scope: 'orchestrator', event: 'managed_hub_attempt_failed', hubUrl: 'https://hub-1.termx.test' }),
    ]))
  })

  it('fails when no managed hub URLs are configured after provider local is unavailable', async () => {
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

  it('reports local candidate failures when no online hub is configured', async () => {
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({
      managedHubApiFactory: (hubUrl) => new MockManagedHubApi(hubUrl),
      managedHubRtcConnectorFactory: ({ hubUrl }) => new RecordingManagedHubConnector(new Error(`${hubUrl} unreachable`)),
    })

    await expect(orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      localHubUrls: ['http://192.168.1.20:18888'],
      localFallbackHubUrls: ['https://frp.termx.test'],
      hubUrls: [],
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })).rejects.toThrow(/all connection paths failed/i)

    expect(snapshots.at(-1)).toEqual({
      stage: 'failed',
      message: 'All connection paths failed',
      errors: [
        {
          path: 'local',
          hubUrl: 'http://192.168.1.20:18888',
          message: 'http://192.168.1.20:18888 unreachable',
        },
        {
          path: 'local',
          hubUrl: 'https://frp.termx.test',
          message: 'https://frp.termx.test unreachable',
        },
      ],
    })
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

  constructor(private readonly result: RtcSession | Error | ((input: unknown, options?: { signal?: AbortSignal }) => Promise<RtcSession>)) {}

  async connect(input: unknown, options?: { signal?: AbortSignal }): Promise<RtcSession> {
    this.calls.push(input)
    if (this.result instanceof Error) throw this.result
    if (typeof this.result === 'function') return this.result(input, options)
    return this.result
  }
}

class MockManagedHubApi implements ManagedHubApi {
  constructor(readonly hubUrl: string) {}

  async getSessionIce(): ReturnType<ManagedHubApi['getSessionIce']> {
    throw new Error('getSessionIce is not used by orchestrator tests')
  }

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

function neverUntilAbort(signal: AbortSignal | undefined): Promise<void> {
  return new Promise((_resolve, reject) => {
    signal?.addEventListener('abort', () => {
      reject(signal.reason instanceof Error ? signal.reason : new Error('connection orchestration aborted'))
    }, { once: true })
  })
}
