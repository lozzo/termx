import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { ApiErrorCode } from '../../src/generated/apipb/common_pb'
import { CommandEnvelopeSchema } from '../../src/generated/apipb/application_pb'
import { ApplicationEventType, EventSubscribeCommandSchema } from '../../src/generated/apipb/events_pb'
import { StorageKeySchema, StoragePutCommandSchema, StorageScope } from '../../src/generated/apipb/storage_pb'
import {
  EventEnvelopeSchema,
  CredentialRecordSchema,
  ManagedEndpointConfigSchema,
  ManagedRelayMode,
  OpenSessionRequestSchema,
  PlatformResponseSchema,
  SignalingEventsSchema,
  ConnectIntent,
  type EventEnvelope,
  type PlatformRequest,
  type PlatformResponse,
} from '../../src/generated/bindingpb/client_binding_pb'
import {
  ReportConnectionOutcomeResponseSchema,
  ReportPathQualityResponseSchema,
  ResolveEndpointRequestSchema,
  ResolvedEndpointSchema,
  CreateSignalingSessionRequestSchema,
  ManagedRoutePlanSchema,
  RelayLeaseSchema,
} from '../../src/generated/cloudpb/cloud_companion_pb'
import { BrowserWasmPlatform, type BrowserCloudPlatform } from '../../src/binding/browserWasmPlatform'
import { BrowserWasmLifecycle } from '../../src/binding/browserWasmLifecycle'
import { TermxWasmRuntime, loadTermxWasmExports } from '../../src/binding/wasmRuntime'

const endpointId = 'web-spike'
const credentialRef = 'credential:web-spike'

void run().catch((error) => finish({ ok: false, error: error instanceof Error ? error.stack || error.message : String(error) }))

async function run(): Promise<void> {
  await stage('module-loaded')
  const exports = await loadTermxWasmExports({ wasmUrl: '/assets/termx-client.wasm', wasmExecUrl: '/assets/wasm_exec.js' })
  await stage('wasm-loaded')
  const cloud = new SpikeCloudPlatform()
  let activeRuntime: TermxWasmRuntime | null = null
  const eventSink = async (payload: Uint8Array) => {
    if (!activeRuntime) throw new Error('browser WASM generation is suspended')
    await activeRuntime.platformEvent(payload)
  }
  let platform: BrowserWasmPlatform | null = new BrowserWasmPlatform(cloud, eventSink, (value) => { void stage(value) })
  const prepared = await platform.prepareCredential(endpointId, credentialRef)
  await stage('credential-prepared')
  const granted = await postProto('/grant', toBinary(CredentialRecordSchema, prepared))
  const grantRecord = fromBinary(CredentialRecordSchema, granted)
  await platform.bindCredential(endpointId, credentialRef, grantRecord.capabilityGrant)
  await stage('credential-bound')

  const lifecycle = new BrowserWasmLifecycle(async () => {
    const nextPlatform = platform ?? new BrowserWasmPlatform(cloud, eventSink, (value) => { void stage(value) })
    platform = null
    const runtime = await TermxWasmRuntime.create(exports, nextPlatform)
    activeRuntime = runtime
    return runtime
  }, (runtime) => { activeRuntime = runtime })
  lifecycle.attach()
  let runtime = await lifecycle.start()
  await stage('first-runtime-created')
  const first = await openAndProve(runtime)
  await stage('first-runtime-proved')
  window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }))
  window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))
  await lifecycle.whenIdle()
  runtime = lifecycle.current as TermxWasmRuntime
  await stage('second-runtime-created')
  const second = await openAndProve(runtime, false)
  await stage('second-runtime-proved')
  if (second.generation <= first.generation) throw new Error(`WASM generation did not advance: ${first.generation} -> ${second.generation}`)
  await lifecycle.dispose()

  await finish({
    ok: true,
    firstGeneration: first.generation.toString(),
    secondGeneration: second.generation.toString(),
    fingerprint: first.fingerprint,
    observedEvent: first.observedEvent,
  })
}

