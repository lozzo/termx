import { create } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'
import { EventEnvelopeSchema } from '../generated/apipb/application_pb'
import { ResourceHandleSchema, ResourceKind } from '../generated/apipb/common_pb'
import {
  CellStyleSchema,
  HistoryCursorSchema,
  HistoryRowSchema,
  HistoryWindowOperation,
  HistoryWindowResultSchema,
  LiveInvalidatedEventSchema,
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
  TerminalSizeSchema,
} from '../generated/apipb/terminal_pb'
import { MockProtoSession, protoResult } from '../test/mockProtoSession'
import { createProtoTerminalProtocolSession } from './protoTerminalProtocolSession'

describe('ProtoTerminalProtocolSession input ordering', () => {
  it('filters terminal events to the terminal being opened', async () => {
    let session: MockProtoSession
    session = new MockProtoSession('machine-scoped-events', (command) => {
      switch (command.command.case) {
        case 'eventSubscribe':
          return protoResult('eventSubscription', { subscription: resource(ResourceKind.SUBSCRIPTION, 1, session) })
        case 'terminalAttach':
          return protoResult('terminalAttach', create(TerminalAttachResultSchema, {
            attachment: create(AttachmentHandleSchema, {
              resource: resource(ResourceKind.ATTACHMENT, 2, session),
              terminal: terminalRef(session, 'terminal-scoped'),
            }),
          }))
        case 'terminalGet':
          return protoResult('terminalGet', create(TerminalGetResultSchema, {
            terminal: create(TerminalInfoSchema, { ref: terminalRef(session, 'terminal-scoped') }),
          }))
        case 'liveScreenGet':
          return protoResult('liveScreen', create(NativeScreenResultSchema))
        default:
          return protoResult('acknowledge', {})
      }
    })
    const protocol = createProtoTerminalProtocolSession(session)

    await protocol.openTerminal('terminal-scoped')

    const subscription = session.commands.find((entry) => entry.command.case === 'eventSubscribe')
    expect(subscription?.command).toMatchObject({
      case: 'eventSubscribe',
      value: { terminal: { endpointId: 'machine-scoped-events', terminalId: 'terminal-scoped' } },
    })
  })

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

  it('preserves prefixed indexed colors in live screen replay', async () => {
    let session: MockProtoSession
    session = new MockProtoSession('machine-live-color', (command) => {
      switch (command.command.case) {
        case 'eventSubscribe':
          return protoResult('eventSubscription', { subscription: resource(ResourceKind.SUBSCRIPTION, 1, session) })
        case 'terminalAttach':
          return protoResult('terminalAttach', create(TerminalAttachResultSchema, {
            attachment: create(AttachmentHandleSchema, {
              resource: resource(ResourceKind.ATTACHMENT, 2, session),
              terminal: terminalRef(session, 'terminal-color'),
            }),
          }))
        case 'terminalGet':
          return protoResult('terminalGet', create(TerminalGetResultSchema, {
            terminal: create(TerminalInfoSchema, { ref: terminalRef(session, 'terminal-color') }),
          }))
        case 'liveScreenGet':
          return protoResult('liveScreen', create(NativeScreenResultSchema, {
            size: create(TerminalSizeSchema, { cols: 5, rows: 1 }),
            rows: [create(ScreenRowSchema, {
              cells: [create(ScreenCellSchema, {
                content: 'COLOR',
                width: 5,
                style: create(CellStyleSchema, {
                  foreground: 'ansi:2',
                  background: 'idx:24',
                  bold: true,
                }),
              })],
            })],
          }))
        default:
          return protoResult('acknowledge', {})
      }
    })
    const protocol = createProtoTerminalProtocolSession(session)
    const replays: string[] = []
    protocol.subscribeTerminal('terminal-color', (event) => {
      if (event.type === 'snapshot') replays.push(event.snapshot.screenReplay ?? '')
    })

    await protocol.openTerminal('terminal-color')

    expect(replays.at(-1)).toContain('\u001b[1;38;5;2;48;5;24mCOLOR')
  })

  it('paginates visual scrollback with the frozen history cursor', async () => {
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
        token: 'history-token',
        historyGeneration: 7n,
        beforeCursor: { lineId: 42n, rowInLine: 0 },
        boundaryFirstLineId: 42n,
        boundaryLastLineId: 42n,
      },
    })
  })

  it('uses the local xterm columns for history instead of the remote PTY size', async () => {
    let session: MockProtoSession
    session = new MockProtoSession('machine-local-cols', (command) => {
      switch (command.command.case) {
        case 'eventSubscribe':
          return protoResult('eventSubscription', { subscription: resource(ResourceKind.SUBSCRIPTION, 1, session) })
        case 'terminalAttach':
          return protoResult('terminalAttach', create(TerminalAttachResultSchema, {
            attachment: create(AttachmentHandleSchema, {
              resource: resource(ResourceKind.ATTACHMENT, 2, session),
              terminal: terminalRef(session, 'terminal-1'),
            }),
          }))
        case 'terminalGet':
          return protoResult('terminalGet', create(TerminalGetResultSchema, {
            terminal: create(TerminalInfoSchema, {
              ref: terminalRef(session, 'terminal-1'),
              size: create(TerminalSizeSchema, { cols: 211, rows: 57 }),
            }),
          }))
        case 'liveScreenGet':
          return protoResult('liveScreen', create(NativeScreenResultSchema, {
            size: create(TerminalSizeSchema, { cols: 211, rows: 57 }),
          }))
        case 'historyWindow':
          return protoResult('historyWindow', create(HistoryWindowResultSchema, {
            terminal: terminalRef(session, 'terminal-1'),
            token: 'local-cols-token',
            operation: HistoryWindowOperation.REPLACE,
            rows: [create(HistoryRowSchema, {
              row: create(ScreenRowSchema, { cells: [create(ScreenCellSchema, { content: 'mobile', width: 6 })] }),
              logicalLineId: 1n,
            })],
            loadedRows: 1,
            historyGeneration: 1n,
            hasMore: false,
          }))
        default:
          return protoResult('acknowledge', {})
      }
    })
    const protocol = createProtoTerminalProtocolSession(session)
    await protocol.openTerminal('terminal-1')

    await protocol.loadScrollback('terminal-1', 0, 250, false, { cols: 50 })

    const historyCommand = session.commands.find((entry) => entry.command.case === 'historyWindow')
    expect(historyCommand?.command).toMatchObject({
      case: 'historyWindow',
      value: { cols: 50 },
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

  it('coalesces a burst of live invalidations while a screen refresh is in flight', async () => {
    let liveScreenCalls = 0
    let completeInFlight: ((revision: bigint) => void) | undefined
    let session: MockProtoSession
    session = new MockProtoSession('machine-live-coalescing', (command) => {
      switch (command.command.case) {
        case 'eventSubscribe':
          return protoResult('eventSubscription', { subscription: resource(ResourceKind.SUBSCRIPTION, 1, session) })
        case 'terminalAttach':
          return protoResult('terminalAttach', create(TerminalAttachResultSchema, {
            attachment: create(AttachmentHandleSchema, {
              resource: resource(ResourceKind.ATTACHMENT, 2, session),
              terminal: terminalRef(session, 'terminal-1'),
            }),
          }))
        case 'terminalGet':
          return protoResult('terminalGet', create(TerminalGetResultSchema, {
            terminal: create(TerminalInfoSchema, { ref: terminalRef(session, 'terminal-1') }),
          }))
        case 'liveScreenGet': {
          liveScreenCalls += 1
          if (liveScreenCalls === 2) {
            return new Promise((resolve) => {
              completeInFlight = (revision) => resolve(protoResult('liveScreen', create(NativeScreenResultSchema, { liveRevision: revision })))
            })
          }
          return protoResult('liveScreen', create(NativeScreenResultSchema, {
            liveRevision: liveScreenCalls === 1 ? 1n : 101n,
          }))
        }
        default:
          return protoResult('acknowledge', {})
      }
    })
    const protocol = createProtoTerminalProtocolSession(session)
    await protocol.openTerminal('terminal-1')

    for (let revision = 2n; revision <= 101n; revision += 1n) {
      session.emit(create(EventEnvelopeSchema, {
        subscription: resource(ResourceKind.SUBSCRIPTION, 1, session),
        event: {
          case: 'liveInvalidated',
          value: create(LiveInvalidatedEventSchema, {
            terminal: terminalRef(session, 'terminal-1'),
            liveRevision: revision,
          }),
        },
      }))
    }

    expect(liveScreenCalls).toBe(2)
    completeInFlight?.(2n)
    await vi.waitFor(() => expect(liveScreenCalls).toBe(3))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(liveScreenCalls).toBe(3)
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

function terminalRef(session: MockProtoSession, terminalId: string) {
  return create(TerminalRefSchema, { endpointId: session.stamp.endpointId, terminalId })
}
