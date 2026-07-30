// @vitest-environment jsdom

import { Children, type ReactElement, type ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { ErrorState, IconButton, Skeleton } from './ui'

function render(element: ReactNode): Document {
  return new DOMParser().parseFromString(renderToStaticMarkup(element), 'text/html')
}

function errorAction(error: unknown, onRetry: () => void) {
  const state = ErrorState({ error, onRetry }) as ReactElement<{ children: ReactNode }>
  return Children.toArray(state.props.children)[1] as ReactElement<{ onClick: () => void }>
}

describe('Cloud UI semantics', () => {
  it('keeps the IconButton base class when callers add a class', () => {
    const document = render(<IconButton label="打开导航" className="menu-button" />)
    const button = document.querySelector('button')

    expect(button?.classList.contains('icon-button')).toBe(true)
    expect(button?.classList.contains('menu-button')).toBe(true)
  })

  it('announces Skeleton loading state while hiding its decoration', () => {
    const document = render(<Skeleton rows={3} />)
    const status = document.querySelector('[role="status"]')

    expect(status?.getAttribute('aria-live')).toBe('polite')
    expect(status?.getAttribute('aria-busy')).toBe('true')
    expect(status?.textContent).toContain('正在加载')
    expect(status?.querySelector('[aria-hidden="true"]')?.querySelectorAll('i')).toHaveLength(3)
  })

  it('runs an explicit retry exactly once without exposing internal error details', () => {
    const retry = vi.fn()
    const internal = Object.assign(new Error('database connection string'), { status: 503, correlationID: 'corr-public-123' })
    const document = render(<ErrorState error={internal} onRetry={retry} />)

    expect(document.body.textContent).toContain('服务暂时不可用，请稍后重试。')
    expect(document.body.textContent).toContain('corr-public-123')
    expect(document.body.textContent).not.toContain('database connection string')
    expect(document.querySelector('button')?.textContent).toBe('重试')
    errorAction(internal, retry).props.onClick()
    expect(retry).toHaveBeenCalledTimes(1)
  })

  it('only exposes the caller-provided recovery action', () => {
    const retry = vi.fn()
    const internal = Object.assign(new Error('private request detail'), { status: 500, correlationID: 'request-public-456' })
    const document = render(<ErrorState error={internal} onRetry={retry} />)

    expect(document.body.textContent).toContain('服务暂时不可用，请稍后重试。')
    expect(document.body.textContent).toContain('request-public-456')
    expect(document.body.textContent).not.toContain('private request detail')
    expect(document.body.textContent).not.toContain('重新加载')
    expect(document.querySelector('button')?.textContent).toBe('重试')
    errorAction(internal, retry).props.onClick()
    expect(retry).toHaveBeenCalledTimes(1)
  })
})
