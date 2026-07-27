import { describe, expect, it } from 'vitest'
import { coreV2HistoryRowsANSI } from './coreV2HistoryANSI'
import type { CoreV2HistorySource } from './coreV2HistorySource'
import { CoreV2ScrollbackPager } from './coreV2ScrollbackPager'
import type {
  CoreV2HistoryCopyRequest,
  CoreV2HistoryWindow,
  CoreV2HistoryWindowRequest,
} from './coreV2TerminalProtocol'

describe('CoreV2ScrollbackPager', () => {
  it('continues older pages with the frozen token, generation, cursor, and boundaries', async () => {
    const source = new MockSource([
      window({ lineId: '42', token: 'token-1', generation: '7', hasMore: true }),
      window({ lineId: '41', token: 'token-1', generation: '7', op: 'prepend', hasMore: false }),
    ])
    const pager = new CoreV2ScrollbackPager(source)

    const latest = await pager.load({ terminalId: 'terminal-1', offset: 0, limit: 1, cols: 80 })
    const older = await pager.load({ terminalId: 'terminal-1', offset: latest.totalLoadedRows, limit: 1, cols: 80 })

    expect(older.totalLoadedRows).toBe(2)
    expect(older.rows[0]?.logicalLineId).toBe('41')
    expect(source.requests[1]).toEqual(expect.objectContaining({
      mode: 'older',
      token: 'token-1',
      generation: '7',
      beforeCursor: { lineId: '42', rowInLine: 0 },
      boundaryFirstLineId: '42',
      boundaryLastLineId: '42',
    }))
    expect(latest.operation).toBe('replace')
    expect(older.operation).toBe('prepend')
  })

  it('replaces a stale frozen window when the caller resets to offset zero', async () => {
    const source = new MockSource([
      window({ lineId: '10', token: 'token-1', generation: '1', hasMore: false }),
      window({ lineId: '20', token: 'token-2', generation: '2', hasMore: true }),
    ])
    const pager = new CoreV2ScrollbackPager(source)

    await pager.load({ terminalId: 'terminal-1', offset: 0, limit: 1, cols: 80 })
    const refreshed = await pager.load({ terminalId: 'terminal-1', offset: 0, limit: 1, cols: 80 })

    expect(refreshed).toMatchObject({
      operation: 'replace',
      totalLoadedRows: 1,
      historyGeneration: '2',
    })
    expect(source.releases).toEqual([{ terminalId: 'terminal-1', token: 'token-1', generation: '1' }])
  })

  it('serializes structured history styles and tail fill into replay ANSI', () => {
    const replay = coreV2HistoryRowsANSI([{
      index: 0,
      logicalLineId: '1',
      rowInLine: 0,
      cells: [{ text: 'ERR', width: 3, style: { fg: 'ansi:1', bold: true } }],
      tailFillStyle: { bg: '#102030' },
    }], 5)

    expect(replay).toContain('\u001b[1;38;5;1mERR')
    expect(replay).toContain('\u001b[48;2;16;32;48m  ')
  })
})

class MockSource implements CoreV2HistorySource {
  readonly requests: CoreV2HistoryWindowRequest[] = []
  readonly releases: Array<{ terminalId: string; token: string; generation?: string | number | bigint }> = []

  constructor(private readonly windows: CoreV2HistoryWindow[]) {}

  async window(request: CoreV2HistoryWindowRequest): Promise<CoreV2HistoryWindow> {
    this.requests.push({ ...request })
    const result = this.windows.shift()
    if (!result) throw new Error('unexpected history window request')
    return result
  }

  async copy(_request: CoreV2HistoryCopyRequest): Promise<string> {
    throw new Error('not used')
  }

  async release(request: { terminalId: string; token: string; generation?: string | number | bigint }): Promise<void> {
    this.releases.push({ ...request })
  }
}

function window(input: {
  lineId: string
  token: string
  generation: string
  op?: 'replace' | 'prepend'
  hasMore: boolean
}): CoreV2HistoryWindow {
  return {
    terminalId: 'terminal-1',
    token: input.token,
    op: input.op ?? 'replace',
    cols: 80,
    rows: 24,
    renderRows: [{
      index: 0,
      cells: [{ text: `line-${input.lineId}`, width: 7 }],
      logicalLineId: input.lineId,
      rowInLine: 0,
    }],
    lines: [],
    beforeOffset: 0,
    loadedRows: 1,
    totalRows: 42,
    loadedLines: 1,
    logicalTotal: 42,
    hasMore: input.hasMore,
    generation: input.generation,
    firstLineId: input.lineId,
    lastLineId: input.lineId,
  }
}
