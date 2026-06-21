import { describe, expect, it, vi } from 'vitest'
import { createCoreV2HistorySource } from './coreV2HistorySource'
import { CORE_V2_TERMINAL_METHODS } from './coreV2TerminalProtocol'
import type { RtcJsonRpcChannel, RtcSession } from '../core/transport'

describe('CoreV2HistorySource', () => {
  it('requests logical-line history windows through the machine api channel', async () => {
    const session = new MockHistorySession()
    const source = createCoreV2HistorySource(session, 'machine-local')

    const latest = await source.window({
      terminalId: 'terminal-1',
      mode: 'latest',
      limit: 20,
      cols: 80,
    })
    const older = await source.window({
      terminalId: 'terminal-1',
      mode: 'older',
      limit: 10,
      cols: 80,
      token: latest.token,
      generation: latest.generation,
      beforeCursor: latest.cursor,
      boundaryFirstLineId: latest.firstLineId,
      boundaryLastLineId: latest.lastLineId,
    })

    expect(session.requests).toEqual([
      {
        method: CORE_V2_TERMINAL_METHODS.historyWindow,
        params: expect.objectContaining({
          terminal_id: 'terminal-1',
          limit: 20,
          cols: 80,
          token: '',
          cursor_valid: false,
        }),
      },
      {
        method: CORE_V2_TERMINAL_METHODS.historyWindow,
        params: expect.objectContaining({
          terminal_id: 'terminal-1',
          limit: 10,
          cols: 80,
          token: 'hist-token',
          history_generation: 7,
          cursor_valid: true,
          before_line_id: 42,
          before_row_in_line: 0,
          boundary_first_line_id: 42,
          boundary_last_line_id: 43,
        }),
      },
    ])
    expect(session.openApiCount).toBe(2)
    expect(session.closeApiCount).toBe(2)
    expect(latest.lines).toEqual([
      expect.objectContaining({ logicalLineId: '42' }),
      expect.objectContaining({ logicalLineId: '43' }),
    ])
    expect(latest.renderRows[0]).toEqual(expect.objectContaining({
      logicalLineId: '42',
      cells: [expect.objectContaining({ text: 'hello', width: 5 })],
    }))
    expect(older.op).toBe('prepend')
    expect(older.renderRows[0]?.logicalLineId).toBe('41')
    expect(session.loadScrollback).not.toHaveBeenCalled()
    expect(session.openTerminal).not.toHaveBeenCalled()
  })

  it('rejects machine mismatches before opening the api channel', async () => {
    const session = new MockHistorySession()
    const source = createCoreV2HistorySource(session, 'machine-other')

    await expect(source.window({
      terminalId: 'terminal-1',
      mode: 'latest',
      limit: 20,
      cols: 80,
    })).rejects.toThrow(/machine-local.*machine-other/)
    expect(session.openApiCount).toBe(0)
  })
})

class MockHistorySession implements Pick<RtcSession, 'openApi' | 'getConnectionInfo'> {
  readonly requests: Array<{ method: string; params: unknown }> = []
  readonly openTerminal = vi.fn()
  readonly loadScrollback = vi.fn()
  openApiCount = 0
  closeApiCount = 0

  async getConnectionInfo() {
    return {
      path: 'local' as const,
      connectionId: 'history-source-test',
      machineId: 'machine-local',
      relayInUse: false,
    }
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    this.openApiCount += 1
    return {
      request: async <TResponse>(method: string, params?: unknown): Promise<TResponse> => {
        this.requests.push({ method, params })
        const record = params as { cursor_valid?: boolean }
        return (record.cursor_valid ? olderWindowPayload() : latestWindowPayload()) as TResponse
      },
      close: () => {
        this.closeApiCount += 1
      },
    }
  }
}

function latestWindowPayload() {
  return {
    terminal_id: 'terminal-1',
    token: 'hist-token',
    op: 'replace',
    size: { cols: 80, rows: 24 },
    rows: [
      { cells: [{ r: 'hello', w: 5 }] },
      { cells: [{ r: 'world', w: 5 }] },
    ],
    row_logical_line_ids: [42, 43],
    row_in_line: [0, 0],
    line_start_rows: [0, 1],
    line_end_rows: [0, 1],
    line_logical_line_ids: [42, 43],
    line_clipped_before: [false, false],
    line_clipped_after: [false, false],
    before_offset: 2,
    loaded_rows: 2,
    total_rows: 3,
    loaded_lines: 2,
    logical_total: 3,
    has_more: true,
    history_generation: 7,
    first_line_id: 42,
    last_line_id: 43,
    cursor_valid: true,
    cursor_before_line_id: 42,
    cursor_before_row_in_line: 0,
  }
}

function olderWindowPayload() {
  return {
    terminal_id: 'terminal-1',
    token: 'hist-token',
    op: 'prepend',
    size: { cols: 80, rows: 24 },
    rows: [
      { cells: [{ r: 'older', w: 5 }] },
    ],
    row_logical_line_ids: [41],
    row_in_line: [0],
    line_start_rows: [0],
    line_end_rows: [0],
    line_logical_line_ids: [41],
    line_clipped_before: [false],
    line_clipped_after: [false],
    before_offset: 1,
    loaded_rows: 1,
    total_rows: 3,
    loaded_lines: 1,
    logical_total: 3,
    has_more: false,
    history_generation: 7,
    first_line_id: 41,
    last_line_id: 41,
  }
}
