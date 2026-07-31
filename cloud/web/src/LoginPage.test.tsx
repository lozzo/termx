// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { LoginPage } from './pages/LoginPage'

type MockReply = { status: number; ok: boolean; headers: Headers; text: () => Promise<string> }

function reply(status: number): MockReply {
  const body = JSON.stringify({
    code: status === 401 ? 'invalid_credentials' : status === 429 ? 'rate_limited' : 'service_unavailable',
    message: 'private authentication detail',
    request_id: `login-${status}`,
  })
  return {
    status,
    ok: false,
    headers: new Headers(),
    text: async () => body,
  }
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

function input(container: HTMLElement, labelText: string): HTMLInputElement {
  const label = Array.from(container.querySelectorAll('label')).find((value) => value.textContent === labelText)
  const control = label?.htmlFor ? document.getElementById(label.htmlFor) : null
  if (!(control instanceof HTMLInputElement)) throw new Error(`input not found: ${labelText}`)
  return control
}

function setInput(control: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  setter?.call(control, value)
  control.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('LoginPage error recovery', () => {
  const roots: Root[] = []

  beforeEach(() => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    for (const root of roots) act(() => root.unmount())
    roots.length = 0
    document.body.replaceChildren()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it.each([
    [429, '登录尝试过于频繁。请稍后再试。'],
    [503, '登录服务暂时不可用。请稍后重试。'],
    [401, '账号或密码不正确。请检查后重新输入。'],
  ] as const)('maps HTTP %s to a clear, redacted recovery message', async (status, expected) => {
    vi.stubGlobal('fetch', vi.fn(async () => reply(status) as unknown as Response))
    const rendered = render(withClient(<MemoryRouter><LoginPage /></MemoryRouter>))
    roots.push(rendered.root)

    act(() => {
      setInput(input(rendered.container, '邮箱或账号'), 'user@example.com')
      setInput(input(rendered.container, '密码'), 'valid-password')
    })
    const submit = Array.from(rendered.container.querySelectorAll('button')).find((button) => button.textContent?.includes('登录'))
    if (!submit) throw new Error('login button not found')
    await act(async () => { submit.dispatchEvent(new MouseEvent('click', { bubbles: true })) })
    await act(async () => {
      await vi.waitFor(() => expect(rendered.container.querySelector('[role="alert"]')?.textContent).toBe(expected))
    })

    expect(rendered.container.textContent).not.toContain('private authentication detail')
    expect(rendered.container.textContent).not.toContain(`login-${status}`)
  })
})
