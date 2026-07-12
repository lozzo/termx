import {
  create,
  fromBinary,
  toBinary,
  type DescMessage,
  type MessageInitShape,
  type MessageShape,
} from '@bufbuild/protobuf'
import {
  APIRequestSchema,
  APIResponseSchema,
  EmptySchema,
  EventEnvelopeSchema,
  EventSubscribeRequestSchema,
  StatusResponseSchema,
  StorageDeleteRequestSchema,
  StorageDeleteResponseSchema,
  StorageEntrySchema,
  StorageGetRequestSchema,
  StorageListRequestSchema,
  StorageListResponseSchema,
  StoragePutRequestSchema,
  TerminalCreateRequestSchema,
  TerminalDirectoryRequestSchema,
  TerminalDirectoryResponseSchema,
  TerminalIDRequestSchema,
  TerminalInventoryItemSchema,
  TerminalListResponseSchema,
  TerminalSetMetadataRequestSchema,
  type APIRequest,
  type APIResponse,
  type EventEnvelope,
  type EventSubscribeRequest,
  type StorageDeleteResponse,
  type StorageEntry,
  type StorageListResponse,
  type TerminalInventoryItem,
} from '../generated/runtimepb/runtime_pb'

export type RuntimeAPIRequest = APIRequest
export type RuntimeAPIResponse = APIResponse
export type RuntimeEventSubscribeRequest = EventSubscribeRequest
export type RuntimeEventEnvelope = EventEnvelope

export function encodeRuntimeAPIRequest(init: MessageInitShape<typeof APIRequestSchema>): Uint8Array {
  return encodeMessage(APIRequestSchema, init)
}

export function decodeRuntimeAPIRequest(data: Uint8Array): APIRequest {
  return decodeMessage(APIRequestSchema, data)
}

export function encodeRuntimeAPIResponse(init: MessageInitShape<typeof APIResponseSchema>): Uint8Array {
  return encodeMessage(APIResponseSchema, init)
}

export function decodeRuntimeAPIResponse(data: Uint8Array): APIResponse {
  return decodeMessage(APIResponseSchema, data)
}

export function encodeRuntimeEventSubscribeRequest(init: MessageInitShape<typeof EventSubscribeRequestSchema>): Uint8Array {
  return encodeMessage(EventSubscribeRequestSchema, init)
}

export function decodeRuntimeEventSubscribeRequest(data: Uint8Array): EventSubscribeRequest {
  return decodeMessage(EventSubscribeRequestSchema, data)
}

export function encodeRuntimeEventEnvelope(init: MessageInitShape<typeof EventEnvelopeSchema>): Uint8Array {
  return encodeMessage(EventEnvelopeSchema, init)
}

export function decodeRuntimeEventEnvelope(data: Uint8Array): EventEnvelope {
  return decodeMessage(EventEnvelopeSchema, data)
}

export function encodeRuntimeRequestBody(path: string, method: string, body: unknown): Uint8Array {
  if (body === undefined || body === null) return new Uint8Array()
  switch (path) {
    case '/storage/get':
      return encodeMessage(StorageGetRequestSchema, storageGetRequestInit(body))
    case '/storage/put':
      return encodeMessage(StoragePutRequestSchema, storagePutRequestInit(body))
    case '/storage/delete':
      return encodeMessage(StorageDeleteRequestSchema, storageDeleteRequestInit(body))
    case '/storage/list':
      return encodeMessage(StorageListRequestSchema, storageListRequestInit(body))
    default:
      return encodeTerminalManagementRequest(method, body)
  }
}

export function decodeRuntimeRequestBody(path: string, method: string, body: Uint8Array): unknown {
  if (body.byteLength === 0) return {}
  switch (path) {
    case '/storage/get':
      return storageGetRequestToAPI(decodeMessage(StorageGetRequestSchema, body))
    case '/storage/put':
      return storagePutRequestToAPI(decodeMessage(StoragePutRequestSchema, body))
    case '/storage/delete':
      return storageDeleteRequestToAPI(decodeMessage(StorageDeleteRequestSchema, body))
    case '/storage/list':
      return storageListRequestToAPI(decodeMessage(StorageListRequestSchema, body))
    default:
      return decodeTerminalManagementRequest(method, body)
  }
}

