// @vitest-environment jsdom

import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RegistryStartupScreen, UnsupportedWebPreview } from './RegistryStartupScreen'

describe('mobile registry startup recovery', () => {
  afterEach(cleanup)

  it('offers native retry, diagnostics, and a confirmed local-only reset', async () => {
    const onRetry = vi.fn(async () => undefined)
    const onExportDiagnostics = vi.fn(async () => undefined)
    const onResetLocalPairings = vi.fn(async () => undefined)
    render(
      <RegistryStartupScreen
        diagnosticsAvailable
        error="native registry checksum failed"
        onExportDiagnostics={onExportDiagnostics}
        onResetLocalPairings={onResetLocalPairings}
        onRetry={onRetry}
      />,
    )

    expect(screen.getByRole('alert').textContent).toMatch(/native endpoint registry/i)
    expect(screen.getByText('native registry checksum failed')).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await userEvent.click(screen.getByRole('button', { name: 'Export diagnostics' }))
    await userEvent.click(screen.getByRole('button', { name: 'Reset local pairings' }))
    expect(onResetLocalPairings).not.toHaveBeenCalled()
    expect(screen.getByText(/removes paired endpoints and transfer history/i)).toBeTruthy()
    await userEvent.click(screen.getByRole('button', { name: 'Reset pairings' }))

    expect(onRetry).toHaveBeenCalledTimes(1)
    expect(onExportDiagnostics).toHaveBeenCalledTimes(1)
    expect(onResetLocalPairings).toHaveBeenCalledTimes(1)
  })

  it('presents unsupported web preview without native recovery controls', () => {
    render(<UnsupportedWebPreview />)

    expect(screen.getByTestId('unsupported-web-preview')).toBeTruthy()
    expect(screen.getByText(/browser preview cannot access native pairing storage/i)).toBeTruthy()
    expect(screen.queryByRole('button')).toBeNull()
  })
})
