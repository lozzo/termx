export interface MuxviaWasmResult {
  status: number
  handle?: number | undefined
  payload?: Uint8Array | undefined
  error?: string | undefined
}

interface GoRuntime {
  importObject: WebAssembly.Imports
  run(instance: WebAssembly.Instance): Promise<void>
}

interface GoConstructor {
  new(): GoRuntime
}

interface MuxviaWasmExports {
  muxviaClientAbiVersion(): number
  muxviaEngineCreate(): MuxviaWasmResult
  muxviaEngineOpenSession(engine: number, payload: Uint8Array): MuxviaWasmResult
  muxviaEngineExecute(engine: number, session: number, payload: Uint8Array): MuxviaWasmResult
  muxviaEngineOpenResourceStream(engine: number, session: number, payload: Uint8Array): MuxviaWasmResult
  muxviaEngineSendResourceStreamFrame(engine: number, stream: number, payload: Uint8Array): Promise<MuxviaWasmResult>
  muxviaEngineCloseResourceStream(engine: number, stream: number): Promise<MuxviaWasmResult>
  muxviaEngineCommand(engine: number, payload: Uint8Array): MuxviaWasmResult
  muxviaEngineNextEvent(engine: number): Promise<MuxviaWasmResult>
  muxviaPlatformNextRequest(engine: number): Promise<MuxviaWasmResult>
  muxviaPlatformComplete(engine: number, payload: Uint8Array): MuxviaWasmResult
  muxviaPlatformEvent(engine: number, payload: Uint8Array): Promise<MuxviaWasmResult>
  muxviaEngineCancel(engine: number, operation: number): MuxviaWasmResult
  muxviaEngineCloseSession(engine: number, session: number): Promise<MuxviaWasmResult>
  muxviaEngineRelease(engine: number, handle: number): MuxviaWasmResult
  muxviaEngineClose(engine: number): Promise<MuxviaWasmResult>
}

export interface WasmPlatformDispatcher {
  handlePlatformRequest(payload: Uint8Array): Promise<Uint8Array>
  close(): Promise<void>
}

export interface LoadMuxviaWasmOptions {
  wasmUrl: string
  wasmExecUrl: string
}

/** MuxviaWasmRuntime owns one WASM engine generation and its serialized platform pump. */
export class MuxviaWasmRuntime {
  private closed = false
  private platformPump: Promise<void> | null = null

  private constructor(
    private readonly exports: MuxviaWasmExports,
    readonly engineHandle: number,
    private readonly platform: WasmPlatformDispatcher,
  ) {}

  static async create(exports: MuxviaWasmExports, platform: WasmPlatformDispatcher): Promise<MuxviaWasmRuntime> {
    if (exports.muxviaClientAbiVersion() !== 3) throw new Error('unsupported Muxvia WASM binding ABI')
    const created = requireWasmResult(exports.muxviaEngineCreate())
    const engine = requireHandle(created)
    const runtime = new MuxviaWasmRuntime(exports, engine, platform)
    runtime.platformPump = runtime.runPlatformPump()
    return runtime
  }

  openSession(payload: Uint8Array): number {
    return requireHandle(requireWasmResult(this.exports.muxviaEngineOpenSession(this.engineHandle, payload)))
  }

  execute(session: number, payload: Uint8Array): number {
    return requireHandle(requireWasmResult(this.exports.muxviaEngineExecute(this.engineHandle, session, payload)))
  }

  openResourceStream(session: number, payload: Uint8Array): number {
    return requireHandle(requireWasmResult(this.exports.muxviaEngineOpenResourceStream(this.engineHandle, session, payload)))
  }

  async sendResourceStreamFrame(stream: number, payload: Uint8Array): Promise<void> {
    requireWasmResult(await this.exports.muxviaEngineSendResourceStreamFrame(this.engineHandle, stream, payload))
  }

  async closeResourceStream(stream: number): Promise<void> {
    requireWasmResult(await this.exports.muxviaEngineCloseResourceStream(this.engineHandle, stream))
  }

