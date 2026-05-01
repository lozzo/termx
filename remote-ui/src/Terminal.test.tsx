import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { Terminal, type TerminalProps } from './Terminal'
import { createMockTerminalTransport } from './test/mockTerminalTransport'

describe('Terminal', () => {
  afterEach(() => {
    cleanup()
  })

  it('uses terminalId as the public component identity and renders a stable terminal surface', async () => {
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
  })

  it('renders streaming terminal output chunks before a snapshot arrives', async () => {
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

    await waitFor(() => expect(screen.getByLabelText('Terminal output').textContent).toContain('streamed output'))
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
})
