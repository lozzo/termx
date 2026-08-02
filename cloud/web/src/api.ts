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

type ErrorEnvelope = { code: string; message: string; request_id: string }

function isErrorEnvelope(value: unknown): value is ErrorEnvelope {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const candidate = value as Record<string, unknown>
  return typeof candidate.code === 'string' && candidate.code.trim().length > 0
    && typeof candidate.message === 'string' && candidate.message.trim().length > 0
    && typeof candidate.request_id === 'string' && candidate.request_id.trim().length > 0
}

async function refreshAccessToken(): Promise<boolean> {
  const csrf = cookie('anytty_cloud_csrf')
  if (!csrf) return false
  if (!refreshInFlight) {
    refreshInFlight = fetch('/api/account/refresh', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-AnyTTY-CSRF': csrf },
      body: '{}',
    }).then((response) => response.ok).catch(() => false).finally(() => { refreshInFlight = undefined })
  }
  return refreshInFlight
}

async function fetchWithAccessToken(path: string, init?: RequestInit): Promise<Response> {
  let response = await fetch(path, init)
  const publicAccountPath = path === '/api/account/login' || path === '/api/account/refresh' || path === '/api/account/setup/redeem'
  if (response.status === 401 && !publicAccountPath && await refreshAccessToken()) {
    response = await fetch(path, init)
  }
  return response
}

async function decode<Schema extends DescMessage>(response: Response, schema: Schema): Promise<MessageShape<Schema>> {
  const body = await response.text()
  if (!response.ok) {
    let error: ErrorEnvelope | undefined
    try {
      const parsed: unknown = body ? JSON.parse(body) : undefined
      if (isErrorEnvelope(parsed)) error = parsed
    } catch {}
    const correlationID = error?.request_id ?? response.headers.get('X-Request-ID') ?? ''
    const message = error?.message ?? '请求失败，请稍后重试。'
    const code = error?.code ?? 'http_error'
    throw new APIError(response.status, message, correlationID, code)
  }
  const json = body ? JSON.parse(body) as JsonValue : {}
  return fromJson(schema, json)
}

export async function protoGet<Schema extends DescMessage>(path: string, schema: Schema): Promise<MessageShape<Schema>> {
  return decode(await fetchWithAccessToken(path, { credentials: 'same-origin', cache: 'no-store' }), schema)
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
  const response = await fetchWithAccessToken(path, {
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
