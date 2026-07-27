import { create } from '@bufbuild/protobuf'
import { act, render, waitFor } from '@testing-library/react'
import { useEffect } from 'react'
import { describe, expect, it } from 'vitest'
import { ResourceHandleSchema, ResourceKind } from '../generated/apipb/common_pb'
import { NativeScreenResultSchema } from '../generated/apipb/history_pb'
import {
  AttachmentHandleSchema,
  TerminalAttachResultSchema,
  TerminalGetResultSchema,
  TerminalInfoSchema,
  TerminalRefSchema,
} from '../generated/apipb/terminal_pb'
import { MockProtoSession, protoResult } from '../test/mockProtoSession'
import { useTerminalSession, type UseTerminalSessionResult } from './useTerminalSession'

describe('useTerminalSession scrollback lifecycle', () => {
  it('cancels an in-flight history operation when the terminal unmounts', async () => {
    let session: MockProtoSession
    session = new MockProtoSession('machine-local', (command) => {
      switch (command.command.case) {
        case 'eventSubscribe':
          return protoResult('eventSubscription', { subscription: resource(ResourceKind.SUBSCRIPTION, 1, session) })
        case 'terminalAttach':
          return protoResult('terminalAttach', create(TerminalAttachResultSchema, {
            attachment: create(AttachmentHandleSchema, {
              resource: resource(ResourceKind.ATTACHMENT, 2, session),
              terminal: terminalRef(session),
            }),
          }))
        case 'terminalGet':
          return protoResult('terminalGet', create(TerminalGetResultSchema, {
            terminal: create(TerminalInfoSchema, { ref: terminalRef(session), name: 'zsh' }),
          }))
        case 'liveScreenGet':
          return protoResult('liveScreen', create(NativeScreenResultSchema))
        case 'historyWindow':
          return new Promise(() => {})
        default:
          return protoResult('acknowledge', {})
      }
    })
    let current: UseTerminalSessionResult | undefined
    const view = render(<Harness session={session} onChange={(value) => { current = value }} />)
    await waitFor(() => expect(current?.terminalSnapshot).not.toBeNull())

    let pending: Promise<unknown> | undefined
    act(() => {
      pending = current?.loadScrollback(25)
    })
    await waitFor(() => {
      const historyIndex = session.commands.findIndex((entry) => entry.command.case === 'historyWindow')
      expect(session.executeSignals[historyIndex]?.aborted).toBe(false)
    })

    view.unmount()

    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    const historyIndex = session.commands.findIndex((entry) => entry.command.case === 'historyWindow')
    expect(session.executeSignals[historyIndex]?.aborted).toBe(true)
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

function terminalRef(session: MockProtoSession) {
  return create(TerminalRefSchema, { endpointId: session.stamp.endpointId, terminalId: 'terminal-1' })
}

function resource(kind: ResourceKind, token: number, session: MockProtoSession) {
  return create(ResourceHandleSchema, {
    kind,
    opaqueToken: new Uint8Array([token]),
    session: session.stamp,
    generation: 1n,
  })
}
