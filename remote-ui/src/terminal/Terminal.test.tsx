import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Terminal, type TerminalProps } from './Terminal'
import { DEFAULT_TERMINAL_SETTINGS } from './terminalSettings'
import { dispatchNativeKeyboardEvent } from '../platform/nativeKeyboard'
import { createMockRtcTerminalSession } from '../test/mockRtcTerminalSession'
import type { ConnectionInfo, RtcBinaryChannel, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSubscription } from '../core/transport'

const xtermMocks = vi.hoisted(() => {
  class FakeXTerm {
    static instances: FakeXTerm[] = []

    readonly options: Record<string, unknown>
    readonly writes: string[] = []
    cols = 80
    rows = 24
    disposed = false
    element: HTMLElement | undefined
    buffer = {
      active: {
        type: 'normal' as const,
        cursorY: 0,
        length: 200,
        viewportY: 20,
      },
      onBufferChange: vi.fn(() => ({ dispose: vi.fn() })),
    }
    private readonly dataHandlers = new Set<(data: string) => void>()
    private readonly binaryHandlers = new Set<(data: string) => void>()
    private readonly cursorHandlers = new Set<() => void>()
    private readonly renderHandlers = new Set<() => void>()
    private readonly writeCallbacks: Array<() => void> = []
    private keyEventHandler: ((event: KeyboardEvent) => boolean) | undefined
    readonly scrollLines = vi.fn((amount: number) => {
      const active = this.buffer.active
      const maxScroll = active.length - this.rows
      active.viewportY = Math.max(0, Math.min(maxScroll, active.viewportY + amount))
      for (const handler of this.renderHandlers) handler()
    })
    readonly scrollToLine = vi.fn((line: number) => {
      const active = this.buffer.active
      const maxScroll = active.length - this.rows
      active.viewportY = Math.max(0, Math.min(maxScroll, line))
      for (const handler of this.renderHandlers) handler()
    })
    readonly scrollToBottom = vi.fn(() => {
      const active = this.buffer.active
      active.viewportY = Math.max(0, active.length - this.rows)
      for (const handler of this.renderHandlers) handler()
    })
    readonly select = vi.fn()
    readonly selectLines = vi.fn()
    readonly focus = vi.fn(() => {
      this.assertActive()
    })
    readonly blur = vi.fn(() => {
      this.assertActive()
    })
    skipNextWriteCallback = false
    deferWriteCallbacks = false

    constructor(options: Record<string, unknown> = {}) {
      this.options = options
      FakeXTerm.instances.push(this)
    }

    loadAddon(): void {}

    open(container: HTMLElement): void {
      this.assertActive()
      const root = document.createElement('div')
      root.className = 'xterm'
      const screen = document.createElement('div')
      screen.className = 'xterm-screen'
      const canvas = document.createElement('canvas')
      canvas.width = 800
      canvas.height = 400
      const textLayer = document.createElement('span')
      textLayer.className = 'xterm-text-layer'
      screen.append(canvas, textLayer)
      root.append(screen)
      container.append(root)
      this.element = root
    }

    write(data: string | Uint8Array, callback?: () => void): void {
      this.assertActive()
      const text = typeof data === 'string' ? data : new TextDecoder().decode(data)
      this.writes.push(text)
      const textLayer = this.element?.querySelector('.xterm-text-layer')
      if (textLayer) textLayer.textContent = `${textLayer.textContent ?? ''}${text}`
      if (this.skipNextWriteCallback) {
        this.skipNextWriteCallback = false
        return
      }
      if (this.deferWriteCallbacks && callback) {
        this.writeCallbacks.push(callback)
        return
      }
      callback?.()
    }

    flushNextWriteCallback(): void {
      this.writeCallbacks.shift()?.()
    }

    pendingWriteCallbacks(): number {
      return this.writeCallbacks.length
    }

    onData(handler: (data: string) => void): { dispose(): void } {
      this.dataHandlers.add(handler)
      return {
        dispose: () => this.dataHandlers.delete(handler),
      }
    }

    onBinary(handler: (data: string) => void): { dispose(): void } {
      this.binaryHandlers.add(handler)
      return {
        dispose: () => this.binaryHandlers.delete(handler),
      }
    }

    attachCustomKeyEventHandler(handler: (event: KeyboardEvent) => boolean): void {
      this.keyEventHandler = handler
    }

    onCursorMove(handler: () => void): { dispose(): void } {
      this.cursorHandlers.add(handler)
      return {
        dispose: () => this.cursorHandlers.delete(handler),
      }
    }

    onRender(handler: () => void): { dispose(): void } {
      this.renderHandlers.add(handler)
      return {
        dispose: () => this.renderHandlers.delete(handler),
      }
    }

    emitData(data: string): void {
      for (const handler of this.dataHandlers) handler(data)
    }

    emitBinary(data: string): void {
      for (const handler of this.binaryHandlers) handler(data)
    }

    emitKey(event: KeyboardEvent): boolean {
      return this.keyEventHandler?.(event) ?? true
    }

    emitCursorMove(): void {
      for (const handler of this.cursorHandlers) handler()
    }

    resize(cols: number, rows: number): void {
      this.assertActive()
      this.cols = cols
      this.rows = rows
    }

    clear(): void {
      this.assertActive()
      const textLayer = this.element?.querySelector('.xterm-text-layer')
      if (textLayer) textLayer.textContent = ''
    }

    readonly reset = vi.fn(() => {
      this.clear()
    })

    paste(text: string): void {
      this.write(text)
    }

    getSelection(): string {
      return ''
    }

    hasSelection(): boolean {
      return false
    }

    clearSelection(): void {}

    dispose(): void {
      this.disposed = true
    }

    private assertActive(): void {
      if (this.disposed) throw new Error('xterm used after dispose')
    }
  }

  class FakeFitAddon {
    static instances: FakeFitAddon[] = []
    static nextDimensions: { cols: number; rows: number } | undefined = { cols: 101, rows: 31 }
    static nextDimensionsSequence: Array<{ cols: number; rows: number } | undefined> | undefined

    fitCalls = 0
    dimensions: { cols: number; rows: number } | undefined
    private readonly dimensionsSequence: Array<{ cols: number; rows: number } | undefined> | undefined

    constructor() {
      this.dimensions = FakeFitAddon.nextDimensions
      this.dimensionsSequence = FakeFitAddon.nextDimensionsSequence?.slice()
      FakeFitAddon.instances.push(this)
    }

    fit(): void {
      this.fitCalls += 1
    }

    proposeDimensions(): { cols: number; rows: number } | undefined {
      if (this.dimensionsSequence && this.dimensionsSequence.length > 0) {
        return this.dimensionsSequence.shift()
      }
      return this.dimensions
    }
  }

  return { FakeFitAddon, FakeXTerm }
})

