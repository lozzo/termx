// @vitest-environment jsdom

import { act, Component, Suspense, type ComponentType, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createLazyRoute, RouteResourceBoundary } from './App'

type RouteModule = { default: ComponentType }
type Deferred<T> = {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (reason: unknown) => void
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function LoadedRoute() {
  return <h2>路由加载成功</h2>
}

class RenderErrorObserver extends Component<{ children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  render() {
    if (this.state.error) return <p data-testid="render-error">{this.state.error.message}</p>
    return this.props.children
  }
}

describe('Cloud lazy route resources', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
  })

  afterEach(async () => {
    await act(async () => { root.unmount() })
    container.remove()
    vi.restoreAllMocks()
  })

  async function renderRoute(loader: () => Promise<RouteModule>) {
    const Page = createLazyRoute(loader)
    await act(async () => {
      root.render(<RouteResourceBoundary>
        <Suspense fallback={<div role="status">正在加载测试路由</div>}><Page /></Suspense>
      </RouteResourceBoundary>)
    })
  }

  it('keeps a rejected route nonblank with an explicit page-resource reload', async () => {
    const attempt = deferred<RouteModule>()
    const loader = vi.fn(() => attempt.promise)

    await renderRoute(loader)
    expect(loader).toHaveBeenCalledTimes(1)
    await act(async () => {
      attempt.reject(new Error('chunk fetch failed'))
      await attempt.promise.catch(() => undefined)
    })

    expect(container.firstElementChild).not.toBeNull()
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('页面资源加载失败')
    expect(document.activeElement).toBe(container.querySelector('h1'))
    expect(container.querySelector('button')?.textContent).toBe('重新加载页面资源')
    expect(loader).toHaveBeenCalledTimes(1)
  })

  it('shows the normal Suspense loading state until the route resolves', async () => {
    const attempt = deferred<RouteModule>()
    const loader = vi.fn(() => attempt.promise)

    await renderRoute(loader)
    expect(loader).toHaveBeenCalledTimes(1)
    expect(container.querySelector('[role="status"]')?.textContent).toContain('正在加载测试路由')

    await act(async () => {
      attempt.resolve({ default: LoadedRoute })
      await attempt.promise
    })
    expect(container.textContent).toContain('路由加载成功')
  })

  it('lets ordinary render errors escape to the owning application boundary', async () => {
    function BrokenRoute(): never {
      throw new Error('ordinary render failure')
    }

    await act(async () => {
      root.render(<RenderErrorObserver>
        <RouteResourceBoundary><BrokenRoute /></RouteResourceBoundary>
      </RenderErrorObserver>)
    })

    expect(container.querySelector('[data-testid="render-error"]')?.textContent).toBe('ordinary render failure')
    expect(container.querySelector('[role="alert"]')).toBeNull()
  })
})