export function encodeRuntimeResponseBody(path: string, method: string, body: unknown): Uint8Array {
  if (body === undefined || body === null) return new Uint8Array()
  switch (path) {
    case '/status':
    case 'status':
      return encodeMessage(StatusResponseSchema, { ok: booleanValue(asRecord(body).ok) })
    case 'list':
      return encodeMessage(TerminalListResponseSchema, {
        terminals: recordArray(asRecord(body).terminals).map(terminalInventoryItemInit),
      })
    case 'create':
      return encodeMessage(TerminalInventoryItemSchema, terminalInventoryItemInit(body))
    case 'get_directory':
      return encodeMessage(TerminalDirectoryResponseSchema, terminalDirectoryResponseInit(body))
    case 'set_metadata':
    case 'restart':
    case 'remove':
      return encodeMessage(EmptySchema, {})
    case '/storage/get':
    case '/storage/put':
      return encodeMessage(StorageEntrySchema, storageEntryInit(body))
    case '/storage/delete':
      return encodeMessage(StorageDeleteResponseSchema, storageDeleteResponseInit(body))
    case '/storage/list':
      return encodeMessage(StorageListResponseSchema, storageListResponseInit(body))
    default:
      return new Uint8Array()
  }
}

export function decodeRuntimeResponseBody(path: string, method: string, body: Uint8Array): unknown {
  switch (path) {
    case '/status':
    case 'status':
      return { ok: decodeMessage(StatusResponseSchema, body).ok }
    case 'list':
      return {
        terminals: decodeMessage(TerminalListResponseSchema, body).terminals.map(terminalInventoryItemToAPI),
      }
    case 'create':
      return terminalInventoryItemToAPI(decodeMessage(TerminalInventoryItemSchema, body))
    case 'get_directory':
      return terminalDirectoryResponseToAPI(decodeMessage(TerminalDirectoryResponseSchema, body))
    case 'set_metadata':
    case 'restart':
    case 'remove':
      return {}
    case '/storage/get':
    case '/storage/put':
      return storageEntryToAPI(decodeMessage(StorageEntrySchema, body))
    case '/storage/delete':
      return storageDeleteResponseToAPI(decodeMessage(StorageDeleteResponseSchema, body))
    case '/storage/list':
      return storageListResponseToAPI(decodeMessage(StorageListResponseSchema, body))
    default:
      if (body.byteLength === 0) return {}
      return { bytes: body, method }
  }
}

export function runtimeEventEnvelopeToRtcEvent(event: EventEnvelope): { type: string; payload?: unknown } {
  const eventType = terminalInventoryProtocolEventNames.get(event.protocolType)
  if (eventType) {
    const payload = cleanRecord({
      eventType,
      protocolType: event.protocolType,
      terminalId: event.terminalId.trim() || undefined,
      timestamp: event.timestampUnixNano !== 0n ? unixNanoToISOString(event.timestampUnixNano) : undefined,
      terminal: event.terminal ? terminalInventoryItemToAPI(event.terminal) : undefined,
    })
    return { type: 'inventory_changed', payload }
  }
  if (event.type) {
    return event.error ? { type: event.type, payload: { error: event.error } } : { type: event.type }
  }
  return {
    type: `protocol_event_${event.protocolType}`,
    payload: eventEnvelopeToAPI(event),
  }
}

function encodeMessage<Desc extends DescMessage>(schema: Desc, init: MessageInitShape<Desc>): Uint8Array {
  return toBinary(schema, create(schema, init))
}

function decodeMessage<Desc extends DescMessage>(schema: Desc, data: Uint8Array): MessageShape<Desc> {
  return fromBinary(schema, data)
}

function encodeTerminalManagementRequest(method: string, body: unknown): Uint8Array {
  switch (method) {
    case 'create':
      return encodeMessage(TerminalCreateRequestSchema, terminalCreateRequestInit(body))
    case 'set_metadata':
      return encodeMessage(TerminalSetMetadataRequestSchema, terminalSetMetadataRequestInit(body))
    case 'restart':
    case 'remove':
      return encodeMessage(TerminalIDRequestSchema, terminalIDRequestInit(body))
    case 'get_directory':
      return encodeMessage(TerminalDirectoryRequestSchema, terminalDirectoryRequestInit(body))
    case 'list':
    case 'GET':
      return new Uint8Array()
    default:
      return new Uint8Array()
  }
}

