import { fromBinary, toBinary, type DescMessage, type MessageShape } from '@bufbuild/protobuf'
import type { PlatformRequest, PlatformResponse } from '../generated/bindingpb/client_binding_pb'
import {
  AcquireRelayLeaseRequestSchema,
  CloudErrorSchema,
  CreateSignalingSessionRequestSchema,
  RelayLeaseSchema,
  ResolveEndpointRequestSchema,
  ResolvedEndpointSchema,
  SignalingEventSchema,
  type CreateSignalingSessionRequest,
  type ResolveEndpointRequest,
} from '../generated/cloudpb/cloud_companion_pb'
import type { RemoteRuntimeFetch, RemoteRuntimeStorage } from '../core/transport'
import type { BrowserCloudPlatform } from './browserWasmPlatform'

const PROTOBUF_MEDIA_TYPE = 'application/x-protobuf'
const STREAM_MEDIA_TYPE = 'application/x-termx-cloud-stream'
const MAX_FRAME_BYTES = 4 << 20

export interface BrowserCloudEndpoint {
  endpointId: string
  hubUrl: string
}

/** BrowserCloudHttpPlatform maps cloudpb requests to the authenticated Hub HTTP primitive only. */
export class BrowserCloudHttpPlatform implements BrowserCloudPlatform {
  private readonly endpoints = new Map<string, string>()
  private readonly managedSessions = new Map<string, string>()

  constructor(private readonly fetchImpl: RemoteRuntimeFetch, private readonly storage: RemoteRuntimeStorage) {}

  registerEndpoint(endpoint: BrowserCloudEndpoint): void {
    const endpointId = endpoint.endpointId.trim()
    if (!endpointId) throw new Error('browser Cloud endpoint id is required')
    this.endpoints.set(endpointId, normalizeHubOrigin(endpoint.hubUrl))
  }

  async resolveEndpoint(request: ResolveEndpointRequest): Promise<PlatformResponse['response']> {
    const resolved = await this.postUnary(request.endpointId, '/v1/endpoints/resolve', ResolveEndpointRequestSchema, request, ResolvedEndpointSchema)
    if (resolved.managedSessionId) this.managedSessions.set(resolved.managedSessionId, request.endpointId)
    return {
      case: 'cloudResolvedEndpoint',
      value: resolved,
    }
  }

  async createSignaling(request: CreateSignalingSessionRequest): Promise<PlatformResponse['response']> {
    const response = await this.post(request.endpointId, '/v1/signaling/create', toBinary(CreateSignalingSessionRequestSchema, request))
    requireMediaType(response, STREAM_MEDIA_TYPE)
    const frames = decodeCloudFrames(new Uint8Array(await response.arrayBuffer()))
    return { case: 'cloudSignaling', value: { $typeName: 'termx.client.binding.v1.SignalingEvents', events: frames.map((frame) => fromBinary(SignalingEventSchema, frame)) } }
  }

  async handleOther(request: PlatformRequest): Promise<PlatformResponse['response']> {
    switch (request.request.case) {
      case 'cloudAcquireRelay':
        return { case: 'cloudRelayLease', value: await this.postUnary(this.endpointForManagedSession(request.request.value.managedSessionId), '/v1/relay/leases/acquire', AcquireRelayLeaseRequestSchema, request.request.value, RelayLeaseSchema) }
      case 'cloudPlanRoute':
        throw new Error('browser managed route planning is unavailable')
      case 'cloudReportQuality':
        throw new Error('browser path quality reporting is unavailable')
      case 'cloudReportOutcome':
        throw new Error('browser connection outcome reporting is unavailable')
      default:
        throw new Error('unsupported browser Cloud request')
    }
  }

  private async postUnary<I extends DescMessage, O extends DescMessage>(endpointId: string, path: string, inputSchema: I, input: MessageShape<I>, outputSchema: O): Promise<MessageShape<O>> {
    const response = await this.post(endpointId, path, toBinary(inputSchema, input))
    requireMediaType(response, PROTOBUF_MEDIA_TYPE)
    return fromBinary(outputSchema, new Uint8Array(await response.arrayBuffer()))
  }

