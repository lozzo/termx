import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { ApiErrorCode, ApiErrorSchema } from '../generated/apipb/common_pb'
import {
  ConnectionCandidateType,
  ConnectionTransport,
  CloudRouteEligibilitySchema,
  CredentialRecordSchema,
  CredentialSignResponseSchema,
  EndpointRegistryLoadedSchema,
  PlatformEventSchema,
  PlatformRequestSchema,
  PlatformResponseSchema,
  SignalingEventsSchema,
  WebRTCBufferedAmountLowEventSchema,
  WebRTCChannelClosedEventSchema,
  WebRTCChannelMessageEventSchema,
  WebRTCChannelSendResultSchema,
  WebRTCCreateOfferResultSchema,
  WebRTCPeerOpenedSchema,
  WebRTCPeerReadySchema,
  WebRTCPeerSnapshotSchema,
  type CredentialRecord,
  type PlatformEvent,
  type PlatformRequest,
  type PlatformResponse,
  type WebRTCOpenPeerRequest,
} from '../generated/bindingpb/client_binding_pb'
import {
  RoutePreference,
  RelayLeaseSchema,
  ManagedRoutePlanSchema,
  ReportConnectionOutcomeResponseSchema,
  ReportPathQualityResponseSchema,
  ResolvedEndpointSchema,
  type CreateSignalingSessionRequest,
  type ResolveEndpointRequest,
} from '../generated/cloudpb/cloud_companion_pb'
import { ObservedPath } from '../generated/cloudpb/cloud_topology_pb'
import { splitOutAnswerCandidates } from '../webrtc/rtcSdpUtils'
import type { WasmPlatformDispatcher } from './wasmRuntime'

const DATABASE_NAME = 'muxvia-web-client'
const DATABASE_VERSION = 2
const CREDENTIAL_STORE = 'credentials'
const ENDPOINT_REGISTRY_STORE = 'endpoint-registry'
const CHANNEL_LABEL = 'protocol'

export interface BrowserCloudPlatform {
  resolveEndpoint(request: ResolveEndpointRequest): Promise<PlatformResponse['response']>
  createSignaling(request: CreateSignalingSessionRequest): Promise<PlatformResponse['response']>
  handleOther(request: PlatformRequest): Promise<PlatformResponse['response']>
}

export type BrowserPlatformEventSink = (payload: Uint8Array) => Promise<void>
export type BrowserPlatformDiagnostic = (stage: string) => void

/** BrowserWasmPlatform is the only JS owner of browser-only WebRTC and WebCrypto primitives. */
export class BrowserWasmPlatform implements WasmPlatformDispatcher {
  private readonly credentials = new BrowserCredentialStore()
  private readonly endpointRegistry = new BrowserEndpointRegistryStore()
  private readonly peers: BrowserPeerStore
  private closed = false

  constructor(cloud: BrowserCloudPlatform, eventSink: BrowserPlatformEventSink, private readonly diagnostic?: BrowserPlatformDiagnostic) {
    this.cloud = cloud
    this.peers = new BrowserPeerStore(eventSink)
  }

  private readonly cloud: BrowserCloudPlatform

  async prepareCredential(endpointId: string, credentialRef: string): Promise<CredentialRecord> {
    return await this.credentials.prepare(endpointId, credentialRef)
  }

  async bindCredential(endpointId: string, credentialRef: string, capabilityGrant: string): Promise<CredentialRecord> {
    return await this.credentials.bind(endpointId, credentialRef, capabilityGrant)
  }

  async handlePlatformRequest(payload: Uint8Array): Promise<Uint8Array> {
    const request = fromBinary(PlatformRequestSchema, payload)
    try {
      this.diagnostic?.(`platform:${request.request.case || 'empty'}:start`)
      const response = await this.dispatch(request)
      this.diagnostic?.(`platform:${request.request.case || 'empty'}:done`)
      return toBinary(PlatformResponseSchema, create(PlatformResponseSchema, { requestId: request.requestId, response }))
    } catch (error) {
      this.diagnostic?.(`platform:${request.request.case || 'empty'}:error:${error instanceof Error ? error.message : 'unknown'}`)
      return toBinary(PlatformResponseSchema, create(PlatformResponseSchema, {
        requestId: request.requestId,
        error: create(ApiErrorSchema, {
          code: ApiErrorCode.UNAVAILABLE,
          message: error instanceof Error ? error.message : 'browser platform request failed',
          retryable: false,
          attempted: true,
        }),
      }))
    }
  }

