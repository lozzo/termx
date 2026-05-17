import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect, useState } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import { createConnectionOrchestrator, type ConnectionAttemptSnapshot } from '../connection/connectionOrchestrator'
import { createMachineStore } from '../state/machineStore'
import type { HubApi } from '../api/hubApi'
import { parsePairingPayload } from '../state/pairingPayload'
import { MachineBrowserShell } from '../app/MachineBrowserShell'
import { useFileManager } from '../files/useFileManager'
import { useTerminalSession } from '../terminal/useTerminalSession'
import type {
  ConnectionCapabilities,
  ConnectionInfo,
  RtcEvent,
  RtcJsonRpcChannel,
  RtcSession,
  RtcSubscription,
} from '../core/transport'
import { MockRtcTerminalSession } from '../test/mockRtcTerminalSession'

describe('APP connection e2e harness', () => {
  afterEach(() => {
    cleanup()
  })

  it('loads a scanned machine and connects by racing hub URLs with one RtcSession runtime', async () => {
    const storage = new MemoryStorage()
    const store = createMachineStore({
      storage,
      now: () => new Date('2026-05-03T12:18:00.000Z'),
    })
    store.saveFromPairingPayload(parsePairingPayload(JSON.stringify({
      type: 'termx_pair',
      schema_version: 4,
      machine: {
        id: 'machine-public',
        name: 'Public Host',
        hostname: 'public-host-agent',
      },
      local: {
        hub_urls: ['http://127.0.0.1:18988', 'http://192.168.1.30:18988', 'http://114.66.58.243:18988'],
      },
      hub: {
        hub_urls: [],
        web_control: 'http://114.66.58.243:12306',
      },
      pairing: {
        session_id: 'pair-session-1',
        secret: 'pair-secret-1',
      },
      bootstrap: {},
      preferred_path: 'local',
    })))
    const machines = store.listMachines()
    const hubSession = new HubE2ESession('machine-public', 'local')
    const hubConnector = new RecordingHubConnector(hubSession)
    const orchestrator = createConnectionOrchestrator({
      hubApiFactory: (hubUrl) => new MockHubApi(hubUrl),
      hubRtcConnectorFactory: () => hubConnector,
    })
    const snapshots: ConnectionAttemptSnapshot[] = []

    render(
      <MachineBrowserShell
        machines={machines.map((machine) => ({
          ...machine,
          state: 'online',
          terminalCount: 1,
          lastSeenAt: '2026-05-03T12:17:00.000Z',
        }))}
        authState="signed_in"
        onAddMachine={() => {}}
        onScanMachine={() => {}}
        onStartConnection={(machine) => {
          const stored = requireStoredMachine(store, machine.machineId)
          expect(stored.addresses.local).toContain('http://127.0.0.1:18988')
          expect(stored.addresses.public).toContain('http://114.66.58.243:18988')
          expect(stored.endpoints.webControl).toBe('http://114.66.58.243:12306')
          expect(stored.pairing?.sessionId).toBe('pair-session-1')
          return orchestrator.connect({
            machineId: machine.machineId,
            terminalId: 'terminal-1',
            sessionToken: 'session-token-1',
            policy: 'app_local_preferred',
            endpoints: stored.addresses.public.map((url) => ({
              url,
              kind: 'local' as const,
              scope: 'public_mapping' as const,
              source: 'pair_qr' as const,
            })),
            onSnapshot: (snapshot) => snapshots.push(snapshot),
          }).then((result) => ({
            stage: 'connected',
            path: result.path,
            relayInUse: result.relayInUse,
            message: 'Connected to terminal runtime.',
          }))
        }}
      />,
    )

    expect(screen.getByTestId('termx-machine-list')).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: /connect to public host/i }))

    await waitFor(() => expect(screen.getByText('Connected')).toBeTruthy())
    expect(screen.getByText('Connected to terminal runtime.')).toBeTruthy()
    expect(screen.queryByText('Relay active')).toBeNull()
    expect(hubConnector.calls).toEqual([{
      machineId: 'machine-public',
      terminalId: 'terminal-1',
      sessionToken: 'session-token-1',
      path: 'local',
    }])
    expect(snapshots.map((snapshot) => snapshot.stage)).toEqual([
      'trying_local',
      'connected',
    ])

    render(
      <RuntimeConsumer
        machineId="machine-public"
        terminalId="terminal-1"
        session={hubSession}
      />,
    )

    await waitFor(() => expect(hubSession.openedLabels).toContain('terminal:terminal-1'))
    await waitFor(() => expect(hubSession.subscribedEvents).toBe(1))
    hubSession.emitTerminalScreenUpdate('terminal-1', 'hello from hub')
    hubSession.emitEvent({ type: 'heartbeat', payload: { ok: true } })
    await waitFor(() => expect(screen.getByTestId('runtime-terminal').textContent).toContain('hello from hub'))
    await waitFor(() => expect(screen.getByTestId('runtime-files').textContent).toContain('remote.log'))
    await waitFor(() => expect(screen.getByTestId('runtime-events').textContent).toContain('heartbeat'))
    expect(hubSession.openedLabels).toContain('terminal:terminal-1')
    expect(hubSession.openApiCount).toBeGreaterThan(0)
    expect(hubSession.subscribedEvents).toBe(1)
    expect(document.body.textContent).not.toMatch(/\brelay path\b|paid relay|hub p2p|anonymous p2p|workspace|pane|tmux/i)
  })
})