  private endpointForManagedSession(managedSessionId: string): string {
    const endpointId = this.managedSessions.get(managedSessionId)
    if (!endpointId) throw new Error('browser Cloud managed session has no owning endpoint')
    return endpointId
  }

  private async post(endpointId: string, path: string, payload: Uint8Array): Promise<Response> {
    const hubUrl = this.endpoints.get(endpointId)
    if (!hubUrl) throw new Error(`browser Cloud endpoint is not registered: ${endpointId}`)
    const identity = browserEdgeIdentity(this.storage)
    const response = await this.fetchImpl(`${hubUrl}${path}`, {
      method: 'POST',
      headers: { authorization: `Bearer ${identity.token}`, 'content-type': 'application/json' },
      body: JSON.stringify({ account_id: identity.accountId, device_id: identity.deviceId, payload: bytesToBase64(payload) }),
    })
    if (response.ok) return response
    const body = new Uint8Array(await response.arrayBuffer())
    if (mediaType(response) === PROTOBUF_MEDIA_TYPE) {
      const cloudError = fromBinary(CloudErrorSchema, body)
      throw new Error(cloudError.message || `Hub request failed with HTTP ${response.status}`)
    }
    throw new Error(`Hub request failed with HTTP ${response.status}`)
  }
}

function browserEdgeIdentity(storage: RemoteRuntimeStorage): { token: string; accountId: string; deviceId: string } {
  const token = storage.getItem('termx.remote.accessToken')?.trim() ?? ''
  if (!token) throw new Error('browser Cloud login is required')
  const claims = jwtClaims(token)
  const accountId = storage.getItem('termx.cloud.accountId')?.trim() || stringClaim(claims, 'account_id', 'accountId', 'sub')
  const deviceId = storage.getItem('termx.cloud.deviceId')?.trim() || stringClaim(claims, 'device_id', 'deviceId')
  if (!accountId || !deviceId) throw new Error('browser Cloud session is missing account/device identity')
  return { token, accountId, deviceId }
}

function jwtClaims(token: string): Record<string, unknown> {
  const encoded = token.split('.')[1]
  if (!encoded) return {}
  try {
    return JSON.parse(atob(encoded.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(encoded.length / 4) * 4, '='))) as Record<string, unknown>
  } catch {
    return {}
  }
}

function stringClaim(claims: Record<string, unknown>, ...keys: string[]): string {
  for (const key of keys) {
    const value = claims[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function normalizeHubOrigin(raw: string): string {
  const url = new URL(raw)
  if (url.protocol !== 'https:' && !(url.protocol === 'http:' && (url.hostname === '127.0.0.1' || url.hostname === 'localhost'))) {
    throw new Error('browser Hub URL must use HTTPS or loopback HTTP')
  }
  if (url.username || url.password || url.search || url.hash) throw new Error('browser Hub URL contains unsupported components')
  return `${url.origin}${url.pathname.replace(/\/$/, '')}`
}

function decodeCloudFrames(payload: Uint8Array): Uint8Array[] {
  const frames: Uint8Array[] = []
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength)
  let offset = 0
  while (offset < payload.byteLength) {
    if (payload.byteLength - offset < 4) throw new Error('Hub signaling stream has a truncated frame header')
    const size = view.getUint32(offset, false)
    offset += 4
    if (size === 0 || size > MAX_FRAME_BYTES || offset + size > payload.byteLength) throw new Error('Hub signaling stream has an invalid frame size')
    frames.push(payload.slice(offset, offset + size))
    offset += size
  }
  return frames
}

function bytesToBase64(payload: Uint8Array): string {
  let binary = ''
  for (const byte of payload) binary += String.fromCharCode(byte)
  return btoa(binary)
}

function mediaType(response: Response): string { return response.headers.get('content-type')?.split(';', 1)[0]?.trim().toLowerCase() ?? '' }
function requireMediaType(response: Response, expected: string): void {
  if (mediaType(response) !== expected) throw new Error(`Hub returned an invalid media type: ${mediaType(response) || 'missing'}`)
}
