import { describe, expect, it, vi } from 'vitest'
import { WasmBindingBackend } from './wasmBindingBackend'
import type { MuxviaWasmRuntime } from './wasmRuntime'

describe('WasmBindingBackend', () => {
  it('closes the runtime exactly once after the event pump fails', async () => {
    const runtime = {
      nextEvent: vi.fn(async () => { throw new Error('event pump failed') }),
      close: vi.fn(async () => {}),
    } as unknown as MuxviaWasmRuntime
    const backend = new WasmBindingBackend(runtime)
    const closed = new Promise<Error>((resolve) => backend.start(() => {}, resolve))
    expect((await closed).message).toBe('event pump failed')
    await backend.close()
    await backend.close()
    expect(runtime.close).toHaveBeenCalledTimes(1)
  })
})
