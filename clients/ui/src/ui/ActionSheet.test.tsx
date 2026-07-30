import { useState } from 'react'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ActionSheet } from './ActionSheet'

describe('ActionSheet', () => {
  afterEach(cleanup)

  it('exposes a named modal, traps focus, closes on Escape, and returns focus', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<Harness onClose={onClose} />)

    const dialog = screen.getByRole('dialog', { name: 'File actions' })
    const trigger = screen.getByText('Open actions')
    const firstAction = screen.getByRole('button', { name: 'Rename' })
    const close = screen.getByRole('button', { name: 'Close' })
    const lastAction = screen.getByRole('button', { name: 'Delete' })

    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(document.activeElement).toBe(firstAction)
    expect(trigger.hasAttribute('inert')).toBe(true)

    close.focus()
    await user.tab({ shift: true })
    expect(document.activeElement).toBe(lastAction)
    await user.tab()
    expect(document.activeElement).toBe(close)
    await user.keyboard('{Escape}')

    expect(onClose).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('dialog', { name: 'File actions' })).toBeNull()
    expect(trigger.hasAttribute('inert')).toBe(false)
    expect(document.activeElement).toBe(trigger)
  })
})

function Harness({ onClose }: { onClose: () => void }) {
  const [open, setOpen] = useState(true)
  return (
    <div>
      <button autoFocus type="button">Open actions</button>
      <ActionSheet
        isOpen={open}
        onClose={() => {
          onClose()
          setOpen(false)
        }}
        title="File actions"
        actions={[
          { label: 'Rename', icon: <span aria-hidden="true">R</span>, onClick: () => undefined },
          { label: 'Delete', icon: <span aria-hidden="true">D</span>, onClick: () => undefined, danger: true },
        ]}
      />
    </div>
  )
}