async function openAndProve(runtime: TermxWasmRuntime, proveEvent = true): Promise<{ generation: bigint; fingerprint: string; observedEvent: boolean }> {
  const events = new BindingEvents(runtime)
  const openOperation = runtime.openSession(toBinary(OpenSessionRequestSchema, create(OpenSessionRequestSchema, {
    requestId: crypto.randomUUID(),
    endpointId,
    intent: ConnectIntent.INTERACTIVE,
    managed: create(ManagedEndpointConfigSchema, {
      targetDeviceId: 'device-web-spike',
      deviceFingerprint: (globalThis as typeof globalThis & { termxDeviceFingerprint: string }).termxDeviceFingerprint,
      credentialRef,
      relayMode: ManagedRelayMode.DIRECT,
    }),
  })))
  const opened = await events.wait((event) => event.event.case === 'openSession' && Number(event.event.value.operationHandle) === openOperation)
  if (opened.event.case !== 'openSession' || opened.event.value.error || !opened.event.value.session) {
    throw new Error(`WASM open session failed: ${opened.event.case === 'openSession' ? opened.event.value.error?.message || 'missing session' : 'unexpected event'}`)
  }
  const session = Number(opened.event.value.sessionHandle)
  const generation = opened.event.value.session.generation
  runtime.release(openOperation)

  let observedEvent = false
  if (proveEvent) {
    const subscription = runtime.execute(session, toBinary(CommandEnvelopeSchema, create(CommandEnvelopeSchema, {
      command: {
        case: 'eventSubscribe',
        value: create(EventSubscribeCommandSchema, { types: [ApplicationEventType.STORAGE_CHANGED], storageAppId: 'web-spike', storageScope: StorageScope.PUBLIC }),
      },
    })))
    const subscribed = await events.wait((event) => event.event.case === 'execute' && Number(event.event.value.operationHandle) === subscription)
    if (subscribed.event.case !== 'execute' || !subscribed.event.value.result?.result.case) throw new Error('WASM event subscribe failed')
    runtime.release(subscription)

    const put = runtime.execute(session, toBinary(CommandEnvelopeSchema, create(CommandEnvelopeSchema, {
      command: {
        case: 'storagePut',
        value: create(StoragePutCommandSchema, {
          key: create(StorageKeySchema, { appId: 'web-spike', scope: StorageScope.PUBLIC, key: 'browser-proof' }),
          value: new TextEncoder().encode('ok'),
        }),
      },
    })))
    let sawPut = false
    for (let attempt = 0; attempt < 4 && (!sawPut || !observedEvent); attempt += 1) {
      const event = await events.next()
      if (event.event.case === 'execute' && Number(event.event.value.operationHandle) === put) sawPut = event.event.value.result?.result.case === 'storagePut'
      if (event.event.case === 'application') observedEvent = event.event.value.event?.event.case === 'storageChanged'
    }
    if (!sawPut || !observedEvent) throw new Error('WASM storage command/event proof failed')
    runtime.release(put)

    const cancelledOperation = runtime.openSession(toBinary(OpenSessionRequestSchema, create(OpenSessionRequestSchema, {
      requestId: crypto.randomUUID(), endpointId, intent: ConnectIntent.PROBE,
      managed: create(ManagedEndpointConfigSchema, {
        targetDeviceId: 'device-web-spike',
        deviceFingerprint: (globalThis as typeof globalThis & { termxDeviceFingerprint: string }).termxDeviceFingerprint,
        credentialRef,
        relayMode: ManagedRelayMode.DIRECT,
      }),
    })))
    runtime.cancel(cancelledOperation)
    const cancelled = await events.wait((event) => event.event.case === 'openSession' && Number(event.event.value.operationHandle) === cancelledOperation)
    if (cancelled.event.case !== 'openSession' || cancelled.event.value.error?.code !== ApiErrorCode.CANCELLED) throw new Error('WASM cancel proof failed')
    runtime.release(cancelledOperation)
  }

  await runtime.closeSession(session)
  await events.wait((event) => event.event.case === 'sessionClosed' && Number(event.event.value.sessionHandle) === session)
  runtime.release(session)
  events.close()
  return {
    generation,
    fingerprint: (globalThis as typeof globalThis & { termxDeviceFingerprint: string }).termxDeviceFingerprint,
    observedEvent,
  }
}

