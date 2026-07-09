import { describe, expect, it } from 'vitest'
import {
  CORE_V2_TERMINAL_METHODS,
  assertLiveCacheOnlyAPIName,
  coreV2EventFromRuntimeEvent,
  coreV2HistoryCopyRequestToProtocolRequest,
  coreV2HistoryReleaseRequestToProtocolRequest,
  coreV2HistoryWindowFromAPI,
  coreV2HistoryWindowRequestToParams,
} from './coreV2TerminalProtocol'

describe('core-v2 terminal protocol contract', () => {
  it('maps latest history windows to core-v2 history.window params without visual row cursors', () => {
    expect(coreV2HistoryWindowRequestToParams({
      terminalId: 'term-1',
      mode: 'latest',
      limit: 50,
      cols: 120,
    })).toEqual({
      terminal_id: 'term-1',
      before_offset: 0,
      limit: 50,
      cols: 120,
      mode: '',
      token: '',
      history_generation: 0,
      cursor_valid: false,
      before_line_id: 0,
      before_row_in_line: 0,
      after_cursor_valid: false,
      after_line_id: 0,
      after_row_in_line: 0,
      boundary_first_line_id: 0,
      boundary_last_line_id: 0,
      range_valid: false,
      range_start_line_id: 0,
      range_start_col: 0,
      range_end_line_id: 0,
      range_end_col: 0,
    })
  })

  it('maps older and newer windows through logical-line cursors and frozen token guards', () => {
    expect(coreV2HistoryWindowRequestToParams({
      terminalId: 'term-1',
      mode: 'older',
      token: 'snap-token',
      generation: '7',
      limit: 25,
      cols: 100,
      beforeCursor: { lineId: '42', rowInLine: 3 },
      boundaryFirstLineId: '40',
      boundaryLastLineId: '48',
    })).toEqual(expect.objectContaining({
      terminal_id: 'term-1',
      mode: '',
      token: 'snap-token',
      history_generation: 7,
      cursor_valid: true,
      before_line_id: 42,
      before_row_in_line: 3,
      boundary_first_line_id: 40,
      boundary_last_line_id: 48,
    }))

    expect(coreV2HistoryWindowRequestToParams({
      terminalId: 'term-1',
      mode: 'newer',
      token: 'snap-token',
      limit: 25,
      cols: 100,
      afterCursor: { lineId: '45', rowInLine: 0 },
      boundaryFirstLineId: '40',
      boundaryLastLineId: '48',
    })).toEqual(expect.objectContaining({
      mode: 'newer',
      token: 'snap-token',
      after_cursor_valid: true,
      after_line_id: 45,
      after_row_in_line: 0,
      cursor_valid: false,
    }))
  })

  it('makes copy a logical range request and rejects cache-like live names', () => {
    const request = coreV2HistoryCopyRequestToProtocolRequest({
      terminalId: 'term-1',
      token: 'snap-token',
      cols: 80,
      generation: 9n,
      boundaryFirstLineId: '41',
      boundaryLastLineId: '49',
      range: {
        startLineId: '42',
        startCol: 1,
        endLineId: '48',
        endCol: 3,
      },
    })

    expect(request.method).toBe(CORE_V2_TERMINAL_METHODS.historyCopy)
    expect(request.params).toEqual(expect.objectContaining({
      terminal_id: 'term-1',
      token: 'snap-token',
      history_generation: 9,
      range_valid: true,
      range_start_line_id: 42,
      range_start_col: 1,
      range_end_line_id: 48,
      range_end_col: 3,
    }))
    expect(() => assertLiveCacheOnlyAPIName('loadScrollback')).toThrow(/live display cache API/)
    expect(() => assertLiveCacheOnlyAPIName('history.window')).not.toThrow()
  })

  it('maps history.release to core-v2 token release without scrollback semantics', () => {
    expect(coreV2HistoryReleaseRequestToProtocolRequest({
      terminalId: 'term-1',
      token: 'snap-token',
    })).toEqual({
      method: CORE_V2_TERMINAL_METHODS.historyRelease,
      params: expect.objectContaining({
        terminal_id: 'term-1',
        token: 'snap-token',
        range_valid: false,
        cursor_valid: false,
      }),
    })
  })

  it('normalizes history.window payload as logical-line render data', () => {
    const window = coreV2HistoryWindowFromAPI({
      terminal_id: 'term-1',
      token: 'g7:0-2:c80',
      op: 'replace',
      size: { cols: 80, rows: 24 },
      rows: [{
        cells: [
          { r: 'ERR', w: 3, s: { fg: 'ansi:1', b: true } },
          { r: '好', w: 2, s: { fg: '#ffcc00', u: true }, link_url: 'file://build.log', link_params: 'line=7' },
        ],
      }],
      row_logical_line_ids: [42],
      row_in_line: [1],
      line_start_rows: [0],
      line_end_rows: [0],
      line_row_kinds: ['output'],
      line_logical_line_ids: [42],
      line_clipped_before: [true],
      line_clipped_after: [false],
      before_offset: 3,
      loaded_rows: 1,
      total_rows: 12,
      loaded_lines: 1,
      logical_total: 4,
      has_more: true,
      history_generation: 7n,
      first_row_id: 100n,
      last_row_id: 100n,
      first_line_id: 42n,
      last_line_id: 42n,
      cursor_valid: true,
      cursor_before_line_id: 42n,
      cursor_before_row_in_line: 1,
    })

    expect(window).toEqual(expect.objectContaining({
      terminalId: 'term-1',
      token: 'g7:0-2:c80',
      op: 'replace',
      cols: 80,
      logicalTotal: 4,
      generation: '7',
      firstLineId: '42',
      cursor: { lineId: '42', rowInLine: 1 },
    }))
    expect(window.renderRows[0]?.logicalLineId).toBe('42')
    expect(window.renderRows[0]?.rowInLine).toBe(1)
    expect(window.renderRows[0]?.cells[0]).toEqual(expect.objectContaining({
      text: 'ERR',
      width: 3,
      style: expect.objectContaining({ fg: 'ansi:1', bold: true }),
    }))
    expect(window.renderRows[0]?.cells[1]).toEqual(expect.objectContaining({
      text: '好',
      width: 2,
      linkUrl: 'file://build.log',
      linkParams: 'line=7',
    }))
    expect(window.lines[0]).toEqual(expect.objectContaining({
      logicalLineId: '42',
      clippedBefore: true,
      clippedAfter: false,
    }))
  })

  it('normalizes core-v2 protocol event ids without inventing UI state truth', () => {
    expect(coreV2EventFromRuntimeEvent({
      protocolType: 2,
      terminalId: 'term-1',
      timestampUnixNano: 1_700_000_000_000_000_000n,
    })).toEqual({
      type: 'terminal.state_changed',
      protocolType: 2,
      terminalId: 'term-1',
      timestampUnixMs: 1_700_000_000_000,
      payload: undefined,
    })
  })
})
