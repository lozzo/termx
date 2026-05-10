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