  async close(): Promise<void> {
    if (this.closed) return
    this.closed = true
    await this.peers.close()
  }

  private async dispatch(request: PlatformRequest): Promise<PlatformResponse['response']> {
    switch (request.request.case) {
      case 'credentialPrepare':
        return { case: 'credential', value: await this.credentials.prepare(request.request.value.endpointId, request.request.value.credentialRef) }
      case 'credentialResolve':
        return { case: 'credential', value: await this.credentials.resolve(request.request.value.endpointId, request.request.value.credentialRef) }
      case 'credentialBind':
        return { case: 'credential', value: await this.credentials.bind(request.request.value.endpointId, request.request.value.credentialRef, request.request.value.capabilityGrant) }
      case 'credentialDelete':
        await this.credentials.delete(request.request.value.credentialRef)
        return { case: undefined }
      case 'credentialSign':
        return {
          case: 'credentialSign',
          value: create(CredentialSignResponseSchema, { signature: await this.credentials.sign(request.request.value.credentialRef, request.request.value.payload) }),
        }
      case 'endpointRegistryLoad':
        return { case: 'endpointRegistry', value: create(EndpointRegistryLoadedSchema, { registryProto: await this.endpointRegistry.load() }) }
      case 'endpointRegistryStore':
        await this.endpointRegistry.store(request.request.value.registryProto, request.request.value.deleteCredentialRefs)
        return { case: undefined }
      case 'cloudResolveEndpoint':
        return await this.cloud.resolveEndpoint(request.request.value)
      case 'cloudCreateSignaling':
        return await this.cloud.createSignaling(request.request.value)
      case 'cloudAcquireRelay':
      case 'cloudPlanRoute':
      case 'cloudReportQuality':
      case 'cloudReportOutcome':
      case 'cloudRouteEligibility':
        return await this.cloud.handleOther(request)
      case 'webrtcOpenPeer':
        return { case: 'webrtcPeerOpened', value: await this.peers.open(request.request.value) }
      case 'webrtcCreateOffer':
        return { case: 'webrtcOffer', value: create(WebRTCCreateOfferResultSchema, { offerSdp: await this.peers.createOffer(request.request.value.peerHandle) }) }
      case 'webrtcApplyAnswer':
        await this.peers.applyAnswer(request.request.value)
        return { case: undefined }
      case 'webrtcWaitReady':
        return { case: 'webrtcPeerReady', value: await this.peers.waitReady(request.request.value.peerHandle) }
      case 'webrtcChannelSend':
        return { case: 'webrtcChannelSent', value: create(WebRTCChannelSendResultSchema, { bufferedAmount: this.peers.send(request.request.value.channelHandle, request.request.value.payload) }) }
      case 'webrtcChannelThreshold':
        this.peers.setBufferedAmountLowThreshold(request.request.value.channelHandle, request.request.value.lowThreshold)
        return { case: undefined }
      case 'webrtcPeerSnapshot':
        return { case: 'webrtcPeerSnapshot', value: await this.peers.snapshot(request.request.value.peerHandle, request.request.value.sampledAtUnixNano) }
      case 'webrtcClosePeer':
        this.peers.closePeer(request.request.value.handle)
        return { case: undefined }
      case 'webrtcCloseChannel':
        this.peers.closeChannel(request.request.value.handle)
        return { case: undefined }
      default:
        throw new Error('browser platform request is empty or unsupported')
    }
  }
}

interface StoredCredential {
  credentialRef: string
  endpointId: string
  publicKey: ArrayBuffer
  privateKey: CryptoKey
  keyFingerprint: string
  capabilityGrant: string
}

