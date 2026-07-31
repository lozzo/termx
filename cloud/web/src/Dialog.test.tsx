// @vitest-environment jsdom

import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Dialog } from './ui'

function render(element: ReactNode) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  act(() => root.render(element))
  return { container, root }
}

describe('Cloud Dialog close contract', () => {
  let roots: Root[] = []

  beforeEach(() => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    for (const root of roots) act(() => root.unmount())
    roots = []
    document.body.replaceChildren()
    vi.restoreAllMocks()
  })

  it('stays closable by default', () => {
    const onClose = vi.fn()
    const rendered = render(<Dialog title="默认可关闭" open onClose={onClose}>内容</Dialog>)
    roots.push(rendered.root)
    const close = rendered.container.querySelector<HTMLButtonElement>('button[aria-label="关闭"]')

    expect(close?.disabled).toBe(false)
    act(() => close?.click())
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('disables close and ignores the backdrop and Escape while locked', () => {
    const onClose = vi.fn()
    const rendered = render(<Dialog title="处理中" open onClose={onClose} closable={false}>内容</Dialog>)
    roots.push(rendered.root)
    const close = rendered.container.querySelector<HTMLButtonElement>('button[aria-label="关闭"]')
    const backdrop = rendered.container.querySelector<HTMLElement>('.dialog-backdrop')
    const dialog = rendered.container.querySelector<HTMLElement>('[role="dialog"]')

    expect(close?.disabled).toBe(true)
    act(() => close?.click())
    act(() => { backdrop?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })) })
    act(() => { dialog?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })) })
    expect(onClose).not.toHaveBeenCalled()
  })
})