function decodeTerminalManagementRequest(method: string, body: Uint8Array): unknown {
  switch (method) {
    case 'create':
      return terminalCreateRequestToAPI(decodeMessage(TerminalCreateRequestSchema, body))
    case 'set_metadata':
      return terminalSetMetadataRequestToAPI(decodeMessage(TerminalSetMetadataRequestSchema, body))
    case 'restart':
    case 'remove':
      return terminalIDRequestToAPI(decodeMessage(TerminalIDRequestSchema, body))
    case 'get_directory':
      return terminalIDRequestToAPI(decodeMessage(TerminalDirectoryRequestSchema, body))
    default:
      return {}
  }
}

function terminalCreateRequestInit(value: unknown): MessageInitShape<typeof TerminalCreateRequestSchema> {
  const record = asRecord(value)
  return {
    name: stringValue(record.name),
    command: stringArray(record.command),
    dir: stringValue(record.dir ?? record.cwd),
    env: stringArray(record.env),
    tags: stringMap(record.tags),
    scrollbackSize: numberValue(record.scrollback_size ?? record.scrollbackSize),
    scrollbackMaxBytes: bigintValue(record.scrollback_max_bytes ?? record.scrollbackMaxBytes),
    scrollbackMaxAgeSeconds: bigintValue(record.scrollback_max_age_seconds ?? record.scrollbackMaxAgeSeconds),
  }
}

function terminalCreateRequestToAPI(value: MessageShape<typeof TerminalCreateRequestSchema>): Record<string, unknown> {
  return cleanRecord({
    name: value.name,
    command: value.command,
    dir: value.dir,
    env: value.env,
    tags: value.tags,
    scrollback_size: value.scrollbackSize,
    scrollback_max_bytes: numberFromBigInt(value.scrollbackMaxBytes),
    scrollback_max_age_seconds: numberFromBigInt(value.scrollbackMaxAgeSeconds),
  })
}

function terminalSetMetadataRequestInit(value: unknown): MessageInitShape<typeof TerminalSetMetadataRequestSchema> {
  const record = asRecord(value)
  return {
    terminalId: stringValue(record.terminal_id ?? record.terminalId),
    name: stringValue(record.name),
    tags: stringMap(record.tags),
  }
}

function terminalSetMetadataRequestToAPI(value: MessageShape<typeof TerminalSetMetadataRequestSchema>): Record<string, unknown> {
  return cleanRecord({
    terminal_id: value.terminalId,
    name: value.name,
    tags: value.tags,
  })
}

function terminalIDRequestInit(value: unknown): MessageInitShape<typeof TerminalIDRequestSchema> {
  const record = asRecord(value)
  return { terminalId: stringValue(record.terminal_id ?? record.terminalId) }
}

function terminalDirectoryRequestInit(value: unknown): MessageInitShape<typeof TerminalDirectoryRequestSchema> {
  const record = asRecord(value)
  return { terminalId: stringValue(record.terminal_id ?? record.terminalId) }
}

function terminalIDRequestToAPI(value: { terminalId: string }): Record<string, unknown> {
  return cleanRecord({
    terminal_id: value.terminalId,
  })
}

function terminalDirectoryResponseInit(value: unknown): MessageInitShape<typeof TerminalDirectoryResponseSchema> {
  const record = asRecord(value)
  return {
    terminalId: stringValue(record.terminal_id ?? record.terminalId),
    path: stringValue(record.path),
    source: stringValue(record.source),
  }
}

function terminalDirectoryResponseToAPI(value: MessageShape<typeof TerminalDirectoryResponseSchema>): Record<string, unknown> {
  return cleanRecord({
    terminal_id: value.terminalId,
    path: value.path,
    source: value.source,
  })
}

function terminalInventoryItemInit(value: unknown): MessageInitShape<typeof TerminalInventoryItemSchema> {
  const record = asRecord(value)
  return {
    terminalId: stringValue(record.terminal_id ?? record.terminalId ?? record.id ?? record.ID),
    name: stringValue(record.name ?? record.title ?? record.Name),
    state: stringValue(record.state ?? record.State),
    command: stringArray(record.command ?? record.Command),
    cols: numberValue(record.cols ?? record.Cols),
    rows: numberValue(record.rows ?? record.Rows),
    cwd: stringValue(record.cwd),
    environment: stringValue(record.environment),
    sizeLocked: booleanValue(record.size_locked ?? record.sizeLocked),
    sizeLockMode: stringValue(record.size_lock_mode ?? record.sizeLockMode),
    resizeOwnerAttachmentCount: numberValue(record.resize_owner_attachment_count ?? record.resizeOwnerAttachmentCount),
  }
}

