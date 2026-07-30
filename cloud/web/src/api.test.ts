import { afterEach, describe, expect, it, vi } from 'vitest'
import { GetCurrentAccountResponseSchema } from './generated/cloud/v1/account_pb'
import { protoGet } from './api'

afterEach(() => vi.unstubAllGlobals())

describe('账号 session 轮换', () => {
  it('并发 401 只执行一次 refresh 并重试原请求', async () => {
    vi.stubGlobal('document', { cookie: 'anytty_cloud_csrf=csrf-proof' })
    let resourceCalls = 0
    let refreshCalls = 0
    let refreshHeader = ''
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const path = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
      if (path === '/api/account/refresh') {
        refreshCalls++
        refreshHeader = new Headers(init?.headers).get('X-AnyTTY-CSRF') ?? ''
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
  })

  it('preserves a public correlation ID on API failures', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'internal storage detail', correlation_id: 'corr-public-123' }), {
      status: 503,
      headers: { 'Content-Type': 'application/json' },
    })))

    await expect(protoGet('/api/account/current', GetCurrentAccountResponseSchema)).rejects.toMatchObject({
      status: 503,
      correlationID: 'corr-public-123',
    })
  })
})
