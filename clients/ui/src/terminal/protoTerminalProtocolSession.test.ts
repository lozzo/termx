import { create } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'
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
import { createProtoTerminalProtocolSession } from './protoTerminalProtocolSession'

describe('ProtoTerminalProtocolSession input ordering', () => {
  it('owns an asynchronous live screen refresh failure', async () => {
    let liveScreenCalls = 0
    let session: MockProtoSession
    session = new MockProtoSession('machine-live-refresh', (command) => {
      switch (command.command.case) {
        case 'eventSubscribe':
          return protoResult('eventSubscription', { subscription: resource(ResourceKind.SUBSCRIPTION, 1, session) })
        case 'terminalAttach':
          return protoResult('terminalAttach', create(TerminalAttachResultSchema, {
            attachment: create(AttachmentHandleSchema, {
              resource: resource(ResourceKind.ATTACHMENT, 2, session),
              terminal: create(TerminalRefSchema, { endpointId: session.stamp.endpointId, terminalId: 'terminal-1' }),
            }),
          }))
        case 'terminalGet':
          return protoResult('terminalGet', create(TerminalGetResultSchema, {
            terminal: create(TerminalInfoSchema, {
              ref: create(TerminalRefSchema, { endpointId: session.stamp.endpointId, terminalId: 'terminal-1' }),
            }),
          }))
        case 'liveScreenGet':
          liveScreenCalls += 1
          if (liveScreenCalls > 1) throw new Error('Go binding bridge disconnected')
          return protoResult('liveScreen', create(NativeScreenResultSchema))
        default:
          return protoResult('acknowledge', {})
      }
    })
    const protocol = createProtoTerminalProtocolSession(session)
    await protocol.openTerminal('terminal-1')
    const events: string[] = []
    protocol.subscribeTerminal('terminal-1', (event) => {
      if (event.type === 'closed') events.push(event.reason ?? '')
    })

    protocol.markSyncLost('terminal-1')

    await vi.waitFor(() => expect(events).toEqual(['Go binding bridge disconnected']))
  })

  it('waits for each terminal input acknowledgement before sending the next input', async () => {
    const sent: string[] = []
    const acknowledge: Array<() => void> = []
    let session: MockProtoSession
    session = new MockProtoSession('machine-input-order', (command) => {
      switch (command.command.case) {
        case 'eventSubscribe':
          return protoResult('eventSubscription', { subscription: resource(ResourceKind.SUBSCRIPTION, 1, session) })
        case 'terminalAttach':
          return protoResult('terminalAttach', create(TerminalAttachResultSchema, {
            attachment: create(AttachmentHandleSchema, {
              resource: resource(ResourceKind.ATTACHMENT, 2, session),
              terminal: create(TerminalRefSchema, { endpointId: session.stamp.endpointId, terminalId: 'terminal-1' }),
            }),
          }))
        case 'terminalGet':
          return protoResult('terminalGet', create(TerminalGetResultSchema, {
            terminal: create(TerminalInfoSchema, {
              ref: create(TerminalRefSchema, { endpointId: session.stamp.endpointId, terminalId: 'terminal-1' }),
            }),
          }))
        case 'liveScreenGet':
          return protoResult('liveScreen', create(NativeScreenResultSchema))
        case 'terminalInput':
          sent.push(new TextDecoder().decode(command.command.value.data))
          return new Promise((resolve) => acknowledge.push(() => resolve(protoResult('acknowledge', {}))))
        default:
          return protoResult('acknowledge', {})
      }
    })
    const protocol = createProtoTerminalProtocolSession(session)
    const channel = await protocol.openTerminal('terminal-1')

    channel.sendInput?.('a')
    channel.sendInput?.('b')
    channel.sendInput?.('c')

    await vi.waitFor(() => expect(sent).toEqual(['a']))
    acknowledge.shift()?.()
    await vi.waitFor(() => expect(sent).toEqual(['a', 'b']))
    acknowledge.shift()?.()
    await vi.waitFor(() => expect(sent).toEqual(['a', 'b', 'c']))
    acknowledge.shift()?.()
  })
})

function resource(kind: ResourceKind, token: number, session: MockProtoSession) {
  return create(ResourceHandleSchema, {
    kind,
    opaqueToken: new Uint8Array([token]),
    session: session.stamp,
    generation: 1n,
  })
}
