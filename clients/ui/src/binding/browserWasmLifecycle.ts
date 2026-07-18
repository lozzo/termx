export interface BrowserWasmGeneration {
  close(): Promise<void>
}

export type BrowserWasmGenerationFactory<T extends BrowserWasmGeneration> = () => Promise<T>
export type BrowserWasmGenerationListener<T extends BrowserWasmGeneration> = (generation: T | null) => void

/** BrowserWasmLifecycle serializes tab suspend/resume into whole-engine generation replacement. */
export class BrowserWasmLifecycle<T extends BrowserWasmGeneration> {
  private generation: T | null = null
  private transition: Promise<void> = Promise.resolve()
  private attached = false
  private disposed = false

  constructor(
    private readonly createGeneration: BrowserWasmGenerationFactory<T>,
    private readonly onGeneration?: BrowserWasmGenerationListener<T>,
  ) {}

  get current(): T | null { return this.generation }

  async start(): Promise<T> {
    this.enqueueResume()
    await this.transition
    if (!this.generation) throw new Error('browser WASM generation did not start')
    return this.generation
  }

  attach(): void {
    if (this.attached || this.disposed) return
    this.attached = true
    document.addEventListener('visibilitychange', this.onVisibilityChange)
    document.addEventListener('freeze', this.onFreeze)
    document.addEventListener('resume', this.onResume)
    window.addEventListener('pagehide', this.onPageHide)
    window.addEventListener('pageshow', this.onPageShow)
  }

  async whenIdle(): Promise<void> { await this.transition }

  async dispose(): Promise<void> {
    if (this.disposed) return
    this.disposed = true
    if (this.attached) {
      this.attached = false
      document.removeEventListener('visibilitychange', this.onVisibilityChange)
      document.removeEventListener('freeze', this.onFreeze)
      document.removeEventListener('resume', this.onResume)
      window.removeEventListener('pagehide', this.onPageHide)
      window.removeEventListener('pageshow', this.onPageShow)
    }
    this.enqueueSuspend()
    await this.transition
  }

  private readonly onVisibilityChange = () => {
    if (document.visibilityState === 'hidden') this.enqueueSuspend()
    else this.enqueueResume()
  }

  private readonly onFreeze = () => this.enqueueSuspend()
  private readonly onResume = () => this.enqueueResume()
  private readonly onPageHide = () => this.enqueueSuspend()
  private readonly onPageShow = () => this.enqueueResume()

  private enqueueSuspend(): void {
    this.transition = this.transition.then(async () => {
      const stale = this.generation
      this.generation = null
      this.onGeneration?.(null)
      await stale?.close()
    })
  }

  private enqueueResume(): void {
    this.transition = this.transition.then(async () => {
      if (this.disposed || this.generation) return
      const fresh = await this.createGeneration()
      if (this.disposed) {
        await fresh.close()
        return
      }
      this.generation = fresh
      this.onGeneration?.(fresh)
    })
  }
}
