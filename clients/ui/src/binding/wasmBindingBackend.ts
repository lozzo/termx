import { BindingOperation, type BindingOperationCode, type ProtoBindingBackend } from './protoBindingClient'
import type { TermxWasmRuntime } from './wasmRuntime'

/** WasmBindingBackend maps the stable binding operation set onto one Go/WASM engine generation. */
export class WasmBindingBackend implements ProtoBindingBackend {
  private active = false
  private onEvent: ((payload: Uint8Array) => void) | null = null
  private onClosed: ((error: Error) => void) | null = null

  constructor(private readonly runtime: TermxWasmRuntime) {}

  start(onEvent: (payload: Uint8Array) => void, onClosed: (error: Error) => void): void {
    if (this.active) throw new Error('WASM binding backend is already started')
    this.active = true
    this.onEvent = onEvent
    this.onClosed = onClosed
    void this.pumpEvents()
  }

  async request(operation: BindingOperationCode, payload: Uint8Array, handle?: bigint, signal?: AbortSignal): Promise<bigint> {
    if (!this.active) throw new Error('WASM binding backend is closed')
    if (signal?.aborted) throw abortError(signal)
    switch (operation) {
      case BindingOperation.OPEN_SESSION:
        return BigInt(this.runtime.openSession(payload))
      case BindingOperation.EXECUTE:
        return BigInt(this.runtime.execute(requiredHandle(handle), payload))
      case BindingOperation.IMPORT_PAIRING:
        return BigInt(this.runtime.importPairing(payload))
      case BindingOperation.DELETE_CREDENTIAL:
        return BigInt(this.runtime.deleteCredential(payload))
      case BindingOperation.CANCEL:
        this.runtime.cancel(requiredHandle(handle))
        return 0n
      case BindingOperation.CLOSE_SESSION:
        await this.runtime.closeSession(requiredHandle(handle))
        return 0n
      case BindingOperation.RELEASE:
        this.runtime.release(requiredHandle(handle))
        return 0n
      case BindingOperation.OPEN_RESOURCE_STREAM:
        return BigInt(this.runtime.openResourceStream(requiredHandle(handle), payload))
      case BindingOperation.SEND_RESOURCE_STREAM_FRAME:
        await this.runtime.sendResourceStreamFrame(requiredHandle(handle), payload)
        return 0n
      case BindingOperation.CLOSE_RESOURCE_STREAM:
        await this.runtime.closeResourceStream(requiredHandle(handle))
        return 0n
    }
  }

  async close(): Promise<void> {
    if (!this.active) return
    this.active = false
    await this.runtime.close()
  }

  private async pumpEvents(): Promise<void> {
    while (this.active) {
      try {
        this.onEvent?.(await this.runtime.nextEvent())
      } catch (error) {
        if (!this.active) return
        this.active = false
        this.onClosed?.(error instanceof Error ? error : new Error('WASM binding event pump failed'))
        return
      }
    }
  }
}

function requiredHandle(value: bigint | undefined): number {
  if (value === undefined || value <= 0n || value > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('WASM binding handle is invalid')
  return Number(value)
}

function abortError(signal: AbortSignal): Error {
  return signal.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError')
}
