/// <reference types="node" />

import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { create } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import { EndpointDaemonIdentitySchema, LocalDiscoveryCandidateSchema } from '../generated/remoteauthpb/remote_auth_pb'
import { decodeEndpointRegistryProjection, EndpointProjectionError, endpointRegistryMaxBytes, type EndpointConnectMode, type EndpointRegistryProjection, type EndpointRouteKind, type EndpointRouteProjection, type EndpointSource } from './endpointContract'

interface Candidate {
  source: EndpointSource
  identity: { device_id: string; device_fingerprint: string }
  suggested_label: string
  connect_mode?: EndpointConnectMode
  apply_client_policy?: boolean
  routes: Array<Record<string, unknown> & { route_id: string; kind: string }>
}

interface ConfirmedIdentityBinding {
  endpoint_id: string
  identity: { device_id: string; device_fingerprint: string }
}

interface ContractFixture {
  schema_version: number
  empty_registry: unknown
  valid_registry: unknown
  defaulted_managed_registry: unknown
  unknown_field_registry: unknown
  missing_field_registry: unknown
  wrong_type_registry: unknown
  whitespace_key_registry: unknown
  invalid_registry_cases: Array<{ name: string; registry: unknown; expected_error: string }>
  assembler: {
    initial_registry: unknown
    confirmed_identity_bindings: ConfirmedIdentityBinding[]
    commutative_candidates: Candidate[]
    identity_conflict_candidate: Candidate
    expected_endpoint_id: string
    expected_new_endpoint_id: string
    expected_label: string
    expected_connect_mode: EndpointConnectMode
    expected_route_ids: string[]
    expected_route_priorities: Record<string, number>
    expected_conflict_error: string
  }
  local_discovery_candidate: {
    claimed_device_id: string
    claimed_device_fingerprint: string
    address: string
    port: number
    protocol_version: number
    ttl_millis: number
    signature_bytes: number
  }
  oversize_bytes: number
}

describe('shared endpoint contract fixture', () => {
  it('agrees on strict parse, round-trip, size, identity conflict and import commutativity', async () => {
    const fixture = loadFixture()
    expect(fixture.schema_version).toBe(1)
    const empty = decodeEndpointRegistryProjection(JSON.stringify(fixture.empty_registry))
    expect(empty).toEqual({ version: 2, default: '', endpoints: {} })
    const registry = decodeEndpointRegistryProjection(JSON.stringify(fixture.valid_registry))
    expect(decodeEndpointRegistryProjection(JSON.stringify(registry))).toEqual(registry)
    const defaultedManaged = decodeEndpointRegistryProjection(JSON.stringify(fixture.defaulted_managed_registry))
    expect(defaultedManaged.endpoints.cloud?.routes.cloud?.relay_mode).toBe('auto')
    expect(() => decodeEndpointRegistryProjection(JSON.stringify(fixture.unknown_field_registry))).toThrowError(EndpointProjectionError)
    expect(() => decodeEndpointRegistryProjection(JSON.stringify(fixture.missing_field_registry))).toThrowError(EndpointProjectionError)
    expect(() => decodeEndpointRegistryProjection(JSON.stringify(fixture.wrong_type_registry))).toThrowError(EndpointProjectionError)
    expect(() => decodeEndpointRegistryProjection(JSON.stringify(fixture.whitespace_key_registry))).toThrowError(EndpointProjectionError)
    for (const testCase of fixture.invalid_registry_cases) {
      expect(() => decodeEndpointRegistryProjection(JSON.stringify(testCase.registry)), testCase.name).toThrowError(
        expect.objectContaining({ code: testCase.expected_error }),
      )
    }
    expect(() => decodeEndpointRegistryProjection('x'.repeat(fixture.oversize_bytes))).toThrowError(
      expect.objectContaining({ code: 'size_limit' }),
    )
    expect(fixture.oversize_bytes).toBe(endpointRegistryMaxBytes + 1)

    const nearby = fixture.local_discovery_candidate
    const discovery = create(LocalDiscoveryCandidateSchema, {
      claimedIdentity: create(EndpointDaemonIdentitySchema, {
        deviceId: nearby.claimed_device_id,
        deviceFingerprint: nearby.claimed_device_fingerprint,
      }),
      address: nearby.address,
      port: nearby.port,
      protocolVersion: nearby.protocol_version,
      announcementExpiresAtUnixNano: BigInt(Date.now() + nearby.ttl_millis) * 1_000_000n,
      announcementSignature: new Uint8Array(nearby.signature_bytes),
    })
    expect(discovery.announcementSignature).toHaveLength(64)
    expect(JSON.stringify(fixture.valid_registry)).not.toContain(discovery.address)

    const initial = decodeEndpointRegistryProjection(JSON.stringify(fixture.assembler.initial_registry))
    const forward = await assembleSequence(initial, fixture.assembler.commutative_candidates, fixture.assembler.confirmed_identity_bindings)
    const reverse = await assembleSequence(initial, [...fixture.assembler.commutative_candidates].reverse(), fixture.assembler.confirmed_identity_bindings)
    expect(reverse).toEqual(forward)
    expect(Object.keys(forward.endpoints)).toHaveLength(1)
    expect(forward.default).toBe(fixture.assembler.expected_endpoint_id)
    const fromEmpty = await assembleSequence(empty, fixture.assembler.commutative_candidates)
    expect(Object.keys(fromEmpty.endpoints)).toHaveLength(1)
    expect(fromEmpty.default).toBe(fixture.assembler.expected_new_endpoint_id)
    const endpoint = forward.endpoints[fixture.assembler.expected_endpoint_id]
    expect(endpoint?.label).toBe(fixture.assembler.expected_label)
    expect(endpoint?.connect_mode).toBe(fixture.assembler.expected_connect_mode)
    expect(Object.keys(endpoint?.routes ?? {}).sort()).toEqual(fixture.assembler.expected_route_ids)
    expect(Object.fromEntries(Object.entries(endpoint?.routes ?? {}).map(([routeId, route]) => [routeId, route.priority]))).toEqual(
      fixture.assembler.expected_route_priorities,
    )
    await expect(assembleSequence(forward, [fixture.assembler.identity_conflict_candidate])).rejects.toMatchObject({
      code: fixture.assembler.expected_conflict_error,
    })
  })
})

