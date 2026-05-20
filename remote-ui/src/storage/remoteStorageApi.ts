import type { RtcSession } from '../core/transport'

export type RemoteStorageScope = 'public' | 'private'

export interface RemoteStorageEntry {
  appId: string
  scope: RemoteStorageScope | string
  ownerId: string
  key: string
  value: Uint8Array
  version: number
  updatedAt?: string | undefined
}

export interface RemoteStorageApi {
  get(input: RemoteStorageKey): Promise<RemoteStorageEntry>
  put(input: RemoteStoragePutInput): Promise<RemoteStorageEntry>
  delete(input: RemoteStorageDeleteInput): Promise<{ deleted: boolean; version: number }>
  list(input: RemoteStorageListInput): Promise<RemoteStorageEntry[]>
}

export interface RemoteStorageKey {
  appId: string
  scope?: RemoteStorageScope | string | undefined
  ownerId?: string | undefined
  key: string
}

export interface RemoteStoragePutInput extends RemoteStorageKey {
  value: Uint8Array | string
  expectedVersion?: number | undefined
}

export interface RemoteStorageDeleteInput extends RemoteStorageKey {
  expectedVersion?: number | undefined
}

export interface RemoteStorageListInput {
  appId: string
  scope?: RemoteStorageScope | string | undefined
  ownerId?: string | undefined
  prefix?: string | undefined
}

const defaultStorageScope: RemoteStorageScope = 'public'

export function createRemoteStorageApi(session: Pick<RtcSession, 'openApi'>): RemoteStorageApi {
  const request = async <TResponse>(path: string, params: Record<string, unknown>): Promise<TResponse> => {
    const channel = await session.openApi()
    return await channel.request<TResponse>('POST', { path, params })
  }

  return {
    async get(input) {
      return normalizeStorageEntry(await request<RawStorageEntry>('/storage/get', storageKeyParams(input)))
    },
    async put(input) {
      return normalizeStorageEntry(await request<RawStorageEntry>('/storage/put', {
        ...storageKeyParams(input),
        value: storageValue(input.value),
        ...(typeof input.expectedVersion === 'number'
          ? { check_version: true, expected_version: input.expectedVersion }
          : {}),
      }))
    },
    async delete(input) {
      const response = await request<{ deleted?: boolean; version?: number }>('/storage/delete', {
        ...storageKeyParams(input),
        ...(typeof input.expectedVersion === 'number'
          ? { check_version: true, expected_version: input.expectedVersion }
          : {}),
      })
      return { deleted: response.deleted === true, version: numberValue(response.version) }
    },
    async list(input) {
      const response = await request<{ entries?: RawStorageEntry[] }>('/storage/list', {
        app_id: input.appId,
        scope: input.scope ?? defaultStorageScope,
        ...(input.ownerId ? { owner_id: input.ownerId } : {}),
        ...(input.prefix ? { prefix: input.prefix } : {}),
      })
      return Array.isArray(response.entries) ? response.entries.map(normalizeStorageEntry) : []
    },
  }
}

export function storageText(entry: Pick<RemoteStorageEntry, 'value'>): string {
  return new TextDecoder().decode(entry.value)
}

function storageKeyParams(input: RemoteStorageKey): Record<string, unknown> {
  return {
    app_id: input.appId,
    scope: input.scope ?? defaultStorageScope,
    ...(input.ownerId ? { owner_id: input.ownerId } : {}),
    key: input.key,
  }
}

function storageValue(value: Uint8Array | string): Uint8Array {
  return typeof value === 'string' ? new TextEncoder().encode(value) : value
}

type RawStorageEntry = {
  app_id?: string
  appId?: string
  scope?: string
  owner_id?: string
  ownerId?: string
  key?: string
  value?: Uint8Array | ArrayBuffer | string | number[]
  version?: number
  updated_at?: string
  updatedAt?: string
}

function normalizeStorageEntry(entry: RawStorageEntry): RemoteStorageEntry {
  return {
    appId: stringValue(entry.app_id ?? entry.appId),
    scope: stringValue(entry.scope) || defaultStorageScope,
    ownerId: stringValue(entry.owner_id ?? entry.ownerId),
    key: stringValue(entry.key),
    value: bytesValue(entry.value),
    version: numberValue(entry.version),
    updatedAt: stringValue(entry.updated_at ?? entry.updatedAt) || undefined,
  }
}

function bytesValue(value: unknown): Uint8Array {
  if (value instanceof Uint8Array) return value
  if (value instanceof ArrayBuffer) return new Uint8Array(value)
  if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
  if (Array.isArray(value)) return new Uint8Array(value.filter((item): item is number => typeof item === 'number'))
  if (typeof value === 'string') return new TextEncoder().encode(value)
  return new Uint8Array()
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function numberValue(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}
