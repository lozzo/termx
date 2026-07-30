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

  it('applies xterm onData modifiers and consumes once only after the delegate accepts', async () => {
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

    act(() => xterm.emitData('c'))
    expect(onInput).toHaveBeenLastCalledWith('\x03')
    expect(onModifierStateChange).not.toHaveBeenCalled()

    act(() => xterm.emitData('c'))
    expect(onInput).toHaveBeenCalledTimes(2)
    expect(onModifierStateChange).toHaveBeenCalledOnce()
    expect(onModifierStateChange).toHaveBeenCalledWith({ ctrl: 'off', alt: 'off' })
  })

  it('sends the same text batch raw from xterm onData and toolbar paste without consuming once', async () => {
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
    const batch = '你好 world'

    act(() => xterm.emitData(batch))
    expect(onInput).toHaveBeenCalledWith(batch)
    expect(onModifierStateChange).not.toHaveBeenCalled()

    expect(ref.current?.pasteText(batch)).toBe(true)
    expect(terminalHarness.sessionSendInput).toHaveBeenCalledWith(batch, { cols: 80, rows: 24 })
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
