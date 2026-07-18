import { create } from '@bufbuild/protobuf'
import type { ProtoClientSession, ProtoResourceStream } from '../../ui/src/core/protoClientSession'
import * as TermxApiApplication from '../../ui/src/generated/apipb/application_pb'
import * as TermxApiCommon from '../../ui/src/generated/apipb/common_pb'
import * as TermxApiFile from '../../ui/src/generated/apipb/file_pb'
import { afterEach, describe, expect, it, vi } from 'vitest'

describe('NativeFileTransferStore', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('waits for pause detach and serializes duplicate resume requests', async () => {
    const detach = deferred<void>()
    const detachStarted = deferred<void>()
    const harness = await createPausedUpload(async () => {
      detachStarted.resolve()
      await detach.promise
    })
    const firstResume = harness.store.resumeTransfer(harness.id)
    const duplicateResume = harness.store.resumeTransfer(harness.id)
    await detachStarted.promise
    expect(harness.openCommands).toHaveLength(1)
    detach.resolve()
    await Promise.all([firstResume, duplicateResume])
    expect(harness.openCommands).toHaveLength(2)
    expect(harness.openCommands[1]?.resume?.opaqueToken).toEqual(harness.resumeToken)
  })

  it('keeps a failed detach fence and does not reopen on resume', async () => {
    const harness = await createPausedUpload(async () => { throw new Error('detach failed') })
    await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'failed')
    await harness.store.resumeTransfer(harness.id)
    expect(harness.openCommands).toHaveLength(1)
    expect(harness.store.getSnapshot().transfers[0]?.error).toContain('detach failed')
  })

  it('does not resume after cancel wins while detach is pending', async () => {
    const detach = deferred<void>()
    const detachStarted = deferred<void>()
    const harness = await createPausedUpload(async () => {
      detachStarted.resolve()
      await detach.promise
    })
    const resume = harness.store.resumeTransfer(harness.id)
    await detachStarted.promise
    harness.store.cancelTransfer(harness.id)
    detach.resolve()
    await resume
    await waitFor(() => harness.counters.cancelCommands === 1 && harness.counters.sessionCloses === 1)
    expect(harness.openCommands).toHaveLength(1)
    expect(harness.store.getSnapshot().transfers[0]?.status).toBe('cancelled')
  })

  it('releases a late upload resource when pause wins during FileUploadOpen', async () => {
    const openGate = deferred<void>()
    const openStarted = deferred<void>()
    const harness = await createUploadHarness({
      releaseResource: async () => undefined,
      beforeOpenResult: async () => {
        openStarted.resolve()
        await openGate.promise
      },
    })
    await openStarted.promise
    harness.store.pauseTransfer(harness.id)
    openGate.resolve()
    await waitFor(() => harness.counters.releaseCommands === 1)
    expect(harness.counters.openStreamCalls).toBe(0)
    expect(harness.statuses).not.toContain('transferring')
    expect(harness.store.getSnapshot().transfers[0]?.status).toBe('paused')
  })

  it('closes a late stream and cancels its resource when cancel wins during stream open', async () => {
    const streamGate = deferred<void>()
    const streamStarted = deferred<void>()
    const harness = await createUploadHarness({
      releaseResource: async () => undefined,
      beforeStreamResult: async () => {
        streamStarted.resolve()
        await streamGate.promise
      },
    })
    await streamStarted.promise
    harness.store.cancelTransfer(harness.id)
    await waitFor(() => harness.counters.cancelCommands === 1 && harness.counters.sessionCloses === 1)
    expect(harness.statuses).not.toContain('transferring')
    expect(harness.store.getSnapshot().transfers[0]?.status).toBe('cancelled')
    streamGate.resolve()
    await waitFor(() => harness.counters.streamCloses === 1)
  })

  it('keeps the failed detach owner until a confirmed destructive cancel', async () => {
    const harness = await createPausedUpload(async () => { throw new Error('detach failed') })
    await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'failed')
    expect(harness.counters.sessionCloses).toBe(0)
    harness.store.cancelTransfer(harness.id)
    await waitFor(() => harness.counters.cancelCommands === 1 && harness.counters.sessionCloses === 1)
  })

  it('upgrades a failing in-flight detach to destructive cancel without owner loss', async () => {
    const detach = deferred<void>()
    const detachStarted = deferred<void>()
    const harness = await createPausedUpload(async () => {
      detachStarted.resolve()
      await detach.promise
      throw new Error('detach failed')
    })
    await detachStarted.promise
    harness.store.cancelTransfer(harness.id)
    detach.resolve()
    await waitFor(() => harness.counters.cancelCommands === 1 && harness.counters.sessionCloses === 1)
    expect(harness.store.getSnapshot().transfers[0]?.status).toBe('cancelled')
  })

  it('uses a fresh session to cancel an already detached paused upload', async () => {
    const harness = await createPausedUpload(async () => undefined)
    await waitFor(() => harness.counters.releaseCommands === 1 && harness.counters.sessionCloses === 1)
    harness.store.cancelTransfer(harness.id)
    await waitFor(() => harness.counters.cancelCommands === 1 && harness.counters.sessionCloses === 2)
    expect(harness.store.getSnapshot().transfers[0]?.status).toBe('cancelled')
	expect(harness.cancelCredentials).toEqual(['upload_resume'])
  })

  it('keeps the owner reachable when cancel arrives during session close', async () => {
	const closeGate = deferred<void>()
	const closeStarted = deferred<void>()
	const harness = await createUploadHarness({
	  releaseResource: async () => undefined,
	  beforeSessionClose: async () => {
		closeStarted.resolve()
		await closeGate.promise
	  },
	})
	await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'transferring')
	harness.store.pauseTransfer(harness.id)
	await closeStarted.promise
	harness.store.dismissTransfer(harness.id)
	expect(harness.store.getSnapshot().transfers).toHaveLength(1)
	closeGate.resolve()
	await waitFor(() => harness.store.getSnapshot().transfers.length === 0)
	expect(harness.cancelCredentials).toEqual(['upload_resume'])
  })

  it('retries fresh-session cleanup after the first resolver failure', async () => {
	const closeGate = deferred<void>()
	const closeStarted = deferred<void>()
	const harness = await createUploadHarness({
	  releaseResource: async () => undefined,
	  freshResolverFailures: 1,
	  beforeSessionClose: async () => {
		closeStarted.resolve()
		await closeGate.promise
	  },
	})
	await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'transferring')
	harness.store.pauseTransfer(harness.id)
	await closeStarted.promise
	harness.store.cancelTransfer(harness.id)
	closeGate.resolve()
	await waitFor(() => harness.resolverCalls() === 2)
	expect(harness.counters.cancelCommands).toBe(0)
	harness.store.cancelTransfer(harness.id)
	await waitFor(() => harness.counters.cancelCommands === 1)
	expect(harness.resolverCalls()).toBe(3)
	expect(harness.cancelCredentials).toEqual(['upload_resume'])
  })

  it('dismisses only after in-flight detach is upgraded and destructive cancel succeeds', async () => {
    const detach = deferred<void>()
    const harness = await createPausedUpload(async () => { await detach.promise })
    harness.store.dismissTransfer(harness.id)
    expect(harness.store.getSnapshot().transfers).toHaveLength(1)
    detach.resolve()
    await waitFor(() => harness.store.getSnapshot().transfers.length === 0)
    expect(harness.counters.cancelCommands).toBe(1)
  })

  it('keeps a dismissed transfer reachable when destructive cleanup fails', async () => {
    const detach = deferred<void>()
    const harness = await createUploadHarness({
      releaseResource: async () => { await detach.promise },
      cancelBehavior: 'false',
    })
    await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'transferring')
    harness.store.pauseTransfer(harness.id)
    harness.store.dismissTransfer(harness.id)
    detach.resolve()
    await waitFor(() => harness.counters.cancelCommands === 1)
    expect(harness.store.getSnapshot().transfers).toHaveLength(1)
    expect(harness.counters.sessionCloses).toBe(0)
  })

  it.each(['false', 'throw'] as const)('retains destructive owner when cancel result is %s', async (cancelBehavior) => {
    const harness = await createUploadHarness({
      releaseResource: async () => { throw new Error('detach failed') },
      cancelBehavior,
    })
    await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'transferring')
    harness.store.pauseTransfer(harness.id)
    await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'failed')
    harness.store.cancelTransfer(harness.id)
    await waitFor(() => harness.counters.cancelCommands === 1)
    expect(harness.counters.sessionCloses).toBe(0)
  })

  it('closes a live failed-cleanup lease before replacing it with a fresh session', async () => {
	const harness = await createUploadHarness({
	  releaseResource: async () => { throw new Error('detach failed') },
	  cancelBehavior: 'false_then_success',
	})
	await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'transferring')
	harness.store.pauseTransfer(harness.id)
	await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'failed')
	harness.store.cancelTransfer(harness.id)
	await waitFor(() => harness.counters.cancelCommands === 2 && harness.counters.sessionCloses === 2)
	expect(harness.cancelCredentials).toEqual(['transfer', 'upload_resume'])
  })

  it('bounds pause while session resolution never returns and ignores the late session', async () => {
    vi.stubGlobal('self', globalThis)
    const { NativeFileTransferStore } = await import('./NativeFileTransferStore')
    const lateSession = deferred<ProtoClientSession>()
    let resolverCalls = 0
    let lateSessionCloses = 0
    const store = new NativeFileTransferStore()
    store.setSessionResolver(async () => {
      resolverCalls += 1
      if (resolverCalls === 1) return await lateSession.promise
      throw new Error('second attempt reached')
    })
    store.startUpload('studio', 'content://upload', 'demo.bin', 8, '/tmp')
    const transfer = store.getSnapshot().transfers[0]
    if (!transfer) throw new Error('transfer was not created')
    store.pauseTransfer(transfer.id)
    await store.resumeTransfer(transfer.id)
    expect(resolverCalls).toBe(2)
    lateSession.resolve({
      stamp: create(TermxApiCommon.EndpointSessionStampSchema, { endpointId: 'studio', routeId: 'cloud', generation: 1n }),
      isAlive: () => true,
      close: async () => { lateSessionCloses += 1 },
      subscribeEvents: () => ({ close() {} }),
      openResourceStream: async () => { throw new Error('unused') },
      execute: async () => { throw new Error('unused') },
    })
    await waitFor(() => lateSessionCloses === 1)
  })
})

