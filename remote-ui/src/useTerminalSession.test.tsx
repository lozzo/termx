import { renderHook, act, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useTerminalSession } from './useTerminalSession'
import { createMockTerminalTransport } from './test/mockTerminalTransport'

describe('useTerminalSession', () => {
  it('keeps native-like lifecycle state in the reducer while the hook sends terminal intent', async () => {
    const transport = createMockTerminalTransport()
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        transport,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    expect(result.current.snapshot.phase).toBe('connected')
    expect(result.current.snapshot.mode).toBe('local')
    expect(result.current.snapshot.activeTerminalId).toBe('terminal-1')

    act(() => result.current.handleAppResume('cold'))

    expect(result.current.snapshot.phase).toBe('verifying')
    expect(result.current.snapshot.resumeVerificationRequired).toBe(true)
    expect(result.current.snapshot.userIntent).toEqual({
      kind: 'terminal',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    })

    act(() => result.current.reattach(transport))

    expect(result.current.snapshot.phase).toBe('connected')
    expect(result.current.snapshot.resumeVerificationRequired).toBe(false)
    expect(transport.openedTerminalIds).toEqual(['terminal-1', 'terminal-1'])
  })

  it('derives connection mode from the injected transport instead of assuming local', async () => {
    const transport = createMockTerminalTransport('machine-remote', 'anonymous_p2p')
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-remote',
        terminalId: 'terminal-1',
        transport,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    expect(result.current.snapshot.mode).toBe('anonymous_p2p')
  })

  it('preserves close reasons as failed channel state', async () => {
    const transport = createMockTerminalTransport()
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        transport,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    act(() => transport.closeTerminal('terminal-1', 'transport reset'))

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']).toEqual({
      state: 'failed',
      error: 'transport reset',
    }))
  })

  it('does not expose pane/session/window state in the hook snapshot', async () => {
    const transport = createMockTerminalTransport()
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        transport,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    expect(JSON.stringify(result.current.snapshot)).not.toMatch(/pane|session|window|workspace|tab/i)
  })
})
