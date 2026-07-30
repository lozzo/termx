import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { mountRemoteControlApp } from './mountRemoteControlApp'

describe('mountRemoteControlApp', () => {
  afterEach(() => {
    cleanup()
    document.body.innerHTML = ''
  })

  it('uses terminal theme semantics for the unavailable state in dark and light themes', async () => {
    document.body.innerHTML = '<div id="root"></div>'
    let root = mountRemoteControlApp({ themeId: 'anytty-dark' })

    let unavailable = await screen.findByTestId('anytty-cloud-unavailable')
    expect(unavailable.style.getPropertyValue('--anytty-bg')).toBe('#030712')
    expect(unavailable.style.getPropertyValue('--anytty-text')).toBe('#f4f4f5')
    expect(screen.getByRole('alert', { name: 'AnyTTY Cloud 暂不可用' })).toBe(unavailable)
    expect(unavailable.getAttribute('aria-describedby')).toBe('anytty-cloud-unavailable-description')
    expect(screen.getByRole('heading', { name: 'AnyTTY Cloud 暂不可用' }).className).toContain('text-[var(--anytty-text)]')
    expect(screen.getByText('云端服务正在重构。Direct 和 SSH 客户端不受影响。').className).toContain('text-[var(--anytty-muted)]')
    expect(screen.queryByRole('button')).toBeNull()
    root.unmount()

    document.body.innerHTML = '<div id="light-root"></div>'
    root = mountRemoteControlApp({ root: document.getElementById('light-root'), themeId: 'github-light' })
    unavailable = await screen.findByTestId('anytty-cloud-unavailable')
    expect(unavailable.style.getPropertyValue('--anytty-bg')).toBe('#f6f8fa')
    expect(unavailable.style.getPropertyValue('--anytty-text')).toBe('#24292f')
    expect(screen.getByText('云端服务正在重构。Direct 和 SSH 客户端不受影响。')).toBeTruthy()
    root.unmount()
  })
})