vi.mock('@xterm/xterm', () => ({ Terminal: xtermMocks.FakeXTerm }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: xtermMocks.FakeFitAddon }))
vi.mock('@xterm/addon-canvas', () => ({ CanvasAddon: class FakeCanvasAddon {} }))
vi.mock('@xterm/addon-webgl', () => ({ WebglAddon: class FakeWebglAddon { onContextLoss(): void {}; dispose(): void {} } }))
vi.mock('@xterm/xterm/css/xterm.css', () => ({}))

class TestResizeObserver {
  static instances: TestResizeObserver[] = []
  readonly observe = vi.fn()
  readonly disconnect = vi.fn()

  constructor(private readonly callback: ResizeObserverCallback) {
    TestResizeObserver.instances.push(this)
  }

  trigger(): void {
    this.callback([], this as unknown as ResizeObserver)
  }
}

describe('Terminal', () => {
  const originalResizeObserver = globalThis.ResizeObserver
  const originalRequestAnimationFrame = window.requestAnimationFrame
  const originalCancelAnimationFrame = window.cancelAnimationFrame
  let canvasContextSpy: ReturnType<typeof vi.spyOn> | undefined

  beforeEach(() => {
    canvasContextSpy = vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(() => ({
      beginPath: vi.fn(),
      clearRect: vi.fn(),
      drawImage: vi.fn(),
      fillRect: vi.fn(),
      lineTo: vi.fn(),
      moveTo: vi.fn(),
      stroke: vi.fn(),
      set fillStyle(_value: string) {},
      set lineWidth(_value: number) {},
      set strokeStyle(_value: string) {},
    }) as unknown as CanvasRenderingContext2D)
    xtermMocks.FakeXTerm.instances.length = 0
    xtermMocks.FakeFitAddon.instances.length = 0
    xtermMocks.FakeFitAddon.nextDimensions = { cols: 101, rows: 31 }
    xtermMocks.FakeFitAddon.nextDimensionsSequence = undefined
    TestResizeObserver.instances.length = 0
    globalThis.ResizeObserver = TestResizeObserver as unknown as typeof ResizeObserver
    window.requestAnimationFrame = ((callback: FrameRequestCallback) => window.setTimeout(() => callback(performance.now()), 0)) as typeof window.requestAnimationFrame
    window.cancelAnimationFrame = ((handle: number) => window.clearTimeout(handle)) as typeof window.cancelAnimationFrame
  })

  afterEach(() => {
    cleanup()
    canvasContextSpy?.mockRestore()
    canvasContextSpy = undefined
    globalThis.ResizeObserver = originalResizeObserver
    window.requestAnimationFrame = originalRequestAnimationFrame
    window.cancelAnimationFrame = originalCancelAnimationFrame
  })

  it('uses terminalId as the public component identity and renders an xterm surface', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1')
    expect(screen.getByTestId('termx-terminal').className).toContain('h-full')
    expect(screen.getByTestId('termx-terminal').className).toContain('min-h-0')
    await waitFor(() => expect(session.openedLabels).toEqual(['terminal:terminal-1']))
    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const terminalOutput = screen.getByLabelText('Terminal output')
    expect(terminalOutput.className).toContain('overflow-hidden')
    expect(terminalOutput.querySelector('.xterm-screen')).not.toBeNull()
  })

  it('creates xterm with terminal settings and applies setting updates without remounting', async () => {
    const session = createMockRtcTerminalSession()
    const settings = {
      ...DEFAULT_TERMINAL_SETTINGS,
      fontSize: 16,
      fontFamily: '"FiraCode NF", monospace',
      cursorBlink: false,
      scrollback: 5000,
    }

    const { rerender } = render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        settings={settings}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    expect(term.options.fontSize).toBe(16)
    expect(term.options.fontFamily).toBe('"FiraCode NF", monospace')
    expect(term.options.cursorBlink).toBe(false)
    expect(term.options.scrollback).toBe(5000)

    rerender(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        settings={{ ...settings, fontSize: 18, cursorBlink: true }}
      />,
    )

    expect(xtermMocks.FakeXTerm.instances).toHaveLength(1)
    expect(term.options.fontSize).toBe(18)
    expect(term.options.cursorBlink).toBe(true)
  })

  it('shows terminal channel loading until the data channel opens', async () => {
    const session = new DeferredOpenSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        settings={{
          ...DEFAULT_TERMINAL_SETTINGS,
          scrollbackPrefetchThresholdRows: 160,
        }}
      />,
    )

    await waitFor(() => expect(screen.getByText('Connecting terminal...')).toBeTruthy())
    act(() => session.openChannel())
    await waitFor(() => expect(screen.queryByText('Connecting terminal...')).toBeNull())
  })

  it('does not focus xterm automatically when the terminal opens', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    await waitFor(() => expect(session.openedLabels).toEqual(['terminal:terminal-1']))
    expect(xtermMocks.FakeXTerm.instances[0]?.focus).not.toHaveBeenCalled()
  })

  it('focuses xterm when the user taps the terminal surface', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement

    act(() => {
      screenElement.dispatchEvent(touchEvent('touchstart', screenElement, 120))
      screenElement.dispatchEvent(touchEvent('touchend', screenElement, 120, []))
    })

    expect(term.focus).toHaveBeenCalled()
  })

  it('writes streaming terminal output chunks into xterm before a snapshot arrives', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(session.openedLabels).toEqual(['terminal:terminal-1']))
    session.emitTerminalScreenUpdate('terminal-1', 'streamed output')

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances[0]?.writes.join('')).toContain('streamed output'))
    await waitFor(() => expect(screen.getByLabelText('Terminal output').textContent).toContain('streamed output'))
  })

  it('does not reset and rewrite the whole terminal when the React text cache trims old output', async () => {
    const session = createMockRtcTerminalSession()
    const initialText = 'A'.repeat(1_500_000)
    const nextChunk = 'B'.repeat(2048)

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(session.openedLabels).toEqual(['terminal:terminal-1']))
    const term = await waitFor(() => {
      const current = xtermMocks.FakeXTerm.instances[0]
      expect(current).toBeTruthy()
      return current!
    })

    act(() => {
      session.emitTerminalScreenUpdate('terminal-1', initialText)
    })
    await waitFor(() => expect(term.writes.some((write) => write.includes(initialText))).toBe(true))
    term.writes.splice(0)
    term.reset.mockClear()

    act(() => {
      session.emitTerminalScreenUpdate('terminal-1', nextChunk)
    })

    await waitFor(() => expect(term.writes.some((write) => write.includes(nextChunk))).toBe(true))
    expect(term.reset).not.toHaveBeenCalled()
  })

  it('buffers live output while xterm is busy and flushes it after the write callback', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(session.openedLabels).toEqual(['terminal:terminal-1']))
    const term = await waitFor(() => {
      const current = xtermMocks.FakeXTerm.instances[0]
      expect(current).toBeTruthy()
      return current!
    })

    term.deferWriteCallbacks = true
    act(() => {
      session.emitTerminalScreenUpdate('terminal-1', 'first')
      session.emitTerminalScreenUpdate('terminal-1', 'second')
      session.emitTerminalScreenUpdate('terminal-1', 'third')
    })

    expect(term.pendingWriteCallbacks()).toBe(1)
    expect(term.writes.join('')).toContain('first')
    expect(term.writes.join('')).not.toContain('second')
    expect(term.writes.join('')).not.toContain('third')

    act(() => term.flushNextWriteCallback())

    await waitFor(() => expect(term.pendingWriteCallbacks()).toBe(1))
    await waitFor(() => {
      const replay = term.writes.join('')
      expect(replay).toContain('second')
      expect(replay).toContain('third')
    })
  })

  it('drops oversized pending live output and requests snapshot recovery when xterm falls behind', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'initial-before-overflow',
      cols: 80,
      rows: 24,
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(session.openedLabels).toEqual(['terminal:terminal-1']))
    const term = await waitFor(() => {
      const current = xtermMocks.FakeXTerm.instances[0]
      expect(current).toBeTruthy()
      return current!
    })
    await waitFor(() => expect(term.writes.join('')).toMatch(/i[\s\S]*n[\s\S]*i[\s\S]*t[\s\S]*i[\s\S]*a[\s\S]*l/))
    session.setTerminalSnapshot('terminal-1', {
      text: 'recovered-after-overflow',
      cols: 80,
      rows: 24,
    })
    term.deferWriteCallbacks = true
    term.reset.mockClear()
    term.writes.splice(0)

    act(() => {
      session.emitTerminalScreenUpdate('terminal-1', 'blocked-live-output')
      session.emitTerminalScreenUpdate('terminal-1', 'x'.repeat(600 * 1024))
    })

    await waitFor(() => expect(session.snapshotRequests('terminal-1').length).toBeGreaterThanOrEqual(2))
    await waitFor(() => expect(term.reset).toHaveBeenCalled())
    await waitFor(() => expect(term.writes.join('')).toMatch(/r[\s\S]*e[\s\S]*c[\s\S]*o[\s\S]*v[\s\S]*e[\s\S]*r[\s\S]*e[\s\S]*d[\s\S]*-[\s\S]*a[\s\S]*f[\s\S]*t[\s\S]*e[\s\S]*r[\s\S]*-[\s\S]*o[\s\S]*v[\s\S]*e[\s\S]*r[\s\S]*f[\s\S]*l[\s\S]*o[\s\S]*w/))
    expect(term.writes.join('')).not.toContain('x'.repeat(1000))
  })

  it('resets and replays a recovery snapshot after streamed output was lost', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'initial',
      cols: 80,
      rows: 24,
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(session.openedLabels).toEqual(['terminal:terminal-1']))
    const term = await waitFor(() => {
      const current = xtermMocks.FakeXTerm.instances[0]
      expect(current).toBeTruthy()
      return current!
    })
    await waitFor(() => expect(term.writes.join('')).toMatch(/i[\s\S]*n[\s\S]*i[\s\S]*t[\s\S]*i[\s\S]*a[\s\S]*l/))

    act(() => {
      session.emitTerminalScreenUpdate('terminal-1', 'stale-live-tail')
    })
    await waitFor(() => expect(term.writes.join('')).toContain('stale-live-tail'))
    term.reset.mockClear()
    term.writes.splice(0)

    act(() => {
      session.setTerminalSnapshot('terminal-1', {
        text: 'recovered-100000',
        cols: 80,
        rows: 24,
      })
      session.emitTerminalSyncLost('terminal-1')
    })

    await waitFor(() => expect(term.reset).toHaveBeenCalled())
    await waitFor(() => expect(term.writes.join('')).toMatch(/r[\s\S]*e[\s\S]*c[\s\S]*o[\s\S]*v[\s\S]*e[\s\S]*r[\s\S]*e[\s\S]*d[\s\S]*-[\s\S]*1[\s\S]*0[\s\S]*0[\s\S]*0[\s\S]*0[\s\S]*0/))
  })

  it('replays structured snapshot output into xterm instead of flattening it to plain text', async () => {
    const session = createMockRtcTerminalSession()
    session.emitTerminalSnapshot('terminal-1', {
      text: 'old\ncurrent',
      cols: 80,
      rows: 24,
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(session.openedLabels).toEqual(['terminal:terminal-1']))

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances[0]?.writes.join('')).toContain('\x1b[H\x1b[2J\x1b[H'))
    const replay = xtermMocks.FakeXTerm.instances[0]?.writes.join('') ?? ''
    expect(replay).toMatch(/o[\s\S]*l[\s\S]*d/)
    expect(replay).toMatch(/c[\s\S]*u[\s\S]*r[\s\S]*r[\s\S]*e[\s\S]*n[\s\S]*t/)
    expect(xtermMocks.FakeXTerm.instances[0]?.writes).not.toContain('old\ncurrent')
    expect(xtermMocks.FakeXTerm.instances[0]?.scrollToBottom).toHaveBeenCalled()
  })

  it('keeps a reopened terminal anchored to the cursor after snapshot rendering and refit settle', async () => {
    const session = createMockRtcTerminalSession()
    session.emitTerminalSnapshot('terminal-1', {
      text: Array.from({ length: 120 }, (_value, index) => `line-${index}`).join('\n'),
      cols: 80,
      rows: 24,
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    await waitFor(() => expect(term.scrollToBottom).toHaveBeenCalled())

    term.scrollToBottom.mockClear()
    term.buffer.active.viewportY = 20
    act(() => TestResizeObserver.instances[0]?.trigger())

    await waitFor(() => expect(term.scrollToBottom).toHaveBeenCalled())
    expect(term.buffer.active.viewportY).toBe(Math.max(0, term.buffer.active.length - term.rows))
  })

  it('forwards xterm input through the terminal protocol interface', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    act(() => xtermMocks.FakeXTerm.instances[0]?.emitData('ls\n'))

    await waitFor(() => expect(session.sentText('terminal-1')).toContain('ls\n'))
  })

  it('delegates user keyboard input when the app shell provides an input router', async () => {
    const session = createMockRtcTerminalSession()
    const onInput = vi.fn()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        onInput={onInput}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    act(() => xtermMocks.FakeXTerm.instances[0]?.emitData('ls\n'))

    expect(onInput).toHaveBeenCalledWith('ls\n')
    expect(session.sentText('terminal-1')).not.toContain('ls\n')
  })

  it('applies mobile modifier state to system keyboard input', async () => {
    const session = createMockRtcTerminalSession()
    const onModifierStateChange = vi.fn()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        modifierState={{ ctrl: 'once', alt: 'off' }}
        onModifierStateChange={onModifierStateChange}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    act(() => xtermMocks.FakeXTerm.instances[0]?.emitData('c'))

    await waitFor(() => expect(session.sentText('terminal-1')).toContain('\x03'))
    expect(onModifierStateChange).toHaveBeenCalledWith({ ctrl: 'off', alt: 'off' })
  })

  it('keeps the xterm instance stable when mobile callbacks or modifier props change', async () => {
    const session = createMockRtcTerminalSession()
    const firstCursorMove = vi.fn()
    const secondCursorMove = vi.fn()
    const firstBufferChange = vi.fn()
    const secondBufferChange = vi.fn()

    const { rerender } = render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        modifierState={{ ctrl: 'off', alt: 'off' }}
        onCursorMove={firstCursorMove}
        onBufferChange={firstBufferChange}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    session.emitTerminalScreenUpdate('terminal-1', 'stable output')
    await waitFor(() => expect(screen.getByLabelText('Terminal output').textContent).toContain('stable output'))

    rerender(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        modifierState={{ ctrl: 'once', alt: 'off' }}
        onCursorMove={secondCursorMove}
        onBufferChange={secondBufferChange}
      />,
    )

    expect(xtermMocks.FakeXTerm.instances).toHaveLength(1)
    await waitFor(() => expect(screen.getByLabelText('Terminal output').textContent).toContain('stable output'))

    act(() => xtermMocks.FakeXTerm.instances[0]?.emitCursorMove())
    expect(firstCursorMove).not.toHaveBeenCalled()
    expect(secondCursorMove).toHaveBeenCalled()

    act(() => xtermMocks.FakeXTerm.instances[0]?.emitData('c'))
    await waitFor(() => expect(session.sentText('terminal-1')).toContain('\x03'))
  })

  it('fits xterm and sends terminal resize through the TermX terminal protocol interface', async () => {
    const session = createMockRtcTerminalSession()
    session.emitResizeControl('terminal-1', { canResize: true, reason: 'owner' })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    act(() => TestResizeObserver.instances[0]?.trigger())

    await waitFor(() => expect(session.sentResize('terminal-1')).toEqual({ cols: 101, rows: 31 }))

    xtermMocks.FakeFitAddon.instances[0]!.dimensions = { cols: 120, rows: 40 }
    act(() => TestResizeObserver.instances[0]?.trigger())

    await waitFor(() => expect(session.sentResize('terminal-1')).toEqual({ cols: 120, rows: 40 }))
  })

  it('fits and sends current dimensions before terminal input so mobile can reclaim size', async () => {
    const session = createMockRtcTerminalSession()
    session.emitResizeControl('terminal-1', { canResize: true, reason: 'owner' })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    xtermMocks.FakeFitAddon.instances[0]!.dimensions = { cols: 120, rows: 40 }

    act(() => xtermMocks.FakeXTerm.instances[0]?.emitData('a'))

    await waitFor(() => expect(session.sentText('terminal-1')).toContain('a'))
    await waitFor(() => expect(session.sentResize('terminal-1')).toEqual({ cols: 120, rows: 40 }))
  })

  it('sends resize later if xterm dimensions are unavailable during initial fit', async () => {
    const session = createMockRtcTerminalSession()
    session.emitResizeControl('terminal-1', { canResize: true, reason: 'owner' })
    xtermMocks.FakeFitAddon.nextDimensions = undefined

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeFitAddon.instances).toHaveLength(1))
    expect(session.sentResize('terminal-1')).toBeUndefined()

    xtermMocks.FakeFitAddon.instances[0]!.dimensions = { cols: 88, rows: 28 }
    act(() => TestResizeObserver.instances[0]?.trigger())

    await waitFor(() => expect(session.sentResize('terminal-1')).toEqual({ cols: 88, rows: 28 }))
  })

  it('fits locally but does not send remote resize without resize ownership', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances[0]?.cols).toBe(101))
    expect(xtermMocks.FakeXTerm.instances[0]?.rows).toBe(31)
    expect(session.sentResize('terminal-1')).toBeUndefined()

    xtermMocks.FakeFitAddon.instances[0]!.dimensions = { cols: 120, rows: 40 }
    act(() => TestResizeObserver.instances[0]?.trigger())

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances[0]?.cols).toBe(120))
    expect(xtermMocks.FakeXTerm.instances[0]?.rows).toBe(40)
    expect(session.sentResize('terminal-1')).toBeUndefined()
  })

  it('ignores delayed fit work after xterm has been disposed', async () => {
    const session = createMockRtcTerminalSession()
    const { unmount } = render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    unmount()

    expect(term.disposed).toBe(true)
    expect(() => {
      act(() => TestResizeObserver.instances[0]?.trigger())
    }).not.toThrow()
  })

  it('re-fits after xterm opens so an early one-row measurement does not persist', async () => {
    const session = createMockRtcTerminalSession()
    xtermMocks.FakeFitAddon.nextDimensionsSequence = [
      { cols: 80, rows: 1 },
      { cols: 80, rows: 1 },
      { cols: 101, rows: 31 },
    ]

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances[0]?.rows).toBe(31))
    expect(xtermMocks.FakeFitAddon.instances[0]?.fitCalls).toBeGreaterThanOrEqual(3)
    expect(session.sentResize('terminal-1')).toBeUndefined()
  })

  it('allows remote resize only when resize ownership is granted by terminal protocol', async () => {
    const session = createMockRtcTerminalSession()
    session.emitResizeControl('terminal-1', { canResize: true, reason: 'owner' })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))

    act(() => TestResizeObserver.instances[0]?.trigger())

    await waitFor(() => expect(session.sentResize('terminal-1')).toEqual({ cols: 101, rows: 31 }))
  })

  it('reports resize control changes to the app shell', async () => {
    const session = createMockRtcTerminalSession()
    session.emitResizeControl('terminal-1', { canResize: false, reason: 'size_locked', sizeLocked: true })
    const onResizeControl = vi.fn()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        onResizeControl={onResizeControl}
      />,
    )

    await waitFor(() => expect(onResizeControl).toHaveBeenCalledWith({
      canResize: false,
      reason: 'size_locked',
      sizeLocked: true,
    }))
  })

  it('does not publish tgent pane/session props on the TermX component boundary', () => {
    const propKeys = Object.keys({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      session: createMockRtcTerminalSession(),
    } satisfies TerminalProps)

    expect(propKeys).not.toContain('paneId')
    expect(propKeys).not.toContain('sessionId')
    expect(propKeys).not.toContain('windowId')
  })

  it('exposes mobile terminal controls without leaking tgent pane concepts', async () => {
    const session = createMockRtcTerminalSession()
    const cursorMoves = vi.fn()
    const ref = { current: null as import('./Terminal').TerminalHandle | null }

    render(
      <Terminal
        ref={ref}
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        onCursorMove={cursorMoves}
      />,
    )

    await waitFor(() => expect(ref.current).not.toBeNull())
    expect(Object.keys(ref.current!)).toEqual(expect.arrayContaining([
      'focus',
      'blur',
      'fit',
      'pasteText',
      'getCursorInfo',
      'adjustInputPosition',
      'getBufferType',
    ]))
    expect(Object.keys(ref.current!)).not.toContain('paneId')
    expect(Object.keys(ref.current!)).not.toContain('sessionId')

    act(() => ref.current!.pasteText('echo pasted\n'))
    await waitFor(() => expect(session.sentText('terminal-1')).toContain('\x1b[200~echo pasted\n\x1b[201~'))

    act(() => ref.current!.adjustInputPosition(144))
    await waitFor(() => {
      const xtermElement = xtermMocks.FakeXTerm.instances[0]?.element
      expect(xtermElement?.style.getPropertyValue('--kb-input-bottom')).toBe('144px')
    })

    act(() => xtermMocks.FakeXTerm.instances[0]?.emitCursorMove())
    expect(cursorMoves).toHaveBeenCalled()
  })

  it('uses touch pixel scrolling with a custom scrollbar instead of browser page scrolling', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement
    Object.defineProperty(terminalOutput, 'clientHeight', { configurable: true, value: 400 })

    act(() => {
      screenElement.dispatchEvent(touchEvent('touchstart', screenElement, 220))
      screenElement.dispatchEvent(touchEvent('touchmove', screenElement, 160))
    })

    expect(xtermMocks.FakeXTerm.instances[0]?.scrollLines).toHaveBeenCalled()
    expect(terminalOutput.querySelector('.term-scrollbar-track')?.className).toContain('visible')
  })

  it('clears transient touch-scroll transforms when native keyboard state changes', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement
    Object.defineProperty(terminalOutput, 'clientHeight', { configurable: true, value: 400 })

    act(() => {
      screenElement.dispatchEvent(touchEvent('touchstart', screenElement, 220))
      screenElement.dispatchEvent(touchEvent('touchmove', screenElement, 167))
    })

    expect(screenElement.style.transform).not.toBe('')

    act(() => dispatchNativeKeyboardEvent({ visible: false }))

    await waitFor(() => expect(screenElement.style.transform).toBe(''))
    expect(screenElement.style.willChange).toBe('')
  })

  it('does not send terminal resize directly from native keyboard events', async () => {
    const session = createMockRtcTerminalSession()
    session.emitResizeControl('terminal-1', { canResize: true, reason: 'owner' })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    session.sentResize('terminal-1')
    xtermMocks.FakeFitAddon.instances[0]!.dimensions = { cols: 140, rows: 20 }
    const before = session.sentResize('terminal-1')

    act(() => dispatchNativeKeyboardEvent({ visible: true, keyboardHeight: 300 }))

    expect(session.sentResize('terminal-1')).toEqual(before)
  })

  it('does not preload older normal-buffer scrollback just because the terminal opened', async () => {
    const session = createMockRtcTerminalSession()
    const firstPageRows = Array.from({ length: 250 }, (_value, index) => `older-${index}`)
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: firstPageRows },
        { offset: 250, rows: ['older-250'] },
      ],
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!

    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 600))
    })

    expect(session.historyReplayRequests('terminal-1')).toEqual([])
    expect(term.writes.join('')).not.toMatch(/o[\s\S]*l[\s\S]*d[\s\S]*e[\s\S]*r[\s\S]*-[\s\S]*0/)
    expect(term.writes.join('')).toMatch(/c[\s\S]*u[\s\S]*r[\s\S]*r[\s\S]*e[\s\S]*n[\s\S]*t/)
    expect(term.scrollToBottom).toHaveBeenCalled()
  })

  it('preserves the user viewport when older scrollback is loaded from an explicit upward scroll', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: Array.from({ length: 250 }, (_value, index) => `manual-${index}`) },
      ],
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    await waitFor(() => expect(term.scrollToBottom).toHaveBeenCalled())

    term.scrollToBottom.mockClear()
    const restoreViewportY = 12
    term.buffer.active.viewportY = restoreViewportY
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement
    act(() => {
      screenElement.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -80 }))
    })

    await waitFor(() => expect(session.historyReplayRequests('terminal-1')).toContainEqual({
      beforeOffset: 0,
      limit: 250,
    }))
    await waitFor(() => expect(term.scrollToLine).toHaveBeenCalledWith(restoreViewportY + 250))
    expect(term.scrollToBottom).not.toHaveBeenCalled()
  })

  it('keeps live output that arrives while scrollback history rendering is queued', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: Array.from({ length: 250 }, (_value, index) => `queued-${index}`) },
      ],
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    term.buffer.active.viewportY = 12
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement

    act(() => {
      screenElement.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -80 }))
    })
    await waitFor(() => expect(session.historyReplayRequests('terminal-1')).toContainEqual({
      beforeOffset: 0,
      limit: 250,
    }))
    await act(async () => {
      await Promise.resolve()
    })

    act(() => {
      session.emitTerminalScreenUpdate('terminal-1', '\nlive-after-history')
    })
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 650))
    })

    await waitFor(() => expect(term.writes.join('')).toContain('live-after-history'))
  })

  it('prefetches more scrollback before the user reaches the top', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: Array.from({ length: 250 }, (_value, index) => `older-${index}`) },
        { offset: 250, rows: ['prefetched-before-top'] },
      ],
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    term.buffer.active.viewportY = 0
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement
    act(() => {
      screenElement.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -80 }))
    })

    await waitFor(() => expect(session.historyReplayRequests('terminal-1')).toContainEqual({
      beforeOffset: 0,
      limit: 250,
    }))
    await waitFor(() => expect(term.writes.join('')).toMatch(/o[\s\S]*l[\s\S]*d[\s\S]*e[\s\S]*r[\s\S]*-[\s\S]*0/))
    term.buffer.active.length = 600
    act(() => term.scrollToLine(20))

    await waitFor(() => expect(session.historyReplayRequests('terminal-1')).toContainEqual({
      beforeOffset: 250,
      limit: 250,
    }))
    expect(term.buffer.active.viewportY).toBeGreaterThan(0)
    await waitFor(() => expect(term.writes.join('')).toMatch(/p[\s\S]*r[\s\S]*e[\s\S]*f[\s\S]*e[\s\S]*t[\s\S]*c[\s\S]*h[\s\S]*e[\s\S]*d/))
  })

  it('loads older scrollback from a top wheel gesture even when the local buffer is not scrollable yet', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: ['older-from-wheel'] },
      ],
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        settings={{
          ...DEFAULT_TERMINAL_SETTINGS,
          scrollbackPrefetchThresholdRows: 0,
        }}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    term.buffer.active.length = term.rows
    term.buffer.active.viewportY = 0
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement

    act(() => {
      screenElement.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -80 }))
    })

    await waitFor(() => expect(session.historyReplayRequests('terminal-1')).toContainEqual({
      beforeOffset: 0,
      limit: 250,
    }))
    await waitFor(() => expect(term.writes.join('')).toMatch(/o[\s\S]*l[\s\S]*d[\s\S]*e[\s\S]*r[\s\S]*-[\s\S]*f[\s\S]*r[\s\S]*o[\s\S]*m[\s\S]*-[\s\S]*w[\s\S]*h[\s\S]*e[\s\S]*e[\s\S]*l/))
  })

  it('coalesces scrollback page rendering while the user is actively scrolling', async () => {
    const session = createMockRtcTerminalSession()
    const firstPageRows = Array.from({ length: 250 }, (_value, index) => `coalesced-${index}`)
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: firstPageRows },
        { offset: 250, rows: ['coalesced-250'] },
      ],
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    await waitFor(() => expect(term.scrollToBottom).toHaveBeenCalled())
    term.buffer.active.viewportY = 0
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement
    act(() => {
      screenElement.dispatchEvent(touchEvent('touchstart', screenElement, 120))
      screenElement.dispatchEvent(touchEvent('touchmove', screenElement, 140))
      screenElement.dispatchEvent(touchEvent('touchmove', screenElement, 260))
    })

    await waitFor(() => expect(session.historyReplayRequests('terminal-1')).toContainEqual({
      beforeOffset: 0,
      limit: 250,
    }))
    act(() => {
      screenElement.dispatchEvent(touchEvent('touchend', screenElement, 260, []))
    })
    await waitFor(() => expect(term.writes.join('')).toMatch(/c[\s\S]*o[\s\S]*a[\s\S]*l[\s\S]*e[\s\S]*s[\s\S]*c[\s\S]*e[\s\S]*d[\s\S]*-[\s\S]*1[\s\S]*0[\s\S]*0/))
    expect(session.historyReplayRequests('terminal-1').length).toBeLessThanOrEqual(2)

    expect(term.reset.mock.calls.length).toBeGreaterThanOrEqual(1)
    expect(term.reset.mock.calls.length).toBeLessThanOrEqual(2)
    expect(term.writes.join('')).toMatch(/c[\s\S]*o[\s\S]*a[\s\S]*l[\s\S]*e[\s\S]*s[\s\S]*c[\s\S]*e[\s\S]*d[\s\S]*-[\s\S]*0/)
    expect(term.writes.join('')).toMatch(/c[\s\S]*u[\s\S]*r[\s\S]*r[\s\S]*e[\s\S]*n[\s\S]*t/)
  })

  it('recovers scrollback loading if a history write callback is lost after a stalled renderer', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: Array.from({ length: 250 }, (_value, index) => `stalled-${index}`) },
        { offset: 250, rows: ['after-stall'] },
      ],
    })

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const term = xtermMocks.FakeXTerm.instances[0]!
    term.skipNextWriteCallback = true
    term.buffer.active.viewportY = 0
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement

    act(() => {
      screenElement.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -80 }))
    })
    await waitFor(() => expect(session.historyReplayRequests('terminal-1')).toContainEqual({
      beforeOffset: 0,
      limit: 250,
    }))
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 4200))
    })
    term.buffer.active.viewportY = 0
    act(() => {
      screenElement.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -80 }))
    })

    await waitFor(() => expect(session.historyReplayRequests('terminal-1')).toContainEqual({
      beforeOffset: 250,
      limit: 250,
    }))
    await waitFor(() => expect(term.writes.join('')).toMatch(/a[\s\S]*f[\s\S]*t[\s\S]*e[\s\S]*r[\s\S]*-[\s\S]*s[\s\S]*t[\s\S]*a[\s\S]*l[\s\S]*l/))
  }, 10000)

  it('shows a magnifier and builds a selection while selection mode is active', async () => {
    const session = createMockRtcTerminalSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
        selectionMode
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    const terminalOutput = screen.getByLabelText('Terminal output')
    const screenElement = terminalOutput.querySelector('.xterm-screen') as HTMLElement
    Object.defineProperty(screenElement, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ bottom: 400, height: 400, left: 0, right: 800, top: 0, width: 800 }),
    })
    Object.defineProperty(terminalOutput, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ bottom: 400, height: 400, left: 0, right: 800, top: 0, width: 800 }),
    })

    act(() => {
      screenElement.dispatchEvent(touchEvent('touchstart', screenElement, 120))
    })

    expect(terminalOutput.querySelector<HTMLElement>('.sel-anchor-marker')?.style.display).toBe('block')
    expect(terminalOutput.querySelector<HTMLElement>('.sel-magnifier')?.style.display).toBe('block')

    act(() => {
      screenElement.dispatchEvent(touchEvent('touchend', screenElement, 120, []))
      screenElement.dispatchEvent(touchEvent('touchstart', screenElement, 180))
      screenElement.dispatchEvent(touchEvent('touchmove', screenElement, 220))
    })

    expect(xtermMocks.FakeXTerm.instances[0]?.select).toHaveBeenCalled()
  })
})

