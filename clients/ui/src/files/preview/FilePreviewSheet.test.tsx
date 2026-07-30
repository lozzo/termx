import { useState } from 'react'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { FilePreviewSheet } from './FilePreviewSheet'

describe('FilePreviewSheet', () => {
  afterEach(cleanup)

  it('exposes a named modal and restores the preview trigger after Escape', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<Harness onClose={onClose} />)

    const dialog = screen.getByRole('dialog', { name: 'notes.txt' })
    const close = screen.getByRole('button', { name: 'Close preview' })
    const trigger = screen.getByText('Preview notes')

    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(document.activeElement).toBe(close)
    expect(trigger.hasAttribute('inert')).toBe(true)

    await user.keyboard('{Escape}')

    expect(onClose).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('dialog', { name: 'notes.txt' })).toBeNull()
    expect(trigger.hasAttribute('inert')).toBe(false)
    expect(document.activeElement).toBe(trigger)
  })
})

function Harness({ onClose }: { onClose: () => void }) {
  const [open, setOpen] = useState(true)
  return (
    <div>
      <button autoFocus type="button">Preview notes</button>
      {open ? (
        <FilePreviewSheet
          path="/docs/notes.txt"
          preview={null}
          loading
          error={null}
          streamPreview={vi.fn()}
          onClose={() => {
            onClose()
            setOpen(false)
          }}
        />
      ) : null}
    </div>
  )
}
