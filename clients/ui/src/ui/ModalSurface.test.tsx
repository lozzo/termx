import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ModalSurface } from './ModalSurface'

describe('ModalSurface', () => {
  afterEach(cleanup)

  it('traps focus, closes on Escape, isolates the background, and restores focus', async () => {
    const onClose = vi.fn()
    const { rerender } = render(<Harness open onClose={onClose} />)
    const dialog = screen.getByRole('dialog', { name: 'Test dialog' })
    const background = screen.getByText('Background action')

    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'First action' }))
    expect(background.hasAttribute('inert')).toBe(true)

    await userEvent.tab({ shift: true })
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Last action' }))
    await userEvent.tab()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'First action' }))
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)

    rerender(<Harness open={false} onClose={onClose} />)
    expect(background.hasAttribute('inert')).toBe(false)
    expect(document.activeElement).toBe(background)
  })
})

function Harness({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <div>
      <button autoFocus type="button">Background action</button>
      {open ? (
        <ModalSurface aria-label="Test dialog" onRequestClose={onClose}>
          <button type="button">First action</button>
          <button type="button">Last action</button>
        </ModalSurface>
      ) : null}
    </div>
  )
}
