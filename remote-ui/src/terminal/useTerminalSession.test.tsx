import { renderHook, act, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useTerminalSession } from './useTerminalSession'
import { createMockRtcTerminalSession } from '../test/mockRtcTerminalSession'
import type { ConnectionInfo, RtcBinaryChannel, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSubscription } from '../core/transport'

describe('useTerminalSession', () => {
  it('keeps native-like lifecycle state in the reducer while the hook sends terminal intent', async () => {
    const session = createMockRtcTerminalSession()
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session: session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    expect(result.current.snapshot.phase).toBe('connected')
    expect(result.current.snapshot.path).toBe('local')
    expect(result.current.snapshot.activeTerminalId).toBe('terminal-1')

    act(() => result.current.handleAppResume('cold'))

    expect(result.current.snapshot.phase).toBe('verifying')
    expect(result.current.snapshot.resumeVerificationRequired).toBe(true)
    expect(result.current.snapshot.userIntent).toEqual({
      kind: 'terminal',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
    })

    act(() => result.current.reattach(session))

    await waitFor(() => expect(result.current.snapshot.phase).toBe('connected'))
    expect(result.current.snapshot.resumeVerificationRequired).toBe(false)
    await waitFor(() => expect(session.openedTerminalIds).toEqual(['terminal-1', 'terminal-1']))
  })

  it('can force a terminal data channel refresh while preserving the app-level session', async () => {
    const session = createMockRtcTerminalSession()
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))

    act(() => result.current.reattach(session, { forceTerminalChannel: true }))

    await waitFor(() => expect(session.openedTerminalIds).toEqual(['terminal-1', 'terminal-1']))
    expect(session.closedTerminalIds).toContain('terminal-1')
  })

  it('derives connection path from the injected session instead of assuming local', async () => {
    const session = createMockRtcTerminalSession('machine-remote', 'public_p2p')
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-remote',
        terminalId: 'terminal-1',
        session: session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    expect(result.current.snapshot.path).toBe('public_p2p')
  })

  it('preserves close reasons as failed channel state', async () => {
    const session = createMockRtcTerminalSession()
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session: session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    act(() => session.closeTerminal('terminal-1', 'session reset'))

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']).toEqual({
      state: 'failed',
      error: 'session reset',
    }))
  })

  it('does not expose pane/session/window state in the hook snapshot', async () => {
    const session = createMockRtcTerminalSession()
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session: session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    expect(JSON.stringify(result.current.snapshot)).not.toMatch(/pane|session|window|workspace|tab/i)
  })

  it('loads older scrollback incrementally without reopening the terminal', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: ['older'] },
      ],
    })
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    await waitFor(() => expect(result.current.terminalText).toMatch(/c[\s\S]*u[\s\S]*r[\s\S]*r[\s\S]*e[\s\S]*n[\s\S]*t/))

    await act(async () => {
      await expect(result.current.loadScrollback(100)).resolves.toMatchObject({
        loadedRows: 1,
        totalRows: 1,
        hasMore: false,
      })
    })

    expect(session.openedTerminalIds).toEqual(['terminal-1'])
    expect(session.historyReplayRequests('terminal-1')).toContainEqual({ beforeOffset: 0, limit: 100 })
    expect(result.current.terminalText).toMatch(/o[\s\S]*l[\s\S]*d[\s\S]*e[\s\S]*r/)
    expect(result.current.terminalText).toMatch(/c[\s\S]*u[\s\S]*r[\s\S]*r[\s\S]*e[\s\S]*n[\s\S]*t/)
  })

  it('preserves loaded scrollback when sync loss recovery replaces the current screen', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: ['older'] },
      ],
    })
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    await act(async () => {
      await result.current.loadScrollback(100)
    })
    expect(result.current.terminalText).toMatch(/o[\s\S]*l[\s\S]*d[\s\S]*e[\s\S]*r/)

    session.setTerminalSnapshot('terminal-1', {
      text: 'updated',
      cols: 80,
      rows: 24,
      scrollbackRows: [],
      alternateScreen: false,
    })
    act(() => {
      session.emitTerminalSyncLost('terminal-1')
    })

    await waitFor(() => {
      expect(session.snapshotRequests('terminal-1').some((request) => request.limit === 1)).toBe(true)
    })
    await waitFor(() => expect(result.current.terminalText).toMatch(/u[\s\S]*p[\s\S]*d[\s\S]*a[\s\S]*t[\s\S]*e[\s\S]*d/))
    expect(result.current.terminalText).toMatch(/o[\s\S]*l[\s\S]*d[\s\S]*e[\s\S]*r/)
  })

  it('drops preserved normal scrollback when sync loss recovery enters alternate screen', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
      pages: [
        { offset: 0, rows: ['older'] },
      ],
    })
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    await act(async () => {
      await result.current.loadScrollback(100)
    })
    expect(result.current.terminalText).toMatch(/o[\s\S]*l[\s\S]*d[\s\S]*e[\s\S]*r/)

    session.setTerminalSnapshot('terminal-1', {
      text: 'updated',
      cols: 80,
      rows: 24,
      raw: {
        size: { cols: 80, rows: 24 },
        modes: { alternate_screen: true },
        screen: {
          rows: [{
            cells: Array.from('updated').map((char) => ({ r: char })),
          }],
        },
      },
      alternateScreen: true,
    })
    act(() => {
      session.emitTerminalSyncLost('terminal-1')
    })

    await waitFor(() => {
      expect(session.snapshotRequests('terminal-1').some((request) => request.limit === 1)).toBe(true)
    })
    await waitFor(() => expect(result.current.terminalText).toMatch(/u[\s\S]*p[\s\S]*d[\s\S]*a[\s\S]*t[\s\S]*e[\s\S]*d/))
    expect(result.current.terminalText).not.toMatch(/o[\s\S]*l[\s\S]*d[\s\S]*e[\s\S]*r/)
  })

  it('keeps the terminal text cache bounded during high-volume output bursts', async () => {
    const session = createMockRtcTerminalSession()
    session.setTerminalSnapshot('terminal-1', {
      text: 'current',
      cols: 80,
      rows: 24,
    })
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))

    const encoder = new TextEncoder()
    act(() => {
      session.emitTerminalOutput('terminal-1', encoder.encode(`${'old'.repeat(600_000)}\nKEEP-RECENT`))
    })

    await waitFor(() => expect(result.current.terminalText).toContain('KEEP-RECENT'))
    expect(result.current.terminalText.length).toBeLessThanOrEqual(1_500_000)
    expect(result.current.terminalText).not.toContain('oldoldoldoldold')
  })

  it('refreshes the terminal data channel and retries recent input when the channel closes during send', async () => {
    const session = createMockRtcTerminalSession()
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    session.failNextTerminalSend('terminal-1')

    act(() => {
      result.current.sendInput('echo recovered\n')
    })

    await waitFor(() => expect(session.openedTerminalIds).toEqual(['terminal-1', 'terminal-1']))
    await waitFor(() => expect(session.sentText('terminal-1')).toBe('echo recovered\n'))
    expect(session.closedTerminalIds).toContain('terminal-1')
    expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open')
  })

  it('reattaches and retries resize ownership when the attachment channel is stale', async () => {
    const session = createMockRtcTerminalSession()
    session.setEnsureResizeControl('terminal-1', { canResize: true, reason: 'owner' })
    const { result } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session,
      }),
    )

    await waitFor(() => expect(result.current.snapshot.terminalChannels['terminal-1']?.state).toBe('open'))
    session.failNextEnsureResize('terminal-1', 'termx: permission denied: attachment channel 1 does not match terminal "1"')

    await act(async () => {
      await expect(result.current.requestResizeOwner({ cols: 120, rows: 40 })).resolves.toEqual({
        canResize: true,
        reason: 'owner',
      })
    })

    expect(session.openedTerminalIds).toEqual(['terminal-1', 'terminal-1'])
    expect(session.closedTerminalIds).toContain('terminal-1')
    expect(result.current.resizeControl).toEqual({ canResize: true, reason: 'owner' })
  })

  it('closes a raw terminal channel that resolves after the hook has unmounted', async () => {
    const session = new DeferredTerminalSession()
    const { unmount } = renderHook(() =>
      useTerminalSession({
        machineId: 'machine-local',
        terminalId: 'terminal-1',
        session,
      }),
    )

    await waitFor(() => expect(session.openedTerminalIds).toEqual(['terminal-1']))
    unmount()
    const channel = new DeferredTerminalChannel('terminal:terminal-1')
    act(() => session.resolveTerminal(channel))

    await waitFor(() => expect(channel.closed).toBe(true))
  })
})

