import { act, cleanup, createEvent, render, waitFor } from '@testing-library/react'
import { createRef } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ProtoClientSession } from '../core/protoClientSession'
import { Terminal, type TerminalHandle } from './Terminal'

interface FakeXTermInstance {
  emitData(data: string): void
  emitKey(event: KeyboardEvent): boolean
}

const terminalHarness = vi.hoisted(() => ({
  instances: [] as unknown[],
  sessionSendInput: vi.fn(),
}))

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {}
    proposeDimensions() { return { cols: 80, rows: 24 } }
  },
}))

vi.mock('@xterm/addon-canvas', () => ({ CanvasAddon: class {} }))
vi.mock('@xterm/addon-webgl', () => ({ WebglAddon: class {} }))

vi.mock('@xterm/xterm', () => ({
  Terminal: class FakeXTerm {
    cols = 80
    rows = 24
    element: HTMLElement | null = null
    options: Record<string, unknown>
    bufferLine = { type: 'normal', cursorY: 0, viewportY: 0, length: 24 }
    buffer = {
      active: this.bufferLine,
      normal: this.bufferLine,
      alternate: { ...this.bufferLine, type: 'alternate' },
      onBufferChange: () => ({ dispose() {} }),
    }
    _core = {
      viewport: { scrollBarWidth: 0 },
      coreMouseService: { areMouseEventsActive: false },
      coreService: { decPrivateModes: { applicationCursorKeys: false } },
    }
    dataHandler: ((data: string) => void) | null = null
    keyHandler: ((event: KeyboardEvent) => boolean) | null = null

    constructor(options: Record<string, unknown>) {
      this.options = { ...options }
      terminalHarness.instances.push(this)
    }

    loadAddon() {}
    open(container: HTMLElement) {
      const element = document.createElement('div')
      element.className = 'xterm'
      const screen = document.createElement('div')
      screen.className = 'xterm-screen'
      const textarea = document.createElement('textarea')
      textarea.className = 'xterm-helper-textarea'
      element.append(screen, textarea)
      container.append(element)
      this.element = element
    }
    onData(handler: (data: string) => void) {
      this.dataHandler = handler
      return { dispose: () => { this.dataHandler = null } }
    }
    onBinary() { return { dispose() {} } }
    onCursorMove() { return { dispose() {} } }
    onRender() { return { dispose() {} } }
    attachCustomKeyEventHandler(handler: (event: KeyboardEvent) => boolean) { this.keyHandler = handler }
    emitData(data: string) { this.dataHandler?.(data) }
    emitKey(event: KeyboardEvent) { return this.keyHandler?.(event) ?? true }
    resize(cols: number, rows: number) { this.cols = cols; this.rows = rows }
    write(_text: string, callback?: () => void) { callback?.() }
    reset() {}
    refresh() {}
    scrollToBottom() { this.bufferLine.viewportY = 0 }
    scrollToLine(line: number) { this.bufferLine.viewportY = line }
    scrollLines(lines: number) { this.bufferLine.viewportY += lines }
    select() {}
    selectAll() {}
    getSelection() { return '' }
    hasSelection() { return false }
    clearSelection() {}
    focus() {}
    blur() {}
    dispose() {}
  },
}))

vi.mock('./useTerminalSession', () => ({
  useTerminalSession: ({ terminalId }: { terminalId: string }) => ({
    snapshot: { phase: 'connected', terminalChannels: { [terminalId]: { state: 'open' } } },
    terminalSnapshot: null,
    terminalText: '',
    terminalInfo: null,
    resizeControl: { canResize: false, reason: 'follower' },
    sendInput: terminalHarness.sessionSendInput,
    sendResize: () => false,
    requestResizeOwner: async () => ({ canResize: true, reason: 'owner' }),
    releaseResizeOwner: async () => ({ canResize: false, reason: 'follower' }),
    loadScrollback: async () => ({ loadedRows: 0, totalRows: 0, hasMore: false, alternate: false }),
    prefetchScrollback: async () => false,
    freezeScrollback: () => {},
    resumeLiveScrollback: () => '',
    markSyncLost: () => {},
    handleAppResume: () => {},
    reattach: () => {},
    client: null,
  }),
}))

