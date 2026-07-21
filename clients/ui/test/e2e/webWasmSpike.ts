import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { CommandEnvelopeSchema } from '../../src/generated/apipb/application_pb'
import { ApplicationEventType, EventSubscribeCommandSchema } from '../../src/generated/apipb/events_pb'
import { StorageKeySchema, StoragePutCommandSchema, StorageScope } from '../../src/generated/apipb/storage_pb'
import {
  CloudRouteEligibilitySchema,
  CredentialRecordSchema,
  OpenSessionRequestSchema,
  PlatformResponseSchema,
  SignalingEventsSchema,
  ConnectIntent,
  type PlatformRequest,
  type PlatformResponse,
} from '../../src/generated/bindingpb/client_binding_pb'
import {
  EndpointConfigV1Schema,
  EndpointConnectMode,
  EndpointDaemonIdentitySchema,
  EndpointRouteConfigV1Schema,
  EndpointSource,
  ManagedWebRTCRelayMode,
  ManagedWebRTCRouteConfigSchema,
} from '../../src/generated/remoteauthpb/remote_auth_pb'
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
import { MuxviaWasmRuntime, loadMuxviaWasmExports } from '../../src/binding/wasmRuntime'
import { ProtoBindingClient } from '../../src/binding/protoBindingClient'
import { WasmBindingBackend } from '../../src/binding/wasmBindingBackend'

const endpointId = 'web-spike'
const credentialRef = 'credential:web-spike'

void run().catch((error) => finish({ ok: false, error: error instanceof Error ? error.stack || error.message : String(error) }))

async function run(): Promise<void> {
  await stage('module-loaded')
  const exports = await loadMuxviaWasmExports({ wasmUrl: '/assets/muxvia-client.wasm', wasmExecUrl: '/assets/wasm_exec.js' })
  await stage('wasm-loaded')
  const cloud = new SpikeCloudPlatform()
  let activeRuntime: MuxviaWasmRuntime | null = null
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
    const runtime = await MuxviaWasmRuntime.create(exports, nextPlatform)
    activeRuntime = runtime
    const client = new ProtoBindingClient(new WasmBindingBackend(runtime))
    return { client, close: () => client.close() }
  }, (generation) => { if (!generation) activeRuntime = null })
  lifecycle.attach()
  let generation = await lifecycle.start()
  await stage('first-runtime-created')
  const first = await openAndProve(generation.client)
  await stage('first-runtime-proved')
  window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }))
  window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))
  await lifecycle.whenIdle()
  generation = lifecycle.current as typeof generation
  await stage('second-runtime-created')
  const second = await openAndProve(generation.client, false)
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

async function openAndProve(client: ProtoBindingClient, proveEvent = true): Promise<{ generation: bigint; fingerprint: string; observedEvent: boolean }> {
  await client.upsertEndpoint(managedEndpointConfig(), true)
  const session = await client.openSession(create(OpenSessionRequestSchema, {
    requestId: crypto.randomUUID(),
    endpointId,
    routeOverride: 'managed',
    intent: ConnectIntent.INTERACTIVE,
  }))
  const generation = session.stamp.generation

  let observedEvent = false
  if (proveEvent) {
    const observed = new Promise<boolean>((resolve) => {
      const subscription = session.subscribeEvents((event) => {
        if (event.event.case !== 'storageChanged') return
        subscription.close()
        resolve(true)
      })
    })
    const subscribed = await session.execute(create(CommandEnvelopeSchema, {
      command: {
        case: 'eventSubscribe',
        value: create(EventSubscribeCommandSchema, { types: [ApplicationEventType.STORAGE_CHANGED], storageAppId: 'web-spike', storageScope: StorageScope.PUBLIC }),
      },
    }))
    if (!subscribed.result.case) throw new Error('WASM event subscribe failed')

    const put = await session.execute(create(CommandEnvelopeSchema, {
      command: {
        case: 'storagePut',
        value: create(StoragePutCommandSchema, {
          key: create(StorageKeySchema, { appId: 'web-spike', scope: StorageScope.PUBLIC, key: 'browser-proof' }),
          value: new TextEncoder().encode('ok'),
        }),
      },
    }))
    observedEvent = await observed
    if (put.result.case !== 'storagePut' || !observedEvent) throw new Error('WASM storage command/event proof failed')

    const controller = new AbortController()
    const cancelled = client.openSession(create(OpenSessionRequestSchema, {
      requestId: crypto.randomUUID(), endpointId, routeOverride: 'managed', intent: ConnectIntent.PROBE,
    }), controller.signal)
    controller.abort(new DOMException('cancel proof', 'AbortError'))
    await cancelled.then(() => { throw new Error('WASM cancel proof unexpectedly opened a session') }, () => undefined)
  }

  await session.close()
  return {
    generation,
    fingerprint: (globalThis as typeof globalThis & { muxviaDeviceFingerprint: string }).muxviaDeviceFingerprint,
    observedEvent,
  }
}

function managedEndpointConfig() {
  return create(EndpointConfigV1Schema, {
    schemaVersion: 1,
    endpointId,
    label: 'Web spike',
    labelSource: EndpointSource.USER,
    identity: create(EndpointDaemonIdentitySchema, {
      deviceId: 'device-web-spike',
      deviceFingerprint: (globalThis as typeof globalThis & { muxviaDeviceFingerprint: string }).muxviaDeviceFingerprint,
    }),
    connectMode: EndpointConnectMode.ON_DEMAND,
    enabled: true,
    routes: [create(EndpointRouteConfigV1Schema, {
      schemaVersion: 1,
      routeId: 'managed',
      enabled: true,
      credentialRef,
      source: EndpointSource.USER,
      policySource: EndpointSource.USER,
      route: {
        case: 'managedWebrtc',
        value: create(ManagedWebRTCRouteConfigSchema, {
          targetDeviceId: 'device-web-spike',
          relayMode: ManagedWebRTCRelayMode.MANAGED_WEBRTC_RELAY_MODE_DIRECT,
        }),
      },
    })],
  })
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
      case 'cloudRouteEligibility': return { case: 'cloudRouteEligibility', value: create(CloudRouteEligibilitySchema, { accountSessionAvailable: true, managedDirectAvailable: true, relayAvailable: true }) }
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