class RecordingHubConnector {
  readonly calls: unknown[] = []

  constructor(private readonly result: RtcSession | Error) {}

  async connect(input: unknown): Promise<RtcSession> {
    this.calls.push(input)
    if (this.result instanceof Error) throw this.result
    return this.result
  }
}

class MockHubApi implements HubApi {
  constructor(readonly hubUrl: string) {}

  async getSessionIce(): ReturnType<HubApi['getSessionIce']> {
    throw new Error('getSessionIce is not used by e2e orchestrator harness')
  }

  async createSession(): ReturnType<HubApi['createSession']> {
    throw new Error('createSession is not used by e2e orchestrator harness')
  }

  async pollSessionAnswer(): ReturnType<HubApi['pollSessionAnswer']> {
    throw new Error('pollSessionAnswer is not used by e2e orchestrator harness')
  }

  async pair(): ReturnType<HubApi['pair']> {
    throw new Error('pair is not used by e2e orchestrator harness')
  }
}

class MemoryStorage implements Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> {
  private readonly values = new Map<string, string>()

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
  }

  removeItem(key: string): void {
    this.values.delete(key)
  }
}

function requireStoredMachine(store: ReturnType<typeof createMachineStore>, machineId: string) {
  const machine = store.getMachine(machineId)
  if (!machine) throw new Error(`stored machine ${machineId} is required`)
  return machine
}

function RuntimeConsumer({
  machineId,
  terminalId,
  session,
}: {
  machineId: string
  terminalId: string
  session: RtcSession
}) {
  const terminal = useTerminalSession({ machineId, terminalId, session })
  const files = useFileManager({ machineId, terminalId, session, initialPath: '/' })
  const [events, setEvents] = useState<RtcEvent[]>([])

  useEffect(() => {
    return session.subscribeEvents((event) => {
      setEvents((current) => [...current, event])
    }).close
  }, [session])

  return (
    <section>
      <div data-testid="runtime-terminal">{terminal.terminalText}</div>
      <div data-testid="runtime-files">{files.entries.map((entry) => entry.name).join(',')}</div>
      <div data-testid="runtime-events">{events.map((event) => event.type).join(',')}</div>
    </section>
  )
}

class HubE2ESession extends MockRtcTerminalSession {
  openApiCount = 0
  subscribedEvents = 0
  private readonly eventHandlers = new Set<(event: RtcEvent) => void>()

  constructor(machineId: string, private readonly connectionPath: ConnectionInfo['path'] = 'hub') {
    super(machineId, connectionPath)
  }

  override async getConnectionInfo(): Promise<ConnectionInfo> {
    return {
      path: this.connectionPath,
      connectionId: `${this.connectionPath}-rtc-1`,
      machineId: 'machine-public',
      terminalId: 'terminal-1',
      relayInUse: this.connectionPath === 'hub',
    }
  }

  override async openApi(): Promise<RtcJsonRpcChannel> {
    this.openApiCount += 1
    return {
      async request<TResponse>(method: string, params?: unknown): Promise<TResponse> {
        if (method === 'POST' && isFileListParams(params)) {
          return {
            path: params.params.path,
            parent: '',
            total: 1,
            entries: [{ name: 'remote.log', type: 'file', size: 32 }],
          } as TResponse
        }
        return { ok: true } as TResponse
      },
      close() {},
    }
  }

  override subscribeEvents(handler: (event: RtcEvent) => void): RtcSubscription {
    this.subscribedEvents += 1
    this.eventHandlers.add(handler)
    return {
      close: () => this.eventHandlers.delete(handler),
    }
  }

  emitEvent(event: RtcEvent): void {
    for (const handler of this.eventHandlers) handler(event)
  }

  override async getCapabilities(): Promise<ConnectionCapabilities> {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: this.connectionPath === 'hub',
    }
  }
}

function isFileListParams(params: unknown): params is { path: '/files/list'; params: { path: string } } {
  if (!params || typeof params !== 'object') return false
  const record = params as Record<string, unknown>
  return record.path === '/files/list' && typeof record.params === 'object' && record.params !== null
}
