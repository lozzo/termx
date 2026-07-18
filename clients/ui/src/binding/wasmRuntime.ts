export interface TermxWasmResult {
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

interface TermxWasmExports {
  termxClientAbiVersion(): number
  termxEngineCreate(): TermxWasmResult
  termxEngineOpenSession(engine: number, payload: Uint8Array): TermxWasmResult
  termxEngineExecute(engine: number, session: number, payload: Uint8Array): TermxWasmResult
  termxEngineOpenResourceStream(engine: number, session: number, payload: Uint8Array): TermxWasmResult
  termxEngineSendResourceStreamFrame(engine: number, stream: number, payload: Uint8Array): Promise<TermxWasmResult>
  termxEngineCloseResourceStream(engine: number, stream: number): Promise<TermxWasmResult>
  termxEngineCommand(engine: number, payload: Uint8Array): TermxWasmResult
  termxEngineNextEvent(engine: number): Promise<TermxWasmResult>
  termxPlatformNextRequest(engine: number): Promise<TermxWasmResult>
  termxPlatformComplete(engine: number, payload: Uint8Array): TermxWasmResult
  termxPlatformEvent(engine: number, payload: Uint8Array): Promise<TermxWasmResult>
  termxEngineCancel(engine: number, operation: number): TermxWasmResult
  termxEngineCloseSession(engine: number, session: number): Promise<TermxWasmResult>
  termxEngineRelease(engine: number, handle: number): TermxWasmResult
  termxEngineClose(engine: number): Promise<TermxWasmResult>
}

export interface WasmPlatformDispatcher {
  handlePlatformRequest(payload: Uint8Array): Promise<Uint8Array>
  close(): Promise<void>
}

export interface LoadTermxWasmOptions {
  wasmUrl: string
  wasmExecUrl: string
}

/** TermxWasmRuntime owns one WASM engine generation and its serialized platform pump. */
export class TermxWasmRuntime {
  private closed = false
  private platformPump: Promise<void> | null = null

  private constructor(
    private readonly exports: TermxWasmExports,
    readonly engineHandle: number,
    private readonly platform: WasmPlatformDispatcher,
  ) {}

  static async create(exports: TermxWasmExports, platform: WasmPlatformDispatcher): Promise<TermxWasmRuntime> {
    if (exports.termxClientAbiVersion() !== 3) throw new Error('unsupported TermX WASM binding ABI')
    const created = requireWasmResult(exports.termxEngineCreate())
    const engine = requireHandle(created)
    const runtime = new TermxWasmRuntime(exports, engine, platform)
    runtime.platformPump = runtime.runPlatformPump()
    return runtime
  }

  openSession(payload: Uint8Array): number {
    return requireHandle(requireWasmResult(this.exports.termxEngineOpenSession(this.engineHandle, payload)))
  }

  execute(session: number, payload: Uint8Array): number {
    return requireHandle(requireWasmResult(this.exports.termxEngineExecute(this.engineHandle, session, payload)))
  }

  openResourceStream(session: number, payload: Uint8Array): number {
    return requireHandle(requireWasmResult(this.exports.termxEngineOpenResourceStream(this.engineHandle, session, payload)))
  }

  async sendResourceStreamFrame(stream: number, payload: Uint8Array): Promise<void> {
    requireWasmResult(await this.exports.termxEngineSendResourceStreamFrame(this.engineHandle, stream, payload))
  }

  async closeResourceStream(stream: number): Promise<void> {
    requireWasmResult(await this.exports.termxEngineCloseResourceStream(this.engineHandle, stream))
  }

  engineCommand(payload: Uint8Array): number {
    return requireHandle(requireWasmResult(this.exports.termxEngineCommand(this.engineHandle, payload)))
  }

  async nextEvent(): Promise<Uint8Array> {
    return requirePayload(requireWasmResult(await this.exports.termxEngineNextEvent(this.engineHandle)))
  }

  async platformEvent(payload: Uint8Array): Promise<void> {
    requireWasmResult(await this.exports.termxPlatformEvent(this.engineHandle, payload))
  }

  cancel(operation: number): void {
    requireWasmResult(this.exports.termxEngineCancel(this.engineHandle, operation))
  }

  async closeSession(session: number): Promise<void> {
    requireWasmResult(await this.exports.termxEngineCloseSession(this.engineHandle, session))
  }

  release(handle: number): void {
    requireWasmResult(this.exports.termxEngineRelease(this.engineHandle, handle))
  }

  async close(): Promise<void> {
    if (this.closed) return
    this.closed = true
    await this.exports.termxEngineClose(this.engineHandle).then(requireWasmResult)
    await this.platform.close()
    await this.platformPump?.catch(() => undefined)
  }

  private async runPlatformPump(): Promise<void> {
    while (!this.closed) {
      const next = await this.exports.termxPlatformNextRequest(this.engineHandle)
      if (next.status !== 0) {
        if (this.closed || next.status === 2 || next.status === 3) return
        throw new Error(next.error || `TermX WASM platform request failed with status ${next.status}`)
      }
      const response = await this.platform.handlePlatformRequest(requirePayload(next))
      requireWasmResult(this.exports.termxPlatformComplete(this.engineHandle, response))
    }
  }
}

let wasmExecPromise: Promise<void> | null = null

/** loadTermxWasmExports loads Go's wasm_exec runtime and waits for the stable TermX exports. */
export async function loadTermxWasmExports(options: LoadTermxWasmOptions): Promise<TermxWasmExports> {
  await loadWasmExec(options.wasmExecUrl)
  const Go = (globalThis as typeof globalThis & { Go?: GoConstructor }).Go
  if (!Go) throw new Error('Go WASM runtime did not register global Go')
  const runtime = new Go()
  const response = await fetch(options.wasmUrl)
  if (!response.ok) throw new Error(`TermX WASM fetch failed: ${response.status}`)
  const bytes = await response.arrayBuffer()
  const instantiated = await WebAssembly.instantiate(bytes, runtime.importObject)
  void runtime.run(instantiated.instance)
  await waitForExports()
  return globalThis as typeof globalThis & TermxWasmExports
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
    if (typeof (globalThis as Partial<TermxWasmExports>).termxEngineCreate === 'function') return
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
  throw new Error('TermX WASM exports did not initialize')
}

function requireWasmResult(result: TermxWasmResult): TermxWasmResult {
  if (result.status !== 0) throw new Error(result.error || `TermX WASM binding failed with status ${result.status}`)
  return result
}

function requireHandle(result: TermxWasmResult): number {
  const handle = result.handle
  if (!Number.isSafeInteger(handle) || (handle ?? 0) <= 0) throw new Error('TermX WASM binding returned an invalid handle')
  return handle as number
}

function requirePayload(result: TermxWasmResult): Uint8Array {
  if (!(result.payload instanceof Uint8Array)) throw new Error('TermX WASM binding returned no binary payload')
  return result.payload.slice()
}
