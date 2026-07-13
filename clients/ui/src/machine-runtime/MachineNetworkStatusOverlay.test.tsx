import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { MachineNetworkStatusOverlay } from './MachineNetworkStatusOverlay'

describe('MachineNetworkStatusOverlay', () => {
  it('uses theme variables so light and dark terminal themes stay readable', () => {
    render(<MachineNetworkStatusOverlay phase="waiting_network" status="Waiting for network..." />)

    const overlay = screen.getByTestId('termx-machine-network-overlay')
    expect(overlay.className).toContain('bg-[var(--termx-overlay)]')

    const card = overlay.firstElementChild
    if (!(card instanceof HTMLDivElement)) throw new Error('overlay card was not rendered')
    expect(card.className).toContain('border-[var(--termx-border)]')
    expect(card.className).toContain('bg-[var(--termx-surface)]')
    expect(card.className).toContain('text-[var(--termx-text)]')
    expect(card.className).not.toMatch(/rounded|shadow/)
    expect(card.textContent).toContain('Network status')
    expect(card.textContent).toContain('Waiting for network...')
  })
})
