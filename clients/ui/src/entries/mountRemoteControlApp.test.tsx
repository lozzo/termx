import { act, cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mountRemoteControlApp } from './mountRemoteControlApp'
import mountSource from './mountRemoteControlApp.tsx?raw'

describe('mountRemoteControlApp', () => {
  afterEach(() => {
    vi.useRealTimers()
    cleanup()
    document.body.innerHTML = ''
  })

  it('uses terminal theme semantics for the unavailable state in dark and light themes', async () => {
    document.body.innerHTML = '<div id="root"></div>'
    let root = mountRemoteControlApp({ themeId: 'anytty-dark' })

    let unavailable = await screen.findByTestId('anytty-cloud-unavailable')
    expect(unavailable.style.getPropertyValue('--anytty-bg')).toBe('#030712')
    expect(unavailable.style.getPropertyValue('--anytty-text')).toBe('#f4f4f5')
    expect(screen.getByRole('heading', { name: 'AnyTTY Cloud 暂不可用' }).className).toContain('text-[var(--anytty-text)]')
    expect(screen.getByText('云端服务正在重构。Direct 和 SSH 客户端不受影响。').className).toContain('text-[var(--anytty-muted)]')
    root.unmount()

    document.body.innerHTML = '<div id="light-root"></div>'
    root = mountRemoteControlApp({ root: document.getElementById('light-root'), themeId: 'github-light' })
    unavailable = await screen.findByTestId('anytty-cloud-unavailable')
    expect(unavailable.style.getPropertyValue('--anytty-bg')).toBe('#f6f8fa')
    expect(unavailable.style.getPropertyValue('--anytty-text')).toBe('#24292f')
    expect(screen.getByText('云端服务正在重构。Direct 和 SSH 客户端不受影响。')).toBeTruthy()
    root.unmount()
  })

  it('restarts only the existing mount render while exposing a stable pending state', async () => {
    document.body.innerHTML = '<div id="root"></div>'
    const root = mountRemoteControlApp({ themeId: 'anytty-dark' })
    const unavailable = await screen.findByTestId('anytty-cloud-unavailable')
    const retry = screen.getByRole('button', { name: '重试 AnyTTY Cloud 挂载' })

    retry.focus()
    expect(document.activeElement).toBe(retry)
    vi.useFakeTimers()
    fireEvent.click(retry)
    fireEvent.click(retry)

    expect((retry as HTMLButtonElement).disabled).toBe(true)
    expect(retry.getAttribute('aria-label')).toBe('正在重试 AnyTTY Cloud 挂载')
    expect(retry.getAttribute('aria-busy')).toBe('true')
    await act(async () => { vi.runAllTimers() })
    expect(screen.getByTestId('anytty-cloud-unavailable')).not.toBe(unavailable)
    expect((screen.getByRole('button', { name: '重试 AnyTTY Cloud 挂载' }) as HTMLButtonElement).disabled).toBe(false)
    expect(mountSource).not.toMatch(/location\.reload/)
    root.unmount()
  })

  it('supports keyboard retry with an accessible name', async () => {
    document.body.innerHTML = '<div id="root"></div>'
    const root = mountRemoteControlApp({ themeId: 'anytty-dark' })
    const unavailable = await screen.findByTestId('anytty-cloud-unavailable')
    const user = userEvent.setup()

    await user.tab()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '重试 AnyTTY Cloud 挂载' }))
    await user.keyboard('{Enter}')
    await waitFor(() => expect(screen.getByTestId('anytty-cloud-unavailable')).not.toBe(unavailable))
    root.unmount()
  })
})
