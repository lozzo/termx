export const endpointRegistryVersion = 2
export const endpointRegistryMaxBytes = 1 << 20

export type EndpointRouteKind = 'local-unix' | 'ssh-stdio' | 'direct-tls' | 'managed-webrtc'
export type EndpointConnectMode = 'auto' | 'on_demand' | 'manual'
export type EndpointSource = 'local' | 'cloud' | 'bootstrap' | 'manual' | 'share' | 'lan' | 'user'

export interface EndpointRouteProjection {
  kind: EndpointRouteKind
  enabled: boolean
  manual_only?: boolean
  priority?: number
  credential_ref?: string
  source?: EndpointSource
  policy_source?: EndpointSource
  socket?: string
  host?: string
  port?: number
  user?: string
  proxy_jump?: string
  remote_socket?: string
  host_key_fingerprints?: string[]
  addresses?: string[]
  server_name?: string
  target_device_id?: string
  account_profile?: string
  relay_mode?: 'auto' | 'direct' | 'relay_only' | 'smart_route'
}

export interface EndpointProjection {
  label: string
  label_source?: EndpointSource
  device_id?: string
  device_fingerprint?: string
  enabled: boolean
  connect_mode: EndpointConnectMode
  selection?: { hedge_delay: string }
  routes: Record<string, EndpointRouteProjection>
}

export interface EndpointRegistryProjection {
  version: 2
  default: string
  endpoints: Record<string, EndpointProjection>
}

/** EndpointProjectionError 是共享 UI 对 native endpoint 投影的稳定 fail-closed 错误。 */
export class EndpointProjectionError extends Error {
  constructor(readonly code: 'config_invalid' | 'size_limit' | 'unsupported_version' | 'identity_conflict', message: string) {
    super(message)
  }
}

/**
 * decodeEndpointRegistryProjection 严格消费 native 脱敏 registry 投影。
 * 它不持久化、不合并 candidate、不建立 transport，也不接触 credential body、CapabilityGrant 或 Cloud token。
 */
export function decodeEndpointRegistryProjection(payload: string): EndpointRegistryProjection {
  if (new TextEncoder().encode(payload).byteLength > endpointRegistryMaxBytes) {
    throw new EndpointProjectionError('size_limit', 'endpoint registry exceeds size limit')
  }
  let value: unknown
  try {
    value = JSON.parse(payload)
  } catch {
    throw new EndpointProjectionError('config_invalid', 'endpoint registry JSON is invalid')
  }
  const root = record(value, 'registry')
  exactKeys(root, ['version', 'default', 'endpoints'])
  if (root.version !== endpointRegistryVersion) {
    throw new EndpointProjectionError('unsupported_version', 'unsupported endpoint registry version')
  }
  const endpointsRecord = record(root.endpoints, 'endpoints')
  const endpoints: Record<string, EndpointProjection> = {}
  const deviceIds = new Map<string, string>()
  const fingerprints = new Map<string, string>()
  for (const endpointId of Object.keys(endpointsRecord).sort()) {
    identifier(endpointId, 'endpoint')
    const endpoint = parseEndpoint(endpointId, endpointsRecord[endpointId])
    if (endpoint.device_id !== undefined && endpoint.device_fingerprint !== undefined) {
      const existingDeviceEndpoint = deviceIds.get(endpoint.device_id)
      if (existingDeviceEndpoint !== undefined) identityConflict('device_id is pinned by multiple endpoints')
      const existingFingerprintEndpoint = fingerprints.get(endpoint.device_fingerprint)
      if (existingFingerprintEndpoint !== undefined) identityConflict('device_fingerprint is pinned by multiple endpoints')
      deviceIds.set(endpoint.device_id, endpointId)
      fingerprints.set(endpoint.device_fingerprint, endpointId)
    }
    endpoints[endpointId] = endpoint
  }
  const defaultEndpoint = string(root.default, 'default')
  if (defaultEndpoint !== defaultEndpoint.trim()) invalid('default endpoint is not canonical')
  if (Object.keys(endpoints).length > 0 && (!endpoints[defaultEndpoint] || !endpoints[defaultEndpoint].enabled)) {
    invalid('default endpoint is missing or disabled')
  }
  if (Object.keys(endpoints).length === 0 && defaultEndpoint !== '') invalid('empty endpoint registry cannot define a default endpoint')
  return { version: 2, default: defaultEndpoint, endpoints }
}

