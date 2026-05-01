import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Terminal, type TerminalProps } from './Terminal'
import { createMockTerminalTransport } from './test/mockTerminalTransport'

const xtermMocks = vi.hoisted(() => {
  class FakeXTerm {
    static instances: FakeXTerm[] = []

    readonly options: Record<string, unknown>
    readonly writes: string[] = []
    cols = 80
    rows = 24
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
      const root = document.createElement('div')
      root.className = 'xterm'
      const screen = document.createElement('div')
      screen.className = 'xterm-screen'
      root.append(screen)
      container.append(root)
      this.element = root
    }

    write(data: string | Uint8Array, callback?: () => void): void {
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

    focus(): void {}

    blur(): void {}

    resize(cols: number, rows: number): void {
      this.cols = cols
      this.rows = rows
    }

    clear(): void {
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

    dispose(): void {}
  }

  class FakeFitAddon {
    static instances: FakeFitAddon[] = []
    static nextDimensions: { cols: number; rows: number } | undefined = { cols: 101, rows: 31 }

    fitCalls = 0
    dimensions: { cols: number; rows: number } | undefined

    constructor() {
      this.dimensions = FakeFitAddon.nextDimensions
      FakeFitAddon.instances.push(this)
    }

    fit(): void {
      this.fitCalls += 1
    }

    proposeDimensions(): { cols: number; rows: number } | undefined {
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

  beforeEach(() => {
    xtermMocks.FakeXTerm.instances.length = 0
    xtermMocks.FakeFitAddon.instances.length = 0
    xtermMocks.FakeFitAddon.nextDimensions = { cols: 101, rows: 31 }
    TestResizeObserver.instances.length = 0
    globalThis.ResizeObserver = TestResizeObserver as unknown as typeof ResizeObserver
  })

  afterEach(() => {
    cleanup()
    globalThis.ResizeObserver = originalResizeObserver
  })

  it('uses terminalId as the public component identity and renders an xterm surface', async () => {
    const transport = createMockTerminalTransport()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
      />,
    )

    expect(screen.getByTestId('termx-terminal').getAttribute('data-terminal-id')).toBe('terminal-1')
    await waitFor(() => expect(transport.openedLabels).toEqual(['terminal:terminal-1']))
    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    expect(screen.getByLabelText('Terminal output').querySelector('.xterm-screen')).not.toBeNull()
  })

  it('writes streaming terminal output chunks into xterm before a snapshot arrives', async () => {
    const transport = createMockTerminalTransport()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
      />,
    )

    await waitFor(() => expect(transport.openedLabels).toEqual(['terminal:terminal-1']))
    transport.emitTerminalOutput('terminal-1', new TextEncoder().encode('streamed output'))

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances[0]?.writes.join('')).toContain('streamed output'))
    await waitFor(() => expect(screen.getByLabelText('Terminal output').textContent).toContain('streamed output'))
  })

  it('forwards xterm input through the terminal transport interface', async () => {
    const transport = createMockTerminalTransport()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    act(() => xtermMocks.FakeXTerm.instances[0]?.emitData('ls\n'))

    await waitFor(() => expect(transport.sentText('terminal-1')).toContain('ls\n'))
  })

  it('applies mobile modifier state to system keyboard input', async () => {
    const transport = createMockTerminalTransport()
    const onModifierStateChange = vi.fn()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
        modifierState={{ ctrl: 'once', alt: 'off' }}
        onModifierStateChange={onModifierStateChange}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    act(() => xtermMocks.FakeXTerm.instances[0]?.emitData('c'))

    await waitFor(() => expect(transport.sentText('terminal-1')).toContain('\x03'))
    expect(onModifierStateChange).toHaveBeenCalledWith({ ctrl: 'off', alt: 'off' })
  })

  it('keeps the xterm instance stable when mobile callbacks or modifier props change', async () => {
    const transport = createMockTerminalTransport()
    const firstCursorMove = vi.fn()
    const secondCursorMove = vi.fn()
    const firstBufferChange = vi.fn()
    const secondBufferChange = vi.fn()

    const { rerender } = render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
        modifierState={{ ctrl: 'off', alt: 'off' }}
        onCursorMove={firstCursorMove}
        onBufferChange={firstBufferChange}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeXTerm.instances).toHaveLength(1))
    transport.emitTerminalOutput('terminal-1', new TextEncoder().encode('stable output'))
    await waitFor(() => expect(screen.getByLabelText('Terminal output').textContent).toContain('stable output'))

    rerender(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
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
    await waitFor(() => expect(transport.sentText('terminal-1')).toContain('\x03'))
  })

  it('fits xterm and sends terminal resize through the TermX transport interface', async () => {
    const transport = createMockTerminalTransport()

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
      />,
    )

    await waitFor(() => expect(transport.sentResize('terminal-1')).toEqual({ cols: 101, rows: 31 }))

    xtermMocks.FakeFitAddon.instances[0]!.dimensions = { cols: 120, rows: 40 }
    act(() => TestResizeObserver.instances[0]?.trigger())

    await waitFor(() => expect(transport.sentResize('terminal-1')).toEqual({ cols: 120, rows: 40 }))
  })

  it('sends resize later if xterm dimensions are unavailable during initial fit', async () => {
    const transport = createMockTerminalTransport()
    xtermMocks.FakeFitAddon.nextDimensions = undefined

    render(
      <Terminal
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
      />,
    )

    await waitFor(() => expect(xtermMocks.FakeFitAddon.instances).toHaveLength(1))
    expect(transport.sentResize('terminal-1')).toBeUndefined()

    xtermMocks.FakeFitAddon.instances[0]!.dimensions = { cols: 88, rows: 28 }
    act(() => TestResizeObserver.instances[0]?.trigger())

    await waitFor(() => expect(transport.sentResize('terminal-1')).toEqual({ cols: 88, rows: 28 }))
  })

  it('does not publish tgent pane/session props on the TermX component boundary', () => {
    const propKeys = Object.keys({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      transport: createMockTerminalTransport(),
    } satisfies TerminalProps)

    expect(propKeys).not.toContain('paneId')
    expect(propKeys).not.toContain('sessionId')
    expect(propKeys).not.toContain('windowId')
  })

  it('exposes mobile terminal controls without leaking tgent pane concepts', async () => {
    const transport = createMockTerminalTransport()
    const cursorMoves = vi.fn()
    const ref = { current: null as import('./Terminal').TerminalHandle | null }

    render(
      <Terminal
        ref={ref}
        machineId="machine-local"
        terminalId="terminal-1"
        transport={transport}
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
    await waitFor(() => expect(transport.sentText('terminal-1')).toContain('\x1b[200~echo pasted\n\x1b[201~'))

    act(() => ref.current!.adjustInputPosition(144))
    await waitFor(() => {
      const xtermElement = xtermMocks.FakeXTerm.instances[0]?.element
      expect(xtermElement?.style.getPropertyValue('--termx-keyboard-bottom')).toBe('144px')
    })

    act(() => xtermMocks.FakeXTerm.instances[0]?.emitCursorMove())
    expect(cursorMoves).toHaveBeenCalled()
  })
})