class BrowserCredentialStore {
  async prepare(endpointId: string, credentialRef: string): Promise<CredentialRecord> {
    const existing = await this.read(credentialRef)
    if (existing) {
      if (existing.endpointId !== endpointId) throw new Error('browser credential endpoint mismatch')
      return credentialRecord(existing)
    }
    const pair = await crypto.subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify'])
    if (pair.privateKey.extractable) throw new Error('browser generated an exportable client private key')
    const publicKey = await crypto.subtle.exportKey('raw', pair.publicKey)
    const record: StoredCredential = {
      credentialRef,
      endpointId,
      publicKey,
      privateKey: pair.privateKey,
      keyFingerprint: await keyFingerprint(publicKey),
      capabilityGrant: '',
    }
    await this.write(record)
    return credentialRecord(record, true)
  }

  async resolve(endpointId: string, credentialRef: string): Promise<CredentialRecord> {
    const record = await this.read(credentialRef)
    if (!record || record.endpointId !== endpointId || !record.capabilityGrant) throw new Error('browser credential is unavailable')
    return credentialRecord(record)
  }

  async bind(endpointId: string, credentialRef: string, capabilityGrant: string): Promise<CredentialRecord> {
    const record = await this.read(credentialRef)
    if (!record || record.endpointId !== endpointId) throw new Error('browser credential bind target is unavailable')
    const bound = { ...record, capabilityGrant }
    await this.write(bound)
    return credentialRecord(bound)
  }

  async delete(credentialRef: string): Promise<void> {
    const db = await openCredentialDatabase()
    await transactionResult(db.transaction(CREDENTIAL_STORE, 'readwrite').objectStore(CREDENTIAL_STORE).delete(credentialRef))
  }

  async sign(credentialRef: string, payload: Uint8Array): Promise<Uint8Array> {
    const record = await this.read(credentialRef)
    if (!record) throw new Error('browser signer is unavailable')
    const signature = await crypto.subtle.sign('Ed25519', record.privateKey, payload.slice().buffer)
    return new Uint8Array(signature)
  }

  private async read(credentialRef: string): Promise<StoredCredential | undefined> {
    const db = await openCredentialDatabase()
    return await transactionResult<StoredCredential | undefined>(db.transaction(CREDENTIAL_STORE).objectStore(CREDENTIAL_STORE).get(credentialRef))
  }

  private async write(record: StoredCredential): Promise<void> {
    const db = await openCredentialDatabase()
    await transactionResult(db.transaction(CREDENTIAL_STORE, 'readwrite').objectStore(CREDENTIAL_STORE).put(record))
  }
}

class BrowserEndpointRegistryStore {
  async load(): Promise<Uint8Array> {
    const db = await openCredentialDatabase()
    const value = await transactionResult<ArrayBuffer | undefined>(db.transaction(ENDPOINT_REGISTRY_STORE).objectStore(ENDPOINT_REGISTRY_STORE).get('registry'))
    return value ? new Uint8Array(value.slice(0)) : new Uint8Array()
  }

  async store(registryProto: Uint8Array, deleteCredentialRefs: string[]): Promise<void> {
    if (registryProto.byteLength === 0 || registryProto.byteLength > 1 << 20) throw new Error('browser endpoint registry payload size is invalid')
    const db = await openCredentialDatabase()
    const transaction = db.transaction([ENDPOINT_REGISTRY_STORE, CREDENTIAL_STORE], 'readwrite')
    transaction.objectStore(ENDPOINT_REGISTRY_STORE).put(registryProto.slice().buffer, 'registry')
    const credentials = transaction.objectStore(CREDENTIAL_STORE)
    for (const credentialRef of new Set(deleteCredentialRefs)) credentials.delete(credentialRef)
    await transactionCompletion(transaction)
  }
}

type PeerRecord = {
  peerHandle: bigint
  channelHandle: bigint
  peer: RTCPeerConnection
  channel: RTCDataChannel
  remoteFingerprint: string
}

class BrowserPeerStore {
  private nextHandle = 0n
  private readonly peers = new Map<bigint, PeerRecord>()
  private readonly channels = new Map<bigint, PeerRecord>()
  private eventChain: Promise<void> = Promise.resolve()

