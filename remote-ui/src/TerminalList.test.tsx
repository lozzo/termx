import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { TerminalList, type TerminalListProps } from './TerminalList'
import type { Terminal } from './model'

describe('TerminalList', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders terminals for one machine and opens a terminal by terminalId', async () => {
    const onOpenTerminal = vi.fn()

    render(
      <TerminalList
        machineId="machine-local"
        terminals={[
          terminal({ terminalId: 'terminal-1', title: 'zsh', command: '/bin/zsh', cols: 120, rows: 36 }),
          terminal({ terminalId: 'terminal-2', title: 'logs', command: 'tail -f app.log', cols: 100, rows: 24 }),
        ]}
        onOpenTerminal={onOpenTerminal}
      />,
    )

    expect(screen.getByText('zsh')).toBeTruthy()
    expect(screen.getByText('/bin/zsh')).toBeTruthy()
    expect(screen.getByText('120x36')).toBeTruthy()
    expect(screen.getByText('logs')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: /open zsh/i }))

    expect(onOpenTerminal).toHaveBeenCalledWith({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    })
  })

  it('shows an empty state without mentioning sessions, windows, panes, tabs, or workspaces', () => {
    render(
      <TerminalList
        machineId="machine-local"
        terminals={[]}
        onOpenTerminal={vi.fn()}
      />,
    )

    expect(screen.getByText('No terminals')).toBeTruthy()
    expect(screen.getByTestId('termx-terminal-list').textContent).not.toMatch(/session|window|pane|workspace|tab/i)
  })

  it('keeps the public props machine/terminal only', () => {
    const propKeys = Object.keys({
      machineId: 'machine-local',
      terminals: [],
      onOpenTerminal: vi.fn(),
    } satisfies TerminalListProps)

    expect(propKeys).not.toContain('sessions')
    expect(propKeys).not.toContain('windows')
    expect(propKeys).not.toContain('panes')
    expect(propKeys).not.toContain('paneId')
    expect(propKeys).not.toContain('sessionId')
    expect(propKeys).not.toContain('windowId')
  })
})

function terminal(overrides: Partial<Terminal>): Terminal {
  return {
    terminalId: overrides.terminalId ?? 'terminal-1',
    machineId: overrides.machineId ?? 'machine-local',
    title: overrides.title ?? 'zsh',
    state: overrides.state ?? 'running',
    command: overrides.command,
    cols: overrides.cols,
    rows: overrides.rows,
  }
}