function parseEndpoint(endpointId: string, value: unknown): EndpointProjection {
  const endpoint = record(value, `endpoint ${endpointId}`)
  exactKeys(endpoint, ['label', 'label_source', 'device_id', 'device_fingerprint', 'enabled', 'connect_mode', 'selection', 'routes'])
  const deviceId = optionalString(endpoint.device_id)
  const fingerprint = optionalString(endpoint.device_fingerprint)
  if (endpoint.device_id !== undefined && string(endpoint.device_id, 'device_id') !== deviceId) invalid('daemon identity is not canonical')
  if (endpoint.device_fingerprint !== undefined && string(endpoint.device_fingerprint, 'device_fingerprint') !== fingerprint) invalid('daemon identity is not canonical')
  if ((deviceId === '') !== (fingerprint === '') || /[\s\p{Cc}]/u.test(deviceId) || /[\s\p{Cc}]/u.test(fingerprint)) invalid('daemon identity is incomplete')
  const routesRecord = record(endpoint.routes, 'routes')
  const routes: Record<string, EndpointRouteProjection> = {}
  let automaticRoutes = 0
  let prioritizedRoutes = 0
  for (const routeId of Object.keys(routesRecord).sort()) {
    identifier(routeId, 'route')
    const route = parseRoute(routeId, routesRecord[routeId], deviceId, fingerprint)
    routes[routeId] = route
    if (route.enabled && !route.manual_only) {
      automaticRoutes += 1
      if (route.priority !== undefined) prioritizedRoutes += 1
    }
  }
  if (Object.keys(routes).length === 0 || prioritizedRoutes > 0 && prioritizedRoutes !== automaticRoutes) invalid('route priority set is incomplete')
  const connectMode = string(endpoint.connect_mode ?? 'on_demand', 'connect_mode')
  if (!['auto', 'on_demand', 'manual'].includes(connectMode)) invalid('connect_mode is invalid')
  const result: EndpointProjection = {
    label: optionalString(endpoint.label) || endpointId,
    enabled: optionalBoolean(endpoint.enabled, true),
    connect_mode: connectMode as EndpointConnectMode,
    routes,
  }
  if (endpoint.label_source !== undefined) result.label_source = source(endpoint.label_source)
  if (deviceId !== '') result.device_id = deviceId
  if (fingerprint !== '') result.device_fingerprint = fingerprint
  if (endpoint.selection !== undefined) {
    const selection = record(endpoint.selection, 'selection')
    exactKeys(selection, ['hedge_delay'])
    const hedgeDelay = string(selection.hedge_delay, 'hedge_delay')
    const millis = durationMillis(hedgeDelay)
    if (millis < 0 || millis > 30_000) invalid('hedge_delay is outside the supported range')
    result.selection = { hedge_delay: hedgeDelay }
  }
  return result
}

function parseRoute(routeId: string, value: unknown, deviceId: string, fingerprint: string): EndpointRouteProjection {
  const route = record(value, `route ${routeId}`)
  exactKeys(route, [
    'kind', 'enabled', 'manual_only', 'priority', 'credential_ref', 'source', 'policy_source', 'socket', 'host', 'port', 'user',
    'proxy_jump', 'remote_socket', 'host_key_fingerprints', 'addresses', 'server_name', 'target_device_id', 'account_profile', 'relay_mode',
  ])
  const kind = string(route.kind, 'kind')
  if (!['local-unix', 'ssh-stdio', 'direct-tls', 'managed-webrtc'].includes(kind)) invalid('route kind is invalid')
  const result: EndpointRouteProjection = { kind: kind as EndpointRouteKind, enabled: optionalBoolean(route.enabled, true) }
  if (route.manual_only !== undefined) result.manual_only = boolean(route.manual_only, 'manual_only')
  if (route.priority !== undefined) {
    const priority = integer(route.priority, 'priority')
    if (priority < 0 || priority > 2_147_483_647) invalid('route priority is invalid')
    result.priority = priority
  }
  for (const key of ['credential_ref', 'socket', 'host', 'user', 'proxy_jump', 'remote_socket', 'server_name', 'target_device_id', 'account_profile'] as const) {
    if (route[key] !== undefined) canonicalRouteText(route[key], key)
  }
  copyOptionalString(route, result as unknown as Record<string, unknown>, 'credential_ref')
  if (route.source !== undefined) result.source = source(route.source)
  if (route.policy_source !== undefined) result.policy_source = source(route.policy_source)
  for (const key of ['socket', 'host', 'user', 'proxy_jump', 'remote_socket', 'server_name', 'target_device_id', 'account_profile'] as const) {
    copyOptionalString(route, result as unknown as Record<string, unknown>, key)
  }
  if (route.port !== undefined) {
    const port = integer(route.port, 'port')
    if (port < 0 || port > 65535) invalid('port is invalid')
    result.port = port
  }
  if (route.host_key_fingerprints !== undefined) result.host_key_fingerprints = stringArray(route.host_key_fingerprints, 'host_key_fingerprints')
  if (route.addresses !== undefined) result.addresses = stringArray(route.addresses, 'addresses')
  if (route.relay_mode !== undefined) {
    const relayMode = string(route.relay_mode, 'relay_mode')
    if (!['auto', 'direct', 'relay_only', 'smart_route'].includes(relayMode)) invalid('relay_mode is invalid')
    result.relay_mode = relayMode as NonNullable<EndpointRouteProjection['relay_mode']>
  } else if (kind === 'managed-webrtc') {
    result.relay_mode = 'auto'
  }
  const hasSSH = optionalString(route.host) !== '' || route.port !== undefined || optionalString(route.user) !== '' || optionalString(route.proxy_jump) !== '' || optionalString(route.remote_socket) !== '' || route.host_key_fingerprints !== undefined
  const hasDirect = route.addresses !== undefined || optionalString(route.server_name) !== ''
  const hasManaged = optionalString(route.target_device_id) !== '' || optionalString(route.account_profile) !== '' || route.relay_mode !== undefined
  if (kind === 'local-unix' && (optionalString(route.socket) === '' || hasSSH || hasDirect || hasManaged || optionalString(route.credential_ref) !== '')) invalid('local-unix route fields are invalid')
  if (kind === 'ssh-stdio' && (optionalString(route.host) === '' || optionalString(route.socket) !== '' || hasDirect || hasManaged)) invalid('ssh-stdio route fields are invalid')
  if (kind === 'direct-tls' && (deviceId === '' || fingerprint === '' || !Array.isArray(route.addresses) || route.addresses.length === 0 || optionalString(route.socket) !== '' || hasSSH || hasManaged)) invalid('direct-tls route fields are invalid')
  if (kind === 'managed-webrtc' && (deviceId === '' || fingerprint === '' || optionalString(route.target_device_id) !== deviceId || result.relay_mode === undefined || optionalString(route.socket) !== '' || hasSSH || hasDirect)) invalid('managed-webrtc route fields are invalid')
  return result
}

