import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { mountRemoteControlApp } from './mountRemoteControlApp'

describe('mountRemoteControlApp', () => {
  afterEach(() => {
    cleanup()
    document.body.innerHTML = ''
  })

  it('shows the explicit Cloud rebuild state without starting a retired runtime', async () => {
    document.body.innerHTML = '<div id="root"></div>'
    const root = mountRemoteControlApp()

    expect((await screen.findByTestId('anytty-cloud-unavailable')).textContent).toContain('AnyTTY Cloud 暂不可用')
    expect(screen.getByText('云端服务正在重构。Direct 和 SSH 客户端不受影响。')).toBeTruthy()
    root.unmount()
  })
})
