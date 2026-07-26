import { create, fromJson, toJson, type DescMessage, type JsonValue, type MessageShape } from '@bufbuild/protobuf'

export class APIError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message)
  }
}

function cookie(name: string): string {
  if (typeof document === 'undefined') return ''
  return document.cookie.split('; ').find((value) => value.startsWith(`${name}=`))?.slice(name.length + 1) ?? ''
}

let refreshInFlight: Promise<boolean> | undefined

async function refreshSession(): Promise<boolean> {
  const csrf = cookie('muxvia_cloud_csrf')
  if (!csrf) return false
  if (!refreshInFlight) {
    refreshInFlight = fetch('/api/account/refresh', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-Muxvia-CSRF': csrf },
    }).then((response) => response.ok).catch(() => false).finally(() => { refreshInFlight = undefined })
  }
  return refreshInFlight
}

async function fetchWithSession(path: string, init?: RequestInit): Promise<Response> {
  let response = await fetch(path, init)
  const publicAccountPath = path === '/api/account/login' || path === '/api/account/register' || path === '/api/account/refresh'
  if (response.status === 401 && !publicAccountPath && await refreshSession()) {
    response = await fetch(path, init)
  }
  return response
}

async function decode<Schema extends DescMessage>(response: Response, schema: Schema): Promise<MessageShape<Schema>> {
  const body = await response.text()
  const json = body ? JSON.parse(body) as JsonValue : {}
  if (!response.ok) {
    const error = json as { error?: string; message?: string }
    throw new APIError(response.status, error.error ?? error.message ?? `请求失败（${response.status}）`)
  }
  return fromJson(schema, json)
}

export async function protoGet<Schema extends DescMessage>(path: string, schema: Schema): Promise<MessageShape<Schema>> {
  return decode(await fetchWithSession(path, { credentials: 'same-origin', cache: 'no-store' }), schema)
}

export async function protoSend<RequestSchema extends DescMessage, ResponseSchema extends DescMessage>(
  path: string,
  requestSchema: RequestSchema,
  request: MessageShape<RequestSchema>,
  responseSchema: ResponseSchema,
  method = 'POST',
): Promise<MessageShape<ResponseSchema>> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const csrf = cookie('muxvia_cloud_csrf')
  if (csrf) headers['X-Muxvia-CSRF'] = csrf
  const response = await fetchWithSession(path, {
    method,
    credentials: 'same-origin',
    headers,
    body: JSON.stringify(toJson(requestSchema, request, { useProtoFieldName: true })),
  })
  return decode(response, responseSchema)
}

export function empty<Schema extends DescMessage>(schema: Schema): MessageShape<Schema> {
  return create(schema)
}
