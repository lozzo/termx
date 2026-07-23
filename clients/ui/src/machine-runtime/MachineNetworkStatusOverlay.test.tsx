import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { muxviaI18n } from '../i18n'
import { MachineNetworkStatusOverlay } from './MachineNetworkStatusOverlay'

describe('MachineNetworkStatusOverlay', () => {
  afterEach(async () => {
    cleanup()
    await muxviaI18n.changeLanguage('en')
  })

  it('uses theme variables so light and dark terminal themes stay readable', () => {
    render(<MachineNetworkStatusOverlay phase="waiting_network" status="Waiting for network..." />)

    const overlay = screen.getByTestId('muxvia-machine-network-overlay')
    expect(overlay.className).toContain('bg-[var(--muxvia-overlay)]')

    const card = overlay.firstElementChild
    if (!(card instanceof HTMLDivElement)) throw new Error('overlay card was not rendered')
    expect(card.className).toContain('border-[var(--muxvia-border)]')
    expect(card.className).toContain('bg-[var(--muxvia-surface)]')
    expect(card.className).toContain('text-[var(--muxvia-text)]')
    expect(card.className).not.toMatch(/rounded|shadow/)
    expect(card.textContent).toContain('Connecting to device')
    expect(card.textContent).toContain('Waiting for network...')
  })

  it('uses the stable phase instead of exposing a native implementation message', async () => {
    await muxviaI18n.changeLanguage('zh-CN')
    render(<MachineNetworkStatusOverlay phase="signaling" status="JNI runtime handle 42" />)

    const overlay = screen.getByTestId('muxvia-machine-network-overlay')
    expect(overlay.textContent).toContain('正在连接设备')
    expect(overlay.textContent).toContain('正在进行 P2P 信令协商')
    expect(overlay.textContent).not.toMatch(/JNI|runtime|handle/i)
  })
})