  engineCommand(payload: Uint8Array): number {
    return requireHandle(requireWasmResult(this.exports.muxviaEngineCommand(this.engineHandle, payload)))
  }

  async nextEvent(): Promise<Uint8Array> {
    return requirePayload(requireWasmResult(await this.exports.muxviaEngineNextEvent(this.engineHandle)))
  }

  async platformEvent(payload: Uint8Array): Promise<void> {
    requireWasmResult(await this.exports.muxviaPlatformEvent(this.engineHandle, payload))
  }

  cancel(operation: number): void {
    requireWasmResult(this.exports.muxviaEngineCancel(this.engineHandle, operation))
  }

  async closeSession(session: number): Promise<void> {
    requireWasmResult(await this.exports.muxviaEngineCloseSession(this.engineHandle, session))
  }

  release(handle: number): void {
    requireWasmResult(this.exports.muxviaEngineRelease(this.engineHandle, handle))
  }

  async close(): Promise<void> {
    if (this.closed) return
    this.closed = true
    await this.exports.muxviaEngineClose(this.engineHandle).then(requireWasmResult)
    await this.platform.close()
    await this.platformPump?.catch(() => undefined)
  }

  private async runPlatformPump(): Promise<void> {
    while (!this.closed) {
      const next = await this.exports.muxviaPlatformNextRequest(this.engineHandle)
      if (next.status !== 0) {
        if (this.closed || next.status === 2 || next.status === 3) return
        throw new Error(next.error || `Muxvia WASM platform request failed with status ${next.status}`)
      }
      const response = await this.platform.handlePlatformRequest(requirePayload(next))
      requireWasmResult(this.exports.muxviaPlatformComplete(this.engineHandle, response))
    }
  }
}

let wasmExecPromise: Promise<void> | null = null

/** loadMuxviaWasmExports loads Go's wasm_exec runtime and waits for the stable Muxvia exports. */
export async function loadMuxviaWasmExports(options: LoadMuxviaWasmOptions): Promise<MuxviaWasmExports> {
  await loadWasmExec(options.wasmExecUrl)
  const Go = (globalThis as typeof globalThis & { Go?: GoConstructor }).Go
  if (!Go) throw new Error('Go WASM runtime did not register global Go')
  const runtime = new Go()
  const response = await fetch(options.wasmUrl)
  if (!response.ok) throw new Error(`Muxvia WASM fetch failed: ${response.status}`)
  const bytes = await response.arrayBuffer()
  const instantiated = await WebAssembly.instantiate(bytes, runtime.importObject)
  void runtime.run(instantiated.instance)
  await waitForExports()
  return globalThis as typeof globalThis & MuxviaWasmExports
}

async function loadWasmExec(url: string): Promise<void> {
  if (wasmExecPromise) return await wasmExecPromise
  wasmExecPromise = new Promise<void>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = url
    script.async = true
    script.onload = () => resolve()
    script.onerror = () => reject(new Error('Go wasm_exec.js failed to load'))
    document.head.append(script)
  })
  return await wasmExecPromise
}

async function waitForExports(): Promise<void> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (typeof (globalThis as Partial<MuxviaWasmExports>).muxviaEngineCreate === 'function') return
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
  throw new Error('Muxvia WASM exports did not initialize')
}

function requireWasmResult(result: MuxviaWasmResult): MuxviaWasmResult {
  if (result.status !== 0) throw new Error(result.error || `Muxvia WASM binding failed with status ${result.status}`)
  return result
}

function requireHandle(result: MuxviaWasmResult): number {
  const handle = result.handle
  if (!Number.isSafeInteger(handle) || (handle ?? 0) <= 0) throw new Error('Muxvia WASM binding returned an invalid handle')
  return handle as number
}

function requirePayload(result: MuxviaWasmResult): Uint8Array {
  if (!(result.payload instanceof Uint8Array)) throw new Error('Muxvia WASM binding returned no binary payload')
  return result.payload.slice()
}