class DeferredTerminalSession implements RtcSession {
  readonly openedTerminalIds: string[] = []
  private resolveOpen: ((channel: RtcBinaryChannel) => void) | null = null

  async openTerminal(terminalId: string): Promise<RtcBinaryChannel> {
    this.openedTerminalIds.push(terminalId)
    return new Promise((resolve) => {
      this.resolveOpen = resolve
    })
  }

  resolveTerminal(channel: RtcBinaryChannel): void {
    this.resolveOpen?.(channel)
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    throw new Error('api is not used by this test')
  }

  async openFileTransfer(): Promise<RtcBinaryChannel> {
    throw new Error('file transfer is not used by this test')
  }

  subscribeEvents(_handler: (event: RtcEvent) => void): RtcSubscription {
    return { close() {} }
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    return {
      path: 'local',
      connectionId: 'deferred-connection',
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      relayInUse: false,
    }
  }

  async getCapabilities() {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: false,
    }
  }

  async disconnect(): Promise<void> {}
}

class DeferredTerminalChannel implements RtcBinaryChannel {
  readyState: RtcBinaryChannel['readyState'] = 'open'
  closed = false

  constructor(readonly label: string) {}

  send(): void {}

  close(): void {
    this.closed = true
    this.readyState = 'closed'
  }

  onMessage() {
    return { close() {} }
  }

  onClose() {
    return { close() {} }
  }

  waitOpen(): Promise<void> {
    return Promise.resolve()
  }
}
