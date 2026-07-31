import { act, cleanup, createEvent, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createRef } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ProtoClientSession } from '../core/protoClientSession'
import { Terminal, type TerminalHandle } from './Terminal'

interface FakeXTermInstance {
  element: HTMLElement | null
  textarea: HTMLTextAreaElement | null
  emitBinary(data: string): void
  emitData(data: string): void
  emitKey(event: KeyboardEvent): boolean
}

const terminalHarness = vi.hoisted(() => ({
  instances: [] as unknown[],
  inputRecoveryFailure: null as string | null,
  unrelatedBanner: false,
  sessionSendInput: vi.fn(),
  sessionSendResize: vi.fn(),
  historySnapshot: false,
  historyLoad: vi.fn(),
  historyReset: vi.fn(),
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
    binaryHandler: ((data: string) => void) | null = null
    keyHandler: ((event: KeyboardEvent) => boolean) | null = null
    textarea: HTMLTextAreaElement | null = null

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
      this.textarea = textarea
    }
    onData(handler: (data: string) => void) {
      this.dataHandler = handler
      return { dispose: () => { this.dataHandler = null } }
    }
    onBinary(handler: (data: string) => void) {
      this.binaryHandler = handler
      return { dispose: () => { this.binaryHandler = null } }
    }
    onCursorMove() { return { dispose() {} } }
    onRender() { return { dispose() {} } }
    attachCustomKeyEventHandler(handler: (event: KeyboardEvent) => boolean) { this.keyHandler = handler }
    emitBinary(data: string) { this.binaryHandler?.(data) }
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
    snapshot: terminalHarness.unrelatedBanner
      ? {
          phase: 'failed',
          terminalChannels: { [terminalId]: { state: 'open' } },
          visibleError: { message: 'unrelated connection failure', recoverable: true, surface: 'banner' },
        }
      : { phase: 'connected', terminalChannels: { [terminalId]: { state: 'open' } } },
    inputRecoveryFailure: terminalHarness.inputRecoveryFailure,
    terminalSnapshot: terminalHarness.historySnapshot
      ? { text: 'live terminal content', cols: 80, rows: 24, alternateScreen: false }
      : null,
    terminalText: terminalHarness.historySnapshot ? 'live terminal content' : '',
    terminalInfo: null,
    resizeControl: { canResize: false, reason: 'follower' },
    sendInput: terminalHarness.sessionSendInput,
    sendResize: terminalHarness.sessionSendResize,
    requestResizeOwner: async () => ({ canResize: true, reason: 'owner' }),
    releaseResizeOwner: async () => ({ canResize: false, reason: 'follower' }),
    loadScrollback: terminalHarness.historyLoad,
    prefetchScrollback: async () => false,
    resetScrollback: terminalHarness.historyReset,
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
    terminalHarness.inputRecoveryFailure = null
    terminalHarness.unrelatedBanner = false
    terminalHarness.historySnapshot = false
    terminalHarness.sessionSendInput.mockReset().mockReturnValue(true)
    terminalHarness.sessionSendResize.mockReset().mockReturnValue(false)
    terminalHarness.historyLoad.mockReset().mockResolvedValue({ loadedRows: 0, totalRows: 0, hasMore: false, alternate: false })
    terminalHarness.historyReset.mockReset()
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

    act(() => {
      expect(xterm.emitKey(event())).toBe(true)
      xterm.emitData('c')
    })
    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['\x03', 'c'])
    expect(onModifierStateChange).not.toHaveBeenCalled()

    act(() => expect(xterm.emitKey(event())).toBe(false))
    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['\x03', 'c', '\x03'])
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

  it('leaves composition keyCode 229 and Unicode custom keys to onData without consuming once', async () => {
    const onInput = vi.fn(() => true)
    const onModifierStateChange = vi.fn()
    render(<Terminal
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

    fireEvent.compositionStart(xterm.textarea!, { data: '' })
    expect(xterm.emitKey(createEvent.keyDown(xterm.textarea!, {
      key: 'a',
      keyCode: 65,
      isComposing: true,
    }) as KeyboardEvent)).toBe(true)
    expect(xterm.emitKey(createEvent.keyDown(xterm.textarea!, {
      key: 'Process',
      keyCode: 229,
      isComposing: false,
    }) as KeyboardEvent)).toBe(true)
    act(() => xterm.emitData('中'))
    fireEvent.compositionEnd(xterm.textarea!, { data: '中' })

    expect(xterm.emitKey(createEvent.keyDown(xterm.textarea!, { key: '中' }) as KeyboardEvent)).toBe(true)
    act(() => xterm.emitData('中'))
    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['中', '中'])
    expect(onModifierStateChange).not.toHaveBeenCalled()

    act(() => expect(xterm.emitKey(createEvent.keyDown(xterm.textarea!, { key: 'c' }) as KeyboardEvent)).toBe(false))
    expect(onInput).toHaveBeenLastCalledWith('\x1b\x03')
    expect(onModifierStateChange).toHaveBeenCalledWith({ ctrl: 'off', alt: 'off' })
  })

  it('reports rejected onData and binary sends through the same owner boundary without consuming once', async () => {
    const onInput = vi.fn(() => false)
    const onModifierStateChange = vi.fn()
    terminalHarness.inputRecoveryFailure = 'input blocked'
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
      xterm.emitData('paste')
      xterm.emitBinary('binary')
    })

    expect(onInput.mock.calls.map(([data]) => data)).toEqual(['paste', 'binary'])
    expect(onModifierStateChange).not.toHaveBeenCalled()
    expect((await screen.findByRole('alert')).textContent).toBe('Input is paused until the connection recovers.')
  })

  it('does not show an input-paused alert for an unrelated banner failure', async () => {
    terminalHarness.unrelatedBanner = true
    render(<Terminal
      machineId="studio"
      terminalId="term-shell"
      session={session}
      renderer="dom"
    />)

    await waitFor(() => expect(terminalHarness.instances).toHaveLength(1))
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('shows stale history recovery and does not retry until reload is pressed', async () => {
    terminalHarness.historySnapshot = true
    terminalHarness.historyLoad
      .mockRejectedValueOnce(Object.assign(new Error('expired token'), { code: 'stale_resource' }))
      .mockResolvedValueOnce({ loadedRows: 0, totalRows: 0, hasMore: false, alternate: false })
    render(<Terminal machineId="studio" terminalId="term-shell" session={session} renderer="dom" />)
    await waitFor(() => expect(terminalHarness.instances).toHaveLength(1))
    const output = (terminalHarness.instances[0] as FakeXTermInstance).element?.querySelector('.xterm-screen')
    if (!output) throw new Error('missing terminal screen')

    fireEvent.wheel(output, { deltaY: -1 })
    const alert = await screen.findByTestId('anytty-history-error')
    expect(alert.getAttribute('role')).toBe('alert')
    expect(screen.getByRole('button', { name: 'Reload history' }).className).toContain('min-h-11')
    expect(terminalHarness.historyLoad).toHaveBeenCalledTimes(1)

    fireEvent.wheel(output, { deltaY: -1 })
    await Promise.resolve()
    expect(terminalHarness.historyLoad).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'Reload history' }))
    await waitFor(() => expect(terminalHarness.historyLoad).toHaveBeenCalledTimes(2))
    expect(terminalHarness.historyReset).toHaveBeenCalledOnce()
  })

  it('dismisses a nonretryable oversized history line without offering reload', async () => {
    terminalHarness.historySnapshot = true
    terminalHarness.historyLoad.mockRejectedValueOnce(Object.assign(new Error('bounded response exceeded'), {
      code: 'resource_exhausted',
      retryable: false,
    }))
    render(<Terminal machineId="studio" terminalId="term-shell" session={session} renderer="dom" />)
    await waitFor(() => expect(terminalHarness.instances).toHaveLength(1))
    const output = (terminalHarness.instances[0] as FakeXTermInstance).element?.querySelector('.xterm-screen')
    if (!output) throw new Error('missing terminal screen')

    fireEvent.wheel(output, { deltaY: -1 })
    const alert = await screen.findByTestId('anytty-history-error')
    expect(alert.textContent).toContain('A terminal history line is too large to display.')
    expect(screen.queryByRole('button', { name: 'Reload history' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    await waitFor(() => expect(screen.queryByTestId('anytty-history-error')).toBeNull())
    fireEvent.wheel(output, { deltaY: -1 })
    await Promise.resolve()
    expect(terminalHarness.historyLoad).toHaveBeenCalledTimes(1)
    expect(terminalHarness.historyReset).not.toHaveBeenCalled()
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
