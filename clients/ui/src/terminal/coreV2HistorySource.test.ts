import { create } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import { HistoryCursorSchema, HistoryWindowOperation, HistoryWindowResultSchema } from '../generated/apipb/history_pb'
import { TerminalSizeSchema } from '../generated/apipb/terminal_pb'
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
})