function terminalInventoryItemToAPI(value: TerminalInventoryItem): Record<string, unknown> {
  return cleanRecord({
    terminal_id: value.terminalId,
    name: value.name,
    title: value.name,
    state: value.state,
    command: value.command,
    cols: value.cols,
    rows: value.rows,
    cwd: value.cwd,
    environment: value.environment,
    size_locked: value.sizeLocked,
    size_lock_mode: value.sizeLockMode,
    resize_ownership: value.resizeOwnership ? resizeOwnershipToAPI(value.resizeOwnership) : undefined,
    resize_owner_attachment_count: value.resizeOwnerAttachmentCount,
  })
}

function resizeOwnershipToAPI(value: NonNullable<TerminalInventoryItem['resizeOwnership']>): Record<string, unknown> {
  return cleanRecord({
    owner_attachment_id: value.ownerAttachmentId,
    owner_surface_id: value.ownerSurfaceId,
    owner_view_id: value.ownerViewId,
    owner_remote_addr: value.ownerRemoteAddr,
    size: value.size ? { cols: value.size.cols, rows: value.size.rows } : undefined,
    size_locked: value.sizeLocked,
    epoch: numberFromBigInt(value.epoch),
  })
}

function storageGetRequestInit(value: unknown): MessageInitShape<typeof StorageGetRequestSchema> {
  const record = asRecord(value)
  return {
    appId: stringValue(record.app_id ?? record.appId),
    scope: stringValue(record.scope),
    ownerId: stringValue(record.owner_id ?? record.ownerId),
    key: stringValue(record.key),
  }
}

function storageGetRequestToAPI(value: MessageShape<typeof StorageGetRequestSchema>): Record<string, unknown> {
  return cleanRecord({
    app_id: value.appId,
    scope: value.scope,
    owner_id: value.ownerId,
    key: value.key,
  })
}

function storagePutRequestInit(value: unknown): MessageInitShape<typeof StoragePutRequestSchema> {
  const record = asRecord(value)
  return {
    appId: stringValue(record.app_id ?? record.appId),
    scope: stringValue(record.scope),
    ownerId: stringValue(record.owner_id ?? record.ownerId),
    key: stringValue(record.key),
    value: bytesValue(record.value),
    checkVersion: booleanValue(record.check_version ?? record.checkVersion),
    expectedVersion: bigintValue(record.expected_version ?? record.expectedVersion),
  }
}

function storagePutRequestToAPI(value: MessageShape<typeof StoragePutRequestSchema>): Record<string, unknown> {
  return cleanRecord({
    app_id: value.appId,
    scope: value.scope,
    owner_id: value.ownerId,
    key: value.key,
    value: value.value,
    check_version: value.checkVersion,
    expected_version: numberFromBigInt(value.expectedVersion),
  })
}

function storageDeleteRequestInit(value: unknown): MessageInitShape<typeof StorageDeleteRequestSchema> {
  const record = asRecord(value)
  return {
    appId: stringValue(record.app_id ?? record.appId),
    scope: stringValue(record.scope),
    ownerId: stringValue(record.owner_id ?? record.ownerId),
    key: stringValue(record.key),
    checkVersion: booleanValue(record.check_version ?? record.checkVersion),
    expectedVersion: bigintValue(record.expected_version ?? record.expectedVersion),
  }
}

function storageDeleteRequestToAPI(value: MessageShape<typeof StorageDeleteRequestSchema>): Record<string, unknown> {
  return cleanRecord({
    app_id: value.appId,
    scope: value.scope,
    owner_id: value.ownerId,
    key: value.key,
    check_version: value.checkVersion,
    expected_version: numberFromBigInt(value.expectedVersion),
  })
}

function storageListRequestInit(value: unknown): MessageInitShape<typeof StorageListRequestSchema> {
  const record = asRecord(value)
  return {
    appId: stringValue(record.app_id ?? record.appId),
    scope: stringValue(record.scope),
    ownerId: stringValue(record.owner_id ?? record.ownerId),
    prefix: stringValue(record.prefix),
  }
}

function storageListRequestToAPI(value: MessageShape<typeof StorageListRequestSchema>): Record<string, unknown> {
  return cleanRecord({
    app_id: value.appId,
    scope: value.scope,
    owner_id: value.ownerId,
    prefix: value.prefix,
  })
}