  constructor(private readonly eventSink: BrowserPlatformEventSink) {}

  async open(request: WebRTCOpenPeerRequest) {
    const peer = new RTCPeerConnection({
      iceServers: request.iceServers
        .map((server) => ({
          urls: request.routePreference === RoutePreference.DIRECT_ONLY
            ? server.urls.filter((url) => !/^turns?:/i.test(url.trim()))
            : server.urls,
          username: server.username,
          credential: server.credential,
        }))
        .filter((server) => server.urls.length > 0),
      iceTransportPolicy: request.relayOnly ? 'relay' : 'all',
    })
    const channel = peer.createDataChannel(CHANNEL_LABEL, { ordered: true })
    channel.binaryType = 'arraybuffer'
    const record: PeerRecord = { peerHandle: this.allocateHandle(), channelHandle: this.allocateHandle(), peer, channel, remoteFingerprint: '' }
    this.peers.set(record.peerHandle, record)
    this.channels.set(record.channelHandle, record)
    channel.addEventListener('message', (event) => {
      this.queueEvent(async () => {
        const payload = await messageBytes(event.data)
        return create(PlatformEventSchema, {
          event: { case: 'webrtcChannelMessage', value: create(WebRTCChannelMessageEventSchema, { channelHandle: record.channelHandle, payload }) },
        })
      })
    })
    channel.addEventListener('close', () => {
      this.queueEvent(() => create(PlatformEventSchema, {
        event: { case: 'webrtcChannelClosed', value: create(WebRTCChannelClosedEventSchema, { channelHandle: record.channelHandle }) },
      }))
    })
    channel.addEventListener('bufferedamountlow', () => {
      this.queueEvent(() => create(PlatformEventSchema, {
        event: {
          case: 'webrtcBufferedAmountLow',
          value: create(WebRTCBufferedAmountLowEventSchema, { channelHandle: record.channelHandle, bufferedAmount: BigInt(channel.bufferedAmount) }),
        },
      }))
    })
    return create(WebRTCPeerOpenedSchema, { peerHandle: record.peerHandle, channelHandle: record.channelHandle })
  }

  async createOffer(peerHandle: bigint): Promise<string> {
    const record = this.peer(peerHandle)
    const offer = await record.peer.createOffer()
    await record.peer.setLocalDescription(offer)
    await waitIceGatheringComplete(record.peer)
    const sdp = record.peer.localDescription?.sdp
    if (!sdp?.trim()) throw new Error('browser WebRTC offer has no SDP')
    return sdp
  }

  async applyAnswer(request: { peerHandle: bigint; answerSdp: string; candidates: Array<{ candidate: string; sdpMid: string; sdpMlineIndex: number; usernameFragment: string }> }): Promise<void> {
    const record = this.peer(request.peerHandle)
    const normalized = splitOutAnswerCandidates(request.answerSdp)
    record.remoteFingerprint = sdpFingerprint(request.answerSdp)
    await record.peer.setRemoteDescription({ type: 'answer', sdp: normalized.sdp })
    const candidates: RTCIceCandidateInit[] = [
      ...normalized.candidates,
      ...request.candidates.map((candidate) => ({
        candidate: candidate.candidate,
        ...(candidate.sdpMid ? { sdpMid: candidate.sdpMid } : {}),
        sdpMLineIndex: candidate.sdpMlineIndex,
        ...(candidate.usernameFragment ? { usernameFragment: candidate.usernameFragment } : {}),
      })),
    ]
    const seen = new Set<string>()
    for (const candidate of candidates) {
      if (!candidate.candidate || seen.has(candidate.candidate)) continue
      seen.add(candidate.candidate)
      await record.peer.addIceCandidate(candidate)
    }
  }

  async waitReady(peerHandle: bigint) {
    const record = this.peer(peerHandle)
    await Promise.all([waitDataChannelOpen(record.channel), waitPeerConnected(record.peer)])
    const actualFingerprint = await verifiedRemoteCertificateFingerprint(record.peer, record.remoteFingerprint)
    const path = await observedPath(record.peer)
    return create(WebRTCPeerReadySchema, { remoteCertificateFingerprint: actualFingerprint, observedPath: path })
  }