const session = {} as ProtoClientSession

describe('Terminal input modifier boundary', () => {
  beforeEach(() => {
    terminalHarness.instances.length = 0
    terminalHarness.sessionSendInput.mockReset().mockReturnValue(true)
    vi.stubGlobal('ResizeObserver', class {
      observe() {}
      disconnect() {}
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('applies an ASCII modifier in the custom key handler and updates once state before the next synchronous key', async () => {
    const onInput = vi.fn(() => true)
    const onModifierStateChange = vi.fn()
    render(<Terminal
      machineId="studio"
      terminalId="term-shell"
      session={session}
      modifierState={{ ctrl: 'once', alt: 'off' }}
      onModifierStateChange={onModifierStateChange}
      onInput={onInput}
      renderer="dom"
    />)
    await waitFor(() => expect(terminalHarness.instances).toHaveLength(1))
    const xterm = terminalHarness.instances[0] as FakeXTermInstance
    const first = createEvent.keyDown(document.body, { key: 'c' }) as KeyboardEvent
    const second = createEvent.keyDown(document.body, { key: 'c' }) as KeyboardEvent

    act(() => {
      expect(xterm.emitKey(first)).toBe(false)
      expect(xterm.emitKey(second)).toBe(true)
      xterm.emitData('c')
    })

    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['\x03', 'c'])
    expect(onModifierStateChange).toHaveBeenCalledOnce()
    expect(onModifierStateChange).toHaveBeenCalledWith({ ctrl: 'off', alt: 'off' })
  })

  it('keeps a custom-key once modifier after failure and consumes it after a later accepted send', async () => {
    const onInput = vi.fn()
      .mockReturnValueOnce(false)
      .mockReturnValueOnce(true)
    const onModifierStateChange = vi.fn()
    render(<Terminal
      machineId="studio"
      terminalId="term-shell"
      session={session}
      modifierState={{ ctrl: 'once', alt: 'off' }}
      onModifierStateChange={onModifierStateChange}
      onInput={onInput}
      renderer="dom"
    />)
    await waitFor(() => expect(terminalHarness.instances).toHaveLength(1))
    const xterm = terminalHarness.instances[0] as FakeXTermInstance
    const event = () => createEvent.keyDown(document.body, { key: 'c' }) as KeyboardEvent

    act(() => expect(xterm.emitKey(event())).toBe(false))
    expect(onInput).toHaveBeenLastCalledWith('\x03')
    expect(onModifierStateChange).not.toHaveBeenCalled()

    act(() => expect(xterm.emitKey(event())).toBe(false))
    expect(onInput).toHaveBeenCalledTimes(2)
    expect(onModifierStateChange).toHaveBeenCalledOnce()
    expect(onModifierStateChange).toHaveBeenCalledWith({ ctrl: 'off', alt: 'off' })
  })

  it('keeps arbitrary onData and toolbar paste raw, then applies only navigation and consumes once synchronously', async () => {
    const onInput = vi.fn(() => true)
    const onModifierStateChange = vi.fn()
    const ref = createRef<TerminalHandle>()
    render(<Terminal
      ref={ref}
      machineId="studio"
      terminalId="term-shell"
      session={session}
      modifierState={{ ctrl: 'once', alt: 'once' }}
      onModifierStateChange={onModifierStateChange}
      onInput={onInput}
      renderer="dom"
    />)
    await waitFor(() => expect(terminalHarness.instances).toHaveLength(1))
    const xterm = terminalHarness.instances[0] as FakeXTermInstance

    act(() => {
      xterm.emitData('c')
      xterm.emitData('中')
      xterm.emitData('p')
      xterm.emitData('paste batch')
      xterm.emitData('\x1b[Z')
    })
    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['c', '中', 'p', 'paste batch', '\x1b[Z'])
    expect(onModifierStateChange).not.toHaveBeenCalled()

    expect(ref.current?.pasteText('p')).toBe(true)
    expect(ref.current?.pasteText('paste text')).toBe(true)
    expect(terminalHarness.sessionSendInput.mock.calls.map(([data]) => data)).toEqual(['p', 'paste text'])
    expect(onModifierStateChange).not.toHaveBeenCalled()

    act(() => {
      xterm.emitData('\x1b[A')
      xterm.emitData('\x1b[A')
    })
    expect(onInput.mock.calls.slice(-2).map(([data]) => data)).toEqual(['\x1b[1;7A', '\x1b[A'])
    expect(onModifierStateChange).toHaveBeenCalledOnce()
    expect(onModifierStateChange).toHaveBeenCalledWith({ ctrl: 'off', alt: 'off' })
  })

  it('keeps an onData navigation once modifier until the target accepts it', async () => {
    const onInput = vi.fn()
      .mockReturnValueOnce(false)
      .mockReturnValueOnce(true)
    const onModifierStateChange = vi.fn()
    render(<Terminal
      machineId="studio"
      terminalId="term-shell"
      session={session}
      modifierState={{ ctrl: 'once', alt: 'off' }}
      onModifierStateChange={onModifierStateChange}
      onInput={onInput}
      renderer="dom"
    />)
    await waitFor(() => expect(terminalHarness.instances).toHaveLength(1))
    const xterm = terminalHarness.instances[0] as FakeXTermInstance

    act(() => {
      xterm.emitData('\x1b[C')
      xterm.emitData('\x1b[C')
    })

    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['\x1b[1;5C', '\x1b[1;5C'])
    expect(onModifierStateChange).toHaveBeenCalledOnce()
    expect(onModifierStateChange).toHaveBeenCalledWith({ ctrl: 'off', alt: 'off' })
  })

  it('keeps locked modifiers across custom ASCII keys and onData navigation', async () => {
    const onInput = vi.fn(() => true)
    const onModifierStateChange = vi.fn()
    render(<Terminal
      machineId="studio"
      terminalId="term-shell"
      session={session}
      modifierState={{ ctrl: 'locked', alt: 'locked' }}
      onModifierStateChange={onModifierStateChange}
      onInput={onInput}
      renderer="dom"
    />)
    await waitFor(() => expect(terminalHarness.instances).toHaveLength(1))
    const xterm = terminalHarness.instances[0] as FakeXTermInstance

    act(() => {
      expect(xterm.emitKey(createEvent.keyDown(document.body, { key: 'c' }) as KeyboardEvent)).toBe(false)
      expect(xterm.emitKey(createEvent.keyDown(document.body, { key: 'c' }) as KeyboardEvent)).toBe(false)
      xterm.emitData('\x1b[D')
      xterm.emitData('\x1b[D')
    })

    expect(onInput.mock.calls.map(([data]) => data)).toEqual([
      '\x1b\x03',
      '\x1b\x03',
      '\x1b[1;7D',
      '\x1b[1;7D',
    ])
    expect(onModifierStateChange).not.toHaveBeenCalled()
  })

  it('returns the underlying acceptance result from imperative input and paste handles', async () => {
    terminalHarness.sessionSendInput.mockReturnValue(false)
    const ref = createRef<TerminalHandle>()
    render(<Terminal ref={ref} machineId="studio" terminalId="term-shell" session={session} renderer="dom" />)
    await waitFor(() => expect(ref.current).not.toBeNull())

    expect(ref.current?.sendInput('key')).toBe(false)
    expect(ref.current?.pasteText('paste')).toBe(false)
  })

  it('leaves unsupported key events to xterm instead of consuming a Ctrl once state', async () => {
    const onInput = vi.fn(() => true)
    const onModifierStateChange = vi.fn()
    render(<Terminal
      machineId="studio"
      terminalId="term-shell"
      session={session}
      modifierState={{ ctrl: 'once', alt: 'off' }}
      onModifierStateChange={onModifierStateChange}
      onInput={onInput}
      renderer="dom"
    />)
    await waitFor(() => expect(terminalHarness.instances).toHaveLength(1))
    const xterm = terminalHarness.instances[0] as FakeXTermInstance
    const event = createEvent.keyDown(document.body, { key: '1' }) as KeyboardEvent

    expect(xterm.emitKey(event)).toBe(true)
    expect(onInput).not.toHaveBeenCalled()
    expect(onModifierStateChange).not.toHaveBeenCalled()
  })
})