function touchEvent(
  type: string,
  target: EventTarget,
  clientY: number,
  touches: Array<{ clientX: number; clientY: number; identifier: number; target: EventTarget }> = [{ identifier: 1, target, clientX: 20, clientY }],
): Event {
  const event = new Event(type, { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'touches', {
    configurable: true,
    value: touches,
  })
  return event
}

class DeferredOpenSession implements RtcSession {
  private readonly backing = createMockRtcTerminalSession()
  private terminalId = ''
  private resolveOpen: ((channel: RtcBinaryChannel) => void) | null = null

  async openTerminal(terminalId: string): Promise<RtcBinaryChannel> {
    this.terminalId = terminalId
    return new Promise((resolve) => {
      this.resolveOpen = resolve
    })
  }

  openChannel(): void {
    const resolve = this.resolveOpen
    this.resolveOpen = null
    if (!resolve) return
    void this.backing.openTerminal(this.terminalId).then(resolve)
  }

  openApi(): Promise<RtcJsonRpcChannel> {
    return this.backing.openApi()
  }

  openFileTransfer(transferId: string): Promise<RtcBinaryChannel> {
    return this.backing.openFileTransfer(transferId)
  }

  subscribeEvents(handler: (event: RtcEvent) => void): RtcSubscription {
    return this.backing.subscribeEvents(handler)
  }

  getConnectionInfo(): Promise<ConnectionInfo> {
    return this.backing.getConnectionInfo()
  }

  getCapabilities(): ReturnType<RtcSession['getCapabilities']> {
    return this.backing.getCapabilities()
  }

  disconnect(): Promise<void> {
    return this.backing.disconnect()
  }
}