async function assembleSequence(
  initial: EndpointRegistryProjection,
  candidates: Candidate[],
  bindings: ConfirmedIdentityBinding[] = [],
): Promise<EndpointRegistryProjection> {
  let registry = structuredClone(initial)
  for (const [index, candidate] of candidates.entries()) {
    if (index === 0) registry = applyConfirmedIdentityBindings(registry, bindings, [candidate])
    registry = await assembleOne(registry, candidate)
  }
  return registry
}

async function assembleOne(registry: EndpointRegistryProjection, candidate: Candidate): Promise<EndpointRegistryProjection> {
  const next = structuredClone(registry)
  let matchId = ''
  for (const [endpointId, endpoint] of Object.entries(next.endpoints)) {
    if (!endpoint.device_id || !endpoint.device_fingerprint) continue
    if (endpoint.device_fingerprint === candidate.identity.device_fingerprint) {
      if (endpoint.device_id !== candidate.identity.device_id) throw { code: 'identity_conflict' }
      matchId = endpointId
    }
    if (endpoint.device_id === candidate.identity.device_id && endpoint.device_fingerprint !== candidate.identity.device_fingerprint) {
      throw { code: 'identity_conflict' }
    }
  }
  const endpointId = matchId || await deriveEndpointId(candidate.identity.device_fingerprint, next)
  const current = next.endpoints[endpointId]
  const incomingRank = sourceRank(candidate.source)
  const labelSource = current?.label_source ?? candidate.source
  const applyPolicy = candidate.source === 'local' || candidate.source === 'manual' || candidate.source === 'user'
    || candidate.source === 'share' && candidate.apply_client_policy === true
  const replaceLabel = current !== undefined && applyPolicy && labelSource !== 'user'
    && incomingRank >= sourceRank(labelSource) && candidate.suggested_label.trim() !== ''
  const label = current === undefined
    ? candidate.suggested_label.trim() || candidate.identity.device_id
    : replaceLabel ? candidate.suggested_label.trim() : current.label
  const routes = { ...(current?.routes ?? {}) }
  for (const candidateRoute of candidate.routes) {
    const existing = routes[candidateRoute.route_id]
    if (existing && existing.kind !== candidateRoute.kind) throw { code: 'route_conflict' }
    const { route_id: _routeId, ...portableRoute } = candidateRoute
    const incoming = {
      ...(portableRoute as unknown as NonNullable<typeof existing>),
      kind: candidateRoute.kind as EndpointRouteKind,
      enabled: candidateRoute.enabled !== false,
      source: candidate.source,
      policy_source: candidate.source,
    } satisfies EndpointRouteProjection
    let merged = existing
    if (!existing || incomingRank >= sourceRank(existing.source ?? 'manual')) {
      merged = existing ? withRoutePolicy(incoming, existing) : incoming
    }
    const forcePolicy = candidate.source === 'share' && candidate.apply_client_policy === true
    if (applyPolicy && merged && (forcePolicy || sourceRank(incoming.policy_source ?? candidate.source) >= sourceRank(existing?.policy_source ?? existing?.source ?? candidate.source))) {
      merged = withRoutePolicy(merged, incoming)
    }
    if (!existing && !applyPolicy) {
      merged = {
        ...(merged ?? incoming),
        manual_only: Object.values(routes).some((route) => route.enabled && !route.manual_only && route.priority !== undefined),
        policy_source: 'local',
      }
    }
    routes[candidateRoute.route_id] = merged ?? incoming
  }
  next.endpoints[endpointId] = {
    label,
    label_source: current === undefined || replaceLabel ? candidate.source : labelSource,
    device_id: candidate.identity.device_id,
    device_fingerprint: candidate.identity.device_fingerprint,
    enabled: current?.enabled ?? true,
    connect_mode: applyPolicy && candidate.connect_mode ? candidate.connect_mode : current?.connect_mode ?? 'on_demand',
    routes,
  }
  if (next.default === '') next.default = endpointId
  return decodeEndpointRegistryProjection(JSON.stringify(next))
}