  send(channelHandle: bigint, payload: Uint8Array): bigint {
    const channel = this.channel(channelHandle).channel
    if (channel.readyState !== 'open') throw new Error('browser WebRTC channel is not open')
    channel.send(payload.slice())
    return BigInt(channel.bufferedAmount)
  }

  setBufferedAmountLowThreshold(channelHandle: bigint, value: bigint): void {
    const threshold = Number(value)
    if (!Number.isSafeInteger(threshold) || threshold < 0) throw new Error('browser DataChannel low threshold is invalid')
    this.channel(channelHandle).channel.bufferedAmountLowThreshold = threshold
  }

  async snapshot(peerHandle: bigint, sampledAtUnixNano: bigint) {
    const record = this.peer(peerHandle)
    const selected = await selectedCandidatePair(record.peer)
    if (!selected) return create(WebRTCPeerSnapshotSchema, { valid: false })
    return create(WebRTCPeerSnapshotSchema, {
      valid: true,
      pairId: selected.pair.id,
      path: candidatePath(selected.local, selected.remote),
      networkClass: stringStat(selected.local, 'networkType'),
      sampledAtUnixNano,
      roundTripNanos: BigInt(Math.round(numberStat(selected.pair, 'currentRoundTripTime') * 1_000_000_000)),
      bytesSent: BigInt(numberStat(selected.pair, 'bytesSent')),
      bytesReceived: BigInt(numberStat(selected.pair, 'bytesReceived')),
      packetsSent: BigInt(numberStat(selected.pair, 'packetsSent')),
      lossEvents: BigInt(numberStat(selected.pair, 'retransmissionsSent') + numberStat(selected.pair, 'packetsDiscardedOnSend')),
      connected: record.peer.connectionState === 'connected',
      localCandidateType: connectionCandidateType(stringStat(selected.local, 'candidateType')),
      remoteCandidateType: connectionCandidateType(stringStat(selected.remote, 'candidateType')),
      localProtocol: connectionTransport(stringStat(selected.local, 'protocol')),
      remoteProtocol: connectionTransport(stringStat(selected.remote, 'protocol')),
      relayTransport: connectionTransport(stringStat(selected.local, 'relayProtocol')),
    })
  }

  closePeer(handle: bigint): void {
    const record = this.peer(handle)
    this.peers.delete(record.peerHandle)
    this.channels.delete(record.channelHandle)
    record.channel.close()
    record.peer.close()
  }

  closeChannel(handle: bigint): void {
    this.channel(handle).channel.close()
  }

  async close(): Promise<void> {
    for (const handle of [...this.peers.keys()]) this.closePeer(handle)
    await this.eventChain.catch(() => undefined)
  }

  private peer(handle: bigint): PeerRecord {
    const record = this.peers.get(handle)
    if (!record) throw new Error('browser WebRTC peer handle is invalid')
    return record
  }

  private channel(handle: bigint): PeerRecord {
    const record = this.channels.get(handle)
    if (!record) throw new Error('browser WebRTC channel handle is invalid')
    return record
  }

  private allocateHandle(): bigint {
    this.nextHandle += 1n
    return this.nextHandle
  }

  private queueEvent(factory: () => PlatformEvent | Promise<PlatformEvent>): void {
    this.eventChain = this.eventChain.then(async () => {
      const event = await factory()
      await this.eventSink(toBinary(PlatformEventSchema, event))
    })
  }
}

function credentialRecord(record: StoredCredential, newlyCreated = false): CredentialRecord {
  return create(CredentialRecordSchema, {
    endpointId: record.endpointId,
    credentialRef: record.credentialRef,
    publicKey: new Uint8Array(record.publicKey.slice(0)),
    keyFingerprint: record.keyFingerprint,
    capabilityGrant: record.capabilityGrant,
    newlyCreated,
  })
}

let credentialDatabase: Promise<IDBDatabase> | null = null

