import { describe, expect, it, vi, type Mock } from 'vitest'
import { TerminalClient, type TerminalClientCallbacks } from './terminalClient'
import { createMockTerminalTransport } from './test/mockTerminalTransport'

describe('TerminalClient', () => {
  it('opens terminal channels by terminalId and emits terminal lifecycle messages', () => {
    const transport = createMockTerminalTransport()
    const callbacks = callbacksForTest()
    const client = new TerminalClient(callbacks)

    client.connect('terminal-1', transport)

    expect(transport.openedTerminalIds).toEqual(['terminal-1'])
    expect(transport.openedLabels).toEqual(['terminal:terminal-1'])
    return vi.waitFor(() => expect(callbacks.onLifecycle).toHaveBeenCalledWith({
      type: 'terminal.channelOpen',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    }))
  })

  it('decodes output, snapshot, and terminal info messages without pane/session fields', () => {
    const transport = createMockTerminalTransport()
    const callbacks = callbacksForTest()
    const client = new TerminalClient(callbacks)

    client.connect('terminal-1', transport)
    transport.emitTerminalOutput('terminal-1', new TextEncoder().encode('hello'))
    transport.emitTerminalSnapshot('terminal-1', {
      text: 'prompt',
      cols: 120,
      rows: 36,
    })
    transport.emitTerminalInfo('terminal-1', {
      terminal_id: 'terminal-1',
      machine_id: 'machine-local',
      title: 'zsh',
      state: 'running',
      command: '/bin/zsh',
    })

    expect(callbacks.onOutput).toHaveBeenCalledWith(new TextEncoder().encode('hello'))
    expect(callbacks.onSnapshot).toHaveBeenCalledWith({ text: 'prompt', cols: 120, rows: 36 })
    expect(callbacks.onTerminalInfo).toHaveBeenCalledWith(
      expect.objectContaining({ terminalId: 'terminal-1', machineId: 'machine-local' }),
    )
    expect(JSON.stringify(callbacks.onTerminalInfo.mock.calls)).not.toMatch(/pane|session|window|workspace|tab/i)
  })

  it('reattaches to a new transport without changing terminal identity', async () => {
    const first = createMockTerminalTransport()
    const second = createMockTerminalTransport()
    const callbacks = callbacksForTest()
    const client = new TerminalClient(callbacks)

    client.connect('terminal-1', first)
    await vi.waitFor(() => expect(callbacks.onLifecycle).toHaveBeenCalledTimes(1))
    client.reattach(second)

    expect(first.closedTerminalIds).not.toContain('terminal-1')
    expect(second.openedTerminalIds).toEqual(['terminal-1'])
    await vi.waitFor(() => expect(callbacks.onLifecycle).toHaveBeenLastCalledWith({
      type: 'terminal.channelOpen',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    }))
  })

  it('sends input and resize through the interface channel and reports dropped input through callbacks', async () => {
    const transport = createMockTerminalTransport()
    const callbacks = callbacksForTest()
    const client = new TerminalClient(callbacks)

    client.connect('terminal-1', transport)
    await vi.waitFor(() => expect(callbacks.onLifecycle).toHaveBeenCalledTimes(1))
    client.sendInput('ls\n')
    client.sendResize(100, 30)

    expect(transport.sentText('terminal-1')).toContain('ls\n')
    expect(transport.sentResize('terminal-1')).toEqual({ cols: 100, rows: 30 })

    transport.closeTerminal('terminal-1')
    client.sendInput('dropped')
    expect(callbacks.onInputDropped).toHaveBeenCalledTimes(1)
  })
})

function callbacksForTest(): TerminalClientCallbacks & {
  onOutput: Mock<TerminalClientCallbacks['onOutput']>
  onSnapshot: Mock<TerminalClientCallbacks['onSnapshot']>
  onTerminalInfo: Mock<NonNullable<TerminalClientCallbacks['onTerminalInfo']>>
  onLifecycle: Mock<NonNullable<TerminalClientCallbacks['onLifecycle']>>
  onInputDropped: Mock<NonNullable<TerminalClientCallbacks['onInputDropped']>>
} {
  return {
    onOutput: vi.fn<TerminalClientCallbacks['onOutput']>(),
    onSnapshot: vi.fn<TerminalClientCallbacks['onSnapshot']>(),
    onTerminalInfo: vi.fn<NonNullable<TerminalClientCallbacks['onTerminalInfo']>>(),
    onLifecycle: vi.fn<NonNullable<TerminalClientCallbacks['onLifecycle']>>(),
    onError: vi.fn<TerminalClientCallbacks['onError']>(),
    onClose: vi.fn<TerminalClientCallbacks['onClose']>(),
    onOpen: vi.fn<NonNullable<TerminalClientCallbacks['onOpen']>>(),
    onInputDropped: vi.fn<NonNullable<TerminalClientCallbacks['onInputDropped']>>(),
  }
}