function applyConfirmedIdentityBindings(
  registry: EndpointRegistryProjection,
  bindings: ConfirmedIdentityBinding[],
  candidates: Candidate[],
): EndpointRegistryProjection {
  if (bindings.length === 0) return registry
  const next = structuredClone(registry)
  const candidateIdentities = new Set(candidates.map((candidate) => identityKey(candidate.identity)))
  const seenEndpoints = new Set<string>()
  const seenIdentities = new Set<string>()
  for (const binding of [...bindings].sort((left, right) => left.endpoint_id.localeCompare(right.endpoint_id) || identityKey(left.identity).localeCompare(identityKey(right.identity)))) {
    const key = identityKey(binding.identity)
    if (!candidateIdentities.has(key)) throw { code: 'config_invalid' }
    if (seenEndpoints.has(binding.endpoint_id) || seenIdentities.has(key)) throw { code: 'identity_conflict' }
    seenEndpoints.add(binding.endpoint_id)
    seenIdentities.add(key)
    const endpoint = next.endpoints[binding.endpoint_id]
    if (!endpoint) throw { code: 'config_invalid' }
    for (const [endpointId, current] of Object.entries(next.endpoints)) {
      if (endpointId === binding.endpoint_id || !current.device_id || !current.device_fingerprint) continue
      if (current.device_id === binding.identity.device_id || current.device_fingerprint === binding.identity.device_fingerprint) {
        throw { code: 'identity_conflict' }
      }
    }
    if (endpoint.device_id || endpoint.device_fingerprint) {
      if (endpoint.device_id !== binding.identity.device_id || endpoint.device_fingerprint !== binding.identity.device_fingerprint) {
        throw { code: 'identity_conflict' }
      }
      continue
    }
    endpoint.device_id = binding.identity.device_id
    endpoint.device_fingerprint = binding.identity.device_fingerprint
  }
  return decodeEndpointRegistryProjection(JSON.stringify(next))
}

function identityKey(identity: { device_id: string; device_fingerprint: string }): string {
  return `${identity.device_id}\u0000${identity.device_fingerprint}`
}

function withRoutePolicy(base: EndpointRouteProjection, policy: EndpointRouteProjection): EndpointRouteProjection {
  const result = { ...base, enabled: policy.enabled }
  for (const key of ['manual_only', 'priority', 'policy_source'] as const) {
    if (policy[key] === undefined) delete result[key]
    else Object.assign(result, { [key]: policy[key] })
  }
  return result
}

async function deriveEndpointId(fingerprint: string, registry: EndpointRegistryProjection): Promise<string> {
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(fingerprint.trim())))
  const encoded = [...digest].map((value) => value.toString(16).padStart(2, '0')).join('')
  for (let length = 12; length <= encoded.length; length += 4) {
    const candidate = `daemon-${encoded.slice(0, length)}`
    if (!registry.endpoints[candidate]) return candidate
  }
  return `daemon-${encoded}`
}

function sourceRank(source: EndpointSource): number {
  return { lan: 0, cloud: 10, bootstrap: 20, local: 25, manual: 30, share: 40, user: 50 }[source]
}

function loadFixture(): ContractFixture {
  const path = resolve(process.cwd(), '../../shared/connection/testdata/endpoint-contract-v1.json')
  return JSON.parse(readFileSync(path, 'utf8')) as ContractFixture
}