function openCredentialDatabase(): Promise<IDBDatabase> {
  if (credentialDatabase) return credentialDatabase
  credentialDatabase = new Promise((resolve, reject) => {
    const request = indexedDB.open(DATABASE_NAME, DATABASE_VERSION)
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(CREDENTIAL_STORE)) request.result.createObjectStore(CREDENTIAL_STORE, { keyPath: 'credentialRef' })
      if (!request.result.objectStoreNames.contains(ENDPOINT_REGISTRY_STORE)) request.result.createObjectStore(ENDPOINT_REGISTRY_STORE)
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error('browser credential database failed'))
  })
  return credentialDatabase
}

function transactionCompletion(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve()
    transaction.onerror = () => reject(transaction.error ?? new Error('browser endpoint registry transaction failed'))
    transaction.onabort = () => reject(transaction.error ?? new Error('browser endpoint registry transaction aborted'))
  })
}

function transactionResult<T = undefined>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error('browser credential transaction failed'))
  })
}

async function keyFingerprint(publicKey: ArrayBuffer): Promise<string> {
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', publicKey))
  return `ed25519-sha256:${base64URL(digest)}`
}

function base64URL(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

async function messageBytes(value: unknown): Promise<Uint8Array> {
  if (value instanceof ArrayBuffer) return new Uint8Array(value.slice(0))
  if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer.slice(value.byteOffset, value.byteOffset + value.byteLength))
  if (value instanceof Blob) return new Uint8Array(await value.arrayBuffer())
  throw new Error('browser DataChannel delivered a non-binary message')
}

async function waitIceGatheringComplete(peer: RTCPeerConnection): Promise<void> {
  if (peer.iceGatheringState === 'complete') return
  await waitEvent(peer, 'icegatheringstatechange', () => peer.iceGatheringState === 'complete')
}

async function waitDataChannelOpen(channel: RTCDataChannel): Promise<void> {
  if (channel.readyState === 'open') return
  await waitEvent(channel, 'open', () => channel.readyState === 'open')
}

async function waitPeerConnected(peer: RTCPeerConnection): Promise<void> {
  if (peer.connectionState === 'connected') return
  await waitEvent(peer, 'connectionstatechange', () => peer.connectionState === 'connected', () => peer.connectionState === 'failed' || peer.connectionState === 'closed')
}

function waitEvent(target: EventTarget, name: string, ready: () => boolean, failed?: () => boolean): Promise<void> {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => finish(new Error(`browser WebRTC ${name} timed out`)), 15_000)
    const listener = () => {
      if (failed?.()) finish(new Error(`browser WebRTC ${name} failed`))
      else if (ready()) finish()
    }
    const finish = (error?: Error) => {
      clearTimeout(timeout)
      target.removeEventListener(name, listener)
      if (error) reject(error)
      else resolve()
    }
    target.addEventListener(name, listener)
    listener()
  })
}

function sdpFingerprint(sdp: string): string {
  const match = sdp.match(/^a=fingerprint:sha-256\s+([0-9a-f:]+)$/im)
  if (!match?.[1]) throw new Error('remote SDP has no SHA-256 fingerprint')
  return normalizeFingerprint(match[1])
}

async function verifiedRemoteCertificateFingerprint(peer: RTCPeerConnection, expectedFingerprint: string): Promise<string> {
  const transport = peer.sctp?.transport as RTCDtlsTransport & { getRemoteCertificates?: () => ArrayBuffer[] }
  const certificate = transport?.getRemoteCertificates?.()[0]
  if (!certificate) throw new Error('browser cannot read the actual remote DTLS certificate')
  return await verifyRemoteDTLSCertificate(expectedFingerprint, certificate)
}

/** verifyRemoteDTLSCertificate binds the SDP fingerprint to the certificate exposed by the established DTLS transport. */
export async function verifyRemoteDTLSCertificate(expectedFingerprint: string, certificate: ArrayBuffer): Promise<string> {
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', certificate))
  const actual = `sha-256:${[...digest].map((byte) => byte.toString(16).padStart(2, '0')).join(':')}`
  if (actual !== expectedFingerprint) throw new Error('browser DTLS certificate does not match remote SDP fingerprint')
  return actual
}

