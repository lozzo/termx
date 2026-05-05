import { afterEach, describe, expect, it, vi } from 'vitest'
import { createBrowserRemoteNetworkRuntime } from './browserNetworkRuntime'
import { createLocalAgentApi } from './localAgentApi'

describe('createLocalAgentApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('normalizes local status without exposing removed localweb signaling endpoints', async () => {
    const fetch = vi.fn(async (input: RequestInfo | URL) => responseFor(String(input), {
      '/api/local/status': {
        machine_id: 'machine-local',
        machine_name: 'Local Mac',
        remote_enabled: true,
        local_rtc: {
          http_url: 'http://127.0.0.1:18888',
          ice_tcp_enabled: true,
          ice_tcp_port: 18889,
        },
        updated_at: '2026-05-01T04:00:00Z',
      },
    }))

    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888', fetch })
    await expect(api.getStatus()).resolves.toMatchObject({
      machine: {
        machineId: 'machine-local',
        name: 'Local Mac',
        state: 'online',
        localRTC: {
          signalingUrl: 'http://127.0.0.1:18888',
          iceTCPUrl: 'tcp://127.0.0.1:18889',
        },
      },
      localWeb: {
        httpUrl: 'http://127.0.0.1:18888',
        rtcOfferUrl: 'http://127.0.0.1:18888',
      },
    })
    expect(fetch).toHaveBeenCalledWith('http://127.0.0.1:18888/api/local/status', expect.any(Object))
    expect(JSON.stringify(fetch.mock.calls)).not.toMatch(/\/api\/local\/rtc\/offer|\/api\/local\/pair|\/api\/local\/terminals/)
  })

  it('uses the browser runtime fetch binding when injected from the browser adapter', async () => {
    const calls: Array<{ thisValue: unknown; input: string }> = []
    const boundFetch = function (this: unknown, input: RequestInfo | URL) {
      calls.push({ thisValue: this, input: String(input) })
      if (this !== globalThis) {
        throw new TypeError("Failed to execute 'fetch' on 'Window': Illegal invocation")
      }
      return Promise.resolve(jsonResponse({
        machine_id: 'machine-local',
        machine_name: 'Local Mac',
        remote_enabled: true,
        local_rtc: {
          http_url: 'http://127.0.0.1:18888',
        },
      }))
    }
    vi.stubGlobal('fetch', boundFetch)
    const runtime = createBrowserRemoteNetworkRuntime()
    const api = createLocalAgentApi({ baseUrl: 'http://127.0.0.1:18888', fetch: runtime.fetch })

    await expect(api.getStatus()).resolves.toMatchObject({
      machine: { machineId: 'machine-local' },
    })
    expect(calls).toEqual([{
      thisValue: globalThis,
      input: 'http://127.0.0.1:18888/api/local/status',
    }])
  })
})

function responseFor(url: string, routes: Record<string, unknown>): Response {
  const path = new URL(url).pathname
  const body = routes[path]
  if (body === undefined) return jsonResponse({ error: { message: 'not found' } }, 404)
  return jsonResponse(body)
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}
