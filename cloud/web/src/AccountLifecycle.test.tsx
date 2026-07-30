// @vitest-environment jsdom

import { create } from '@bufbuild/protobuf'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ProvisionAccountResponseSchema } from './generated/cloud/v1/operator_pb'
import { AccountsPage, AccountDetailPage, SetupCredentialResult } from './pages/AccountsPage'
import { SetupPage } from './pages/SetupPage'

type MockReply = { status: number; body: string }

function reply(body: object, status = 200): MockReply & { ok: boolean; headers: Headers; text: () => Promise<string> } {
	return { status, ok: status >= 200 && status < 300, headers: new Headers(), body: JSON.stringify(body), text: async () => JSON.stringify(body) }
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
	if (!label?.htmlFor) throw new Error(`label not found: ${labelText}`)
	const control = document.getElementById(label.htmlFor) as HTMLInputElement | null
	if (!control) throw new Error(`input not found: ${labelText}`)
	return control
}

function setInput(control: HTMLInputElement, value: string) {
	const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
	setter?.call(control, value)
	control.dispatchEvent(new Event('input', { bubbles: true }))
}

function clickButton(container: HTMLElement, text: string, last = false) {
	const matches = Array.from(container.querySelectorAll('button')).filter((value) => value.textContent?.trim() === text)
	const button = last ? matches.at(-1) : matches[0]
	if (!button) throw new Error(`button not found: ${text}`)
	button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
}

async function waitForText(container: HTMLElement, text: string) {
	await vi.waitFor(() => expect(container.textContent).toContain(text))
}

describe('account lifecycle UI', () => {
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

	it('copies the one-time credential and makes its lifetime explicit', async () => {
		const writeText = vi.fn().mockResolvedValue(undefined)
		Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
		const value = create(ProvisionAccountResponseSchema, { setupCredential: 'one-time-credential', expiresAt: { seconds: 1_800_000_000n, nanos: 0 } })
		const rendered = render(<SetupCredentialResult value={value} />)
		roots.push(rendered.root)
		expect(rendered.container.textContent).toContain('仅展示一次')
		await act(async () => clickButton(rendered.container, '复制'))
		expect(writeText).toHaveBeenCalledWith('one-time-credential')
		expect(rendered.container.textContent).toContain('已复制')
	})

	it('provisions without a password and shows the returned credential once', async () => {
		const fetchMock = vi.fn(async (_path: RequestInfo | URL, init?: RequestInit) => {
			if (!init?.method) return reply({ accounts: [] }) as unknown as Response
			return reply({ account: { account_id: 'new-account', email: 'user@example.com', display_name: 'New User', state: 'ACCOUNT_STATE_PENDING', revision: '1' }, setup_credential: 'provision-once', expires_at: '2027-01-15T00:00:00Z' }, 200) as unknown as Response
		})
		vi.stubGlobal('fetch', fetchMock)
		const rendered = render(withClient(<MemoryRouter><AccountsPage /></MemoryRouter>))
		roots.push(rendered.root)
		await waitForText(rendered.container, '没有匹配的账号')
		act(() => clickButton(rendered.container, '创建账号'))
		act(() => {
			setInput(input(rendered.container, '邮箱'), 'user@example.com')
			setInput(input(rendered.container, '显示名称'), 'New User')
			setInput(input(rendered.container, '创建原因'), 'approved')
		})
		await act(async () => clickButton(rendered.container, '创建账号', true))
		await waitForText(rendered.container, 'provision-once')
		const request = fetchMock.mock.calls.find((call) => call[1]?.method === 'POST')?.[1]
		expect(request?.body).toContain('"reason":"approved"')
		expect(request?.body).not.toContain('password')
	})

	it('resets an account and replaces the destructive confirmation with the credential result', async () => {
		const fetchMock = vi.fn(async (path: RequestInfo | URL, init?: RequestInit) => {
			const value = String(path)
			if (!init?.method && value.includes('/api/operator/accounts/')) return reply({ account: { account: { account_id: 'target', email: 'target@example.com', display_name: 'Target', state: 'ACCOUNT_STATE_ACTIVE', revision: '3' }, roles: ['ACCOUNT_ROLE_USER'], daemon_count: '0' } }) as unknown as Response
			if (!init?.method) return reply({ orders: [] }) as unknown as Response
			return reply({ account: { account_id: 'target', email: 'target@example.com', display_name: 'Target', state: 'ACCOUNT_STATE_PENDING', revision: '4' }, setup_credential: 'reset-once', expires_at: '2027-01-15T00:00:00Z' }) as unknown as Response
		})
		vi.stubGlobal('fetch', fetchMock)
		const rendered = render(withClient(<MemoryRouter initialEntries={['/app/admin/accounts/target']}><Routes><Route path="/app/admin/accounts/:accountId" element={<AccountDetailPage />} /></Routes></MemoryRouter>))
		roots.push(rendered.root)
		await waitForText(rendered.container, '重置凭据')
		act(() => clickButton(rendered.container, '重置凭据'))
		expect(rendered.container.textContent).toContain('旧密码和全部登录会话立即失效')
		act(() => setInput(input(rendered.container, '操作原因'), 'lost credential'))
		await act(async () => clickButton(rendered.container, '重置凭据', true))
		await waitForText(rendered.container, 'reset-once')
	})

	it('keeps setup input recoverable after failure and allows a successful retry', async () => {
		let attempts = 0
		const fetchMock = vi.fn(async () => {
			attempts++
			if (attempts === 1) return reply({ code: 'setup_invalid', message: '一次性凭据无效或已过期。', request_id: 'request-1' }, 400) as unknown as Response
			return reply({ account: { account_id: 'target', state: 'ACCOUNT_STATE_ACTIVE', revision: '2' } }) as unknown as Response
		})
		vi.stubGlobal('fetch', fetchMock)
		const rendered = render(withClient(<MemoryRouter><SetupPage /></MemoryRouter>))
		roots.push(rendered.root)
		act(() => {
			setInput(input(rendered.container, '一次性凭据'), 'A'.repeat(43))
			setInput(input(rendered.container, '新密码'), 'new-password')
			setInput(input(rendered.container, '确认新密码'), 'new-password')
		})
		await act(async () => clickButton(rendered.container, '设置密码'))
		await waitForText(rendered.container, '请向管理员申请重置')
		expect(input(rendered.container, '一次性凭据').value).toBe('A'.repeat(43))
		await act(async () => clickButton(rendered.container, '设置密码'))
		await waitForText(rendered.container, '密码已设置')
		expect(attempts).toBe(2)
	})
})
