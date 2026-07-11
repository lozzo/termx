import { describe, expect, it, vi } from 'vitest'
import {
  copyHistorySelection,
  rangeFromHistorySelection,
  searchHistorySurface,
  selectionFromSurfaceRows,
} from './coreV2HistoryInteraction'
import { createCoreV2HistorySource } from './coreV2HistorySource'
import { CORE_V2_TERMINAL_METHODS } from './coreV2TerminalProtocol'
import type { CoreV2HistorySource } from './coreV2HistorySource'
import type { CoreV2HistorySurfaceSnapshot } from './coreV2HistorySurface'
import type { CoreV2HistoryRow } from './coreV2TerminalProtocol'
import type { RtcJsonRpcChannel, RtcSession } from '../core/transport'

describe('CoreV2 history interaction', () => {
  it('copies final text through core-v2 history.copy logical range', async () => {
    const session = new MockCopySession('copied from core')
    const source = createCoreV2HistorySource(session, 'machine-local')
    const snapshot = surfaceSnapshot([
      row('41', 0, 'alpha  '),
      row('42', 0, 'beta'),
      row('43', 0, 'gamma'),
    ])
    const liveSelection = vi.fn(() => {
      throw new Error('live xterm selection must not be used')
    })
    const loadScrollback = vi.fn(() => {
      throw new Error('visual scrollback must not be used')
    })
    const domText = vi.fn(() => {
      throw new Error('renderer text must not be used')
    })

    const selection = selectionFromSurfaceRows(
      snapshot,
      { lineId: '41', rowInLine: 0, col: 2 },
      { lineId: '43', rowInLine: 0, col: 3 },
    )
    const copied = await copyHistorySelection(source, snapshot, selection)

    expect(copied).toBe('copied from core')
    expect(session.requests).toEqual([
      {
        method: CORE_V2_TERMINAL_METHODS.historyCopy,
        params: expect.objectContaining({
          terminal_id: 'terminal-1',
          token: 'token-1',
          history_generation: 7,
          boundary_first_line_id: 41,
          boundary_last_line_id: 43,
          range_valid: true,
          range_start_line_id: 41,
          range_start_col: 2,
          range_end_line_id: 43,
          range_end_col: 3,
        }),
      },
    ])
    expect(liveSelection).not.toHaveBeenCalled()
    expect(loadScrollback).not.toHaveBeenCalled()
    expect(domText).not.toHaveBeenCalled()
  })

  it('decodes history.copy raw bytes returned by the runtime API channel', async () => {
    const session = new MockCopySession({
      bytes: new TextEncoder().encode('byte text'),
      method: CORE_V2_TERMINAL_METHODS.historyCopy,
    })
    const source = createCoreV2HistorySource(session, 'machine-local')
    const snapshot = surfaceSnapshot([row('41', 0, 'alpha')])

    await expect(copyHistorySelection(source, snapshot, {
      anchor: { lineId: '41', col: 0 },
      focus: { lineId: '41', col: 5 },
    })).resolves.toBe('byte text')
  })

  it('normalizes reversed selections and clamps columns to logical line width', () => {
    const snapshot = surfaceSnapshot([
      row('41', 0, 'alpha'),
      row('42', 0, 'beta'),
    ])

    expect(rangeFromHistorySelection(snapshot, {
      anchor: { lineId: '42', col: 99 },
      focus: { lineId: '41', col: -1 },
    })).toEqual({
      startLineId: '41',
      startCol: 0,
      endLineId: '42',
      endCol: 4,
    })
  })

  it('searches only logical-line surface rows and returns logical ranges', () => {
    const snapshot = surfaceSnapshot([
      row('41', 0, 'alpha ALPHA'),
      row('42', 0, 'beta'),
    ])
    const hiddenLiveAppend = vi.fn(() => 'ALPHA from append log')

    expect(searchHistorySurface(snapshot, 'alpha')).toEqual([
      {
        text: 'alpha',
        range: { startLineId: '41', startCol: 0, endLineId: '41', endCol: 5 },
      },
      {
        text: 'ALPHA',
        range: { startLineId: '41', startCol: 6, endLineId: '41', endCol: 11 },
      },
    ])
    expect(searchHistorySurface(snapshot, 'ALPHA', { caseSensitive: true })).toEqual([
      {
        text: 'ALPHA',
        range: { startLineId: '41', startCol: 6, endLineId: '41', endCol: 11 },
      },
    ])
    expect(hiddenLiveAppend).not.toHaveBeenCalled()
  })

  it('rejects stale surfaces before building copy/search ranges', async () => {
    const source: Pick<CoreV2HistorySource, 'copy'> = {
      copy: vi.fn(async () => 'should not run'),
    }
    const snapshot = {
      ...surfaceSnapshot([row('41', 0, 'alpha')]),
      stale: true,
      staleReason: 'generation changed',
    }

    await expect(copyHistorySelection(source, snapshot, {
      anchor: { lineId: '41', col: 0 },
      focus: { lineId: '41', col: 5 },
    })).rejects.toThrow(/stale/)
    expect(() => searchHistorySurface(snapshot, 'alpha')).toThrow(/stale/)
  })
})

class MockCopySession implements Pick<RtcSession, 'openApi' | 'getConnectionInfo'> {
  readonly requests: Array<{ method: string; params: unknown }> = []
  private opened = 0
  private closed = 0

  constructor(private readonly response: unknown) {}

  get openApiCount(): number {
    return this.opened
  }

  get closeApiCount(): number {
    return this.closed
  }

  async getConnectionInfo() {
    return {
      path: 'local' as const,
      connectionId: 'copy-test',
      machineId: 'machine-local',
      relayInUse: false,
    }
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    this.opened += 1
    return {
      request: async <TResponse>(method: string, params?: unknown): Promise<TResponse> => {
        this.requests.push({ method, params })
        return this.response as TResponse
      },
      close: () => {
        this.closed += 1
      },
    }
  }
}

function surfaceSnapshot(rows: CoreV2HistoryRow[]): CoreV2HistorySurfaceSnapshot {
  const firstLineId = rows[0]?.logicalLineId ?? '0'
  const lastLineId = rows.at(-1)?.logicalLineId ?? firstLineId
  return {
    terminalId: 'terminal-1',
    cols: 80,
    viewportRows: 24,
    revision: 1,
    token: 'token-1',
    generation: '7',
    stale: false,
    staleReason: null,
    rows,
    renderRows: rows,
    lines: rows.map((item, index) => ({
      startRow: index,
      endRow: index,
      logicalLineId: item.logicalLineId ?? '',
      clippedBefore: false,
      clippedAfter: false,
    })),
    loadedRows: rows.length,
    totalRows: rows.length,
    logicalTotal: rows.length,
    hasOlder: false,
    hasNewer: false,
    renderWindow: {
      startIndex: 0,
      endIndex: rows.length,
      viewportStartIndex: 0,
      viewportEndIndex: rows.length,
      viewportOffsetRows: 0,
      overscanRows: 0,
      edgePrefetchRows: 0,
      shouldPrefetchOlder: false,
      shouldPrefetchNewer: false,
    },
    firstCursor: { lineId: firstLineId, rowInLine: 0 },
    lastCursor: { lineId: lastLineId, rowInLine: 0 },
    firstLineId,
    lastLineId,
  }
}

function row(lineId: string, rowInLine: number, text: string): CoreV2HistoryRow {
  return {
    index: Number(lineId),
    logicalLineId: lineId,
    rowInLine,
    cells: [{ text, width: text.length }],
  }
}