function storageEntryInit(value: unknown): MessageInitShape<typeof StorageEntrySchema> {
  const record = asRecord(value)
  return {
    appId: stringValue(record.app_id ?? record.appId),
    scope: stringValue(record.scope),
    ownerId: stringValue(record.owner_id ?? record.ownerId),
    key: stringValue(record.key),
    value: bytesValue(record.value),
    version: bigintValue(record.version),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
  }
}

function storageEntryToAPI(value: StorageEntry): Record<string, unknown> {
  return cleanRecord({
    app_id: value.appId,
    scope: value.scope,
    owner_id: value.ownerId,
    key: value.key,
    value: value.value,
    version: numberFromBigInt(value.version),
    updated_at: value.updatedAt,
  })
}

function storageDeleteResponseInit(value: unknown): MessageInitShape<typeof StorageDeleteResponseSchema> {
  const record = asRecord(value)
  return {
    appId: stringValue(record.app_id ?? record.appId),
    scope: stringValue(record.scope),
    ownerId: stringValue(record.owner_id ?? record.ownerId),
    key: stringValue(record.key),
    deleted: booleanValue(record.deleted),
    version: bigintValue(record.version),
  }
}

function storageDeleteResponseToAPI(value: StorageDeleteResponse): Record<string, unknown> {
  return cleanRecord({
    app_id: value.appId,
    scope: value.scope,
    owner_id: value.ownerId,
    key: value.key,
    deleted: value.deleted,
    version: numberFromBigInt(value.version),
  })
}

function storageListResponseInit(value: unknown): MessageInitShape<typeof StorageListResponseSchema> {
  return { entries: recordArray(asRecord(value).entries).map(storageEntryInit) }
}

function storageListResponseToAPI(value: StorageListResponse): Record<string, unknown> {
  return { entries: value.entries.map(storageEntryToAPI) }
}

function eventEnvelopeToAPI(value: EventEnvelope): Record<string, unknown> {
  return cleanRecord({
    type: value.type,
    protocol_type: value.protocolType,
    terminal_id: value.terminalId,
    timestamp: value.timestampUnixNano !== 0n ? unixNanoToISOString(value.timestampUnixNano) : undefined,
    terminal: value.terminal ? terminalInventoryItemToAPI(value.terminal) : undefined,
    error: value.error,
  })
}

const terminalInventoryProtocolEventNames = new Map<number, string>([
  [1, 'terminal_created'],
  [2, 'terminal_state_changed'],
  [3, 'terminal_resized'],
  [4, 'terminal_removed'],
  [10, 'terminal_metadata_changed'],
])

function unixNanoToISOString(value: bigint): string {
  const ms = Number(value / 1_000_000n)
  return new Date(ms).toISOString().replace('.000Z', 'Z')
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function recordArray(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.filter((item): item is Record<string, unknown> => typeof item === 'object' && item !== null && !Array.isArray(item)) : []
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function numberValue(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? Math.trunc(value) : 0
}

function booleanValue(value: unknown): boolean {
  return value === true
}

function bigintValue(value: unknown): bigint {
  if (typeof value === 'bigint') return value
  if (typeof value === 'number' && Number.isFinite(value)) return BigInt(Math.trunc(value))
  if (typeof value === 'string' && value.trim()) return BigInt(value)
  return 0n
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

function stringMap(value: unknown): Record<string, string> {
  const record = asRecord(value)
  const out: Record<string, string> = {}
  for (const [key, item] of Object.entries(record)) {
    if (typeof item === 'string') out[key] = item
  }
  return out
}

function bytesValue(value: unknown): Uint8Array {
  if (value instanceof Uint8Array) return value
  if (value instanceof ArrayBuffer) return new Uint8Array(value)
  if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
  if (typeof value === 'string') return new TextEncoder().encode(value)
  return new Uint8Array()
}

function numberFromBigInt(value: bigint): number {
  const max = BigInt(Number.MAX_SAFE_INTEGER)
  const min = BigInt(Number.MIN_SAFE_INTEGER)
  if (value > max) return Number.MAX_SAFE_INTEGER
  if (value < min) return Number.MIN_SAFE_INTEGER
  return Number(value)
}

function cleanRecord<T extends Record<string, unknown>>(record: T): T {
  for (const key of Object.keys(record)) {
    const value = record[key]
    if (
      value === undefined ||
      value === '' ||
      value === 0 ||
      value === false ||
      (Array.isArray(value) && value.length === 0) ||
      (isPlainRecord(value) && Object.keys(value).length === 0)
    ) {
      delete record[key]
    }
  }
  return record
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
