import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { NATIVE_BACK_PRIORITY, addNativeBackHandler, dispatchNativeBack } from './nativeBack'
import { useNativeBackHandler } from './useNativeBackHandler'

const unregisterHandlers: Array<() => void> = []

afterEach(() => {
  cleanup()
  for (const unregister of unregisterHandlers.splice(0).reverse()) unregister()
})

describe('native back handler registry', () => {
  it('dispatches by priority and LIFO order within one priority', () => {
    const calls: string[] = []
    register(() => { calls.push('low'); return true }, NATIVE_BACK_PRIORITY.ROOT)
    register(() => { calls.push('high-old'); return true }, NATIVE_BACK_PRIORITY.TRANSFER)
    const unregisterNewest = register(() => { calls.push('high-new'); return true }, NATIVE_BACK_PRIORITY.TRANSFER)

    expect(dispatchNativeBack()).toBe(true)
    expect(calls).toEqual(['high-new'])

    unregisterNewest()
    expect(dispatchNativeBack()).toBe(true)
    expect(calls).toEqual(['high-new', 'high-old'])
  })

  it('never calls an unregistered handler, including one removed during dispatch', () => {
    const removed = vi.fn(() => true)
    const unregisterRemoved = register(removed, NATIVE_BACK_PRIORITY.ROOT)
    register(() => {
      unregisterRemoved()
      return false
    }, NATIVE_BACK_PRIORITY.TRANSFER)

    expect(dispatchNativeBack()).toBe(false)
    expect(removed).not.toHaveBeenCalled()
    expect(dispatchNativeBack()).toBe(false)
    expect(removed).not.toHaveBeenCalled()
  })

  it('stops after one handler consumes an event', () => {
    const lower = vi.fn(() => true)
    const top = vi.fn(() => true)
    register(lower, NATIVE_BACK_PRIORITY.WORKSPACE)
    register(top, NATIVE_BACK_PRIORITY.NESTED_OVERLAY)

    expect(dispatchNativeBack()).toBe(true)
    expect(top).toHaveBeenCalledOnce()
    expect(lower).not.toHaveBeenCalled()
  })

  it('orders nested overlay, scanner, transfer, workspace, then root', () => {
    const consumed: string[] = []
    const active = new Set(['nested', 'scanner', 'transfer', 'workspace', 'root'])
    const entries = [
      ['root', NATIVE_BACK_PRIORITY.ROOT],
      ['workspace', NATIVE_BACK_PRIORITY.WORKSPACE],
      ['transfer', NATIVE_BACK_PRIORITY.TRANSFER],
      ['scanner', NATIVE_BACK_PRIORITY.SCANNER],
      ['nested', NATIVE_BACK_PRIORITY.NESTED_OVERLAY],
    ] as const
    for (const [name, priority] of entries) {
      register(() => {
        if (!active.has(name)) return false
        active.delete(name)
        consumed.push(name)
        return true
      }, priority)
    }

    for (let index = 0; index < entries.length; index += 1) {
      expect(dispatchNativeBack()).toBe(true)
    }
    expect(consumed).toEqual(['nested', 'scanner', 'transfer', 'workspace', 'root'])
    expect(dispatchNativeBack()).toBe(false)
  })

  it('keeps one registration across rerenders and invokes the latest closure', () => {
    const calls: string[] = []
    const view = render(<NativeBackHarness value="first" onBack={(value) => calls.push(value)} />)
    view.rerender(<NativeBackHarness value="second" onBack={(value) => calls.push(value)} />)

    expect(dispatchNativeBack()).toBe(true)
    expect(calls).toEqual(['second'])

    view.unmount()
    expect(dispatchNativeBack()).toBe(false)
    expect(calls).toEqual(['second'])
  })
})

function NativeBackHarness({ value, onBack }: { value: string; onBack: (value: string) => void }) {
  useNativeBackHandler(() => {
    onBack(value)
    return true
  }, NATIVE_BACK_PRIORITY.WORKSPACE)
  return null
}

function register(handler: () => boolean, priority: number): () => void {
  const unregister = addNativeBackHandler(handler, priority)
  unregisterHandlers.push(unregister)
  return unregister
}
