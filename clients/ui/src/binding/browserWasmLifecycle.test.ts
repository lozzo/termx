import { describe, expect, it } from 'vitest'
import { BrowserWasmLifecycle } from './browserWasmLifecycle'

describe('BrowserWasmLifecycle', () => {
  it('closes the stale generation before page resume publishes a fresh one', async () => {
    const order: string[] = []
    let next = 0
    const lifecycle = new BrowserWasmLifecycle(async () => {
      const id = ++next
      order.push(`create:${id}`)
      return { id, async close() { order.push(`close:${id}`) } }
    })
    lifecycle.attach()
    const first = await lifecycle.start()
    window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }))
    window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))
    await lifecycle.whenIdle()

    expect(first.id).toBe(1)
    expect(lifecycle.current?.id).toBe(2)
    expect(order).toEqual(['create:1', 'close:1', 'create:2'])
    await lifecycle.dispose()
  })
})
