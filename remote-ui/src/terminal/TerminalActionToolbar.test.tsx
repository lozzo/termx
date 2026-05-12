import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { TerminalActionToolbar } from './TerminalActionToolbar'

describe('TerminalActionToolbar', () => {
  afterEach(() => {
    cleanup()
  })

  it('keeps resize control clicks inside the toolbar', () => {
    const onAcquireResizeOwner = vi.fn()
    const onOuterPointerDown = vi.fn()
    const onOuterClick = vi.fn()

    render(
      <div onPointerDown={onOuterPointerDown} onClick={onOuterClick}>
        <TerminalActionToolbar
          mode="default"
          hasSelection={false}
          resizeControl={{ canResize: false, reason: 'follower' }}
          onModeChange={vi.fn()}
          onSelectAll={vi.fn()}
          onSelectVisible={vi.fn()}
          onCopy={vi.fn()}
          onPaste={vi.fn()}
          onOpenSnippets={vi.fn()}
          onAcquireResizeOwner={onAcquireResizeOwner}
        />
      </div>,
    )

    const resizeButton = screen.getByRole('button', { name: /acquire resize control/i })
    fireEvent.pointerDown(resizeButton)
    fireEvent.click(resizeButton)

    expect(onAcquireResizeOwner).toHaveBeenCalledTimes(1)
    expect(onOuterPointerDown).not.toHaveBeenCalled()
    expect(onOuterClick).not.toHaveBeenCalled()
  })
})
