import { act, cleanup, render, waitFor } from '@testing-library/react'
import { useEffect } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MockProtoSession } from '../test/mockProtoSession'
import { useTerminalSession, type UseTerminalSessionResult } from './useTerminalSession'

const recoveryHarness = vi.hoisted(() => ({
  failNextSend: false,
  openCalls: 0,
  sent: [] as string[],
  resolveRecoveryOpen: null as null | (() => void),
}))

vi.mock('./protoTerminalProtocolSession', () => ({
  createProtoTerminalProtocolSession: () => ({
    async getConnectionInfo() {
      return {
        path: 'local' as const,
        connectionId: 'recovery-test',
        machineId: 'machine-local',
        relayInUse: false,
      }
    },
    async openTerminal(terminalId: string) {
      recoveryHarness.openCalls += 1
      const createChannel = () => {
        const channel = {
          label: `proto-terminal:${terminalId}`,
          readyState: 'open' as 'open' | 'closed',
          send() {},
          sendInput(data: string) {
            if (recoveryHarness.failNextSend) {
              recoveryHarness.failNextSend = false
              throw new Error('terminal channel send failed')
            }
            recoveryHarness.sent.push(data)
          },
          close() { channel.readyState = 'closed' },
        }
        return channel
      }
      if (recoveryHarness.openCalls === 1) return createChannel()
      return await new Promise<ReturnType<typeof createChannel>>((resolve) => {
        recoveryHarness.resolveRecoveryOpen = () => resolve(createChannel())
      })
    },
    subscribeTerminal() { return () => {} },
    async loadScrollback() {
      return { beforeOffset: 0, limit: 0, rows: 0, replay: '', hasMore: false, alternate: false }
    },
    closeTerminalChannel() {},
  }),
}))

describe('useTerminalSession input recovery ownership', () => {
  beforeEach(() => {
    recoveryHarness.failNextSend = false
    recoveryHarness.openCalls = 0
    recoveryHarness.sent.length = 0
    recoveryHarness.resolveRecoveryOpen = null
  })

  afterEach(() => cleanup())

  it('returns accepted when asynchronous recovery owns the input and retries it exactly once', async () => {
    const session = new MockProtoSession('machine-local')
    let current: UseTerminalSessionResult | undefined
    const view = render(<Harness session={session} onChange={(value) => { current = value }} />)
    await waitFor(() => expect(recoveryHarness.openCalls).toBe(1))

    recoveryHarness.failNextSend = true
    let accepted = false
    let concurrentAccepted = true
    act(() => {
      accepted = current!.sendInput('\x03')
      concurrentAccepted = current!.sendInput('second')
    })

    expect(accepted).toBe(true)
    expect(concurrentAccepted).toBe(false)
    expect(recoveryHarness.sent).toEqual([])
    await waitFor(() => expect(recoveryHarness.resolveRecoveryOpen).not.toBeNull())

    act(() => recoveryHarness.resolveRecoveryOpen?.())
    await waitFor(() => expect(recoveryHarness.sent).toEqual(['\x03']))
    expect(recoveryHarness.openCalls).toBe(2)
    view.unmount()
  })

  it('returns false without transferring ownership after the session dies', async () => {
    const session = new MockProtoSession('machine-local')
    let current: UseTerminalSessionResult | undefined
    const view = render(<Harness session={session} onChange={(value) => { current = value }} />)
    await waitFor(() => expect(recoveryHarness.openCalls).toBe(1))
    await session.close()

    expect(current!.sendInput('blocked')).toBe(false)
    expect(recoveryHarness.sent).toEqual([])
    expect(recoveryHarness.openCalls).toBe(1)
    view.unmount()
  })
})

function Harness({
  session,
  onChange,
}: {
  session: MockProtoSession
  onChange: (value: UseTerminalSessionResult) => void
}) {
  const value = useTerminalSession({ machineId: 'machine-local', terminalId: 'terminal-1', session })
  useEffect(() => onChange(value), [onChange, value])
  return null
}
