import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Terminal, type TerminalProps } from './Terminal'
import { createMockRtcTerminalSession } from './test/mockRtcTerminalSession'
import type { ConnectionInfo, RtcBinaryChannel, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSubscription } from './transport'

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
      },
      onBufferChange: vi.fn(() => ({ dispose: vi.fn() })),
    }
    private readonly dataHandlers = new Set<(data: string) => void>()
    private readonly cursorHandlers = new Set<() => void>()

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
      root.append(screen)
      container.append(root)
      this.element = root
    }

    write(data: string | Uint8Array, callback?: () => void): void {
      this.assertActive()
      const text = typeof data === 'string' ? data : new TextDecoder().decode(data)
      this.writes.push(text)
      const screenElement = this.element?.querySelector('.xterm-screen')
      if (screenElement) screenElement.textContent = `${screenElement.textContent ?? ''}${text}`
      callback?.()
    }

    onData(handler: (data: string) => void): { dispose(): void } {
      this.dataHandlers.add(handler)
      return {
        dispose: () => this.dataHandlers.delete(handler),
      }
    }

    onCursorMove(handler: () => void): { dispose(): void } {
      this.cursorHandlers.add(handler)
      return {
        dispose: () => this.cursorHandlers.delete(handler),
      }
    }

    emitData(data: string): void {
      for (const handler of this.dataHandlers) handler(data)
    }

    emitCursorMove(): void {
      for (const handler of this.cursorHandlers) handler()
    }

    focus(): void {
      this.assertActive()
    }

    blur(): void {
      this.assertActive()
    }

    resize(cols: number, rows: number): void {
      this.assertActive()
      this.cols = cols
      this.rows = rows
    }

    clear(): void {
      this.assertActive()
      const screenElement = this.element?.querySelector('.xterm-screen')
      if (screenElement) screenElement.textContent = ''
    }

    reset(): void {
      this.clear()
    }

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

  beforeEach(() => {
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

  it('shows terminal channel loading until the data channel opens', async () => {
    const session = new DeferredOpenSession()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        session={session}
      />,
    )

    await waitFor(() => expect(screen.getByText('Connecting terminal...')).toBeTruthy())
    act(() => session.openChannel())
    await waitFor(() => expect(screen.queryByText('Connecting terminal...')).toBeNull())
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
    session.emitTerminalOutput('terminal-1', new TextEncoder().encode('streamed output'))

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances[0]?.writes.join('')).toContain('streamed output'))
    await waitFor(() => expect(screen.getByLabelText('Terminal output').textContent).toContain('streamed output'))
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
    session.emitTerminalOutput('terminal-1', new TextEncoder().encode('stable output'))
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
    expect(screen.getByLabelText('Terminal output').textContent).toContain('stable output')

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
      expect(xtermElement?.style.getPropertyValue('--termx-keyboard-bottom')).toBe('144px')
    })

    act(() => xtermMocks.FakeXTerm.instances[0]?.emitCursorMove())
    expect(cursorMoves).toHaveBeenCalled()
  })
})

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
