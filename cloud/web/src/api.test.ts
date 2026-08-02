import { afterEach, describe, expect, it, vi } from 'vitest'
import { GetCurrentAccountResponseSchema } from './generated/cloud/v1/account_pb'
import { protoGet } from './api'

afterEach(() => vi.unstubAllGlobals())

describe('账号 token 轮换', () => {
  it('并发 401 只执行一次 refresh 并重试原请求', async () => {
    vi.stubGlobal('document', { cookie: 'anytty_cloud_csrf=csrf-proof' })
    let resourceCalls = 0
    let refreshCalls = 0
    let refreshHeader = ''
    let refreshContentType = ''
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const path = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
      if (path === '/api/account/refresh') {
        refreshCalls++
        refreshHeader = new Headers(init?.headers).get('X-AnyTTY-CSRF') ?? ''
        refreshContentType = new Headers(init?.headers).get('Content-Type') ?? ''
        await Promise.resolve()
        return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
      }
      resourceCalls++
      return new Response('{}', { status: resourceCalls <= 2 ? 401 : 200, headers: { 'Content-Type': 'application/json' } })
    }))

    await Promise.all([
      protoGet('/api/account/current?request=1', GetCurrentAccountResponseSchema),
      protoGet('/api/account/current?request=2', GetCurrentAccountResponseSchema),
    ])

    expect(refreshCalls).toBe(1)
    expect(resourceCalls).toBe(4)
    expect(refreshHeader).toBe('csrf-proof')
    expect(refreshContentType).toBe('application/json')
  })

  it('preserves the stable code and request ID on API failures', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 'service_unavailable', message: '服务暂时不可用。', request_id: 'req-public-123' }), {
      status: 503,
      headers: { 'Content-Type': 'application/json' },
    })))

    await expect(protoGet('/api/account/current', GetCurrentAccountResponseSchema)).rejects.toMatchObject({
      status: 503,
      correlationID: 'req-public-123',
      code: 'service_unavailable',
      message: '服务暂时不可用。',
    })
  })

  it('preserves status and the response request ID for a non-JSON 503', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('<!doctype html><title>private proxy failure</title>', {
      status: 503,
      headers: { 'Content-Type': 'text/html', 'X-Request-ID': 'proxy-request-503' },
    })))

    await expect(protoGet('/api/account/current', GetCurrentAccountResponseSchema)).rejects.toMatchObject({
      status: 503,
      correlationID: 'proxy-request-503',
      code: 'http_error',
      message: '请求失败，请稍后重试。',
    })
  })

  it.each([
    ['message only', { message: 'private upstream detail' }],
    ['missing request ID', { code: 'service_unavailable', message: 'private upstream detail' }],
    ['wrong code type', { code: 503, message: 'private upstream detail', request_id: 'untrusted-body-id' }],
    ['wrong message type', { code: 'service_unavailable', message: { detail: 'private upstream detail' }, request_id: 'untrusted-body-id' }],
    ['wrong request ID type', { code: 'service_unavailable', message: 'private upstream detail', request_id: 503 }],
  ])('uses the redacted fallback for a malformed error envelope: %s', async (_name, body) => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), {
      status: 503,
      headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'trusted-header-id' },
    })))

    await expect(protoGet('/api/account/current', GetCurrentAccountResponseSchema)).rejects.toMatchObject({
      status: 503,
      correlationID: 'trusted-header-id',
      code: 'http_error',
      message: '请求失败，请稍后重试。',
    })
  })

  it('keeps successful Proto JSON responses strict', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('<!doctype html><title>not proto JSON</title>', {
      status: 200,
      headers: { 'Content-Type': 'text/html' },
    })))

    await expect(protoGet('/api/account/current', GetCurrentAccountResponseSchema)).rejects.toBeInstanceOf(SyntaxError)
  })

  it('does not refresh or replay a forbidden request', async () => {
    vi.stubGlobal('document', { cookie: 'anytty_cloud_csrf=csrf-proof' })
    const fetchMock = vi.fn(async (_input: string | URL | Request) => new Response(JSON.stringify({ code: 'forbidden', message: '没有权限。', request_id: 'req-forbidden' }), {
      status: 403,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(protoGet('/api/operator/accounts', GetCurrentAccountResponseSchema)).rejects.toMatchObject({ status: 403, code: 'forbidden' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/operator/accounts')
  })
})
