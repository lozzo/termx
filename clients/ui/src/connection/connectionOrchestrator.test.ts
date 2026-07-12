import { describe, expect, it, vi } from 'vitest'
import {
  createConnectionOrchestrator,
  type ConnectionAttemptSnapshot,
} from './connectionOrchestrator'
import type { ConnectionLogEvent } from './connectionLogger'
import source from './connectionOrchestrator.ts?raw'
import { ManualLocalHubUrlProvider } from './localHubUrlProvider'
import type { HubApi } from '../api/hubApi'
import type {
  ConnectionCapabilities,
  ConnectionInfo,
  RtcBinaryChannel,
  RtcJsonRpcChannel,
  RtcSession,
} from '../core/transport'

describe('ConnectionOrchestrator', () => {
  it('keeps local web scoped to the current local Hub endpoint', async () => {
    const localSession = new MockRtcSession({
      path: 'local',
      connectionId: 'local-rtc-1',
      machineId: 'machine-1',
      relayInUse: false,
    })
    const localConnector = new RecordingHubConnector(localSession)
    const hubConnector = new RecordingHubConnector(new Error('hub should not run for local web'))
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({
      localHubUrlProvider: new ManualLocalHubUrlProvider('http://127.0.0.1:18888'),
      hubApiFactory: (hubUrl) => new MockHubApi(hubUrl),
      hubRtcConnectorFactory: ({ hubUrl }) => hubUrl.includes('127.0.0.1') ? localConnector : hubConnector,
    })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      policy: 'local_web',
      endpoints: [
        { url: 'https://hub-1.termx.test', kind: 'hub', scope: 'hub', source: 'web_control' },
      ],
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
    expect(hubConnector.calls).toEqual([])
    expect(snapshots).toEqual([
      { stage: 'trying_local', path: 'local', message: 'Racing 1 local address(es)' },
      { stage: 'connected', path: 'local', relayInUse: false, message: 'Connected' },
    ])
  })

  it('starts app local and hub races together and uses the first successful path', async () => {
    vi.useFakeTimers()
    try {
      const localSession = new MockRtcSession({
        path: 'local',
        connectionId: 'local-rtc',
        machineId: 'machine-1',
        relayInUse: false,
      })
      const hubSession = new MockRtcSession({
        path: 'hub',
        connectionId: 'hub-rtc',
        machineId: 'machine-1',
        relayInUse: true,
      })
      const localConnector = new RecordingHubConnector(async () => {
        await delayForTest(40)
        return localSession
      })
      const hubConnector = new RecordingHubConnector(async () => {
        await delayForTest(10)
        return hubSession
      })
      const snapshots: ConnectionAttemptSnapshot[] = []
      const orchestrator = createConnectionOrchestrator({
        hubApiFactory: (hubUrl) => new MockHubApi(hubUrl),
        hubRtcConnectorFactory: ({ hubUrl }) => hubUrl.includes('192.168.1.20') ? localConnector : hubConnector,
      })

      const promise = orchestrator.connect({
        machineId: 'machine-1',
        terminalId: 'terminal-1',
        sessionToken: 'session-token-1',
        policy: 'app_fastest',
        endpoints: [
          { url: 'http://192.168.1.20:18888', kind: 'local', scope: 'lan', source: 'pair_qr' },
          { url: 'https://hub-1.termx.test', kind: 'hub', scope: 'hub', source: 'web_control' },
        ],
        onSnapshot: (snapshot) => snapshots.push(snapshot),
      })
      await vi.advanceTimersByTimeAsync(10)
      expect(localConnector.calls).toHaveLength(1)
      expect(hubConnector.calls).toHaveLength(1)
      const result = await promise

      expect(result.session).toBe(hubSession)
      expect(result.path).toBe('hub')
      await vi.advanceTimersByTimeAsync(30)
      expect(localSession.disconnectCalls).toBe(1)
      expect(snapshots).toEqual([
        { stage: 'trying_local', path: 'local', message: 'Racing 1 local address(es)' },
        { stage: 'trying_hub', path: 'hub', message: 'Racing 1 hub endpoint(s)' },
        { stage: 'connected', path: 'hub', relayInUse: true, message: 'Connected' },
      ])
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not wait for a slower local connection once hub has connected', async () => {
    vi.useFakeTimers()
    try {
      const localSession = new MockRtcSession({
        path: 'local',
        connectionId: 'local-rtc',
        machineId: 'machine-1',
        relayInUse: false,
      })
      const hubSession = new MockRtcSession({
        path: 'hub',
        connectionId: 'hub-rtc',
        machineId: 'machine-1',
        relayInUse: true,
      })
      const localConnector = new RecordingHubConnector(async () => {
        await delayForTest(200)
        return localSession
      })
      const hubConnector = new RecordingHubConnector(async () => {
        await delayForTest(10)
        return hubSession
      })
      const orchestrator = createConnectionOrchestrator({
        hubApiFactory: (hubUrl) => new MockHubApi(hubUrl),
        hubRtcConnectorFactory: ({ hubUrl }) => hubUrl.includes('frp.termx.test') ? localConnector : hubConnector,
      })

      const promise = orchestrator.connect({
        machineId: 'machine-1',
        terminalId: 'terminal-1',
        sessionToken: 'session-token-1',
        policy: 'app_fastest',
        endpoints: [
          { url: 'https://frp.termx.test', kind: 'local', scope: 'public_mapping', source: 'pair_qr' },
          { url: 'https://hub-1.termx.test', kind: 'hub', scope: 'hub', source: 'web_control' },
        ],
      })
      await vi.advanceTimersByTimeAsync(10)
      const result = await promise

      expect(result.session).toBe(hubSession)
      expect(result.path).toBe('hub')
      expect(localConnector.calls).toEqual([expect.objectContaining({ path: 'local' })])
    } finally {
      vi.useRealTimers()
    }
  })

  it('treats FRP/public mappings as local endpoints, not hub hubs', async () => {
    const localSession = new MockRtcSession({
      path: 'local',
      connectionId: 'local-rtc-frp',
      machineId: 'machine-1',
      relayInUse: false,
    })
    const localConnector = new RecordingHubConnector(localSession)
    const hubConnector = new RecordingHubConnector(new Error('hub should not run'))
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({
      hubApiFactory: (hubUrl) => new MockHubApi(hubUrl),
      hubRtcConnectorFactory: ({ hubUrl }) => hubUrl.includes('frp.termx.test') ? localConnector : hubConnector,
    })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      policy: 'app_fastest',
      endpoints: [
        { url: 'https://frp.termx.test', kind: 'local', scope: 'public_mapping', source: 'pair_qr' },
      ],
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })

    expect(result.session).toBe(localSession)
    expect(result.path).toBe('local')
    expect(localConnector.calls).toEqual([expect.objectContaining({ path: 'local' })])
    expect(hubConnector.calls).toEqual([])
    expect(snapshots[0]).toEqual({ stage: 'trying_local', path: 'local', message: 'Racing 1 public local address(es)' })
  })

  it('logs every attempted endpoint when all connection paths fail', async () => {
    const logs: ConnectionLogEvent[] = []
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({
      hubApiFactory: (hubUrl) => new MockHubApi(hubUrl),
      hubRtcConnectorFactory: ({ hubUrl }) => new RecordingHubConnector(new Error(`${hubUrl} rejected offer`)),
      logger: { log: (event) => logs.push(event) },
    })

    await expect(orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      policy: 'app_fastest',
      endpoints: [
        { url: 'http://192.168.1.20:18888', kind: 'local', scope: 'lan', source: 'pair_qr' },
        { url: 'https://hub-1.termx.test', kind: 'hub', scope: 'hub', source: 'web_control' },
        { url: 'https://hub-2.termx.test', kind: 'hub', scope: 'hub', source: 'web_control' },
      ],
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })).rejects.toThrow(/all connection paths failed/i)

    const failedSnapshot = snapshots.find((snapshot) => snapshot.stage === 'failed')
    expect(failedSnapshot?.errors).toEqual(expect.arrayContaining([
      {
        path: 'local',
        hubUrl: 'http://192.168.1.20:18888',
        message: 'http://192.168.1.20:18888 rejected offer',
      },
      {
        path: 'hub',
        hubUrl: 'https://hub-1.termx.test',
        message: 'https://hub-1.termx.test rejected offer',
      },
      {
        path: 'hub',
        hubUrl: 'https://hub-2.termx.test',
        message: 'https://hub-2.termx.test rejected offer',
      },
    ]))
    expect(logs).toEqual(expect.arrayContaining([
      expect.objectContaining({ scope: 'orchestrator', event: 'local_hub_attempt_failed', path: 'local', hubUrl: 'http://192.168.1.20:18888' }),
      expect.objectContaining({ scope: 'orchestrator', event: 'hub_attempt_failed', path: 'hub', hubUrl: 'https://hub-1.termx.test' }),
      expect.objectContaining({ scope: 'orchestrator', event: 'hub_attempt_failed', path: 'hub', hubUrl: 'https://hub-2.termx.test' }),
      expect.objectContaining({ scope: 'orchestrator', event: 'connect_failed_all_paths', level: 'error' }),
    ]))
  })

  it('keeps orchestration above signaling/runtime adapters and old offer signing', () => {
    expect(source).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket|fetch\(|XMLHttpRequest/)
    expect(source).not.toMatch(/appCertificate|app_certificate|signOffer|ed25519|offer_signature/)
    expect(source).not.toMatch(/paid_relay|managed_p2p|anonymous_p2p|"relay"|'relay'/)
    expect(source).not.toMatch(/openTerminal\(|openApi\(|openFileChannel\(|subscribeEvents\(/)
  })
})

class RecordingHubConnector {
  readonly calls: unknown[] = []

  constructor(private readonly result: RtcSession | Error | ((input: unknown, options?: { signal?: AbortSignal }) => Promise<RtcSession>)) {}

  async connect(input: unknown, options?: { signal?: AbortSignal }): Promise<RtcSession> {
    this.calls.push(input)
    if (this.result instanceof Error) throw this.result
    if (typeof this.result === 'function') return this.result(input, options)
    return this.result
  }
}

class MockHubApi implements HubApi {
  constructor(readonly hubUrl: string) {}

  async getSessionIce(): ReturnType<HubApi['getSessionIce']> {
    throw new Error('getSessionIce is not used by orchestrator tests')
  }

  async createSession(): ReturnType<HubApi['createSession']> {
    throw new Error('createSession is not used by orchestrator tests')
  }

  async pollSessionAnswer(): ReturnType<HubApi['pollSessionAnswer']> {
    throw new Error('pollSessionAnswer is not used by orchestrator tests')
  }

  async pair(): ReturnType<HubApi['pair']> {
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

  async openFileChannel(): Promise<RtcBinaryChannel> {
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

function delayForTest(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