function normalizeFingerprint(value: string): string {
  const compact = value.replace(/:/g, '').toLowerCase()
  if (!/^[0-9a-f]{64}$/.test(compact)) throw new Error('remote SDP SHA-256 fingerprint is malformed')
  return `sha-256:${compact.match(/.{2}/g)?.join(':')}`
}

async function observedPath(peer: RTCPeerConnection): Promise<ObservedPath> {
  const selected = await selectedCandidatePair(peer)
  if (!selected) throw new Error('browser WebRTC has no selected candidate pair')
  return candidatePath(selected.local, selected.remote)
}

async function selectedCandidatePair(peer: RTCPeerConnection): Promise<{ pair: RTCStats; local: RTCStats; remote: RTCStats } | null> {
  const report = await peer.getStats()
  let selectedId = ''
  report.forEach((entry) => {
    if (entry.type === 'transport' && typeof entry.selectedCandidatePairId === 'string') selectedId = entry.selectedCandidatePairId
  })
  let pair = selectedId ? report.get(selectedId) : undefined
  if (!pair) {
    report.forEach((entry) => {
      if (!pair && entry.type === 'candidate-pair' && entry.state === 'succeeded' && (entry.nominated === true || entry.selected === true)) pair = entry
    })
  }
  if (!pair) return null
  const local = report.get(stringStat(pair, 'localCandidateId'))
  const remote = report.get(stringStat(pair, 'remoteCandidateId'))
  return local && remote ? { pair, local, remote } : null
}

function candidatePath(local: RTCStats, remote: RTCStats): ObservedPath {
  return stringStat(local, 'candidateType') === 'relay' || stringStat(remote, 'candidateType') === 'relay'
    ? ObservedPath.SINGLE_RELAY
    : ObservedPath.DIRECT
}

function connectionCandidateType(value: string): ConnectionCandidateType {
  switch (value.toLowerCase()) {
    case 'host': return ConnectionCandidateType.HOST
    case 'srflx': return ConnectionCandidateType.SERVER_REFLEXIVE
    case 'prflx': return ConnectionCandidateType.PEER_REFLEXIVE
    case 'relay': return ConnectionCandidateType.RELAY
    default: return ConnectionCandidateType.UNSPECIFIED
  }
}

function connectionTransport(value: string): ConnectionTransport {
  switch (value.toLowerCase()) {
    case 'udp': return ConnectionTransport.UDP
    case 'tcp':
    case 'tls': return ConnectionTransport.TCP
    default: return ConnectionTransport.UNSPECIFIED
  }
}

function stringStat(value: RTCStats, key: string): string {
  const field = (value as unknown as Record<string, unknown>)[key]
  return typeof field === 'string' ? field : ''
}

function numberStat(value: RTCStats, key: string): number {
  const field = (value as unknown as Record<string, unknown>)[key]
  return typeof field === 'number' && Number.isFinite(field) && field > 0 ? field : 0
}

export const emptyBrowserCloudPlatform: BrowserCloudPlatform = {
  async resolveEndpoint(request) {
    return { case: 'cloudResolvedEndpoint', value: create(ResolvedEndpointSchema, { endpointId: request.endpointId, targetDeviceId: request.targetDeviceId }) }
  },
  async createSignaling() {
    return { case: 'cloudSignaling', value: create(SignalingEventsSchema, {}) }
  },
  async handleOther(request) {
    switch (request.request.case) {
      case 'cloudAcquireRelay': return { case: 'cloudRelayLease', value: create(RelayLeaseSchema, {}) }
      case 'cloudPlanRoute': return { case: 'cloudRoutePlan', value: create(ManagedRoutePlanSchema, {}) }
      case 'cloudReportQuality': return { case: 'cloudQualityReported', value: create(ReportPathQualityResponseSchema, {}) }
      case 'cloudReportOutcome': return { case: 'cloudOutcomeReported', value: create(ReportConnectionOutcomeResponseSchema, {}) }
      case 'cloudRouteEligibility': return { case: 'cloudRouteEligibility', value: create(CloudRouteEligibilitySchema, {}) }
      default: throw new Error('unsupported browser Cloud request')
    }
  },
}
