import { create, fromJson, toJson, type DescMessage, type JsonValue, type MessageShape } from '@bufbuild/protobuf'

export class APIError extends Error {
  constructor(public readonly status: number, message: string, public readonly correlationID = '', public readonly code = '') {
    super(message)
  }
}

function cookie(name: string): string {
  if (typeof document === 'undefined') return ''
  return document.cookie.split('; ').find((value) => value.startsWith(`${name}=`))?.slice(name.length + 1) ?? ''
}

let refreshInFlight: Promise<boolean> | undefined

async function refreshSession(): Promise<boolean> {
  const csrf = cookie('anytty_cloud_csrf')
  if (!csrf) return false
  if (!refreshInFlight) {
    refreshInFlight = fetch('/api/account/refresh', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-AnyTTY-CSRF': csrf },
    }).then((response) => response.ok).catch(() => false).finally(() => { refreshInFlight = undefined })
  }
  return refreshInFlight
}

async function fetchWithSession(path: string, init?: RequestInit): Promise<Response> {
  let response = await fetch(path, init)
	const publicAccountPath = path === '/api/account/login' || path === '/api/account/refresh' || path === '/api/account/setup/redeem'
  if (response.status === 401 && !publicAccountPath && await refreshSession()) {
    response = await fetch(path, init)
  }
  return response
}

async function decode<Schema extends DescMessage>(response: Response, schema: Schema): Promise<MessageShape<Schema>> {
  const body = await response.text()
  if (!response.ok) {
    let error: Record<string, unknown> = {}
    try {
      const parsed = body ? JSON.parse(body) : {}
      if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) error = parsed as Record<string, unknown>
    } catch {}
    const requestID = typeof error.request_id === 'string' ? error.request_id : ''
    const correlationID = requestID || response.headers.get('X-Request-ID') || ''
    const message = typeof error.message === 'string' && error.message ? error.message : '请求失败，请稍后重试。'
    const code = typeof error.code === 'string' && error.code ? error.code : 'http_error'
    throw new APIError(response.status, message, correlationID, code)
  }
  const json = body ? JSON.parse(body) as JsonValue : {}
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
  const csrf = cookie('anytty_cloud_csrf')
  if (csrf) headers['X-AnyTTY-CSRF'] = csrf
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
