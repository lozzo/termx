import { create } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'
import { ResourceHandleSchema, ResourceKind } from '../generated/apipb/common_pb'
import {
  HistoryCursorSchema,
  HistoryRowSchema,
  HistoryWindowOperation,
  HistoryWindowResultSchema,
  NativeScreenResultSchema,
  ScreenCellSchema,
  ScreenRowSchema,
} from '../generated/apipb/history_pb'
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
  it('publishes endpoint identity with terminal metadata', async () => {
    let session: MockProtoSession
    session = new MockProtoSession('machine-info', (command) => {
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
              name: 'zsh',
            }),
          }))
        case 'liveScreenGet':
          return protoResult('liveScreen', create(NativeScreenResultSchema))
        default:
          return protoResult('acknowledge', {})
      }
    })
    const protocol = createProtoTerminalProtocolSession(session)
    const info: Array<Record<string, unknown>> = []
    protocol.subscribeTerminal('terminal-1', (event) => {
      if (event.type === 'info') info.push(event.info)
    })

    await protocol.openTerminal('terminal-1')

    expect(info).toContainEqual(expect.objectContaining({
      terminal_id: 'terminal-1',
      machine_id: 'machine-info',
      name: 'zsh',
    }))
  })

  it('paginates visual scrollback with the frozen history cursor instead of beforeOffset', async () => {
    let historyCall = 0
    let session: MockProtoSession
    session = new MockProtoSession('machine-history', (command) => {
      switch (command.command.case) {
        case 'eventSubscribe':
          return protoResult('eventSubscription', { subscription: resource(ResourceKind.SUBSCRIPTION, 1, session) })
        case 'historyWindow': {
          historyCall += 1
          const lineId = historyCall === 1 ? 42n : 41n
          return protoResult('historyWindow', create(HistoryWindowResultSchema, {
            terminal: create(TerminalRefSchema, { endpointId: session.stamp.endpointId, terminalId: 'terminal-1' }),
            token: 'history-token',
            operation: historyCall === 1 ? HistoryWindowOperation.REPLACE : HistoryWindowOperation.PREPEND,
            rows: [create(HistoryRowSchema, {
              row: create(ScreenRowSchema, { cells: [create(ScreenCellSchema, { content: `line-${lineId}`, width: 7 })] }),
              logicalLineId: lineId,
              rowInLine: 0,
            })],
            loadedRows: 1,
            totalRows: 42,
            logicalTotal: 42,
            historyGeneration: 7n,
            firstLineId: lineId,
            lastLineId: lineId,
            hasMore: historyCall === 1,
            cursor: create(HistoryCursorSchema, { lineId }),
          }))
        }
        default:
          return protoResult('acknowledge', {})
      }
    })
    const protocol = createProtoTerminalProtocolSession(session)

    const latest = await protocol.loadScrollback('terminal-1', 0, 1)
    const older = await protocol.loadScrollback('terminal-1', latest.rows, 1)

    expect(latest.replay).toContain('line-42')
    expect(older.replay).toContain('line-41')
    const historyCommands = session.commands.filter((entry) => entry.command.case === 'historyWindow')
    expect(historyCommands[1]?.command).toMatchObject({
      case: 'historyWindow',
      value: {
        mode: 2,
        beforeOffset: 0,
        token: 'history-token',
        historyGeneration: 7n,
        beforeCursor: { lineId: 42n, rowInLine: 0 },
        boundaryFirstLineId: 42n,
        boundaryLastLineId: 42n,
      },
    })
  })

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
