// @vitest-environment jsdom

import { act, Suspense, type ComponentType } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRetryableLazyRoute, RouteResourceBoundary } from './App'

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
    const Page = createRetryableLazyRoute(loader)
    await act(async () => {
      root.render(<RouteResourceBoundary>
        <Suspense fallback={<div role="status">正在加载测试路由</div>}><Page /></Suspense>
      </RouteResourceBoundary>)
    })
  }

  it('keeps a failed route nonblank and succeeds with a fresh lazy attempt on retry', async () => {
    const first = deferred<RouteModule>()
    const second = deferred<RouteModule>()
    const loader = vi.fn()
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise)

    await renderRoute(loader)
    expect(loader).toHaveBeenCalledTimes(1)
    await act(async () => {
      first.reject(new Error('chunk fetch failed'))
      await first.promise.catch(() => undefined)
    })

    expect(container.firstElementChild).not.toBeNull()
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('页面资源加载失败')
    expect(document.activeElement).toBe(container.querySelector('h1'))

    await act(async () => { (container.querySelector('button') as HTMLButtonElement).click() })
    expect(loader).toHaveBeenCalledTimes(2)
    expect(container.querySelector('[role="status"]')?.textContent).toContain('正在加载测试路由')

    await act(async () => {
      second.resolve({ default: LoadedRoute })
      await second.promise
    })
    expect(container.textContent).toContain('路由加载成功')
    expect(container.querySelector('[role="alert"]')).toBeNull()
  })

  it('returns focus to the error heading when a fresh retry also rejects', async () => {
    const first = deferred<RouteModule>()
    const second = deferred<RouteModule>()
    const loader = vi.fn()
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise)

    await renderRoute(loader)
    await act(async () => {
      first.reject(new Error('first module failure'))
      await first.promise.catch(() => undefined)
    })
    await act(async () => { (container.querySelector('button') as HTMLButtonElement).click() })
    await act(async () => {
      second.reject(new Error('second module failure'))
      await second.promise.catch(() => undefined)
    })

    expect(loader).toHaveBeenCalledTimes(2)
    expect(document.activeElement).toBe(container.querySelector('h1'))
    expect(document.activeElement).not.toBe(document.body)
    expect(container.querySelector('button')?.textContent).toBe('重新加载页面资源')
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
})