async function createPausedUpload(releaseResource: () => Promise<void>) {
  const harness = await createUploadHarness({ releaseResource })
  await waitFor(() => harness.store.getSnapshot().transfers[0]?.status === 'transferring')
  harness.store.pauseTransfer(harness.id)
  return harness
}

async function createUploadHarness(options: {
  releaseResource: () => Promise<void>
  beforeOpenResult?: () => Promise<void>
  beforeStreamResult?: () => Promise<void>
	beforeSessionClose?: () => Promise<void>
	freshResolverFailures?: number
  cancelBehavior?: 'success' | 'false' | 'throw' | 'false_then_success'
}) {
  vi.stubGlobal('self', globalThis)
  const { NativeFileTransferStore } = await import('./NativeFileTransferStore')
  const resumeToken = new Uint8Array([7, 8, 9])
  const resource = create(TermxApiCommon.ResourceHandleSchema, {
    kind: TermxApiCommon.ResourceKind.FILE_TRANSFER,
    opaqueToken: new Uint8Array([1, 2, 3]),
	session: create(TermxApiCommon.EndpointSessionStampSchema, { endpointId: 'studio', routeId: 'cloud', generation: 1n }),
  })
  const counters = { openStreamCalls: 0, releaseCommands: 0, cancelCommands: 0, streamCloses: 0, sessionCloses: 0 }
  const stream: ProtoResourceStream = {
    handle: 1n,
    send: async () => undefined,
    subscribe: () => ({ close() {} }),
    subscribeClosed: () => ({ close() {} }),
    close: async () => { counters.streamCloses += 1 },
  }
  const openCommands: TermxApiFile.FileUploadOpenCommand[] = []
	const cancelCredentials: string[] = []
	let resolverCalls = 0
  const makeSession = (): ProtoClientSession => {
    let alive = true
    return {
      stamp: create(TermxApiCommon.EndpointSessionStampSchema, { endpointId: 'studio', routeId: 'cloud', generation: BigInt(counters.sessionCloses + 1) }),
      isAlive: () => alive,
      close: async () => {
        if (!alive) return
		await options.beforeSessionClose?.()
        alive = false
        counters.sessionCloses += 1
      },
      subscribeEvents: () => ({ close() {} }),
      openResourceStream: async () => {
        counters.openStreamCalls += 1
        await options.beforeStreamResult?.()
        return stream
      },
      execute: async (envelope) => {
      if (envelope.command.case === 'fileUploadOpen') {
        openCommands.push(envelope.command.value)
        if (openCommands.length > 1) throw new Error('resume-open-observed')
        await options.beforeOpenResult?.()
        return create(TermxApiApplication.ResultEnvelopeSchema, {
          result: {
            case: 'fileTransferOpen',
            value: create(TermxApiFile.FileTransferOpenResultSchema, {
              transfer: create(TermxApiFile.FileTransferHandleSchema, {
                resource,
                size: 8n,
                offset: 4n,
                chunkBytes: 4,
                windowBytes: 4n,
                resume: create(TermxApiFile.FileUploadResumeHandleSchema, { opaqueToken: resumeToken }),
              }),
            }),
          },
        })
      }
      if (envelope.command.case === 'releaseResource') {
        counters.releaseCommands += 1
        await options.releaseResource()
        return create(TermxApiApplication.ResultEnvelopeSchema)
      }
      if (envelope.command.case === 'fileTransferCancel') {
        counters.cancelCommands += 1
		cancelCredentials.push(envelope.command.value.transfer ? 'transfer' : envelope.command.value.uploadResume ? 'upload_resume' : 'missing')
        if (options.cancelBehavior === 'throw') throw new Error('cancel transport failed')
		const cancelled = options.cancelBehavior === 'false_then_success' ? counters.cancelCommands > 1 : options.cancelBehavior !== 'false'
        return create(TermxApiApplication.ResultEnvelopeSchema, {
          result: {
            case: 'fileTransferCancel',
			value: create(TermxApiFile.FileTransferCancelResultSchema, { cancelled }),
          },
        })
      }
        throw new Error(`unexpected command ${envelope.command.case}`)
      },
    }
  }
  vi.stubGlobal('fetch', vi.fn((_input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
    init?.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })
  })))
  const store = new NativeFileTransferStore()
  store.setSessionResolver(async () => {
	resolverCalls += 1
	if (resolverCalls > 1 && resolverCalls <= 1 + (options.freshResolverFailures ?? 0)) throw new Error('fresh session unavailable')
	return makeSession()
  })
  const statuses: string[] = []
  store.subscribe(() => {
    const status = store.getSnapshot().transfers[0]?.status
    if (status) statuses.push(status)
  })
  store.startUpload('studio', 'content://upload', 'demo.bin', 8, '/tmp')
  const transfer = store.getSnapshot().transfers[0]
  if (!transfer) throw new Error('transfer was not created')
  return { store, id: transfer.id, openCommands, resumeToken, counters, statuses, cancelCredentials, resolverCalls: () => resolverCalls }
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

async function waitFor(predicate: () => boolean): Promise<void> {
  const deadline = Date.now() + 1_000
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error('condition was not reached')
    await new Promise((resolve) => setTimeout(resolve, 1))
  }
}
