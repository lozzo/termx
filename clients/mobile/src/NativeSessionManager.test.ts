import { create } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'
import type { ProtoClientSession, ProtoClientSubscription, ProtoResourceStream } from '@anytty/ui'
import { EndpointSessionStampSchema, type ResourceHandle } from '../../ui/src/generated/apipb/common_pb'
import { CommandEnvelopeSchema, type CommandEnvelope, type EventEnvelope, type ResultEnvelope } from '../../ui/src/generated/apipb/application_pb'
import { ConnectionCandidateType, ConnectionObservedPath, ConnectionRouteKind, ConnectionSnapshotSchema, type ConnectionSnapshot } from '../../ui/src/generated/bindingpb/client_binding_pb'
import { NativeSessionManager } from './NativeSessionManager'

type ProtoClientSessionCloseHandler = Parameters<ProtoClientSession['subscribeClosed']>[0]
type ProtoClientSessionCloseError = Parameters<ProtoClientSessionCloseHandler>[0]

describe('NativeSessionManager', () => {
  it('shares one Go session across independent UI leases', async () => {
    const session = fakeSession()
    const connect = vi.fn(async () => session)
    const manager = new NativeSessionManager('daemon-a', { connect })

    const workspace = await manager.lease()
    const transfer = await manager.get()

    expect(connect).toHaveBeenCalledTimes(1)
    await transfer.close()
    expect(workspace.isAlive()).toBe(true)
    expect(session.close).not.toHaveBeenCalled()

    await manager.reset()
    expect(workspace.isAlive()).toBe(false)
    expect(session.close).toHaveBeenCalledTimes(1)
  })

  it('reuses the device DataChannel after every terminal UI lease has closed', async () => {
    const session = fakeSession()
    const connect = vi.fn(async () => session)
    const manager = new NativeSessionManager('daemon-a', { connect })

    const inventory = await manager.get()
    const firstTerminal = await manager.lease()
    await inventory.close()
    await firstTerminal.close()

    const reopenedTerminal = await manager.lease()

    expect(connect).toHaveBeenCalledTimes(1)
    expect(reopenedTerminal.isAlive()).toBe(true)
    expect(session.close).not.toHaveBeenCalled()
  })

  it('projects a pooled relay connection until it is explicitly reset', async () => {
    const session = fakeSession(1n, create(ConnectionSnapshotSchema, {
      routeId: 'cloud-primary',
      routeKind: ConnectionRouteKind.CLOUD,
      observedPath: ConnectionObservedPath.SINGLE_RELAY,
      localCandidateType: ConnectionCandidateType.RELAY,
      connected: true,
    }))
    const manager = new NativeSessionManager('daemon-a', { connect: vi.fn(async () => session) })
    const listener = vi.fn()
    manager.connectionState.subscribe(listener)

    const workspace = await manager.lease()
    await workspace.close()

    expect(manager.connectionState.getSnapshot()).toMatchObject({
      phase: 'connected',
      relayInUse: true,
      connectionInfo: { path: 'hub', observedPath: 'single_relay' },
    })
    expect(session.close).not.toHaveBeenCalled()

    await manager.reset()

    expect(manager.connectionState.getSnapshot()).toMatchObject({ phase: 'idle', relayInUse: false })
    expect(session.close).toHaveBeenCalledTimes(1)
    expect(listener).toHaveBeenCalled()
  })

  it('does not report a reconnect phase when a new UI lease reuses the pooled session', async () => {
    const session = fakeSession(1n, create(ConnectionSnapshotSchema, {
      routeId: 'direct-primary',
      routeKind: ConnectionRouteKind.DIRECT,
      observedPath: ConnectionObservedPath.DIRECT,
      connected: true,
    }))
    const connect = vi.fn(async () => session)
    const manager = new NativeSessionManager('daemon-a', { connect })
    const initial = await manager.get()
    await initial.close()
    const phases: string[] = []

    const reopened = await manager.get({ onConnectionState: (snapshot) => phases.push(snapshot.phase) })

    expect(connect).toHaveBeenCalledTimes(1)
    expect(reopened.isAlive()).toBe(true)
    expect(phases.length).toBeGreaterThan(0)
    expect(phases.every((phase) => phase === 'connected')).toBe(true)
  })

  it('opens a fresh Go session after the owned session becomes stale', async () => {
    const first = fakeSession()
    const second = fakeSession(2n)
    const connect = vi.fn()
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce(second)
    const manager = new NativeSessionManager('daemon-a', { connect })

    await manager.get()
    first.markDead()
    const current = await manager.get()

    expect(connect).toHaveBeenCalledTimes(2)
    expect(current.stamp.generation).toBe(2n)
  })

  it('evicts an asynchronously closed session and preserves its structured failure', async () => {
    const first = fakeSession()
    const second = fakeSession(2n)
    const connect = vi.fn()
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce(second)
    const manager = new NativeSessionManager('daemon-a', { connect })
    await manager.get()
    const failure = Object.assign(new Error('daemon blocked'), {
      code: 'daemon_blocked',
      retryable: true,
    })

    first.fail(failure)

    expect(connect).toHaveBeenCalledTimes(1)
    expect(manager.connectionState.getSnapshot()).toMatchObject({
      phase: 'failed',
      statusText: 'daemon blocked',
      error: { message: 'daemon blocked', code: 'daemon_blocked', retryable: true },
    })
    expect(manager.connectionState.getSnapshot().error).toBe(failure)

    const current = await manager.get()
    expect(connect).toHaveBeenCalledTimes(2)
    expect(current.stamp.generation).toBe(2n)
  })

  it('keeps structured connection failures in connection-state callbacks', async () => {
    const failure = Object.assign(new Error('daemon deleted'), {
      code: 'daemon_deleted',
      retryable: false,
    })
    const connect = vi.fn(async (_input, options) => {
      options?.onConnectionState?.({
        machineId: 'daemon-a',
        phase: 'failed',
        statusText: failure.message,
        relayInUse: false,
        error: failure,
      })
      throw failure
    })
    const manager = new NativeSessionManager('daemon-a', { connect })
    const snapshots: Array<{ error?: Error }> = []

    await expect(manager.get({ onConnectionState: (snapshot) => snapshots.push(snapshot) })).rejects.toBe(failure)

    expect(snapshots.some((snapshot) => snapshot.error === failure)).toBe(true)
    expect(manager.connectionState.getSnapshot().error).toBe(failure)
  })

  it('does not publish an asynchronous failure for an explicit reset', async () => {
    const session = fakeSession()
    const manager = new NativeSessionManager('daemon-a', { connect: vi.fn(async () => session) })
    const phases: string[] = []
    manager.connectionState.subscribe(() => phases.push(manager.connectionState.getSnapshot().phase))
    await manager.get()

    await manager.reset()
    session.fail(Object.assign(new Error('late close'), { code: 'unavailable' }))

    expect(manager.connectionState.getSnapshot()).toMatchObject({ phase: 'idle', error: null })
    expect(phases).not.toContain('failed')
  })

  it('lets one lease cancel its wait without cancelling the shared connect', async () => {
    let resolveConnect!: (session: ProtoClientSession) => void
    let managerSignal: AbortSignal | undefined
    const connect = vi.fn((_input, options) => {
      managerSignal = options?.signal
      return new Promise<ProtoClientSession>((resolve) => { resolveConnect = resolve })
    })
    const manager = new NativeSessionManager('daemon-a', { connect })
    const controller = new AbortController()

    const cancelled = manager.get({ signal: controller.signal })
    const workspace = manager.lease()
    controller.abort(new DOMException('Aborted', 'AbortError'))
    resolveConnect(fakeSession())

    await expect(cancelled).rejects.toMatchObject({ name: 'AbortError' })
    await expect(workspace).resolves.toMatchObject({ stamp: { endpointId: 'daemon-a' } })
    expect(connect).toHaveBeenCalledTimes(1)
    expect(managerSignal?.aborted).toBe(false)
  })

  it('does not block generation reset on an old pending connect', async () => {
    let managerSignal: AbortSignal | undefined
    const connect = vi.fn((_input, options) => {
      managerSignal = options?.signal
      return new Promise<ProtoClientSession>(() => {})
    })
    const manager = new NativeSessionManager('daemon-a', { connect })
    void manager.get()

    await expect(manager.reset()).resolves.toBeUndefined()
    expect(managerSignal?.aborted).toBe(true)
  })

  it('keeps the full managed connection window bounded without truncating the Go ICE deadline', async () => {
    vi.useFakeTimers()
    let managerSignal: AbortSignal | undefined
    const connect = vi.fn((_input, options) => {
      managerSignal = options?.signal
      return new Promise<ProtoClientSession>((_resolve, reject) => {
        options?.signal?.addEventListener('abort', () => reject(options.signal?.reason), { once: true })
      })
    })
    const manager = new NativeSessionManager('daemon-a', { connect })
    const pending = manager.get()
    const settled = pending.then(() => null, (error: unknown) => error)

    try {
      await vi.advanceTimersByTimeAsync(44_999)
      expect(managerSignal?.aborted).toBe(false)
      await vi.advanceTimersByTimeAsync(1)
      expect(await settled).toMatchObject({ message: 'client session timed out' })
      expect(managerSignal?.aborted).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not block generation reset on an unresponsive old session close', async () => {
    const session = fakeSession()
    session.close = vi.fn(() => new Promise<void>(() => {}))
    const manager = new NativeSessionManager('daemon-a', { connect: vi.fn(async () => session) })
    await manager.get()

    await expect(manager.reset()).resolves.toBeUndefined()
    expect(session.close).toHaveBeenCalledTimes(1)
  })

  it('reports a closed lease through rejected async operations', async () => {
    const manager = new NativeSessionManager('daemon-a', { connect: vi.fn(async () => fakeSession()) })
    const lease = await manager.get()
    await manager.reset()

    await expect(lease.execute(create(CommandEnvelopeSchema))).rejects.toThrow('Proto session lease is closed')
  })

  it('forwards live connection snapshot sampling through the UI lease', async () => {
    const session = fakeSession()
    session.getConnectionSnapshot = vi.fn()
      .mockResolvedValueOnce(create(ConnectionSnapshotSchema, { sampledAtUnixNano: 10n, bytesSent: 20n }))
      .mockResolvedValueOnce(create(ConnectionSnapshotSchema, { sampledAtUnixNano: 30n, bytesSent: 40n }))
    const manager = new NativeSessionManager('daemon-a', { connect: vi.fn(async () => session) })
    const lease = await manager.get()

    await expect(lease.getConnectionSnapshot?.()).resolves.toMatchObject({ sampledAtUnixNano: 10n, bytesSent: 20n })
    await expect(lease.getConnectionSnapshot?.()).resolves.toMatchObject({ sampledAtUnixNano: 30n, bytesSent: 40n })
    expect(session.getConnectionSnapshot).toHaveBeenCalledTimes(2)
  })
})

function fakeSession(generation = 1n, connection?: ConnectionSnapshot): ProtoClientSession & {
  markDead(): void
  fail(error: ProtoClientSessionCloseError): void
} {
  let alive = true
  const closeHandlers = new Set<ProtoClientSessionCloseHandler>()
  return {
    stamp: create(EndpointSessionStampSchema, { endpointId: 'daemon-a', routeId: 'direct', generation }),
    ...(connection ? { connection } : {}),
    execute: vi.fn(async (_command: CommandEnvelope) => ({} as ResultEnvelope)),
    subscribeEvents: vi.fn((_handler: (event: EventEnvelope) => void): ProtoClientSubscription => ({ close() {} })),
    subscribeClosed: vi.fn((handler): ProtoClientSubscription => {
      closeHandlers.add(handler)
      return { close: () => closeHandlers.delete(handler) }
    }),
    openResourceStream: vi.fn(async (_resource: ResourceHandle): Promise<ProtoResourceStream> => { throw new Error('unused') }),
    isAlive: () => alive,
    close: vi.fn(async () => {
      alive = false
      closeHandlers.clear()
    }),
    markDead: () => { alive = false },
    fail: (error) => {
      if (!alive) return
      alive = false
      const handlers = [...closeHandlers]
      closeHandlers.clear()
      handlers.forEach((handler) => handler(error))
    },
  }
}
