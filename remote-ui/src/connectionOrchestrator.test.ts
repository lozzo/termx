import { describe, expect, it } from 'vitest'
import {
  createConnectionOrchestrator,
  type ConnectionAttemptSnapshot,
} from './connectionOrchestrator'
import source from './connectionOrchestrator.ts?raw'
import type {
  ConnectionCapabilities,
  ConnectionInfo,
  RtcConnectionTarget,
  RtcBinaryChannel,
  RtcConnector,
  RtcJsonRpcChannel,
  RtcSession,
} from './transport'

describe('ConnectionOrchestrator', () => {
  it('returns the local RtcSession and does not trigger public_p2p when local succeeds', async () => {
    const localSession = new MockRtcSession({
      path: 'local',
      connectionId: 'local-rtc-1',
      machineId: 'machine-1',
      relayInUse: false,
    })
    const local = new RecordingConnector(localSession)
    const publicP2p = new RecordingConnector(new Error('public should not run'))
    const managed = new RecordingConnector(new Error('managed should not run'))
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({ local, publicP2p, managed })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      managed: { hubSessionId: 'hub-session-1', deviceId: 'app-device-1' },
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })

    expect(result.session).toBe(localSession)
    expect(result.path).toBe('local')
    expect(local.calls).toEqual([{ machineId: 'machine-1', terminalId: 'terminal-1' }])
    expect(publicP2p.calls).toEqual([])
    expect(managed.calls).toEqual([])
    expect(snapshots.map((snapshot) => snapshot.stage)).toEqual(['trying_local', 'connected'])
    expect(snapshots.at(-1)).toMatchObject({ stage: 'connected', path: 'local', relayInUse: false })
  })

  it('tries public_p2p after local fails and stops before managed when public_p2p succeeds', async () => {
    const publicSession = new MockRtcSession({
      path: 'public_p2p',
      connectionId: 'public-rtc-1',
      machineId: 'machine-1',
      relayInUse: false,
    })
    const local = new RecordingConnector(new Error('local unreachable'))
    const publicP2p = new RecordingConnector(publicSession)
    const managed = new RecordingConnector(new Error('managed should not run'))
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({ local, publicP2p, managed })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      managed: { hubSessionId: 'hub-session-1', deviceId: 'app-device-1' },
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })

    expect(result.session).toBe(publicSession)
    expect(result.path).toBe('public_p2p')
    expect(publicP2p.calls).toEqual([{
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
    }])
    expect(managed.calls).toEqual([])
    expect(snapshots).toEqual([
      { stage: 'trying_local', path: 'local', message: 'Trying local connection' },
      { stage: 'trying_public_p2p', path: 'public_p2p', message: 'Trying public P2P connection' },
      { stage: 'connected', path: 'public_p2p', relayInUse: false, message: 'Connected' },
    ])
  })

  it('tries managed after local and public_p2p fail, carrying relay as info not a path', async () => {
    const managedSession = new MockRtcSession({
      path: 'managed',
      connectionId: 'managed-rtc-1',
      machineId: 'machine-1',
      relayInUse: true,
    })
    const local = new RecordingConnector(new Error('local unreachable'))
    const publicP2p = new RecordingConnector(new Error('public P2P timed out'))
    const managed = new RecordingConnector(managedSession)
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({ local, publicP2p, managed })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      managed: { hubSessionId: 'hub-session-1', deviceId: 'app-device-1' },
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })

    expect(result.session).toBe(managedSession)
    expect(result.path).toBe('managed')
    expect(result.relayInUse).toBe(true)
    expect(managed.calls).toEqual([{
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      hubSessionId: 'hub-session-1',
      deviceId: 'app-device-1',
    }])
    expect(snapshots.map((snapshot) => snapshot.stage)).toEqual([
      'trying_local',
      'trying_public_p2p',
      'trying_managed',
      'connected',
    ])
    expect(snapshots.at(-1)).toEqual({
      stage: 'connected',
      path: 'managed',
      relayInUse: true,
      message: 'Connected',
    })
    expect(JSON.stringify(snapshots)).not.toMatch(/"path":"relay"|paid_relay|managed_p2p|anonymous_p2p/)
  })

  it('rejects relay reported by public_p2p and falls back to managed', async () => {
    const publicSession = new MockRtcSession({
      path: 'public_p2p',
      connectionId: 'public-rtc-1',
      machineId: 'machine-1',
      relayInUse: true,
    })
    const managedSession = new MockRtcSession({
      path: 'managed',
      connectionId: 'managed-rtc-1',
      machineId: 'machine-1',
      relayInUse: true,
    })
    const local = new RecordingConnector(new Error('local unreachable'))
    const publicP2p = new RecordingConnector(publicSession)
    const managed = new RecordingConnector(managedSession)
    const orchestrator = createConnectionOrchestrator({ local, publicP2p, managed })

    const result = await orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      managed: { hubSessionId: 'hub-session-1', deviceId: 'app-device-1' },
    })

    expect(result.session).toBe(managedSession)
    expect(result.path).toBe('managed')
    expect(publicSession.disconnectCalls).toBe(1)
  })

  it('reports failure after all paths fail without exposing non-RtcSession runtime objects', async () => {
    const local = new RecordingConnector(new Error('local unreachable'))
    const publicP2p = new RecordingConnector(new Error('public P2P timed out'))
    const managed = new RecordingConnector(new Error('managed unavailable'))
    const snapshots: ConnectionAttemptSnapshot[] = []
    const orchestrator = createConnectionOrchestrator({ local, publicP2p, managed })

    await expect(orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      managed: { hubSessionId: 'hub-session-1', deviceId: 'app-device-1' },
      onSnapshot: (snapshot) => snapshots.push(snapshot),
    })).rejects.toThrow(/all connection paths failed/i)

    expect(snapshots.at(-1)).toEqual({
      stage: 'failed',
      message: 'All connection paths failed',
      errors: [
        { path: 'local', message: 'local unreachable' },
        { path: 'public_p2p', message: 'public P2P timed out' },
        { path: 'managed', message: 'managed unavailable' },
      ],
    })
  })

  it('stops on abort instead of falling through to later paths', async () => {
    const controller = new AbortController()
    const local = new RecordingConnector(async () => {
      controller.abort(new Error('user canceled connection'))
      return new MockRtcSession({
        path: 'local',
        connectionId: 'local-rtc-1',
        machineId: 'machine-1',
        relayInUse: false,
      })
    })
    const publicP2p = new RecordingConnector(new Error('public should not run after abort'))
    const managed = new RecordingConnector(new Error('managed should not run after abort'))
    const orchestrator = createConnectionOrchestrator({ local, publicP2p, managed })

    await expect(orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      managed: { hubSessionId: 'hub-session-1', deviceId: 'app-device-1' },
    }, { signal: controller.signal })).rejects.toThrow(/user canceled connection/i)

    expect(publicP2p.calls).toEqual([])
    expect(managed.calls).toEqual([])
  })

  it('disconnects a created session when post-connect validation fails', async () => {
    const mismatchedSession = new MockRtcSession({
      path: 'managed',
      connectionId: 'managed-rtc-1',
      machineId: 'machine-1',
      relayInUse: false,
    })
    const local = new RecordingConnector(mismatchedSession)
    const publicP2p = new RecordingConnector(new Error('public unavailable'))
    const managed = new RecordingConnector(new Error('managed unavailable'))
    const orchestrator = createConnectionOrchestrator({ local, publicP2p, managed })

    await expect(orchestrator.connect({
      machineId: 'machine-1',
      terminalId: 'terminal-1',
      machinePublicKeyFingerprint: 'sha256:machine',
      managed: { hubSessionId: 'hub-session-1', deviceId: 'app-device-1' },
    })).rejects.toThrow(/all connection paths failed/i)

    expect(mismatchedSession.disconnectCalls).toBe(1)
  })

  it('keeps orchestration above signaling/runtime adapters', () => {
    expect(source).not.toMatch(/RTCPeerConnection|RTCDataChannel|WebSocket|fetch\(|XMLHttpRequest/)
    expect(source).not.toMatch(/paid_relay|managed_p2p|anonymous_p2p|"relay"|'relay'/)
    expect(source).not.toMatch(/openTerminal\(|openApi\(|openFileTransfer\(|subscribeEvents\(/)
  })
})

class RecordingConnector<TInput extends RtcConnectionTarget> implements RtcConnector<TInput> {
  readonly calls: TInput[] = []

  constructor(private readonly result: RtcSession | Error | (() => Promise<RtcSession>)) {}

  async connect(input: TInput): Promise<RtcSession> {
    this.calls.push(input)
    if (this.result instanceof Error) throw this.result
    if (typeof this.result === 'function') return this.result()
    return this.result
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
