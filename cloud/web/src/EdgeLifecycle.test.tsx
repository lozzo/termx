// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { EdgeDetailPage } from './pages/EdgesPage'

function reply(body: object, status = 200): Response {
  return { status, ok: status >= 200 && status < 300, headers: new Headers(), text: async () => JSON.stringify(body) } as Response
}

function render(element: ReactNode) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  act(() => root.render(element))
  return { container, root }
}

function withClient(element: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return <QueryClientProvider client={client}>{element}</QueryClientProvider>
}

function clickButton(container: HTMLElement, text: string, last = false) {
  const matches = Array.from(container.querySelectorAll('button')).filter((button) => button.textContent?.trim() === text)
  const button = last ? matches.at(-1) : matches[0]
  if (!button) throw new Error(`button not found: ${text}`)
  button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
}

function input(container: HTMLElement, labelText: string): HTMLInputElement {
  const label = Array.from(container.querySelectorAll('label')).find((value) => value.textContent === labelText)
  const control = label?.htmlFor ? document.getElementById(label.htmlFor) as HTMLInputElement | null : null
  if (!control) throw new Error(`input not found: ${labelText}`)
  return control
}

function setInput(control: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  setter?.call(control, value)
  control.dispatchEvent(new Event('input', { bubbles: true }))
}

async function waitForText(container: HTMLElement, text: string) {
  await act(async () => {
    await vi.waitFor(() => expect(container.textContent).toContain(text))
  })
}

function edgeResponse(enabled: boolean, online: boolean) {
  return {
    edges: [{
      config: { edge_id: '11b896bc-11f9-4935-8462-c59012429818', version: '4', name: 'CN1', region: 'cn-north-1', capacity: '1000', public_endpoint: 'cn1.example.com:443', enabled },
      config_revision: '4',
      runtime: { online, software_version: online ? 'current' : '', agent_count: '0', session_count: '0' },
    }],
  }
}

describe('Edge lifecycle UI', () => {
  let roots: Root[] = []

  beforeEach(() => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    for (const root of roots) act(() => root.unmount())
    roots = []
    document.body.replaceChildren()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('stops an enabled Edge without changing its configuration', async () => {
    const fetchMock = vi.fn(async (_path: RequestInfo | URL, init?: RequestInit) => {
      if (!init?.method) return reply(edgeResponse(true, true))
      return reply({ edge: edgeResponse(false, false).edges[0] })
    })
    vi.stubGlobal('fetch', fetchMock)
    const rendered = render(withClient(<MemoryRouter initialEntries={['/app/admin/edges/11b896bc-11f9-4935-8462-c59012429818/settings']}><Routes><Route path="/app/admin/edges/:edgeId/:tab" element={<EdgeDetailPage />} /></Routes></MemoryRouter>))
    roots.push(rendered.root)
    await waitForText(rendered.container, '停用 Edge')
    act(() => clickButton(rendered.container, '停用 Edge'))
    expect(rendered.container.textContent).toContain('立即使现有控制连接失效')
    await act(async () => clickButton(rendered.container, '确认停用'))
    const request = fetchMock.mock.calls.find((call) => call[1]?.method === 'PUT')?.[1]
    const body = JSON.parse(String(request?.body)) as { enabled?: boolean }
    expect(body.enabled ?? false).toBe(false)
    expect(request?.body).toContain('"public_endpoint":"cn1.example.com:443"')
  })

  it('permanently deletes an Edge only after it is disabled and offline', async () => {
    const fetchMock = vi.fn(async (_path: RequestInfo | URL, init?: RequestInit) => {
      if (!init?.method) return reply(edgeResponse(false, false))
      return reply({})
    })
    vi.stubGlobal('fetch', fetchMock)
    const rendered = render(withClient(<MemoryRouter initialEntries={['/app/admin/edges/11b896bc-11f9-4935-8462-c59012429818/settings']}><Routes><Route path="/app/admin/edges/:edgeId/:tab" element={<EdgeDetailPage />} /><Route path="/app/admin/edges" element={<h1>Edge 列表</h1>} /></Routes></MemoryRouter>))
    roots.push(rendered.root)
    await waitForText(rendered.container, '删除 Edge')
    act(() => clickButton(rendered.container, '删除 Edge'))
    expect(rendered.container.textContent).toContain('删除不可恢复')
    const confirm = Array.from(rendered.container.querySelectorAll('button')).find((button) => button.textContent?.trim() === '确认永久删除')
    expect(confirm?.disabled).toBe(true)
    act(() => setInput(input(rendered.container, '操作原因'), 'obsolete deployment'))
    await act(async () => clickButton(rendered.container, '确认永久删除'))
    await waitForText(rendered.container, 'Edge 列表')
    const request = fetchMock.mock.calls.find((call) => call[1]?.method === 'DELETE')
    expect(request?.[0]).toBe('/api/operator/edges/11b896bc-11f9-4935-8462-c59012429818')
    expect(request?.[1]?.body).toContain('"expected_revision":"4"')
    expect(request?.[1]?.body).toContain('"reason":"obsolete deployment"')
  })

  it('creates a one-time identity recovery credential only for an offline Edge', async () => {
    const fetchMock = vi.fn(async (_path: RequestInfo | URL, init?: RequestInit) => {
      if (!init?.method) return reply(edgeResponse(true, false))
      return reply({ recovery_token: 'one-time-edge-recovery', expires_at: '2026-08-02T12:10:00Z' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const rendered = render(withClient(<MemoryRouter initialEntries={['/app/admin/edges/11b896bc-11f9-4935-8462-c59012429818/settings']}><Routes><Route path="/app/admin/edges/:edgeId/:tab" element={<EdgeDetailPage />} /></Routes></MemoryRouter>))
    roots.push(rendered.root)
    await waitForText(rendered.container, '恢复身份证书')
    act(() => clickButton(rendered.container, '恢复身份证书'))
    act(() => setInput(input(rendered.container, '操作原因'), 'expired mTLS identity'))
    await act(async () => clickButton(rendered.container, '生成恢复凭据'))
    await waitForText(rendered.container, 'identity_recovery_token: one-time-edge-recovery')
    const request = fetchMock.mock.calls.find((call) => call[1]?.method === 'POST')
    expect(request?.[0]).toBe('/api/operator/edges/11b896bc-11f9-4935-8462-c59012429818/identity-recovery')
    expect(request?.[1]?.body).toContain('"reason":"expired mTLS identity"')
  })
})