class BindingEvents {
  private readonly buffered: EventEnvelope[] = []
  private readonly waiters: Array<(event: EventEnvelope) => boolean> = []
  private active = true

  constructor(private readonly runtime: TermxWasmRuntime) {
    void this.pump()
  }

  async next(): Promise<EventEnvelope> {
    if (this.buffered.length > 0) return this.buffered.shift() as EventEnvelope
    return await new Promise<EventEnvelope>((resolve) => this.waiters.push((event) => { resolve(event); return true }))
  }

  async wait(predicate: (event: EventEnvelope) => boolean): Promise<EventEnvelope> {
    const index = this.buffered.findIndex(predicate)
    if (index >= 0) return this.buffered.splice(index, 1)[0] as EventEnvelope
    return await new Promise<EventEnvelope>((resolve) => this.waiters.push((event) => {
      if (!predicate(event)) return false
      resolve(event)
      return true
    }))
  }

  close(): void { this.active = false }

  private async pump(): Promise<void> {
    while (this.active) {
      try {
        const event = fromBinary(EventEnvelopeSchema, await this.runtime.nextEvent())
        const waiter = this.waiters.find((candidate) => candidate(event))
        if (waiter) this.waiters.splice(this.waiters.indexOf(waiter), 1)
        else this.buffered.push(event)
      } catch {
        return
      }
    }
  }
}

class SpikeCloudPlatform implements BrowserCloudPlatform {
  async resolveEndpoint(request: Parameters<BrowserCloudPlatform['resolveEndpoint']>[0]): Promise<PlatformResponse['response']> {
    const payload = await postProto('/resolve', toBinary(ResolveEndpointRequestSchema, request))
    return { case: 'cloudResolvedEndpoint', value: fromBinary(ResolvedEndpointSchema, payload) }
  }

  async createSignaling(request: Parameters<BrowserCloudPlatform['createSignaling']>[0]): Promise<PlatformResponse['response']> {
    const payload = await postProto('/signal', toBinary(CreateSignalingSessionRequestSchema, request))
    return { case: 'cloudSignaling', value: fromBinary(SignalingEventsSchema, payload) }
  }

  async handleOther(request: PlatformRequest): Promise<PlatformResponse['response']> {
    switch (request.request.case) {
      case 'cloudAcquireRelay': return { case: 'cloudRelayLease', value: create(RelayLeaseSchema, {}) }
      case 'cloudPlanRoute': return { case: 'cloudRoutePlan', value: create(ManagedRoutePlanSchema, {}) }
      case 'cloudReportQuality': return { case: 'cloudQualityReported', value: create(ReportPathQualityResponseSchema, {}) }
      case 'cloudReportOutcome': return { case: 'cloudOutcomeReported', value: create(ReportConnectionOutcomeResponseSchema, {}) }
      default: throw new Error('unexpected spike Cloud request')
    }
  }
}

async function postProto(path: string, payload: Uint8Array): Promise<Uint8Array> {
  const response = await fetch(path, { method: 'POST', headers: { 'content-type': 'application/x-protobuf' }, body: payload.slice().buffer })
  if (!response.ok) throw new Error(`${path} failed: ${response.status} ${await response.text()}`)
  return new Uint8Array(await response.arrayBuffer())
}

async function finish(result: Record<string, unknown>): Promise<void> {
  document.body.dataset.result = JSON.stringify(result)
  document.body.textContent = JSON.stringify(result)
  await fetch('/result', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(result) })
}

async function stage(value: string): Promise<void> {
  await fetch('/stage', { method: 'POST', body: value })
}
