import { create } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import {
  CellStyleSchema,
  HistoryCursorSchema,
  HistoryLineSpanSchema,
  HistoryRowSchema,
  HistoryWindowOperation,
  HistoryWindowResultSchema,
  RowOwnership,
  ScreenCellSchema,
  ScreenRowSchema,
} from '../generated/apipb/history_pb'
import { TerminalRefSchema, TerminalSizeSchema } from '../generated/apipb/terminal_pb'
import { MockProtoSession, protoResult } from '../test/mockProtoSession'
import { createCoreV2HistorySource } from './coreV2HistorySource'

describe('CoreV2HistorySource generated Proto API', () => {
  it('dispatches endpoint-aware history windows and projects the result', async () => {
    const session = new MockProtoSession('machine-local', () => protoResult('historyWindow', create(HistoryWindowResultSchema, {
      token: 'hist-token', operation: HistoryWindowOperation.REPLACE, size: create(TerminalSizeSchema, { cols: 80, rows: 24 }),
      loadedRows: 0, totalRows: 3, loadedLines: 0, logicalTotal: 3, hasMore: true, historyGeneration: 7n,
      firstLineId: 42n, lastLineId: 43n, cursor: create(HistoryCursorSchema, { lineId: 42n }),
    })))
    const source = createCoreV2HistorySource(session, 'machine-local')

    const result = await source.window({ terminalId: 'terminal-1', mode: 'latest', limit: 20, cols: 80 })

    expect(result).toMatchObject({ token: 'hist-token', generation: '7', totalRows: 3, logicalTotal: 3, hasMore: true })
    expect(session.commands[0]?.command).toMatchObject({
      case: 'historyWindow',
      value: { terminal: { endpointId: 'machine-local', terminalId: 'terminal-1' }, limit: 20, cols: 80 },
    })
  })

  it('rejects endpoint mismatches before dispatch', () => {
    const session = new MockProtoSession('machine-local')
    expect(() => createCoreV2HistorySource(session, 'machine-other')).toThrow(/machine-local.*machine-other/)
    expect(session.commands).toEqual([])
  })

  it('forwards cancellation to the generated Proto execute operation', async () => {
    const session = new MockProtoSession('machine-local', () => protoResult('historyWindow', create(HistoryWindowResultSchema, {
      token: 'hist-token', operation: HistoryWindowOperation.REPLACE, historyGeneration: 1n,
    })))
    const source = createCoreV2HistorySource(session, 'machine-local')
    const controller = new AbortController()

    await source.window(
      { terminalId: 'terminal-1', mode: 'latest', limit: 20, cols: 80 },
      { signal: controller.signal },
    )

    const historyIndex = session.commands.findIndex((entry) => entry.command.case === 'historyWindow')
    expect(session.executeSignals[historyIndex]).toBe(controller.signal)
  })

  it('projects generated Proto history rows, styles, and line spans', async () => {
    const session = new MockProtoSession('machine-local', () => protoResult('historyWindow', create(HistoryWindowResultSchema, {
      terminal: create(TerminalRefSchema, { endpointId: 'machine-local', terminalId: 'terminal-1' }),
      token: 'hist-token',
      operation: HistoryWindowOperation.PREPEND,
      size: create(TerminalSizeSchema, { cols: 80, rows: 24 }),
      rows: [create(HistoryRowSchema, {
        row: create(ScreenRowSchema, {
          cells: [create(ScreenCellSchema, {
            content: 'ERR',
            width: 3,
            style: create(CellStyleSchema, { foreground: 'ansi:1', background: '#102030', bold: true }),
          })],
          tailFill: create(CellStyleSchema, { background: 'idx:24' }),
        }),
        rowKind: 'output',
        ownership: RowOwnership.PERSISTED,
        logicalLineId: 42n,
        rowInLine: 1,
      })],
      lines: [create(HistoryLineSpanSchema, {
        startRow: 0,
        endRow: 0,
        rowKind: 'output',
        logicalLineId: 42n,
        clippedBefore: true,
      })],
      loadedRows: 1,
      totalRows: 10,
      loadedLines: 1,
      logicalTotal: 10,
      hasMore: true,
      historyGeneration: 7n,
      firstLineId: 42n,
      lastLineId: 42n,
      cursor: create(HistoryCursorSchema, { lineId: 42n, rowInLine: 1 }),
    })))
    const source = createCoreV2HistorySource(session, 'machine-local')

    const result = await source.window({ terminalId: 'terminal-1', mode: 'older', limit: 20, cols: 80, token: 'hist-token', beforeCursor: { lineId: '43', rowInLine: 0 } })

    expect(result).toMatchObject({
      terminalId: 'terminal-1',
      op: 'prepend',
      renderRows: [{
        logicalLineId: '42',
        rowInLine: 1,
        ownership: 'persisted',
        cells: [{ text: 'ERR', width: 3, style: { fg: 'ansi:1', bg: '#102030', bold: true } }],
        tailFillStyle: { bg: 'idx:24' },
      }],
      lines: [{ logicalLineId: '42', startRow: 0, endRow: 0, clippedBefore: true }],
    })
  })
})