function copyOptionalString(sourceRecord: Record<string, unknown>, target: Record<string, unknown>, key: string): void {
  if (sourceRecord[key] !== undefined) target[key] = string(sourceRecord[key], key)
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) invalid(`${name} must be an object`)
  return value as Record<string, unknown>
}

function exactKeys(value: Record<string, unknown>, allowed: readonly string[]): void {
  const allowedSet = new Set(allowed)
  const unknown = Object.keys(value).filter((key) => !allowedSet.has(key)).sort()[0]
  if (unknown !== undefined) invalid(`unknown field ${unknown}`)
}

function source(value: unknown): EndpointSource {
  const parsed = string(value, 'source')
  if (!['local', 'cloud', 'bootstrap', 'manual', 'share', 'lan', 'user'].includes(parsed)) invalid('source is invalid')
  return parsed as EndpointSource
}

function string(value: unknown, name: string): string {
  if (typeof value !== 'string') invalid(`${name} must be a string`)
  return value
}

function optionalString(value: unknown): string {
  return value === undefined ? '' : string(value, 'field').trim()
}

function boolean(value: unknown, name: string): boolean {
  if (typeof value !== 'boolean') invalid(`${name} must be a boolean`)
  return value
}

function optionalBoolean(value: unknown, fallback: boolean): boolean {
  return value === undefined ? fallback : boolean(value, 'field')
}

function integer(value: unknown, name: string): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) invalid(`${name} must be an integer`)
  return value
}

function stringArray(value: unknown, name: string): string[] {
  if (!Array.isArray(value)) invalid(`${name} must be a string array`)
  const values = value.map((item) => canonicalRouteText(item, name, true))
  if (new Set(values).size !== values.length) invalid(`${name} contains a duplicate value`)
  return values.sort()
}

function canonicalRouteText(value: unknown, name: string, required = false): string {
  const parsed = string(value, name)
  if ((required && parsed === '') || parsed !== parsed.trim() || /\p{Cc}/u.test(parsed)) {
    invalid(`${name} is not canonical`)
  }
  return parsed
}

function identifier(value: string, kind: string): void {
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value)) invalid(`${kind} identifier is invalid`)
}

function durationMillis(value: string): number {
  const match = /^(\d+)(ms|s)$/.exec(value)
  if (!match) invalid('hedge_delay is invalid')
  const amount = Number(match[1])
  return match[2] === 's' ? amount * 1000 : amount
}

function invalid(message: string): never {
  throw new EndpointProjectionError('config_invalid', message)
}

function identityConflict(message: string): never {
  throw new EndpointProjectionError('identity_conflict', message)
}
